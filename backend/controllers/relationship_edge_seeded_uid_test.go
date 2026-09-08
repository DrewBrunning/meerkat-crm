package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mycorrhizal/internal/canonicalfixture"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/internal/largedata"
	"mycorrhizal/middleware"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateRelationshipEdge_SeededContactUIDPassesRealValidation is the
// closing-the-loop regression test for issue #868: a contact seeded via
// internal/largedata (the same generator PERF-01/02/03, the large-dataset
// migration test, and cmd/pentestseed use) carries a card UID that must
// satisfy RelationshipEdgeInput's real `uuid4` validation and reach
// resolveRelationshipEndpoint's ownership-resolution query, not fail at the
// validator the way it did before #868 (regeneratedUID produced a v5 UUID,
// which uuid4 rejects with a 400 naming SourceID/TargetID as invalid).
//
// This wires the real middleware.ValidateJSONMiddleware(&models.
// RelationshipEdgeInput{}) — the exact middleware routes/routes.go installs
// on POST /relationship-edges — rather than a test-only validation bypass,
// so the assertion cannot pass for the wrong reason. It uses dbtest.New(t)
// (a real database.InitDB-migrated schema, CLAUDE.md backend trap #1), and
// seeds contacts through canonicalfixture.Populate(db, scaled) exactly like
// the large-dataset tests do (CLAUDE.md backend trap #2 — ApplyRecordToContact,
// not a direct field mutation).
func TestCreateRelationshipEdge_SeededContactUIDPassesRealValidation(t *testing.T) {
	db := dbtest.New(t)

	m, err := canonicalfixture.Read()
	require.NoError(t, err)
	scaled, err := largedata.Scale(m, largedata.MinContacts) // one manifest block (27 contacts)
	require.NoError(t, err)
	ds, err := canonicalfixture.Populate(db, scaled)
	require.NoError(t, err)

	ada, ok := ds.Contacts["ada_000000"]
	require.True(t, ok, "the scaled manifest must carry a block-0 ada")
	bob, ok := ds.Contacts["bob_000000"]
	require.True(t, ok, "the scaled manifest must carry a block-0 bob")

	// Sanity-check the premise directly against the real DTO validator before
	// going through HTTP: a seeded contact's vcard_uid must satisfy the same
	// uuid4 tag RelationshipEdgeInput.SourceID/TargetID declare.
	require.True(t, middleware.ValidateVar(ada.VCardUID, "uuid4"),
		"seeded contact ada's vcard_uid %q must satisfy uuid4", ada.VCardUID)
	require.True(t, middleware.ValidateVar(bob.VCardUID, "uuid4"),
		"seeded contact bob's vcard_uid %q must satisfy uuid4", bob.VCardUID)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", ds.User.ID)
		c.Next()
	})
	router.POST("/relationship-edges", middleware.ValidateJSONMiddleware(&models.RelationshipEdgeInput{}), CreateRelationshipEdge)

	payload := models.RelationshipEdgeInput{SourceID: ada.VCardUID, TargetID: bob.VCardUID, Type: "friend_of"}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req, _ := http.NewRequest("POST", "/relationship-edges", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Before #868 this returned 400 with a validation error naming SourceID
	// (or TargetID) as invalid, and CreateRelationshipEdge's ownership
	// resolution was never reached at all.
	require.Equal(t, http.StatusCreated, w.Code, "seeded contact UIDs must pass real DTO validation and reach ownership resolution: %s", w.Body.String())

	var created struct {
		RelationshipEdge models.RelationshipEdge `json:"relationship_edge"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, ada.VCardUID, created.RelationshipEdge.SourceID, "ownership resolution must have matched the seeded source contact")
	assert.Equal(t, bob.VCardUID, created.RelationshipEdge.TargetID, "ownership resolution must have matched the seeded target contact")
	assert.Equal(t, "friend_of", created.RelationshipEdge.Type)

	// The edge must actually be persisted, scoped to the seeded user -- not
	// just echoed back in the response.
	var persisted models.RelationshipEdge
	require.NoError(t, db.Where("id = ? AND user_id = ?", created.RelationshipEdge.ID, ds.User.ID).First(&persisted).Error)
	assert.Equal(t, ada.VCardUID, persisted.SourceID)
	assert.Equal(t, bob.VCardUID, persisted.TargetID)
}
