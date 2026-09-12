package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emptyMigrationVersionTable removes the applied-migration row, leaving the
// schema_migrations table present but empty — the state a corrupt/tampered
// database reads as after its version bookkeeping is lost (issue #926).
func emptyMigrationVersionTable(t *testing.T, dbPath string) {
	t.Helper()
	db, err := InitDB(dbPath)
	require.NoError(t, err)

	require.NoError(t, db.Exec("DELETE FROM schema_migrations").Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// TestMigrateUpRefusesPopulatedVersionlessDatabase is the issue #926 pin: a
// populated database whose schema_migrations row was removed must not be
// mistaken for a fresh install (which would skip the mandatory pre-migration
// backup and replay 000001 against existing tables). It must refuse with the
// typed error and leave the schema untouched.
func TestMigrateUpRefusesPopulatedVersionlessDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "populated-versionless.db")
	require.NoError(t, MigrateUp(path))
	emptyMigrationVersionTable(t, path)

	// The database really does still carry application tables.
	version, _, ok, err := MigrationVersion(path)
	require.NoError(t, err)
	assert.False(t, ok, "the version row is gone; the file must read as version-less")
	assert.Zero(t, version)

	err = MigrateUp(path)
	var versionless *ErrPopulatedVersionlessDatabase
	require.Error(t, err)
	require.True(t, errors.As(err, &versionless), "want *ErrPopulatedVersionlessDatabase, got %T: %v", err, err)
	assert.NotEmpty(t, versionless.Tables)
	assert.NotContains(t, versionless.Tables, defaultMigrationsTable)

	// Nothing was migrated and no half-applied state was created.
	_, _, stillOk, err := MigrationVersion(path)
	require.NoError(t, err)
	assert.False(t, stillOk, "the refusal must not write a version row")
	assert.Equal(t, "ok", integrityCheck(t, path), "the refusal must leave the database intact")
}

// TestInitDBRefusesPopulatedVersionlessDatabase covers the server startup path
// through the same guard.
func TestInitDBRefusesPopulatedVersionlessDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "populated-versionless-init.db")
	require.NoError(t, MigrateUp(path))
	emptyMigrationVersionTable(t, path)

	_, err := InitDB(path)
	var versionless *ErrPopulatedVersionlessDatabase
	require.Error(t, err)
	assert.True(t, errors.As(err, &versionless), "InitDB must refuse, got %T: %v", err, err)
}

// TestMigrateUpStillMigratesGenuinelyFreshDatabase is the control: an empty
// database has no application tables, so the version-less guard must not fire
// and the fresh install must migrate normally.
func TestMigrateUpStillMigratesGenuinelyFreshDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fresh.db")
	require.NoError(t, MigrateUp(path))

	version, dirty, ok, err := MigrationVersion(path)
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, dirty)
	latest, err := LatestMigrationVersion()
	require.NoError(t, err)
	assert.Equal(t, latest, version)
}

// TestApplicationTablesClassifiesBookkeepingOnlyDatabase pins the classifier:
// application tables are reported on a migrated database, and a database whose
// only non-internal table is the migration bookkeeping table is not populated.
func TestApplicationTablesClassifiesBookkeepingOnlyDatabase(t *testing.T) {
	t.Parallel()

	t.Run("migrated database reports application tables", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "migrated.db")
		require.NoError(t, MigrateUp(path))
		conn, err := sql.Open("sqlite", openDSN(path))
		require.NoError(t, err)
		defer conn.Close()

		tables, err := applicationTables(conn)
		require.NoError(t, err)
		assert.NotEmpty(t, tables)
		for _, name := range tables {
			assert.NotEqual(t, defaultMigrationsTable, name)
			assert.NotContains(t, name, "sqlite_")
		}
	})

	t.Run("bookkeeping-only database is empty", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "bookkeeping-only.db")
		conn, err := sql.Open("sqlite", openDSN(path))
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.Exec("CREATE TABLE " + defaultMigrationsTable + " (version INTEGER, dirty INTEGER)")
		require.NoError(t, err)

		tables, err := applicationTables(conn)
		require.NoError(t, err)
		assert.Empty(t, tables)
	})
}

// TestApplicationTablesReportsQueryError covers the guard's fail-closed error
// path: if the table list cannot be read, RunMigrations must surface the error
// rather than assume the database is fresh.
func TestApplicationTablesReportsQueryError(t *testing.T) {
	t.Parallel()
	conn, err := sql.Open("sqlite", openDSN(filepath.Join(t.TempDir(), "closed.db")))
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = applicationTables(conn)
	require.Error(t, err)
}
