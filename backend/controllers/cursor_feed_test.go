package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupRouterWithRetention is setupRouter (activity_controller_test.go) with a
// non-zero DELETE_RETENTION_DAYS so ?since= feed cursors are not all rejected
// as too old. The T17 feed tests need a real retention window; the default
// test router's config has zero.
func setupRouterWithRetention(retentionDays int) (*gorm.DB, *gin.Engine) {
	gin.SetMode(gin.ReleaseMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	db.AutoMigrate(&models.Contact{}, &models.Activity{}, &models.Note{}, models.Reminder{}, models.User{}, models.Webhook{}, models.WebhookDelivery{}, models.ContactSubscription{}, models.ContactSyncLink{}, models.RelationshipEdge{}, models.Circle{}, models.CircleMember{}, models.Tag{}, models.ContactTag{}, models.LifeEvent{}, models.Household{}, models.HouseholdMember{}, models.FieldDefinition{}, models.FieldValue{}, models.CardDAVSync{}, models.ApiToken{}, models.DeviceGrant{}, models.ReminderCompletion{}, models.CalendarSubscription{}, models.CalendarEventLink{}, models.Preference{}, models.CadencePolicy{}, models.ConversationAgenda{}, models.Gift{}, models.ReachOutSuggestion{}, models.ReachOutCursor{})

	user := models.User{Username: "tester", Password: "password123", Email: "tester@example.com"}
	if err := db.Create(&user).Error; err != nil {
		panic("failed to seed user")
	}

	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", user.ID)
		c.Set("cfg", config.Config{ProfilePhotoDir: os.Getenv("PROFILE_PHOTO_DIR"), DeleteRetentionDays: retentionDays})
		c.Next()
	})

	return db, router
}

// contactsResponse is the slim decoder the feed tests use to read the T17
// cursor envelope (there is no total/page anymore).
type contactsResponse struct {
	Contacts   []map[string]any `json:"contacts"`
	NextCursor string           `json:"next_cursor"`
	Limit      int              `json:"limit"`
	Sync       map[string]any   `json:"sync"`
}

