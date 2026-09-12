package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mycorrhizal/database"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/internal/sqlitecorrupt"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// migratedDBFile returns the path to a freshly migrated database file, with
// its dbtest connection already closed so the CLI opens its own.
func migratedDBFile(t *testing.T, seed func(db *gorm.DB)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doctor.db")
	db := dbtest.NewAt(t, path)
	if seed != nil {
		seed(db)
	}
	require.NoError(t, db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	return path
}

func seedUserContact(t *testing.T, db *gorm.DB) (models.User, models.Contact) {
	t.Helper()
	u := models.User{Username: "alice", Email: "a@example.com", Password: "x"}
	require.NoError(t, db.Create(&u).Error)
	c := models.Contact{UserID: u.ID, Firstname: "A"}
	require.NoError(t, db.Create(&c).Error)
	return u, c
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runCLI(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestDoctor_CleanDBExitsZero(t *testing.T) {
	path := migratedDBFile(t, func(db *gorm.DB) {
		u, c := seedUserContact(t, db)
		b := models.Contact{UserID: u.ID, Firstname: "B"}
		require.NoError(t, db.Create(&b).Error)
		require.NoError(t, db.Create(&models.RelationshipEdge{
			UserID: u.ID, SourceID: c.VCardUID, TargetID: b.VCardUID, Type: "friend_of",
			Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
			Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
		}).Error)
	})

	code, out, _ := run(t, "-db", path)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "result: OK")
}

func TestDoctor_ViolationExitsOneAndNamesInvariant(t *testing.T) {
	path := migratedDBFile(t, func(db *gorm.DB) {
		u, c := seedUserContact(t, db)
		require.NoError(t, db.Create(&models.RelationshipEdge{
			UserID: u.ID, SourceID: c.VCardUID, TargetID: "00000000-0000-4000-8000-000000000000",
			Type: "friend_of", Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
			Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
		}).Error)
	})

	code, out, _ := run(t, "-db", path)
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "INV-D1")
	assert.Contains(t, out, "relationship_edge.endpoint_missing")
	assert.Contains(t, out, "PROBLEMS FOUND")
}

func TestDoctor_JSONOutput(t *testing.T) {
	path := migratedDBFile(t, func(db *gorm.DB) {
		u, c := seedUserContact(t, db)
		require.NoError(t, db.Create(&models.RelationshipEdge{
			UserID: u.ID, SourceID: c.VCardUID, TargetID: "00000000-0000-4000-8000-000000000000",
			Type: "friend_of", Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
			Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
		}).Error)
	})

	code, out, _ := run(t, "-db", path, "-json")
	assert.Equal(t, 1, code)
	var res doctorResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.False(t, res.OK)
	require.NotEmpty(t, res.Data.Findings)
	assert.Equal(t, "INV-D1", res.Data.Findings[0].Invariant)
}

func TestDoctor_RepairDryRunMutatesNothing(t *testing.T) {
	var edgeID string
	path := migratedDBFile(t, func(db *gorm.DB) {
		u, c := seedUserContact(t, db)
		e := models.RelationshipEdge{
			UserID: u.ID, SourceID: c.VCardUID, TargetID: "00000000-0000-4000-8000-000000000000",
			Type: "friend_of", Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
			Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
		}
		require.NoError(t, db.Create(&e).Error)
		edgeID = e.ID
	})

	code, out, _ := run(t, "-db", path, "-repair")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "DRY RUN")
	assert.Contains(t, out, "would be deleted")
	assert.Contains(t, strings.ToLower(out), "re-run with -confirm")

	// The orphan edge is still there.
	assertRowCount(t, path, "relationship_edges", "id = ?", edgeID, 1)
}

func TestDoctor_RepairConfirmDeletesOrphans(t *testing.T) {
	path := migratedDBFile(t, func(db *gorm.DB) {
		u, c := seedUserContact(t, db)
		require.NoError(t, db.Create(&models.RelationshipEdge{
			UserID: u.ID, SourceID: c.VCardUID, TargetID: "00000000-0000-4000-8000-000000000000",
			Type: "friend_of", Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
			Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
		}).Error)
	})

	code, out, _ := run(t, "-db", path, "-repair", "-confirm")
	assert.Equal(t, 0, code)
	assert.NotContains(t, out, "DRY RUN")
	assert.Contains(t, out, "deleted")

	assertRowCount(t, path, "relationship_edges", "target_id = ?", "00000000-0000-4000-8000-000000000000", 0)

	// A follow-up detection run is now clean.
	code2, out2, _ := run(t, "-db", path)
	assert.Equal(t, 0, code2, out2)
}

