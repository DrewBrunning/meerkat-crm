package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"

	"mycorrhizal/contactmodel"
	"mycorrhizal/internal/canonicalfixture"
	"mycorrhizal/internal/semanticequal"
	"mycorrhizal/models"
	"mycorrhizal/vcard4"

	"github.com/emersion/go-webdav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Real-server CardDAV integration suite (issues #496 + #681): the same full
// sync lifecycle + TEST-02 fixture round trip the fake suite drives, but
// against a REAL CardDAV server in Docker. A fake encodes our own
// understanding of the protocol — this suite exists because only a real
// server can contradict us.
//
// The server is selectable via MYCORRHIZAL_CARDDAV_SERVER_ID (issue #681,
// TEST-09 direction 2): "radicale" (default, the original #496 choice),
// "baikal", "nextcloud" or "davical". The differences that matter between
// them are captured in referenceServerLayout (collection URL + card href
// shape) and the per-server divergence register below. SabreDAV-based
// servers (baikal/nextcloud) are served through Sabre VObject which
// re-serializes vCard 4.0 as vCard 3.0 (our go-webdav client cannot
// negotiate an address-data version), and the register documents exactly
// what that re-serialization changes; anything else that diverges is a real
// failure.
//
// Gating: the test SKIPS unless MYCORRHIZAL_CARDDAV_SERVER_URL is set, so it
// never runs as part of the normal Go suite. The carddav-e2e.yml workflow
// starts the server, sets the env, and runs `go test -run
// TestCardDAVReferenceServer`. It is scheduled (nightly) and path-gated via
// .github/filters.yaml (carddav filter), not run per-PR — the fake suite
// stays the fast deterministic gate.
// ---------------------------------------------------------------------------

// referenceServerEnv reads the real-server connection info from the
// environment. MYCORRHIZAL_RADICALE_* are accepted as fallbacks so an
// existing setup keeps working unchanged.
func referenceServerEnv(t *testing.T) (serverID, baseURL, user, password string) {
	t.Helper()
	serverID = os.Getenv("MYCORRHIZAL_CARDDAV_SERVER_ID")
	if serverID == "" {
		serverID = "radicale"
	}
	baseURL = strings.TrimRight(os.Getenv("MYCORRHIZAL_CARDDAV_SERVER_URL"), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(os.Getenv("MYCORRHIZAL_RADICALE_URL"), "/")
	}
	if baseURL == "" {
		t.Skip("MYCORRHIZAL_CARDDAV_SERVER_URL not set — this suite runs against a real CardDAV server via .github/workflows/carddav-e2e.yml")
	}
	user = os.Getenv("MYCORRHIZAL_CARDDAV_USER")
	if user == "" {
		user = os.Getenv("MYCORRHIZAL_RADICALE_USER")
	}
	if user == "" {
		user = "syncuser"
	}
	password = os.Getenv("MYCORRHIZAL_CARDDAV_PASSWORD")
	if password == "" {
		password = os.Getenv("MYCORRHIZAL_RADICALE_PASSWORD")
	}
	if password == "" {
		password = "syncsecret"
	}
	return serverID, baseURL, user, password
}

// referenceServerLayout captures the server-specific differences the suite
// must adapt to. Everything else (PROPFIND listing, PUT, DELETE, sync-
// collection, multiget) is driven the same way against every server.
type referenceServerLayout struct {
	id string
	// collectionPath is the full URL of the sync user's address book.
	collectionPath func(baseURL, user string) string
	// cardHref is the href the server reports in its multistatus responses
	// (absolute URL path, not full URL) — the value ContactSyncLink.Href
	// stores, which is why the suite must know the exact shape.
	cardHref func(user, uid string) string
	// mkcol is true for servers where the address book collection must be
	// created explicitly (Radicale starts empty); SabreDAV-based servers
	// have the collection pre-provisioned.
	mkcol bool
}

