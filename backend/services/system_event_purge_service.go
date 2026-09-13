package services

import (
	"context"
	"errors"

	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"time"

	"gorm.io/gorm"
)

// systemEventPurgeMinInterval is slightly less than the 24h cron cadence so a
// natural clock-skew or overlap doesn't cause a skipped run (mirrors the
// audit purge job's own constant).
var systemEventPurgeMinInterval = JobCatchupWindow(24 * time.Hour)

// PurgeExpiredSystemEvents hard-deletes system_events rows older than the
// retention window (SYSTEM_EVENT_RETENTION_DAYS, default 30 — operational
// diagnostics, short-lived by design; long enough to investigate an incident,
// short enough to bound growth on a single-file SQLite database). This is the
// only delete path for the table.
//
// It returns the delete error (nil when disabled or successful) so the
// scheduled caller records a failing purge as `failed` instead of advancing
// last_run_at as success (issue #975).
func PurgeExpiredSystemEvents(ctx context.Context, db *gorm.DB, cfg config.Config) error {
	if cfg.SystemEventRetentionDays <= 0 {
		// Misconfigured to 0/negative: treat as disabled rather than deleting
		// every row.
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(cfg.SystemEventRetentionDays) * 24 * time.Hour)

	result := db.Exec("DELETE FROM system_events WHERE occurred_at < ?", cutoff)
	if result.Error != nil {
		logger.Ctx(ctx).Error().Err(result.Error).
			Str(logger.FieldEvent, "system_event_purge_failed").
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Msg("system event purge: failed to delete expired rows")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Ctx(ctx).Info().
			Int64("rows", result.RowsAffected).
			Time("cutoff", cutoff).
			Str(logger.FieldEvent, "system_event_purge_completed").
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Msg("Purged expired system events")
	}
	return nil
}

// PurgeExpiredSystemEventsScheduled is the scheduled cron entry point; it
// acquires a job lock so concurrent runs (multi-instance, rapid restarts)
// don't double-purge. The purge's outcome is passed to releaseJobLock so a
// failure is recorded as `failed` and leaves last_run_at stale for
// job_stopped (issue #975).
func PurgeExpiredSystemEventsScheduled(db *gorm.DB, cfg config.Config) (err error) {
	ctx := logger.JobContext(models.JobNameSystemEventPurge)

	acquired, lockErr := acquireJobLock(db, models.JobNameSystemEventPurge, systemEventPurgeMinInterval)
	if lockErr != nil { // # pragma: no cover — acquireJobLock never returns an error (job_lock.go returns err == nil, nil)
		logger.Ctx(ctx).Error().Err(lockErr).
			Str(logger.FieldEvent, "job_failed").
			Str(logger.FieldOperation, models.JobNameSystemEventPurge).
			Msg("system event purge: failed to check job lock") // # pragma: no cover — see the comment above
		return lockErr // # pragma: no cover — see the comment above
	}
	if !acquired {
		return nil
	}
	defer func() {
		if relErr := releaseJobLock(db, models.JobNameSystemEventPurge, err == nil); relErr != nil {
			logger.Ctx(ctx).Error().Err(relErr).
				Str(logger.FieldOperation, models.JobNameSystemEventPurge).
				Msg("system event purge: failed to release job lock")
			err = errors.Join(err, relErr)
		}
	}()

	return PurgeExpiredSystemEvents(ctx, db, cfg)
}
