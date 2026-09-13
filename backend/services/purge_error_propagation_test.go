package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Issue #975: before this fix every scheduled purge called
// releaseJobLock(..., true) unconditionally. The work functions returned
// nothing, so a purge that failed on every run (SQLITE_BUSY, full disk, a
// schema mismatch) logged an error, advanced last_run_at, and wrote
// job_runs.result=success — job_stopped could never fire while the database
// grew to fill the disk. These tests pin the outcome now that the purge
// functions return their error and the scheduled entry points pass
// `err == nil` to releaseJobLock.
//
// The failure seam is a Raw callback that fails every `db.Exec`, which is what
// every purge's DELETE goes through. Closing the DB is not usable: that also
// makes acquireJobLock's transaction fail, and acquireJobLock silently
// normalizes a DB failure to (acquired=false, err=nil), so the purge would
// never run and the test would prove nothing.

// failPurgeRawExec makes every subsequent raw Exec on db fail, simulating a
// storage error under the purge's DELETE without disturbing the job lock's own
// query/create/update.
func failPurgeRawExec(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Raw().Before("gorm:raw").
		Register("purge_test_fail_raw", func(tx *gorm.DB) {
			tx.AddError(errors.New("simulated purge failure"))
		}))
}

// abortDeletesOn installs a BEFORE DELETE trigger that aborts every delete on
// table, so a purge's GORM-model delete (which does not go through the Raw
// callback) also fails. Used to reach PurgeSoftDeletedRows' per-model and
// contacts error branches.
func abortDeletesOn(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	require.NoError(t, db.Exec(fmt.Sprintf(
		"CREATE TRIGGER abort_delete_%s BEFORE DELETE ON %s BEGIN SELECT RAISE(ABORT, 'simulated purge failure'); END",
		table, table)).Error)
}

// seedStaleJobLock inserts a JobExecution for jobName whose last_run_at is well
// outside any de-dup window, so the next scheduled run actually acquires the
// lock (instead of being suppressed as `deduped`) and its release can be
// observed. It returns the stale instant for comparison.
func seedStaleJobLock(t *testing.T, db *gorm.DB, jobName string) time.Time {
	t.Helper()
	stale := time.Now().Add(-100 * time.Hour).UTC()
	require.NoError(t, db.Create(&models.JobExecution{
		JobName:     jobName,
		LastRunAt:   stale,
		LastOutcome: models.JobOutcomeRan,
	}).Error)
	return stale
}

// scheduledPurgeCases enumerates every scheduled purge entry point, so a new
// one is forced into this table when it is added.
func scheduledPurgeCases() []struct {
	name    string
	jobName string
	run     func(db *gorm.DB) error
} {
	return []struct {
		name    string
		jobName string
		run     func(db *gorm.DB) error
	}{
		{"purge_deleted", models.JobNamePurgeDeleted, func(db *gorm.DB) error {
			return PurgeDeletedRows(db, config.Config{DeleteRetentionDays: 30, ContactShareRetentionDays: 30})
		}},
		{"audit_purge", models.JobNameAuditPurge, func(db *gorm.DB) error {
			return PurgeExpiredAuditEventsScheduled(db, config.Config{AuditRetentionDays: 30})
		}},
		{"system_event_purge", models.JobNameSystemEventPurge, func(db *gorm.DB) error {
			return PurgeExpiredSystemEventsScheduled(db, config.Config{SystemEventRetentionDays: 30})
		}},
		{"job_run_purge", models.JobNameJobRunPurge, func(db *gorm.DB) error {
			return PurgeExpiredJobRunsScheduled(db, config.Config{JobRunRetentionDays: 30})
		}},
		{"webhook_delivery_purge", models.JobNameWebhookDeliveryPurge, func(db *gorm.DB) error {
			return PurgeExpiredWebhookDeliveriesScheduled(db, config.Config{WebhookDeliveryRetentionDays: 30})
		}},
		{"idempotency_key_purge", models.JobNameIdempotencyKeyPurge, func(db *gorm.DB) error {
			return PurgeExpiredIdempotencyKeysScheduled(db, config.Config{IdempotencyKeyRetentionHours: 24})
		}},
		{"session_purge", models.JobNameSessionPurge, func(db *gorm.DB) error {
			return PurgeExpiredSessionsScheduled(db)
		}},
	}
}

func TestScheduledPurges_FailureIsPropagatedToJobLock(t *testing.T) {
	for _, tc := range scheduledPurgeCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := dbtest.New(t)
			stale := seedStaleJobLock(t, db, tc.jobName)
			failPurgeRawExec(t, db)

			runErr := tc.run(db)
			require.Error(t, runErr, "a failing purge must return its error")

			var job models.JobExecution
			require.NoError(t, db.Where("job_name = ?", tc.jobName).First(&job).Error)
			assert.Equal(t, models.JobOutcomeFailed, job.LastOutcome,
				"a failing purge must be recorded `failed`, not `ran`")
			assert.Equal(t, stale.Unix(), job.LastRunAt.Unix(),
				"a failed release must not advance last_run_at — that is what lets job_stopped fire")
			assert.Nil(t, job.LockedAt, "the lock must still be released")
		})
	}
}