func getContactsPage(t *testing.T, router *gin.Engine, query string) contactsResponse {
	t.Helper()
	req, _ := http.NewRequest("GET", "/contacts?"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp contactsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestNameCursorEncodeDecodeRoundTrip proves a T73 name-sort cursor survives
// the base64url round trip with both the sort name and id intact, for both
// PK types the contacts list actually uses.
func TestNameCursorEncodeDecodeRoundTrip(t *testing.T) {
	for _, id := range []any{uint(7), uint64(42)} {
		raw := EncodeNameCursor("smith", id)
		cur, err := DecodeNameCursor(raw)
		require.NoError(t, err)
		assert.Equal(t, "smith", cur.SortName, "sort name must survive the round trip")
		assert.Equal(t, fmt.Sprint(id), cur.ID, "id must survive the round trip")
	}

	// A sort name containing the "|" delimiter must still round-trip: the id
	// is always the trailing numeric component, so the split must be on the
	// LAST delimiter, not the first.
	raw := EncodeNameCursor("a|b|c", uint(9))
	cur, err := DecodeNameCursor(raw)
	require.NoError(t, err)
	assert.Equal(t, "a|b|c", cur.SortName, "a sort name containing the delimiter must round-trip intact")
	assert.Equal(t, "9", cur.ID)
}

// TestNameCursorDecodeRejectsMalformed pins the T73 400 path for the name
// cursor: garbage, truncated base64url, a missing id, and — the cross-shape
// case — a valid TIME cursor must all fail decode, so a cursor minted by an
// updated_at-sorted request can never be silently misapplied under sort=name.
func TestNameCursorDecodeRejectsMalformed(t *testing.T) {
	timeCursor := EncodeCursor(time.Date(2026, 8, 2, 18, 7, 19, 0, time.UTC), uint(7))
	for _, raw := range []string{
		"",
		"!!!not-base64url!!!",
		encodeRawURL("smith|"), // missing id
		"aGVsbG8=",             // decodes but has no | separator
		timeCursor,             // a time-based cursor is the wrong shape
	} {
		_, err := DecodeNameCursor(raw)
		assert.Error(t, err, "cursor %q should fail to decode", raw)
	}
}

// TestCursorEncodeDecodeRoundTrip proves a cursor survives the base64url
// round trip with both timestamp and id intact, and that the timestamp keeps
// its offset (the DATETIME-as-text comparison depends on the bound value
// matching the stored format — see EncodeCursor's doc comment).
func TestCursorEncodeDecodeRoundTrip(t *testing.T) {
	for _, id := range []any{uint(7), uint64(42), "1f2e3d4c-5b6a-7890-abcd-ef1234567890"} {
		ts := time.Date(2026, 8, 2, 18, 7, 19, 402476499, time.FixedZone("CDT", -5*3600))
		raw := EncodeCursor(ts, id)
		cur, err := DecodeCursor(raw)
		require.NoError(t, err)
		assert.True(t, ts.Equal(cur.UpdatedAt), "timestamp must survive the round trip")
		_, off := cur.UpdatedAt.Zone()
		assert.Equal(t, -5*3600, off, "timestamp offset must survive the round trip")
		assert.Equal(t, fmt.Sprint(id), cur.ID, "id must survive the round trip")
	}
}

// TestCursorDecodeRejectsMalformed pins the 400 path: garbage, truncated
// base64url, missing id, and a non-timestamp payload must all fail decode.
func TestCursorDecodeRejectsMalformed(t *testing.T) {
	for _, raw := range []string{
		"",
		"!!!not-base64url!!!",
		encodeRawURL("2026-08-02T18:07:19.402476499-05:00|"), // missing id
		encodeRawURL("not-a-time|7"),                         // bad timestamp
		"aGVsbG8=",                                           // decodes but has no | separator
	} {
		_, err := DecodeCursor(raw)
		assert.Error(t, err, "cursor %q should fail to decode", raw)
	}
}

// encodeRawURL is a tiny mirror of EncodeCursor's encoding for test inputs.
func encodeRawURL(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

// TestGetContactsCursorStableUnderBoundaryInsert is the T17 stability test —
// the bug the (updated_at, id) cursor scheme exists to prevent. Six contacts
// are paged through with limit=2; a seventh is inserted AT the page boundary
// mid-walk (between the last row of page 1 and the first row of page 2).
// Cursor pagination must still return every row exactly once — no drops, no
// duplicates.
func TestGetContactsCursorStableUnderBoundaryInsert(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/contacts", GetContacts)

	for _, name := range []string{"A", "B", "C", "D", "E", "F"} {
		c := models.Contact{UserID: user.ID, Firstname: name}
		require.NoError(t, db.Create(&c).Error)
	}

	// Page 1: newest two by (updated_at, id) DESC.
	page1 := getContactsPage(t, router, "limit=2")
	require.Len(t, page1.Contacts, 2)
	require.NotEmpty(t, page1.NextCursor, "a full page must carry a next_cursor")

	// Decode page 1's last row position and insert a new contact just after
	// it in the ordering — i.e. exactly at the page boundary.
	boundary := page1.Contacts[1]
	boundaryID := uint(boundary["id"].(float64))
	var last models.Contact
	require.NoError(t, db.First(&last, boundaryID).Error)

	inserted := models.Contact{UserID: user.ID, Firstname: "G"}
	require.NoError(t, db.Create(&inserted).Error)
	boundaryTime := last.UpdatedAt.Add(-time.Nanosecond)
	require.NoError(t, db.Model(&inserted).Update("updated_at", boundaryTime).Error)

	// Walk every page following next_cursor and collect ids.
	seen := map[uint]int{}
	next := ""
	for page := 0; ; page++ {
		query := "limit=2"
		if next != "" {
			query += "&cursor=" + next
		}
		resp := getContactsPage(t, router, query)
		for _, raw := range resp.Contacts {
			seen[uint(raw["id"].(float64))]++
		}
		if resp.NextCursor == "" {
			break
		}
		next = resp.NextCursor
		if page > 10 {
			t.Fatal("cursor pagination did not terminate")
		}
	}

	// All seven contacts, each exactly once.
	assert.Len(t, seen, 7, "every contact must appear")
	for id, count := range seen {
		assert.Equal(t, 1, count, "contact %d must not be dropped or duplicated", id)
	}
}

// TestChangeFeedSinceAndTombstones is the change-feed contract test: ?since=
// returns only rows changed after the cursor, ordered forward, AND a
// soft-deleted row is returned as a deletion (deleted:true) rather than
// silently vanishing.
func TestChangeFeedSinceAndTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/contacts", GetContacts)

	// Seed three contacts, then archive one (archived rows must still appear
	// in the feed — a replica needs the whole picture). Archiving bumps
	// Alice's updated_at past Bob/Carol's.
	a := models.Contact{UserID: user.ID, Firstname: "Alice"}
	b := models.Contact{UserID: user.ID, Firstname: "Bob"}
	c := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	require.NoError(t, db.Create(&c).Error)
	require.NoError(t, db.Model(&a).Update("archived", true).Error)

	// Cursor positioned after Carol (the last-created row).
	afterCarol := EncodeCursor(c.UpdatedAt, c.ID)

	// The feed must be strictly after the cursor: only Alice's archive
	// update. Bob is before the cursor; Carol is the cursor row itself.
	feed := getContactsPage(t, router, "since="+afterCarol+"&limit=10")
	require.Len(t, feed.Contacts, 1, "only the row changed after the cursor should appear")
	assert.Equal(t, "Alice", feed.Contacts[0]["firstname"])
	assert.Equal(t, true, feed.Contacts[0]["archived"], "archived contacts must appear in the feed")
	assert.Equal(t, "incremental", feed.Sync["mode"])

	// Soft-delete Carol, then replay the same since: Carol must come back as
	// a deletion, not vanish. Contact.AfterDelete bumps her updated_at past
	// the cursor so the tombstone actually lands in the feed.
	require.NoError(t, db.Delete(&c).Error)
	feed2 := getContactsPage(t, router, "since="+afterCarol+"&limit=10")
	require.Len(t, feed2.Contacts, 2, "Alice's archive update and Carol's tombstone both changed after the cursor")
	byName := map[string]map[string]any{}
	for _, raw := range feed2.Contacts {
		byName[raw["firstname"].(string)] = raw
	}
	assert.Equal(t, true, byName["Carol"]["deleted"], "soft-deleted rows must be returned with deleted:true")
	assert.NotEqual(t, true, byName["Alice"]["deleted"], "a live row must not be marked deleted")
}

// TestChangeFeedCursorTooOld410 pins the sync-horizon contract (T17/T26): a
// ?since= cursor older than DELETED_RETENTION_DAYS returns 410 Gone telling
// the client to full-resync, because tombstones in that range were purged.
func TestChangeFeedCursorTooOld410(t *testing.T) {
	_, router := setupRouterWithRetention(30)
	router.GET("/contacts", GetContacts)

	tooOld := EncodeCursor(time.Now().AddDate(0, 0, -40), uint(1))
	req, _ := http.NewRequest("GET", "/contacts?since="+tooOld, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusGone, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errBody := body["error"].(map[string]any)
	assert.Equal(t, "GONE", errBody["code"])
	assert.Contains(t, errBody["message"].(string), "full resync")

	// A plain ?cursor= (browse navigation, not long-lived sync state) is NOT
	// subject to the horizon — no 410.
	browseReq, _ := http.NewRequest("GET", "/contacts?cursor="+tooOld, nil)
	browseW := httptest.NewRecorder()
	router.ServeHTTP(browseW, browseReq)
	require.Equal(t, http.StatusOK, browseW.Code)
}

// TestChangeFeedMalformedCursor400 pins that a garbage ?since= is a 400, not
// a 500.
func TestChangeFeedMalformedCursor400(t *testing.T) {
	_, router := setupRouterWithRetention(30)
	router.GET("/contacts", GetContacts)

	req, _ := http.NewRequest("GET", "/contacts?since=!!!not-a-cursor!!!", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestChangeFeedNotesTombstones covers the same tombstone trap for the notes
// feed: a soft-deleted note must surface via ?since= as deleted:true (Note's
// AfterDelete hook advances updated_at so the cursor moves past it).
func TestChangeFeedNotesTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/notes", GetUnassignedNotes)

	n1 := models.Note{UserID: user.ID, Content: "one"}
	n2 := models.Note{UserID: user.ID, Content: "two"}
	require.NoError(t, db.Create(&n1).Error)
	require.NoError(t, db.Create(&n2).Error)

	// Cursor positioned after the first note.
	afterOne := EncodeCursor(n1.UpdatedAt, n1.ID)

	req := func(query string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("GET", "/notes?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	feed := req("since=" + afterOne + "&limit=10")
	notes := feed["notes"].([]any)
	require.Len(t, notes, 1, "only the note after the cursor should appear")
	assert.Equal(t, "two", notes[0].(map[string]any)["content"])
	assert.Equal(t, "incremental", feed["sync"].(map[string]any)["mode"])

	// Soft-delete the second note, replay the same since: it comes back as a
	// deletion.
	require.NoError(t, db.Delete(&n2).Error)
	feed2 := req("since=" + afterOne + "&limit=10")
	notes2 := feed2["notes"].([]any)
	require.Len(t, notes2, 1)
	assert.Equal(t, "two", notes2[0].(map[string]any)["content"])
	assert.Equal(t, true, notes2[0].(map[string]any)["deleted"], "soft-deleted notes must be returned with deleted:true")
}

// TestChangeFeedActivitiesTombstones covers the tombstone trap for the
// activities feed: a soft-deleted activity must surface via ?since= as
// deleted:true (Activity.AfterDelete bumps updated_at so the cursor
// sees it).
func TestChangeFeedActivitiesTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/activities", GetActivities)

	a1 := models.Activity{UserID: user.ID, Title: "one", Date: time.Now()}
	a2 := models.Activity{UserID: user.ID, Title: "two", Date: time.Now()}
	require.NoError(t, db.Create(&a1).Error)
	require.NoError(t, db.Create(&a2).Error)

	afterOne := EncodeCursor(a1.UpdatedAt, a1.ID)

	req := func(query string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("GET", "/activities?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	feed := req("since=" + afterOne + "&limit=10")
	activities := feed["activities"].([]any)
	require.Len(t, activities, 1, "only the activity after the cursor should appear")
	assert.Equal(t, "two", activities[0].(map[string]any)["title"])
	assert.Equal(t, "incremental", feed["sync"].(map[string]any)["mode"])

	require.NoError(t, db.Delete(&a2).Error)
	feed2 := req("since=" + afterOne + "&limit=10")
	activities2 := feed2["activities"].([]any)
	require.Len(t, activities2, 1)
	assert.Equal(t, "two", activities2[0].(map[string]any)["title"])
	assert.Equal(t, true, activities2[0].(map[string]any)["deleted"], "soft-deleted activities must be returned with deleted:true")
}

// TestChangeFeedLifeEventsTombstones covers the tombstone trap for the
// life-events feed: a soft-deleted LifeEvent must surface via ?since= as
// deleted:true (LifeEvent.AfterDelete bumps updated_at so the cursor
// sees it).
func TestChangeFeedLifeEventsTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/life-events", ListLifeEvents)

	subject := models.Contact{UserID: user.ID, Firstname: "Subject"}
	require.NoError(t, db.Create(&subject).Error)

	le1 := models.LifeEvent{UserID: user.ID, EntityID: subject.VCardUID, Type: models.LifeEventTypeMarried}
	le2 := models.LifeEvent{UserID: user.ID, EntityID: subject.VCardUID, Type: models.LifeEventTypeGraduated}
	require.NoError(t, db.Create(&le1).Error)
	require.NoError(t, db.Create(&le2).Error)

	afterOne := EncodeCursor(le1.UpdatedAt, le1.ID)

	req := func(query string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("GET", "/life-events?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	feed := req("since=" + afterOne + "&limit=10")
	events := feed["life_events"].([]any)
	require.Len(t, events, 1, "only the life event after the cursor should appear")
	assert.Equal(t, models.LifeEventTypeGraduated, events[0].(map[string]any)["type"])
	assert.Equal(t, "incremental", feed["sync"].(map[string]any)["mode"])

	require.NoError(t, db.Delete(&le2).Error)
	feed2 := req("since=" + afterOne + "&limit=10")
	events2 := feed2["life_events"].([]any)
	require.Len(t, events2, 1)
	assert.Equal(t, models.LifeEventTypeGraduated, events2[0].(map[string]any)["type"])
	assert.Equal(t, true, events2[0].(map[string]any)["deleted"], "soft-deleted life events must be returned with deleted:true")
}

// TestChangeFeedPreferencesTombstones covers the tombstone trap for the
// preferences feed: a soft-deleted Preference must surface via ?since= as
// deleted:true (Preference.AfterDelete bumps updated_at so the cursor
// sees it).
func TestChangeFeedPreferencesTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/preferences", ListPreferences)

	subject := models.Contact{UserID: user.ID, Firstname: "Subject"}
	require.NoError(t, db.Create(&subject).Error)

	p1 := models.Preference{UserID: user.ID, EntityID: subject.VCardUID, Category: models.PreferenceCategoryFood, Value: "pizza"}
	p2 := models.Preference{UserID: user.ID, EntityID: subject.VCardUID, Category: models.PreferenceCategoryFood, Value: "sushi"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)

	afterOne := EncodeCursor(p1.UpdatedAt, p1.ID)

	req := func(query string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("GET", "/preferences?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	feed := req("since=" + afterOne + "&limit=10")
	prefs := feed["preferences"].([]any)
	require.Len(t, prefs, 1, "only the preference after the cursor should appear")
	assert.Equal(t, "sushi", prefs[0].(map[string]any)["value"])
	assert.Equal(t, "incremental", feed["sync"].(map[string]any)["mode"])
	// Feed mode deliberately omits total.
	assert.NotContains(t, feed, "total")

	require.NoError(t, db.Delete(&p2).Error)
	feed2 := req("since=" + afterOne + "&limit=10")
	prefs2 := feed2["preferences"].([]any)
	require.Len(t, prefs2, 1)
	assert.Equal(t, "sushi", prefs2[0].(map[string]any)["value"])
	assert.Equal(t, true, prefs2[0].(map[string]any)["deleted"], "soft-deleted preferences must be returned with deleted:true")
}

// TestChangeFeedConversationAgendaTombstones covers the tombstone trap for the
// conversation-agenda feed: a soft-deleted ConversationAgenda must surface via
// ?since= as deleted:true (ConversationAgenda.AfterDelete bumps updated_at so
// the cursor sees it).
func TestChangeFeedConversationAgendaTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/conversation-agenda", ListConversationAgenda)

	subject := models.Contact{UserID: user.ID, Firstname: "Subject"}
	require.NoError(t, db.Create(&subject).Error)

	a1 := models.ConversationAgenda{UserID: user.ID, EntityID: subject.VCardUID, Content: "Ask about the garden"}
	a2 := models.ConversationAgenda{UserID: user.ID, EntityID: subject.VCardUID, Content: "Ask about the new job"}
	require.NoError(t, db.Create(&a1).Error)
	require.NoError(t, db.Create(&a2).Error)

	afterOne := EncodeCursor(a1.UpdatedAt, a1.ID)

	req := func(query string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("GET", "/conversation-agenda?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	feed := req("since=" + afterOne + "&limit=10")
	items := feed["conversation_agenda"].([]any)
	require.Len(t, items, 1, "only the agenda item after the cursor should appear")
	assert.Equal(t, "Ask about the new job", items[0].(map[string]any)["content"])
	assert.Equal(t, "incremental", feed["sync"].(map[string]any)["mode"])

	require.NoError(t, db.Delete(&a2).Error)
	feed2 := req("since=" + afterOne + "&limit=10")
	items2 := feed2["conversation_agenda"].([]any)
	require.Len(t, items2, 1)
	assert.Equal(t, "Ask about the new job", items2[0].(map[string]any)["content"])
	assert.Equal(t, true, items2[0].(map[string]any)["deleted"], "soft-deleted agenda items must be returned with deleted:true")
}

// TestChangeFeedGiftTombstones covers the tombstone trap for the gifts feed:
// a soft-deleted Gift must surface via ?since= as deleted:true (Gift.AfterDelete
// bumps updated_at so the cursor sees it), exactly like the agenda items above.
func TestChangeFeedGiftTombstones(t *testing.T) {
	db, router := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)
	router.GET("/gifts", ListGifts)

	subject := models.Contact{UserID: user.ID, Firstname: "Subject"}
	require.NoError(t, db.Create(&subject).Error)

	g1 := models.Gift{UserID: user.ID, EntityID: subject.VCardUID, Description: "Idea one"}
	g2 := models.Gift{UserID: user.ID, EntityID: subject.VCardUID, Description: "Idea two"}
	require.NoError(t, db.Create(&g1).Error)
	require.NoError(t, db.Create(&g2).Error)

	afterOne := EncodeCursor(g1.UpdatedAt, g1.ID)

	req := func(query string) map[string]any {
		t.Helper()
		r, _ := http.NewRequest("GET", "/gifts?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	feed := req("since=" + afterOne + "&limit=10")
	items := feed["gifts"].([]any)
	require.Len(t, items, 1, "only the gift after the cursor should appear")
	assert.Equal(t, "Idea two", items[0].(map[string]any)["description"])
	assert.Equal(t, "incremental", feed["sync"].(map[string]any)["mode"])

	require.NoError(t, db.Delete(&g2).Error)
	feed2 := req("since=" + afterOne + "&limit=10")
	items2 := feed2["gifts"].([]any)
	require.Len(t, items2, 1)
	assert.Equal(t, "Idea two", items2[0].(map[string]any)["description"])
	assert.Equal(t, true, items2[0].(map[string]any)["deleted"], "soft-deleted gifts must be returned with deleted:true")
}

// TestAfterDeleteBumpsUpdatedAt proves every soft-delete entity's AfterDelete
// hook advances updated_at so the T17 change feed sees the tombstone. GORM's
// soft-delete UPDATE only writes deleted_at, leaving updated_at at its
// pre-delete value — without this hook a feed cursor stored before the delete
// would forever sit ahead of the tombstone (the exact trap the ticket
// calls out).
func TestAfterDeleteBumpsUpdatedAt(t *testing.T) {
	db, _ := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)

	subject := models.Contact{UserID: user.ID, Firstname: "Subject"}
	require.NoError(t, db.Create(&subject).Error)

	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	requireAfter := func(t *testing.T, updatedAt time.Time, label string) {
		t.Helper()
		assert.True(t, updatedAt.After(past), "%s AfterDelete must bump updated_at past %v, got %v", label, past, updatedAt)
	}

	// Contact (uint PK, gorm.Model soft delete)
	t.Run("Contact", func(t *testing.T) {
		c := models.Contact{UserID: user.ID, Firstname: "AD"}
		require.NoError(t, db.Create(&c).Error)
		require.NoError(t, db.Model(&c).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&c).Error)
		var reloaded models.Contact
		require.NoError(t, db.Unscoped().First(&reloaded, c.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "Contact")
	})

	// Note (uint PK, gorm.Model soft delete)
	t.Run("Note", func(t *testing.T) {
		n := models.Note{UserID: user.ID, Content: "test", Date: time.Now()}
		require.NoError(t, db.Create(&n).Error)
		require.NoError(t, db.Model(&n).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&n).Error)
		var reloaded models.Note
		require.NoError(t, db.Unscoped().First(&reloaded, n.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "Note")
	})

	// Activity (uint PK, gorm.Model soft delete)
	t.Run("Activity", func(t *testing.T) {
		a := models.Activity{UserID: user.ID, Title: "test", Date: time.Now()}
		require.NoError(t, db.Create(&a).Error)
		require.NoError(t, db.Model(&a).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&a).Error)
		var reloaded models.Activity
		require.NoError(t, db.Unscoped().First(&reloaded, a.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "Activity")
	})

	// LifeEvent (UUID string PK, explicit DeletedAt)
	t.Run("LifeEvent", func(t *testing.T) {
		le := models.LifeEvent{UserID: user.ID, EntityID: subject.VCardUID, Type: models.LifeEventTypeMarried}
		require.NoError(t, db.Create(&le).Error)
		require.NoError(t, db.Model(&le).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&le).Error)
		var reloaded models.LifeEvent
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", le.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "LifeEvent")
	})

	// Preference (UUID string PK, explicit DeletedAt)
	t.Run("Preference", func(t *testing.T) {
		p := models.Preference{UserID: user.ID, EntityID: subject.VCardUID, Category: models.PreferenceCategoryFood, Value: "pasta"}
		require.NoError(t, db.Create(&p).Error)
		require.NoError(t, db.Model(&p).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&p).Error)
		var reloaded models.Preference
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", p.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "Preference")
	})

	// ConversationAgenda (UUID string PK, explicit DeletedAt)
	t.Run("ConversationAgenda", func(t *testing.T) {
		a := models.ConversationAgenda{UserID: user.ID, EntityID: subject.VCardUID, Content: "Ask about the trip"}
		require.NoError(t, db.Create(&a).Error)
		require.NoError(t, db.Model(&a).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&a).Error)
		var reloaded models.ConversationAgenda
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", a.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "ConversationAgenda")
	})

	// Gift (UUID string PK, explicit DeletedAt)
	t.Run("Gift", func(t *testing.T) {
		g := models.Gift{UserID: user.ID, EntityID: subject.VCardUID, Description: "A nice book"}
		require.NoError(t, db.Create(&g).Error)
		require.NoError(t, db.Model(&g).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Delete(&g).Error)
		var reloaded models.Gift
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", g.ID).Error)
		requireAfter(t, reloaded.UpdatedAt, "Gift")
	})
}

// TestAfterDeleteSkipsBulkDelete proves that bulk soft deletes (which fire
// AfterDelete with a zero-value model — DeletedAt.Valid == false) do NOT bump
// updated_at. The explicit bump in deleteContactAssociations exists precisely
// because this path is skipped.
func TestAfterDeleteSkipsBulkDelete(t *testing.T) {
	db, _ := setupRouterWithRetention(30)
	var user models.User
	db.First(&user)

	subject := models.Contact{UserID: user.ID, Firstname: "Subject"}
	require.NoError(t, db.Create(&subject).Error)

	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)

	// Contact: bulk soft-delete via Where().Delete() fires hook with
	// zero-value receiver — DeletedAt.Valid is false, hook skips.
	t.Run("Contact", func(t *testing.T) {
		c1 := models.Contact{UserID: user.ID, Firstname: "Bulk1"}
		c2 := models.Contact{UserID: user.ID, Firstname: "Bulk2"}
		require.NoError(t, db.Create(&c1).Error)
		require.NoError(t, db.Create(&c2).Error)
		require.NoError(t, db.Model(&c1).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Model(&c2).UpdateColumn("updated_at", past).Error)
		names := []string{c1.Firstname, c2.Firstname}
		require.NoError(t, db.Where("firstname IN ? AND user_id = ?", names, user.ID).Delete(&models.Contact{}).Error)
		var reloaded1, reloaded2 models.Contact
		require.NoError(t, db.Unscoped().First(&reloaded1, c1.ID).Error)
		require.NoError(t, db.Unscoped().First(&reloaded2, c2.ID).Error)
		expected := past.Format(time.RFC3339Nano)
		assert.Equal(t, expected, reloaded1.UpdatedAt.Format(time.RFC3339Nano), "Contact bulk delete must not bump updated_at")
		assert.Equal(t, expected, reloaded2.UpdatedAt.Format(time.RFC3339Nano), "Contact bulk delete must not bump updated_at")
	})

	// Note: same pattern.
	t.Run("Note", func(t *testing.T) {
		n1 := models.Note{UserID: user.ID, Content: "bulk1", Date: time.Now()}
		n2 := models.Note{UserID: user.ID, Content: "bulk2", Date: time.Now()}
		require.NoError(t, db.Create(&n1).Error)
		require.NoError(t, db.Create(&n2).Error)
		require.NoError(t, db.Model(&n1).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Model(&n2).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Where("content IN ? AND user_id = ?", []string{"bulk1", "bulk2"}, user.ID).Delete(&models.Note{}).Error)
		var reloaded models.Note
		require.NoError(t, db.Unscoped().First(&reloaded, n1.ID).Error)
		assert.Equal(t, past.Format(time.RFC3339Nano), reloaded.UpdatedAt.Format(time.RFC3339Nano), "Note bulk delete must not bump updated_at")
	})

	// Activity: same pattern.
	t.Run("Activity", func(t *testing.T) {
		a1 := models.Activity{UserID: user.ID, Title: "bulk a1", Date: time.Now()}
		a2 := models.Activity{UserID: user.ID, Title: "bulk a2", Date: time.Now()}
		require.NoError(t, db.Create(&a1).Error)
		require.NoError(t, db.Create(&a2).Error)
		require.NoError(t, db.Model(&a1).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Model(&a2).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Where("title IN ? AND user_id = ?", []string{"bulk a1", "bulk a2"}, user.ID).Delete(&models.Activity{}).Error)
		var reloaded models.Activity
		require.NoError(t, db.Unscoped().First(&reloaded, a1.ID).Error)
		assert.Equal(t, past.Format(time.RFC3339Nano), reloaded.UpdatedAt.Format(time.RFC3339Nano), "Activity bulk delete must not bump updated_at")
	})

	// LifeEvent: same pattern (UUID string PK).
	t.Run("LifeEvent", func(t *testing.T) {
		le1 := models.LifeEvent{UserID: user.ID, EntityID: subject.VCardUID, Type: "bulk-test"}
		le2 := models.LifeEvent{UserID: user.ID, EntityID: subject.VCardUID, Type: "bulk-test"}
		require.NoError(t, db.Create(&le1).Error)
		require.NoError(t, db.Create(&le2).Error)
		require.NoError(t, db.Model(&le1).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Model(&le2).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Where("type = ? AND user_id = ?", "bulk-test", user.ID).Delete(&models.LifeEvent{}).Error)
		var reloaded models.LifeEvent
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", le1.ID).Error)
		assert.Equal(t, past.Format(time.RFC3339Nano), reloaded.UpdatedAt.Format(time.RFC3339Nano), "LifeEvent bulk delete must not bump updated_at")
	})

	// Preference: same pattern (UUID string PK).
	t.Run("Preference", func(t *testing.T) {
		p1 := models.Preference{UserID: user.ID, EntityID: subject.VCardUID, Category: models.PreferenceCategoryFood, Value: "bulk1"}
		p2 := models.Preference{UserID: user.ID, EntityID: subject.VCardUID, Category: models.PreferenceCategoryFood, Value: "bulk2"}
		require.NoError(t, db.Create(&p1).Error)
		require.NoError(t, db.Create(&p2).Error)
		require.NoError(t, db.Model(&p1).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Model(&p2).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Where("value IN ? AND user_id = ?", []string{"bulk1", "bulk2"}, user.ID).Delete(&models.Preference{}).Error)
		var reloaded models.Preference
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", p1.ID).Error)
		assert.Equal(t, past.Format(time.RFC3339Nano), reloaded.UpdatedAt.Format(time.RFC3339Nano), "Preference bulk delete must not bump updated_at")
	})

	// ConversationAgenda: same pattern (UUID string PK).
	t.Run("ConversationAgenda", func(t *testing.T) {
		a1 := models.ConversationAgenda{UserID: user.ID, EntityID: subject.VCardUID, Content: "bulk1"}
		a2 := models.ConversationAgenda{UserID: user.ID, EntityID: subject.VCardUID, Content: "bulk2"}
		require.NoError(t, db.Create(&a1).Error)
		require.NoError(t, db.Create(&a2).Error)
		require.NoError(t, db.Model(&a1).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Model(&a2).UpdateColumn("updated_at", past).Error)
		require.NoError(t, db.Where("content IN ? AND user_id = ?", []string{"bulk1", "bulk2"}, user.ID).Delete(&models.ConversationAgenda{}).Error)
		var reloaded models.ConversationAgenda
		require.NoError(t, db.Unscoped().First(&reloaded, "id = ?", a1.ID).Error)
		assert.Equal(t, past.Format(time.RFC3339Nano), reloaded.UpdatedAt.Format(time.RFC3339Nano), "ConversationAgenda bulk delete must not bump updated_at")
	})
}

// TestFeedIndexesExistInMigratedSchema is the real-DB check for migration
// 000043: the composite (user_id, updated_at, id) indexes must exist on every
// paginated table in the real migrated schema (the cursor query degrades to a
// scan without them). Uses database.InitDB, not AutoMigrate.
func TestFeedIndexesExistInMigratedSchema(t *testing.T) {
	db := dbtest.New(t)

	for _, table := range []string{
		"contacts", "notes", "activities", "life_events", "preferences",
		"circles", "households", "tags", "relationship_edges", "field_definitions",
		"conversation_agenda",
	} {
		var count int64
		err := db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?",
			"idx_"+table+"_feed", table,
		).Scan(&count).Error
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "migration 000043 must create idx_%s_feed", table)
	}
}
