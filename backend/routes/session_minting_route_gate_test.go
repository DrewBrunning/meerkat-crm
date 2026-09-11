package routes

// TestSessionMintingRoutesGate is the mechanical check issue #840 asks for:
// every route that mints a session must carry BOTH AuthRateLimitMiddleware and
// EnforceMinClientVersion ahead of any credential work, and no other route may
// carry the client-version floor. It is the same self-maintaining shape as
// #609/#610/#611/#612 — derive the subject list from the running system, fail
// on an undeclared member, in both directions — applied here to the
// session-minting surface #692 established and #722's device-grant exchange
// extended.
//
// Why this needs reflection into gin internals: router.Routes() exposes only a
// route's FINAL handler, not its middleware chain, so it cannot see that a
// public route is missing the floor or the auth rate limiter.
// routes/authorization_matrix_test.go asserts the authorization boundary and
// controllers/client_version_route_test.go exercises the middleware in
// isolation; neither can assert "this middleware is wired ahead of the handler
// on exactly this set of routes". handlerChainByRoute walks gin's unexported
// routing trees to recover the ordered chain for every registered route. It is
// a deliberate, contained reach into gin v1.12.0 internals; the guard in the
// test asserts the walk still finds a known chain, so a gin upgrade that
// reshapes the tree fails loudly here rather than silently returning nothing.
//
// The table is exhaustive in both directions:
//
//   - a declared session-minting route missing the floor or the auth rate
//     limiter, or not registered at all, FAILS; and
//   - a route that carries EnforceMinClientVersion but is not declared
//     session-minting FAILS — a new auth path added to the #692 pattern is
//     caught until it is declared, and the floor spreading to a non-minting
//     route (a policy smell) is caught too.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

// The runtime function names of the two middlewares this gate is about. Both
// are closures with a single creation site, so runtime.FuncForPC resolves
// every wired instance to a name containing one of these markers.
// AuthRateLimitMiddleware wraps RateLimitMiddleware in its own closure
// precisely so it is distinguishable by name from the API and CardDAV
// limiters (see middleware/rate_limiter.go).
//
// Markers, not full qualified names: mid-stack inlining renames a closure to
// prefix it with every enclosing call the compiler chose to inline it into
// (observed under the go.mod floor, Go 1.26.0: "mycorrhizal/routes.
// RegisterRoutes.AuthRateLimitMiddleware.func34" instead of the unlined
// "mycorrhizal/middleware.AuthRateLimitMiddleware.func1" the newer pinned
// toolchain produces here) and renumbers the trailing .funcN — both the
// prefix and the number are inlining-decision artifacts that differ across
// Go versions, not part of the closure's identity. The "<Name>.func" marker
// survives both: it names the one thing that cannot move, the original
// function this closure was declared in.
const (
	floorFuncMarker  = "EnforceMinClientVersion.func"
	authRLFuncMarker = "AuthRateLimitMiddleware.func"
)

// sessionMintingRoutes is every public route whose successful response
// establishes a session — sets the auth_token cookie, returns a session JWT,
// or issues a device grant. The value is the reason it is in the set. This is
// the authoritative list the gate asserts the router against.
var sessionMintingRoutes = map[string]string{
	"POST /api/v1/register":            "creates an account from an unauthenticated request — a client below the floor must not be able to create an account it can never use (#692)",
	"POST /api/v1/login":               "password login — mints the auth_token session cookie, or a 2fa_pending challenge when TOTP is enabled",
	"POST /api/v1/login/2fa":           "step 2 of interactive login — exchanges the 2FA challenge for the real session cookie",
	"POST /api/v1/auth/device/session": "device-grant exchange (#722) — a held grant is traded for a fresh session JWT; possession of a grant is as powerful as a password",
}

// clientVersionFloorExclusions documents the routes that touch authentication,
// or even mint a session, but are deliberately NOT under the min-client-version
// floor, each with the reason it is out of scope for #692's native-client
// contract. The gate asserts every entry is a registered route that does NOT
// carry the floor, so this stays honest as routes.go changes.
//
// Not listed here because they are only registered when OIDC is configured
// (and so are absent from this test's router): GET /api/v1/auth/oidc/login
// (starts the handshake, mints no session) and GET /api/v1/auth/oidc/callback
// (mints auth_token + id_token, but the caller is the system browser mid
// redirect from the identity provider — X-Client-Version is a native-app
// header, so the floor structurally cannot apply).
var clientVersionFloorExclusions = map[string]string{
	"POST /api/v1/logout":                  "clears the session; mints nothing",
	"POST /api/v1/check-password-strength": "stateless zxcvbn utility — no credentials, no session",
	"POST /api/v1/password-reset/request":  "starts the reset-email flow; mints nothing",
	"POST /api/v1/password-reset/confirm":  "sets a new password and bumps TokenVersion; it does NOT log the user in (no cookie or token issued)",
	"POST /api/v1/users/change-password":   "re-mints auth_token, but only for a caller who already holds a valid session (behind AuthMiddleware) — not a public entry point",
	"POST /api/v1/users/2fa/confirm":       "re-mints auth_token behind AuthMiddleware after a TokenVersion bump — authenticated, not a public session mint",
	"POST /api/v1/users/2fa/disable":       "re-mints auth_token behind AuthMiddleware after a TokenVersion bump — authenticated, not a public session mint",
}