var referenceServerLayouts = map[string]referenceServerLayout{
	"radicale": {
		id: "radicale",
		collectionPath: func(baseURL, user string) string {
			return baseURL + "/" + url.PathEscape(user) + "/contacts/"
		},
		cardHref: func(user, uid string) string {
			return "/" + url.PathEscape(user) + "/contacts/" + uid + ".vcf"
		},
		mkcol: true,
	},
	"baikal": {
		id: "baikal",
		collectionPath: func(baseURL, user string) string {
			return baseURL + "/dav.php/addressbooks/" + url.PathEscape(user) + "/contacts/"
		},
		cardHref: func(user, uid string) string {
			return "/dav.php/addressbooks/" + url.PathEscape(user) + "/contacts/" + uid + ".vcf"
		},
	},
	"nextcloud": {
		id: "nextcloud",
		collectionPath: func(baseURL, user string) string {
			return baseURL + "/remote.php/dav/addressbooks/users/" + url.PathEscape(user) + "/contacts/"
		},
		cardHref: func(user, uid string) string {
			return "/remote.php/dav/addressbooks/users/" + url.PathEscape(user) + "/contacts/" + uid + ".vcf"
		},
	},
	"davical": {
		id: "davical",
		collectionPath: func(baseURL, user string) string {
			return baseURL + "/caldav.php/" + url.PathEscape(user) + "/contacts/"
		},
		cardHref: func(user, uid string) string {
			return "/caldav.php/" + url.PathEscape(user) + "/contacts/" + uid + ".vcf"
		},
	},
}

// serverDivergence is one fixture contact a real server does not round-trip
// faithfully, and why. concepts is the ";"-joined expected divergent concept
// ids; "" means the server REJECTED the card at PUT.
type serverDivergence struct {
	name     string
	concepts string
	reason   string
}

