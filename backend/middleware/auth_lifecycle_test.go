package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mycorrhizal/models"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Matches the key setupAuthTestRouter configures.
const testJWTSecret = "test-secret-key-32-chars-minimum!"

// signJWT builds a token the way services.GenerateToken does, constructed here
// rather than imported so the middleware is exercised in isolation.
func signJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	return signed
}

func jwtRequest(router http.Handler, token string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// seedSession inserts an active server-side session row (issue #866) and
// returns its id for use as the token's `sid` claim. lastSeen lets a test
// place the session far enough in the past to trip the idle timeout.
func seedSession(t *testing.T, db *gorm.DB, userID uint, lastSeen time.Time) string {
	t.Helper()
	id := fmt.Sprintf("sess-%d-%d", userID, time.Now().UnixNano())
	require.NoError(t, db.Create(&models.Session{
		ID:         id,
		UserID:     userID,
		CreatedAt:  time.Now(),
		LastSeenAt: lastSeen,
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}).Error)
	return id
}

func TestAuthMiddleware_JWTWithMatchingTokenVersion(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)
	sid := seedSession(t, db, user.ID, time.Now())

	w := jwtRequest(router, signJWT(t, jwt.MapClaims{
		"user_id":       user.ID,
		"username":      user.Username,
		"token_version": user.TokenVersion,
		"sid":           sid,
		"exp":           time.Now().Add(time.Hour).Unix(),
	}))

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(user.ID), body["user_id"])
}

// The point of the whole mechanism: bumping token_version (what a password
// change or reset does) must invalidate an already-issued token.
func TestAuthMiddleware_JWTRejectedAfterTokenVersionBump(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)
	sid := seedSession(t, db, user.ID, time.Now())

	token := signJWT(t, jwt.MapClaims{
		"user_id":       user.ID,
		"username":      user.Username,
		"token_version": user.TokenVersion,
		"sid":           sid,
		"exp":           time.Now().Add(time.Hour).Unix(),
	})

	// Still valid before the bump.
	assert.Equal(t, http.StatusOK, jwtRequest(router, token).Code)

	require.NoError(t, db.Model(&models.User{}).Where("id = ?", user.ID).
		Update("token_version", user.TokenVersion+1).Error)

	w := jwtRequest(router, token)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Session expired, please sign in again", body["error"])
}

// Tokens minted before token versioning existed carry no such claim and must be
// rejected rather than treated as version 0.
func TestAuthMiddleware_JWTWithoutTokenVersionClaimRejected(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)

	w := jwtRequest(router, signJWT(t, jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"exp":      time.Now().Add(time.Hour).Unix(),
	}))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_JWTForMissingUserRejected(t *testing.T) {
	_, router := setupAuthTestRouter()

	w := jwtRequest(router, signJWT(t, jwt.MapClaims{
		"user_id":       uint(99999),
		"username":      "ghost",
		"token_version": uint(0),
		"exp":           time.Now().Add(time.Hour).Unix(),
	}))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Issue #866: a token minted before migration 000053 carries no `sid` and is
// rejected, forcing one re-login — the same treatment as a missing
// token_version.
func TestAuthMiddleware_JWTWithoutSidClaimRejected(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)

	w := jwtRequest(router, signJWT(t, jwt.MapClaims{
		"user_id":       user.ID,
		"username":      user.Username,
		"token_version": user.TokenVersion,
		"exp":           time.Now().Add(time.Hour).Unix(),
	}))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Issue #866: the logout path revokes the session row; the very next request
// with the same (otherwise valid, unexpired) token must fail.
func TestAuthMiddleware_JWTRejectedAfterSessionRevoked(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)
	sid := seedSession(t, db, user.ID, time.Now())

	token := signJWT(t, jwt.MapClaims{
		"user_id":       user.ID,
		"username":      user.Username,
		"token_version": user.TokenVersion,
		"sid":           sid,
		"exp":           time.Now().Add(time.Hour).Unix(),
	})

	assert.Equal(t, http.StatusOK, jwtRequest(router, token).Code)

	require.NoError(t, db.Model(&models.Session{}).Where("id = ?", sid).
		Update("revoked_at", time.Now()).Error)

	w := jwtRequest(router, token)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Session expired, please sign in again", body["error"])
}

// Issue #866: a session unused for longer than SESSION_IDLE_TIMEOUT_HOURS is
// rejected before its absolute expiry; a fresh one is not.
func TestAuthMiddleware_JWTRejectedAfterIdleTimeout(t *testing.T) {
	db, router := setupAuthTestRouterWithIdle(1) // 1-hour idle window

	var user models.User
	db.First(&user)

	claims := func(sid string) jwt.MapClaims {
		return jwt.MapClaims{
			"user_id":       user.ID,
			"username":      user.Username,
			"token_version": user.TokenVersion,
			"sid":           sid,
			"exp":           time.Now().Add(24 * time.Hour).Unix(),
		}
	}

	stale := seedSession(t, db, user.ID, time.Now().Add(-2*time.Hour))
	assert.Equal(t, http.StatusUnauthorized, jwtRequest(router, signJWT(t, claims(stale))).Code)

	fresh := seedSession(t, db, user.ID, time.Now().Add(-30*time.Minute))
	assert.Equal(t, http.StatusOK, jwtRequest(router, signJWT(t, claims(fresh))).Code)
}

func TestAuthMiddleware_ExpiredApiTokenRejected(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)

	plaintext := "mycorrhizal_expiredtoken12345"
	expired := time.Now().Add(-time.Hour)
	require.NoError(t, db.Create(&models.ApiToken{
		UserID:    user.ID,
		Name:      "expired",
		TokenHash: hashToken(plaintext),
		ExpiresAt: &expired,
	}).Error)

	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_UnexpiredApiTokenAccepted(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)

	plaintext := "mycorrhizal_unexpiredtoken123"
	future := time.Now().Add(24 * time.Hour)
	require.NoError(t, db.Create(&models.ApiToken{
		UserID:    user.ID,
		Name:      "current",
		TokenHash: hashToken(plaintext),
		ExpiresAt: &future,
	}).Error)

	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// Rows predating the expires_at column have NULL there and must keep working.
func TestAuthMiddleware_ApiTokenWithNullExpiryAccepted(t *testing.T) {
	db, router := setupAuthTestRouter()

	var user models.User
	db.First(&user)

	plaintext := "mycorrhizal_legacynoexpiry123"
	require.NoError(t, db.Create(&models.ApiToken{
		UserID:    user.ID,
		Name:      "legacy",
		TokenHash: hashToken(plaintext),
	}).Error)

	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
