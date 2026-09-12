package database

import (
	"fmt"
	"os"
	"strings"

	"mycorrhizal/logger"
)

// ErrDatabaseCorrupt is the fail-closed error the startup/migration path returns
// when an existing database file fails PRAGMA integrity_check (issue #921). It
// is a stable, assertable sentinel in the same family as ErrDirtyMigration /
// ErrSchemaAheadOfBinary / ErrSubFloorMigration: the server start path logs it
// via logger.Fatal ("Failed to initialize database") and cmd/migrate prints it,
// both BEFORE any migration or mandatory pre-migration backup runs, with the
// file left exactly as it was.
//
// Detection previously happened only in the scheduled integrity job (default
// every DB_INTEGRITY_CHECK_INTERVAL_HOURS = 24h), so a corrupt-but-version-
// readable file could boot, be snapshotted as the rollback point, and be
// migrated before anyone noticed. The startup probe makes the outcome defined:
// refuse before any write, naming the recovery.
type ErrDatabaseCorrupt struct {
	// Detail is what integrity_check reported (its first problem line, or the
	// driver error when the check itself could not run). It carries no user
	// data — integrity_check reports page/structure findings.
	Detail string
}

func (e *ErrDatabaseCorrupt) Error() string {
	return fmt.Sprintf(
		"database failed PRAGMA integrity_check: %s. The file is corrupt, so migrating it "+
			"(and snapshotting it as the mandatory pre-migration rollback point) would be unsafe. "+
			"Refusing to start (fail-closed); the database is untouched. Restore the most recent "+
			"good backup — see docs/deployment.md (Backups → Restore) — then start again.",
		e.Detail,
	)
}

// probeStartupIntegrity runs PRAGMA integrity_check against dbPath before the
// startup/migration path takes its pre-migration backup or applies any
// migration (issue #921). A missing file is a fresh install, not corruption, so
// it is skipped and the normal migrate-from-empty path runs.
//
// It deliberately reuses IntegrityCheck — the same raw-sql, GORM-free probe the
// operator CLIs (cmd/dbinspect, cmd/doctor) and the external-fault chaos harness
// use — so the startup definition of "corrupt" cannot drift from theirs.
func probeStartupIntegrity(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil // fresh install: nothing to check
		}
		return fmt.Errorf("cannot read database %q: %w", dbPath, err)
	}

	result, err := IntegrityCheck(dbPath)
	if err != nil {
		logger.Error().
			Str(logger.FieldComponent, "database").
			Err(err).
			Msg("startup integrity check failed to run")
		return &ErrDatabaseCorrupt{Detail: err.Error()}
	}
	if !strings.EqualFold(result, "ok") {
		logger.Error().
			Str(logger.FieldComponent, "database").
			Str("detail", result).
			Msg("startup integrity check found corruption")
		return &ErrDatabaseCorrupt{Detail: result}
	}
	return nil
}
