package services

import (
	"errors"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"gorm.io/gorm"
)

// idempotencyKeyPurgeMinInterval is slightly less than the cron cadence so a
// natural clock-skew or overlap doesn't cause a skipped run (mirrors the other
// purge jobs' constants).
var idempotencyKeyPurgeMinInterval = JobCatchupWindow(6 * time.Hour)

// PurgeExpiredIdempotencyKeys hard-deletes idempotency_keys rows older than the
// retention window (IDEMPOTENCY_KEY_RETENTION_HOURS, default 24 — issue #459,
// CON-04, ADR 0010).
//
// The window is short by design: an Idempotency-Key protects a single client
// operation across its retries, which happen within seconds to minutes, not
// days. A day is a generous ceiling. Each row can hold a copy of the created
// entity in response_body, so the window also bounds that copy the way
// WEBHOOK_DELIVERY_RETENTION_DAYS bounds a delivery payload.
//
// The window anchors on created_at (when the key was first claimed). A
// non-positive retention value disables the purge rather than deleting every
// row, matching webhook_delivery_purge_service.go / audit_purge_service.go.
//
// It returns the delete error (nil when disabled or successful) so the
// scheduled caller records a failing purge as `failed` instead of advancing
// last_run_at as success (issue #975).
func PurgeExpiredIdempotencyKeys(db *gorm.DB, cfg config.Config) error {
	if cfg.IdempotencyKeyRetentionHours <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(cfg.IdempotencyKeyRetentionHours) * time.Hour)

	result := db.Exec("DELETE FROM idempotency_keys WHERE created_at < ?", cutoff)
	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("idempotency key purge: failed to delete expired keys")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info().Int64("rows", result.RowsAffected).Time("cutoff", cutoff).Msg("Purged expired idempotency keys")
	}
	return nil
}

// PurgeExpiredIdempotencyKeysScheduled is the scheduled cron entry point; it
// acquires a job lock so concurrent runs (multi-instance, rapid restarts)
// don't double-purge. The purge's outcome is passed to releaseJobLock so a
// failure is recorded as `failed` and leaves last_run_at stale for
// job_stopped (issue #975).
func PurgeExpiredIdempotencyKeysScheduled(db *gorm.DB, cfg config.Config) (err error) {
	acquired, lockErr := acquireJobLock(db, models.JobNameIdempotencyKeyPurge, idempotencyKeyPurgeMinInterval)
	// The error branch below is structurally unreachable today: acquireJobLock
	// normalizes every transaction failure (a lost connection, a locked DB)
	// into (acquired=false, err=nil) — job_lock.go returns `err == nil, nil` —
	// so a failing lock check lands in the `!acquired` path, not here. Kept as
	// the defensive guard the (bool, error) contract implies for a future
	// acquireJobLock that propagates errors.
	if lockErr != nil { // # pragma: no cover — unreachable: acquireJobLock never returns an error (job_lock.go returns err == nil, nil)
		logger.Error().Err(lockErr).Msg("idempotency key purge: failed to check job lock") // # pragma: no cover — see the comment above
		return lockErr                                                                     // # pragma: no cover — see the comment above
	}
	if !acquired {
		return nil
	}
	defer func() {
		if relErr := releaseJobLock(db, models.JobNameIdempotencyKeyPurge, err == nil); relErr != nil {
			logger.Error().Err(relErr).Msg("idempotency key purge: failed to release job lock")
			err = errors.Join(err, relErr)
		}
	}()

	return PurgeExpiredIdempotencyKeys(db, cfg)
}
