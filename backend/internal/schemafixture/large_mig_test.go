package schemafixture

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mycorrhizal/database"
	"mycorrhizal/internal/canonicalfixture"
	"mycorrhizal/internal/faults"
	"mycorrhizal/internal/largedata"
	"mycorrhizal/logger"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// largeDatasetContacts is the CI-sized "large" dataset: 2,000 contacts, which
// rounds up to 75 manifest blocks (2,025 contacts, since the canonical manifest
// carries 27 records after the I18N-01 addition, issue #484). It is enough
// rows that the row-touching migrations do real work — the sort-name backfill,
// the revision-token UPDATE, the audit table rebuild — while staying fast
// enough to run in the dedicated nightly/main-push job. The full production-
// scale profile (100k+ contacts) is a documented operator-run measurement, not
// a CI test; see docs/development/scale-testing.md and cmd/migratebench.
const largeDatasetContacts = 2000

// largeTestsEnabled gates the slow large-dataset migration tests. They are
// deliberately NOT part of the default `go test ./...` suite: at 2,025
// contacts each they take minutes under -race, which every PR would pay. The
// migration-tests `large-dataset` CI job (main push + nightly) sets
// MYCORRHIZAL_LARGE_TESTS=1 to run them; a contributor sets the same variable
// to run them locally.
func largeTestsEnabled() bool {
	return os.Getenv("MYCORRHIZAL_LARGE_TESTS") == "1"
}

// migrationStatementFault is the documented failure-injection seam name for
// the migration driver's per-statement Run (docs/development/fault-injection.md;
// the external-fault job arms the same seam via
// MYCORRHIZAL_FAULTS=database.migration.statement:pause:...). Referenced by its
// documented value because database.faultMigrationStatement is unexported.
const migrationStatementFault = "database.migration.statement"

// buildLargeFixture builds a large database FILE populated with the scaled
// canonical manifest and migrated to exactly the release's schema version, and
// returns the CLOSED file path. The closed file is what the upgrade tests
// migrate: database.InitDB reopens and runs every pending migration — the
// exact path the server boots through (CLAUDE.md backend trap #1).
func buildLargeFixture(t *testing.T, release Release) string {
	t.Helper()
	if !largeTestsEnabled() {
		t.Skip("large-dataset migration tests run in the nightly/main-push large-dataset CI job (set MYCORRHIZAL_LARGE_TESTS=1 to run them locally)")
	}

	srcPath := filepath.Join(t.TempDir(), "large-src.db")
	srcDB, err := database.InitDB(srcPath)
	require.NoError(t, err, "current-schema scratch database must build")
	defer closeFixtureDB(t, srcDB)

	m, err := canonicalfixture.Read()
	require.NoError(t, err, "the committed canonical manifest must load")
	scaled, err := largedata.Scale(m, largeDatasetContacts)
	require.NoError(t, err)
	_, err = canonicalfixture.Populate(srcDB, scaled)
	require.NoError(t, err, "the scaled manifest must populate through the real REST code paths")

	floorPath := filepath.Join(t.TempDir(), "large-"+release.Tag+".db")
	require.NoError(t, TransplantDataToVersion(srcDB, release.Version, floorPath),
		"the current-schema data must transplant into the %s schema", release.Tag)
	return floorPath
}

// TestLargeDatasetUpgradeToCurrent is the large-dataset migration test (issue
// #495): every supported release's large fixture — populated at 75x the
// canonical pathological manifest, including 75 soft-deleted contacts and 75
// vcard-uid-recreating pairs — migrates to the current schema through
// database.InitDB with every table's row count surviving, a clean flag, the
// current version, and PRAGMA integrity_check ok. The v0.6.0 entry is the
// longest supported skip (v0.6.0 -> current) on the large dataset.
func TestLargeDatasetUpgradeToCurrent(t *testing.T) {
	latest, err := database.LatestMigrationVersion()
	require.NoError(t, err)

	for _, r := range SupportedReleases {
		t.Run(r.Tag, func(t *testing.T) {
			path := buildLargeFixture(t, r)
			before := tableCountsAtPath(t, path)
			require.NoError(t, database.MigrateUp(path), "%s large fixture must migrate to the current schema", r.Tag)

			db := databaseMustOpen(t, path)
			defer closeFixtureDB(t, db)

			version, dirty, ok, err := database.AppliedMigrationVersion(db)
			require.NoError(t, err)
			require.True(t, ok)
			assert.EqualValues(t, latest, version, "%s -> current must land on the current schema", r.Tag)
			assert.False(t, dirty)
			assertRowCountsPreserved(t, r.Tag+" (large) -> current", before, tableCounts(t, db))
			assertIntegrity(t, path, "ok")
		})
	}
}

// TestLargeDatasetUpgradeEmitsProgress pins issue #495 action 6 ("emit
// progress") on the large dataset itself: an operator watching a long upgrade
// must see per-migration heartbeat lines, not silence until the whole batch
// finishes.
func TestLargeDatasetUpgradeEmitsProgress(t *testing.T) {
	floor := SupportedReleases[0] // v0.6.0, the longest skip
	path := buildLargeFixture(t, floor)

	buf := captureLogsAt(t, zerolog.InfoLevel)
	require.NoError(t, database.MigrateUp(path))

	out := buf.String()
	assert.Contains(t, out, "migration_step_started", "a long upgrade must log each migration start")
	assert.Contains(t, out, "migration_step_completed", "a long upgrade must log each migration completion")
	// The last pending migration on a floor database runs, so the completed
	// heartbeat must name the actual file, not a placeholder.
	assert.Contains(t, out, ".up.sql")
}

