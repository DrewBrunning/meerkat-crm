package controllers

// Issue #861: the flat CSV export (`GET /api/v1/export`) deliberately carries
// EVERY sensitivity and EVERY status, while the vCard/JSContact exporters
// filter both. That asymmetry is the design (ExportData's three inline
// comments, `openapi.yaml`'s `/export` description, and
// `docs/security/data-retention-lifecycle.md` §11): the CSV is the user's own
// full personal-data backup landing on their own device, not a projection
// that can leave the instance, so holding data back there is silent data loss
// in the one full-fidelity export the app offers. The neutral-Card exporters
// are the surface that can reach another party or another system, so they
// default-deny and require the explicit `?include_sensitive=true` opt-in.
//
// A pen-test read the *documentation* — which claimed the blanket rule
// "anything above normal is excluded from exports" — and correctly reported
// the CSV as a leak. The docs were wrong, not the code. This file exists so
// the asymmetry is a pinned, self-describing decision rather than something a
// future reader has to reconstruct from prose: both halves are asserted in
// one test, against the real migrated schema, over the same seeded data.
//
// If you are here because you intend to make the CSV filter, that is a policy
// change, not a bug fix: it needs the docs above changed with it, and a
// decision about what the user's backup is allowed to omit.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// csvFullFidelityFixture seeds one user with, for each sensitivity-bearing
// surface the CSV writes, a `normal` control alongside the above-normal rows.
// The controls are what stop a "sensitive value absent" assertion from
// passing vacuously in the vCard half: if the control is missing too, the
// export is simply broken and the test says so.
type csvFullFidelityFixture struct {
	router *gin.Engine
	user   models.User
	ada    models.Contact
	bob    models.Contact
}

func seedCSVFullFidelity(t *testing.T) csvFullFidelityFixture {
	t.Helper()

	db := dbtest.New(t)

	user := models.User{Username: "csvfidelity", Password: "password123!A", Email: "csvfidelity@example.com"}
	require.NoError(t, db.Create(&user).Error)

	ada := models.Contact{UserID: user.ID, Firstname: "Ada", Lastname: "Lovelace"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob", Lastname: "Smith"}
	cyd := models.Contact{UserID: user.ID, Firstname: "Cyd", Lastname: "Charisse"}
	dee := models.Contact{UserID: user.ID, Firstname: "Dee", Lastname: "Dixon"}
	for _, c := range []*models.Contact{&ada, &bob, &cyd, &dee} {
		require.NoError(t, db.Create(c).Error)
	}

	edge := func(source, target models.Contact, edgeType, status, sensitivity string) {
		t.Helper()
		require.NoError(t, db.Create(&models.RelationshipEdge{
			UserID: user.ID, SourceID: source.VCardUID, TargetID: target.VCardUID,
			Type: edgeType, Directional: true,
			Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0,
			Status: status, Sensitivity: sensitivity,
		}).Error)
	}
	// The control, plus one row per property the CSV is claimed to withhold.
	edge(ada, bob, "friend_of", models.RelationshipStatusConfirmed, models.RelationshipSensitivityNormal)
	edge(ada, cyd, "spouse_of", models.RelationshipStatusConfirmed, models.RelationshipSensitivityPrivate)
	edge(ada, dee, "mentor_of", models.RelationshipStatusConfirmed, models.RelationshipSensitivitySecret)
	edge(bob, cyd, "parent_of", models.RelationshipStatusSuggested, models.RelationshipSensitivityNormal)

	customField := func(label, key, value, sensitivity string) {
		t.Helper()
		def := models.FieldDefinition{
			UserID: user.ID, Label: label, Key: key, Target: "contact", Type: "string",
			Projection: "vcard:X-" + strings.ToUpper(key), Sensitivity: sensitivity,
		}
		require.NoError(t, db.Create(&def).Error)
		require.NoError(t, db.Create(&models.FieldValue{
			FieldDefinitionID: def.ID, UserID: user.ID, EntityID: ada.VCardUID,
			Value: json.RawMessage(`"` + value + `"`),
		}).Error)
	}
	customField("Normal Field", "normal_field", csvFidelityNormalFieldValue, models.RelationshipSensitivityNormal)
	customField("Secret Field", "secret_field", csvFidelitySecretFieldValue, models.RelationshipSensitivitySecret)

	preference := func(value, sensitivity string) {
		t.Helper()
		require.NoError(t, db.Create(&models.Preference{
			UserID: user.ID, EntityID: ada.VCardUID,
			Category: models.PreferenceCategoryFood, Key: "favorite",
			Value: value, Source: models.PreferenceSourceUser, Sensitivity: sensitivity,
		}).Error)
	}
	preference(csvFidelityNormalPreference, models.RelationshipSensitivityNormal)
	preference(csvFidelitySecretPreference, models.RelationshipSensitivitySecret)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", user.ID)
		c.Next()
	})
	router.GET("/export", ExportData)
	registerVCFRoute(router, "")

	return csvFullFidelityFixture{router: router, user: user, ada: ada, bob: bob}
}