// referenceServerDivergences returns the divergence register for a server.
// radicale's register is the original #496 find; the SabreDAV-based servers
// (baikal/nextcloud) re-serialize vCard 4.0 as vCard 3.0 on output (Sabre
// VObject) because our go-webdav client cannot negotiate an address-data
// version (go-webdav v0.7.0 AddressDataRequest has no version field — a
// documented client limitation, see docs/development/testing.md). The 3.0
// downgrade moves the 4.0-only concepts into the passthrough and derives
// name.full from FN; the register pins exactly which concepts each fixture
// contact is affected in. Anything OUTSIDE this set is a real failure.
func referenceServerDivergences(serverID string) []serverDivergence {
	switch serverID {
	case "radicale":
		return []serverDivergence{
			{name: "celine", reason: "rejected by Radicale: exported card carries two N properties (RFC 9554 §3.3 alternative-name ALTID); vobject refuses >1 N"},
			{name: "bob", concepts: "adr;photo", reason: "vobject re-serializes on output: inline data: PHOTO truncated at the first ';' and ADR components beyond the seven RFC 6350 slots (apartment, floor) dropped"},
			// I18N-01 (issue #484): compound-surname and extended-granularity
			// address personas hit the same vobject-only-knows-RFC-6350 wall as
			// bob. Radicale stores the 4.0 card otherwise verbatim (KIND,
			// LANGUAGE, ADR CC/TZ all survive), so the divergence is confined to
			// the component counts vobject truncates.
			{name: "carmen", concepts: "name.surname2", reason: "vobject re-serializes on output: N truncated to the five RFC 6350 components, dropping the RFC 9554 6th (secondary surname)"},
			{name: "joao", concepts: "name.surname2", reason: "vobject re-serializes on output: N truncated to the five RFC 6350 components, dropping the RFC 9554 6th (secondary surname)"},
			{name: "somchai", concepts: "adr", reason: "vobject re-serializes on output: ADR components beyond the seven RFC 6350 slots (subdistrict, district) dropped"},
		}
	case "baikal", "nextcloud":
		// The accepted-card divergences are byte-identical between Baikal and
		// Nextcloud (both serve through Sabre VObject, which re-serializes
		// vCard 4.0 as vCard 3.0 because our go-webdav client cannot negotiate
		// an address-data version — a documented client limitation, see
		// docs/development/testing.md). Nextcloud additionally REJECTS eve at
		// PUT (its VObject re-validates BDAY as an iCalendar datetime and
		// rejects eve's historical year-1000 birthday), so the registers differ
		// only in eve.
		common := []serverDivergence{
			{name: "celine", reason: "rejected: exported card carries two N properties (RFC 9554 §3.3 alternative-name ALTID); Sabre VObject rejects >1 N"},
			{name: "ada", concepts: "adr;adr.tz;created;gramgender;hobby;impp;interest;kind;language;member;prodid;pronouns;pt.vcard;related;social", reason: "Sabre VObject re-serializes the vCard 4.0 export as vCard 3.0: 4.0-only concepts (kind, created, language, social, gramgender, pronouns, hobby, interest, related, member, adr CC/TZ, impp scheme) land in passthrough, prodid added"},
			{name: "bob", concepts: "adr;impp;kind;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: adr/photo structure and impp land in passthrough"},
			{name: "dmitri", concepts: "kind;name.full;prodid", reason: "Sabre VObject 3.0 downgrade; the vcard3 importer derives name.full from FN where the neutral record stores only components"},
			{name: "frank", concepts: "kind;prodid", reason: "Sabre VObject 3.0 downgrade"},
			{name: "hugo", concepts: "adr;kind;prodid", reason: "Sabre VObject 3.0 downgrade: adr CC/TZ params dropped"},
			{name: "ida", concepts: "kind;prodid", reason: "Sabre VObject 3.0 downgrade"},
			{name: "julie", concepts: "kind;name.full;prodid", reason: "Sabre VObject 3.0 downgrade; the vcard3 importer derives name.full from FN where the neutral record stores only components"},
			{name: "test07_country_code_only_address", concepts: "adr;kind;name.full;prodid", reason: "Sabre VObject 3.0 downgrade: the country-only ADR loses its country code"},
			{name: "test07_duplicate_keywords", concepts: "kind;name.full;prodid", reason: "Sabre VObject 3.0 downgrade; name.full derived from FN"},
			{name: "test07_empty_note_with_params", concepts: "kind;name.full;note;prodid", reason: "Sabre VObject 3.0 downgrade: an empty NOTE carrying parameters is dropped by VObject"},
			{name: "test07_multi_grammatical_gender", concepts: "gramgender;kind;name.full;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: GRAMGENDER survives only as passthrough"},
			{name: "test07_timestamped_birthday", concepts: "kind;name.full;prodid", reason: "Sabre VObject 3.0 downgrade; name.full derived from FN"},

			// I18N-01 (issue #484): every international persona carries KIND,
			// per-record LANGUAGE and an ADR with a CC + per-address TZ param —
			// all 4.0-only, so the Sabre VObject 3.0 downgrade drops the ADR
			// CC/TZ from the structured value and lands LANGUAGE in passthrough,
			// exactly the ada/hugo pattern. name.full survives (their FN matches
			// the recomposed components). carmen/joao additionally lose the
			// RFC 9554 N 6th component (secondary surname); naoki's ADR has no
			// TZ in the fixture, so it has no adr.tz divergence.
			{name: "naoki", concepts: "adr;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC dropped, prodid added"},
			{name: "wei", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "minjun", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "layla", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "yael", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "bjork", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "carmen", concepts: "adr;adr.tz;kind;language;name.surname2;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, N truncated to five RFC 6350 components dropping the RFC 9554 6th (secondary surname), prodid added"},
			{name: "joao", concepts: "adr;adr.tz;kind;language;name.surname2;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, N truncated to five RFC 6350 components dropping the RFC 9554 6th (secondary surname), prodid added"},
			{name: "jan", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "somchai", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params and the RFC 9554 extended components (subdistrict, district) dropped, prodid added"},
			{name: "priya", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
			{name: "aoife", concepts: "adr;adr.tz;kind;language;prodid;pt.vcard", reason: "Sabre VObject 3.0 downgrade: KIND/LANGUAGE 4.0-only (LANGUAGE lands in passthrough), ADR CC/TZ params dropped, prodid added"},
		}
		if serverID == "baikal" {
			// eve is ACCEPTED by Baikal but its BDAY (no value-type) survives
			// only as passthrough under the 3.0 downgrade.
			common = append(common, serverDivergence{
				name: "eve", concepts: "anniversary.birth;kind;prodid;pt.vcard",
				reason: "Sabre VObject 3.0 downgrade: BDAY without a value-type survives only as passthrough",
			})
		} else {
			// ... and REJECTED outright by Nextcloud.
			common = append(common, serverDivergence{
				name:   "eve",
				reason: "additionally REJECTED by Nextcloud at PUT: its VObject re-validates BDAY as an iCalendar datetime and rejects eve's historical year-1000 birthday ('The supplied iCalendar datetime value is incorrect')",
			})
		}
		return common
	default:
		return nil
	}
}

// referenceRequest does an authenticated HTTP request against the reference
// server, returning the response (caller closes the body). rawURL is a full
// URL.
func referenceRequest(t *testing.T, user, password, method, rawURL, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	require.NoError(t, err)
	switch method {
	case http.MethodPut:
		req.Header.Set("Content-Type", "text/vcard; charset=utf-8")
	case "MKCOL":
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	req.SetBasicAuth(user, password)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "%s %s must reach the server", method, rawURL)
	return resp
}

// referencePut stages one vCard on the reference server and returns the
// server's ETag (unquoted), so the test can assert it landed in the real
// etag column. fullURL is the complete resource URL.
func referencePut(t *testing.T, user, password, fullURL, raw string) string {
	t.Helper()
	resp := referenceRequest(t, user, password, http.MethodPut, fullURL, raw)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, []int{http.StatusCreated, http.StatusNoContent}, resp.StatusCode,
		"PUT %s failed: %s (%s)", fullURL, resp.Status, body)
	return strings.Trim(resp.Header.Get("ETag"), `"`)
}

// referenceEnsureCollection idempotently creates the address book collection
// for servers that need it (Radicale starts with no collections and does not
// discover them for us). SabreDAV-based servers are pre-provisioned, so this
// is a no-op for them. The user's principal is created implicitly on first
// access and is deliberately NOT mkcol'd — the principal resource itself is
// protected (403), which is correct.
func referenceEnsureCollection(t *testing.T, serverID, user, password, collectionPath string) {
	t.Helper()
	if serverID != "radicale" {
		return
	}
	mkcol := `<?xml version="1.0" encoding="utf-8" ?>
<D:mkcol xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:set><D:prop>
    <D:resourcetype><D:collection/><C:addressbook/></D:resourcetype>
  </D:prop></D:set>
</D:mkcol>`
	resp := referenceRequest(t, user, password, "MKCOL", collectionPath, mkcol)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Contains(t, []int{http.StatusCreated, http.StatusMethodNotAllowed, http.StatusConflict}, resp.StatusCode,
		"MKCOL %s failed: %s (%s)", collectionPath, resp.Status, body)
}

// referenceDeleteAll empties the collection so each run starts clean
// regardless of what a previous (possibly failed) run left behind. It lists
// via PROPFIND (ReadDir) rather than addressbook-query, which some servers
// reject for filter-less queries.
func referenceDeleteAll(t *testing.T, baseURL, collectionPath, user, password string) {
	t.Helper()
	parsed, err := url.Parse(collectionPath)
	require.NoError(t, err)
	client, err := webdav.NewClient(webdav.HTTPClientWithBasicAuth(http.DefaultClient, user, password), collectionPath)
	require.NoError(t, err)
	infos, err := client.ReadDir(context.Background(), parsed.Path, false)
	if err != nil {
		t.Logf("reference: initial listing of an empty/fresh collection (expected to error): %v", err)
		return
	}
	for _, info := range infos {
		// ReadDir includes the collection itself; deleting that would take the
		// address book down mid-test.
		if info.Path == parsed.Path || info.IsDir {
			continue
		}
		resp := referenceRequest(t, user, password, http.MethodDelete, baseURL+info.Path, "")
		resp.Body.Close()
		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent, http.StatusNotFound}, resp.StatusCode,
			"DELETE %s failed: %s", info.Path, resp.Status)
	}
}

