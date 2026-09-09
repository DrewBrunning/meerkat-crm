package routes

// TestAuthorizationMatrixNonJWTCredentials and TestDAVSurfaceCredentialBoundary
// extend issue #371's six-persona authorization matrix
// (authorization_matrix_test.go) to the two credential types that are NOT a
// cookie JWT yet can still reach these routes (issue #566):
//
//	7. carddav-basic  — a valid CardDAV/CalDAV HTTP Basic-auth credential (the
//	   account password). Legitimate only against the /carddav + /caldav
//	   surface, and only for its own collections. It must NEVER authenticate a
//	   request to /api/v1/* — the REST auth layer only accepts a cookie or a
//	   `Bearer` token, so a `Basic` credential there is a flat 401.
//
//	8. carddav-token  — an API token minted with Scope "carddav" (issue #413).
//	   Usable on the DAV surface exactly like the password, but explicitly
//	   rejected (403) by middleware.AuthMiddleware on the REST surface: "This
//	   token is scoped to CardDAV and cannot be used for the API".
//
// A third column, full-token (Scope "full", the default), is asserted to
// behave IDENTICALLY to the cookie-JWT "owner" persona on every /api/v1 route:
// scope enforcement must neither under-grant (a full token that should reach a
// route but 401/403s) nor over-grant (a full token that reaches an admin route
// the owner JWT can't) relative to the JWT path.
//
// The REST-surface test reuses buildTable / seedResources / the completeness
// guard from the sibling file, so a newly-added /api/v1 route with no declared
// row fails here too — a future route cannot silently skip these credential
// types. The DAV-surface test carries its own bidirectional guard over the
// registered DAV route families (a new DAV surface with no declared family
// fails; a declared family with no route fails as stale).

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// apiTokenSeq makes every minted token string unique within a test run, so the
// full-token column can re-mint freely (token_hash carries a UNIQUE index).
var apiTokenSeq int

// mintAPIToken inserts an ApiToken row with the given scope and returns the
// plaintext "mycorrhizal_"-prefixed credential. Mirrors
// controllers/api_token_controller.go's generateApiToken shape and
// middleware.LookupAPIToken's hashing (sha256 hex of the raw string).
func mintAPIToken(t *testing.T, db *gorm.DB, userID uint, scope string) string {
	t.Helper()
	apiTokenSeq++
	raw := "mycorrhizal_" + scope + "_u" + strconv.FormatUint(uint64(userID), 10) +
		"_n" + strconv.Itoa(apiTokenSeq)
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	expires := time.Now().Add(90 * 24 * time.Hour)
	require.NoError(t, db.Create(&models.ApiToken{
		UserID:    userID,
		Name:      scope + "-scope",
		TokenHash: hash,
		Scope:     scope,
		ExpiresAt: &expires,
	}).Error)
	return raw
}

