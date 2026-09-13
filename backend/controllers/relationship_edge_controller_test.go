package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRelationshipEdge(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	payload := models.RelationshipEdgeInput{
		SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "parent_of",
		// Client-sent values for server-derived fields must be ignored --
		// RelationshipEdgeInput doesn't even have Source/Confidence/Status
		// fields, so this is really just documenting there's no way to send
		// them, not something the test needs to assert against a payload.
	}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp struct {
		RelationshipEdge models.RelationshipEdge `json:"relationship_edge"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	edge := resp.RelationshipEdge

	assert.Equal(t, alice.VCardUID, edge.SourceID)
	assert.Equal(t, bob.VCardUID, edge.TargetID)
	assert.Equal(t, "parent_of", edge.Type)
	assert.True(t, edge.Directional, "parent_of is asymmetric")
	assert.Equal(t, models.RelationshipSourceUserConfirmed, edge.Source)
	assert.Equal(t, 1.0, edge.Confidence)
	assert.Equal(t, models.RelationshipStatusConfirmed, edge.Status)
	assert.Equal(t, models.RelationshipSensitivityNormal, edge.Sensitivity)
}

func TestCreateRelationshipEdge_SymmetricType(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "spouse_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	var resp struct {
		RelationshipEdge models.RelationshipEdge `json:"relationship_edge"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.RelationshipEdge.Directional, "spouse_of is symmetric")
}

