package services

import (
	"errors"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// CON-04 (issue #459, ADR 0010): idempotency_keys rows are transient replay
// bookkeeping — a key protects one operation across its retries, which happen
// within seconds to minutes. PurgeExpiredIdempotencyKeys hard-deletes rows
// past IDEMPOTENCY_KEY_RETENTION_HOURS (default 24), anchored on created_at.
//
// Real migrated schema (dbtest), not AutoMigrate — the raw DELETE names the
// real idempotency_keys columns (CLAUDE.md backend trap #1).

func newIdempotencyPurgeDB(t *testing.T) (*gorm.DB, uint) {
	t.Helper()
	db := dbtest.New(t)
	user := models.User{Username: "idem-purge", Email: "idem-purge@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	return db, user.ID
}

func seedIdemKey(t *testing.T, db *gorm.DB, userID uint, key string, age time.Duration) {
	t.Helper()
	ts := time.Now().Add(-age)
	require.NoError(t, db.Create(&models.IdempotencyKey{
		UserID: userID, Key: key, Method: "POST", Path: "/api/v1/contacts",
		RequestFingerprint: "fp-" + key, State: models.IdempotencyStateCompleted,
		ResponseStatus: 201, CreatedAt: ts, UpdatedAt: ts,
	}).Error)
}

func countIdemKeys(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.IdempotencyKey{}).Count(&n).Error)
	return n
}

func TestPurgeExpiredIdempotencyKeys_RemovesOnlyExpired(t *testing.T) {
	db, uid := newIdempotencyPurgeDB(t)
	seedIdemKey(t, db, uid, "fresh", 1*time.Hour)
	seedIdemKey(t, db, uid, "borderline", 23*time.Hour)
	seedIdemKey(t, db, uid, "stale", 25*time.Hour)
	seedIdemKey(t, db, uid, "ancient", 30*24*time.Hour)

	PurgeExpiredIdempotencyKeys(db, config.Config{IdempotencyKeyRetentionHours: 24})

	assert.EqualValues(t, 2, countIdemKeys(t, db), "only rows younger than 24h survive")
	var survivors []string
	require.NoError(t, db.Model(&models.IdempotencyKey{}).Order("idempotency_key").Pluck("idempotency_key", &survivors).Error)
	assert.Equal(t, []string{"borderline", "fresh"}, survivors)
}

func TestPurgeExpiredIdempotencyKeys_NonPositiveRetentionDisables(t *testing.T) {
	db, uid := newIdempotencyPurgeDB(t)
	seedIdemKey(t, db, uid, "ancient", 90*24*time.Hour)

	PurgeExpiredIdempotencyKeys(db, config.Config{IdempotencyKeyRetentionHours: 0})
	assert.EqualValues(t, 1, countIdemKeys(t, db), "retention <= 0 disables the purge, it must not wipe the table")

	PurgeExpiredIdempotencyKeys(db, config.Config{IdempotencyKeyRetentionHours: -5})
	assert.EqualValues(t, 1, countIdemKeys(t, db))
}

func TestPurgeExpiredIdempotencyKeysScheduled_JobLockGuards(t *testing.T) {
	db, uid := newIdempotencyPurgeDB(t)
	seedIdemKey(t, db, uid, "ancient", 48*time.Hour)

	cfg := config.Config{IdempotencyKeyRetentionHours: 24}
	PurgeExpiredIdempotencyKeysScheduled(db, cfg)
	assert.EqualValues(t, 0, countIdemKeys(t, db), "first scheduled run purges")

	// Immediately re-running is suppressed by the job lock (min-interval),
	// so a fresh stale row seeded now survives until the interval elapses.
	seedIdemKey(t, db, uid, "ancient-2", 48*time.Hour)
	PurgeExpiredIdempotencyKeysScheduled(db, cfg)
	assert.EqualValues(t, 1, countIdemKeys(t, db), "the job lock suppresses the immediate second run")
}

// --- failure branches (issue #809) -----------------------------------------

