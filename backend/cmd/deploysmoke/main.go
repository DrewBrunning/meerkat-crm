// Command deploysmoke drives a *freshly installed* running instance through
// one basic end-to-end workflow and fails if any step a clean install can
// get wrong does not work: register the first user, log in, create a contact
// with several field types, relate it to a second contact, attach a file,
// upload a profile photo, search for the contact, export it, and read every
// field back.
//
// It exists for DEPLOY-01 (issue #450). `docker-build-check.yml` proves the
// image builds and `e2e-tests.yml` proves the pre-seeded e2e stack runs, but
// neither proves that a stranger's first boot — empty database, no reused
// volumes, config set only from what docs/getting-started.md tells them — is
// actually usable. Each step here touches a subsystem a fresh install can
// get wrong:
//
//   - /health/ready facets: migrations ran on the empty database, and the
//     required directories are writable (attachment/photo dir permissions).
//   - register + login: JWT signing works (JWT_SECRET_KEY is a real secret).
//   - create/refetch contact: the neutral Card round-trips through SQLite.
//   - attach file: ATTACHMENTS_DIR exists and is writable by the app user.
//   - upload photo: PROFILE_PHOTO_DIR exists and is writable by the app user.
//   - search: the FTS index and its triggers were created on the empty DB.
//   - export: the shared exporter path plus a JWT-authed read.
//
// Run via `go run ./cmd/deploysmoke` against the docker-compose.yml instance
// brought up from an empty state — see .github/workflows/deploy-smoke.yml.
// Mirrors cmd/loadsmoke's shape: run() split into small named steps, an
// injected getenv for testability, and httptest-based unit tests.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // register JPEG for image.Decode — stored profile photos are JPG
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

type smokeConfig struct {
	baseURL string
}

// defaultSmokeConfig mirrors cmd/loadsmoke's env-var-with-defaults style. The
// port matches docker-compose.yml's default mapping (FRONTEND_PORT:-7300).
var defaultSmokeConfig = smokeConfig{
	baseURL: "http://localhost:7300",
}

// smokeConfigFromEnv reads DEPLOYSMOKE_* overrides via getenv (injected so
// this is testable without mutating real process env).
func smokeConfigFromEnv(getenv func(string) string) smokeConfig {
	cfg := defaultSmokeConfig
	if v := getenv("DEPLOYSMOKE_BASE_URL"); v != "" {
		cfg.baseURL = strings.TrimRight(v, "/")
	}
	return cfg
}

func main() {
	if err := run(smokeConfigFromEnv(os.Getenv)); err != nil { // # pragma: no cover — thin wiring; tests exercise run() directly
		log.Fatal(err) // # pragma: no cover
	}
}

// run executes every workflow step in order against cfg.baseURL, stopping at
// the first failure. A single cookie jar carries the auth_token cookie set by
// login through every later request.
func run(cfg smokeConfig) error {
	jar, err := cookiejar.New(nil)
	if err != nil { // # pragma: no cover — cookiejar.New(nil) never returns an error
		return fmt.Errorf("create cookie jar: %w", err)
	}
	r := &smokeRun{
		client:  &http.Client{Jar: jar, Timeout: 30 * time.Second},
		baseURL: cfg.baseURL,
		// A username unique per run so re-running against the same instance
		// (local iteration) does not collide on users.username.
		username: fmt.Sprintf("deploysmoke-%d", time.Now().UnixNano()),
	}

	for _, s := range steps {
		fmt.Printf("deploysmoke: %-22s ", s.name+"...")
		if err := s.fn(r); err != nil {
			fmt.Println("FAIL")
			return fmt.Errorf("%s: %w", s.name, err)
		}
		fmt.Println("ok")
	}

	fmt.Printf("deploysmoke: all %d workflow steps passed against %s\n", len(steps), cfg.baseURL)
	return nil
}

// smokeRun carries the HTTP client and the state captured across steps (the
// contact created in createContact is attached to, photographed, searched
// for, and read back by later steps).
type smokeRun struct {
	client       *http.Client
	baseURL      string
	username     string
	contactID    int64
	contactUID   string
	relatedUID   string
	attachmentID int64
}

const smokePassword = "CorrectHorseBattery9!"

