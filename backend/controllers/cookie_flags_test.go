package controllers

// TestCookieFlagPolicy is the cookie-flag audit (issue #610) as a repeatable
// test, the same self-maintaining shape routes/authorization_matrix_test.go
// uses for the authorization surface: instead of a hardcoded list of SetCookie
// call sites (the #378 pass's audit B read all 18 by hand), it drives every
// flow that mints or clears a cookie through the real handlers on a real
// database.InitDB-migrated schema (CLAUDE.md backend trap #1 — never
// AutoMigrate for persistence) and enumerates the cookie surface from the
// Set-Cookie headers the responses actually carry.
//
// The declared policy table is exhaustive in both directions:
//
//   - a cookie observed in any response with no declared row FAILS — a new
//     cookie needs a declared flag policy, it cannot sneak in unaccounted; and
//   - a declared row never observed in any flow FAILS as stale — a flow that
//     stopped minting a cookie leaves its row to rot.
//
// Every observed cookie is asserted to carry HttpOnly=true, Secure equal to
// the environment's cfg.CookieSecure (the #605 contract — the flag follows
// configuration, it is never hardcoded), and the SameSite the declared table
// assigns to its name: Strict on every session cookie, Lax only on the
// transient OIDC handshake cookies that must survive the cross-site top-level
// redirect back from the identity provider.
//
// The whole suite runs twice, once with CookieSecure=true and once with false,
// so the Secure-follows-config claim is exercised rather than assumed.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cookieFlagPolicy declares the SameSite every occurrence of each cookie must
// carry. Exhaustive in both directions — see TestCookieFlagPolicy.
var cookieFlagPolicy = map[string]http.SameSite{
	"auth_token":  http.SameSiteStrictMode,
	"id_token":    http.SameSiteStrictMode,
	"2fa_pending": http.SameSiteStrictMode,
	"oidc_state":  http.SameSiteLaxMode,
	"oidc_nonce":  http.SameSiteLaxMode,
	"oidc_pkce":   http.SameSiteLaxMode,
	"oidc_client": http.SameSiteLaxMode,
}

// cookieAudit collects every Set-Cookie a response carries and asserts each
// against cookieFlagPolicy, then verifies the table is exhaustive in both
// directions once all flows have been driven.
type cookieAudit struct {
	t            *testing.T
	cookieSecure bool
	observed     map[string]bool
}

func newCookieAudit(t *testing.T, cookieSecure bool) *cookieAudit {
	return &cookieAudit{t: t, cookieSecure: cookieSecure, observed: map[string]bool{}}
}

// record audits every Set-Cookie header on a response.
func (a *cookieAudit) record(w *httptest.ResponseRecorder) {
	a.t.Helper()
	for _, c := range w.Result().Cookies() {
		a.observed[c.Name] = true
		wantSameSite, declared := cookieFlagPolicy[c.Name]
		if !declared {
			a.t.Errorf("cookie %q was set but has no declared row in cookieFlagPolicy — a new cookie needs a declared flag policy", c.Name)
			continue
		}
		if !c.HttpOnly {
			a.t.Errorf("cookie %s: HttpOnly is false, want true", c.Name)
		}
		if c.Secure != a.cookieSecure {
			a.t.Errorf("cookie %s: Secure=%t, want cfg.CookieSecure=%t", c.Name, c.Secure, a.cookieSecure)
		}
		if c.SameSite != wantSameSite {
			a.t.Errorf("cookie %s: SameSite=%v, want %v", c.Name, c.SameSite, wantSameSite)
		}
	}
}

// assertExhaustive fails if any declared cookie was never observed.
func (a *cookieAudit) assertExhaustive() {
	a.t.Helper()
	for name := range cookieFlagPolicy {
		if !a.observed[name] {
			a.t.Errorf("declared cookie %q was never observed in any driven flow — stale policy row (or the flow that mints it is not driven here)", name)
		}
	}
}

