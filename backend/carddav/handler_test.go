package carddav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHandlerDiscovery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Contact{}))

	handler := NewHandler(db, t.TempDir())
	h := handler.GinHandler()

	propfindBody := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:current-user-principal/>
    <D:resourcetype/>
  </D:prop>
</D:propfind>`

	cases := []struct {
		name     string
		path     string
		wantCode int
	}{
		{name: "principal PROPFIND", path: "/carddav/principals/alice/", wantCode: http.StatusMultiStatus},
		{name: "root PROPFIND", path: "/carddav/", wantCode: http.StatusMultiStatus},
		{name: "bare root", path: "/carddav", wantCode: http.StatusMultiStatus},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PROPFIND", tc.path, strings.NewReader(propfindBody))
			req.Header.Set("Depth", "0")
			req.Header.Set("Content-Type", "application/xml")

			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Set("userID", uint(1))
			c.Set("username", "alice")
			h(c)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestWellKnownRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/.well-known/carddav", nil)

	WellKnownRedirect(c)

	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/carddav/", w.Header().Get("Location"))
}

func TestHandlerDelegatesToWebDAV(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Contact{}))

	handler := NewHandler(db, t.TempDir())
	h := handler.GinHandler()

	// A non-principal, non-root path falls through to go-webdav's own handler.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PROPFIND", "/carddav/addressbooks/alice/contacts/", strings.NewReader(
		`<?xml version="1.0" encoding="utf-8" ?><D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/></D:prop></D:propfind>`))
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")

	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)

	// go-webdav returns 207 Multi-Status for a valid addressbook PROPFIND.
	assert.Equal(t, http.StatusMultiStatus, w.Code)
}

// TestHandlerFilterLessAddressBookQuery pins TEST-09's first real-client find
// at the wire level: a filter-less addressbook-query REPORT (RFC 6352 §8.6 —
// exactly how Thunderbird, Apple Contacts, DAVx5 and vdirsyncer enumerate a
// collection) used to come back as an empty multistatus because go-webdav's
// Match() treats an empty FilterAnyOf as "match nothing". Runs against a REAL
// migrated schema (CLAUDE.md backend trap 1) so the query path is the one
// production serves.
func TestHandlerFilterLessAddressBookQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	backend := NewBackend(db, t.TempDir())
	ctx := ContextWithUser(context.Background(), 1, "alice", db, backend.photoDir, "")
	_, err := backend.PutAddressObject(ctx, "/carddav/addressbooks/alice/contacts/wire-uid-1.vcf",
		mustParseVCard(t, simpleVCard4("wire-uid-1", "Wire One")), nil)
	require.NoError(t, err)

	h := NewHandler(db, t.TempDir()).GinHandler()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("REPORT", "/carddav/addressbooks/alice/contacts/", strings.NewReader(
		`<?xml version="1.0" encoding="utf-8" ?>
<card:addressbook-query xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav">
  <d:prop><d:getetag/><card:address-data/></d:prop>
</card:addressbook-query>`))
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")

	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)

	require.Equal(t, http.StatusMultiStatus, w.Code)
	assert.Contains(t, w.Body.String(), "wire-uid-1.vcf",
		"a filter-less addressbook-query must return the whole collection over the wire")
}

// TestHandlerMKCOLUnsupportedCollectionForbidden pins that a real client
// probing for collection creation (vdirsyncer's discover, DAVx5's new-account
// wizard) gets a clean 403, not a 500 Internal Server Error. A 500 makes
// clients report a broken server; a 403 is "this server is single-addressbook
// by design" and lets them fall back to the existing collection.
func TestHandlerMKCOLUnsupportedCollectionForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	h := NewHandler(db, t.TempDir()).GinHandler()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("MKCOL", "/carddav/addressbooks/alice/other/", strings.NewReader(
		`<?xml version="1.0" encoding="utf-8" ?>
