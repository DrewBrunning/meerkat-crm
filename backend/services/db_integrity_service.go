package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"gorm.io/gorm"
)

// EventDBIntegrityCheckFailed is the webhook event fired when a scheduled
// integrity check finds a problem — storage-level (PRAGMA) or application-level
// (the data-invariant pass, issue #460). The payload's "kind" field
// ("storage" | "data") says which. An operator can wire up ntfy/Gotify/a
// generic webhook receiver instead of only relying on log scraping.
const EventDBIntegrityCheckFailed = "db.integrity_check_failed"

// dbIntegrityCheckMinInterval is the de-dup window for this configurable-cadence
// job: the shared JobCatchupWindow of the configured period (issue #526, ADR
// 0011 — one margin for the whole fleet, no per-job constant).
func dbIntegrityCheckMinInterval(cfg config.Config) time.Duration {
	return JobCatchupWindow(time.Duration(cfg.DBIntegrityCheckIntervalHours) * time.Hour)
}

// integrityCheckRow is the shape of one row PRAGMA integrity_check returns.
// A healthy database returns exactly one row containing "ok"; a corrupted
// one returns one row per problem found, so every row is collected rather
// than only the first (backup.go's verifyBackup takes only the first row,
// which is fine there since VACUUM INTO's own success already implies the
// snapshot round-tripped cleanly — here the *list* of problems is the point).
type integrityCheckRow struct {
	IntegrityCheck string `gorm:"column:integrity_check"`
}

// foreignKeyCheckRow is one row PRAGMA foreign_key_check returns: a child row
// (table + rowid) whose foreign key has no parent, and which key (fkid) it
// is. A healthy database returns no rows. This catches what integrity_check
// cannot — a referential hole left behind if FK enforcement was ever bypassed
// (a migration that toggled PRAGMA foreign_keys=OFF, a restore from a
// non-enforcing tool). foreign_keys(1) is enforced on every app connection
// (database/migrate.go), so this should always be empty in practice; running
// it is the cheap proof.
type foreignKeyCheckRow struct {
	Table  string `gorm:"column:table"`
	RowID  int64  `gorm:"column:rowid"`
	Parent string `gorm:"column:parent"`
	FKID   int64  `gorm:"column:fkid"`
}

// walCheckpointRow is the shape PRAGMA wal_checkpoint(...) returns.
type walCheckpointRow struct {
	Busy         int `gorm:"column:busy"`
	Log          int `gorm:"column:log"`
	Checkpointed int `gorm:"column:checkpointed"`
}

// StorageIntegrityReport is the storage-level pass: the two SQLite pragmas
// that answer "are the pages, indexes, and declared foreign keys structurally
// sound". Distinct from the application-invariant DataIntegrityReport.
type StorageIntegrityReport struct {
	// OK is true when both pragmas are clean.
	OK bool `json:"ok"`
	// IntegrityCheck is "ok" or the "; "-joined problem lines from PRAGMA
	// integrity_check.
	IntegrityCheck string `json:"integrity_check"`
	// ForeignKeyCheck is "ok" or a "; "-joined summary of PRAGMA
	// foreign_key_check violations ("<table> row <rowid> -> <parent>").
	ForeignKeyCheck string `json:"foreign_key_check"`
}

// Detail folds the report into the one-line, secret-free string the
// OperationalCheckResult / webhook / alert paths want. "" when OK.
func (r StorageIntegrityReport) Detail() string {
	if r.OK {
		return ""
	}
	var parts []string
	if r.IntegrityCheck != "ok" && r.IntegrityCheck != "" {
		parts = append(parts, "integrity_check: "+r.IntegrityCheck)
	}
	if r.ForeignKeyCheck != "ok" && r.ForeignKeyCheck != "" {
		parts = append(parts, "foreign_key_check: "+r.ForeignKeyCheck)
	}
	return strings.Join(parts, "; ")
}

