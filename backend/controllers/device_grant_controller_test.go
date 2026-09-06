package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// deviceGrantRouter mounts the device-grant surface against a real migrated
// database, authenticated as actingUser, with a fixed JWT secret so the
// exchange endpoint can actually mint a session.
func deviceGrantRouter(t *testing.T, actingUser models.User, cfg config.Config) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	db := dbtest.New(t)
	require.NoError(t, db.Create(&actingUser).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", actingUser.ID)
		c.Set("cfg", cfg)
		c.Next()
	})

	router.POST("/auth/device/session", func(c *gin.Context) { ExchangeDeviceGrant(c, &cfg) })
	router.GET("/auth/device/grants", ListDeviceGrants)
	router.POST("/auth/device/grants", CreateDeviceGrant)
	router.POST("/auth/device/grants/revoke-all", RevokeAllDeviceGrants)
	router.DELETE("/auth/device/grants/:id", RevokeDeviceGrant)
	return db, router
}

func postJSON(t *testing.T, router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func testConfig() config.Config {
	return config.Config{JWTSecretKey: "0123456789abcdef0123456789abcdef", JWTExpiryHours: 96}
}

// Issue #722: the full biometric-login round trip — enroll (create a grant
// while authenticated), then possess it (exchange for a fresh session cookie).
func TestDeviceGrant_EnrollThenExchangeMintsSession(t *testing.T) {
	db, router := deviceGrantRouter(t, models.User{Username: "alice", Email: "alice@example.com", Password: "x"}, testConfig())

	created := postJSON(t, router, "/auth/device/grants", `{"label":"Pixel 8a"}`)
	require.Equal(t, http.StatusCreated, created.Code)
	var createResp models.DeviceGrantCreateResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createResp))
	require.NotEmpty(t, createResp.Token)
	require.NotEmpty(t, createResp.ID)
	require.Equal(t, "Pixel 8a", createResp.Label)

	// Only the SHA-256 hash is stored, never the plaintext.
	var stored models.DeviceGrant
	require.NoError(t, db.First(&stored, createResp.ID).Error)
	assert.Equal(t, services.HashDeviceGrantToken(createResp.Token), stored.TokenHash)
	assert.NotEqual(t, createResp.Token, stored.TokenHash)

	// Exchange possession of the grant for a fresh session, exactly like a
	// password login: auth_token cookie + language/date_format body.
	exchanged := postJSON(t, router, "/auth/device/session", `{"device_token":"`+createResp.Token+`"}`)
	require.Equal(t, http.StatusOK, exchanged.Code, exchanged.Body.String())
	authCookie := exchanged.Header().Get("Set-Cookie")
	assert.Contains(t, authCookie, "auth_token=")
	assert.Contains(t, authCookie, "Secure")
	assert.Contains(t, authCookie, "HttpOnly")
	assert.Contains(t, authCookie, "SameSite=Strict")
}

func TestDeviceGrant_ExchangeWithUnknownOrRevokedTokenIsRejected(t *testing.T) {
	db, router := deviceGrantRouter(t, models.User{Username: "bob", Email: "bob@example.com", Password: "x"}, testConfig())

	// Unknown token → same generic response as bad credentials (no enumeration).
	unknown := postJSON(t, router, "/auth/device/session", `{"device_token":"`+strings.Repeat("A", 43)+`"}`)
	require.Equal(t, http.StatusUnauthorized, unknown.Code)

	// A valid grant that is then revoked stops minting sessions.
	created := postJSON(t, router, "/auth/device/grants", `{}`)
	var createResp models.DeviceGrantCreateResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createResp))
	revoked := postJSON(t, router, "/auth/device/grants/revoke-all", `{}`)
	require.Equal(t, http.StatusOK, revoked.Code)

	var count int64
	require.NoError(t, db.Model(&models.DeviceGrant{}).Where("id = ? AND revoked_at IS NULL", createResp.ID).Count(&count).Error)
	assert.Zero(t, count, "revoke-all must end every standing grant")

	after := postJSON(t, router, "/auth/device/session", `{"device_token":"`+createResp.Token+`"}`)
	require.Equal(t, http.StatusUnauthorized, after.Code)
}