// The name components the workflow writes and later asserts survive a full
// round trip through SQLite and back out of GET /contacts/{id}.
const (
	smokeGiven    = "Deploysmoke"
	smokeSurname  = "Firstrun"
	smokeEmail    = "deploysmoke.firstrun@example.com"
	smokePhone    = "+1 555-0142"
	smokeLocality = "Cleaninstalltown"
)

type step struct {
	name string
	fn   func(*smokeRun) error
}

var steps = []step{
	{"health", (*smokeRun).checkHealth},
	{"register-first-user", (*smokeRun).registerFirstUser},
	{"login", (*smokeRun).login},
	{"create-contact", (*smokeRun).createContact},
	{"relate-contact", (*smokeRun).relateContact},
	{"attach-file", (*smokeRun).attachFile},
	{"upload-photo", (*smokeRun).uploadPhoto},
	{"search-contact", (*smokeRun).searchContact},
	{"export", (*smokeRun).exportAndReadBack},
	{"refetch-fields", (*smokeRun).refetchAndAssertFields},
	// The checks below exercise the shipped nginx layer specifically — no Go
	// test in this repo sees nginx (httptest hits the Gin router directly), so
	// a proxy-config regression is invisible everywhere else.
	{"export-loss-header", (*smokeRun).exportLossHeaderThroughProxy}, // issue #863
	{"import-body-limit", (*smokeRun).importBodyLimitOwnedByApp},     // issue #876
	{"wellknown-discovery", (*smokeRun).wellKnownDiscoveryRelative},  // issue #865
}

// smokeLossyCRM is the CRM-envelope the smoke contact carries. Each field is a
// CRM-only concept with no vCard/JSContact home, so a structured export
// reports a fidelity loss for each — which is how the export-loss-header step
// gets a non-trivial X-Mycorrhizal-Export-Loss-Report to send back through
// nginx (issue #863).
var smokeLossyCRM = map[string]any{
	"how_we_met":          "Met at the clean-install open house",
	"work_information":    "Runs the deployment smoke test",
	"contact_information": "Reachable only via this test",
	"gender":              "unspecified",
}

// checkHealth asserts the three-endpoint health surface (issue #421) reports
// a fresh install as ready: /health/live answers, /health/ready has every
// facet ok (database reachable, migrations applied on the empty DB, required
// directories writable), and the deep /health is not "unhealthy".
func (r *smokeRun) checkHealth() error {
	body, err := r.getOK("/health/live")
	if err != nil {
		return err
	}
	var live struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &live); err != nil {
		return fmt.Errorf("GET /health/live: decode %q: %w", truncate(body, 200), err)
	}
	if live.Status != "live" {
		return fmt.Errorf("GET /health/live: status field = %q, want \"live\"", live.Status)
	}

	body, err = r.expect(http.MethodGet, "/health/ready", "", nil, []int{http.StatusOK},
		"a fresh install must be ready — migrations applied, required dirs writable")
	if err != nil {
		return err
	}
	var ready struct {
		Checks map[string]struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(body, &ready); err != nil {
		return fmt.Errorf("GET /health/ready: decode %q: %w", truncate(body, 300), err)
	}
	for _, facet := range []string{"database", "migrations", "filesystem"} {
		ck, present := ready.Checks[facet]
		if !present {
			return fmt.Errorf("GET /health/ready: no %q facet in checks: %s", facet, truncate(body, 300))
		}
		if ck.Status != "ok" {
			return fmt.Errorf("GET /health/ready: %q facet = %q (%s) — a fresh install must satisfy it", facet, ck.Status, ck.Reason)
		}
	}

	body, err = r.getOK("/health")
	if err != nil {
		return err
	}
	var deep struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &deep); err != nil {
		return fmt.Errorf("GET /health: decode %q: %w", truncate(body, 300), err)
	}
	// "degraded" is a valid fresh-install state and still 200 (issue #421):
	// e.g. the restore drill has no prior backup to verify yet. Only
	// "unhealthy" (or an unrecognized status) means the CRM cannot operate.
	if deep.Status != "healthy" && deep.Status != "degraded" {
		return fmt.Errorf("GET /health: status = %q, want \"healthy\" or \"degraded\" on a fresh install", deep.Status)
	}
	return nil
}