// RunStorageIntegrityChecks runs PRAGMA integrity_check and PRAGMA
// foreign_key_check against the live database, then a best-effort WAL
// checkpoint. It reuses the app's own live *gorm.DB connection rather than
// opening a second one — every other scheduled job in this package does the
// same. The returned error is non-nil only when a pragma could not be run at
// all (distinct from a clean run that found problems).
//
// PASSIVE checkpoint, not TRUNCATE: TRUNCATE reports busy=1 the instant any
// connection holds a read transaction, which is exactly the state a live
// server under real traffic is normally in (see database/backup.go's
// checkpoint(), which made the identical call for the identical reason). A
// checkpoint that spuriously fails under load would turn this job into a
// source of false alarms, so the checkpoint step is a best-effort tidy-up:
// its error is logged but never treated as a failed check.
func RunStorageIntegrityChecks(db *gorm.DB) (StorageIntegrityReport, error) {
	report := StorageIntegrityReport{IntegrityCheck: "ok", ForeignKeyCheck: "ok"}

	var iRows []integrityCheckRow
	if err := db.Raw("PRAGMA integrity_check").Scan(&iRows).Error; err != nil {
		return report, err
	}
	if !(len(iRows) == 1 && strings.EqualFold(iRows[0].IntegrityCheck, "ok")) {
		lines := make([]string, 0, len(iRows))
		for _, r := range iRows {
			lines = append(lines, r.IntegrityCheck)
		}
		report.IntegrityCheck = strings.Join(lines, "; ")
	}

	var fkRows []foreignKeyCheckRow
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&fkRows).Error; err != nil {
		return report, err
	}
	if len(fkRows) > 0 {
		lines := make([]string, 0, len(fkRows))
		for _, r := range fkRows {
			lines = append(lines, fmt.Sprintf("%s row %d -> %s", r.Table, r.RowID, r.Parent))
		}
		report.ForeignKeyCheck = strings.Join(lines, "; ")
	}

	report.OK = report.IntegrityCheck == "ok" && report.ForeignKeyCheck == "ok"

	var cp walCheckpointRow
	if cpErr := db.Raw("PRAGMA wal_checkpoint(PASSIVE)").Scan(&cp).Error; cpErr != nil {
		logger.Warn().Err(cpErr).Msg("db integrity check: best-effort WAL checkpoint failed")
	}

	return report, nil
}

// checkDBIntegrity is the pre-#460 signature (ok / detail / err) kept as a
// thin adapter over RunStorageIntegrityChecks for the storage-pass callers and
// tests that only care about the folded verdict.
func checkDBIntegrity(db *gorm.DB) (ok bool, detail string, err error) {
	report, err := RunStorageIntegrityChecks(db)
	if err != nil {
		return false, "", err
	}
	return report.OK, report.Detail(), nil
}

// CheckDBIntegrityScheduled is the scheduled job entry point (issue #273 for
// the storage pass, issue #460 for the data pass, issue #462 for the FTS
// consistency pass). Job-lock guarded (the T19 pattern) so a multi-instance
// deploy or a rapid restart doesn't re-run it back to back. All three passes
// run under the one lock, on the one schedule and config gate, but record and
// alert distinctly.
func CheckDBIntegrityScheduled(db *gorm.DB, cfg config.Config) {
	ctx := logger.JobContext(models.JobNameDBIntegrityCheck)
	if !cfg.DBIntegrityCheckEnabled {
		return
	}

	acquired, err := acquireJobLock(db, models.JobNameDBIntegrityCheck, dbIntegrityCheckMinInterval(cfg))
	if err != nil {
		logger.Error().Err(err).Msg("db integrity check: failed to check job lock")
		return
	}
	if !acquired {
		return
	}
	defer func() {
		if err := releaseJobLock(db, models.JobNameDBIntegrityCheck, true); err != nil {
			logger.Error().Err(err).Msg("db integrity check: failed to release job lock")
		}
	}()

	runStorageIntegrityPass(ctx, db, cfg)
	runDataIntegrityPass(ctx, db, cfg)

	// SEARCH-02 (issue #462): the derived FTS index gets its own consistency
	// pass, folded into this job rather than a third schedule. It is a meaning
	// check, not a corruption check — the storage can be fine while search is
	// degraded — so it records its own result and fires a warning-level
	// webhook, independently of the two passes above.
	checkSearchIndexConsistencyScheduled(ctx, db, cfg)
}

// runStorageIntegrityPass is the PRAGMA pass: records its outcome under
// models.JobNameDBIntegrityCheck and fires the webhook with kind "storage".
func runStorageIntegrityPass(ctx context.Context, db *gorm.DB, cfg config.Config) {
	report, err := RunStorageIntegrityChecks(db)
	if err != nil {
		logger.Error().Err(err).Msg("db integrity check: failed to run storage pragmas")
		RecordOperationalCheckResult(db, models.JobNameDBIntegrityCheck, models.OpCheckStatusError, err.Error())
		triggerWebhooksForAllUsers(ctx, db, cfg, EventDBIntegrityCheckFailed, map[string]interface{}{
			"kind":  "storage",
			"error": err.Error(),
		})
		return
	}
	if !report.OK {
		detail := report.Detail()
		logger.Error().Str("detail", detail).Msg("db integrity check: storage corruption detected")
		RecordOperationalCheckResult(db, models.JobNameDBIntegrityCheck, models.OpCheckStatusFailed, detail)
		triggerWebhooksForAllUsers(ctx, db, cfg, EventDBIntegrityCheckFailed, map[string]interface{}{
			"kind":   "storage",
			"detail": detail,
		})
		return
	}
	RecordOperationalCheckResult(db, models.JobNameDBIntegrityCheck, models.OpCheckStatusOK, "")
	logger.Info().Msg("db integrity check: storage ok")
}

