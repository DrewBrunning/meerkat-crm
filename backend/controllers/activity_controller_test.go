package controllers

import (
	"bytes"
	"encoding/json"
	"mycorrhizal/config"
	"mycorrhizal/models"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"

	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRouter() (*gorm.DB, *gin.Engine) {
	gin.SetMode(gin.ReleaseMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	db.AutoMigrate(&models.Contact{}, &models.Activity{}, &models.Note{}, models.Reminder{}, models.User{}, models.Webhook{}, models.WebhookDelivery{}, models.ContactSubscription{}, models.ContactSyncLink{}, models.RelationshipEdge{}, models.Circle{}, models.CircleMember{}, models.Tag{}, models.ContactTag{}, models.LifeEvent{}, models.Household{}, models.HouseholdMember{}, models.FieldDefinition{}, models.FieldValue{}, models.CardDAVSync{}, models.ApiToken{}, models.DeviceGrant{}, models.ReminderCompletion{}, models.CalendarSubscription{}, models.CalendarEventLink{}, models.Preference{}, models.CadencePolicy{}, models.ConversationAgenda{}, models.Gift{}, models.ExternalIdentity{}, models.ExternalActivity{}, models.ImmichConfig{}, models.PaperlessConfig{}, models.SeafileConfig{}, models.WebDAVConfig{}, models.ContactShare{}, models.LinkFieldType{}, models.NotificationDelivery{}, models.NotificationConfig{}, models.PushSubscription{}, models.DeviceRegistration{}, models.ServerSetting{}, models.Attachment{}, models.DismissedDuplicatePair{}, models.RecoveryCode{}, models.ReachOutSuggestion{}, models.ReachOutCursor{}, models.ContactSyncConflict{}, models.ImportRun{}, models.IdempotencyKey{}, models.Session{})

	// T85:
	// applyContactSearch unconditionally references contacts_fts for any
	// search= term of two-plus runes, but that virtual table is hand-written
	// migration SQL (000007, widened by 000010/000020) that AutoMigrate does
	// not know about. Create it empty here (no triggers, no rows) purely so
	// the query doesn't 500 under this fast AutoMigrate schema — the FTS
	// clause then contributes nothing, which is fine: this helper's tests
	// exercise the LIKE clause, and FTS-specific matching is covered against
	// the real migrated schema (database.InitDB) in
	// contact_fts_search_test.go.
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS contacts_fts USING fts5(
		firstname, lastname, nickname, email, phone, org, addresses_flat, phones_normalized,
		user_id UNINDEXED
	)`).Error; err != nil {
		panic("failed to create stub contacts_fts table: " + err.Error())
	}

	user := models.User{Username: "tester", Password: "password123", Email: "tester@example.com"}
	if err := db.Create(&user).Error; err != nil {
		panic("failed to seed user")
	}

	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", user.ID)
		c.Set("cfg", config.Config{ProfilePhotoDir: os.Getenv("PROFILE_PHOTO_DIR")})
		c.Next()
	})

	return db, router
}

func withValidated(factory func() any) gin.HandlerFunc {
	return func(c *gin.Context) {
		payload := factory()
		if payload != nil {
			if err := c.ShouldBindJSON(payload); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.Set("validated", payload)
		}
		c.Next()
	}
}

func TestCreateActivity(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.POST("/activities", withValidated(func() any { return &models.ActivityInput{} }), CreateActivity)

	contacts := []models.Contact{
		{
			UserID:    user.ID,
			Firstname: "John",
			Lastname:  "Doe",
		},
		{
			UserID:    user.ID,
			Firstname: "Jane",
			Lastname:  "Smith",
		},
	}

	db.Create(&contacts[0])
	db.Create(&contacts[1])

	activityPayload := models.ActivityInput{
		Title:       "Great activity",
		Description: "A fun get-together.",
		Location:    "Somewhere out there",
		Date:        time.Now().AddDate(0, 0, 1),
		ContactIDs:  []uint{contacts[0].ID, contacts[1].ID},
	}
	jsonValue, _ := json.Marshal(activityPayload)

	req, _ := http.NewRequest("POST", "/activities", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "Activity created successfully", responseBody["message"])
}

func TestGetActivitiesForContact(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/contacts/:id/activities", GetActivitiesForContact)

	// Create a contact
	contact := models.Contact{
		UserID:    user.ID,
		Firstname: "John",
		Lastname:  "Doe",
	}
	db.Create(&contact)

	// Create some activities
	activity1 := models.Activity{
		UserID:      user.ID,
		Title:       "Activity One",
		Description: "First activity",
		Location:    "Location One",
		Date:        time.Now().AddDate(0, 0, 1),
	}
	activity2 := models.Activity{
		UserID:      user.ID,
		Title:       "Activity Two",
		Description: "Second activity",
		Location:    "Location Two",
		Date:        time.Now().AddDate(0, 0, 2),
	}
	db.Create(&activity1)
	db.Create(&activity2)

	// Associate the contact with the activities
	db.Model(&activity1).Association("Contacts").Append(&contact)
	db.Model(&activity2).Association("Contacts").Append(&contact)

	// A second contact whose activities must NOT leak into this contact's list.
	otherContact := models.Contact{UserID: user.ID, Firstname: "Other"}
	db.Create(&otherContact)
	otherActivity := models.Activity{
		UserID: user.ID,
		Title:  "Not Yours",
		Date:   time.Now().AddDate(0, 0, 3),
	}
	db.Create(&otherActivity)
	db.Model(&otherActivity).Association("Contacts").Append(&otherContact)

	// Make the request to get activities for the contact
	req, _ := http.NewRequest("GET", "/contacts/"+strconv.Itoa(int(contact.ID))+"/activities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Len(t, responseBody["activities"], 2) // Should return both activities for the contact
	assert.NotContains(t, responseBody, "total")
	assert.EqualValues(t, 25, responseBody["limit"])
	// M19: cursor envelope present; two rows fit in the default page so no cursor.
	assert.Equal(t, "", responseBody["next_cursor"])

	// The activity also carries its participant contacts so the client can
	// render the chips without a second lookup.
	activities := responseBody["activities"].([]any)
	first := activities[0].(map[string]any)
	participants := first["contacts"].([]any)
	assert.Len(t, participants, 1)
}

func TestGetActivitiesForContactSearchAndDateFilter(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/contacts/:id/activities", GetActivitiesForContact)

	contact := models.Contact{UserID: user.ID, Firstname: "John", Lastname: "Doe"}
	db.Create(&contact)

	mkActivity := func(title, desc string, daysAhead int) models.Activity {
		a := models.Activity{UserID: user.ID, Title: title, Description: desc, Date: time.Now().AddDate(0, 0, daysAhead)}
		db.Create(&a)
		require.NoError(t, db.Model(&a).Association("Contacts").Append(&contact))
		return a
	}
	mkActivity("Coffee with Dana", "Talked about the trip", 1)
	mkActivity("Phone call", "Quick check-in", 3)
	mkActivity("Gift shipped", "Birthday present", 10)

	// Search narrows across title and description (case-insensitive).
	req, _ := http.NewRequest("GET", "/contacts/"+strconv.Itoa(int(contact.ID))+"/activities?search=TRIP", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	assert.Len(t, body["activities"].([]any), 1)

	// fromDate/toDate filter on the activity date, inclusive both ends.
	from := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	to := time.Now().AddDate(0, 0, 11).Format("2006-01-02")
	req2, _ := http.NewRequest("GET", "/contacts/"+strconv.Itoa(int(contact.ID))+"/activities?fromDate="+from+"&toDate="+to, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	json.Unmarshal(w2.Body.Bytes(), &body)
	assert.Len(t, body["activities"].([]any), 2, "the 3- and 10-day-out activities fall inside the window")
}

func TestGetActivitiesForContactPagination(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/contacts/:id/activities", GetActivitiesForContact)

	contact := models.Contact{UserID: user.ID, Firstname: "John", Lastname: "Doe"}
	db.Create(&contact)

	base := time.Now().Add(-time.Hour)
	for i, title := range []string{"One", "Two", "Three"} {
		a := models.Activity{UserID: user.ID, Title: title, Date: time.Now()}
		require.NoError(t, db.Create(&a).Error)
		require.NoError(t, db.Model(&a).Update("updated_at", base.Add(time.Duration(i)*time.Minute)).Error)
		require.NoError(t, db.Model(&a).Association("Contacts").Append(&contact))
	}

	req, _ := http.NewRequest("GET", "/contacts/"+strconv.Itoa(int(contact.ID))+"/activities?limit=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var page1 struct {
		Activities []models.Activity `json:"activities"`
		NextCursor string            `json:"next_cursor"`
	}
	json.Unmarshal(w.Body.Bytes(), &page1)
	require.Len(t, page1.Activities, 2)
	require.NotEmpty(t, page1.NextCursor, "a full page must carry a next_cursor")

	req2, _ := http.NewRequest("GET", "/contacts/"+strconv.Itoa(int(contact.ID))+"/activities?limit=2&cursor="+page1.NextCursor, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var page2 struct {
		Activities []models.Activity `json:"activities"`
		NextCursor string            `json:"next_cursor"`
	}
	json.Unmarshal(w2.Body.Bytes(), &page2)
	require.Len(t, page2.Activities, 1)
	assert.Empty(t, page2.NextCursor, "no more rows after the last page")

	seen := map[uint]bool{}
	for _, a := range append(page1.Activities, page2.Activities...) {
		seen[a.ID] = true
	}
	require.Len(t, seen, 3, "every activity appears exactly once across the walk")
}

func TestGetActivitiesForContactNotFound(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/contacts/:id/activities", GetActivitiesForContact)

	req, _ := http.NewRequest("GET", "/contacts/999999/activities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetActivities(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/activities", GetActivities)

	// Create some activities
	activity1 := models.Activity{
		UserID:      user.ID,
		Title:       "Activity One",
		Description: "First activity",
		Location:    "Location One",
		Date:        time.Now().AddDate(0, 0, 1),
	}
	activity2 := models.Activity{
		UserID:      user.ID,
		Title:       "Activity Two",
		Description: "Second activity",
		Location:    "Location Two",
		Date:        time.Now().AddDate(0, 0, 2),
	}
	db.Create(&activity1)
	db.Create(&activity2)

	// Make the request to get all activities
	req, _ := http.NewRequest("GET", "/activities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Len(t, responseBody["activities"], 2)
	// T17: the cursor envelope has no total/page.
	assert.NotContains(t, responseBody, "total")
	assert.NotContains(t, responseBody, "page")
	assert.EqualValues(t, 25, responseBody["limit"])
	assert.Equal(t, "incremental", responseBody["sync"].(map[string]any)["mode"])
}

func TestGetActivitiesSearchByContact(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/activities", GetActivities)

	contact := models.Contact{UserID: user.ID, Firstname: "Search", Lastname: "Target"}
	db.Create(&contact)

	activityWithContact := models.Activity{
		UserID:      user.ID,
		Title:       "Targeted Activity",
		Description: "Includes contact",
		Date:        time.Now().AddDate(0, 0, 1),
	}
	activityWithoutContact := models.Activity{
		UserID:      user.ID,
		Title:       "Other Activity",
		Description: "No matching contact",
		Date:        time.Now().AddDate(0, 0, 2),
	}
	db.Create(&activityWithContact)
	db.Create(&activityWithoutContact)
	db.Model(&activityWithContact).Association("Contacts").Append(&contact)

	req, _ := http.NewRequest("GET", "/activities?search=target", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)

	activitiesRaw, ok := responseBody["activities"].([]any)
	if !ok {
		t.Fatalf("expected activities array in response")
	}
	assert.Len(t, activitiesRaw, 1)
	assert.NotContains(t, responseBody, "total")
}

func TestGetActivity(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.GET("/activities/:id", GetActivity)

	// Create an activity
	activity := models.Activity{
		UserID:      user.ID,
		Title:       "Activity One",
		Description: "First activity",
		Location:    "Location One",
		Date:        time.Now().AddDate(0, 0, 1),
	}
	db.Create(&activity)

	// Make the request to get the activity by ID
	req, _ := http.NewRequest("GET", "/activities/"+strconv.Itoa(int(activity.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody models.Activity
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, activity.Title, responseBody.Title)
}

func TestUpdateActivity(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.PUT("/activities/:id", withValidated(func() any { return &models.ActivityInput{} }), UpdateActivity)

	// Create an activity
	activity := models.Activity{
		UserID:      user.ID,
		Title:       "Activity One",
		Description: "First activity",
		Location:    "Location One",
		Date:        time.Now().AddDate(0, 0, 1),
	}
	db.Create(&activity)

	// Update activity details
	activityUpdate := models.ActivityInput{
		Title:       "Updated Activity",
		Description: "Updated description",
		Location:    "Updated location",
		Date:        time.Now(),
		ContactIDs:  []uint{},
	}
	jsonValue, _ := json.Marshal(activityUpdate)

	// Make the request to update the activity
	req, _ := http.NewRequest("PUT", "/activities/"+strconv.Itoa(int(activity.ID)), bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody models.Activity
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, activityUpdate.Title, responseBody.Title)
}

// gap fix: Type/ExternalRef (added to Activity ) previously
// round-tripped on read but could never be set via the API — Create/Update
// never copied them from ActivityInput onto the model.
func TestCreateActivitySetsTypeAndExternalRef(t *testing.T) {
	_, router := setupRouter()
	router.POST("/activities", withValidated(func() any { return &models.ActivityInput{} }), CreateActivity)

	payload := models.ActivityInput{
		Title: "Coffee", Date: time.Now().AddDate(0, 0, 1),
		Type: models.InteractionTypeMeal, ExternalRef: "caldav:abc123",
	}
	jsonValue, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "/activities", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	activity := responseBody["activity"].(map[string]any)
	assert.Equal(t, models.InteractionTypeMeal, activity["type"])
	assert.Equal(t, "caldav:abc123", activity["external_ref"])
}

func TestUpdateActivitySetsTypeAndExternalRef(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)
	router.PUT("/activities/:id", withValidated(func() any { return &models.ActivityInput{} }), UpdateActivity)

	activity := models.Activity{UserID: user.ID, Title: "Coffee", Date: time.Now().AddDate(0, 0, 1)}
	db.Create(&activity)

	payload := models.ActivityInput{
		Title: "Coffee", Date: time.Now(),
		Type: models.InteractionTypeVisit, ExternalRef: "caldav:xyz789",
	}
	jsonValue, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", "/activities/"+strconv.Itoa(int(activity.ID)), bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var reloaded models.Activity
	db.First(&reloaded, activity.ID)
	assert.Equal(t, models.InteractionTypeVisit, reloaded.Type)
	assert.Equal(t, "caldav:xyz789", reloaded.ExternalRef)
}

func TestDeleteActivity(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)

	router.DELETE("/activities/:id", DeleteActivity)

	// Create an activity
	activity := models.Activity{
		UserID:      user.ID,
		Title:       "Activity One",
		Description: "First activity",
		Location:    "Location One",
		Date:        time.Now().AddDate(0, 0, 1),
	}
	db.Create(&activity)

	// Make the request to delete the activity
	req, _ := http.NewRequest("DELETE", "/activities/"+strconv.Itoa(int(activity.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "Activity deleted", responseBody["message"])

	// Verify activity has been deleted
	var deletedActivity models.Activity
	result := db.First(&deletedActivity, activity.ID)
	assert.True(t, result.Error != nil) // This should return an error as it has been deleted
}