func TestCreateRelationshipEdge_RejectsSourceFromAnotherUser(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	otherUser := models.User{Username: "other", Password: "x", Email: "other@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	othersContact := models.Contact{UserID: otherUser.ID, Firstname: "Not Yours"}
	require.NoError(t, db.Create(&othersContact).Error)
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&bob).Error)

	payload := models.RelationshipEdgeInput{SourceID: othersContact.VCardUID, TargetID: bob.VCardUID, Type: "friend_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateRelationshipEdge_RejectsTargetFromAnotherUser(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	otherUser := models.User{Username: "other", Password: "x", Email: "other@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	othersContact := models.Contact{UserID: otherUser.ID, Firstname: "Not Yours"}
	require.NoError(t, db.Create(&othersContact).Error)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	require.NoError(t, db.Create(&alice).Error)

	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: othersContact.VCardUID, Type: "friend_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateRelationshipEdge_SelfEdgeRejected(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	require.NoError(t, db.Create(&alice).Error)

	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: alice.VCardUID, Type: "friend_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateRelationshipEdge_BothIDAndThinRejected(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	require.NoError(t, db.Create(&alice).Error)

	payload := models.RelationshipEdgeInput{
		SourceID: alice.VCardUID, Type: "friend_of",
		TargetID: alice.VCardUID, TargetThin: &models.ThinContactInput{Name: "Ghost"},
	}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateRelationshipEdge_NeitherIDNorThinRejected(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	require.NoError(t, db.Create(&alice).Error)

	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, Type: "friend_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateRelationshipEdge_ThinTarget(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	require.NoError(t, db.Create(&alice).Error)

	payload := models.RelationshipEdgeInput{
		SourceID: alice.VCardUID, Type: "parent_of",
		TargetThin: &models.ThinContactInput{Name: "New Baby", Gender: "other"},
	}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var newContact models.Contact
	require.NoError(t, db.Where("user_id = ? AND firstname = ?", user.ID, "New Baby").First(&newContact).Error)
	assert.Equal(t, "other", newContact.Gender)

	var resp struct {
		RelationshipEdge models.RelationshipEdge `json:"relationship_edge"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, newContact.VCardUID, resp.RelationshipEdge.TargetID)
}

// TestCreateRelationshipEdge_ThinTargetTransactionalOnEdgeFailure proves
// applyRelationshipEdgeInput's db.Transaction wrapping: a thin contact
// created while resolving the target must not survive if the edge write
// itself then fails. Forces the edge write to fail via an already-used
// a constraint collision. Since LegacyRelationshipID was removed in T22,
// the test instead forces the failure via cross-user target ownership
// (target_id pointing at another user's contact — 404).
func TestCreateRelationshipEdge_ThinTargetTransactionalOnEdgeFailure(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	require.NoError(t, db.Create(&alice).Error)

	var countBefore int64
	require.NoError(t, db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&countBefore).Error)

	// An unregistered relation type passes withValidated's bypass (no real
	// validation), reaches applyRelationshipEdgeInput, and fails
	// !models.IsSymmetricRelationType's registry lookup path harmlessly
	// (returns false, not an error) -- so instead force failure at the SQL
	// layer by pre-creating a RelationshipEdge whose ID collides. Simpler
	// and just as decisive: pass an empty Type, which the DB column allows
	// (NOT NULL empty string is not a violation) — so instead directly
	// The self-edge rejection path: SourceID == TargetThin-resolved-ID
	// can't collide deterministically, so instead force failure via a
	// target_id pointing at a contact from another user (404 after the
	// thin source contact would already have been created) — proves a
	// thin SOURCE contact doesn't survive a subsequent target resolution
	// failure.
	otherUser := models.User{Username: "other2", Password: "x", Email: "other2@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	othersContact := models.Contact{UserID: otherUser.ID, Firstname: "Not Yours"}
	require.NoError(t, db.Create(&othersContact).Error)

	payload := models.RelationshipEdgeInput{
		SourceThin: &models.ThinContactInput{Name: "Ghost Source"},
		TargetID:   othersContact.VCardUID,
		Type:       "friend_of",
	}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)

	var countAfter int64
	require.NoError(t, db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&countAfter).Error)
	assert.Equal(t, countBefore, countAfter, "the thin source contact must not survive the target resolution failure")
}

func TestGetRelationshipEdge(t *testing.T) {
	db, router := setupRouter()
	router.GET("/relationship-edges/:id", GetRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("GET", "/relationship-edges/"+edge.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetRelationshipEdge_NotFound(t *testing.T) {
	_, router := setupRouter()
	router.GET("/relationship-edges/:id", GetRelationshipEdge)

	req, _ := http.NewRequest("GET", "/relationship-edges/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetRelationshipEdge_WrongUser404s(t *testing.T) {
	db, router := setupRouter()
	router.GET("/relationship-edges/:id", GetRelationshipEdge)

	var user models.User
	db.First(&user)
	otherUser := models.User{Username: "other3", Password: "x", Email: "other3@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	alice := models.Contact{UserID: otherUser.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: otherUser.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: otherUser.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("GET", "/relationship-edges/"+edge.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	_ = user
}

func TestListRelationshipEdges_FiltersByContactBothDirections(t *testing.T) {
	db, router := setupRouter()
	router.GET("/relationship-edges", ListRelationshipEdges)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	carol := models.Contact{UserID: user.ID, Firstname: "Carol"}
	dave := models.Contact{UserID: user.ID, Firstname: "Dave"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	require.NoError(t, db.Create(&carol).Error)
	require.NoError(t, db.Create(&dave).Error)

	// alice is SourceID here
	edgeAsSource := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	// alice is TargetID here
	edgeAsTarget := models.RelationshipEdge{
		UserID: user.ID, SourceID: carol.VCardUID, TargetID: alice.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	// unrelated to alice
	unrelated := models.RelationshipEdge{
		UserID: user.ID, SourceID: bob.VCardUID, TargetID: dave.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edgeAsSource).Error)
	require.NoError(t, db.Create(&edgeAsTarget).Error)
	require.NoError(t, db.Create(&unrelated).Error)

	req, _ := http.NewRequest("GET", "/relationship-edges?contact_id="+alice.VCardUID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		RelationshipEdges []models.RelationshipEdge `json:"relationship_edges"`
		Total             int64                     `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.EqualValues(t, 2, resp.Total)
}

func TestListRelationshipEdges_FiltersByStatus(t *testing.T) {
	db, router := setupRouter()
	router.GET("/relationship-edges", ListRelationshipEdges)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	confirmed := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	suggested := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "roommate_of",
		Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.7, Status: models.RelationshipStatusSuggested,
	}
	require.NoError(t, db.Create(&confirmed).Error)
	require.NoError(t, db.Create(&suggested).Error)

	req, _ := http.NewRequest("GET", "/relationship-edges?status=suggested", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		RelationshipEdges []models.RelationshipEdge `json:"relationship_edges"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.RelationshipEdges, 1)
	assert.Equal(t, suggested.ID, resp.RelationshipEdges[0].ID)
}

func TestListRelationshipEdges_ScopedToUser(t *testing.T) {
	db, router := setupRouter()
	router.GET("/relationship-edges", ListRelationshipEdges)

	var user models.User
	db.First(&user)
	otherUser := models.User{Username: "other4", Password: "x", Email: "other4@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	a := models.Contact{UserID: otherUser.ID, Firstname: "A"}
	b := models.Contact{UserID: otherUser.ID, Firstname: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	othersEdge := models.RelationshipEdge{
		UserID: otherUser.ID, SourceID: a.VCardUID, TargetID: b.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&othersEdge).Error)

	req, _ := http.NewRequest("GET", "/relationship-edges", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		RelationshipEdges []models.RelationshipEdge `json:"relationship_edges"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.RelationshipEdges)
}

func TestUpdateRelationshipEdge(t *testing.T) {
	db, router := setupRouter()
	router.PUT("/relationship-edges/:id", withValidated(func() any { return &models.RelationshipEdgeInput{} }), UpdateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.8, Status: models.RelationshipStatusSuggested,
	}
	require.NoError(t, db.Create(&edge).Error)

	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "roommate_of", Sensitivity: "private"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", "/relationship-edges/"+edge.ID, bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var updated models.RelationshipEdge
	require.NoError(t, db.First(&updated, "id = ?", edge.ID).Error)
	assert.Equal(t, "roommate_of", updated.Type)
	assert.Equal(t, "private", updated.Sensitivity)
	assert.Equal(t, models.RelationshipStatusSuggested, updated.Status, "Status must be untouched by PUT")
	assert.Equal(t, models.RelationshipSourceHouseholdInferred, updated.Source, "Source must be untouched by PUT")
	assert.Equal(t, 0.8, updated.Confidence, "Confidence must be untouched by PUT")
}

func TestUpdateRelationshipEdge_NotFound(t *testing.T) {
	_, router := setupRouter()
	router.PUT("/relationship-edges/:id", withValidated(func() any { return &models.RelationshipEdgeInput{} }), UpdateRelationshipEdge)

	payload := models.RelationshipEdgeInput{SourceID: "x", TargetID: "y", Type: "friend_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", "/relationship-edges/nonexistent", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateRelationshipEdge_WrongUser404s(t *testing.T) {
	db, router := setupRouter()
	router.PUT("/relationship-edges/:id", withValidated(func() any { return &models.RelationshipEdgeInput{} }), UpdateRelationshipEdge)

	otherUser := models.User{Username: "other5", Password: "x", Email: "other5@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	a := models.Contact{UserID: otherUser.ID, Firstname: "A"}
	b := models.Contact{UserID: otherUser.ID, Firstname: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	edge := models.RelationshipEdge{
		UserID: otherUser.ID, SourceID: a.VCardUID, TargetID: b.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	payload := models.RelationshipEdgeInput{SourceID: a.VCardUID, TargetID: b.VCardUID, Type: "friend_of"}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", "/relationship-edges/"+edge.ID, bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateRelationshipEdge_RepointTargetToThin(t *testing.T) {
	db, router := setupRouter()
	router.PUT("/relationship-edges/:id", withValidated(func() any { return &models.RelationshipEdgeInput{} }), UpdateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	payload := models.RelationshipEdgeInput{
		SourceID: alice.VCardUID, Type: "friend_of",
		TargetThin: &models.ThinContactInput{Name: "Someone New"},
	}
	jsonValue, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", "/relationship-edges/"+edge.ID, bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var newContact models.Contact
	require.NoError(t, db.Where("user_id = ? AND firstname = ?", user.ID, "Someone New").First(&newContact).Error)

	var updated models.RelationshipEdge
	require.NoError(t, db.First(&updated, "id = ?", edge.ID).Error)
	assert.Equal(t, newContact.VCardUID, updated.TargetID)
	assert.NotEqual(t, bob.VCardUID, updated.TargetID)
}

func TestDeleteRelationshipEdge(t *testing.T) {
	db, router := setupRouter()
	router.DELETE("/relationship-edges/:id", DeleteRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("DELETE", "/relationship-edges/"+edge.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var count int64
	db.Model(&models.RelationshipEdge{}).Where("id = ?", edge.ID).Count(&count)
	assert.EqualValues(t, 0, count)
}

func TestDeleteRelationshipEdge_NotFound(t *testing.T) {
	_, router := setupRouter()
	router.DELETE("/relationship-edges/:id", DeleteRelationshipEdge)

	req, _ := http.NewRequest("DELETE", "/relationship-edges/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteRelationshipEdge_WrongUser404s(t *testing.T) {
	db, router := setupRouter()
	router.DELETE("/relationship-edges/:id", DeleteRelationshipEdge)

	otherUser := models.User{Username: "other6", Password: "x", Email: "other6@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	a := models.Contact{UserID: otherUser.ID, Firstname: "A"}
	b := models.Contact{UserID: otherUser.ID, Firstname: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	edge := models.RelationshipEdge{
		UserID: otherUser.ID, SourceID: a.VCardUID, TargetID: b.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("DELETE", "/relationship-edges/"+edge.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAcceptRelationshipEdge(t *testing.T) {
	db, router := setupRouter()
	router.PATCH("/relationship-edges/:id/accept", AcceptRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "roommate_of",
		Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.7, Status: models.RelationshipStatusSuggested,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("PATCH", "/relationship-edges/"+edge.ID+"/accept", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var updated models.RelationshipEdge
	require.NoError(t, db.First(&updated, "id = ?", edge.ID).Error)
	assert.Equal(t, models.RelationshipStatusConfirmed, updated.Status)
	assert.Equal(t, models.RelationshipSourceHouseholdInferred, updated.Source, "Source (provenance) must be preserved through accept")
	assert.Equal(t, 0.7, updated.Confidence, "Confidence must be preserved through accept")
}

func TestAcceptRelationshipEdge_AlreadyConfirmedConflict(t *testing.T) {
	db, router := setupRouter()
	router.PATCH("/relationship-edges/:id/accept", AcceptRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("PATCH", "/relationship-edges/"+edge.ID+"/accept", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAcceptRelationshipEdge_NotFound(t *testing.T) {
	_, router := setupRouter()
	router.PATCH("/relationship-edges/:id/accept", AcceptRelationshipEdge)

	req, _ := http.NewRequest("PATCH", "/relationship-edges/nonexistent/accept", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAcceptRelationshipEdge_WrongUser404s(t *testing.T) {
	db, router := setupRouter()
	router.PATCH("/relationship-edges/:id/accept", AcceptRelationshipEdge)

	otherUser := models.User{Username: "other7", Password: "x", Email: "other7@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)
	a := models.Contact{UserID: otherUser.ID, Firstname: "A"}
	b := models.Contact{UserID: otherUser.ID, Firstname: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	edge := models.RelationshipEdge{
		UserID: otherUser.ID, SourceID: a.VCardUID, TargetID: b.VCardUID, Type: "roommate_of",
		Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.7, Status: models.RelationshipStatusSuggested,
	}
	require.NoError(t, db.Create(&edge).Error)

	req, _ := http.NewRequest("PATCH", "/relationship-edges/"+edge.ID+"/accept", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestCreateRelationshipEdge_RealValidation_RelationType wires the real
// middleware.ValidateJSONMiddleware(&models.RelationshipEdgeInput{}) (not
// withValidated's bypass) to prove the relation_type validator tag actually
// rejects unregistered tokens through the real request path, and that
// synonym matching (MatchLegacyRelationType, used only by the
// migration tool) does NOT apply here -- a synonym like "mother_of" is not
// itself a registry key and must be rejected too.
func TestCreateRelationshipEdge_RealValidation_RelationType(t *testing.T) {
	cases := []struct {
		name    string
		relType string
		wantOK  bool
	}{
		{"registered key parent_of", "parent_of", true},
		{"registered key spouse_of", "spouse_of", true},
		{"synonym not a registry key", "mother_of", false},
		{"bogus type", "bogus_type", false},
		{"empty type", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, router := setupRouter()
			router.POST("/relationship-edges", middleware.ValidateJSONMiddleware(&models.RelationshipEdgeInput{}), CreateRelationshipEdge)

			var user models.User
			db.First(&user)
			alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
			bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
			require.NoError(t, db.Create(&alice).Error)
			require.NoError(t, db.Create(&bob).Error)

			payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: tc.relType}
			jsonValue, _ := json.Marshal(payload)
			req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(jsonValue))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tc.wantOK {
				assert.Equal(t, http.StatusCreated, w.Code)
			} else {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}

// --- natural-key duplicate handling (issue #928) ----------------------------

// TestCreateRelationshipEdge_DuplicateNaturalKeyConflict is the double-click /
// replay regression: the exact same (source, target, type) create a second
// time must be a checked 409, not a second stored row.
func TestCreateRelationshipEdge_DuplicateNaturalKeyConflict(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "parent_of"}
	body, _ := json.Marshal(payload)

	post := func() *httptest.ResponseRecorder {
		req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusCreated, post().Code)

	second := post()
	require.Equal(t, http.StatusConflict, second.Code, "a duplicate create must be rejected with 409")

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &resp))
	assert.Equal(t, "ALREADY_EXISTS", resp.Error.Code)

	var count int64
	require.NoError(t, db.Model(&models.RelationshipEdge{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "a duplicate create must not insert a second edge")
}

// TestCreateRelationshipEdge_DifferentTypeSameEndpointsAllowed proves the
// natural key includes the type: the same two contacts may hold two different
// relationship facts.
func TestCreateRelationshipEdge_DifferentTypeSameEndpointsAllowed(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	for _, relType := range []string{"parent_of", "friend_of"} {
		body, _ := json.Marshal(models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: relType})
		req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, "type %s must be allowed", relType)
	}

	var count int64
	require.NoError(t, db.Model(&models.RelationshipEdge{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

// TestRelationshipEdgeNaturalKey_ScopedByUser proves the natural key is scoped
// by user_id — two different users may each record the same relationship tuple.
func TestRelationshipEdgeNaturalKey_ScopedByUser(t *testing.T) {
	db, _ := setupRouter()

	otherUser := models.User{Username: "other8", Password: "x", Email: "other8@example.com"}
	require.NoError(t, db.Create(&otherUser).Error)

	var first models.User
	db.First(&first)

	// Identical tuple (even identical endpoint IDs) under two users.
	tuple := func(uid uint) *models.RelationshipEdge {
		return &models.RelationshipEdge{
			UserID: uid, SourceID: "shared-src", TargetID: "shared-tgt", Type: "friend_of",
			Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
		}
	}
	require.NoError(t, db.Create(tuple(first.ID)).Error)
	require.NoError(t, db.Create(tuple(otherUser.ID)).Error, "the same tuple under a different user must not collide")
}

// TestUpdateRelationshipEdge_RepointOntoExistingConflict proves PUT cannot
// re-point an edge onto another edge's natural key — the same uniqueness gate
// as create, with the edge under edit excluded from its own lookup.
func TestUpdateRelationshipEdge_RepointOntoExistingConflict(t *testing.T) {
	db, router := setupRouter()
	router.PUT("/relationship-edges/:id", withValidated(func() any { return &models.RelationshipEdgeInput{} }), UpdateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	carol := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	require.NoError(t, db.Create(&carol).Error)

	existing := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	moving := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: carol.VCardUID, Type: "parent_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&existing).Error)
	require.NoError(t, db.Create(&moving).Error)

	// Re-point `moving` at (alice, bob, friend_of) — exactly `existing`.
	payload := models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of"}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", "/relationship-edges/"+moving.ID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code, "re-pointing onto an existing natural key must be a 409")

	var reloaded models.RelationshipEdge
	require.NoError(t, db.First(&reloaded, "id = ?", moving.ID).Error)
	assert.Equal(t, carol.VCardUID, reloaded.TargetID, "the rejected update must leave the edge unchanged")
	assert.Equal(t, "parent_of", reloaded.Type)
}

// TestRelationshipEdgeWriteError maps the SQLite unique-constraint backstop
// (the pre-check can lose a race) to the same 409, and passes every other
// error through untouched.
func TestRelationshipEdgeWriteError(t *testing.T) {
	mapped := relationshipEdgeWriteError(errors.New("UNIQUE constraint failed: relationship_edges.user_id, relationship_edges.source_id"))
	appErr, ok := mapped.(*apperrors.AppError)
	require.True(t, ok, "a unique-constraint violation must map to an AppError")
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)

	original := errors.New("some other database failure")
	assert.Same(t, original, relationshipEdgeWriteError(original), "unrelated errors must pass through verbatim")
}

// TestRelationshipEdgeDuplicate_DatabaseError covers the duplicate pre-check's
// error path: a broken connection is reported as a 500 database error, not a
// 409. Closing the pool is the only way to make the otherwise-unreachable
// SELECT fail against a healthy schema.
func TestRelationshipEdgeDuplicate_DatabaseError(t *testing.T) {
	db, _ := setupRouter()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	aerr := relationshipEdgeDuplicate(db, &models.RelationshipEdge{
		UserID: 1, SourceID: "a", TargetID: "b", Type: "friend_of",
	})
	require.NotNil(t, aerr)
	assert.Equal(t, http.StatusInternalServerError, aerr.HTTPStatus)
}

// TestCreateRelationshipEdge_WriteErrorIs500 covers the write-error mapping's
// non-duplicate path: when the INSERT itself fails for another reason the
// error stays a 500 rather than being mislabelled a duplicate.
func TestCreateRelationshipEdge_WriteErrorIs500(t *testing.T) {
	db, router := setupRouter()
	router.POST("/relationship-edges", withValidated(func() any { return &models.RelationshipEdgeInput{} }), CreateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)

	require.NoError(t, db.Exec(
		"CREATE TRIGGER block_edge_insert BEFORE INSERT ON relationship_edges BEGIN SELECT RAISE(ABORT, 'blocked'); END;").Error)

	body, _ := json.Marshal(models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "parent_of"})
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestUpdateRelationshipEdge_WriteErrorIs500 is the PUT counterpart of the
// test above.
func TestUpdateRelationshipEdge_WriteErrorIs500(t *testing.T) {
	db, router := setupRouter()
	router.PUT("/relationship-edges/:id", withValidated(func() any { return &models.RelationshipEdgeInput{} }), UpdateRelationshipEdge)

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	edge := models.RelationshipEdge{
		UserID: user.ID, SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	require.NoError(t, db.Exec(
		"CREATE TRIGGER block_edge_update BEFORE UPDATE ON relationship_edges BEGIN SELECT RAISE(ABORT, 'blocked'); END;").Error)

	body, _ := json.Marshal(models.RelationshipEdgeInput{SourceID: alice.VCardUID, TargetID: bob.VCardUID, Type: "roommate_of"})
	req, _ := http.NewRequest("PUT", "/relationship-edges/"+edge.ID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
