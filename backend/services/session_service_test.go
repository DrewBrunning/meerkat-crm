package services

import (
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Issue #866 (ADR 0017): sessions rows are the server-side handle on a login —
// per-device logout, idle timeout, bulk revoke on a password/2FA change, and a
// job-locked TTL purge. Real migrated schema (dbtest), not AutoMigrate: the
// raw DELETE in PurgeExpiredSessions names the real columns (backend trap #1).

func newSessionDB(t *testing.T) (*gorm.DB, uint) {
	t.Helper()
	db := dbtest.New(t)
	user := models.User{Username: "sess-svc", Email: "sess-svc@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	return db, user.ID
}

func sessionCfg() *config.Config {
	return &config.Config{JWTSecretKey: "session-service-test-secret-key-32b", JWTExpiryHours: 96}
}

func countSessions(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.Session{}).Count(&n).Error)
	return n
}

func TestCreateSession_PersistsRowWithAbsoluteExpiry(t *testing.T) {
	db, uid := newSessionDB(t)

	id, err := CreateSession(db, uid, sessionCfg(), "Mozilla/5.0", "203.0.113.7")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	var s models.Session
	require.NoError(t, db.First(&s, "id = ?", id).Error)
	assert.Equal(t, uid, s.UserID)
	assert.Nil(t, s.RevokedAt)
	assert.Equal(t, "203.0.113.7", s.IP)
	assert.WithinDuration(t, time.Now().Add(96*time.Hour), s.ExpiresAt, time.Minute)
	assert.WithinDuration(t, s.CreatedAt, s.LastSeenAt, time.Second)
}

func TestCreateSession_TruncatesHostileFields(t *testing.T) {
	db, uid := newSessionDB(t)
	longUA := make([]byte, 2000)
	for i := range longUA {
		longUA[i] = 'a'
	}

	id, err := CreateSession(db, uid, sessionCfg(), string(longUA), string(longUA))
	require.NoError(t, err)

	var s models.Session
	require.NoError(t, db.First(&s, "id = ?", id).Error)
	assert.Len(t, s.UserAgent, 512)
	assert.Len(t, s.IP, 64)
}

func TestIssueSession_MintsRowAndTokenCarryingSid(t *testing.T) {
	db, uid := newSessionDB(t)
	var user models.User
	require.NoError(t, db.First(&user, uid).Error)

	tok, err := IssueSession(db, user, sessionCfg(), "ua", "ip")
	require.NoError(t, err)

	sid := SessionIDFromToken(tok, sessionCfg())
	require.NotEmpty(t, sid)

	var s models.Session
	require.NoError(t, db.First(&s, "id = ?", sid).Error)
	assert.Equal(t, uid, s.UserID)
}

func TestCreateSession_ClosedDBErrors(t *testing.T) {
	db, uid := newSessionDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = CreateSession(db, uid, sessionCfg(), "", "")
	assert.Error(t, err)
}