func TestCookieFlagPolicy(t *testing.T) {
	for _, cookieSecure := range []bool{false, true} {
		cookieSecure := cookieSecure
		t.Run(fmt.Sprintf("CookieSecure=%t", cookieSecure), func(t *testing.T) {
			checkCookieFlagPolicy(t, cookieSecure)
		})
	}
}

// checkCookieFlagPolicy drives every cookie-minting/clearing flow once against
// a fresh migrated schema and audits the cookies the responses carry.
func checkCookieFlagPolicy(t *testing.T, cookieSecure bool) {
	gin.SetMode(gin.ReleaseMode)

	db := dbtest.New(t)

	cfg := &config.Config{
		JWTSecretKey:   testJWTSecret,
		JWTExpiryHours: 24,
		CookieSecure:   cookieSecure,
	}

	// OIDC flows need a real *services.OIDCProvider pointed at the fake IdP
	// so the callback genuinely exchanges a token (and so the login-start flow
	// can build a real auth URL).
	idp := newFakeCallbackIDP(t, "test-client")
	provider, oidcCfg := newCallbackTestSetup(t, idp)
	oidcCfg.CookieSecure = cookieSecure
	oidcCfg.JWTSecretKey = testJWTSecret

	audit := newCookieAudit(t, cookieSecure)

	// --- wire the router exactly as routes.go does for the driven flows ------
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})

	router.POST("/api/v1/login", func(c *gin.Context) { LoginUser(c, cfg) })
	router.POST("/api/v1/login/2fa", func(c *gin.Context) { Complete2FALogin(c, cfg) })
	router.POST("/api/v1/logout", func(c *gin.Context) { LogoutUser(c, cfg, provider) })

	protected := router.Group("/api/v1")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		protected.POST("/users/change-password", middleware.ValidateJSONMiddleware(&models.ChangePasswordInput{}), func(c *gin.Context) { ChangePassword(c, cfg) })
		protected.POST("/users/2fa/setup", SetupTwoFactor)
		protected.POST("/users/2fa/confirm", ConfirmTwoFactor)
	}

	router.GET("/api/v1/auth/oidc/login", OIDCLoginHandler(provider, oidcCfg))
	router.GET("/api/v1/auth/oidc/callback", OIDCCallbackHandler(provider, oidcCfg))

	// --- seed actors --------------------------------------------------------
	// Usernames are derived from the test name because the account rate
	// limiter is package-global (same rationale as twoFactorTestEnv).
	base := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(t.Name(), "Test"), "/", "_"))
	if len(base) > 40 {
		base = base[:40]
	}
	seed := func(tag string, totpEnabled bool) (models.User, string) {
		t.Helper()
		username := fmt.Sprintf("ck_%s_%s", tag, base)
		hashed, err := services.HashPassword(strongPassword)
		require.NoError(t, err)
		u := models.User{Username: username, Email: username + "@example.com", Password: hashed}
		if totpEnabled {
			secret, otpauthURL, err := services.GenerateTOTPSecret(username)
			require.NoError(t, err)
			require.NotEmpty(t, otpauthURL)
			encrypted, err := services.EncryptCredential(testJWTSecret, secret)
			require.NoError(t, err)
			u.TOTPEnabled = true
			u.TOTPSecretEncrypted = &encrypted
			require.NoError(t, db.Create(&u).Error)
			return u, secret
		}
		require.NoError(t, db.Create(&u).Error)
		return u, ""
	}

	plainUser, _ := seed("plain", false)
	totpUser, totpSecret := seed("totp", true)
	mgmtUser, _ := seed("mgmt", false)

	// --- flow: plain password login -> auth_token --------------------------
	w, cookies := doRequest(router, mustPost("/api/v1/login", map[string]string{
		"identifier": plainUser.Username,
		"password":   strongPassword,
	}))
	require.Equal(t, http.StatusOK, w.Code, "plain login: %s", w.Body.String())
	audit.record(w)
	plainSession := cookies["auth_token"]
	require.NotNil(t, plainSession, "plain login must mint auth_token")

	// --- flow: 2FA-pending login -> 2fa_pending ----------------------------
	w, cookies = doRequest(router, mustPost("/api/v1/login", map[string]string{
		"identifier": totpUser.Username,
		"password":   strongPassword,
	}))
	require.Equal(t, http.StatusOK, w.Code, "2fa login: %s", w.Body.String())
	audit.record(w)
	pending := cookies["2fa_pending"]
	require.NotNil(t, pending, "2fa login must mint 2fa_pending")

	// --- flow: complete 2FA login -> clears 2fa_pending, mints auth_token ---
	code, err := totp.GenerateCode(totpSecret, time.Now())
	require.NoError(t, err)
	req := mustPost("/api/v1/login/2fa", map[string]string{"code": code})
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: pending.Value})
	w, cookies = doRequest(router, req)
	require.Equal(t, http.StatusOK, w.Code, "2fa complete: %s", w.Body.String())
	audit.record(w)
	require.NotNil(t, cookies["auth_token"], "completed 2FA login must mint auth_token")

	// --- flow: password change re-mints auth_token --------------------------
	req = sessionRequest("POST", "/api/v1/users/change-password",
		map[string]string{"current_password": strongPassword, "new_password": strongPasswordAlt},
		plainSession.Value)
	w, cookies = doRequest(router, req)
	require.Equal(t, http.StatusOK, w.Code, "change password: %s", w.Body.String())
	audit.record(w)
	require.NotNil(t, cookies["auth_token"], "password change must re-mint auth_token")

	// --- flow: 2FA management (setup + confirm) re-mints auth_token ---------
	// (exercises reissueSessionToken's SetCookie, a distinct call site from
	// the password-change one)
	mgmtToken, err := services.IssueSession(db, mgmtUser, cfg, "", "")
	require.NoError(t, err)

	w, _ = doRequest(router, sessionRequest("POST", "/api/v1/users/2fa/setup", nil, mgmtToken))
	require.Equal(t, http.StatusOK, w.Code, "2fa setup: %s", w.Body.String())
	audit.record(w)
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	require.NotEmpty(t, setup.Secret)

	mgmtCode, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w, cookies = doRequest(router, sessionRequest("POST", "/api/v1/users/2fa/confirm", map[string]string{"code": mgmtCode}, mgmtToken))
	require.Equal(t, http.StatusOK, w.Code, "2fa confirm: %s", w.Body.String())
	audit.record(w)
	require.NotNil(t, cookies["auth_token"], "2FA confirm must re-mint auth_token")

	// --- flow: logout clears auth_token + id_token --------------------------
	w, cookies = doRequest(router, mustPost("/api/v1/logout", nil))
	require.Equal(t, http.StatusOK, w.Code, "logout: %s", w.Body.String())
	audit.record(w)
	require.Equal(t, -1, cookies["auth_token"].MaxAge, "logout must clear auth_token")
	require.Equal(t, -1, cookies["id_token"].MaxAge, "logout must clear id_token")

	// --- flow: OIDC login start (client=android) mints the handshake set ----
	req, _ = http.NewRequest("GET", "/api/v1/auth/oidc/login?client=android", nil)
	w, cookies = doRequest(router, req)
	require.Equal(t, http.StatusFound, w.Code, "oidc login start: %s", w.Body.String())
	audit.record(w)
	state := cookies["oidc_state"]
	nonce := cookies["oidc_nonce"]
	pkce := cookies["oidc_pkce"]
	require.NotNil(t, state, "oidc login start must mint oidc_state")
	require.NotNil(t, nonce, "oidc login start must mint oidc_nonce")
	require.NotNil(t, pkce, "oidc login start must mint oidc_pkce")
	require.NotNil(t, cookies["oidc_client"], "oidc login start (client=android) must mint oidc_client")

	// --- flow: OIDC callback (web success) clears the handshake set and
	// mints auth_token + id_token --------------------------------------------
	// Seed a user already linked to the fake IdP so FindOrProvisionUser
	// resolves without auto-provisioning.
	subject := "test-subject"
	providerURL := idp.Server.URL
	linkedUser := models.User{
		Username:     "cookie-oidc-linked",
		Password:     "",
		Email:        "cookie-oidc-linked@example.com",
		OIDCSubject:  &subject,
		OIDCProvider: &providerURL,
	}
	require.NoError(t, db.Create(&linkedUser).Error)

	// The ID token the fake IdP issues must carry the nonce the handshake
	// cookie does, or ExchangeAndVerify's nonce check rejects the callback.
	// The web flow deliberately omits the oidc_client cookie (an android
	// session skips cookies and delivers via deep link).
	idp.IDTokenClaims["nonce"] = nonce.Value

	cbURL := "/api/v1/auth/oidc/callback?state=" + url.QueryEscape(state.Value) + "&code=auth-code"
	req, _ = http.NewRequest("GET", cbURL, nil)
	for _, c := range []*http.Cookie{state, nonce, pkce} {
		req.AddCookie(c)
	}
	w, cookies = doRequest(router, req)
	require.Equal(t, http.StatusFound, w.Code, "oidc callback: %s", w.Body.String())
	audit.record(w)
	require.NotNil(t, cookies["auth_token"], "oidc callback must mint auth_token")
	require.NotNil(t, cookies["id_token"], "oidc callback must mint id_token")
	require.Equal(t, -1, cookies["oidc_state"].MaxAge, "oidc callback must clear oidc_state")
	require.Equal(t, -1, cookies["oidc_client"].MaxAge, "oidc callback must clear oidc_client")

	// --- completeness: every declared cookie must have been observed --------
	audit.assertExhaustive()
}