// corruptDBWithOrphan builds a migrated database with enough data to span
// several pages, a truly-orphaned relationship edge that -repair would delete if
// it were allowed to run, and then corrupts one data page so the storage pass is
// not OK.
func corruptDBWithOrphan(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doctor-corrupt.db")
	db := dbtest.NewAt(t, path)

	u := models.User{Username: "doctorcorrupt", Email: "doctorcorrupt@example.com", Password: "x"}
	require.NoError(t, db.Create(&u).Error)
	for i := 0; i < 500; i++ {
		require.NoError(t, db.Create(&models.Note{
			UserID: u.ID, Content: "bulk note content padding pages for the corruption target", Date: time.Now(),
		}).Error)
	}
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: u.ID, SourceID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		TargetID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
		Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivityNormal,
	}).Error)
	require.NoError(t, db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	sqlitecorrupt.DataPage(t, path)
	return path
}

// TestDoctor_RepairRefusesCorruptDatabase is the issue #921 gap-2 pin: repair
// must not issue its destructive DELETEs against a structurally corrupt
// database. It refuses with the distinct exit code 3 in both dry-run and confirm
// modes, and the would-be-deleted orphan survives.
func TestDoctor_RepairRefusesCorruptDatabase(t *testing.T) {
	path := corruptDBWithOrphan(t)

	t.Run("dry run refuses", func(t *testing.T) {
		code, _, errOut := run(t, "-db", path, "-repair")
		assert.Equal(t, 3, code)
		assert.Contains(t, errOut, "refusing to repair")
	})

	t.Run("confirm refuses and leaves the orphan in place", func(t *testing.T) {
		code, _, errOut := run(t, "-db", path, "-repair", "-confirm")
		assert.Equal(t, 3, code)
		assert.Contains(t, errOut, "refusing to repair")

		// The destructive DELETE never ran.
		assertRowCount(t, path, "relationship_edges", "target_id = ?",
			"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 1)
	})

	t.Run("json refusal is machine-readable", func(t *testing.T) {
		code, out, _ := run(t, "-db", path, "-repair", "-json")
		assert.Equal(t, 3, code)
		var refusal repairRefusal
		require.NoError(t, json.Unmarshal([]byte(out), &refusal))
		assert.True(t, refusal.Refused)
		assert.False(t, refusal.OK)
		assert.Contains(t, refusal.Reason, "storage integrity")
	})
}

// TestDoctor_RepairRefusesWhenStorageProbeCannotRun covers the other refusal
// branch: gorm can open the file, but the integrity_check query itself aborts
// (structural-page corruption). Repair is still refused with exit code 3, just
// with the "could not run" message.
func TestDoctor_RepairRefusesWhenStorageProbeCannotRun(t *testing.T) {
	path := migratedDBFile(t, nil)
	sqlitecorrupt.Page(t, path, 2)

	code, _, errOut := run(t, "-db", path, "-repair", "-confirm")
	assert.Equal(t, 3, code)
	assert.Contains(t, errOut, "storage integrity check could not run")
}

func TestDoctor_UsageErrors(t *testing.T) {
	path := migratedDBFile(t, nil)

	t.Run("confirm without repair", func(t *testing.T) {
		code, _, errOut := run(t, "-db", path, "-confirm")
		assert.Equal(t, 2, code)
		assert.Contains(t, errOut, "-confirm has no effect without -repair")
	})

	t.Run("positional argument", func(t *testing.T) {
		code, _, errOut := run(t, path)
		assert.Equal(t, 2, code)
		assert.Contains(t, errOut, "unexpected argument")
	})

	t.Run("missing file", func(t *testing.T) {
		code, _, errOut := run(t, "-db", filepath.Join(t.TempDir(), "nope.db"))
		assert.Equal(t, 2, code)
		assert.Contains(t, errOut, "no such file")
	})
}

// assertRowCount opens its own connection to verify persisted state after the
// CLI has closed its handle.
func assertRowCount(t *testing.T, path, table, where string, arg interface{}, want int64) {
	t.Helper()
	db, err := database.OpenMigratedFile(path)
	require.NoError(t, err)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	var n int64
	// #nosec G201 -- table/where are test-controlled constants.
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM "+table+" WHERE "+where, arg).Scan(&n).Error)
	assert.Equal(t, want, n)
}
