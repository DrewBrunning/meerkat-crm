package services

import (
	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"time"

	"gorm.io/gorm"
)

// PurgeExpiredContactShares hard-deletes ContactShare rows past the retention
// window (CONTACT_SHARE_RETENTION_DAYS, default 30 — the issue #574 decision:
// a uniform window for all statuses, mirroring T26's DELETE_RETENTION_DAYS).
// ContactShare is a frozen, independent PII copy of a contact (a once-off
// filtered JSContact snapshot, `models/contact_share.go`); it has no soft
// delete and no FK back to the original Contact, so without this job the
// *only* thing that ever removed a row was DeleteUser's cascade and every
// pending/declined/accepted share sat in the DB — and in every backup —
// forever. This is the issue #414 gap that #574 closed.
//
// The window anchors on the moment the share stopped being actionable:
//
//   - pending: created_at — the recipient gets CONTACT_SHARE_RETENTION_DAYS to
//     action the invite; an abandoned invite cannot materialize years later.
//   - accepted/declined: responded_at — the recipient has already imported
//     (accepted) or rejected (declined) it; the snapshot's purpose is done and
//     the real imported contact, if any, lives on in the recipient's account.
//
// Deleting the snapshot never destroys anyone's actual data: the sender's
// original Contact is untouched (no FK, by design), and an accepted share's
// payload was already imported into the recipient's account. The recipient
// declining does not touch the sender's contact either.
//
// Called from PurgeDeletedRows (T26's daily cron, under the same job lock)
// and from the admin trigger-purge endpoint — same cadence as every other
// hard-delete of user content. CONTACT_SHARE_RETENTION_DAYS <= 0 disables the
// purge rather than deleting every share, matching audit_purge_service.go's
// stance.
//
// It returns the delete error (nil when disabled or successful) so the
// scheduling caller can record a failed run rather than advancing last_run_at
// as success (issue #975).
func PurgeExpiredContactShares(db *gorm.DB, cfg config.Config) error {
	if cfg.ContactShareRetentionDays <= 0 {
		// Misconfigured to 0/negative: treat as disabled rather than
		// deleting every share.
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.ContactShareRetentionDays)

	result := db.Exec(
		`DELETE FROM contact_shares WHERE
			(status = ? AND created_at < ?)
			OR (status IN (?, ?) AND responded_at IS NOT NULL AND responded_at < ?)`,
		models.ContactShareStatusPending, cutoff,
		models.ContactShareStatusAccepted, models.ContactShareStatusDeclined, cutoff,
	)
	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("contact share purge: failed to delete expired shares")
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Info().
			Int64("rows", result.RowsAffected).
			Time("cutoff", cutoff).
			Msg("Purged expired contact shares")
	}
	return nil
}
