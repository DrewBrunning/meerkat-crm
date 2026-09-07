package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newClientVersionEnforcedRouter wires the four session-minting auth routes
// with EnforceMinClientVersion(cfg) in front of the real handlers, against a
// real migrated schema. It mirrors routes.go's ordering (Enforce runs before
// ValidateJSON and the handler) minus the auth rate limiter, which the request
// counts here would trip.
func newClientVersionEnforcedRouter(t *testing.T, cfg *config.Config) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	db := dbtest.New(t)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})

	router.POST("/api/v1/register", middleware.EnforceMinClientVersion(cfg), middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))
	router.POST("/api/v1/login", middleware.EnforceMinClientVersion(cfg), func(c *gin.Context) { LoginUser(c, cfg) })
	router.POST("/api/v1/login/2fa", middleware.EnforceMinClientVersion(cfg), func(c *gin.Context) { Complete2FALogin(c, cfg) })
	router.POST("/api/v1/auth/device/session", middleware.EnforceMinClientVersion(cfg), middleware.ValidateJSONMiddleware(&models.DeviceGrantSessionInput{}), func(c *gin.Context) { ExchangeDeviceGrant(c, cfg) })

	return db, router
}

// clientVersionRouterCfg returns a config with the floor set (or empty).
func clientVersionRouterCfg(floor string) *config.Config {
	return &config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24, MinClientVersion: floor}
}

// authDoJSON posts JSON with an optional X-Client-Version header.
func authDoJSON(router *gin.Engine, method, path, clientVersion string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if clientVersion != "" {
		req.Header.Set(middleware.ClientVersionHeader, clientVersion)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// seedLoginUser creates a plain user whose password is strongPassword.
func seedLoginUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	user := models.User{Username: "clientveruser", Email: "clientveruser@example.com", Password: hashed}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func assertClientNotSupported(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "CLIENT_NOT_SUPPORTED", body.Error.Code)
	assert.NotEmpty(t, body.Error.Message)
}

// --- login: the authoritative backstop -------------------------------------

func TestClientVersionEnforcement_LoginRejectsOldClientBeforeCredentials(t *testing.T) {
	db, router := newClientVersionEnforcedRouter(t, clientVersionRouterCfg("0.6.0"))
	seedLoginUser(t, db)

	// Old client + VALID credentials -> refused before any credential work.
	rec := authDoJSON(router, "POST", "/api/v1/login", "0.5.0", map[string]string{"identifier": "clientveruser", "password": strongPassword})
	assertClientNotSupported(t, rec)
	// No session cookie may be set by the rejection.
	assert.NotContains(t, rec.Header().Get("Set-Cookie"), "auth_token")

	// Old client + WRONG credentials -> still the floor rejection (403), not a
	// credential verdict (401): the handler never looked at the password.
	rec = authDoJSON(router, "POST", "/api/v1/login", "0.5.0", map[string]string{"identifier": "clientveruser", "password": "definitelyWrongPassword1"})
	assertClientNotSupported(t, rec)

	// At-or-above-floor client with valid credentials authenticates normally.
	rec = authDoJSON(router, "POST", "/api/v1/login", "0.6.0", map[string]string{"identifier": "clientveruser", "password": strongPassword})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "auth_token")

	// Newer client also authenticates.
	rec = authDoJSON(router, "POST", "/api/v1/login", "0.9.9", map[string]string{"identifier": "clientveruser", "password": strongPassword})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Absent / invalid version headers are refused once a floor is declared.
	for _, missing := range []string{"", "not-a-version"} {
		rec = authDoJSON(router, "POST", "/api/v1/login", missing, map[string]string{"identifier": "clientveruser", "password": strongPassword})
		assertClientNotSupported(t, rec)
	}
}

func TestClientVersionEnforcement_LoginNoFloorIsInert(t *testing.T) {
	db, router := newClientVersionEnforcedRouter(t, clientVersionRouterCfg(""))
	seedLoginUser(t, db)

	// The policy default: no floor declared, every client authenticates —
	// including a headerless and a garbage-header request.
	for _, clientVersion := range []string{"0.5.0", "", "garbage"} {
		rec := authDoJSON(router, "POST", "/api/v1/login", clientVersion, map[string]string{"identifier": "clientveruser", "password": strongPassword})
		assert.Equal(t, http.StatusOK, rec.Code, "client version %q with no floor declared must pass: %s", clientVersion, rec.Body.String())
	}
}

// --- register: an old client must not be able to create an account ---------

func TestClientVersionEnforcement_RegisterRejectsOldClientWithoutCreatingUser(t *testing.T) {
	db, router := newClientVersionEnforcedRouter(t, clientVersionRouterCfg("0.6.0"))

	before := int64(0)
	require.NoError(t, db.Model(&models.User{}).Count(&before).Error)

	body := map[string]string{
		"username": "newclientuser",
		"email":    "newclientuser@example.com",
		"password": strongPassword,
	}

	// Old client -> refused, and no user row is created (no account the client
	// could never use).
	rec := authDoJSON(router, "POST", "/api/v1/register", "0.5.0", body)
	assertClientNotSupported(t, rec)
	var after int64
	require.NoError(t, db.Model(&models.User{}).Count(&after).Error)
	assert.Equal(t, before, after)

	// At/above-floor client registers normally.
	rec = authDoJSON(router, "POST", "/api/v1/register", "0.6.0", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// --- 2FA step + device-grant exchange are gated the same way ---------------

func TestClientVersionEnforcement_Gates2FAAndDeviceSession(t *testing.T) {
	_, router := newClientVersionEnforcedRouter(t, clientVersionRouterCfg("0.6.0"))

	// Below floor -> the floor rejection fires before either handler runs, so
	// even a malformed body is never reached.
	rec := authDoJSON(router, "POST", "/api/v1/login/2fa", "0.5.0", map[string]string{"code": "123456"})
	assertClientNotSupported(t, rec)

	// At floor -> passes the gate and reaches the handler, which fails on its
	// own terms (no pending 2FA challenge) rather than on the version check.
	rec = authDoJSON(router, "POST", "/api/v1/login/2fa", "0.6.0", map[string]string{"code": "123456"})
	assert.NotEqual(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = authDoJSON(router, "POST", "/api/v1/auth/device/session", "0.5.0", map[string]string{"device_token": "x"})
	assertClientNotSupported(t, rec)

	rec = authDoJSON(router, "POST", "/api/v1/auth/device/session", "0.6.0", map[string]string{"device_token": "x"})
	assert.NotEqual(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// --- the response body must be actionable for a client that reads it --------

func TestClientVersionEnforcement_RejectionNamesRequiredVersion(t *testing.T) {
	db, router := newClientVersionEnforcedRouter(t, clientVersionRouterCfg("0.7.0"))
	seedLoginUser(t, db)

	rec := authDoJSON(router, "POST", "/api/v1/login", "0.6.9", map[string]string{"identifier": "clientveruser", "password": strongPassword})
	assertClientNotSupported(t, rec)
	assert.True(t, strings.Contains(rec.Body.String(), "0.7.0"),
		"rejection body should name the required version: %s", rec.Body.String())
}