// TestPurgeExpiredIdempotencyKeys_DBErrorIsReturnedNotPanic pins the
// DELETE-error branch of the plain purge: a failed statement is logged and
// returned (issue #975), it must not panic. (Same close-the-sql.DB-mid-test
// technique as
// db_integrity_service_test.go's TestCheckDBIntegrityErrorsOnClosedConnection.)
func TestPurgeExpiredIdempotencyKeys_DBErrorIsReturnedNotPanic(t *testing.T) {
	buf := captureLoggerOutput(t)
	db, uid := newIdempotencyPurgeDB(t)
	seedIdemKey(t, db, uid, "ancient", 48*time.Hour)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	var purgeErr error
	require.NotPanics(t, func() {
		purgeErr = PurgeExpiredIdempotencyKeys(db, config.Config{IdempotencyKeyRetentionHours: 24})
	})
	require.Error(t, purgeErr, "a failing purge must report the error so the run is recorded as failed")
	require.Contains(t, buf.String(), "idempotency key purge: failed to delete expired keys")
}

// TestPurgeExpiredIdempotencyKeysScheduled_LockCheckDBFailureIsSilentNoOp
// documents the *current* contract of a DB-level failure during the lock check,
// which issue #809's item 9 assumed would hit the "failed to check job lock"
// error branch. It cannot: acquireJobLock normalizes every transaction failure
// into (acquired=false, err=nil) — job_lock.go returns `err == nil, nil` — so a
// closed-DB lock check is a quiet suppression, not an error. That branch is
// therefore unreachable and carries a `# pragma: no cover` justification in
// idempotency_purge_service.go. This test pins the actual behavior so a future
// acquireJobLock that propagates errors makes this test fail loudly (it would
// then log "failed to check job lock") instead of changing semantics silently.
func TestPurgeExpiredIdempotencyKeysScheduled_LockCheckDBFailureIsSilentNoOp(t *testing.T) {
	buf := captureLoggerOutput(t)
	db, uid := newIdempotencyPurgeDB(t)
	seedIdemKey(t, db, uid, "ancient", 48*time.Hour)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	require.NotPanics(t, func() {
		PurgeExpiredIdempotencyKeysScheduled(db, config.Config{IdempotencyKeyRetentionHours: 24})
	})
	require.NotContains(t, buf.String(), "idempotency key purge: failed to check job lock",
		"acquireJobLock normalizes DB failures to acquired=false; the error branch is unreachable today")
	require.NotContains(t, buf.String(), "Purged expired idempotency keys",
		"the job must not run (and therefore not purge) when the lock check cannot reach the database")
}

// TestPurgeExpiredIdempotencyKeysScheduled_ReleaseLockErrorIsReturned pins the
// deferred releaseJobLock-error branch. There is no clean seam that fails only
// the *release*: acquire and release both run inside db.Transaction, and
// closing the whole DB makes the acquire fail first (the
// LockCheckDBFailureIsSilentNoOp test above). The release's one distinguishing
// operation is the final tx.Save(&job) — an UPDATE — so that is poisoned with a
// GORM update callback registered after seeding. Acquire (query + insert) and
// the purge DELETE (raw Exec) are unaffected, so the job runs and only the
// deferred cleanup fails — the branch under test. A failed release must be
// surfaced (issue #975) so the run isn't reported as a success.
func TestPurgeExpiredIdempotencyKeysScheduled_ReleaseLockErrorIsReturned(t *testing.T) {
	buf := captureLoggerOutput(t)
	db, uid := newIdempotencyPurgeDB(t)
	seedIdemKey(t, db, uid, "ancient", 48*time.Hour)

	require.NoError(t, db.Callback().Update().Before("gorm:update").
		Register("idempotency_purge_test_fail_release", func(tx *gorm.DB) {
			tx.AddError(errors.New("simulated release failure"))
		}))

	var purgeErr error
	require.NotPanics(t, func() {
		purgeErr = PurgeExpiredIdempotencyKeysScheduled(db, config.Config{IdempotencyKeyRetentionHours: 24})
	})
	require.Error(t, purgeErr, "a failed lock release must be surfaced, not swallowed")
	require.Contains(t, buf.String(), "idempotency key purge: failed to release job lock")
}