// gateTestConfig returns a config that makes RegisterRoutes register the
// widest route set (metrics + CardDAV + CalDAV), with the client-version floor
// active so EnforceMinClientVersion is not inert.
func gateTestConfig() *config.Config {
	return &config.Config{
		JWTSecretKey:     "session-minting-gate-secret-key-that-is-long-enough",
		JWTExpiryHours:   96,
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
		MinClientVersion: "0.6.0",
		MetricsToken:     "metrics-token-16-chars-min",
		CardDAVEnabled:   true,
		CalDAVEnabled:    true,
	}
}

func TestSessionMintingRoutesGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := gateTestConfig()

	router := gin.New()
	RegisterRoutes(router, cfg, nil, nil)

	chains := handlerChainByRoute(t, router)

	// --- guard: the reflection walk still works against this gin version ---
	// Anchored on a route that is NOT one of this gate's subjects, so a real
	// invariant break below produces the specific failure, not a confusing
	// "walker is broken" here.
	require.Greaterf(t, len(chains), 100,
		"handlerChainByRoute recovered only %d routes — gin's routing tree was reshaped; update the walker", len(chains))
	healthChain, ok := chains["GET /health"]
	require.True(t, ok, "GET /health not found in the recovered chains — the walker is not enumerating routes")
	require.Contains(t, healthChain, "mycorrhizal/controllers.HealthCheck",
		"the walker found GET /health but not its handler — the walk is not reading handler chains correctly")

	// --- direction A: every declared session-minting route is wired --------
	for _, key := range sortedKeys(sessionMintingRoutes) {
		chain, ok := chains[key]
		require.Truef(t, ok, "declared session-minting route %q is not registered — stale entry in sessionMintingRoutes", key)

		floorIdx := indexOfMarker(chain, floorFuncMarker)
		rlIdx := indexOfMarker(chain, authRLFuncMarker)
		handlerIdx := len(chain) - 1

		assert.GreaterOrEqualf(t, floorIdx, 0,
			"session-minting route %q does not carry EnforceMinClientVersion — a client below the floor could mint a session through it (#692). Chain: %v", key, chain)
		assert.GreaterOrEqualf(t, rlIdx, 0,
			"session-minting route %q does not carry AuthRateLimitMiddleware — session minting is as sensitive as /login and must be rate limited the same way (#722). Chain: %v", key, chain)

		if floorIdx >= 0 {
			assert.Lessf(t, floorIdx, handlerIdx,
				"session-minting route %q registers EnforceMinClientVersion at or after its handler — it must run before any credential work. Chain: %v", key, chain)
		}
		if rlIdx >= 0 {
			assert.Lessf(t, rlIdx, handlerIdx,
				"session-minting route %q registers AuthRateLimitMiddleware at or after its handler. Chain: %v", key, chain)
		}
		if floorIdx >= 0 && rlIdx >= 0 {
			assert.Lessf(t, rlIdx, floorIdx,
				"session-minting route %q runs the client-version floor before the rate limiter — rate limiting must be outermost so a flood of below-floor requests is still shed. Chain: %v", key, chain)
		}
	}

	// --- direction B: no other route carries the floor --------------------
	for _, key := range sortedKeys(chains) {
		if _, declared := sessionMintingRoutes[key]; declared {
			continue
		}
		assert.Falsef(t, containsMarker(chains[key], floorFuncMarker),
			"route %q carries EnforceMinClientVersion but is not a declared session-minting route — either add it to sessionMintingRoutes (with the reason it mints a session) or remove the floor. A floor on a non-minting route is a policy smell. Chain: %v", key, chains[key])
	}

	// --- the documented exclusions stay honest ---------------------------
	for _, key := range sortedKeys(clientVersionFloorExclusions) {
		chain, ok := chains[key]
		require.Truef(t, ok, "documented floor-exclusion %q is not a registered route — stale entry in clientVersionFloorExclusions", key)
		assert.Falsef(t, containsMarker(chain, floorFuncMarker),
			"route %q is documented as deliberately outside the client-version floor, but now carries it — reconcile clientVersionFloorExclusions and sessionMintingRoutes. Chain: %v", key, chain)
	}
}

