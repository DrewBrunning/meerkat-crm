package models

import (
	"context"
	"time"

	"mycorrhizal/logger"

	"gorm.io/gorm"
)

// JobRun is one persisted background-job execution outcome (issue #391) — the
// per-run history that job_executions (lock + last-run only) never kept and
// that system_events (issue #424) deliberately omits to keep its timeline
// lean. An admin consults it to tell "this job is red right now" apart from
// "this job has been getting slower for a week".
//
// System-generated, hard-delete (no DeletedAt): rows are removed only by
// PurgeExpiredJobRuns (JOB_RUN_RETENTION_DAYS), mirroring SystemEvent's
// lifecycle — CLAUDE.md backend trap 7.
//
// Every field carries an explicit gorm:"column:..." tag: GORM's name
// derivation disagrees with hand-written migration SQL for acronyms/IDs and
// AutoMigrate-based tests cannot see it (CLAUDE.md backend trap 1). The
// result / trigger vocabularies are mirrored in migration 000041's CHECK
// constraint, frontend/src/api/jobRuns.ts, backend/openapi.yaml, and the
// Android core/model mirror — keep them in sync (frontend trap 4).
type JobRun struct {
	ID        uint      `gorm:"column:id;primarykey" json:"id"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`

	// JobName is the canonical JobName* token (see job_execution.go), not the
	// scheduler's descriptive label — so history groups cleanly per job
	// regardless of which trigger fired it.
	JobName string `gorm:"column:job_name;not null;index" json:"job_name"`

	// Trigger is scheduled | initial | manual — the cron tick, the
	// boot-time catch-up run, or an operator-forced run via /admin/trigger-*.
	Trigger string `gorm:"column:trigger;not null;default:'scheduled'" json:"trigger"`

	StartedAt  time.Time `gorm:"column:started_at;not null;index" json:"started_at"`
	FinishedAt time.Time `gorm:"column:finished_at;not null" json:"finished_at"`
	DurationMS int64     `gorm:"column:duration_ms;not null" json:"duration_ms"`

	// Result is success | failure | skipped. "skipped" means the work did not
	// run (job lock held by another instance / ran too recently) — recorded,
	// not silent (#526).
	Result string `gorm:"column:result;not null" json:"result"`

	// Error is a sanitized, length-capped error string on the failure path,
	// empty otherwise.
	Error string `gorm:"column:error;not null;default:''" json:"error,omitempty"`

	// ItemsProcessed is the count of items the run acted on (reminders sent,
	// suggestions created, rows purged) when the job reports one; nil when it
	// does not.
	ItemsProcessed *int `gorm:"column:items_processed" json:"items_processed,omitempty"`

	// Detail is a bounded, low-cardinality string for extra context. Never a
	// contact ID or a raw URL.
	Detail string `gorm:"column:detail;not null;default:''" json:"detail,omitempty"`

	// CorrelationID ties this run to its chain of work in system_events —
	// "job:<name>:<uuid>". RecordJobRun fills it from the context when the
	// caller leaves it empty.
	CorrelationID string `gorm:"column:correlation_id;not null;default:'';index" json:"correlation_id"`
}

// JobRun result tokens. Mirrored by migration 000041's CHECK constraint,
// frontend/src/api/jobRuns.ts, backend/openapi.yaml, and the Android mirror.
// These are the logger.Result* values under a JobRun-local name so mirrors
// have one obvious list to track.
const (
	JobRunResultSuccess = logger.ResultSuccess
	JobRunResultFailure = logger.ResultFailure
	JobRunResultSkipped = logger.ResultSkipped
)

// JobRunResults is the full result vocabulary, for validation and tests.
var JobRunResults = []string{JobRunResultSuccess, JobRunResultFailure, JobRunResultSkipped}

// JobRun trigger tokens. Mirrored by migration 000041's CHECK constraint.
const (
	JobTriggerScheduled = "scheduled"
	JobTriggerInitial   = "initial"
	JobTriggerManual    = "manual"
)

// JobRunTriggers is the full trigger vocabulary, for validation and tests.
var JobRunTriggers = []string{JobTriggerScheduled, JobTriggerInitial, JobTriggerManual}

// KnownJobNames is every background job that produces JobRun rows: first the
// scheduled jobs in the order the scheduler registers them (main.go), then the
// operator-triggered jobs that have no schedule. ComputeJobRunHealth iterates
// this set so the health API returns one entry per job in a deterministic
// order — including jobs that have not run yet (status "unknown"). Keep in
// sync with the scheduler registrations and the JobName* consts.
var KnownJobNames = []string{
	JobNameDailyReminders,
	JobNameWebhookRetries,
	JobNameCalendarSync,
	JobNamePurgeDeleted,
	JobNameAuditPurge,
	JobNameSystemEventPurge,
	JobNameWebhookDeliveryPurge,
	JobNameIdempotencyKeyPurge,
	JobNameSessionPurge,
	JobNameCadenceOverdue,
	JobNameReachOutDetection,
	JobNameImmichSync,
	JobNameDBIntegrityCheck,
	JobNameRestoreDrill,
	JobNameAlertEval,
	JobNameJobRunPurge,
	JobNameStorageSample,
	// Operator-triggered, not scheduled (SEARCH-01, issue #461).
	JobNameSearchIndexRebuild,
	// Operator-triggered, not scheduled (issue #497).
	JobNameDerivedColumnsRebuild,
}

// maxJobRunFieldLen caps the persisted Error / Detail strings.
const maxJobRunFieldLen = 1024

// RecordJobRun persists one job-execution outcome, best-effort: it fills
// sensible defaults, sanitizes the free-text fields, and never returns an
// error or blocks the caller — a diagnostic write must not be able to fail a
// job. A failed insert is logged and dropped. Mirrors RecordSystemEvent.
func RecordJobRun(ctx context.Context, db *gorm.DB, r JobRun) {
	if db == nil {
		return
	}

	now := time.Now().UTC()
	if r.FinishedAt.IsZero() {
		r.FinishedAt = now
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = r.FinishedAt
	}
	if r.DurationMS == 0 {
		r.DurationMS = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
	}
	if r.Trigger == "" {
		r.Trigger = JobTriggerScheduled
	}
	if r.Result == "" {
		r.Result = logger.ResultSuccess
	}
	if r.CorrelationID == "" {
		r.CorrelationID = logger.CorrelationID(ctx)
	}
	r.Error = truncateRunes(logger.SanitizeLogField(r.Error), maxJobRunFieldLen)
	r.Detail = truncateRunes(logger.SanitizeLogField(r.Detail), maxJobRunFieldLen)
	r.CreatedAt = now

	if err := db.WithContext(ctx).Create(&r).Error; err != nil {
		logger.Ctx(ctx).Error().
			Err(err).
			Str(logger.FieldEvent, "job_run_write_failed").
			Str("job_name", r.JobName).
			Msg("failed to persist job run")
	}
}
