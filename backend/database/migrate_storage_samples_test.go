package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationsAddStorageSamples covers 000043: the storage_samples table is
// created with its taken_at index, accepts a valid row, and the down migration
// drops it cleanly on a populated database. storage_samples holds only
// timestamps + byte counts for the daily storage sampler (issue #652), so
// there is no existing-data-preservation concern — the table is new.
func TestMigrationsAddStorageSamples(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "storage-samples.db")

	db, err := InitDB(dbPath)
	require.NoError(t, err, "full migration chain incl. 000043 must apply to an empty database")
	sqlDB, err := db.DB()
	require.NoError(t, err)

	var tableCount int64
	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='storage_samples'",
	).Scan(&tableCount))
	assert.Equal(t, int64(1), tableCount)

	var idxCount int64
	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_storage_samples_taken_at'",
	).Scan(&idxCount))
	assert.Equal(t, int64(1), idxCount, "the taken_at index must exist for the windowed trend query")

	// A valid row inserts; defaults fill the per-directory byte columns.
	_, err = sqlDB.Exec(`
		INSERT INTO storage_samples (taken_at, database_bytes, fs_used_bytes, fs_total_bytes)
		VALUES (datetime('now', '-1 day'), 1048576, 53687091200, 107374182400)`)
	require.NoError(t, err)

	var rowCount int64
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM storage_samples").Scan(&rowCount))
	assert.Equal(t, int64(1), rowCount)

	require.NoError(t, sqlDB.Close())

	// Down drops the table. 000043 is no longer the migration tip — 000044
	// (revision tokens) and 000045 (import source links) sit on top — so roll
	// those back first, then 000043's own down migration.
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000052_data_backfills
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000051_device_grants
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000050_contacts_user_vcard_uid_index
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000049_integration_terminal_failures
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000048_job_execution_outcome
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000047_idempotency_keys
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000046_import_runs_add_source_formats
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000045_import_source_links
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000044_revision_tokens
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000043_storage_samples

	sqlDB2, err := sql.Open("sqlite", openDSN(dbPath))
	require.NoError(t, err)
	defer sqlDB2.Close()
	require.NoError(t, sqlDB2.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='storage_samples'",
	).Scan(&tableCount))
	assert.Equal(t, int64(0), tableCount, "the down migration must drop storage_samples")
}
