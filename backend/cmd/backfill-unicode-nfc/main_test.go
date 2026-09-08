package main

import (
	"path/filepath"
	"testing"

	"mycorrhizal/database"

	"github.com/stretchr/testify/require"
)

// run() drives the CLI body against a real migrated database. The two-run
// shape pins both the happy path (migrations + at-rest init + backfill) and
// the idempotence gate: the second invocation finds the data_backfills ledger
// row migration 000052's backfill wrote and exits cleanly without re-scanning.
func TestRun_BackfillsOnceAndIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backfill-unicode-nfc.db")

	require.NoError(t, run(dbPath), "first run must migrate + backfill cleanly")
	require.NoError(t, run(dbPath), "second run must short-circuit on the ledger")

	// Prove the run really wrote the completion ledger (the backfill's own
	// semantics live in services/unicode_nfc_backfill_test.go; here we assert
	// the CLI's contract: running it leaves the once-gate set).
	db, err := database.InitDB(dbPath)
	require.NoError(t, err)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	var n int64
	require.NoError(t, db.Table("data_backfills").
		Where("name = ?", "contacts.unicode_nfc").Count(&n).Error)
	require.EqualValues(t, 1, n, "the CLI run must record its completion in data_backfills")
}

// TestRun_SurfacesOperatorErrors pins the two failure exits a CLI user can
// actually hit and that run() is responsible for surfacing: an unusable
// database path and a malformed DATA_ENCRYPTION_KEY.
func TestRun_SurfacesOperatorErrors(t *testing.T) {
	t.Run("unopenable database path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "no", "such", "dir", "x.db")
		err := run(missing)
		require.Error(t, err)
	})

	t.Run("malformed DATA_ENCRYPTION_KEY", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "badkey.db")
		// Create + migrate a valid db first so the run fails at key resolution,
		// not at database open.
		db, err := database.InitDB(dbPath)
		require.NoError(t, err)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		t.Setenv("DATA_ENCRYPTION_KEY", "not-a-valid-master-key")
		err = run(dbPath)
		require.Error(t, err)
	})
}