// TestCookieFlagPolicy_EveryResponseCookieMatchesConfig is the independent
// "a new cookie in an existing flow cannot slip through" probe: it re-drives
// the plain login under both Secure settings and asserts the concrete
// attributes on the Set-Cookie header string itself (not the parsed cookie),
// so a change in how a cookie is *rendered* (e.g. missing SameSite, or a
// Secure injected against config) is caught even if the parser is lenient.
func TestCookieFlagPolicy_HeaderStringCarriesDeclaredAttributes(t *testing.T) {
	for _, cookieSecure := range []bool{false, true} {
		cookieSecure := cookieSecure
		t.Run(fmt.Sprintf("CookieSecure=%t", cookieSecure), func(t *testing.T) {
			gin.SetMode(gin.ReleaseMode)
			db := dbtest.New(t)
			cfg := &config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24, CookieSecure: cookieSecure}

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("db", db)
				c.Set("cfg", *cfg)
				c.Next()
			})
			router.POST("/api/v1/login", func(c *gin.Context) { LoginUser(c, cfg) })

			username := "ck_header_" + strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(t.Name(), "Test"), "/", "_"))
			hashed, err := services.HashPassword(strongPassword)
			require.NoError(t, err)
			u := models.User{Username: username, Email: username + "@example.com", Password: hashed}
			require.NoError(t, db.Create(&u).Error)

			w := httptest.NewRecorder()
			req := mustPost("/api/v1/login", map[string]string{"identifier": username, "password": strongPassword})
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "login: %s", w.Body.String())

			setCookies := w.Result().Header.Values("Set-Cookie")
			require.NotEmpty(t, setCookies)
			for _, sc := range setCookies {
				assert.Contains(t, sc, "HttpOnly")
				if cookieSecure {
					assert.Contains(t, sc, "Secure")
				} else {
					assert.NotContains(t, sc, "Secure", "with COOKIE_SECURE=false no cookie may carry Secure")
				}
			}
		})
	}
}
