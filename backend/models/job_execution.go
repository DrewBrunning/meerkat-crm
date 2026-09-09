package models

import (
	"time"

	"gorm.io/gorm"
)

// JobExecution tracks the execution of scheduled jobs to prevent duplicates
// during rapid restarts or crash loops (the distributed job lock), and records
// the last run's outcome so a caught-up run can be told from a suppressed one
// (issue #526, ADR 0011).
type JobExecution struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	JobName   string    `gorm:"uniqueIndex;not null" json:"job_name"`
	LastRunAt time.Time `gorm:"not null" json:"last_run_at"`
	// LastOutcome is the outcome of the most recent acquire/release cycle
	// (issue #526 rule 4): "ran" (normal), "deduped" (suppressed inside the
	// de-dup window), "caught_up" (ran after the process was down long enough
	// to miss an occurrence), "failed", or transiently "running" while a run
	// holds the lock. Empty on a row that predates migration 000048.
	LastOutcome string         `gorm:"column:last_outcome;not null;default:''" json:"last_outcome"`
	LockedAt    *time.Time     `json:"locked_at"`
	LockedBy    string         `json:"locked_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at"`
}

// JobExecution.LastOutcome values (issue #526, ADR 0011).
const (
	JobOutcomeRan      = "ran"
	JobOutcomeDeduped  = "deduped"
	JobOutcomeCaughtUp = "caught_up"
	JobOutcomeFailed   = "failed"
	JobOutcomeRunning  = "running"
)

const (
	// JobNameDailyReminders is the job name for the daily reminder email job
	JobNameDailyReminders = "daily_reminders"

	// JobNameCalendarSync is the job name for the scheduled calendar sync job
	JobNameCalendarSync = "calendar_sync"

	// JobNameWebhookRetries is the job name for the scheduled webhook retry job
	JobNameWebhookRetries = "webhook_retries"

	// JobNamePurgeDeleted is the job name for the periodic hard-deletion of
	// soft-deleted rows past their retention window (T26).
	JobNamePurgeDeleted = "purge_deleted"

	// JobNameCadenceOverdue is the job name for the daily overdue-cadence
	// webhook emission job (T19).
	JobNameCadenceOverdue = "cadence_overdue"

	// JobNameImmichSync is the job name for the scheduled Immich enrichment
	// sync job (T16).
	JobNameImmichSync = "immich_sync"

	// JobNameAuditPurge is the job name for the periodic removal of expired
	// audit events past their retention window (T18).
	JobNameAuditPurge = "audit_purge"

	// JobNameReachOutDetection is the job name for the daily event-driven
	// reach-out-suggestion detection job (issue #177).
	JobNameReachOutDetection = "reach_out_detection"

	// JobNameDBIntegrityCheck is the job name for the scheduled live-DB
	// PRAGMA integrity_check job (issue #273).
	JobNameDBIntegrityCheck = "db_integrity_check"

	// JobNameRestoreDrill is the job name for the scheduled backup-restore
	// drill job (issue #275).
	JobNameRestoreDrill = "restore_drill"

	// JobNameSystemEventPurge is the job name for the periodic removal of
	// system_events rows past their retention window (issue #424).
	JobNameSystemEventPurge = "system_event_purge"

	// JobNameWebhookDeliveryPurge is the job name for the periodic removal of
	// webhook_deliveries rows past their retention window (issue #622).
	JobNameWebhookDeliveryPurge = "webhook_delivery_purge"

	// JobNameIdempotencyKeyPurge is the job name for the periodic removal of
	// idempotency_keys rows past their (short) TTL window (issue #459, CON-04).
	JobNameIdempotencyKeyPurge = "idempotency_key_purge"

	// JobNameSessionPurge is the job name for the periodic removal of sessions
	// rows past their absolute expiry (plus a grace window) and long-revoked
	// rows (issue #866).
	JobNameSessionPurge = "session_purge"

	// JobNameAlertEval is the job name for the scheduled alert evaluator that
	// detects failure/recovery transitions on the tracked subsystems and
	// notifies on them (issue #428).
	JobNameAlertEval = "alert_eval"

	// JobNameJobRunPurge is the job name for the periodic removal of job_runs
	// rows past their retention window (issue #391).
	JobNameJobRunPurge = "job_run_purge"

	// JobNameStorageSample is the job name for the daily storage-growth
	// sampler that writes one storage_samples row and prunes rows past their
	// retention window (issue #652).
	JobNameStorageSample = "storage_sample"

	// JobNameSearchIndexRebuild is the job name for the FTS5 full-text search
	// index rebuild (SEARCH-01, issue #461). Unlike every other JobName* this
	// is not a scheduled job — it is operator-triggered (POST
	// /admin/search/rebuild) and auto-run after a restore. It still produces
	// job_runs rows so the rebuild's duration and outcome are visible on the
	// admin job-run timeline (issue #391) rather than only on a terminal.
	JobNameSearchIndexRebuild = "search_index_rebuild"

	// JobNameDerivedColumnsRebuild is the job name for the denormalized
	// contact-column rebuild (issue #497) — the flat contacts.* projection,
	// contacts.sort_name, contacts.addresses_flat and contacts.phones_normalized
	// re-derived from the authoritative nested Card. Like
	// JobNameSearchIndexRebuild it is operator-triggered (POST
	// /admin/contacts/rebuild-derived), not scheduled, and auto-run after a
	// restore; it produces job_runs rows so its duration and outcome show on
	// the admin job-run timeline.
	JobNameDerivedColumnsRebuild = "derived_columns_rebuild"
)