func TestIssueSession_PropagatesCreateError(t *testing.T) {
	db, uid := newSessionDB(t)
	var user models.User
	require.NoError(t, db.First(&user, uid).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = IssueSession(db, user, sessionCfg(), "", "")
	assert.Error(t, err)
}

func TestRevokeSession_SetsRevokedAtOnceAndIsIdempotent(t *testing.T) {
	db, uid := newSessionDB(t)
	id, err := CreateSession(db, uid, sessionCfg(), "", "")
	require.NoError(t, err)

	require.NoError(t, RevokeSession(db, id))
	var s models.Session
	require.NoError(t, db.First(&s, "id = ?", id).Error)
	require.NotNil(t, s.RevokedAt)
	first := *s.RevokedAt

	// A second revoke is a no-op (WHERE revoked_at IS NULL) and not an error.
	require.NoError(t, RevokeSession(db, id))
	require.NoError(t, db.First(&s, "id = ?", id).Error)
	assert.Equal(t, first.UnixNano(), s.RevokedAt.UnixNano())

	// Empty / unknown id: also no error.
	require.NoError(t, RevokeSession(db, ""))
	require.NoError(t, RevokeSession(db, "no-such-session"))
}

func TestRevokeAllSessions_RevokesOnlyActiveRowsForThatUser(t *testing.T) {
	db, uid := newSessionDB(t)
	other := models.User{Username: "sess-other", Email: "sess-other@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)

	a, _ := CreateSession(db, uid, sessionCfg(), "", "")
	_, _ = CreateSession(db, uid, sessionCfg(), "", "")
	require.NoError(t, RevokeSession(db, a)) // already revoked — not recounted
	foreign, _ := CreateSession(db, other.ID, sessionCfg(), "", "")

	n, err := RevokeAllSessions(db, uid)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "only the still-active row for this user")

	var f models.Session
	require.NoError(t, db.First(&f, "id = ?", foreign).Error)
	assert.Nil(t, f.RevokedAt, "another user's session is untouched")
}

func TestPurgeExpiredSessions_RemovesExpiredAndLongRevokedKeepsLive(t *testing.T) {
	db, uid := newSessionDB(t)
	now := time.Now()

	mk := func(expOffset time.Duration, revokedAgo time.Duration) string {
		id, err := CreateSession(db, uid, sessionCfg(), "", "")
		require.NoError(t, err)
		upd := map[string]any{"expires_at": now.Add(expOffset)}
		if revokedAgo > 0 {
			upd["revoked_at"] = now.Add(-revokedAgo)
		}
		require.NoError(t, db.Model(&models.Session{}).Where("id = ?", id).Updates(upd).Error)
		return id
	}

	live := mk(24*time.Hour, 0)
	recentlyRevoked := mk(24*time.Hour, 1*time.Hour) // within grace
	expired := mk(-1*time.Hour, 0)
	longRevoked := mk(24*time.Hour, 48*time.Hour) // past 24h grace

	PurgeExpiredSessions(db)

	assert.EqualValues(t, 2, countSessions(t, db))
	require.NoError(t, db.First(&models.Session{}, "id = ?", live).Error)
	require.NoError(t, db.First(&models.Session{}, "id = ?", recentlyRevoked).Error)
	assert.Error(t, db.First(&models.Session{}, "id = ?", expired).Error)
	assert.Error(t, db.First(&models.Session{}, "id = ?", longRevoked).Error)
}

func TestPurgeExpiredSessionsScheduled_JobLockGuards(t *testing.T) {
	db, uid := newSessionDB(t)
	seedExpired := func() {
		id, err := CreateSession(db, uid, sessionCfg(), "", "")
		require.NoError(t, err)
		require.NoError(t, db.Model(&models.Session{}).Where("id = ?", id).
			Update("expires_at", time.Now().Add(-time.Hour)).Error)
	}

	seedExpired()
	PurgeExpiredSessionsScheduled(db)
	assert.EqualValues(t, 0, countSessions(t, db), "first scheduled run purges")

	seedExpired()
	PurgeExpiredSessionsScheduled(db)
	assert.EqualValues(t, 1, countSessions(t, db), "the job lock suppresses the immediate second run")
}

func TestPurgeExpiredSessions_DBErrorIsReturnedNotPanic(t *testing.T) {
	buf := captureLoggerOutput(t)
	db, uid := newSessionDB(t)
	_, err := CreateSession(db, uid, sessionCfg(), "", "")
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	var purgeErr error
	require.NotPanics(t, func() { purgeErr = PurgeExpiredSessions(db) })
	require.Error(t, purgeErr, "a failing purge must report the error so the run is recorded as failed")
	assert.Contains(t, buf.String(), "session purge: failed to delete expired sessions")
}

func TestSessionIDFromToken_RejectsBadInputAcceptsExpired(t *testing.T) {
	cfg := sessionCfg()
	db, uid := newSessionDB(t)
	var user models.User
	require.NoError(t, db.First(&user, uid).Error)

	assert.Empty(t, SessionIDFromToken("", cfg))
	assert.Empty(t, SessionIDFromToken("not-a-jwt", cfg))

	// Wrong signing key → empty.
	tok, err := IssueSession(db, user, cfg, "", "")
	require.NoError(t, err)
	assert.Empty(t, SessionIDFromToken(tok, &config.Config{JWTSecretKey: "different-secret-key-32-characters"}))

	// Expired but correctly signed → still yields the sid (logout must be able
	// to revoke the row a stale token names).
	expiredTok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID, "sid": "sid-expired",
		"exp": time.Now().Add(-time.Hour).Unix(),
	}).SignedString([]byte(cfg.JWTSecretKey))
	require.NoError(t, err)
	assert.Equal(t, "sid-expired", SessionIDFromToken(expiredTok, cfg))
}