// dispatchBasic issues one request with an HTTP Basic credential and returns
// the status and body (body is used by the DAV cross-collection probe).
func dispatchBasic(router http.Handler, method, path, username, password string) (int, string) {
	// http.NoBody (not nil): go-webdav's PROPFIND path calls r.Body.Read and a
	// real net/http server always sets a non-nil Body; httptest does not.
	req, err := http.NewRequest(method, path, http.NoBody)
	if err != nil {
		return 0, ""
	}
	req.SetBasicAuth(username, password)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// isDAVSurface reports whether a registered route path belongs to the DAV
// surface rather than /api/v1 — used to exclude the wildcard CardDAV/CalDAV
// routes from the REST-surface completeness guard.
func isDAVSurface(path string) bool {
	return strings.HasPrefix(path, "/carddav") ||
		strings.HasPrefix(path, "/caldav") ||
		strings.HasPrefix(path, "/.well-known/")
}

func TestAuthorizationMatrixNonJWTCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:     "authz-matrix-cred-test-secret-key-that-is-long-enough",
		JWTExpiryHours:   96,
		ProfilePhotoDir:  t.TempDir(),
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
	}

	// Same shared IP-keyed bucket concern as the sibling test: this issues a
	// few hundred requests from one httptest IP. Raise the burst so a 429
	// never masquerades as an authorization verdict.
	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	// owner: the account the full-token and the cookie-JWT belong to, and the
	// one every seeded resource is owned by. full-token parity is asserted
	// against this account's "owner" persona expectations.
	owner := models.User{Username: "cred-owner", Email: "cred-owner@example.com", Password: "password123"}
	require.NoError(t, db.Create(&owner).Error)

	// davUser: a *different* account holding the CardDAV credentials. Separate
	// so that a probe of POST /api/v1/api-tokens/revoke-all (which runs for the
	// full-token and revokes every token of *its* user) can't also invalidate
	// the carddav-scoped token mid-run.
	davPWHash, err := bcrypt.GenerateFromPassword([]byte("dav-user-password"), bcrypt.DefaultCost)
	require.NoError(t, err)
	davUser := models.User{Username: "cred-dav", Email: "cred-dav@example.com", Password: string(davPWHash)}
	require.NoError(t, db.Create(&davUser).Error)

	ownerJWT, err := services.IssueSession(db, owner, cfg, "", "")
	require.NoError(t, err)
	// carddav-scoped token (davUser): rejected by AuthMiddleware before any
	// handler runs, so it never mutates state — one is enough for the whole run.
	cardDAVToken := mintAPIToken(t, db, davUser.ID, "carddav")
	// full-scoped token (owner): a few probed routes (POST /api-tokens/revoke-all)
	// do run and would invalidate the running credential, so it is re-minted
	// lazily whenever a probe comes back 401 (see probeFull below).
	fullToken := mintAPIToken(t, db, owner.ID, "full")

	res := seedResources(t, db, owner.ID)
	table := buildTable(res)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	// DAV disabled here on purpose — this router is the REST surface only, so
	// the completeness guard below stays byte-identical to the sibling test's.
	RegisterRoutes(router, cfg, db, nil)

	registered := map[string]gin.RouteInfo{}
	for _, r := range router.Routes() {
		if isDAVSurface(r.Path) {
			continue
		}
		registered[routeKey(r.Method, r.Path)] = r
	}

	var missing, stale []string
	for key := range registered {
		if _, ok := table[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range table {
		if _, ok := registered[key]; !ok {
			stale = append(stale, key)
		}
	}
	require.Empty(t, missing,
		"registered routes with no authorization row — add a row to buildTable:\n  %v", missing)
	require.Empty(t, stale,
		"declared authorization rows with no matching registered route:\n  %v", stale)

	failures := 0
	fail := func(persona, method, path string, status int, want string) {
		failures++
		t.Errorf("%-13s %-6s %s -> %d (want %s)", persona, method, path, status, want)
	}

	// probeFull issues a full-token request and, if the credential was
	// invalidated by an earlier destructive probe (revoke-all), re-mints once
	// and retries — so a stale credential never masquerades as an under-grant.
	probeFull := func(method, path string) int {
		got := dispatch(router, method, path, fullToken)
		if got == http.StatusUnauthorized {
			fullToken = mintAPIToken(t, db, owner.ID, "full")
			got = dispatch(router, method, path, fullToken)
		}
		return got
	}

	for _, r := range router.Routes() {
		if isDAVSurface(r.Path) {
			continue
		}
		key := routeKey(r.Method, r.Path)
		row := table[key]

		path := r.Path
		if row.probe != "" {
			path = row.probe
		}

		// --- full-token: must match the cookie-JWT owner persona exactly -----
		// (public routes included: owner's verdict there is "admitted", and a
		// full token hitting a public handler is admitted too.)
		wantOwner := expect(row, r.Method, personaOwner)
		if got := probeFull(r.Method, path); !wantOwner(got) {
			// Cross-check what the owner JWT itself returns, so a failure
			// message points at the divergence, not just the number.
			ownerGot := dispatch(router, r.Method, path, ownerJWT)
			failures++
			t.Errorf("full-token     %-6s %s -> %d, owner-JWT -> %d (must match owner persona)",
				r.Method, path, got, ownerGot)
		}

		// Public routes have no auth boundary — the DAV credentials pass
		// straight through to the handler there, which is not an authz concern.
		if row.class == classPublic {
			continue
		}

		// --- carddav-basic: a valid CardDAV Basic credential is never accepted
		// on /api/v1 — the REST auth layer only reads a cookie or a Bearer
		// token, so a Basic header is a flat 401 before the password is even
		// checked. ---
		if got, _ := dispatchBasic(router, r.Method, path, davUser.Username, "dav-user-password"); got != http.StatusUnauthorized {
			fail("carddav-basic", r.Method, path, got, "401")
		}

		// --- carddav-token: scope rejection is a hard 403 on every REST route -
		if got := dispatch(router, r.Method, path, cardDAVToken); got != http.StatusForbidden {
			fail("carddav-token", r.Method, path, got, "403")
		}
	}

	if failures > 0 {
		t.Fatalf("%d non-JWT-credential authorization violation(s); see the mismatches above", failures)
	}
}

// davProbe is one request issued against the CardDAV/CalDAV surface by a named
// credential class during TestDAVSurfaceCredentialBoundary.
type davProbe struct {
	name   string
	method string
	path   string
}

// davRoutePathPrefix reduces a registered DAV route path to the family key the
// declared table below is written in terms of: the two WebDAV surfaces collapse
// their "/*path" catch-all to "/carddav/" and "/caldav/", the discovery
// redirects stay verbatim.
func davRoutePathPrefix(routePath string) string {
	if strings.HasPrefix(routePath, "/carddav") {
		return "/carddav/"
	}
	if strings.HasPrefix(routePath, "/caldav") {
		return "/caldav/"
	}
	return routePath // /.well-known/carddav, /.well-known/caldav
}

func TestDAVSurfaceCredentialBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:     "authz-dav-cred-test-secret-key-that-is-long-enough",
		JWTExpiryHours:   96,
		ProfilePhotoDir:  t.TempDir(),
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
		CardDAVEnabled:   true,
		CalDAVEnabled:    true,
	}

	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	// User A authenticates to the DAV surface with a password; user B holds
	// both scoped API tokens. A owns a contact whose data must never surface
	// through B's session (the "own collections only" assertion).
	pwHash, err := bcrypt.GenerateFromPassword([]byte("dav-a-password"), bcrypt.DefaultCost)
	require.NoError(t, err)
	userA := models.User{Username: "dav-alpha", Email: "dav-alpha@example.com", Password: string(pwHash)}
	require.NoError(t, db.Create(&userA).Error)
	userB := models.User{Username: "dav-bravo", Email: "dav-bravo@example.com", Password: "unused-placeholder"}
	require.NoError(t, db.Create(&userB).Error)

	secretName := "AlphaOnlySecretContact"
	aContact := models.Contact{UserID: userA.ID, Firstname: secretName}
	require.NoError(t, db.Create(&aContact).Error)

	bFullToken := mintAPIToken(t, db, userB.ID, "full")
	bCardDAVToken := mintAPIToken(t, db, userB.ID, "carddav")

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	RegisterRoutes(router, cfg, db, nil)

	// --- completeness guard for the DAV surface -----------------------------
	// declaredDAVFamilies is this test's equivalent of buildTable: the exhaustive
	// set of DAV route families it knows about. The guard is bidirectional, the
	// same shape as the six-persona matrix's — a registered DAV route whose
	// family is not declared FAILS (a new surface can't silently skip these
	// credential types), and a declared family with no registered route fails as
	// stale.
	declaredDAVFamilies := map[string]bool{
		"/carddav/":            true,
		"/caldav/":             true,
		"/.well-known/carddav": true,
		"/.well-known/caldav":  true,
	}
	seenDAVFamilies := map[string]bool{}
	var undeclared []string
	for _, r := range router.Routes() {
		if !isDAVSurface(r.Path) {
			continue
		}
		fam := davRoutePathPrefix(r.Path)
		seenDAVFamilies[fam] = true
		if !declaredDAVFamilies[fam] {
			undeclared = append(undeclared, routeKey(r.Method, r.Path))
		}
	}
	require.Empty(t, undeclared,
		"registered DAV routes whose family is not declared in declaredDAVFamilies:\n  %v", undeclared)
	for fam := range declaredDAVFamilies {
		require.Truef(t, seenDAVFamilies[fam], "declared DAV family %q has no registered route (stale)", fam)
	}

	// The behavioural probes below; every one must fall under a registered DAV
	// family, so a typo'd path can't silently pass.
	probes := []davProbe{
		{name: "carddav-root", method: "PROPFIND", path: "/carddav/"},
		{name: "carddav-collection", method: "PROPFIND", path: "/carddav/addressbooks/dav-alpha/contacts/"},
		{name: "caldav-root", method: "PROPFIND", path: "/caldav/"},
		{name: "caldav-collection", method: "PROPFIND", path: "/caldav/calendars/dav-alpha/calendar/"},
		{name: "well-known-carddav", method: "GET", path: "/.well-known/carddav"},
		{name: "well-known-caldav", method: "GET", path: "/.well-known/caldav"},
	}
	for _, p := range probes {
		require.Truef(t, seenDAVFamilies[davRoutePathPrefix(p.path)],
			"probe %q targets %s, which is not a registered DAV route family", p.name, p.path)
	}

	// --- unauthenticated: the whole DAV surface is 401 ---------------------
	for _, p := range probes {
		if strings.HasPrefix(p.path, "/.well-known/") {
			continue // discovery redirect, deliberately unauthenticated
		}
		got, _ := dispatchNoAuth(router, p.method, p.path)
		require.Equal(t, http.StatusUnauthorized, got,
			"unauth %s %s should be 401", p.method, p.path)
	}

	// --- wrong password: 401, never admitted -----------------------------
	got, _ := dispatchBasic(router, "PROPFIND", "/carddav/", "dav-alpha", "not-the-password")
	require.Equal(t, http.StatusUnauthorized, got, "wrong password must be 401")

	// --- each DAV credential reaches the DAV root (not 401/403) ----------
	admittedOnDAV := func(label, method, path, user, pass string) {
		t.Helper()
		got, _ := dispatchBasic(router, method, path, user, pass)
		require.NotEqual(t, http.StatusUnauthorized, got, "%s: %s %s unexpectedly 401", label, method, path)
		require.NotEqual(t, http.StatusForbidden, got, "%s: %s %s unexpectedly 403", label, method, path)
	}
	admittedOnDAV("password", "PROPFIND", "/carddav/", "dav-alpha", "dav-a-password")
	admittedOnDAV("password", "PROPFIND", "/caldav/", "dav-alpha", "dav-a-password")
	admittedOnDAV("carddav-token", "PROPFIND", "/carddav/", "dav-bravo", bCardDAVToken)
	admittedOnDAV("carddav-token", "PROPFIND", "/caldav/", "dav-bravo", bCardDAVToken)
	admittedOnDAV("full-token", "PROPFIND", "/carddav/", "dav-bravo", bFullToken)

	// --- own collections only: B must never see A's contact --------------
	// B authenticates fine (so this is never a 401/403), but every DAV backend
	// query is scoped by the authenticated user id and every collection path is
	// derived from the authenticated username — so probing A's collection path
	// as B returns B's own (empty) view or a "not your collection" error, never
	// A's data. The security assertion is: auth succeeded AND nothing of A's
	// leaked into the response body.
	for _, probe := range []struct{ method, path string }{
		{"PROPFIND", "/carddav/addressbooks/dav-alpha/contacts/"},
		{"REPORT", "/carddav/addressbooks/dav-alpha/contacts/"},
		{"GET", "/carddav/addressbooks/dav-alpha/contacts/" + aContact.VCardUID + ".vcf"},
	} {
		status, body := dispatchBasic(router, probe.method, probe.path, "dav-bravo", bFullToken)
		require.NotEqualf(t, http.StatusUnauthorized, status, "%s %s: B is authenticated", probe.method, probe.path)
		require.NotEqualf(t, http.StatusForbidden, status, "%s %s: B is authenticated", probe.method, probe.path)
		require.NotContainsf(t, body, secretName,
			"%s %s leaked user A's contact name to user B (status %d)", probe.method, probe.path, status)
		require.NotContainsf(t, body, aContact.VCardUID,
			"%s %s leaked user A's contact UID to user B (status %d)", probe.method, probe.path, status)
	}
}

// dispatchNoAuth issues a request with no credential at all.
func dispatchNoAuth(router http.Handler, method, path string) (int, string) {
	req, err := http.NewRequest(method, path, http.NoBody)
	if err != nil {
		return 0, ""
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}