<D:mkcol xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:set><D:prop><D:resourcetype><D:collection/><C:addressbook/></D:resourcetype></D:prop></D:set>
</D:mkcol>`))
	req.Header.Set("Content-Type", "application/xml")

	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)

	assert.Equal(t, http.StatusForbidden, w.Code,
		"MKCOL for an unsupported second address book must be a clean 403, not a 500")
}

// TestHandlerGetMissingCardNotFound pins TEST-09's second real-client find:
// a real client that has deleted (or never seen) a card GETs it and expects a
// 404. vdirsyncer's delete round-trip hit a 500 here because
// GetAddressObject/DeleteAddressObject returned a plain error, which go-webdav
// maps to Internal Server Error. Clients treat "500" as "the server is broken"
// and "404" as "the card is gone" — the distinction is interop-visible.
func TestHandlerGetMissingCardNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	handler := NewHandler(db, t.TempDir())
	ctx := ContextWithUser(context.Background(), 1, "alice", db, handler.photoDir, "")
	_, err := handler.backend.PutAddressObject(ctx, "/carddav/addressbooks/alice/contacts/live-uid-1.vcf",
		mustParseVCard(t, simpleVCard4("live-uid-1", "Live One")), nil)
	require.NoError(t, err)
	h := handler.GinHandler()

	// A card that exists serves 200...
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/carddav/addressbooks/alice/contacts/live-uid-1.vcf", nil)
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)
	assert.Equal(t, http.StatusOK, w.Code)

	// ...and a card that is gone serves 404, not 500.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/carddav/addressbooks/alice/contacts/nope-missing.vcf", nil)
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)
	assert.Equal(t, http.StatusNotFound, w.Code,
		"GET of a missing card must be a 404 (the interop-correct 'gone'), not a 500")

	// A path with no card (the collection root) is malformed: 400 for GET.
	// DELETE of the collection root is routed to DeleteAddressBook (403) by
	// go-webdav's resource-type dispatch, so the DeleteAddressObject
	// invalid-path branch is reached via a card-shaped path whose base is a
	// reserved word (e.g. "contacts.vcf").
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/carddav/addressbooks/alice/contacts/", nil)
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)
	assert.Equal(t, http.StatusBadRequest, w.Code, "GET of the collection root is an invalid path")

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodDelete, "/carddav/addressbooks/alice/contacts/contacts.vcf", nil)
	c.Set("userID", uint(1))
	c.Set("username", "alice")
	h(c)
	assert.Equal(t, http.StatusBadRequest, w.Code, "DELETE of a reserved-word card path is an invalid path")
}

// TestHandlerCrossUserAddressBookPropfindNotFound pins issue #874's credentialed
// pen-test finding: authenticated as one principal, a PROPFIND on another
// principal's (or any non-own) collection path used to return HTTP 500 with the
// internal string "address book not found" as a plaintext body, because
// GetAddressBook returned a bare fmt.Errorf that the DAV layer maps to Internal
// Server Error. Isolation was never at risk — every path resolves to the
// authenticated principal — but a foreign/unknown collection must render as a
// clean 404, consistent with a missing card's 404 and the REST layer's
// structured errors.
func TestHandlerCrossUserAddressBookPropfindNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	h := NewHandler(db, t.TempDir()).GinHandler()

	propfind := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/></D:prop></D:propfind>`

	cases := []struct {
		name string
		path string
	}{
		{name: "another principal's collection", path: "/carddav/addressbooks/pentest1/contacts/"},
		{name: "unknown collection under own principal", path: "/carddav/addressbooks/pentest2/other/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PROPFIND", tc.path, strings.NewReader(propfind))
			req.Header.Set("Depth", "1")
			req.Header.Set("Content-Type", "application/xml")

			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Set("userID", uint(2))
			c.Set("username", "pentest2")
			h(c)

			assert.Equal(t, http.StatusNotFound, w.Code,
				"a foreign/unknown collection PROPFIND must be a clean 404, not a 500")
			assert.NotEqual(t, http.StatusInternalServerError, w.Code)
		})
	}
}
