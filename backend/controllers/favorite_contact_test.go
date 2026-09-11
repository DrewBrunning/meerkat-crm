package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FavoriteContact / UnfavoriteContact (issue #173) ------------------------
//
// Mirrors the ArchiveContact/UnarchiveContact tests in
// uncovered_endpoints_test.go: flag toggling, 404 on unknown id, ownership
// scoping (IDOR guard), the change-feed/ETag propagation, and a real-schema
// persistence check.

func TestFavoriteContact_SetsFlag(t *testing.T) {
	db, router := setupRouter()
	router.POST("/contacts/:id/favorite", FavoriteContact)

	var user models.User
	require.NoError(t, db.First(&user).Error)

	contact := models.Contact{UserID: user.ID, Firstname: "Star"}
	require.NoError(t, db.Create(&contact).Error)

	req, _ := http.NewRequest("POST", "/contacts/"+idString(contact.ID)+"/favorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.True(t, reloaded.IsFavorite)
}

func TestUnfavoriteContact_ClearsFlag(t *testing.T) {
	db, router := setupRouter()
	router.POST("/contacts/:id/unfavorite", UnfavoriteContact)

	var user models.User
	require.NoError(t, db.First(&user).Error)

	contact := models.Contact{UserID: user.ID, Firstname: "Star", IsFavorite: true}
	require.NoError(t, db.Create(&contact).Error)

	req, _ := http.NewRequest("POST", "/contacts/"+idString(contact.ID)+"/unfavorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.False(t, reloaded.IsFavorite)
}

func TestFavoriteContact_UnknownID_404(t *testing.T) {
	_, router := setupRouter()
	router.POST("/contacts/:id/favorite", FavoriteContact)

	req, _ := http.NewRequest("POST", "/contacts/999999/favorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUnfavoriteContact_UnknownID_404(t *testing.T) {
	_, router := setupRouter()
	router.POST("/contacts/:id/unfavorite", UnfavoriteContact)

	req, _ := http.NewRequest("POST", "/contacts/999999/unfavorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestFavoriteContact_ScopedToOwner(t *testing.T) {
	db, router := setupRouter()
	router.POST("/contacts/:id/favorite", FavoriteContact)

	other := models.User{Username: "other-favorite", Email: "other-favorite@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)
	theirs := models.Contact{UserID: other.ID, Firstname: "Not Yours"}
	require.NoError(t, db.Create(&theirs).Error)

	req, _ := http.NewRequest("POST", "/contacts/"+idString(theirs.ID)+"/favorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, theirs.ID).Error)
	assert.False(t, reloaded.IsFavorite, "another user's contact must not be favoritable")
}

func TestUnfavoriteContact_ScopedToOwner(t *testing.T) {
	db, router := setupRouter()
	router.POST("/contacts/:id/unfavorite", UnfavoriteContact)

	other := models.User{Username: "other-unfavorite", Email: "other-unfavorite@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)
	theirs := models.Contact{UserID: other.ID, Firstname: "Not Yours", IsFavorite: true}
	require.NoError(t, db.Create(&theirs).Error)

	req, _ := http.NewRequest("POST", "/contacts/"+idString(theirs.ID)+"/unfavorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestFavoriteContact_BumpsETagAndChangeFeed pins the hook-firing choice: a
// favorite flip must bump the ETag (so CardDAV sync sees a change) and
// surface in the T17 ?since= change feed — a favorite is a change of the
// contact, and a replica needs to see it. This is exactly why the handlers
// use `Update` rather than `UpdateColumn` (which skips hooks).
func TestFavoriteContact_BumpsETagAndChangeFeed(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.POST("/contacts/:id/favorite", FavoriteContact)
	router.POST("/contacts/:id/unfavorite", UnfavoriteContact)
	router.GET("/contacts", GetContacts)

	contact := models.Contact{UserID: user.ID, Firstname: "Feed"}
	require.NoError(t, db.Create(&contact).Error)
	// Overwrite the stored etag with a sentinel so the assertion below can
	// tell "recomputed" apart from "already had this value" — UpdateColumn
	// skips hooks, so this is otherwise inert. The favorite's hook-firing
	// Update must then recompute it to the real value (CON-01, ADR 0018:
	// etag is a monotonic-revision-derived, not updated_at-derived, token —
	// see Contact.AfterSave), which is exactly the mechanism this test pins.
	// db.Table(...), not db.Model(&contact): etag is tagged `<-:create`
	// (ADR 0018), so a Model-scoped statement would have this test's own
	// sentinel write filtered out by GORM's field-permission check.
	require.NoError(t, db.Table("contacts").Where("id = ?", contact.ID).UpdateColumn("etag", "e-stale-sentinel").Error)
	require.NoError(t, db.First(&contact, contact.ID).Error)
	etagBefore := contact.ETag
	require.Equal(t, "e-stale-sentinel", etagBefore)

	// Cursor positioned at the contact's creation.
	cursor := EncodeCursor(contact.UpdatedAt, contact.ID)

	req, _ := http.NewRequest("POST", "/contacts/"+idString(contact.ID)+"/favorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.True(t, reloaded.IsFavorite)
	assert.NotEqual(t, etagBefore, reloaded.ETag, "favoriting must bump the ETag (hooks fire)")

	feed := getContactsPage(t, router, "since="+cursor+"&limit=10")
	require.Len(t, feed.Contacts, 1, "the favorite flip must appear in the change feed")
	assert.Equal(t, "Feed", feed.Contacts[0]["firstname"])
	assert.Equal(t, true, feed.Contacts[0]["is_favorite"], "the feed row must carry the updated flag")
}

// TestFavoriteFlag_RealMigratedSchema is the real-DB check for the recurring
// GORM-vs-migration column-name drift trap (CLAUDE.md backend trap 1): the
// `is_favorite` column must exist with exactly that name in the hand-written
// migration SQL, and the flag must round-trip through a real
// database.InitDB-migrated file DB.
func TestFavoriteFlag_RealMigratedSchema(t *testing.T) {
	db := dbtest.New(t)

	user := models.User{Username: "realdb-fav", Password: "password123!A", Email: "realdb-fav@example.com"}
	require.NoError(t, db.Create(&user).Error)
	contact := models.Contact{UserID: user.ID, Firstname: "Real", IsFavorite: true}
	require.NoError(t, db.Create(&contact).Error)

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.True(t, reloaded.IsFavorite, "is_favorite must persist through the real migrated schema")

	// Unfavorite back and confirm it round-trips too.
	require.NoError(t, db.Model(&reloaded).Update("is_favorite", false).Error)
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.False(t, reloaded.IsFavorite)
}

// --- GET /contacts?favorites=true ---------------------------------------------

func TestGetContacts_FavoritesFilter(t *testing.T) {
	db, router := setupRouter()
	router.GET("/contacts", GetContacts)

	var user models.User
	require.NoError(t, db.First(&user).Error)

	fav := models.Contact{UserID: user.ID, Firstname: "Fav", IsFavorite: true}
	plain := models.Contact{UserID: user.ID, Firstname: "Plain"}
	archivedFav := models.Contact{UserID: user.ID, Firstname: "ArchFav", IsFavorite: true, Archived: true}
	require.NoError(t, db.Create(&fav).Error)
	require.NoError(t, db.Create(&plain).Error)
	require.NoError(t, db.Create(&archivedFav).Error)

	req, _ := http.NewRequest("GET", "/contacts?favorites=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Contacts []models.ContactSummary `json:"contacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Contacts, 1, "only the live favorite should match")
	assert.Equal(t, "Fav", resp.Contacts[0].Firstname)
	assert.True(t, resp.Contacts[0].IsFavorite)
}

// TestGetContactsSummary_AlwaysSerializesIsFavorite asserts the raw JSON: a
// non-favorite contact must still carry `"is_favorite":false` on the wire (no
// omitempty) — decoding into the Go struct makes absent and false
// indistinguishable (CLAUDE.md frontend trap 8), so this reads the raw bytes.
func TestGetContactsSummary_AlwaysSerializesIsFavorite(t *testing.T) {
	db, router := setupRouter()
	router.GET("/contacts", GetContacts)

	var user models.User
	require.NoError(t, db.First(&user).Error)
	contact := models.Contact{UserID: user.ID, Firstname: "Plain"}
	require.NoError(t, db.Create(&contact).Error)

	req, _ := http.NewRequest("GET", "/contacts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, `"is_favorite":false`, "a non-favorite must serialize is_favorite:false, not omit it")
	assert.False(t, strings.Contains(body, `"is_favorite":null`))
}

// TestGetContacts_FavoritesFilterComposesWithArchived pins that favorites
// composes with the other predicates like every other filter — a user asking
// for favorites while browsing archived contacts gets their archived
// favorites, not an empty list.
func TestGetContacts_FavoritesFilterComposesWithArchived(t *testing.T) {
	db, router := setupRouter()
	router.GET("/contacts", GetContacts)

	var user models.User
	require.NoError(t, db.First(&user).Error)

	liveFav := models.Contact{UserID: user.ID, Firstname: "Live", IsFavorite: true}
	archivedFav := models.Contact{UserID: user.ID, Firstname: "Arch", IsFavorite: true, Archived: true}
	require.NoError(t, db.Create(&liveFav).Error)
	require.NoError(t, db.Create(&archivedFav).Error)

	req, _ := http.NewRequest("GET", "/contacts?favorites=true&archived=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Contacts []models.ContactSummary `json:"contacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Contacts, 1)
	assert.Equal(t, "Arch", resp.Contacts[0].Firstname)
}

// TestGetContacts_FavoritesFilterComposesWithSearch pins favorites composing
// with search: both predicates are ANDed, so a search that matches a
// non-favorite returns nothing when favorites=true.
func TestGetContacts_FavoritesFilterComposesWithSearch(t *testing.T) {
	db, router := setupRouter()
	router.GET("/contacts", GetContacts)

	var user models.User
	require.NoError(t, db.First(&user).Error)

	fav := models.Contact{UserID: user.ID, Firstname: "Alice", Lastname: "Fav", IsFavorite: true}
	plain := models.Contact{UserID: user.ID, Firstname: "Alice", Lastname: "Plain"}
	require.NoError(t, db.Create(&fav).Error)
	require.NoError(t, db.Create(&plain).Error)

	req, _ := http.NewRequest("GET", "/contacts?favorites=true&search=Alice", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Contacts []models.ContactSummary `json:"contacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Contacts, 1, "search+ favorites must AND: only the matching favorite")
	assert.Equal(t, "Fav", resp.Contacts[0].Lastname)
	assert.True(t, resp.Contacts[0].IsFavorite)
}

// TestChangeFeedIgnoresFavoritesFilter pins the contract documented in
// openapi.yaml: ?favorites=true is a browsing lens and must be ignored by the
// ?since= change feed, which is sync state carrying every row regardless of
// filters. A replica that saw a favorite-only feed would silently diverge.
func TestChangeFeedIgnoresFavoritesFilter(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/contacts", GetContacts)
	router.POST("/contacts/:id/favorite", FavoriteContact)

	// Cursor positioned before both contacts exist.
	cursor := EncodeCursor(time.Now().Add(-time.Second), uint(0))

	fav := models.Contact{UserID: user.ID, Firstname: "FavA"}
	plain := models.Contact{UserID: user.ID, Firstname: "PlainB"}
	require.NoError(t, db.Create(&fav).Error)
	require.NoError(t, db.Create(&plain).Error)

	// Favorite only fav; PlainB stays a non-favorite.
	req, _ := http.NewRequest("POST", "/contacts/"+idString(fav.ID)+"/favorite", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	feed := getContactsPage(t, router, "since="+cursor+"&favorites=true&limit=10")
	require.Len(t, feed.Contacts, 2, "favorites must be ignored by the change feed — a replica needs every row")
	names := map[string]bool{}
	for _, raw := range feed.Contacts {
		names[raw["firstname"].(string)] = true
	}
	assert.True(t, names["FavA"])
	assert.True(t, names["PlainB"])
}