// runDataIntegrityPass is the application-invariant pass (issue #460): records
// its outcome under models.CheckNameDataIntegrity and fires the webhook with
// kind "data". Only real violations (not info findings) flip the result to
// failed.
func runDataIntegrityPass(ctx context.Context, db *gorm.DB, cfg config.Config) {
	report, err := RunDataIntegrityChecks(ctx, db, cfg)
	if err != nil {
		logger.Error().Err(err).Msg("db integrity check: data-invariant pass could not complete")
		RecordOperationalCheckResult(db, models.CheckNameDataIntegrity, models.OpCheckStatusError, err.Error())
		triggerWebhooksForAllUsers(ctx, db, cfg, EventDBIntegrityCheckFailed, map[string]interface{}{
			"kind":  "data",
			"error": err.Error(),
		})
		return
	}
	if !report.OK {
		detail := summarizeIntegrityFindings(report.Findings)
		logger.Error().Str("detail", detail).Msg("db integrity check: data invariant violation detected")
		RecordOperationalCheckResult(db, models.CheckNameDataIntegrity, models.OpCheckStatusFailed, detail)
		triggerWebhooksForAllUsers(ctx, db, cfg, EventDBIntegrityCheckFailed, map[string]interface{}{
			"kind":     "data",
			"findings": report.violationCount(),
			"detail":   detail,
		})
		return
	}
	RecordOperationalCheckResult(db, models.CheckNameDataIntegrity, models.OpCheckStatusOK, "")
	logger.Info().Msg("db integrity check: data invariants ok")
}

// summarizeIntegrityFindings folds violation findings into one secret-free
// line: "<check> (<count>)" per class, joined. Info findings are omitted.
func summarizeIntegrityFindings(findings []IntegrityFinding) string {
	var parts []string
	for _, f := range findings {
		if f.Severity != IntegritySeverityViolation {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s x%d", f.Check, f.Count))
	}
	return strings.Join(parts, "; ")
}

// OpCheckSearchIndexConsistency is the operational-check-result name for the
// scheduled FTS index consistency pass (SEARCH-02, issue #462).
const OpCheckSearchIndexConsistency = "search_index_consistency"

// EventSearchIndexInconsistent is the webhook event fired when the scheduled
// FTS consistency pass finds the index out of sync with canonical data. It
// reuses the db.integrity_check_failed channel (an operator wiring "database
// health" alerts wants this too) with a discriminating payload; the storage
// integrity_check itself is unaffected and its own OK result still stands.
const EventSearchIndexInconsistent = EventDBIntegrityCheckFailed

func checkSearchIndexConsistencyScheduled(ctx context.Context, db *gorm.DB, cfg config.Config) {
	res, err := CheckSearchIndexConsistency(db)
	if err != nil {
		logger.Error().Err(err).Msg("search index consistency: check failed to run")
		RecordOperationalCheckResult(db, OpCheckSearchIndexConsistency, models.OpCheckStatusError, err.Error())
		// Alert on the error branch too (issue #921): a physically corrupt FTS
		// whose row-count probe still succeeds, or any other read failure that
		// stops the consistency check from running, must not be silent. The
		// storage/data passes already fire here; the search pass was the one
		// shape that only wrote a result row.
		triggerWebhooksForAllUsers(ctx, db, cfg, EventSearchIndexInconsistent, map[string]interface{}{
			"check": OpCheckSearchIndexConsistency,
			"error": err.Error(),
		})
		return
	}
	if !res.Clean() {
		summary := res.Summary()
		logger.Warn().Str("detail", summary).Int("divergences", len(res.Divergences)).
			Msg("search index consistency: index is out of sync with canonical data — a rebuild (POST /admin/search/rebuild) will repair it")
		RecordOperationalCheckResult(db, OpCheckSearchIndexConsistency, models.OpCheckStatusFailed, summary)
		triggerWebhooksForAllUsers(ctx, db, cfg, EventSearchIndexInconsistent, map[string]interface{}{
			"check":  OpCheckSearchIndexConsistency,
			"detail": summary,
		})
		return
	}
	RecordOperationalCheckResult(db, OpCheckSearchIndexConsistency, models.OpCheckStatusOK, "")
	logger.Info().Msg("search index consistency: ok")
}