func TestScheduledPurges_SuccessKeepsCaughtUpAndAdvancesLastRunAt(t *testing.T) {
	for _, tc := range scheduledPurgeCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := dbtest.New(t)
			stale := seedStaleJobLock(t, db, tc.jobName)

			require.NoError(t, tc.run(db), "a healthy purge must report success")

			var job models.JobExecution
			require.NoError(t, db.Where("job_name = ?", tc.jobName).First(&job).Error)
			assert.Equal(t, models.JobOutcomeCaughtUp, job.LastOutcome,
				"a caught-up marker must be preserved on a successful release")
			assert.Greater(t, job.LastRunAt.Unix(), stale.Unix(),
				"a successful release must advance last_run_at")
			assert.Nil(t, job.LockedAt, "the lock must be released")
		})
	}
}

// TestFailedPurgeIsDetectedByJobStopped is the end-to-end assertion behind the
// issue: a purge that fails must leave the state job_stopped keys on — a
// stale last_run_at — so the condition fires instead of the failure being
// invisible.
func TestFailedPurgeIsDetectedByJobStopped(t *testing.T) {
	db := dbtest.New(t)
	seedStaleJobLock(t, db, models.JobNamePurgeDeleted)
	failPurgeRawExec(t, db)

	require.Error(t, PurgeDeletedRows(db,
		config.Config{DeleteRetentionDays: 30, ContactShareRetentionDays: 30}))

	res := jobStoppedCondition(context.Background(), db, config.Config{AlertJobStaleMultiplier: 3})
	require.True(t, res.firing, "a failing purge must leave job_stopped firing")
	assert.Contains(t, res.detail, models.JobNamePurgeDeleted)
}

// TestScheduledPurges_ReleaseFailureIsSurfaced pins the deferred
// releaseJobLock-error branch for every scheduled purge: a purge whose work
// succeeded but whose lock release failed must still report failure, or the
// JobExecution row stays locked and last_run_at stale silently. The lock's
// release is an UPDATE, so a failing update callback is the one seam that
// affects only the release (acquire inserts a brand-new lock row).
func TestScheduledPurges_ReleaseFailureIsSurfaced(t *testing.T) {
	for _, tc := range scheduledPurgeCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := dbtest.New(t)
			require.NoError(t, db.Callback().Update().Before("gorm:update").
				Register(tc.name+"_fail_release", func(tx *gorm.DB) {
					tx.AddError(errors.New("simulated release failure"))
				}))

			err := tc.run(db)
			require.Error(t, err, "a failed lock release must be surfaced, not swallowed")
			require.Contains(t, err.Error(), "simulated release failure")
		})
	}
}

// TestPurgeExpiredReachOutSuggestions_DeleteFailureIsReturned pins the
// reach-out half of the audit purge (issue #978) against issue #975: a failed
// delete must be returned, not swallowed, so the scheduled audit purge records
// a failure.
func TestPurgeExpiredReachOutSuggestions_DeleteFailureIsReturned(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "reachout-fail", Email: "reachout-fail@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.ReachOutSuggestion{
		UserID: user.ID, ContactVCardUID: "11111111-1111-4111-8111-111111111111",
		Kind: models.ReachOutKindTitle, OldValue: "Engineer", NewValue: "Manager",
		AuditEventID: 1, Status: models.ReachOutStatusPending,
		CreatedAt: time.Now().AddDate(0, 0, -100), UpdatedAt: time.Now(),
	}).Error)
	abortDeletesOn(t, db, "reach_out_suggestions")

	require.Error(t, PurgeExpiredReachOutSuggestions(db, config.Config{AuditRetentionDays: 90}),
		"a failed reach-out delete must be returned, not swallowed")
}

// TestPurgeSoftDeletedRows_ModelDeleteFailuresAreJoined covers the
// GORM-model delete branches of PurgeSoftDeletedRows (soft-deleted child
// content and the contacts themselves), which the Raw-callback seam cannot
// reach: it must join them and still attempt every table.
func TestPurgeSoftDeletedRows_ModelDeleteFailuresAreJoined(t *testing.T) {
	db, userID := newPurgeDB(t)

	contact := models.Contact{UserID: userID, Firstname: "Doomed"}
	require.NoError(t, db.Create(&contact).Error)
	softDeleteAt(t, db, &models.Contact{}, contact.ID, time.Now().AddDate(0, 0, -60))

	note := models.Note{UserID: userID, ContactID: &contact.ID, Content: "old note"}
	require.NoError(t, db.Create(&note).Error)
	softDeleteAt(t, db, &models.Note{}, note.ID, time.Now().AddDate(0, 0, -60))

	abortDeletesOn(t, db, "notes")
	abortDeletesOn(t, db, "contacts")

	err := PurgeSoftDeletedRows(db, purgeConfig())
	require.Error(t, err, "a failed model delete must be reported")
	assert.Contains(t, err.Error(), "purge soft-deleted *models.Note")
	assert.Contains(t, err.Error(), "purge soft-deleted contacts")
}
