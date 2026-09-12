package services

import (
	"context"
	"errors"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"gorm.io/gorm"
)

// jobRunPurgeMinInterval is slightly less than the 24h cron cadence so a
// natural clock-skew or overlap doesn't cause a skipped run (mirrors the
// system-event / audit purge jobs' own constant).
var jobRunPurgeMinInterval = JobCatchupWindow(24 * time.Hour)

// PurgeExpiredJobRuns hard-deletes job_runs rows older than the retention
// window (JOB_RUN_RETENTION_DAYS, default 30 — operational diagnostics,
// short-lived by design; long enough to spot a slow-creep trend, short enough
// to bound growth on a single-file SQLite database). This is the only delete
// path for the table.
//
// It returns the delete error (nil when disabled or successful) so the
// scheduled caller records a failing purge as `failed` instead of advancing
// last_run_at as success (issue #975).
func PurgeExpiredJobRuns(ctx context.Context, db *gorm.DB, cfg config.Config) error {
	if cfg.JobRunRetentionDays <= 0 {
		// Misconfigured to 0/negative: treat as disabled rather than deleting
		// every row.
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(cfg.JobRunRetentionDays) * 24 * time.Hour)

	result := db.Exec("DELETE FROM job_runs WHERE started_at < ?", cutoff)
	if result.Error != nil {
		logger.Ctx(ctx).Error().Err(result.Error).
			Str(logger.FieldEvent, "job_run_purge_failed").
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Msg("job run purge: failed to delete expired rows")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Ctx(ctx).Info().
			Int64("rows", result.RowsAffected).
			Time("cutoff", cutoff).
			Str(logger.FieldEvent, "job_run_purge_completed").
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Msg("Purged expired job runs")
	}
	return nil
}

// PurgeExpiredJobRunsScheduled is the scheduled cron entry point; it acquires
// a job lock so concurrent runs (multi-instance, rapid restarts) don't
// double-purge. The purge's outcome is passed to releaseJobLock so a failure
// is recorded as `failed` and leaves last_run_at stale for job_stopped
// (issue #975).
func PurgeExpiredJobRunsScheduled(db *gorm.DB, cfg config.Config) (err error) {
	ctx := logger.JobContext(models.JobNameJobRunPurge)

	acquired, lockErr := acquireJobLock(db, models.JobNameJobRunPurge, jobRunPurgeMinInterval)
	if lockErr != nil { // # pragma: no cover — acquireJobLock never returns an error (job_lock.go returns err == nil, nil)
		logger.Ctx(ctx).Error().Err(lockErr).
			Str(logger.FieldEvent, "job_failed").
			Str(logger.FieldOperation, models.JobNameJobRunPurge).
			Msg("job run purge: failed to check job lock") // # pragma: no cover — see the comment above
		return lockErr // # pragma: no cover — see the comment above
	}
	if !acquired {
		return nil
	}
	defer func() {
		if relErr := releaseJobLock(db, models.JobNameJobRunPurge, err == nil); relErr != nil {
			logger.Ctx(ctx).Error().Err(relErr).
				Str(logger.FieldOperation, models.JobNameJobRunPurge).
				Msg("job run purge: failed to release job lock")
			err = errors.Join(err, relErr)
		}
	}()

	return PurgeExpiredJobRuns(ctx, db, cfg)
}
