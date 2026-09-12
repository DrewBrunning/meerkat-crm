package services

import (
	"errors"
	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"time"

	"gorm.io/gorm"
)

// auditPurgeMinInterval is slightly less than the 24h cron cadence so a
// natural clock-skew or overlap doesn't cause a skipped run (mirrors the T26
// purge job's own constant).
var auditPurgeMinInterval = JobCatchupWindow(24 * time.Hour)

// PurgeExpiredAuditEvents hard-deletes audit events older than the retention
// window (AUDIT_RETENTION_DAYS, default 90 — the ticket's locked decision:
// long enough for the debugging/undo use case, short enough to control SQLite
// file growth on a single-file database). This is the ONLY sanctioned delete
// path for the append-only audit table: the model has no delete receiver
// method, and the 000016 UPDATE trigger blocks every other mutation.
//
// The delete breaks the tamper-evident hash chain at its head (issue #381), so
// after any deletion the survivors are re-linked via models.RecomputeAuditChain.
//
// It returns the delete error, or a failure to re-link the chain after a
// successful delete, so the scheduled caller records a failing run as `failed`
// instead of advancing last_run_at as success (issue #975).
func PurgeExpiredAuditEvents(db *gorm.DB, cfg config.Config) error {
	if cfg.AuditRetentionDays <= 0 {
		// Misconfigured to 0/negative: treat as disabled rather than
		// deleting every audit row.
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(cfg.AuditRetentionDays) * 24 * time.Hour)

	result := db.Exec("DELETE FROM audit_events WHERE created_at < ?", cutoff)
	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("audit purge: failed to delete expired audit events")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info().Int64("rows", result.RowsAffected).Time("cutoff", cutoff).Msg("Purged expired audit events")
		if err := models.RecomputeAuditChain(db); err != nil {
			logger.Error().Err(err).Msg("audit purge: failed to re-link the audit hash chain after purge")
			return err
		}
	}
	return nil
}

// PurgeExpiredReachOutSuggestions hard-deletes reach-out suggestions older
// than the audit retention window (issue #978). A suggestion is derived from
// an AuditEvent before/after diff, and `pii-inventory.md` ties its lifecycle
// to AUDIT_RETENTION_DAYS ("derived from the audit trail, ages with it") — so
// it is purged on that same window rather than a separate knob. Before this,
// a dismissed suggestion merely had its status flipped and a pending one was
// removed only alongside its contact, so both survived forever and carried
// `old_value`/`new_value` PII into every backup. Both statuses are covered:
// the filter is pure age.
//
// Non-positive AUDIT_RETENTION_DAYS disables the purge entirely, matching
// PurgeExpiredAuditEvents — "disabled" must never mean "delete everything".
//
// It returns the delete error (nil when disabled or successful) so the
// scheduled caller records a failing run as `failed` instead of advancing
// last_run_at as success (issue #975).
func PurgeExpiredReachOutSuggestions(db *gorm.DB, cfg config.Config) error {
	if cfg.AuditRetentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(cfg.AuditRetentionDays) * 24 * time.Hour)

	result := db.Where("created_at < ?", cutoff).Delete(&models.ReachOutSuggestion{})
	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("reach-out purge: failed to delete expired reach-out suggestions")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info().Int64("rows", result.RowsAffected).Time("cutoff", cutoff).Msg("Purged expired reach-out suggestions")
	}
	return nil
}

// PurgeExpiredAuditEventsScheduled is the scheduled cron entry point; it
// acquires a job lock so concurrent runs (multi-instance, rapid restarts)
// don't double-purge. It purges the audit trail and the reach-out suggestions
// derived from it together, under one lock, so the two can never diverge
// (issue #978): a suggestion cannot outlive the audit window its retention is
// documented against. The outcome is passed to releaseJobLock so a failure is
// recorded as `failed` and leaves last_run_at stale for job_stopped
// (issue #975).
func PurgeExpiredAuditEventsScheduled(db *gorm.DB, cfg config.Config) (err error) {
	acquired, lockErr := acquireJobLock(db, models.JobNameAuditPurge, auditPurgeMinInterval)
	if lockErr != nil { // # pragma: no cover — acquireJobLock never returns an error (job_lock.go returns err == nil, nil)
		logger.Error().Err(lockErr).Msg("audit purge: failed to check job lock") // # pragma: no cover — see the comment above
		return lockErr                                                           // # pragma: no cover — see the comment above
	}
	if !acquired {
		return nil
	}
	defer func() {
		if relErr := releaseJobLock(db, models.JobNameAuditPurge, err == nil); relErr != nil {
			logger.Error().Err(relErr).Msg("audit purge: failed to release job lock")
			err = errors.Join(err, relErr)
		}
	}()

	return errors.Join(
		PurgeExpiredAuditEvents(db, cfg),
		PurgeExpiredReachOutSuggestions(db, cfg),
	)
}