// registerFirstUser registers the very first account. On a clean install the
// first user is granted admin rights automatically (docs/getting-started.md
// "Post-Installation Setup").
func (r *smokeRun) registerFirstUser() error {
	_, err := r.postJSON("/api/v1/register", map[string]any{
		"username": r.username,
		"email":    r.username + "@example.com",
		"password": smokePassword,
	}, http.StatusCreated)
	return err
}

// login authenticates as the registered user; the client's cookie jar then
// carries auth_token for every later request. A working login proves the
// server can sign a JWT — i.e. JWT_SECRET_KEY passed config validation and is
// usable.
func (r *smokeRun) login() error {
	body, err := r.postJSON("/api/v1/login", map[string]any{
		"identifier": r.username,
		"password":   smokePassword,
	}, http.StatusOK)
	if err != nil {
		return err
	}
	u, _ := url.Parse(r.baseURL)
	for _, ck := range r.client.Jar.Cookies(u) {
		if ck.Name == "auth_token" {
			return nil
		}
	}
	return fmt.Errorf("POST /api/v1/login: no auth_token cookie set: %s", truncate(body, 300))
}

// createContact creates a contact carrying several distinct field types
// (structured name, email, phone, a five-component postal address, and a
// partial-date birth anniversary) so the round trip exercises more than one
// column family. The id and UID are captured for the later steps.
func (r *smokeRun) createContact() error {
	card := map[string]any{
		"name": map[string]any{
			"components": []map[string]string{
				{"kind": "given", "value": smokeGiven},
				{"kind": "surname", "value": smokeSurname},
			},
		},
		"emails": []map[string]any{
			{"address": smokeEmail, "contexts": []string{"home"}},
		},
		"phones": []map[string]any{
			{"number": smokePhone, "contexts": []string{"mobile"}},
		},
		"addresses": []map[string]any{
			{
				"components": []map[string]string{
					{"kind": "name", "value": "1 Cleaninstall Way"},
					{"kind": "locality", "value": smokeLocality},
					{"kind": "region", "value": "CA"},
					{"kind": "postcode", "value": "94000"},
					{"kind": "country", "value": "US"},
				},
				"contexts": []string{"home"},
			},
		},
		"anniversaries": []map[string]any{
			{"kind": "birth", "date": map[string]any{"partial": map[string]int{"year": 1990, "month": 6, "day": 15}}},
		},
		"keywords": []string{"deploysmoke"},
	}
	body, err := r.postJSON("/api/v1/contacts", map[string]any{"card": card, "crm": smokeLossyCRM}, http.StatusCreated)
	if err != nil {
		return err
	}
	var created struct {
		Contact struct {
			ID  int64  `json:"id"`
			UID string `json:"uid"`
		} `json:"contact"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return fmt.Errorf("POST /api/v1/contacts: decode %q: %w", truncate(body, 400), err)
	}
	if created.Contact.ID == 0 || created.Contact.UID == "" {
		return fmt.Errorf("POST /api/v1/contacts: response missing id/uid: %s", truncate(body, 400))
	}
	r.contactID = created.Contact.ID
	r.contactUID = created.Contact.UID
	return nil
}

// relateContact creates a second contact and a confirmed relationship edge
// between the two — the graph-entity write path, keyed by contact UID rather
// than the integer id.
func (r *smokeRun) relateContact() error {
	body, err := r.postJSON("/api/v1/contacts", map[string]any{
		"card": map[string]any{
			"name": map[string]any{
				"components": []map[string]string{
					{"kind": "given", "value": "Deploysmoke"},
					{"kind": "surname", "value": "Related"},
				},
			},
		},
		"crm": map[string]any{},
	}, http.StatusCreated)
	if err != nil {
		return fmt.Errorf("second contact: %w", err)
	}
	var second struct {
		Contact struct {
			UID string `json:"uid"`
		} `json:"contact"`
	}
	if err := json.Unmarshal(body, &second); err != nil || second.Contact.UID == "" {
		return fmt.Errorf("second contact: no uid in %q: %v", truncate(body, 300), err)
	}
	r.relatedUID = second.Contact.UID

	_, err = r.postJSON("/api/v1/relationship-edges", map[string]any{
		"source_id": r.contactUID,
		"target_id": r.relatedUID,
		"type":      "friend_of",
	}, http.StatusOK, http.StatusCreated)
	return err
}

// attachFile uploads a small text file to the contact and reads it back
// byte-for-byte. A failure here on a fresh install is almost always
// ATTACHMENTS_DIR missing or not writable by the non-root app user.
func (r *smokeRun) attachFile() error {
	want := []byte("deploysmoke attachment payload\n")
	body, err := r.postMultipart(
		fmt.Sprintf("/api/v1/contacts/%d/attachments", r.contactID),
		"file", "deploysmoke.txt", want,
		"check ATTACHMENTS_DIR exists and is writable by the app user",
		http.StatusCreated,
	)
	if err != nil {
		return err
	}
	var att struct {
		Attachment struct {
			ID int64 `json:"id"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(body, &att); err != nil || att.Attachment.ID == 0 {
		return fmt.Errorf("POST .../attachments: no attachment id in %q: %v", truncate(body, 300), err)
	}
	r.attachmentID = att.Attachment.ID

	got, err := r.getOK(fmt.Sprintf("/api/v1/attachments/%d/download", r.attachmentID))
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("attachment round trip mismatch: wrote %q, read back %q", want, truncate(got, 200))
	}
	return nil
}

