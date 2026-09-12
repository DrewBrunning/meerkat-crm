package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationDedupesRelationshipEdges covers 000055 (issue #928): the
// migration removes pre-existing duplicate relationship edges before adding
// the natural-key unique index, the index then rejects a new duplicate, and
// the down migration drops the index without resurrecting the removed rows.
//
// The canonical fixture has no duplicates, so MIG-03's semantic suite sees
// this migration as pure DDL (see the .expect.yaml); this test seeds the
// duplicates directly.
func TestMigrationDedupesRelationshipEdges(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "relationship-edge-natural-key.db")
	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	require.NoError(t, err)
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB)
	require.NoError(t, err)
	// Everything up to but NOT including 000055, so the duplicates below
	// genuinely predate the index.
	require.NoError(t, m.Steps(54))

	_, err = sqlDB.Exec(
		"INSERT INTO users (created_at, updated_at, username, password, email) VALUES (datetime('now'), datetime('now'), 'edge-dedup', 'x', 'edge-dedup@example.com')")
	require.NoError(t, err)
	var userID int64
	require.NoError(t, sqlDB.QueryRow("SELECT id FROM users WHERE username = 'edge-dedup'").Scan(&userID))

	insertEdge := func(id, relType string, confidence float64, status, createdAt string) {
		t.Helper()
		_, err := sqlDB.Exec(`
			INSERT INTO relationship_edges
				(id, created_at, updated_at, user_id, source_id, target_id, type, directional, source, confidence, status, sensitivity)
			VALUES (?, ?, ?, ?, 'src-uid', 'tgt-uid', ?, 0, 'user-confirmed', ?, ?, 'normal')`,
			id, createdAt, createdAt, userID, relType, confidence, status)
		require.NoErrorf(t, err, "seeding edge %s", id)
	}

	// Four rows for the same natural key, deliberately out of authority order:
	// the survivor must be the confirmed 0.9 edge with the earliest
	// created_at (p4), not the seeded order.
	insertEdge("p1", "friend_of", 0.5, "confirmed", "2026-01-01 00:00:00")
	insertEdge("p2", "friend_of", 0.9, "suggested", "2026-01-02 00:00:00")
	insertEdge("p3", "friend_of", 0.9, "confirmed", "2026-01-04 00:00:00")
	insertEdge("p4", "friend_of", 0.9, "confirmed", "2026-01-03 00:00:00")
	// A different natural key is a different fact and must survive untouched.
	insertEdge("other", "coworker_of", 1.0, "confirmed", "2026-01-01 00:00:00")

	// Apply exactly 000055.
	require.NoError(t, m.Steps(1))

	var idxCount int64
	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_relationship_edges_natural_key'",
	).Scan(&idxCount))
	assert.Equal(t, int64(1), idxCount, "the natural-key unique index must exist after 000055")

	var survivors []string
	rows, err := sqlDB.Query("SELECT id FROM relationship_edges ORDER BY id")
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		survivors = append(survivors, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"other", "p4"}, survivors,
		"the dedup must keep the most authoritative duplicate (confirmed, higher confidence, earlier created_at) and the distinct edge")

	// The survivor's other data is untouched.
	var confidence float64
	var status, provenance string
	require.NoError(t, sqlDB.QueryRow(
		"SELECT confidence, status, source FROM relationship_edges WHERE id = 'p4'",
	).Scan(&confidence, &status, &provenance))
	assert.Equal(t, 0.9, confidence)
	assert.Equal(t, "confirmed", status)
	assert.Equal(t, "user-confirmed", provenance)

	// A fresh duplicate insert is rejected by the index.
	_, err = sqlDB.Exec(`
		INSERT INTO relationship_edges
			(id, created_at, updated_at, user_id, source_id, target_id, type, directional, source, confidence, status, sensitivity)
		VALUES ('replay', datetime('now'), datetime('now'), ?, 'src-uid', 'tgt-uid', 'friend_of', 0, 'user-confirmed', 1.0, 'confirmed', 'normal')`,
		userID)
	require.Error(t, err, "the natural-key index must reject a duplicate edge")
	assert.Contains(t, err.Error(), "UNIQUE constraint failed: relationship_edges")

	// The down migration drops the index. The rows it already removed stay
	// removed (their redundancy carried no unique information), but a new
	// duplicate is accepted again.
	require.NoError(t, MigrateDown(dbPath))

	require.NoError(t, sqlDB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_relationship_edges_natural_key'",
	).Scan(&idxCount))
	assert.Equal(t, int64(0), idxCount, "the down migration must drop the natural-key index")

	_, err = sqlDB.Exec(`
		INSERT INTO relationship_edges
			(id, created_at, updated_at, user_id, source_id, target_id, type, directional, source, confidence, status, sensitivity)
		VALUES ('after-down', datetime('now'), datetime('now'), ?, 'src-uid', 'tgt-uid', 'friend_of', 0, 'user-confirmed', 1.0, 'confirmed', 'normal')`,
		userID)
	require.NoError(t, err, "without the index a duplicate insert is accepted again")
}
