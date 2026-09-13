package services

import (
	"errors"
	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"time"

	"gorm.io/gorm"
)

// webhookDeliveryPurgeMinInterval is slightly less than the 24h cron cadence
// so a natural clock-skew or overlap doesn't cause a skipped run (mirrors the
// audit purge job's own constant).
const webhookDeliveryPurgeMinInterval = 23 * time.Hour

// PurgeExpiredWebhookDeliveries hard-deletes webhook_deliveries rows older
// than the retention window (WEBHOOK_DELIVERY_RETENTION_DAYS, default 30).
//
// This is the issue #622 decision, surfaced by the #510 privacy/data-
// minimization review. Every row carries a copy of the serialized entity that
// triggered the event — a `contact.created` delivery holds the whole contact
// record — so without this job a plaintext copy of every entity that ever
// changed accumulated in the table forever (the only prior deletes were
// cascade-on-parent: account deletion and the webhook row's FK). The window
// bounds that copy, and successful deliveries now also trim it (see
// trimSuccessfulDeliveryPayload in webhook_service.go).
//
// The window anchors on created_at — the moment of the delivery attempt. A
// row older than the window is past every retry a webhook could ever have
// taken (max 3 attempts across a ~20-minute backoff window), so purging a
// still-pending-retry row is not a lost delivery, it is the retention decision
// being allowed to be the last word; re-sending a month-old webhook is the
// same stale-PII hazard the window exists to bound.
//
// WEBHOOK_DELIVERY_RETENTION_DAYS <= 0 disables the purge rather than deleting
// every delivery, matching audit_purge_service.go's stance on a misconfigured
// window.
//
// It returns the delete error (nil when disabled or successful) so the
// scheduled caller records a failing purge as `failed` instead of advancing
// last_run_at as success (issue #975).
func PurgeExpiredWebhookDeliveries(db *gorm.DB, cfg config.Config) error {
	if cfg.WebhookDeliveryRetentionDays <= 0 {
		// Misconfigured to 0/negative: treat as disabled rather than
		// deleting every delivery.
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(cfg.WebhookDeliveryRetentionDays) * 24 * time.Hour)

	result := db.Exec("DELETE FROM webhook_deliveries WHERE created_at < ?", cutoff)
	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("webhook delivery purge: failed to delete expired deliveries")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info().Int64("rows", result.RowsAffected).Time("cutoff", cutoff).Msg("Purged expired webhook deliveries")
	}
	return nil
}

// PurgeExpiredWebhookDeliveriesScheduled is the scheduled cron entry point; it
// acquires a job lock so concurrent runs (multi-instance, rapid restarts)
// don't double-purge. The purge's outcome is passed to releaseJobLock so a
// failure is recorded as `failed` and leaves last_run_at stale for
// job_stopped (issue #975).
func PurgeExpiredWebhookDeliveriesScheduled(db *gorm.DB, cfg config.Config) (err error) {
	acquired, lockErr := acquireJobLock(db, models.JobNameWebhookDeliveryPurge, webhookDeliveryPurgeMinInterval)
	if lockErr != nil { // # pragma: no cover — acquireJobLock never returns an error (job_lock.go returns err == nil, nil)
		logger.Error().Err(lockErr).Msg("webhook delivery purge: failed to check job lock") // # pragma: no cover — see the comment above
		return lockErr                                                                      // # pragma: no cover — see the comment above
	}
	if !acquired {
		return nil
	}
	defer func() {
		if relErr := releaseJobLock(db, models.JobNameWebhookDeliveryPurge, err == nil); relErr != nil {
			logger.Error().Err(relErr).Msg("webhook delivery purge: failed to release job lock")
			err = errors.Join(err, relErr)
		}
	}()

	return PurgeExpiredWebhookDeliveries(db, cfg)
}