// uploadPhoto uploads a real (tiny) PNG as the contact's profile photo and
// reads the stored image back. A failure here on a fresh install is almost
// always PROFILE_PHOTO_DIR missing or not writable by the non-root app user.
func (r *smokeRun) uploadPhoto() error {
	_, err := r.postMultipart(
		fmt.Sprintf("/api/v1/contacts/%d/profile_picture", r.contactID),
		"photo", "deploysmoke.png", smokePNG,
		"check PROFILE_PHOTO_DIR exists and is writable by the app user",
		http.StatusOK, http.StatusCreated,
	)
	if err != nil {
		return err
	}

	got, err := r.getOK(fmt.Sprintf("/api/v1/contacts/%d/profile_picture", r.contactID))
	if err != nil {
		return err
	}
	if _, _, err := image.Decode(bytes.NewReader(got)); err != nil {
		return fmt.Errorf("GET .../profile_picture: response is not a decodable image (%d bytes): %w", len(got), err)
	}
	return nil
}

// searchContact searches for the contact by surname. This is the only step
// that exercises the FTS5 index and its insert triggers, both created on the
// empty database during the first-boot migration run.
func (r *smokeRun) searchContact() error {
	body, err := r.getOK("/api/v1/search?q=" + url.QueryEscape(smokeSurname))
	if err != nil {
		return err
	}
	var res struct {
		Contacts []struct {
			ID int64 `json:"id"`
		} `json:"contacts"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("GET /api/v1/search: decode %q: %w", truncate(body, 300), err)
	}
	for _, c := range res.Contacts {
		if c.ID == r.contactID {
			return nil
		}
	}
	return fmt.Errorf("GET /api/v1/search?q=%s: contact %d not among %d hits (FTS index/triggers may not have been created) — %s",
		smokeSurname, r.contactID, len(res.Contacts), truncate(body, 300))
}

// exportAndReadBack pulls all three export formats and asserts the contact's
// surname survives into each — the shared exporter path plus a JWT-authed
// read. The jscontact form is also checked to be valid JSON.
func (r *smokeRun) exportAndReadBack() error {
	for _, ex := range []struct {
		path   string
		isJSON bool
	}{
		{"/api/v1/export/vcf", false},      // text/vcard
		{"/api/v1/export/jscontact", true}, // application/jscontact+json
		{"/api/v1/export", false},          // the CSV data bundle
	} {
		body, err := r.getOK(ex.path)
		if err != nil {
			return err
		}
		if ex.isJSON && !json.Valid(body) {
			return fmt.Errorf("GET %s: response is not valid JSON: %s", ex.path, truncate(body, 300))
		}
		if !bytes.Contains(body, []byte(smokeSurname)) {
			return fmt.Errorf("GET %s: export does not contain %q: %s", ex.path, smokeSurname, truncate(body, 400))
		}
	}
	return nil
}

// exportLossHeaderThroughProxy pins issue #863: the structured exporters set
// X-Mycorrhizal-Export-Loss-Report, a URL-encoded JSON header. nginx's default
// proxy_buffer_size is a single ~4 KB page that must hold the *entire*
// upstream header block; a large loss-report header overran it and nginx
// returned 502 in place of the file (the CSV export, which sets no such
// header, kept working — exactly what the pen test observed). This asserts
// both exporters return 200 through the shipped nginx and that the loss-report
// header arrives intact, well-formed, and within a stock proxy header buffer.
// The smoke contact carries smokeLossyCRM so the header is non-trivial rather
// than an empty report.
func (r *smokeRun) exportLossHeaderThroughProxy() error {
	const lossHeader = "X-Mycorrhizal-Export-Loss-Report"
	for _, path := range []string{"/api/v1/export/vcf", "/api/v1/export/jscontact"} {
		status, header, body, err := r.doResp(http.MethodGet, path, "", nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("GET %s: status %d, want 200 — a 5xx here is issue #863 (loss-report header overran the proxy header buffer): %s",
				path, status, truncate(body, 300))
		}
		raw := header.Get(lossHeader)
		if raw == "" {
			return fmt.Errorf("GET %s: no %s header — nginx may have dropped an oversized header", path, lossHeader)
		}
		// nginx's compiled default proxy_buffer_size is one page (~4 KB) and
		// holds the whole header block; the value alone must stay well inside it.
		if len(raw) > 4096 {
			return fmt.Errorf("GET %s: %s is %d bytes — over a stock 4 KB proxy header buffer (issue #863)", path, lossHeader, len(raw))
		}
		decoded, err := url.QueryUnescape(raw)
		if err != nil {
			return fmt.Errorf("GET %s: %s is not URL-decodable: %w", path, lossHeader, err)
		}
		var report struct {
			Count       int  `json:"count"`
			Truncated   bool `json:"truncated"`
			Diagnostics []struct {
				Concept string `json:"concept"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(decoded), &report); err != nil {
			return fmt.Errorf("GET %s: %s is not valid JSON: %w (%s)", path, lossHeader, err, truncate([]byte(decoded), 200))
		}
		if report.Count < len(smokeLossyCRM) {
			return fmt.Errorf("GET %s: loss-report count = %d, want >= %d (the smoke contact carries that many CRM-only fields with no export home)",
				path, report.Count, len(smokeLossyCRM))
		}
	}
	return nil
}

