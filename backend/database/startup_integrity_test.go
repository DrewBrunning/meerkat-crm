package database

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corruptPageSize is SQLite's default page size for the databases this app
// builds.
const corruptPageSize = 4096

// corruptDataPage overwrites one whole leaf data page of the closed database at
// path with 0xFF bytes in place, probing candidate pages outward from the middle
// until it lands one whose corruption PRAGMA integrity_check reports as findings
// rather than aborting the connection with SQLITE_CORRUPT. The database
// package's own tests cannot import internal/dbtest (import cycle), so this
// small helper is local to them instead of shared.
func corruptDataPage(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	pageCount := info.Size() / corruptPageSize
	require.Greater(t, pageCount, int64(4), "seed a multi-page database before corrupting it")

	mid := pageCount / 2
	if probePageReportsFindings(t, path, mid) {
		return
	}
	for delta := int64(1); delta < pageCount/2; delta++ {
		for _, candidate := range []int64{mid + delta, mid - delta} {
			if candidate < 2 || candidate >= pageCount {
				continue
			}
			if probePageReportsFindings(t, path, candidate) {
				return
			}
		}
	}
	t.Fatal("could not find a data page whose corruption integrity_check reports as findings")
}

// probePageReportsFindings corrupts page target (1-indexed) in place, checks
// whether IntegrityCheck reports findings, and restores the original bytes on
// any other outcome so the caller can try the next page.
func probePageReportsFindings(t *testing.T, path string, target int64) bool {
	t.Helper()
	original := readCorruptPage(t, path, target)
	writeCorruptPage(t, path, target, bytes.Repeat([]byte{0xFF}, corruptPageSize))

	result, err := IntegrityCheck(path)
	if err == nil && result != "" && !strings.EqualFold(result, "ok") {
		return true
	}
	writeCorruptPage(t, path, target, original)
	return false
}

func readCorruptPage(t *testing.T, path string, target int64) []byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	page := make([]byte, corruptPageSize)
	_, err = f.ReadAt(page, (target-1)*corruptPageSize)
	require.NoError(t, err)
	return page
}

func writeCorruptPage(t *testing.T, path string, target int64, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = f.WriteAt(data, (target-1)*corruptPageSize)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// seedMultiPageDatabase builds a migrated database with enough rows that it
// spans several SQLite pages, then closes it so corruptDataPage can overwrite a
// real data page. The filler table is deliberately independent of every
// application table so corrupting one of its pages cannot disturb the
// schema_migrations row that must stay readable for this test.
func seedMultiPageDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seed.db")
	db, err := InitDB(path)
	require.NoError(t, err)

	require.NoError(t, db.Exec(`CREATE TABLE filler (id INTEGER PRIMARY KEY, payload TEXT)`).Error)
	for i := 0; i < 500; i++ {
		require.NoError(t, db.Exec(`INSERT INTO filler (payload) VALUES (?)`,
			strings.Repeat("data-page-padding-", 20)).Error)
	}
	require.NoError(t, db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	return path
}

// TestProbeStartupIntegrityPassesFreshAndHealthy pins the two non-corrupt
// outcomes: a path that does not exist yet is a fresh install (skipped, not an
// error), and a real migrated database is clean.
func TestProbeStartupIntegrityPassesFreshAndHealthy(t *testing.T) {
	t.Parallel()

	t.Run("missing file is a fresh install", func(t *testing.T) {
		t.Parallel()
		err := probeStartupIntegrity(filepath.Join(t.TempDir(), "not-created-yet.db"))
		assert.NoError(t, err)
	})

	t.Run("migrated database is healthy", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "healthy.db")
		require.NoError(t, MigrateUp(path))
		assert.NoError(t, probeStartupIntegrity(path))
	})
}

// TestProbeStartupIntegrityDetectsCorruptDataPage is the issue #921 gap-1
// pin: a corrupt page that leaves schema_migrations readable must be detected
// on the startup path and returned as the typed fail-closed error, before any
// migration or backup — not discovered up to 24h later by the scheduled job.
func TestProbeStartupIntegrityDetectsCorruptDataPage(t *testing.T) {
	path := seedMultiPageDatabase(t)
	corruptDataPage(t, path)

	// The corruption is the "page content does not parse" kind: the migration
	// version is still readable, which is exactly the case the old startup path
	// could not see.
	_, _, ok, err := MigrationVersion(path)
	require.NoError(t, err)
	assert.True(t, ok, "the corruption target must leave schema_migrations readable")

	err = probeStartupIntegrity(path)
	var corrupt *ErrDatabaseCorrupt
	require.Error(t, err)
	require.True(t, errors.As(err, &corrupt), "want *ErrDatabaseCorrupt, got %T: %v", err, err)
	assert.NotEmpty(t, corrupt.Detail)

	// InitDB surfaces the same typed refusal (wrapped) and does not migrate.
	_, initErr := InitDB(path)
	require.Error(t, initErr)
	assert.True(t, errors.As(initErr, &corrupt), "InitDB must wrap the typed corruption error, got %v", initErr)
}

// TestProbeStartupIntegrityOnUnopenableFile pins the other corruption shape: a
// file whose bytes are not a usable database at all. IntegrityCheck itself
// errors, and the probe still fails closed with the same typed error rather
// than limping on.
func TestProbeStartupIntegrityOnUnopenableFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "header-only.db")
	require.NoError(t, os.WriteFile(path, make([]byte, 512), 0o600))

	err := probeStartupIntegrity(path)
	var corrupt *ErrDatabaseCorrupt
	require.Error(t, err)
	assert.True(t, errors.As(err, &corrupt), "want *ErrDatabaseCorrupt, got %T: %v", err, err)
	assert.NotEmpty(t, corrupt.Detail)
}

// TestMigrateUpRefusesCorruptDatabase pins the operator `make migrate-up` path:
// it goes through the same file-level wrapper as startup, so corruption refuses
// there too (the probe runs before the pre-migration backup).
func TestMigrateUpRefusesCorruptDatabase(t *testing.T) {
	path := seedMultiPageDatabase(t)
	corruptDataPage(t, path)

	err := MigrateUp(path)
	var corrupt *ErrDatabaseCorrupt
	require.Error(t, err)
	assert.True(t, errors.As(err, &corrupt), "want *ErrDatabaseCorrupt, got %T: %v", err, err)

	// No pre-migration backup directory was created: the probe refused before
	// the backup path ran.
	_, statErr := os.Stat(filepath.Join(filepath.Dir(path), "pre-migration"))
	assert.True(t, os.IsNotExist(statErr), "the corruption probe must refuse before writing a pre-migration backup")
}

// TestProbeStartupIntegrityDoesNotCreateDatabase proves the probe never
// materializes a database at a missing path (sql.Open is lazy and would
// otherwise create an empty file, turning a typo'd path into a silent "fresh
// install").
func TestProbeStartupIntegrityDoesNotCreateDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "must-not-appear.db")
	require.NoError(t, probeStartupIntegrity(path))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "the probe must not create the database file")
}

// TestProbeStartupIntegrityPropagatesStatError covers the non-ENOENT stat
// failure branch: the probe reports the read error instead of mistaking it for a
// fresh install. A path with a NUL byte makes os.Stat fail with EINVAL.
func TestProbeStartupIntegrityPropagatesStatError(t *testing.T) {
	t.Parallel()
	err := probeStartupIntegrity("\x00")
	require.Error(t, err)
	var corrupt *ErrDatabaseCorrupt
	assert.False(t, errors.As(err, &corrupt), "a stat failure is a read error, not a corruption finding")
}
