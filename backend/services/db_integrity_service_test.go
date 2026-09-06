package services

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// corruptDataPage overwrites one whole page of a closed SQLite file with
// garbage bytes, in place, well past the schema pages so it lands on real
// notes-table data rather than the header.
//
// An earlier version of this helper truncated one page's worth of bytes off
// the end instead. That shrinks the file below what page 1's header still
// claims as the total page count, and whether that reads back as graceful
// ("Page N: never used" / a freelist-count mismatch PRAGMA integrity_check
// can report as text) or as a hard, unopenable "database disk image is
// malformed" I/O error depends entirely on whether the now-missing tail
// page happened to be on the freelist (unreferenced -- safe) or still
// referenced by a live b-tree (a real short read past EOF -- fatal). Which
// one you get is a function of the database's exact page count, which is
// sensitive to incidental things like how many connections happened to
// checkpoint the WAL before this file was sealed -- not something a test
// should be pinned to. Overwriting a page's bytes in place instead never
// changes the file's size, so it can never produce a short read: every page
// the header claims still physically exists, and corruption is always the
// gentler "this page's content doesn't parse" case integrity_check is
// built to report as findings, not the fatal "this page doesn't exist"
// case. See PR #579 review.
//
// That is still not quite enough, because which *kind* of page the target
// offset lands on is also a function of the exact page layout: a b-tree
// interior page or the freelist corrupts "fatally" (integrity_check itself
// aborts with SQLITE_CORRUPT) while a leaf data page corrupts gracefully
// (integrity_check reports text findings). Schema changes that shift the
// page layout — e.g. the 000044 revision/etag columns widening every notes
// row — move the boundary. So the helper probes: it corrupts a candidate
// page, runs integrity_check, and on a fatal result restores the original
// bytes and tries the adjacent page, until it lands a graceful one. The
// seeded 500-note file has plenty of leaf pages to find.
func corruptDataPage(t *testing.T, path string) {
	t.Helper()
	const pageSize = 4096
	info, err := os.Stat(path)
	require.NoError(t, err)
	pageCount := info.Size() / pageSize
	require.Greater(t, pageCount, int64(8), "need enough pages for the corruption target to land past the schema pages, on real notes data")

	// The schema pages (sqlite_master, and the small users/webhooks/
	// job_executions tables) sit at the front of the file; bulk notes data
	// dominates everything after that. Start at the middle of the file —
	// comfortably inside that notes region without ever risking page 1 (the
	// file header) or the tables the webhook-delivery test depends on
	// surviving untouched — and walk outward on a fatal probe result.
	mid := pageCount / 2
	if probePageCorruptsGracefully(t, path, mid) {
		return
	}
	for delta := int64(1); delta < pageCount/2; delta++ {
		for _, candidate := range []int64{mid + delta, mid - delta} {
			if candidate < 2 || candidate >= pageCount {
				continue
			}
			if probePageCorruptsGracefully(t, path, candidate) {
				return
			}
		}
	}
	t.Fatal("could not find a data page whose corruption integrity_check reports gracefully")
}