// importBodyLimitOwnedByApp pins issue #876: docker/nginx.conf set no
// client_max_body_size, so nginx's compiled 1 MB default rejected every
// import over 1 MB before it reached the backend — making services.MaxCSVSize
// (20 MB) and friends unreachable in the shipped image, and hiding the app's
// structured 413 behind nginx's HTML one. With client_max_body_size raised
// above the largest app cap, the Go BodySizeLimitMiddleware is the binding
// limit again.
func (r *smokeRun) importBodyLimitOwnedByApp() error {
	const path = "/api/v1/contacts/import/upload"

	// (1) A CSV comfortably above nginx's old 1 MB default but well below the
	// app's 20 MB CSV cap must reach the Go handler: a structured JSON
	// response, never nginx's text/html 413.
	ct, small, err := buildMultipartBody("file", "big.csv", csvBytes(2<<20))
	if err != nil {
		return err
	}
	status, header, body, err := r.doResp(http.MethodPost, path, ct, small)
	if err != nil {
		return err
	}
	if status == http.StatusRequestEntityTooLarge {
		return fmt.Errorf("POST %s (2 MB): 413 — nginx client_max_body_size is rejecting below the app's 20 MB CSV cap (issue #876): %s",
			path, truncate(body, 200))
	}
	if mt := header.Get("Content-Type"); !strings.Contains(mt, "json") {
		return fmt.Errorf("POST %s (2 MB): Content-Type %q, want JSON — the request did not reach the Go handler (nginx HTML error page?): %s",
			path, mt, truncate(body, 200))
	}

	// (2) A CSV above the app's 20 MB cap must be rejected by
	// BodySizeLimitMiddleware with its structured body — proving the app
	// layer, not the proxy, owns the limit.
	ct, big, err := buildMultipartBody("file", "big.csv", csvBytes(21<<20))
	if err != nil {
		return err
	}
	status, header, body, err = r.doResp(http.MethodPost, path, ct, big)
	if err != nil {
		return err
	}
	if status != http.StatusRequestEntityTooLarge {
		return fmt.Errorf("POST %s (21 MB): status %d, want 413 from the app: %s", path, status, truncate(body, 200))
	}
	if mt := header.Get("Content-Type"); !strings.Contains(mt, "json") {
		return fmt.Errorf("POST %s (21 MB): Content-Type %q, want JSON — a 413 from nginx, not the app", path, mt)
	}
	if !bytes.Contains(body, []byte("request body too large")) {
		return fmt.Errorf("POST %s (21 MB): body %q, want the app's \"request body too large\"", path, truncate(body, 200))
	}
	return nil
}

