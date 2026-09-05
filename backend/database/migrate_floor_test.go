package database

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subFloorDB builds a database whose schema is exactly migrations 000001..N —
// the shape a release whose highest applied migration is N would present —
// without going through RunMigrations (which now refuses sub-floor databases).
func subFloorDB(t *testing.T, steps int) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "subfloor.db")

	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	require.NoError(t, err)
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB)
	require.NoError(t, err)
	require.NoError(t, m.Steps(steps))
	return dbPath
}

// TestSubFloorDatabaseRefusesToMigrate is issue #529 action 4: a database
// whose schema predates the v0.6.0 floor must refuse to migrate and name
// v0.6.0 as the required intermediate — not run a partial migration, not
// crash. Version 30 is the last sub-floor release (v0.5.9).
func TestSubFloorDatabaseRefusesToMigrate(t *testing.T) {
	t.Parallel()
	dbPath := subFloorDB(t, 30)

	versionBefore, _, okBefore, err := MigrationVersion(dbPath)
	require.NoError(t, err)
	require.True(t, okBefore)
	assert.EqualValues(t, 30, versionBefore)

	_, err = InitDB(dbPath)
	require.Error(t, err, "a sub-floor database must refuse to migrate")

	var subFloor *ErrSubFloorMigration
	require.ErrorAs(t, err, &subFloor, "the refusal must be a typed ErrSubFloorMigration, not a generic failure")
	assert.EqualValues(t, 30, subFloor.Version)

	msg := err.Error()
	assert.Contains(t, msg, "v0.6.0", "the refusal must name the required intermediate release")
	assert.Contains(t, msg, "Upgrade this instance to v0.6.0 first", "the refusal must carry the two-step instruction")

	versionAfter, dirty, okAfter, err := MigrationVersion(dbPath)
	require.NoError(t, err)
	require.True(t, okAfter)
	assert.EqualValues(t, 30, versionAfter, "a refused database must not have been partially migrated")
	assert.False(t, dirty, "a refused database must not be left dirty")
}

// TestSubFloorRefusalHoldsForEveryPreFloorVersion makes the refusal monotone
// across the whole pre-floor range: v0.2.0-alpha-candidate (000008) through
// the last sub-floor release (v0.5.9, 000030) all refuse rather than migrate
// best-effort.
func TestSubFloorRefusalHoldsForEveryPreFloorVersion(t *testing.T) {
	t.Parallel()
	for _, version := range []uint{8, 15, 22, 30} {
		t.Run(fmt.Sprintf("version-%d", version), func(t *testing.T) {
			dbPath := subFloorDB(t, int(version))
			_, err := InitDB(dbPath)
			require.Error(t, err)
			var subFloor *ErrSubFloorMigration
			require.ErrorAs(t, err, &subFloor)
			assert.EqualValues(t, version, subFloor.Version)
		})
	}
}

// TestFreshDatabaseIsNotSubFloor pins that the refusal targets only databases
// with an existing pre-floor schema: a brand-new database (no schema_migrations
// row) is not a sub-floor instance and must migrate to the latest version.
func TestFreshDatabaseIsNotSubFloor(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	db, err := InitDB(dbPath)
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	version, _, ok, err := MigrationVersion(dbPath)
	require.NoError(t, err)
	require.True(t, ok)
	assert.EqualValues(t, 51, version, "a fresh database must migrate to the latest version")
}

// TestDirtySubFloorDatabaseRefusesToMigrate pins the MIG-04 (issue #439 /
// #546) posture for the sub-floor edge: a DIRTY database refuses to start
// regardless of version. The old code exempted a dirty sub-floor database so
// the dirty-force recovery could run; that exemption was the fail-open bug —
// a dirty flag means the schema state is unknown, so the refusal comes first
// and names the dirty version, and recovery is restore-from-backup or the
// operator-only CLI force. A dirty sub-floor database is an interrupted
// migration run, not a stable deployment, but the torn state still must not be
// force-continued at boot.
func TestDirtySubFloorDatabaseRefusesToMigrate(t *testing.T) {
	t.Parallel()
	dbPath := subFloorDB(t, 30)

	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	require.NoError(t, err)
	_, err = sqlDB.Exec("UPDATE schema_migrations SET dirty = 1")
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = InitDB(dbPath)
	require.Error(t, err, "a dirty sub-floor database must refuse to migrate")

	var dirtyErr *ErrDirtyMigration
	require.ErrorAs(t, err, &dirtyErr, "the refusal must be a typed ErrDirtyMigration, not a generic failure")
	assert.EqualValues(t, 30, dirtyErr.Version, "the refusal must name the dirty version")

	version, dirty, ok, err := MigrationVersion(dbPath)
	require.NoError(t, err)
	require.True(t, ok)
	assert.EqualValues(t, 30, version, "a refused database must not have been partially migrated")
	assert.True(t, dirty, "a refused database must still carry its dirty flag — a refusal must not alter it")
}

// TestBridgeOverrideMigratesSubFloor pins the one-time bridge escape hatch
// (issue #529 action 5): the documented env var is the ONLY way a pre-floor
// database can be migrated in one binary, and it is exactly what the v0.2.0
// bridge procedure (docs/upgrade-compatibility.md) sets when the v0.6.0
// intermediate binary cannot be produced. Default is refuse; the override is
// explicit and logged, never silent.
func TestBridgeOverrideMigratesSubFloor(t *testing.T) {
	dbPath := subFloorDB(t, 30)

	t.Setenv(subFloorMigrationEnvVar, "1")
	db, err := InitDB(dbPath)
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	version, dirty, ok, err := MigrationVersion(dbPath)
	require.NoError(t, err)
	require.True(t, ok)
	assert.EqualValues(t, 51, version, "the bridge override must migrate to the latest version")
	assert.False(t, dirty)
}

// TestErrSubFloorMigrationIsStableSentinel guards the exact message operators
// see in the two-step refusal, so the documentation (which quotes it verbatim)
// cannot drift from the implementation.
func TestErrSubFloorMigrationIsStableSentinel(t *testing.T) {
	t.Parallel()
	err := &ErrSubFloorMigration{Version: 30}
	msg := err.Error()
	assert.Contains(t, msg, "predates the supported upgrade floor (v0.6.0, migration 31)")
	assert.Contains(t, msg, "In-place upgrade is supported only from v0.6.0 and later")
	assert.Contains(t, msg, "Upgrade this instance to v0.6.0 first")
	assert.Contains(t, msg, "docs/upgrade-compatibility.md")
	// A value that IS at or above the floor is never constructed — the guard
	// returns nil for those — but the sentinel must still be comparable.
	assert.True(t, errors.As(err, new(*ErrSubFloorMigration)))
}