// probePageCorruptsGracefully corrupts page `target` (1-indexed), runs
// integrity_check, and returns true only when the corruption produces text
// findings rather than a fatal I/O error. On a fatal result the page's
// original bytes are restored before returning false.
func probePageCorruptsGracefully(t *testing.T, path string, target int64) bool {
	t.Helper()
	const pageSize = 4096

	readOriginal := func() []byte {
		f, err := os.Open(path)
		require.NoError(t, err)
		defer func() { require.NoError(t, f.Close()) }()
		original := make([]byte, pageSize)
		_, err = f.ReadAt(original, (target-1)*pageSize)
		require.NoError(t, err)
		return original
	}
	writePage := func(data []byte) {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		require.NoError(t, err)
		_, err = f.WriteAt(data, (target-1)*pageSize)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}

	original := readOriginal()
	writePage(bytes.Repeat([]byte{0xFF}, pageSize))

	// A page whose zeroing breaks even *opening* the file (SQLite reads a
	// handful of structural pages at first connect, not just sqlite_master)
	// is a fatal candidate just like one that fails integrity_check — restore
	// it and let the caller try the next page. openRaw's require would abort
	// the whole test on this path, which is wrong: which page sits at a given
	// offset is pure schema layout, and a later migration shifting the
	// mid-file target must not make the test fail before it ever searches.
	raw, openerr := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if openerr != nil {
		writePage(original)
		return false
	}
	_, _, err := checkDBIntegrity(raw)
	if err != nil {
		// Fatal corruption (SQLITE_CORRUPT from integrity_check itself):
		// restore the page and let the caller try the next candidate.
		if sqlDB, cerr := raw.DB(); cerr == nil {
			sqlDB.Close()
		}
		writePage(original)
		return false
	}

	// The scheduled job runs the application-invariant and FTS-consistency
	// passes after the storage pragmas, so a candidate is only usable when it
	// corrupts *storage* without breaking the other passes — a zeroed page
	// inside a table those passes read would fire a second webhook. Skip such
	// candidates so the test keeps exercising exactly the corruption it
	// intends (bulk notes data; see corruptDataPage's doc comment). The
	// storage checks already ran against `raw`, so reuse that handle.
	ctx := context.Background()
	if report, derr := RunDataIntegrityChecks(ctx, raw, config.Config{}); derr != nil || !report.OK {
		if sqlDB, cerr := raw.DB(); cerr == nil {
			sqlDB.Close()
		}
		writePage(original)
		return false
	}
	if fts, ferr := CheckSearchIndexConsistency(raw); ferr != nil || !fts.Clean() {
		if sqlDB, cerr := raw.DB(); cerr == nil {
			sqlDB.Close()
		}
		writePage(original)
		return false
	}
	if sqlDB, cerr := raw.DB(); cerr == nil {
		sqlDB.Close()
	}
	return true
}

// seededLiveDB builds a real migrated database (CLAUDE.md trap #1 — never
// AutoMigrate) with enough bulk data that it spans multiple SQLite pages, so
// corruptDataPage has real data to land on.
func seededLiveDB(t *testing.T, name string) (db *gorm.DB, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), name)
	db = dbtest.NewAt(t, path)

	user := models.User{Username: "integritytester", Password: "password123!A", Email: "integrity@example.com"}
	require.NoError(t, db.Create(&user).Error)

	for i := 0; i < 500; i++ {
		note := models.Note{UserID: user.ID, Content: "bulk note content padding the file with real pages so truncation lands on data", Date: time.Now()}
		require.NoError(t, db.Create(&note).Error)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db, path
}

