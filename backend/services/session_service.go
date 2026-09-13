package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"gorm.io/gorm"
)

// sessionIDBytes is the entropy of a session id / JWT `sid` claim. 32 bytes
// (256 bits) matches the device-grant token and is well past ASVS 3.2.2's
// 64-bit floor; the id is an opaque lookup key, never shown to the user.
const sessionIDBytes = 32

// sessionRevokedGrace is how long a revoked (but not yet absolutely expired)
// row is kept before the purge job removes it, so "when did this session end?"
// stays answerable on the admin timeline for a while after a logout.
const sessionRevokedGrace = 24 * time.Hour

// sessionPurgeMinInterval guards the scheduled purge against a double run on
// overlap / rapid restart, slightly under the 6h cron cadence (mirrors the
// other purge jobs' constants).
var sessionPurgeMinInterval = JobCatchupWindow(6 * time.Hour)

// CreateSession writes one server-side session row and returns its id, which
// the caller embeds in the JWT's `sid` claim. expires_at is the absolute
// ceiling (created_at + JWT_EXPIRY_HOURS) — the same window the token's own
// `exp` carries — so a row can never outlive the token that points at it.
func CreateSession(db *gorm.DB, userID uint, cfg *config.Config, userAgent, ip string) (string, error) {
	raw := make([]byte, sessionIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err // # pragma: no cover — crypto/rand only fails on catastrophic OS entropy exhaustion
	}
	id := base64.RawURLEncoding.EncodeToString(raw)

	now := time.Now()
	session := models.Session{
		ID:         id,
		UserID:     userID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(time.Duration(cfg.JWTExpiryHours) * time.Hour),
		UserAgent:  truncate(userAgent, 512),
		IP:         truncate(ip, 64),
	}
	if err := db.Create(&session).Error; err != nil {
		return "", err
	}
	return id, nil
}

// IssueSession mints the server-side session row and returns a signed JWT
// carrying its id in the `sid` claim. It is the one entry point every
// interactive-login path uses (password login, 2FA completion, OIDC callback,
// device-grant exchange, and the post-password-change re-issue) so the token
// and its row are always created together.
func IssueSession(db *gorm.DB, user models.User, cfg *config.Config, userAgent, ip string) (string, error) {
	sid, err := CreateSession(db, user.ID, cfg, userAgent, ip)
	if err != nil {
		return "", err
	}
	return GenerateToken(user, cfg, sid)
}

// RevokeSession marks a single session revoked (the per-device logout path).
// A no-op — no error — when the id is empty, unknown, or already revoked, so
// callers on the best-effort logout path don't have to special-case any of
// those.
func RevokeSession(db *gorm.DB, sid string) error {
	if sid == "" {
		return nil
	}
	return db.Model(&models.Session{}).
		Where("id = ? AND revoked_at IS NULL", sid).
		Update("revoked_at", time.Now()).Error
}

// RevokeAllSessions revokes every currently-active session for a user and
// returns how many were revoked. Shared by every call site that already ends
// a user's sessions in bulk by bumping TokenVersion — password change/reset,
// admin password reset, 2FA enable/disable/regenerate — so those actions also
// leave the server-side rows in a revoked state instead of lingering until
// the idle/expiry purge. Mirrors RevokeAllDeviceGrants.
func RevokeAllSessions(db *gorm.DB, userID uint) (int64, error) {
	result := db.Model(&models.Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now())
	return result.RowsAffected, result.Error
}

// PurgeExpiredSessions hard-deletes sessions rows that are past their absolute
// expiry or have been revoked longer than the grace window. A no-op-safe
// DELETE; unlike the retention-days purges there is no operator knob to
// disable it — an expired session row is dead weight with no recovery value.
//
// It returns the delete error (nil on success) so the scheduled caller records
// a failing purge as `failed` instead of advancing last_run_at as success
// (issue #975).
func PurgeExpiredSessions(db *gorm.DB) error {
	now := time.Now()
	result := db.Exec(
		"DELETE FROM sessions WHERE expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)",
		now, now.Add(-sessionRevokedGrace),
	)
	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("session purge: failed to delete expired sessions")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info().Int64("rows", result.RowsAffected).Msg("Purged expired sessions")
	}
	return nil
}

// PurgeExpiredSessionsScheduled is the scheduled cron entry point; it acquires
// a job lock so concurrent runs (multi-instance, rapid restarts) don't
// double-purge. The purge's outcome is passed to releaseJobLock so a failure
// is recorded as `failed` and leaves last_run_at stale for job_stopped
// (issue #975).
func PurgeExpiredSessionsScheduled(db *gorm.DB) (err error) {
	acquired, lockErr := acquireJobLock(db, models.JobNameSessionPurge, sessionPurgeMinInterval)
	if lockErr != nil { // # pragma: no cover — acquireJobLock never returns an error (job_lock.go returns err == nil, nil)
		logger.Error().Err(lockErr).Msg("session purge: failed to check job lock") // # pragma: no cover — see the comment above
		return lockErr                                                             // # pragma: no cover — see the comment above
	}
	if !acquired {
		return nil
	}
	defer func() {
		if relErr := releaseJobLock(db, models.JobNameSessionPurge, err == nil); relErr != nil {
			logger.Error().Err(relErr).Msg("session purge: failed to release job lock") // # pragma: no cover — releaseJobLock only errors on a failing store
			err = errors.Join(err, relErr)                                              // # pragma: no cover — see the comment above
		}
	}()

	return PurgeExpiredSessions(db)
}

// truncate caps a captured string field at n bytes so a hostile User-Agent /
// forwarded-for header can't bloat the row. Byte-wise is fine — the value is
// display-only and never re-parsed.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
