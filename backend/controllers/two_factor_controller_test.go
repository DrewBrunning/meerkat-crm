package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"gorm.io/gorm"
)

// testJWTSecret must satisfy config.Validate's ≥32-char rule where it matters;
// these tests never call Validate, but keeping it realistic avoids surprises.
const testJWTSecret = "test-jwt-secret-that-is-at-least-32-characters-long"

// twoFactorTestEnv builds a real migrated database (database.InitDB — CLAUDE.md
// backend trap 1: never AutoMigrate for anything touching persistence) with the
// login, 2FA-login, and protected 2FA-management routes wired, and one seeded
// password user. Authenticated requests must carry an auth_token cookie minted
// for that user.
//
// The username is derived from the test name because the account rate limiter
// is package-global: two tests sharing a username would bleed failures into
// each other (the code-step rate-limit test deliberately locks its account).
func twoFactorTestEnv(t *testing.T) (*gorm.DB, *gin.Engine, *config.Config, models.User) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	db := dbtest.New(t)

	cfg := &config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24}

	username := "tf_" + strings.ReplaceAll(strings.TrimPrefix(t.Name(), "Test"), "/", "_")
	username = strings.ToLower(username) // LoginUser lowercases the identifier
	if len(username) > 50 {
		username = username[:50]
	}
	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	user := models.User{Username: username, Email: strings.ToLower(username) + "@example.com", Password: hashed}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	router.POST("/login", func(c *gin.Context) { LoginUser(c, cfg) })
	router.POST("/login/2fa", func(c *gin.Context) { Complete2FALogin(c, cfg) })

	protected := router.Group("/")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		protected.GET("/users/2fa/status", GetTwoFactorStatus)
		protected.POST("/users/2fa/setup", SetupTwoFactor)
		protected.POST("/users/2fa/confirm", ConfirmTwoFactor)
		protected.POST("/users/2fa/disable", DisableTwoFactor)
		protected.POST("/users/2fa/recovery-codes/regenerate", RegenerateRecoveryCodes)
	}

	return db, router, cfg, user
}

// totpCodeAt computes the RFC 6238 code for secret at the given time — the
// code a real authenticator app would produce.
func totpCodeAt(t *testing.T, secret string, when time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, when)
	require.NoError(t, err)
	return code
}

func totpCode(t *testing.T, secret string) string {
	return totpCodeAt(t, secret, time.Now())
}

// sessionRequest returns a request carrying the given auth_token cookie value.
func sessionRequest(method, path string, body any, token string) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	}
	return req
}

// doRequest serves the request and returns the recorder plus any set-cookie
// values keyed by name (for asserting cookie behavior).
func doRequest(router *gin.Engine, req *http.Request) (*httptest.ResponseRecorder, map[string]*http.Cookie) {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	cookies := map[string]*http.Cookie{}
	for _, c := range w.Result().Cookies() {
		cookies[c.Name] = c
	}
	return w, cookies
}