// closeDB is a t.Cleanup-free explicit close, used when the test needs the
// file closed (and corrupted) partway through, before its own t.Cleanup would
// otherwise fire.
func closeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	// WAL mode means recent writes may still be sitting in the -wal sidecar
	// rather than the main file; truncate-checkpoint first so the bulk data
	// corruptDataPage depends on is actually in the file it corrupts.
	require.NoError(t, db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// openRaw opens a plain (non-migrating) connection to an existing file —
// used for the corrupted-file case, where running database.InitDB's
// migration check against a corrupted schema page would be its own
// (unrelated) failure mode.
func openRaw(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

func TestCheckDBIntegrityHealthyDB(t *testing.T) {
	db, _ := seededLiveDB(t, "healthy.db")

	ok, detail, err := checkDBIntegrity(db)
	require.NoError(t, err)
	assert.True(t, ok, "detail: %s", detail)
	assert.Empty(t, detail)
}

func TestCheckDBIntegrityDetectsCorruption(t *testing.T) {
	db, path := seededLiveDB(t, "corrupt.db")
	closeDB(t, db)

	corruptDataPage(t, path)

	raw := openRaw(t, path)
	ok, detail, err := checkDBIntegrity(raw)
	require.NoError(t, err, "integrity_check itself must not error on a corrupted-but-openable file")
	assert.False(t, ok)
	assert.NotEmpty(t, detail, "must report what integrity_check found")
}

// TestCheckDBIntegrityErrorsOnClosedConnection distinguishes "the
// integrity_check query itself failed" (a real Go error, e.g. a lost
// connection) from "the query ran and found corruption" (TestCheckDBIntegrityDetectsCorruption).
// CheckDBIntegrityScheduled fires a differently-shaped webhook payload for
// each ("error" vs. "detail"), so the two must stay distinguishable.
func TestCheckDBIntegrityErrorsOnClosedConnection(t *testing.T) {
	db, _ := seededLiveDB(t, "integrity-closed.db")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	ok, detail, err := checkDBIntegrity(db)
	require.Error(t, err, "a failed integrity_check query itself (not corruption found) must surface as an error")
	assert.False(t, ok)
	assert.Empty(t, detail)
}

// TestDBIntegrityCheckMinIntervalClampsToMargin pins the same guard
// restoreDrillMinInterval has: a configured interval at or below the margin
// must floor at the margin rather than going zero/negative, which would
// otherwise let the job re-run on every cron tick.
func TestDBIntegrityCheckMinIntervalClampsToMargin(t *testing.T) {
	got := dbIntegrityCheckMinInterval(config.Config{DBIntegrityCheckIntervalHours: 0})
	assert.Equal(t, JobCatchupMargin, got)
}

func TestDBIntegrityCheckMinIntervalAboveMargin(t *testing.T) {
	got := dbIntegrityCheckMinInterval(config.Config{DBIntegrityCheckIntervalHours: 24})
	assert.Equal(t, 24*time.Hour-JobCatchupMargin, got)
}

func TestCheckDBIntegrityScheduledFiresWebhookOnCorruption(t *testing.T) {
	db, path := seededLiveDB(t, "corrupt-scheduled.db")
	user := models.User{}
	require.NoError(t, db.Where("username = ?", "integritytester").First(&user).Error)
	closeDB(t, db)

	corruptDataPage(t, path)
	raw := openRaw(t, path)

	// job_executions and webhooks tables are created early by migrations and
	// were empty at corruption time (all the bulk data went into notes), so
	// they're expected to have survived the corruption intact.
	var hits int32
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	wh := models.Webhook{UserID: user.ID, Name: "alerts", URL: server.URL, Events: []string{EventDBIntegrityCheckFailed}, Secret: "s", IsActive: true}
	require.NoError(t, raw.Create(&wh).Error)

	cfg := config.Config{DBIntegrityCheckEnabled: true, DBIntegrityCheckIntervalHours: 24}
	require.NotPanics(t, func() { CheckDBIntegrityScheduled(raw, cfg) })

	// deliverWebhook runs in its own goroutine (see TriggerWebhooks) and only
	// writes the WebhookDelivery row *after* the HTTP round-trip completes, so
	// polling on `hits` alone (set the instant the request arrives, before the
	// response is even sent) races that write. Poll for the row itself instead.
	var deliveries []models.WebhookDelivery
	require.Eventually(t, func() bool {
		if atomic.LoadInt32(&hits) < 1 {
			return false
		}
		if err := raw.Find(&deliveries).Error; err != nil {
			return false
		}
		return len(deliveries) >= 1
	}, 3*time.Second, 10*time.Millisecond, "corruption must trigger a db.integrity_check_failed webhook and persist a delivery")

	require.Len(t, deliveries, 1)
	assert.Equal(t, EventDBIntegrityCheckFailed, deliveries[0].EventType)

	// The wire body carries the full payload (the receiver still gets the
	// diagnostic detail); the stored receipt is the trimmed envelope (issue
	// #622: a successful delivery never needs the entity body it carried).
	var envelope struct {
		Event string `json:"event"`
		Data  struct {
			Detail string `json:"detail"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(receivedBody, &envelope))
	assert.NotEmpty(t, envelope.Data.Detail, "the delivered HTTP body must still carry the diagnostic detail")

	var storedEnvelope struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(deliveries[0].Payload), &storedEnvelope))
	assert.Equal(t, EventDBIntegrityCheckFailed, storedEnvelope.Event)
	assert.Nil(t, storedEnvelope.Data, "the stored receipt must not retain the entity body")

	var job models.JobExecution
	require.NoError(t, raw.Where("job_name = ?", models.JobNameDBIntegrityCheck).First(&job).Error)
	assert.Nil(t, job.LockedAt, "lock must be released after the job completes")
}

func TestCheckDBIntegrityScheduledSkipsWhenLocked(t *testing.T) {
	db, _ := seededLiveDB(t, "locked.db")

	seededAt := time.Now().Add(-1 * time.Minute)
	require.NoError(t, db.Create(&models.JobExecution{
		JobName: models.JobNameDBIntegrityCheck, LastRunAt: seededAt,
	}).Error)

	cfg := config.Config{DBIntegrityCheckEnabled: true, DBIntegrityCheckIntervalHours: 24}
	require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

	var job models.JobExecution
	require.NoError(t, db.Where("job_name = ?", models.JobNameDBIntegrityCheck).First(&job).Error)
	assert.WithinDuration(t, seededAt, job.LastRunAt, time.Second, "a run within the min interval must be skipped, not re-run")
	assert.Nil(t, job.LockedAt)
}

func TestCheckDBIntegrityScheduledDisabledByConfig(t *testing.T) {
	db, _ := seededLiveDB(t, "disabled.db")

	cfg := config.Config{DBIntegrityCheckEnabled: false, DBIntegrityCheckIntervalHours: 24}
	require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

	var count int64
	require.NoError(t, db.Model(&models.JobExecution{}).Where("job_name = ?", models.JobNameDBIntegrityCheck).Count(&count).Error)
	assert.Zero(t, count, "a disabled job must never touch the job_executions table")
}

// ---------------------------------------------------------------------------
// DB-01 (issue #460) — storage pass gains PRAGMA foreign_key_check, and the
// scheduled job runs the data-invariant pass alongside it, recording distinctly.
// ---------------------------------------------------------------------------

func TestRunStorageIntegrityChecks_HealthyDB(t *testing.T) {
	db, _ := seededLiveDB(t, "storage-healthy.db")

	report, err := RunStorageIntegrityChecks(db)
	require.NoError(t, err)
	assert.True(t, report.OK)
	assert.Equal(t, "ok", report.IntegrityCheck)
	assert.Equal(t, "ok", report.ForeignKeyCheck)
	assert.Empty(t, report.Detail())
}

func TestRunStorageIntegrityChecks_DetectsForeignKeyViolation(t *testing.T) {
	db, _ := seededLiveDB(t, "storage-fk.db")

	// Insert a child row whose foreign key has no parent, with enforcement
	// off — the exact hole a migration that toggled PRAGMA foreign_keys=OFF,
	// or a restore from a non-enforcing tool, could leave behind.
	require.NoError(t, db.Exec("PRAGMA foreign_keys=OFF").Error)
	require.NoError(t, db.Exec(
		`INSERT INTO circle_members (created_at, updated_at, circle_id, user_id, member_vcard_uid)
		 VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'no-such-circle', 1, 'no-such-contact')`).Error)
	require.NoError(t, db.Exec("PRAGMA foreign_keys=ON").Error)

	report, err := RunStorageIntegrityChecks(db)
	require.NoError(t, err, "the pragma itself must run cleanly even when it finds problems")
	assert.False(t, report.OK)
	assert.Equal(t, "ok", report.IntegrityCheck, "the pages are fine; only the FK is dangling")
	assert.Contains(t, report.ForeignKeyCheck, "circle_members")
	assert.Contains(t, report.Detail(), "foreign_key_check")
}

func TestCheckDBIntegrityScheduled_RecordsDistinctDataResult(t *testing.T) {
	db, _ := seededLiveDB(t, "scheduled-data.db")
	var user models.User
	require.NoError(t, db.Where("username = ?", "integritytester").First(&user).Error)

	// Seed a data-invariant violation the PRAGMA pass cannot see: an edge
	// pointing at a contact that does not exist.
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: user.ID, SourceID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		TargetID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
		Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
	}).Error)

	cfg := config.Config{DBIntegrityCheckEnabled: true, DBIntegrityCheckIntervalHours: 24}
	require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

	var storage models.OperationalCheckResult
	require.NoError(t, db.Where("check_name = ?", models.JobNameDBIntegrityCheck).First(&storage).Error)
	assert.Equal(t, models.OpCheckStatusOK, storage.Status, "storage pass is clean")

	var data models.OperationalCheckResult
	require.NoError(t, db.Where("check_name = ?", models.CheckNameDataIntegrity).First(&data).Error)
	assert.Equal(t, models.OpCheckStatusFailed, data.Status, "data pass found the dangling edge")
	assert.Contains(t, data.Detail, "relationship_edge.endpoint_missing")
}

func TestCheckDBIntegrityScheduled_DataPassCleanRecordsOK(t *testing.T) {
	db, _ := seededLiveDB(t, "scheduled-data-ok.db")

	cfg := config.Config{DBIntegrityCheckEnabled: true, DBIntegrityCheckIntervalHours: 24}
	require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

	var data models.OperationalCheckResult
	require.NoError(t, db.Where("check_name = ?", models.CheckNameDataIntegrity).First(&data).Error)
	assert.Equal(t, models.OpCheckStatusOK, data.Status)
	assert.Empty(t, data.Detail)
}

// TestCheckDBIntegrityScheduledFoldsInSearchIndexConsistency pins SEARCH-02
// (issue #462) recommended action 6: the FTS consistency pass rides the
// existing integrity job — no third schedule — and records its own
// operational-check-result, distinct from the storage integrity_check.
func TestCheckDBIntegrityScheduledFoldsInSearchIndexConsistency(t *testing.T) {
	cfg := config.Config{DBIntegrityCheckEnabled: true, DBIntegrityCheckIntervalHours: 24}

	t.Run("clean index records ok", func(t *testing.T) {
		db := dbtest.New(t)
		u := models.User{Username: "fts-fold-ok", Password: "password123!A", Email: "fts-fold-ok@example.com"}
		require.NoError(t, db.Create(&u).Error)
		require.NoError(t, db.Create(&models.Contact{UserID: u.ID, Firstname: "Fold", Lastname: "Ok"}).Error)

		require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

		var row models.OperationalCheckResult
		require.NoError(t, db.Where("check_name = ?", OpCheckSearchIndexConsistency).First(&row).Error)
		assert.Equal(t, models.OpCheckStatusOK, row.Status)
	})

	t.Run("divergent index records failed with a detail", func(t *testing.T) {
		db := dbtest.New(t)
		u := models.User{Username: "fts-fold-bad", Password: "password123!A", Email: "fts-fold-bad@example.com"}
		require.NoError(t, db.Create(&u).Error)
		c := models.Contact{UserID: u.ID, Firstname: "Fold", Lastname: "Bad"}
		require.NoError(t, db.Create(&c).Error)
		require.NoError(t, db.Exec(`DELETE FROM contacts_fts WHERE rowid = ?`, c.ID).Error)

		require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

		var row models.OperationalCheckResult
		require.NoError(t, db.Where("check_name = ?", OpCheckSearchIndexConsistency).First(&row).Error)
		assert.Equal(t, models.OpCheckStatusFailed, row.Status)
		assert.Contains(t, row.Detail, "contacts_fts/"+FTSDivergenceMissingFromIndex)

		// The storage integrity_check itself is untouched and still ok.
		var storage models.OperationalCheckResult
		require.NoError(t, db.Where("check_name = ?", models.JobNameDBIntegrityCheck).First(&storage).Error)
		assert.Equal(t, models.OpCheckStatusOK, storage.Status)
	})

	t.Run("check that cannot run records error, not failed", func(t *testing.T) {
		db := dbtest.New(t)
		u := models.User{Username: "fts-fold-err", Password: "password123!A", Email: "fts-fold-err@example.com"}
		require.NoError(t, db.Create(&u).Error)
		require.NoError(t, db.Exec(`DROP TABLE notes_fts`).Error)

		require.NotPanics(t, func() { CheckDBIntegrityScheduled(db, cfg) })

		var row models.OperationalCheckResult
		require.NoError(t, db.Where("check_name = ?", OpCheckSearchIndexConsistency).First(&row).Error)
		assert.Equal(t, models.OpCheckStatusError, row.Status)
	})
}
