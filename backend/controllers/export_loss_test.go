package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"mycorrhizal/config"
	"mycorrhizal/contactmodel"
	"mycorrhizal/correspondence"
	"mycorrhizal/internal/canonicalfixture"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerPreflightRoute wires GET /export/preflight onto a router the same
// way routes.go does.
func registerPreflightRoute(router *gin.Engine) {
	router.GET("/export/preflight", func(c *gin.Context) {
		c.Set("cfg", config.Config{})
		ExportPreflight(c)
	})
}

// parseLossHeader decodes the X-Mycorrhizal-Export-Loss-Report header the
// export handlers set (issue #442).
func parseLossHeader(t *testing.T, w *httptest.ResponseRecorder) exportLossHeader {
	t.Helper()
	raw := w.Header().Get(exportLossReportHeader)
	require.NotEmpty(t, raw, "export response must carry the loss-report header")
	decoded, err := url.QueryUnescape(raw)
	require.NoError(t, err)
	var hdr exportLossHeader
	require.NoError(t, json.Unmarshal([]byte(decoded), &hdr))
	return hdr
}

// preflightReports returns the decoded diagnostics of a preflight request.
func preflightReports(t *testing.T, router *gin.Engine, query string) models.ExportLossPreflightResponse {
	t.Helper()
	req, _ := http.NewRequest("GET", "/export/preflight"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp models.ExportLossPreflightResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestAdapterForFormat pins the format-token -> adapter mapping and the
// unknown-token default (nil), covering the total switch that every export
// handler and the preflight endpoint route through.
func TestAdapterForFormat(t *testing.T) {
	require.NotNil(t, adapterForFormat(exportFormatVCard4))
	require.NotNil(t, adapterForFormat(exportFormatVCard3))
	require.NotNil(t, adapterForFormat(exportFormatJSContact))
	assert.Nil(t, adapterForFormat("bogus"), "an unknown token must not panic or route to a real adapter")
}

// TestExportPreflight_ReportsEnvelopeLoss pins the milestone criterion: a
// contact with a field that has no home in the target format produces a report
// naming the contact, the field, the format, and the reason — without producing
// the file. The Gender case (issue #515 canary) is the load-bearing example.
func TestExportPreflight_ReportsEnvelopeLoss(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	db.Create(&models.Contact{
		UserID:    user.ID,
		Firstname: "Ada",
		Lastname:  "Lovelace",
		Gender:    "female",
		// Also a populated CRM-only envelope field with no serialized home.
		HowWeMet: "Analytical Engine",
	})

	resp := preflightReports(t, router, "?format=vcard4")

	assert.Equal(t, "vcard4", resp.Format)
	assert.Equal(t, 1, resp.ContactCount)
	require.Len(t, resp.Diagnostics, 2)

	// Which contact, which field, which format, and why.
	gender := resp.Diagnostics[0]
	assert.Equal(t, "Ada Lovelace", gender.ContactName)
	assert.Equal(t, "crm.gender", gender.Concept)
	assert.Equal(t, "vcard4", gender.Format)
	assert.Equal(t, string(correspondence.BucketUnsupported), gender.Bucket)
	assert.NotEmpty(t, gender.Reason, "the report must carry the DATA-01 reason")
	assert.NotEmpty(t, gender.Message, "the report must carry the adapter's own message")

	howWeMet := resp.Diagnostics[1]
	assert.Equal(t, "crm.how_we_met", howWeMet.Concept)
	assert.NotEmpty(t, howWeMet.Reason)
}

// TestExportPreflight_UnknownFormat pins the 400 path: an unknown format token
// is an explicit rejection, not a silent default.
func TestExportPreflight_UnknownFormat(t *testing.T) {
	_, router := setupRouter()
	registerPreflightRoute(router)

	req, _ := http.NewRequest("GET", "/export/preflight?format=bogus", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestExportPreflight_UnknownSection pins that an unknown sections token is a
// 400, exactly like the export handlers it mirrors (a typo must not silently
// narrow the preflight).
func TestExportPreflight_UnknownSection(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	db.Create(&models.Contact{UserID: user.ID, Firstname: "Ada", Lastname: "Lovelace", Gender: "x"})

	req, _ := http.NewRequest("GET", "/export/preflight?format=vcard4&sections=emails,bogus_section", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestExportPreflight_DBError pins the 500 path with operation/category details
// (the milestone v0.6.2 export-failure contract, issue #532): a database
// failure during preflight is identified, not swallowed.
func TestExportPreflight_DBError(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	req, _ := http.NewRequest("GET", "/export/preflight?format=vcard4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assertExportFailureDetails(t, w, "export:preflight", "database")
}

// TestExportPreflight_NoAuth_Unauthorized exercises the early
// currentUserID(c) !ok return branch, mirroring the export handlers' own
// unauthenticated tests.
func TestExportPreflight_NoAuth_Unauthorized(t *testing.T) {
	db, _ := setupRouter()
	router := routerWithoutAuth(db)
	router.GET("/export/preflight", ExportPreflight)

	req, _ := http.NewRequest("GET", "/export/preflight?format=vcard4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// TestExportPreflight_CanonicalFixtureReportsCorrespondToMatrix is the
// end-to-end DATA-01 correspondence assertion (issue #442 requirement 5): drive
// the real preflight computation over the canonical fixture (the pathological
// contact set carrying every field the neutral model can hold) and assert that
// every report produced is a matrix entry — same concept, format, bucket, and
// reason — and that the envelope-loss Gender canary surfaces rather than
// vanishing. This is the "every report must correspond to a matrix entry" half,
// checked on real data through the real shared computation.
func TestExportPreflight_CanonicalFixtureReportsCorrespondToMatrix(t *testing.T) {
	db := dbtest.New(t)
	closeTestDBAtTeardown(t, db)

	m, err := canonicalfixture.Read()
	require.NoError(t, err)
	ds, err := canonicalfixture.Populate(db, m)
	require.NoError(t, err)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", ds.User.ID)
		c.Set("cfg", config.Config{})
		c.Next()
	})
	registerPreflightRoute(router)

	// Build the matrix entry index: concept|format -> (bucket, reason).
	matrix := make(map[string]models.LossReport)
	for _, lr := range correspondence.LossReports() {
		matrix[lr.Concept+"|"+string(lr.Format)] = models.LossReport{
			Concept: lr.Concept, Format: string(lr.Format),
			Bucket: string(lr.Bucket), Reason: lr.Reason,
		}
	}

	for _, format := range []string{"vcard4", "vcard3", "jscontact"} {
		resp := preflightReports(t, router, "?format="+format)
		for _, report := range resp.Diagnostics {
			entry, ok := matrix[report.Concept+"|"+report.Format]
			if !ok {
				t.Errorf("[%s] report for %s/%s has no DATA-01 matrix entry — every report must correspond to a matrix entry",
					format, report.Concept, report.Format)
				continue
			}
			if report.Bucket != entry.Bucket || report.Reason != entry.Reason {
				t.Errorf("[%s] report %s/%s carries bucket %q / reason %q, matrix says %q / %q",
					format, report.Concept, report.Format, report.Bucket, report.Reason, entry.Bucket, entry.Reason)
			}
		}
		// The Gender canary must surface in every format (envelope is never
		// serialized): the issue's "The Gender case surfaces a report rather
		// than vanishing" verification.
		foundGender := false
		for _, report := range resp.Diagnostics {
			if report.Concept == "crm.gender" && report.Format == format {
				foundGender = true
			}
		}
		assert.True(t, foundGender, "[%s] the Gender case must surface a report, not vanish", format)
	}
}

// TestExportPreflight_UserScoping pins that a preflight is scoped to the
// caller's own contacts (backend trap #5).
func TestExportPreflight_UserScoping(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	other := models.User{Username: "other", Password: "password456", Email: "other@example.com"}
	db.Create(&other)

	db.Create(&models.Contact{UserID: user.ID, Firstname: "Mine", Lastname: "One", Gender: "x"})
	db.Create(&models.Contact{UserID: other.ID, Firstname: "Theirs", Lastname: "Two", Gender: "x"})

	resp := preflightReports(t, router, "?format=vcard4")
	assert.Equal(t, 1, resp.ContactCount)
	for _, d := range resp.Diagnostics {
		assert.NotEqual(t, "Theirs Two", d.ContactName, "another user's contact must not appear in the preflight")
	}
}

// TestExportPreflight_SingleContactScope pins the ?vcard_uid= narrowing: a
// preflight restricted to one contact reports only that contact's losses,
// mirroring the export handlers' own scoping.
func TestExportPreflight_SingleContactScope(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	lossy := models.Contact{UserID: user.ID, Firstname: "Lossy", Lastname: "One", Gender: "x"}
	clean := models.Contact{UserID: user.ID, Firstname: "Clean", Lastname: "Two"}
	db.Create(&lossy)
	db.Create(&clean)

	resp := preflightReports(t, router, "?format=vcard4&vcard_uid="+lossy.VCardUID)
	assert.Equal(t, 1, resp.ContactCount)
	require.Len(t, resp.Diagnostics, 1)
	assert.Equal(t, "Lossy One", resp.Diagnostics[0].ContactName, "only the scoped contact's loss is reported")
	assert.Equal(t, "crm.gender", resp.Diagnostics[0].Concept)
}

// TestExportPreflight_NoLossesEmptyArray pins the frontend trap #8 shape: an
// export with nothing to lose reports an empty diagnostics array, never null.
// The assertion reads the raw JSON body — decoding into the Go struct makes
// "absent" and "[]" indistinguishable, which is exactly why the trap exists.
func TestExportPreflight_NoLossesEmptyArray(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	// A contact with only identity data: nothing the matrix classifies as lost.
	db.Create(&models.Contact{UserID: user.ID, Firstname: "Clean", Lastname: "Contact", Email: "c@example.com"})

	req, _ := http.NewRequest("GET", "/export/preflight?format=vcard4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The wire must carry `"diagnostics":[]`, never `"diagnostics":null` or an
	// absent key.
	assert.Contains(t, w.Body.String(), `"diagnostics":[]`, "empty diagnostics must serialize as an array on the wire")
	assert.NotContains(t, w.Body.String(), `"diagnostics":null`)
}

// TestExportPreflight_VCard3VsVCard4FormatSpecificity pins that the report
// depends on the format: a field unsupported only in vCard 3.0 (e.g. a
// populated vCard 4.0-only concept) is reported for vcard3 and not vcard4.
func TestExportPreflight_VCard3VsVCard4FormatSpecificity(t *testing.T) {
	db, router := setupRouter()
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	// Language is a vCard 4.0 concept with no vCard 3.0 home.
	db.Create(&models.Contact{
		UserID:    user.ID,
		Firstname: "Grace",
		Lastname:  "Hopper",
		// Force a persisted Card.Language via the neutral path: the flat
		// Contact has no Language column, so build the record through the
		// same ApplyRecordToContact path the REST API uses.
	})

	// Language isn't reachable from a flat contact's flat fields via
	// RecordFromContact; seed it by writing the Card directly through
	// ApplyRecordToContact with a Language set, then Save.
	contact := models.Contact{UserID: user.ID, Firstname: "Grace", Lastname: "Hopper"}
	models.ApplyRecordToContact(&contact, &contactmodel.Record{Card: contactmodel.Card{
		UID:      "urn:uuid:grace",
		Language: "en",
	}}, "")
	require.NoError(t, db.Create(&contact).Error)

	v4 := preflightReports(t, router, "?format=vcard4")
	var v4Lang bool
	for _, d := range v4.Diagnostics {
		if d.Concept == "language" {
			v4Lang = true
		}
	}
	assert.False(t, v4Lang, "language has a vCard 4.0 home; not a loss there")

	v3 := preflightReports(t, router, "?format=vcard3")
	var v3Lang bool
	for _, d := range v3.Diagnostics {
		if d.Concept == "language" {
			v3Lang = true
		}
	}
	assert.True(t, v3Lang, "language has no vCard 3.0 home; must be reported there")
}

// TestExportLossHeader_AgreesWithPreflight pins the "preflight and the actual
// export agree for unchanged data" criterion: the export response header and
// the preflight endpoint run the same computation and produce the same report
// set for identical data.
func TestExportLossHeader_AgreesWithPreflight(t *testing.T) {
	db, router := setupRouter()
	registerVCFRoute(router, "")
	registerPreflightRoute(router)

	var user models.User
	db.First(&user)
	db.Create(&models.Contact{
		UserID:    user.ID,
		Firstname: "Ada",
		Lastname:  "Lovelace",
		Gender:    "female",
	})

	req, _ := http.NewRequest("GET", "/export/vcf?version=4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	hdr := parseLossHeader(t, w)
	require.False(t, hdr.Truncated)

	pre := preflightReports(t, router, "?format=vcard4")

	require.Equal(t, len(pre.Diagnostics), hdr.Count, "header count and preflight count must agree")
	require.Len(t, hdr.Diagnostics, len(pre.Diagnostics))
	for i := range pre.Diagnostics {
		assert.Equal(t, pre.Diagnostics[i], hdr.Diagnostics[i],
			"preflight and export must produce identical loss reports for unchanged data")
	}
}

// TestExportLossHeader_TruncatesToHeaderBudget pins the header-size bound: the
// header carries the full report when it fits, and a bounded subset plus a
// truncated flag plus the true count when it does not. The complete list stays
// available via preflight.
func TestExportLossHeader_TruncatesToHeaderBudget(t *testing.T) {
	db, router := setupRouter()
	registerVCFRoute(router, "")

	var user models.User
	db.First(&user)
	// Enough lossy contacts that the full report exceeds the header budget.
	for i := 0; i < 200; i++ {
		db.Create(&models.Contact{
			UserID:    user.ID,
			Firstname: "Many",
			Lastname:  "Losses",
			Gender:    "x",
		})
	}

	req, _ := http.NewRequest("GET", "/export/vcf?version=4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	hdr := parseLossHeader(t, w)
	assert.Equal(t, 200, hdr.Count, "count always reflects the true total")
	assert.True(t, hdr.Truncated, "a report exceeding the header budget must be flagged")
	assert.LessOrEqual(t, len(hdr.Diagnostics), 200)
	assert.NotEmpty(t, hdr.Diagnostics, "the bounded header still carries what fit")
}

// TestExportLossHeader_FitsStockProxyHeaderBuffer pins issue #863: the
// URL-encoded loss-report header must fit inside a reverse proxy's *default*
// response-header buffer. nginx — the proxy in this project's shipped
// all-in-one image — holds the entire upstream header block in a single
// proxy_buffer_size buffer whose compiled default is one memory page (4 KB on
// the linux/amd64 Alpine build shipped). The previous 6 KB bound overran that
// on its own, and nginx returned "502 Bad Gateway" in place of the export
// (the CSV export, which sets no such header, kept working — exactly the
// asymmetry the pen test reported). The value must stay well under 4 KB so
// the status line and the dozen other response headers also fit.
func TestExportLossHeader_FitsStockProxyHeaderBuffer(t *testing.T) {
	db, router := setupRouter()
	registerVCFRoute(router, "")

	var user models.User
	db.First(&user)
	// Far more lossy contacts than the bound can carry, each padded with a
	// realistic free-text CRM field so a diagnostic is not artificially small.
	for i := 0; i < 300; i++ {
		db.Create(&models.Contact{
			UserID:    user.ID,
			Firstname: "Many",
			Lastname:  "Losses",
			Gender:    "x",
			HowWeMet:  "met at a conference; long-ish note so the report entry is a realistic size",
		})
	}

	req, _ := http.NewRequest("GET", "/export/vcf?version=4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	raw := w.Header().Get(exportLossReportHeader)
	require.NotEmpty(t, raw)

	hdr := parseLossHeader(t, w)
	require.True(t, hdr.Truncated, "300 lossy contacts must overflow the bound and truncate")
	require.Greater(t, hdr.Count, len(hdr.Diagnostics),
		"count is the true total; diagnostics is the bounded subset that fit")

	// The concrete regression: the wire value must fit a stock 4 KB proxy
	// header buffer with room to spare for every other response header.
	assert.LessOrEqualf(t, len(raw), maxExportLossHeaderBytes,
		"the emitted header (%d bytes) must not exceed its own bound", len(raw))
	assert.LessOrEqual(t, maxExportLossHeaderBytes, 3072,
		"issue #863: keep the bound small enough that the whole response header block fits a 4 KB proxy buffer")
}

// TestExportLossHeader_EmptyCarriesEmptyList pins that an export with nothing
// lost still sets the header — as an empty diagnostics list, not an absent
// key or null — so a client can always read count/diagnostics.
func TestExportLossHeader_EmptyCarriesEmptyList(t *testing.T) {
	db, router := setupRouter()
	registerVCFRoute(router, "")

	var user models.User
	db.First(&user)
	db.Create(&models.Contact{UserID: user.ID, Firstname: "Clean", Lastname: "Contact"})

	req, _ := http.NewRequest("GET", "/export/vcf?version=4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	hdr := parseLossHeader(t, w)
	assert.Equal(t, 0, hdr.Count)
	assert.False(t, hdr.Truncated)
	assert.Empty(t, hdr.Diagnostics)
}

// TestExportLossHeader_SensitivityPolicyExclusionNotReported pins requirement
// 7: a field excluded from the export because it is private/secret is a policy
// exclusion, not a fidelity loss — it must not appear in the loss report
// (either by default or when the file omits it). Conflating the two would
// teach users to ignore the report.
func TestExportLossHeader_SensitivityPolicyExclusionNotReported(t *testing.T) {
	db, router := setupRouter()
	registerVCFRoute(router, "")

	var user models.User
	db.First(&user)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice", Lastname: "Anderson"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob", Lastname: "Brown"}
	db.Create(&alice)
	db.Create(&bob)
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID:      user.ID,
		SourceID:    alice.VCardUID,
		TargetID:    bob.VCardUID,
		Type:        "spouse_of",
		Source:      models.RelationshipSourceUserConfirmed,
		Confidence:  1.0,
		Status:      models.RelationshipStatusConfirmed,
		Sensitivity: models.RelationshipSensitivitySecret,
	}).Error)

	// Default export: the secret edge is excluded by policy, and it must not
	// surface as a loss report either.
	req, _ := http.NewRequest("GET", "/export/vcf?version=4&sections=related_to", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "RELATED", "policy exclusion: the secret edge is not in the file")
	hdr := parseLossHeader(t, w)
	assert.Equal(t, 0, hdr.Count, "a policy-excluded secret edge is not fidelity loss and must not be reported")
}