// enableTwoFactor walks the whole enrollment flow for the seeded user and
// returns the stored (encrypted) secret so tests can mint fresh TOTP codes,
// the recovery codes, and the REISSUED session token from the confirm
// response. Using that cookie value (not a freshly-minted token) is the point:
// it pins the bug where the enrollment's token_version bump left the reissued
// token stale and AuthMiddleware rejected it — the user's own session used to
// die the moment they enabled 2FA.
func enableTwoFactor(t *testing.T, db *gorm.DB, router *gin.Engine, cfg *config.Config, user models.User) (secret string, recoveryCodes []string, reissuedSession string) {
	t.Helper()

	token, err := services.IssueSession(db, user, cfg, "", "")
	require.NoError(t, err)

	// setup
	req := sessionRequest("POST", "/users/2fa/setup", nil, token)
	w, _ := doRequest(router, req)
	require.Equal(t, http.StatusOK, w.Code, "setup: %s", w.Body.String())
	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	require.NotEmpty(t, setup.Secret)
	require.Contains(t, setup.OtpauthURL, "otpauth://")

	// wrong code is rejected
	req = sessionRequest("POST", "/users/2fa/confirm", map[string]string{"code": "000000"}, token)
	w, _ = doRequest(router, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Confirm with a code from the PREVIOUS 30s step (still inside the server's
	// ±1 step window). Enrollment now burns the confirming code's step for
	// single use (issue #873, RFC 6238 §5.2); spending step S-1 here leaves the
	// current step free for whatever TOTP operation the caller does next. Real
	// users are 30s apart between codes; these tests are milliseconds apart.
	req = sessionRequest("POST", "/users/2fa/confirm",
		map[string]string{"code": totpCodeAt(t, setup.Secret, time.Now().Add(-30*time.Second))}, token)
	w, cookies := doRequest(router, req)
	require.Equal(t, http.StatusOK, w.Code, "confirm: %s", w.Body.String())
	var confirm struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &confirm))
	require.Len(t, confirm.RecoveryCodes, 10)

	// The reissued auth_token cookie must be a live session for the
	// post-enrollment user (TokenVersion bumped by the confirm transaction).
	require.NotNil(t, cookies["auth_token"], "enrollment must re-issue the session cookie")

	return setup.Secret, confirm.RecoveryCodes, cookies["auth_token"].Value
}