// referenceRejectsVCard reports whether the server refused to store raw at
// fullURL and, if so, returns the HTTP status and response body.
func referenceRejectsVCard(t *testing.T, user, password, fullURL, raw string) (bool, int, string) {
	t.Helper()
	resp := referenceRequest(t, user, password, http.MethodPut, fullURL, raw)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusNoContent {
		return false, resp.StatusCode, ""
	}
	return true, resp.StatusCode, string(body)
}

// sortedDifferenceConcepts returns the divergent concept_ids of a report in
// stable order (for ElementsMatch-style divergence-register pins).
func sortedDifferenceConcepts(report semanticequal.Report) []string {
	out := make([]string, 0, len(report.Differences))
	for _, d := range report.Differences {
		out = append(out, d.Concept)
	}
	sort.Strings(out)
	return out
}

// TestCardDAVReferenceServer_RoundTrip is the load-bearing real-server test:
// the TEST-02 fixture round-trips through a REAL CardDAV server (selectable
// per issue #681) and is compared semantically (issues #496 items 3 + 5), and
// the incremental lifecycle (unchanged = no re-fetch, remote update refetches,
// remote delete archives) is asserted against the server's real ETags and
// sync-tokens.
func TestCardDAVReferenceServer_RoundTrip(t *testing.T) {
	serverID, baseURL, user, password := referenceServerEnv(t)
	layout, ok := referenceServerLayouts[serverID]
	require.True(t, ok, "unknown reference server %q — known: radicale, baikal, nextcloud, davical", serverID)
	collectionPath := layout.collectionPath(baseURL, user)
	referenceEnsureCollection(t, serverID, user, password, collectionPath)
	referenceDeleteAll(t, baseURL, collectionPath, user, password)

	db, cfg, syncUser := setupCardDAVLifecycle(t)

	// The divergence register (issues #496 item 5 + #681): fixture contacts a
	// REAL CardDAV server does not round-trip faithfully, and why. The suite
	// asserts reality matches this register exactly, so a change on either
	// side — our exporter or the server — flips a pin into a failure that
	// names the change instead of silently carrying a stale expectation.
	divergenceRegister := referenceServerDivergences(serverID)

	// --- stage the TEST-02 fixture on the real server ------------------------
	m, err := canonicalfixture.Read()
	require.NoError(t, err)

	type fixtureEntry struct {
		name string
		rec  *contactmodel.Record
		uid  string
	}
	var entries []fixtureEntry
	var rejected []string
	for _, entry := range m.Contacts {
		if entry.SoftDeleted {
			continue
		}
		rec := entry.Record()
		if entry.RecreatesVCardUIDOf != "" {
			for _, other := range m.Contacts {
				if other.Name == entry.RecreatesVCardUIDOf {
					rec.Card.UID = other.Card.UID
				}
			}
		}
		raw, _, exportErr := vcard4.Adapter{}.Export(rec)
		require.NoError(t, exportErr, "export %s", entry.Name)

		href := collectionPath + rec.Card.UID + ".vcf"
		if refused, status, body := referenceRejectsVCard(t, user, password, href, string(raw)); refused {
			t.Logf("%s refused to store %s (%d: %s)", serverID, entry.Name, status, body)
			rejected = append(rejected, entry.Name)
			continue
		}
		etag := referencePut(t, user, password, href, string(raw))
		require.NotEmpty(t, etag, "%s must return an ETag on PUT", serverID)
		entries = append(entries, fixtureEntry{name: entry.Name, rec: rec, uid: rec.Card.UID})
	}
	require.NotEmpty(t, entries, "fixture must yield live contacts")

	// Pin the REJECTED half of the register.
	var expectedRejected []string
	for _, d := range divergenceRegister {
		if d.concepts == "" {
			expectedRejected = append(expectedRejected, d.name)
		}
	}
	assert.Equal(t, expectedRejected, rejected,
		"the set of fixture cards a real CardDAV server (%s) refuses to store changed — investigate and update the divergence register", serverID)

	// --- initial pull ---------------------------------------------------------
	sub := newContactTestSubscription(t, db, cfg, syncUser.ID, collectionPath, user, password)
	service := NewContactSyncService(false)
	stats, err := service.SyncSubscription(context.Background(), db, cfg, sub)
	require.NoError(t, err, "initial pull from %s", serverID)
	require.Equal(t, len(entries), stats.Created,
		"every staged fixture contact must be pulled from the real server (%s) (created=%d)", serverID, stats.Created)

	// Every card pulled in must be semantically equal to what was staged —
	// the re-serialize/re-parse cycle through a real implementation must not
	// lose anything (issue #496 item 5) — EXCEPT the documented divergences,
	// which are pinned to be exactly what the register says they are.
	registerByContact := make(map[string]serverDivergence, len(divergenceRegister))
	for _, d := range divergenceRegister {
		registerByContact[d.name] = d
	}
	for _, entry := range entries {
		entry := entry
		t.Run(entry.name, func(t *testing.T) {
			var link models.ContactSyncLink
			require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, layout.cardHref(user, entry.uid)).First(&link).Error)
			var contact models.Contact
			require.NoError(t, db.First(&contact, link.ContactID).Error)

			pulled := models.RecordForContact(&contact, cfg.ProfilePhotoDir, db)
			report := semanticequal.Compare(entry.rec, pulled)
			divergent := sortedDifferenceConcepts(report)

			reg, documented := registerByContact[entry.name]
			switch {
			case documented:
				require.NotEmpty(t, divergent,
					"fixture contact %q is in the divergence register (%s) but now round-trips CLEANLY on %s — the divergence is fixed; update the register",
					entry.name, reg.reason, serverID)
				assert.ElementsMatch(t, strings.Split(reg.concepts, ";"), divergent,
					"fixture contact %q diverges on %s in different concepts than the register documents (%s):\n%s",
					entry.name, serverID, reg.reason, report.DiffText())
			case len(divergent) > 0:
				t.Errorf("fixture contact %q did not survive the round trip through %s (not in the divergence register):\n%s",
					entry.name, serverID, report.DiffText())
			}

			require.NotEmpty(t, link.ETag, "the link must carry the server's real ETag")
		})
	}

	// --- unchanged server -> no re-fetch, no reconcile ------------------------
	stats, err = service.SyncSubscription(context.Background(), db, cfg, sub)
	require.NoError(t, err)
	assert.Equal(t, ContactSyncStats{}, stats, "an unchanged real server must reconcile nothing")

	// --- remote update refetches and updates ----------------------------------
	target := entries[0]
	newEmail := "updated." + target.uid + "@example.com"
	newETag := referencePut(t, user, password, collectionPath+target.uid+".vcf",
		fmt.Sprintf(carddavTestVCard, target.uid, "Updated", "Person", "Person", "Updated", newEmail))

	stats, err = service.SyncSubscription(context.Background(), db, cfg, sub)
	require.NoError(t, err, "incremental update pull from %s", serverID)
	assert.Equal(t, ContactSyncStats{Updated: 1}, stats,
		"the remote update must be reconciled exactly once (created=%d updated=%d archived=%d skipped=%d)",
		stats.Created, stats.Updated, stats.Archived, stats.Skipped)

	var updated models.Contact
	require.NoError(t, db.Where("user_id = ? AND vcard_uid = ?", syncUser.ID, target.uid).First(&updated).Error)
	assert.Equal(t, newEmail, updated.Email, "the remote edit must land")

	var updatedLink models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, layout.cardHref(user, target.uid)).First(&updatedLink).Error)
	assert.Equal(t, newETag, updatedLink.ETag, "the etag column must track %s's new ETag", serverID)

	// --- remote delete archives ------------------------------------------------
	deleted := entries[1]
	resp := referenceRequest(t, user, password, http.MethodDelete, collectionPath+deleted.uid+".vcf", "")
	resp.Body.Close()
	assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, resp.StatusCode, "DELETE %s failed", deleted.uid)

	stats, err = service.SyncSubscription(context.Background(), db, cfg, sub)
	require.NoError(t, err, "incremental delete pull from %s", serverID)
	assert.Equal(t, ContactSyncStats{Archived: 1}, stats,
		"the remote delete must archive exactly one contact (created=%d updated=%d archived=%d skipped=%d)",
		stats.Created, stats.Updated, stats.Archived, stats.Skipped)

	var archived models.Contact
	require.NoError(t, db.Where("user_id = ? AND vcard_uid = ?", syncUser.ID, deleted.uid).First(&archived).Error)
	assert.True(t, archived.Archived, "a real-server delete must archive (soft delete), not hard-delete")

	// --- subscription bookkeeping ----------------------------------------------
	require.NoError(t, db.First(sub, sub.ID).Error)
	assert.Equal(t, models.ContactSyncStatusSuccess, sub.LastSyncStatus)
	assert.NotEmpty(t, sub.SyncToken, "the subscription must carry the server's sync-token")

	// Nothing left behind: the idempotent second run must stay clean.
	stats, err = service.SyncSubscription(context.Background(), db, cfg, sub)
	require.NoError(t, err)
	assert.Equal(t, ContactSyncStats{}, stats, "a quiescent real server must stay quiescent")
}

// TestCardDAVRadicale_RoundTrip keeps the historical name so the pre-#681
// command line (`go test -run TestCardDAVRadicale`) keeps working; it is the
// same server-generic test with the Radicale default layout.
func TestCardDAVRadicale_RoundTrip(t *testing.T) {
	TestCardDAVReferenceServer_RoundTrip(t)
}