const (
	csvFidelityNormalFieldValue  = "ordinary custom value"
	csvFidelitySecretFieldValue  = "top secret custom value"
	csvFidelityNormalPreference  = "Ordinary Ramen"
	csvFidelitySecretPreference  = "Secret Souffle"
	csvFidelityRelationshipsHead = "=== RELATIONSHIPS ==="
)

func csvFidelityGet(t *testing.T, router *gin.Engine, path string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "%s must succeed", path)
	return w.Body.String()
}

// relationshipsSection returns just the CSV's `=== RELATIONSHIPS ===` block,
// so a `spouse_of` assertion cannot be satisfied by some other section that
// happens to mention the same word.
func relationshipsSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, csvFidelityRelationshipsHead)
	require.GreaterOrEqual(t, start, 0, "the CSV export must have a relationships section")
	rest := body[start+len(csvFidelityRelationshipsHead):]
	if next := strings.Index(rest, "\n=== "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// TestExportCSV_IsFullFidelityBackup_UnlikeVCard asserts both halves of the
// #861 decision at once: the CSV withholds nothing, and the vCard projection
// of the same data withholds exactly the private/secret rows.
func TestExportCSV_IsFullFidelityBackup_UnlikeVCard(t *testing.T) {
	f := seedCSVFullFidelity(t)

	t.Run("csv carries every sensitivity and status", func(t *testing.T) {
		body := csvFidelityGet(t, f.router, "/export")
		edges := relationshipsSection(t, body)

		// Control first: a plain confirmed/normal edge is present, so the
		// assertions below are testing the filter, not an empty section.
		assert.Contains(t, edges, "friend_of", "the normal/confirmed control edge must be exported")

		assert.Contains(t, edges, "spouse_of", "a private edge belongs in the user's own backup")
		assert.Contains(t, edges, "mentor_of", "a secret edge belongs in the user's own backup")
		assert.Contains(t, edges, "parent_of", "a suggested edge belongs in the user's own backup")

		// The sensitivity and status of every row travels with it, which is
		// what makes including them honest rather than misleading: a consumer
		// can re-apply the classification, and a `suggested` row is never
		// presented as fact.
		assert.Contains(t, edges, models.RelationshipSensitivityPrivate)
		assert.Contains(t, edges, models.RelationshipSensitivitySecret)
		assert.Contains(t, edges, models.RelationshipStatusSuggested)

		assert.Contains(t, body, csvFidelityNormalFieldValue, "the normal custom-field control must be exported")
		assert.Contains(t, body, csvFidelitySecretFieldValue, "a secret custom-field value belongs in the user's own backup")
		assert.Contains(t, body, csvFidelityNormalPreference, "the normal food-preference control must be exported")
		assert.Contains(t, body, csvFidelitySecretPreference, "a secret food preference belongs in the user's own backup")
	})

	t.Run("vcard default-denies the same rows", func(t *testing.T) {
		body := csvFidelityGet(t, f.router, "/export/vcf")

		// Control: the normal edge projects, so the absences below are the
		// sensitivity filter and not a dead projection path.
		assert.Contains(t, body, "RELATED", "the normal/confirmed control edge must project to vCard")
		assert.Contains(t, body, "X-NORMAL_FIELD", "the normal custom-field control must project to vCard")

		assert.NotContains(t, body, "X-SECRET_FIELD", "a secret custom field must not leave the instance without an opt-in")
		assert.NotContains(t, body, csvFidelitySecretFieldValue)
		assert.NotContains(t, body, csvFidelitySecretPreference)
	})

	t.Run("vcard opt-in includes what the csv includes unconditionally", func(t *testing.T) {
		body := csvFidelityGet(t, f.router, "/export/vcf?include_sensitive=true")
		assert.Contains(t, body, csvFidelitySecretFieldValue, "include_sensitive=true is the vCard path's explicit opt-in")
	})
}