func TestTwoFactor_EnrollmentFlow(t *testing.T) {
	db, router, _, user := twoFactorTestEnv(t)
	token, err := services.IssueSession(db, user, &config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24}, "", "")
	require.NoError(t, err)

	// Issue #722: a remembered device enrolled under the pre-2FA posture must
	// be revoked the moment 2FA is enabled.
	grant, _, err := services.CreateDeviceGrant(db, user.ID, "test-phone")
	require.NoError(t, err)

	// status: disabled before anything
	w, _ := doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, token))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"enabled":false}`, w.Body.String())

	secret, _, sessionToken := enableTwoFactor(t, db, router, &config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24}, user)

	// The enrollment bump revoked every standing device grant.
	var reloaded models.DeviceGrant
	require.NoError(t, db.First(&reloaded, grant.ID).Error)
	require.NotNil(t, reloaded.RevokedAt, "enabling 2FA must revoke device grants")

	// status: enabled now (post-enrollment token, since TokenVersion bumped)
	w, _ = doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, sessionToken))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"enabled":true}`, w.Body.String())

	// secret is stored encrypted, never plaintext
	var stored models.User
	err = db.First(&stored, user.ID).Error
	require.NoError(t, err)
	require.NotNil(t, stored.TOTPSecretEncrypted)
	require.NotEqual(t, secret, *stored.TOTPSecretEncrypted)
	assert.True(t, stored.TOTPEnabled)
	require.NotNil(t, stored.TOTPConfirmedAt)

	// second setup/confirm both conflict
	w, _ = doRequest(router, sessionRequest("POST", "/users/2fa/setup", nil, sessionToken))
	assert.Equal(t, http.StatusConflict, w.Code)
	w, _ = doRequest(router, sessionRequest("POST", "/users/2fa/confirm", map[string]string{"code": totpCode(t, secret)}, sessionToken))
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestTwoFactor_LoginBlockedWithoutSecondFactor(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	secret, _, _ := enableTwoFactor(t, db, router, cfg, user)

	// Step 1: password alone must NOT issue a session.
	w, cookies := doRequest(router, mustPost("/login", map[string]string{
		"identifier": user.Username,
		"password":   strongPassword,
	}))
	require.Equal(t, http.StatusOK, w.Code)
	var login struct {
		TwoFactorRequired bool `json:"two_factor_required"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	assert.True(t, login.TwoFactorRequired)
	assert.NotNil(t, cookies["2fa_pending"], "a 2fa_pending challenge cookie must be set")
	assert.Nil(t, cookies["auth_token"], "no session may be minted before the second factor")

	// Step 2: wrong code is rejected and still no session.
	req := sessionRequest("POST", "/login/2fa", map[string]string{"code": "000000"}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: cookies["2fa_pending"].Value})
	w, cookies2 := doRequest(router, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Nil(t, cookies2["auth_token"])

	// Step 2: correct TOTP code completes login.
	req = sessionRequest("POST", "/login/2fa", map[string]string{"code": totpCode(t, secret)}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: cookies["2fa_pending"].Value})
	w, cookies3 := doRequest(router, req)
	require.Equal(t, http.StatusOK, w.Code, "login/2fa: %s", w.Body.String())
	assert.NotNil(t, cookies3["auth_token"], "a real session must be minted")
	assert.NotNil(t, cookies3["2fa_pending"], "the challenge cookie must be cleared (max-age <= 0 still appears)")

	// The minted auth_token is a real session.
	sessionToken := cookies3["auth_token"].Value
	w, _ = doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, sessionToken))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTwoFactor_LoginWithoutPendingChallengeRejected(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	enableTwoFactor(t, db, router, cfg, user)

	// No 2fa_pending cookie at all.
	w, _ := doRequest(router, mustPost("/login/2fa", map[string]string{"code": "000000"}))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTwoFactor_RecoveryCodeWorksExactlyOnce(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	_, recoveryCodes, _ := enableTwoFactor(t, db, router, cfg, user)
	code := recoveryCodes[0]

	// First use succeeds and mints a session.
	w, cookies := doRequest(router, loginWith2FA(router, user, code))
	require.Equal(t, http.StatusOK, w.Code, "first recovery use: %s", w.Body.String())
	assert.NotNil(t, cookies["auth_token"])

	// Same code again must fail — it was consumed by the first use.
	w, _ = doRequest(router, loginWith2FA(router, user, code))
	require.Equal(t, http.StatusBadRequest, w.Code, "reused recovery code must be rejected: %s", w.Body.String())

	// A different unused code still works.
	w, cookies = doRequest(router, loginWith2FA(router, user, recoveryCodes[1]))
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, cookies["auth_token"])
}

func TestTwoFactor_CodeStepIsRateLimited(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	enableTwoFactor(t, db, router, cfg, user)

	// Start the login ONCE and reuse the 2fa_pending challenge: the attack
	// pattern is one password step then repeated code guesses. Re-running step
	// 1 each time would clear the failure counter (RecordSuccessfulLogin),
	// which is exactly the real-world behavior this test must not emulate.
	_, step1Cookies := doRequest(router, mustPost("/login", map[string]string{
		"identifier": user.Username,
		"password":   strongPassword,
	}))
	require.NotNil(t, step1Cookies["2fa_pending"])

	// Exhaust the account's failure budget: MaxLoginAttempts-1 wrong codes are
	// rejected, the MaxLoginAttempts-th failure trips the lockout (429), and
	// everything after stays locked.
	codeReq := func(code string) *http.Request {
		req := sessionRequest("POST", "/login/2fa", map[string]string{"code": code}, "")
		req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: step1Cookies["2fa_pending"].Value})
		return req
	}
	for i := 0; i < middleware.MaxLoginAttempts-1; i++ {
		w, _ := doRequest(router, codeReq("000000"))
		require.Equal(t, http.StatusBadRequest, w.Code, "attempt %d: %s", i+1, w.Body.String())
	}
	w, _ := doRequest(router, codeReq("000000"))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Account is now locked for the password step too (same limiter, N8).
	w, _ = doRequest(router, mustPost("/login", map[string]string{
		"identifier": user.Username,
		"password":   strongPassword,
	}))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestTwoFactor_DisableFlow(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	secret, recoveryCodes, sessionToken := enableTwoFactor(t, db, router, cfg, user)
	require.NotEmpty(t, recoveryCodes)

	// Issue #722: a device grant minted while 2FA was on must be revoked when
	// 2FA is switched off (the TOTP that backed a stolen grant is now gone).
	grant, _, err := services.CreateDeviceGrant(db, user.ID, "test-phone")
	require.NoError(t, err)

	// Wrong code can't disable.
	w, _ := doRequest(router, sessionRequest("POST", "/users/2fa/disable", map[string]string{"code": "000000"}, sessionToken))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Correct code disables and re-issues the session.
	w, cookies := doRequest(router, sessionRequest("POST", "/users/2fa/disable", map[string]string{"code": totpCode(t, secret)}, sessionToken))
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, cookies["auth_token"])

	var reloaded models.DeviceGrant
	require.NoError(t, db.First(&reloaded, grant.ID).Error)
	require.NotNil(t, reloaded.RevokedAt, "disabling 2FA must revoke device grants")

	var stored models.User
	require.NoError(t, db.First(&stored, user.ID).Error)
	assert.False(t, stored.TOTPEnabled)
	assert.Nil(t, stored.TOTPSecretEncrypted)
	var codeCount int64
	require.NoError(t, db.Model(&models.RecoveryCode{}).Where("user_id = ?", user.ID).Count(&codeCount).Error)
	assert.Zero(t, codeCount, "recovery codes must be cleared on disable")

	// 2FA no longer required at login.
	w, _ = doRequest(router, mustPost("/login", map[string]string{
		"identifier": user.Username,
		"password":   strongPassword,
	}))
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "two_factor_required")
}

func TestTwoFactor_RegenerateRecoveryCodes(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	secret, _, sessionToken := enableTwoFactor(t, db, router, cfg, user)

	w, _ := doRequest(router, sessionRequest("POST", "/users/2fa/recovery-codes/regenerate", map[string]string{"code": totpCode(t, secret)}, sessionToken))
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.RecoveryCodes, 10)

	var count int64
	require.NoError(t, db.Model(&models.RecoveryCode{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Equal(t, int64(10), count)
}

func TestTwoFactor_OIDCUserCannotEnroll(t *testing.T) {
	db, router, cfg, _ := twoFactorTestEnv(t)

	oidcSubject := "sub-123"
	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	oidcUser := models.User{Username: "sso-user", Email: "sso@example.com", Password: hashed, OIDCSubject: &oidcSubject}
	require.NoError(t, db.Create(&oidcUser).Error)

	token, err := services.IssueSession(db, oidcUser, cfg, "", "")
	require.NoError(t, err)

	w, _ := doRequest(router, sessionRequest("POST", "/users/2fa/setup", nil, token))
	assert.Equal(t, http.StatusForbidden, w.Code)
	w, _ = doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, token))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"enabled":false}`, w.Body.String())
}