// TestSessionMintingRoutesGate_FloorEnforcedThroughLiveRouter ties the
// structural assertion above to real behaviour: with a floor configured, every
// declared session-minting route refuses a below-floor client with
// 403 CLIENT_NOT_SUPPORTED before any handler runs, and a non-minting control
// route does not. This guards against EnforceMinClientVersion being reduced to
// a pass-through while still appearing in the chain.
func TestSessionMintingRoutesGate_FloorEnforcedThroughLiveRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := gateTestConfig()

	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	RegisterRoutes(router, cfg, db, nil)

	// Each request carries a distinct X-Forwarded-For so it lands in its own
	// auth-rate-limiter bucket — the limiter is a process-global keyed by
	// client IP and other tests in this package share it, so a fixed IP could
	// arrive already throttled and turn a floor verdict into a spurious 429.
	var reqNo int
	belowFloor := func(method, path string) *httptest.ResponseRecorder {
		reqNo++
		req, _ := http.NewRequest(method, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(middleware.ClientVersionHeader, "0.5.0")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", reqNo))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	isClientNotSupported := func(w *httptest.ResponseRecorder) bool {
		if w.Code != http.StatusForbidden {
			return false
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			return false
		}
		return body.Error.Code == "CLIENT_NOT_SUPPORTED"
	}

	for _, key := range sortedKeys(sessionMintingRoutes) {
		method, path, _ := strings.Cut(key, " ")
		w := belowFloor(method, path)
		assert.Truef(t, isClientNotSupported(w),
			"below-floor client on session-minting route %q: want 403 CLIENT_NOT_SUPPORTED, got %d %s", key, w.Code, w.Body.String())
	}

	// Control: a non-minting public route with the same below-floor header is
	// NOT rejected by the floor (it fails on its own terms instead).
	w := belowFloor("POST", "/api/v1/password-reset/confirm")
	assert.Falsef(t, isClientNotSupported(w),
		"POST /api/v1/password-reset/confirm rejected a below-floor client — the floor has spread to a non-minting route. Got %d %s", w.Code, w.Body.String())
}

// handlerChainByRoute walks gin's unexported routing trees and returns, for
// every registered route, the ordered list of handler/middleware function
// names in its chain ("METHOD /full/path" -> [name, name, ...]). It uses plain
// reflect (no unsafe): every field read is either .String()/.Len()/.Index()/
// .Elem()/.IsNil() or .Pointer() on a func value, none of which require an
// exported or addressable value.
func handlerChainByRoute(t *testing.T, engine *gin.Engine) map[string][]string {
	t.Helper()
	out := map[string][]string{}

	ev := reflect.ValueOf(engine).Elem()
	trees := ev.FieldByName("trees")
	require.Truef(t, trees.IsValid() && trees.Kind() == reflect.Slice,
		"gin.Engine has no 'trees' slice field — gin internals changed; update handlerChainByRoute for the current version")

	var walk func(method string, nodePtr reflect.Value)
	walk = func(method string, nodePtr reflect.Value) {
		if !nodePtr.IsValid() || nodePtr.IsNil() {
			return
		}
		n := nodePtr.Elem()

		handlers := n.FieldByName("handlers")
		fullPath := n.FieldByName("fullPath")
		require.Truef(t, handlers.IsValid() && fullPath.IsValid(),
			"gin routing node has no 'handlers'/'fullPath' field — gin internals changed; update handlerChainByRoute")

		if handlers.Len() > 0 && fullPath.String() != "" {
			names := make([]string, 0, handlers.Len())
			for i := 0; i < handlers.Len(); i++ {
				pc := handlers.Index(i).Pointer()
				if fn := runtime.FuncForPC(pc); fn != nil {
					names = append(names, fn.Name())
				} else {
					names = append(names, "<unknown>")
				}
			}
			out[method+" "+fullPath.String()] = names
		}

		children := n.FieldByName("children")
		for i := 0; i < children.Len(); i++ {
			walk(method, children.Index(i))
		}
	}

	for i := 0; i < trees.Len(); i++ {
		tree := trees.Index(i)
		walk(tree.FieldByName("method").String(), tree.FieldByName("root"))
	}
	return out
}

// indexOfMarker returns the index of the first chain entry containing
// marker as a substring — see the comment on floorFuncMarker/authRLFuncMarker
// for why this is a substring match, not an exact one.
func indexOfMarker(haystack []string, marker string) int {
	for i, s := range haystack {
		if strings.Contains(s, marker) {
			return i
		}
	}
	return -1
}

// containsMarker reports whether any chain entry contains marker.
func containsMarker(haystack []string, marker string) bool {
	return indexOfMarker(haystack, marker) >= 0
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