// TestLargeDatasetInterruptedMigrationFailsClosed is the large-dataset half of
// TEST-06 / MIG-04 (issues #434, #439): a migration interrupted partway
// through on a large database must leave the MIG-04 refusal state — dirty at
// the interrupted version, schema rolled back, integrity intact — never a
// partially-migrated database that looks healthy. The interruption is injected
// in-process at the migration driver's statement seam (the same seam the
// external-fault chaos job interrupts with SIGKILL), because per the
// split-harness rule it can be expressed as an error across an existing
// interface.
func TestLargeDatasetInterruptedMigrationFailsClosed(t *testing.T) {
	faults.Reset()
	t.Cleanup(faults.Reset)

	floor := SupportedReleases[0]
	path := buildLargeFixture(t, floor)

	faults.ArmError(migrationStatementFault, &faults.ErrInjected{Name: migrationStatementFault})
	err := database.MigrateUp(path)
	require.Error(t, err, "an interrupted large migration must fail")
	faults.Disarm(migrationStatementFault)

	// The crash signature: dirty at the first pending migration, the schema
	// still at the floor (the body rolled back), integrity ok.
	version, dirty := migrationStateAtPath(t, path)
	assert.EqualValues(t, floor.Version+1, version, "interrupted at the first pending migration")
	assert.True(t, dirty, "an interrupted migration must leave the dirty flag")
	assertIntegrity(t, path, "ok")

	// Fail-closed: the next startup run refuses with the typed sentinel and
	// names the recovery — it must not boot and repair on its own.
	err = database.MigrateUp(path)
	var dirtyErr *database.ErrDirtyMigration
	require.ErrorAs(t, err, &dirtyErr, "the refusal must be a typed ErrDirtyMigration, not a generic failure")
	assert.EqualValues(t, floor.Version+1, dirtyErr.Version)
	assert.Contains(t, err.Error(), "Restore the pre-migration backup")

	// Operator-only recovery still lands cleanly on the large database.
	require.NoError(t, database.MigrateForce(path), "force must recover the interrupted large migration")
	latest, err := database.LatestMigrationVersion()
	require.NoError(t, err)
	version, dirty = migrationStateAtPath(t, path)
	assert.EqualValues(t, latest, version)
	assert.False(t, dirty)
	assertIntegrity(t, path, "ok")
}

// TestLargeDatasetBackupRestoresAtScale covers issue #495's "the pre-migration
// backup is actually restorable at that size" (related: issue #530): a
// BackupSnapshot of the large floor database — the shape an operator takes
// before a big upgrade — must be a self-contained file that passes PRAGMA
// integrity_check at full size, presents the floor version, and can be
// migrated to current (restore, then upgrade).
func TestLargeDatasetBackupRestoresAtScale(t *testing.T) {
	floor := SupportedReleases[0]
	path := buildLargeFixture(t, floor)

	backupPath := filepath.Join(t.TempDir(), "large-backup.db")
	require.NoError(t, database.BackupSnapshot(path, backupPath), "a pre-migration backup at scale must succeed")

	// The restored backup is a complete database on its own: integrity ok and
	// at the floor version, no WAL sidecar required.
	assertIntegrity(t, backupPath, "ok")
	version, dirty := migrationStateAtPath(t, backupPath)
	assert.EqualValues(t, floor.Version, version, "the backup must capture the pre-upgrade schema")
	assert.False(t, dirty)

	// Restore-then-upgrade: the backup migrates to the current schema cleanly.
	require.NoError(t, database.MigrateUp(backupPath), "a restored large backup must upgrade to the current schema")
	latest, err := database.LatestMigrationVersion()
	require.NoError(t, err)
	version, dirty = migrationStateAtPath(t, backupPath)
	assert.EqualValues(t, latest, version)
	assert.False(t, dirty)
	assertIntegrity(t, backupPath, "ok")
}

// databaseMustOpen opens a migrated GORM handle to a file that was just
// migrated, failing the test on error.
func databaseMustOpen(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := database.OpenMigratedFile(path)
	require.NoError(t, err)
	return db
}

// migrationStateAtPath reads the schema_migrations version/dirty pair straight
// off disk, failing the test on error.
func migrationStateAtPath(t *testing.T, path string) (version uint, dirty bool) {
	t.Helper()
	version, dirty, ok, err := database.MigrationVersion(path)
	require.NoError(t, err)
	require.True(t, ok, "the file must carry a schema_migrations row")
	return version, dirty
}

// assertIntegrity runs PRAGMA integrity_check on a file and asserts the result.
func assertIntegrity(t *testing.T, path, want string) {
	t.Helper()
	db := databaseMustOpen(t, path)
	defer closeFixtureDB(t, db)
	var result string
	require.NoError(t, db.Raw("PRAGMA integrity_check").Scan(&result).Error)
	assert.Equal(t, want, result)
}

// captureLogsAt swaps the package logger for one writing JSON to a buffer at
// the given level and restores it afterwards (mirrors the database package's
// captureMigrationLogger).
func captureLogsAt(t *testing.T, level zerolog.Level) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := logger.Logger
	oldDefault := zerolog.DefaultContextLogger
	oldLevel := zerolog.GlobalLevel()
	logger.Logger = zerolog.New(buf)
	zerolog.DefaultContextLogger = &logger.Logger
	zerolog.SetGlobalLevel(level)
	t.Cleanup(func() {
		logger.Logger = old
		zerolog.DefaultContextLogger = oldDefault
		zerolog.SetGlobalLevel(oldLevel)
	})
	return buf
}
