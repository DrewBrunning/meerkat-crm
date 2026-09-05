package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationsAddSystemEvents covers 000038: the system_events table is
// created with its CHECK vocabularies and indexes, accepts a valid row,
// rejects an out-of-vocabulary event_type, and the down migration drops it
// cleanly. system_events holds only system-generated diagnostic data, so
// there is no existing-data-preservation concern — the table is new.
func TestMigrationsAddSystemEvents(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "sys-events.db")

	db, err := InitDB(dbPath)
	require.NoError(t, err, "full migration chain incl. 000038 must apply to an empty database")
	sqlDB, err := db.DB()
	require.NoError(t, err)

	var tableCount int64
	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='system_events'",
	).Scan(&tableCount))
	assert.Equal(t, int64(1), tableCount)

	var idxCount int64
	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_system_events_correlation_id'",
	).Scan(&idxCount))
	assert.Equal(t, int64(1), idxCount, "correlation_id index must exist for the timeline drill-down")

	// A valid row inserts.
	_, err = sqlDB.Exec(`
		INSERT INTO system_events (created_at, occurred_at, event_type, severity, component, correlation_id)
		VALUES (datetime('now'), datetime('now'), 'sync_completed', 'info', 'contact_sync', 'chain-1')`)
	require.NoError(t, err)

	// RunMigrations records its own migration_completed operational event
	// (issue #424) once 000038 has created the table — so a fresh InitDB that
	// applied the whole chain must have left one behind.
	var migRows int64
	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM system_events WHERE event_type = 'migration_completed' AND component = 'migration'",
	).Scan(&migRows))
	assert.Equal(t, int64(1), migRows, "InitDB's migration run must record a migration_completed event")

	// An out-of-vocabulary event_type is rejected by the CHECK constraint.
	_, err = sqlDB.Exec(`
		INSERT INTO system_events (created_at, occurred_at, event_type, severity)
		VALUES (datetime('now'), datetime('now'), 'totally_made_up', 'info')`)
	require.Error(t, err, "CHECK constraint must reject an unknown event_type token")

	// An out-of-vocabulary result is rejected too.
	_, err = sqlDB.Exec(`
		INSERT INTO system_events (created_at, occurred_at, event_type, severity, result)
		VALUES (datetime('now'), datetime('now'), 'job_completed', 'info', 'maybe')`)
	require.Error(t, err, "CHECK constraint must reject an unknown result token")

	require.NoError(t, sqlDB.Close())

	// Down drops the table. 000038 is no longer the migration tip — later
	// migrations sit on top — so roll each of them back first, then 000038's
	// own down migration.
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000051_device_grants
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000050_contacts_user_vcard_uid_index
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000049_integration_terminal_failures
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000048_job_execution_outcome
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000047_idempotency_keys
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000046_import_runs_add_source_formats
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000045_import_source_links
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000044_revision_tokens
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000043_storage_samples
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000042_import_runs
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000041_job_runs
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000040_alert_states
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000039_sync_health_fields
	require.NoError(t, MigrateDown(dbPath)) // rolls back 000038_system_events

	sqlDB2, err := sql.Open("sqlite", openDSN(dbPath))
	require.NoError(t, err)
	defer sqlDB2.Close()
	require.NoError(t, sqlDB2.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='system_events'",
	).Scan(&tableCount))
	assert.Equal(t, int64(0), tableCount, "the down migration must drop system_events")
}