// csvBytes returns approximately n bytes of well-formed CSV (a header plus
// repeated data rows). Used to build import uploads of a controlled wire size.
func csvBytes(n int) []byte {
	var buf bytes.Buffer
	buf.Grow(n + 32)
	buf.WriteString("given,family,email\n")
	for buf.Len() < n {
		buf.WriteString("Smoke,Row,smoke.row@example.com\n")
	}
	return buf.Bytes()
}

// wellKnownDiscoveryRelative pins issue #865: the CardDAV/CalDAV .well-known
// discovery redirects were emitted by nginx with the default
// absolute_redirect, which builds the Location from the Host header and
// nginx's own listen port — leaking the internal :8080 and handing real
// clients (arriving over https on the published port) a URL they cannot
// follow. A relative Location resolves against the request URI and carries
// neither.
func (r *smokeRun) wellKnownDiscoveryRelative() error {
	// A client that does not follow redirects, so the 301 itself is readable.
	noRedirect := &http.Client{
		Jar:     r.client.Jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, wk := range []struct{ path, want string }{
		{"/.well-known/carddav", "/carddav/"},
		{"/.well-known/caldav", "/caldav/"},
	} {
		req, err := http.NewRequest(http.MethodGet, r.baseURL+wk.path, nil)
		if err != nil {
			return fmt.Errorf("build request for %s: %w", wk.path, err)
		}
		resp, err := noRedirect.Do(req)
		if err != nil {
			return fmt.Errorf("GET %s: %w", wk.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMovedPermanently {
			return fmt.Errorf("GET %s: status %d, want 301", wk.path, resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if loc == "" {
			return fmt.Errorf("GET %s: 301 with no Location header", wk.path)
		}
		if strings.Contains(loc, ":8080") {
			return fmt.Errorf("GET %s: Location %q discloses the internal port — nginx absolute_redirect must be off (issue #865)", wk.path, loc)
		}
		if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
			return fmt.Errorf("GET %s: Location %q is absolute; a relative Location avoids the scheme/port leak (issue #865)", wk.path, loc)
		}
		if loc != wk.want {
			return fmt.Errorf("GET %s: Location %q, want %q", wk.path, loc, wk.want)
		}
	}
	return nil
}

// refetchAndAssertFields reads the contact back and checks that every field
// type written in createContact survived the round trip through SQLite.
func (r *smokeRun) refetchAndAssertFields() error {
	body, err := r.getOK(fmt.Sprintf("/api/v1/contacts/%d", r.contactID))
	if err != nil {
		return err
	}
	// GET /api/v1/contacts/{id} returns a bare ContactRecordResponse (card at
	// the top level), unlike POST which wraps it in {"contact": ...}.
	var resp struct {
		Card struct {
			Name struct {
				Components []component `json:"components"`
			} `json:"name"`
			Emails []struct {
				Address string `json:"address"`
			} `json:"emails"`
			Phones []struct {
				Number string `json:"number"`
			} `json:"phones"`
			Addresses []struct {
				Components []component `json:"components"`
			} `json:"addresses"`
			Anniversaries []struct {
				Kind string `json:"kind"`
			} `json:"anniversaries"`
		} `json:"card"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("GET /api/v1/contacts/%d: decode %q: %w", r.contactID, truncate(body, 400), err)
	}
	card := resp.Card

	if !hasComponent(card.Name.Components, "given", smokeGiven) ||
		!hasComponent(card.Name.Components, "surname", smokeSurname) {
		return fmt.Errorf("name components did not round trip: %s", truncate(body, 400))
	}
	if len(card.Emails) == 0 || card.Emails[0].Address != smokeEmail {
		return fmt.Errorf("email did not round trip: %s", truncate(body, 400))
	}
	if len(card.Phones) == 0 || card.Phones[0].Number != smokePhone {
		return fmt.Errorf("phone did not round trip: %s", truncate(body, 400))
	}
	if len(card.Addresses) == 0 || !hasComponent(card.Addresses[0].Components, "locality", smokeLocality) {
		return fmt.Errorf("address did not round trip: %s", truncate(body, 400))
	}
	if len(card.Anniversaries) == 0 || card.Anniversaries[0].Kind != "birth" {
		return fmt.Errorf("birth anniversary did not round trip: %s", truncate(body, 400))
	}
	return nil
}

type component struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func hasComponent(components []component, kind, value string) bool {
	for _, cmp := range components {
		if cmp.Kind == kind && cmp.Value == value {
			return true
		}
	}
	return false
}

// --- HTTP helpers (adapted from cmd/loadsmoke) --------------------------------

// expect issues one request and returns the response body only when the
// status is one of wantStatus. A transport failure or an unexpected status is
// returned as a single formatted error, so each step needs exactly one error
// check rather than a separate transport and status branch. hint, when set,
// is appended to the status-mismatch message (e.g. the config variable a
// fresh-install operator most likely got wrong).
func (r *smokeRun) expect(method, path, contentType string, reqBody []byte, wantStatus []int, hint string) ([]byte, error) {
	body, status, err := r.do(method, path, contentType, reqBody)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(wantStatus, status) {
		msg := fmt.Sprintf("%s %s: status %d (want one of %v)", method, path, status, wantStatus)
		if hint != "" {
			msg += " — " + hint
		}
		return body, fmt.Errorf("%s: %s", msg, truncate(body, 400))
	}
	return body, nil
}

func (r *smokeRun) getOK(path string) ([]byte, error) {
	return r.expect(http.MethodGet, path, "", nil, []int{http.StatusOK}, "")
}

func (r *smokeRun) postJSON(path string, body any, wantStatus ...int) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil { // # pragma: no cover — every call site passes plain maps/slices/scalars
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return r.expect(http.MethodPost, path, "application/json", b, wantStatus, "")
}

func (r *smokeRun) postMultipart(path, field, filename string, content []byte, hint string, wantStatus ...int) ([]byte, error) {
	contentType, body, err := buildMultipartBody(field, filename, content)
	if err != nil {
		return nil, err
	}
	return r.expect(http.MethodPost, path, contentType, body, wantStatus, hint)
}

// buildMultipartBody assembles a single-file multipart/form-data body and
// returns its Content-Type (with boundary) and bytes.
func buildMultipartBody(field, filename string, content []byte) (string, []byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil { // # pragma: no cover — CreateFormFile on a fresh writer does not fail
		return "", nil, fmt.Errorf("build multipart part: %w", err)
	}
	if _, err := part.Write(content); err != nil { // # pragma: no cover — writing to a bytes.Buffer-backed part does not fail
		return "", nil, fmt.Errorf("write multipart content: %w", err)
	}
	if err := w.Close(); err != nil { // # pragma: no cover — closing a bytes.Buffer-backed writer does not fail
		return "", nil, fmt.Errorf("close multipart writer: %w", err)
	}
	return w.FormDataContentType(), buf.Bytes(), nil
}

// do issues one request and returns the fully-read body and status. The
// response body is drained and closed here (not returned via *http.Response) —
// same rationale as cmd/loadsmoke's doJSON.
func (r *smokeRun) do(method, path, contentType string, body []byte) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, r.baseURL+path, reader)
	if err != nil { // # pragma: no cover — method and URL are always well-formed constants
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil { // # pragma: no cover — the test servers always send a complete body
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// doResp issues one request and returns the status, response headers and the
// fully-read body. Unlike do(), it surfaces the header map — the nginx-layer
// steps assert on Location / Content-Type / X-Mycorrhizal-Export-Loss-Report.
func (r *smokeRun) doResp(method, path, contentType string, body []byte) (int, http.Header, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, r.baseURL+path, reader)
	if err != nil { // # pragma: no cover — method and URL are always well-formed constants
		return 0, nil, nil, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil { // # pragma: no cover — the test servers always send a complete body
		return resp.StatusCode, resp.Header, nil, fmt.Errorf("read response body: %w", err)
	}
	return resp.StatusCode, resp.Header, respBody, nil
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// smokePNG is a real 64×64 PNG built at init so uploadPhoto sends bytes the
// server's image decoder actually accepts (it rejects non-image uploads).
var smokePNG = mustPNG()

func mustPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil { // # pragma: no cover — encoding a valid in-memory RGBA image cannot fail
		panic("deploysmoke: encode embedded PNG: " + err.Error())
	}
	return buf.Bytes()
}