func TestDeviceGrant_ListScopesToCallerAndRevokeRespectsOwnership(t *testing.T) {
	db, router := deviceGrantRouter(t, models.User{Username: "carol", Email: "carol@example.com", Password: "x"}, testConfig())
	other := models.User{Username: "dave", Email: "dave@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)

	postJSON(t, router, "/auth/device/grants", `{"label":"mine"}`)

	// A second caller's grant is invisible to the first.
	otherRouter := gin.New()
	otherRouter.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", other.ID)
		c.Set("cfg", testConfig())
		c.Next()
	})
	otherRouter.GET("/auth/device/grants", ListDeviceGrants)
	otherRouter.POST("/auth/device/grants", CreateDeviceGrant)
	otherRouter.DELETE("/auth/device/grants/:id", RevokeDeviceGrant)
	otherCreated := postJSON(t, otherRouter, "/auth/device/grants", `{}`)
	var otherCreate models.DeviceGrantCreateResponse
	require.NoError(t, json.Unmarshal(otherCreated.Body.Bytes(), &otherCreate))

	list := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/auth/device/grants", nil)
	router.ServeHTTP(list, req)
	require.Equal(t, http.StatusOK, list.Code)
	var listResp struct {
		DeviceGrants []models.DeviceGrantResponse `json:"device_grants"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listResp))
	assert.Len(t, listResp.DeviceGrants, 1)
	assert.Equal(t, "mine", listResp.DeviceGrants[0].Label)

	// Carol cannot revoke Dave's grant (ownership is scoped by user_id).
	notMine := httptest.NewRecorder()
	delReq, _ := http.NewRequest(http.MethodDelete, "/auth/device/grants/"+strconv.FormatUint(uint64(otherCreate.ID), 10), nil)
	router.ServeHTTP(notMine, delReq)
	require.Equal(t, http.StatusNotFound, notMine.Code)
}

// Issue #722 revocation decision: a self-service password change revokes every
// device grant, so a stolen password can't keep a passwordless door open.
func TestDeviceGrant_PasswordChangeRevokesAllGrants(t *testing.T) {
	db, router := deviceGrantRouter(t, models.User{Username: "erin", Email: "erin@example.com", Password: "x"}, testConfig())

	// Enroll a grant while the session is valid.
	created := postJSON(t, router, "/auth/device/grants", `{}`)
	var createResp models.DeviceGrantCreateResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createResp))

	// Give the user a real bcrypt hash of the "old" password so the change
	// succeeds (deviceGrantRouter seeds a raw placeholder).
	var user models.User
	require.NoError(t, db.Where("username = ?", "erin").First(&user).Error)
	hashedOld, err := services.HashPassword("old-secret")
	require.NoError(t, err)
	require.NoError(t, db.Model(&user).Update("password", hashedOld).Error)
	router.POST("/users/change-password", func(c *gin.Context) {
		c.Set("username", "erin")
		c.Set("validated", &models.ChangePasswordInput{
			CurrentPassword: "old-secret",
			NewPassword:     "new-secret-123",
		})
		ChangePassword(c, &config.Config{})
	})
	req, _ := http.NewRequest(http.MethodPost, "/users/change-password", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var grant models.DeviceGrant
	require.NoError(t, db.First(&grant, createResp.ID).Error)
	require.NotNil(t, grant.RevokedAt, "password change must revoke device grants")

	exchanged := postJSON(t, router, "/auth/device/session", `{"device_token":"`+createResp.Token+`"}`)
	require.Equal(t, http.StatusUnauthorized, exchanged.Code)
}

// --- Error paths / edge branches ----------------------------------------

func TestDeviceGrant_InvalidJsonIsRejected(t *testing.T) {
	_, router := deviceGrantRouter(t, models.User{Username: "erinx", Email: "erinx@example.com", Password: "x"}, testConfig())

	bad := postJSON(t, router, "/auth/device/session", `{"device_token":`)
	require.Equal(t, http.StatusBadRequest, bad.Code)

	badCreate := postJSON(t, router, "/auth/device/grants", `not-json`)
	require.Equal(t, http.StatusBadRequest, badCreate.Code)
}

func TestDeviceGrant_RevokeOwnGrantAndInvalidId(t *testing.T) {
	_, router := deviceGrantRouter(t, models.User{Username: "frank", Email: "frank@example.com", Password: "x"}, testConfig())

	created := postJSON(t, router, "/auth/device/grants", `{}`)
	var createResp models.DeviceGrantCreateResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createResp))

	delReq, _ := http.NewRequest(http.MethodDelete, "/auth/device/grants/"+strconv.FormatUint(uint64(createResp.ID), 10), nil)
	delResp := httptest.NewRecorder()
	router.ServeHTTP(delResp, delReq)
	require.Equal(t, http.StatusOK, delResp.Code)

	bad := httptest.NewRecorder()
	badReq, _ := http.NewRequest(http.MethodDelete, "/auth/device/grants/abc", nil)
	router.ServeHTTP(bad, badReq)
	require.Equal(t, http.StatusBadRequest, bad.Code)
}

// A token cannot be minted when the server has no JWT secret configured — the
// same failure the password-login path reports as a 500.
func TestDeviceGrant_ExchangeFailsWhenTokenCannotBeMinted(t *testing.T) {
	cfg := config.Config{} // empty JWT secret -> GenerateToken errors
	_, router := deviceGrantRouter(t, models.User{Username: "grace", Email: "grace@example.com", Password: "x"}, cfg)

	created := postJSON(t, router, "/auth/device/grants", `{}`)
	var createResp models.DeviceGrantCreateResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createResp))

	exchanged := postJSON(t, router, "/auth/device/session", `{"device_token":"`+createResp.Token+`"}`)
	require.Equal(t, http.StatusInternalServerError, exchanged.Code)
}

// A device grant whose owner has been deleted must never mint a session. If
// the FK cascade removed the grant row too, the token is simply unknown —
// either order lands on 401.
func TestDeviceGrant_ExchangeForDeletedUserIsRejected(t *testing.T) {
	db, router := deviceGrantRouter(t, models.User{Username: "heidi", Email: "heidi@example.com", Password: "x"}, testConfig())
	var user models.User
	require.NoError(t, db.Where("username = ?", "heidi").First(&user).Error)

	_, plaintext, err := services.CreateDeviceGrant(db, user.ID, "orphan")
	require.NoError(t, err)
	require.NoError(t, db.Unscoped().Delete(&models.User{}, user.ID).Error)

	exchanged := postJSON(t, router, "/auth/device/session", `{"device_token":"`+plaintext+`"}`)
	require.Equal(t, http.StatusUnauthorized, exchanged.Code)
}

// Every device-grant handler is user-scoped: with no authenticated user in
// context, all of them must short-circuit instead of touching the DB.
func TestDeviceGrant_HandlersRequireAuthentication(t *testing.T) {
	db := dbtest.New(t)
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("db", db); c.Next() })
	router.GET("/auth/device/grants", ListDeviceGrants)
	router.POST("/auth/device/grants", CreateDeviceGrant)
	router.DELETE("/auth/device/grants/:id", RevokeDeviceGrant)
	router.POST("/auth/device/grants/revoke-all", RevokeAllDeviceGrants)

	req, _ := http.NewRequest(http.MethodGet, "/auth/device/grants", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeviceGrant_ClosedDatabaseErrorsSurface(t *testing.T) {
	db, router := deviceGrantRouter(t, models.User{Username: "ingrid", Email: "ingrid@example.com", Password: "x"}, testConfig())
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	listReq, _ := http.NewRequest(http.MethodGet, "/auth/device/grants", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	require.Equal(t, http.StatusInternalServerError, listW.Code)

	createW := postJSON(t, router, "/auth/device/grants", `{}`)
	require.Equal(t, http.StatusInternalServerError, createW.Code)

	revokeAll := postJSON(t, router, "/auth/device/grants/revoke-all", `{}`)
	require.Equal(t, http.StatusInternalServerError, revokeAll.Code)
}