func TestTwoFactor_ChallengeTokenIsNotASession(t *testing.T) {
	_, router, cfg, user := twoFactorTestEnv(t)

	challenge, err := services.Generate2FAChallengeToken(user, cfg)
	require.NoError(t, err)

	// The 2fa-purpose challenge must never authenticate a protected route.
	w, _ := doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, challenge))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTwoFactor_OwnershipScoping(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	enableTwoFactor(t, db, router, cfg, user)

	// A second user must not be able to read the first user's 2FA state via
	// their own token (status is per-caller).
	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	other := models.User{Username: "other-user", Email: "other@example.com", Password: hashed}
	require.NoError(t, db.Create(&other).Error)
	token, err := services.IssueSession(db, other, cfg, "", "")
	require.NoError(t, err)

	w, _ := doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, token))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"enabled":false}`, w.Body.String())

	// A challenge minted for user A plus a code from user A's own secret logs
	// in as A — but a code from user B's (absent) secret cannot. The binding
	// to exercise: a challenge can never authenticate as a different user.
	challenge, err := services.Generate2FAChallengeToken(user, cfg)
	require.NoError(t, err)
	req := sessionRequest("POST", "/login/2fa", map[string]string{"code": "000000"}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: challenge})
	w, _ = doRequest(router, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a wrong code must never complete the login")
}

func TestTwoFactor_CrossUserRecoveryCodeRejected(t *testing.T) {
	db, router, cfg, userA := twoFactorTestEnv(t)
	_, codesA, _ := enableTwoFactor(t, db, router, cfg, userA)
	require.NotEmpty(t, codesA)

	// A second user with their own 2FA (and their own recovery codes).
	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	userB := models.User{Username: "twofa-b-user", Email: "twofa-b@example.com", Password: hashed}
	require.NoError(t, db.Create(&userB).Error)
	_, _, _ = enableTwoFactor(t, db, router, cfg, userB)

	// Steal user A's pending challenge, then try to complete it with one of
	// user B's recovery codes. The code must NOT validate: it belongs to B's
	// secret and B's recovery rows, not A's.
	challengeForA, err := services.Generate2FAChallengeToken(userA, cfg)
	require.NoError(t, err)
	req := sessionRequest("POST", "/login/2fa", map[string]string{"code": codesA[0]}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: challengeForA})
	w, cookies := doRequest(router, req)
	require.Equal(t, http.StatusOK, w.Code, "A's own code must complete A's login: %s", w.Body.String())
	assert.NotNil(t, cookies["auth_token"])

	// Now the reverse direction: A's code must not complete B's challenge.
	challengeForB, err := services.Generate2FAChallengeToken(userB, cfg)
	require.NoError(t, err)
	req = sessionRequest("POST", "/login/2fa", map[string]string{"code": codesA[0]}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: challengeForB})
	w, cookies = doRequest(router, req)
	require.Equal(t, http.StatusBadRequest, w.Code, "A's code must not complete B's login: %s", w.Body.String())
	assert.Nil(t, cookies["auth_token"])
}

func TestTwoFactor_ReissuedSessionIsLiveAfterEnableAndDisable(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)

	// After enrollment, the REISSUED cookie from the response (not a freshly
	// minted token) must authenticate a protected route.
	secret, _, sessionAfterEnable := enableTwoFactor(t, db, router, cfg, user)
	w, _ := doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, sessionAfterEnable))
	require.Equal(t, http.StatusOK, w.Code, "reissued session after enable: %s", w.Body.String())
	assert.Equal(t, `{"enabled":true}`, w.Body.String())

	// Disable using that same reissued session (the realistic path: the user
	// who just enabled stays logged in, and can still disable later).
	w, cookies := doRequest(router, sessionRequest("POST", "/users/2fa/disable",
		map[string]string{"code": totpCode(t, secret)}, sessionAfterEnable))
	require.Equal(t, http.StatusOK, w.Code, "disable: %s", w.Body.String())
	require.NotNil(t, cookies["auth_token"], "disable must re-issue the session cookie")

	// The disable reissued cookie must also be a live session.
	w, _ = doRequest(router, sessionRequest("GET", "/users/2fa/status", nil, cookies["auth_token"].Value))
	require.Equal(t, http.StatusOK, w.Code, "reissued session after disable: %s", w.Body.String())
	assert.Equal(t, `{"enabled":false}`, w.Body.String())
}

func TestTwoFactor_ChallengeRejectedAfterDisable(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	secret, _, sessionCookie := enableTwoFactor(t, db, router, cfg, user)

	// Complete a 2FA login so we have a real, unspent challenge cookie value.
	_, step1Cookies := doRequest(router, mustPost("/login", map[string]string{
		"identifier": user.Username,
		"password":   strongPassword,
	}))
	require.NotNil(t, step1Cookies["2fa_pending"])
	pending := step1Cookies["2fa_pending"].Value

	// Disable 2FA before the challenge is spent.
	w, _ := doRequest(router, sessionRequest("POST", "/users/2fa/disable",
		map[string]string{"code": totpCode(t, secret)}, sessionCookie))
	require.Equal(t, http.StatusOK, w.Code)

	// The pre-existing challenge must no longer complete the login: 2FA was
	// disabled, so the minted challenge is void.
	req := sessionRequest("POST", "/login/2fa", map[string]string{"code": totpCode(t, secret)}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: pending})
	w, cookies := doRequest(router, req)
	require.Equal(t, http.StatusUnauthorized, w.Code, "a challenge minted before 2FA was disabled must be void: %s", w.Body.String())
	assert.Nil(t, cookies["auth_token"])
}

// mustPost builds a JSON POST request with no auth cookie.
func mustPost(path string, body map[string]string) *http.Request {
	return sessionRequest("POST", path, body, "")
}

// loginWith2FA runs the two-step login: POST /login for the user (password),
// then POST /login/2fa with the given code using the resulting 2fa_pending
// cookie.
func loginWith2FA(router *gin.Engine, user models.User, code string) *http.Request {
	w, cookies := doRequest(router, mustPost("/login", map[string]string{
		"identifier": user.Username,
		"password":   strongPassword,
	}))
	if w.Code != http.StatusOK {
		return mustPost("/login/2fa", map[string]string{"code": code})
	}
	req := sessionRequest("POST", "/login/2fa", map[string]string{"code": code}, "")
	req.AddCookie(&http.Cookie{Name: "2fa_pending", Value: cookies["2fa_pending"].Value})
	return req
}

// TestTwoFactor_TOTPReplayRejectedWithinWindow is the issue #873 repro: a TOTP
// code that authenticated one session cannot be replayed to authenticate a
// second, independent session inside its ±1 step validity window. Before the
// single-use burn the second /login/2fa returned 200 with a fresh auth_token.
func TestTwoFactor_TOTPReplayRejectedWithinWindow(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)
	secret, _, _ := enableTwoFactor(t, db, router, cfg, user)

	// enrollment burned step S-1; this code is step S.
	code := totpCode(t, secret)

	// Session A: first use authenticates.
	w, cookies := doRequest(router, loginWith2FA(router, user, code))
	require.Equal(t, http.StatusOK, w.Code, "first use: %s", w.Body.String())
	require.NotNil(t, cookies["auth_token"])

	// Session B: a brand-new pending challenge (fresh /login), same code — must
	// be rejected now.
	w, cookies = doRequest(router, loginWith2FA(router, user, code))
	require.Equal(t, http.StatusBadRequest, w.Code, "replayed TOTP code must be rejected: %s", w.Body.String())
	assert.Nil(t, cookies["auth_token"])

	// A code from the next step still authenticates — the burn spends the used
	// step, it does not disable the authenticator.
	next := totpCodeAt(t, secret, time.Now().Add(30*time.Second))
	require.NotEqual(t, code, next)
	w, cookies = doRequest(router, loginWith2FA(router, user, next))
	require.Equal(t, http.StatusOK, w.Code, "a fresh code must still authenticate: %s", w.Body.String())
	assert.NotNil(t, cookies["auth_token"])

	var stored models.User
	require.NoError(t, db.First(&stored, user.ID).Error)
	require.NotNil(t, stored.TOTPLastUsedStep)
}

// TestTwoFactor_ConfirmRecordsStepSoEnrollmentCodeCannotLogin pins that the code
// used to *enable* 2FA is itself burned: an attacker who observes the enrollment
// code cannot replay it at the account's first login.
func TestTwoFactor_ConfirmRecordsStepSoEnrollmentCodeCannotLogin(t *testing.T) {
	db, router, cfg, user := twoFactorTestEnv(t)

	// Enroll manually so the test holds the exact confirming code.
	token, err := services.IssueSession(db, user, cfg, "", "")
	require.NoError(t, err)
	w, _ := doRequest(router, sessionRequest("POST", "/users/2fa/setup", nil, token))
	require.Equal(t, http.StatusOK, w.Code, "setup: %s", w.Body.String())
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	require.NotEmpty(t, setup.Secret)

	enrollCode := totpCode(t, setup.Secret)
	w, _ = doRequest(router, sessionRequest("POST", "/users/2fa/confirm", map[string]string{"code": enrollCode}, token))
	require.Equal(t, http.StatusOK, w.Code, "confirm: %s", w.Body.String())

	var stored models.User
	require.NoError(t, db.First(&stored, user.ID).Error)
	require.NotNil(t, stored.TOTPLastUsedStep, "confirm must record the consumed step")

	// The same enrollment code must not complete a login.
	w, cookies := doRequest(router, loginWith2FA(router, user, enrollCode))
	require.Equal(t, http.StatusBadRequest, w.Code, "enrollment code must not be replayable at login: %s", w.Body.String())
	assert.Nil(t, cookies["auth_token"])
}
