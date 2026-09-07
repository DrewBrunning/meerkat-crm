package controllers

import (
	"testing"

	"mycorrhizal/config"
	"mycorrhizal/internal/canonicalfixture"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetContacts_NameSortsTheInternationalFixture is the I18N-01 (issue #484)
// sort-order flow-through: the canonical fixture's international records —
// CJK/RTL/mononym/compound-surname/tussenvoegsel names — are loaded into a real
// migrated database and the name-sorted GET /contacts paging walk must return
// every live fixture contact exactly once, in (sort_name, id) order, matching
// the database column. Sorting is driven by the dedicated contacts.sort_name
// field (derived from the structured surname, or given when there is none),
// never by display-name token order; a fixture contact whose international name
// does not reach sort_name breaks the walk here.
func TestGetContacts_NameSortsTheInternationalFixture(t *testing.T) {
	db := dbtest.New(t)
	m, err := canonicalfixture.Read()
	require.NoError(t, err)
	ds, err := canonicalfixture.Populate(db, m)
	require.NoError(t, err)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", ds.User.ID)
		c.Set("cfg", config.Config{ProfilePhotoDir: ""})
		c.Next()
	})
	router.GET("/contacts", GetContacts)

	all := walkNameSortedPages(t, router, "asc", 4)
	require.NotEmpty(t, all)

	// Live fixture contacts (the tombstoned gina is excluded by the deleted
	// scope) are all present exactly once across the paginated walk.
	seen := map[uint]bool{}
	var gotIDs []uint
	for _, it := range all {
		id := uint(it["id"].(float64))
		require.False(t, seen[id], "contact id %d appeared twice in the name-sorted walk", id)
		seen[id] = true
		gotIDs = append(gotIDs, id)
	}
	var live int64
	db.Model(&models.Contact{}).Where("user_id = ?", ds.User.ID).Count(&live)
	require.EqualValues(t, live, len(gotIDs), "the name-sorted walk must return every live fixture contact exactly once")

	// Every international record (issue #484) is in the walk.
	for _, name := range []string{"naoki", "wei", "minjun", "layla", "yael", "bjork", "carmen", "joao", "jan", "somchai", "priya", "aoife"} {
		assert.True(t, seen[ds.Contacts[name].ID], "international record %q must appear in the name-sorted list", name)
	}

	// The returned order is exactly the (sort_name, id) total order the
	// dedicated column drives — pagination over international names must not
	// reorder or drop a row at a page boundary.
	var want []models.Contact
	require.NoError(t, db.Where("user_id = ?", ds.User.ID).
		Order("sort_name ASC, id ASC").Find(&want).Error)
	require.Len(t, want, len(gotIDs))
	for i, c := range want {
		assert.Equalf(t, c.ID, gotIDs[i], "name-sorted walk position %d must match (sort_name, id) order", i)
	}
}
