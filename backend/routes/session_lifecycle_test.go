package routes

// TestSessionLifecycle_* is the account/session-lifecycle E2E suite for
// issue #372: it proves every TokenVersion bump site (models/user.go:28) and
// the other revocation paths *actually kill* an otherwise-valid session on the
// real migrated schema (database.InitDB — CLAUDE.md backend trap #1: never
// AutoMigrate for persistence).
//
// Each scenario follows the issue's verification recipe: mint a token, perform
// the action through the live router, assert the old token now fails, and —
// where the account still exists — assert a freshly minted token still works
// (revocation must not brick the account).
//
// TokenVersion bump sites covered:
//
//   - ChangePassword          (user_controller.go:701) — password change
//   - ConfirmPasswordReset    (user_controller.go:428) — password reset
//   - ConfirmTwoFactor        (two_factor_controller.go:160) — 2FA enable
//   - DisableTwoFactor        (two_factor_controller.go:231) — 2FA disable
//   - UpdateUser (admin)      (admin_user_controller.go:408) — admin password reset
//
// The issue text also names an "email change" bump site; no such endpoint
// exists — the two user_controller.go sites above are both password paths.
// Non-TokenVersion revocation paths covered:
//
//   - RevokeApiToken          (api_token_controller.go) — revoked token 401s
//   - RevokeAllApiTokens      (api_token_controller.go) — issue #413, revokes every standing token at once
//   - RotateApiToken          (api_token_controller.go) — issue #413, old token dead, new one live immediately
//   - UpdateUser (admin)      (admin_user_controller.go) — issue #413, admin password reset also revokes API tokens
//   - soft-deleted user       — AuthMiddleware's user lookup misses the row
//   - RegenerateRecoveryCodes — old recovery codes stop validating
//
// Where the middleware-level mechanism is already pinned by
// middleware/auth_lifecycle_test.go, this suite drives the real controllers
// instead of duplicating that unit coverage.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	lifecyclePassword  = "CorrectHorseBattery42!"
	lifecyclePassword2 = "TrulySecurePassphrase99#"
	lifecyclePassword3 = "UltraSafePassphrase88$"
	lifecycleSecret    = "lifecycle-test-secret-key-that-is-long-enough"
)

// lifecycleEnv wires the real migrated schema + the full router (as main.go
// does, minus CORS/logging) and returns a fresh environment per test. Usernames
// are derived from the test name because the account rate limiter is
// package-global (same rationale as twoFactorTestEnv).
func lifecycleEnv(t *testing.T) (*gorm.DB, *gin.Engine, *config.Config) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:     lifecycleSecret,
		JWTExpiryHours:   24,
		ProfilePhotoDir:  t.TempDir(),
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	RegisterRoutes(router, cfg, db, nil)

	return db, router, cfg
}

// lifecycleUsername derives a rate-limiter-safe, validation-clean username from
// the test name (LoginUser lowercases; the no_at_sign rule forbids slashes).
func lifecycleUsername(t *testing.T) string {
	name := strings.ReplaceAll(strings.TrimPrefix(t.Name(), "TestSessionLifecycle_"), "_", "-")
	name = strings.ToLower(name)
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}

func seedUser(t *testing.T, db *gorm.DB, username, password string, isAdmin bool) models.User {
	t.Helper()
	hashed, err := services.HashPassword(password)
	require.NoError(t, err)
	u := models.User{
		Username: username,
		Email:    username + "@example.com",
		Password: hashed,
		IsAdmin:  isAdmin,
	}
	require.NoError(t, db.Create(&u).Error)
	return u
}

func mintToken(t *testing.T, db *gorm.DB, cfg *config.Config, u models.User) string {
	t.Helper()
	// issue #866: IssueSession also writes the server-side session row that
	// AuthMiddleware now requires, so tests exercise the real login path.
	tok, err := services.IssueSession(db, u, cfg, "", "")
	require.NoError(t, err)
	return tok
}

// reloadUser re-reads the row so a freshly minted token carries the current
// TokenVersion (the value a bump site may have advanced).
func reloadUser(t *testing.T, db *gorm.DB, id uint) models.User {
	t.Helper()
	var u models.User
	require.NoError(t, db.First(&u, id).Error)
	return u
}

// probe is a minimal protected route that 200s for any valid session (JWT or
// full-scope API token) and 401s otherwise. /users/2fa/status is chosen for its
// lack of side effects.
const probePath = "/api/v1/users/2fa/status"

func probe(t *testing.T, router http.Handler, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, probePath, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code
}

func doJSON(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req, err := http.NewRequest(method, path, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func totpCodeAt(t *testing.T, secret string, when time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, when)
	require.NoError(t, err)
	return code
}

func totpCode(t *testing.T, secret string) string {
	return totpCodeAt(t, secret, time.Now())
}

// enableTwoFactor drives setup + confirm for the caller and returns the stored
// plaintext secret (to mint further codes) and the recovery codes minted at
// enrollment. The caller's token is dead afterwards (TokenVersion bump).
func enableTwoFactor(t *testing.T, router http.Handler, token string) (secret string, recoveryCodes []string) {
	t.Helper()
	w := doJSON(t, router, http.MethodPost, "/api/v1/users/2fa/setup", token, nil)
	require.Equal(t, http.StatusOK, w.Code, "setup: %s", w.Body.String())
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	require.NotEmpty(t, setup.Secret)

	// Confirm with a code from the previous 30s step (still inside the server's
	// ±1 step window). Enrollment burns the confirming code's step for single
	// use (issue #873), so spending step S-1 here leaves the current step free
	// for the TOTP operation each test does next.
	w = doJSON(t, router, http.MethodPost, "/api/v1/users/2fa/confirm", token,
		map[string]string{"code": totpCodeAt(t, setup.Secret, time.Now().Add(-30*time.Second))})
	require.Equal(t, http.StatusOK, w.Code, "confirm: %s", w.Body.String())
	var confirm struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &confirm))
	require.Len(t, confirm.RecoveryCodes, 10)
	return setup.Secret, confirm.RecoveryCodes
}

func TestSessionLifecycle_ChangePasswordRevokesPriorSessions(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	oldToken := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, oldToken))

	w := doJSON(t, router, http.MethodPost, "/api/v1/users/change-password", oldToken, map[string]string{
		"current_password": lifecyclePassword,
		"new_password":     lifecyclePassword2,
	})
	require.Equal(t, http.StatusOK, w.Code, "change-password: %s", w.Body.String())

	// The pre-change token is dead; a freshly minted one still works.
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, oldToken))
	assert.Equal(t, http.StatusOK, probe(t, router, mintToken(t, db, cfg, reloadUser(t, db, user.ID))))

	// Issue #866: the change also revoked the server-side session row, not
	// only bumped token_version — the pre-change row carries a revoked_at.
	var revoked int64
	require.NoError(t, db.Model(&models.Session{}).
		Where("user_id = ? AND revoked_at IS NOT NULL", user.ID).Count(&revoked).Error)
	assert.GreaterOrEqual(t, revoked, int64(1), "the prior session row is marked revoked")
}

func TestSessionLifecycle_PasswordResetRevokesPriorSessions(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	oldToken := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, oldToken))

	// Seed a pending reset token the way RequestPasswordReset would, then drive
	// the confirm endpoint through the real route.
	token, tokenHash, err := services.GeneratePasswordResetToken()
	require.NoError(t, err)
	expires := services.PasswordResetExpiry()
	requested := time.Now()
	user.PasswordResetTokenHash = &tokenHash
	user.PasswordResetExpiresAt = &expires
	user.PasswordResetRequestedAt = &requested
	require.NoError(t, db.Save(&user).Error)

	w := doJSON(t, router, http.MethodPost, "/api/v1/password-reset/confirm", "", map[string]string{
		"token":    token,
		"password": lifecyclePassword2,
	})
	require.Equal(t, http.StatusOK, w.Code, "password-reset/confirm: %s", w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, probe(t, router, oldToken))
	assert.Equal(t, http.StatusOK, probe(t, router, mintToken(t, db, cfg, reloadUser(t, db, user.ID))))
}

func TestSessionLifecycle_TOTPEnableRevokesPriorSessions(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	oldToken := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, oldToken))

	enableTwoFactor(t, router, oldToken)

	assert.Equal(t, http.StatusUnauthorized, probe(t, router, oldToken))
	assert.Equal(t, http.StatusOK, probe(t, router, mintToken(t, db, cfg, reloadUser(t, db, user.ID))))
}

func TestSessionLifecycle_TOTPDisableRevokesPriorSessions(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	oldToken := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, oldToken))
	secret, _ := enableTwoFactor(t, router, oldToken)

	// Enabling bumped TokenVersion; mint a fresh session to drive the disable.
	enabledToken := mintToken(t, db, cfg, reloadUser(t, db, user.ID))
	require.Equal(t, http.StatusOK, probe(t, router, enabledToken))

	w := doJSON(t, router, http.MethodPost, "/api/v1/users/2fa/disable", enabledToken, map[string]string{"code": totpCode(t, secret)})
	require.Equal(t, http.StatusOK, w.Code, "disable: %s", w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, probe(t, router, enabledToken))
	assert.Equal(t, http.StatusOK, probe(t, router, mintToken(t, db, cfg, reloadUser(t, db, user.ID))))
}

// Issue #866 (pen-test #860 F-6): POST /logout must revoke *this device's*
// server-side session row so a token copied before logout stops working — but
// only that device's, not every session for the user.
func TestSessionLifecycle_LogoutRevokesThisDeviceOnly(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	deviceA := mintToken(t, db, cfg, user)
	deviceB := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, deviceA))
	require.Equal(t, http.StatusOK, probe(t, router, deviceB))

	// /logout reads the sid from the auth_token *cookie*, not a bearer header.
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: deviceA})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "logout: %s", w.Body.String())

	// The replayed pre-logout token is now rejected; the other device is not.
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, deviceA))
	assert.Equal(t, http.StatusOK, probe(t, router, deviceB))
}

func TestSessionLifecycle_AdminPasswordResetRevokesPriorSessions(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	admin := seedUser(t, db, lifecycleUsername(t)+"-admin", lifecyclePassword, true)
	target := seedUser(t, db, lifecycleUsername(t)+"-target", lifecyclePassword, false)

	adminToken := mintToken(t, db, cfg, admin)
	targetToken := mintToken(t, db, cfg, target)
	require.Equal(t, http.StatusOK, probe(t, router, targetToken))

	w := doJSON(t, router, http.MethodPatch, "/api/v1/admin/users/"+strconv.FormatUint(uint64(target.ID), 10), adminToken, map[string]string{
		"password": lifecyclePassword3,
	})
	require.Equal(t, http.StatusOK, w.Code, "admin password reset: %s", w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, probe(t, router, targetToken))
	assert.Equal(t, http.StatusOK, probe(t, router, mintToken(t, db, cfg, reloadUser(t, db, target.ID))))
}

// Issue #413: an admin password reset is the operator-side response to a
// suspected account takeover, so it must end standing API tokens the same
// way the self-service recovery-path reset already does (#411) --
// TokenVersion alone only kills JWT sessions, leaving a leaked API token
// fully live otherwise.
func TestSessionLifecycle_AdminPasswordResetRevokesAPITokens(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	admin := seedUser(t, db, lifecycleUsername(t)+"-admin", lifecyclePassword, true)
	target := seedUser(t, db, lifecycleUsername(t)+"-target", lifecyclePassword, false)

	adminToken := mintToken(t, db, cfg, admin)
	targetToken := mintToken(t, db, cfg, target)

	w := doJSON(t, router, http.MethodPost, "/api/v1/api-tokens", targetToken, map[string]string{"name": "target-script"})
	require.Equal(t, http.StatusCreated, w.Code, "create api-token: %s", w.Body.String())
	var created struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Token)

	// The API token is live before the admin resets the account's password.
	assert.Equal(t, http.StatusOK, probe(t, router, created.Token))

	w = doJSON(t, router, http.MethodPatch, "/api/v1/admin/users/"+strconv.FormatUint(uint64(target.ID), 10), adminToken, map[string]string{
		"password": lifecyclePassword3,
	})
	require.Equal(t, http.StatusOK, w.Code, "admin password reset: %s", w.Body.String())

	assert.Equal(t, http.StatusUnauthorized, probe(t, router, created.Token))
}

func TestSessionLifecycle_RevokedAPITokenIsRejectedImmediately(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	session := mintToken(t, db, cfg, user)

	w := doJSON(t, router, http.MethodPost, "/api/v1/api-tokens", session, map[string]string{"name": "revoke-me"})
	require.Equal(t, http.StatusCreated, w.Code, "create api-token: %s", w.Body.String())
	var created struct {
		ID    uint   `json:"id"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Token)

	// The API token is a live credential before revocation.
	assert.Equal(t, http.StatusOK, probe(t, router, created.Token))

	w = doJSON(t, router, http.MethodDelete, "/api/v1/api-tokens/"+strconv.FormatUint(uint64(created.ID), 10), session, nil)
	require.Equal(t, http.StatusOK, w.Code, "revoke api-token: %s", w.Body.String())

	// Revoked token is rejected immediately; the session is unaffected.
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, created.Token))
	assert.Equal(t, http.StatusOK, probe(t, router, session))
}

// Issue #413: self-service revoke-all -- a user who suspects a token leaked
// can end every standing token at once without knowing which one leaked.
func TestSessionLifecycle_RevokeAllAPITokensRejectsImmediately(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	session := mintToken(t, db, cfg, user)

	w := doJSON(t, router, http.MethodPost, "/api/v1/api-tokens", session, map[string]string{"name": "one"})
	require.Equal(t, http.StatusCreated, w.Code, "create api-token 1: %s", w.Body.String())
	var first struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))

	w = doJSON(t, router, http.MethodPost, "/api/v1/api-tokens", session, map[string]string{"name": "two"})
	require.Equal(t, http.StatusCreated, w.Code, "create api-token 2: %s", w.Body.String())
	var second struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &second))

	assert.Equal(t, http.StatusOK, probe(t, router, first.Token))
	assert.Equal(t, http.StatusOK, probe(t, router, second.Token))

	w = doJSON(t, router, http.MethodPost, "/api/v1/api-tokens/revoke-all", session, nil)
	require.Equal(t, http.StatusOK, w.Code, "revoke-all: %s", w.Body.String())

	// Both tokens rejected immediately; the JWT session is unaffected
	// (revoke-all only touches API tokens, not TokenVersion).
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, first.Token))
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, second.Token))
	assert.Equal(t, http.StatusOK, probe(t, router, session))
}

// Issue #413: rotating a token kills the old credential and hands back a new
// one that works immediately, in one round trip.
func TestSessionLifecycle_RotatedAPITokenReplacesOldOne(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	session := mintToken(t, db, cfg, user)

	w := doJSON(t, router, http.MethodPost, "/api/v1/api-tokens", session, map[string]string{"name": "rotate-me"})
	require.Equal(t, http.StatusCreated, w.Code, "create api-token: %s", w.Body.String())
	var created struct {
		ID    uint   `json:"id"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	assert.Equal(t, http.StatusOK, probe(t, router, created.Token))

	w = doJSON(t, router, http.MethodPost, "/api/v1/api-tokens/"+strconv.FormatUint(uint64(created.ID), 10)+"/rotate", session, nil)
	require.Equal(t, http.StatusCreated, w.Code, "rotate: %s", w.Body.String())
	var rotated struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rotated))
	require.NotEmpty(t, rotated.Token)
	require.NotEqual(t, created.Token, rotated.Token)

	// Old token dead, new token live immediately, session unaffected.
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, created.Token))
	assert.Equal(t, http.StatusOK, probe(t, router, rotated.Token))
	assert.Equal(t, http.StatusOK, probe(t, router, session))
}

func TestSessionLifecycle_SoftDeletedUserCannotAuthenticate(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	oldToken := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, oldToken))

	// Soft-delete the account (the "disabled" persona). No API endpoint does
	// this — admin DeleteUser hard-deletes by design — so drive GORM directly.
	require.NoError(t, db.Delete(&user).Error)

	// Neither a pre-delete token nor a freshly minted one may authenticate:
	// AuthMiddleware's user lookup misses the soft-deleted row.
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, oldToken))
	assert.Equal(t, http.StatusUnauthorized, probe(t, router, mintToken(t, db, cfg, user)))
}

func TestSessionLifecycle_RecoveryCodeRegenerationInvalidatesPriorCodes(t *testing.T) {
	db, router, cfg := lifecycleEnv(t)
	user := seedUser(t, db, lifecycleUsername(t), lifecyclePassword, false)

	oldToken := mintToken(t, db, cfg, user)
	require.Equal(t, http.StatusOK, probe(t, router, oldToken))
	secret, oldCodes := enableTwoFactor(t, router, oldToken)
	require.Len(t, oldCodes, 10)

	// Enabling bumped TokenVersion; mint a fresh session to drive regeneration.
	enabledToken := mintToken(t, db, cfg, reloadUser(t, db, user.ID))
	w := doJSON(t, router, http.MethodPost, "/api/v1/users/2fa/recovery-codes/regenerate", enabledToken, map[string]string{"code": totpCode(t, secret)})
	require.Equal(t, http.StatusOK, w.Code, "regenerate: %s", w.Body.String())
	var resp struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.RecoveryCodes, 10)

	// A pre-regeneration code no longer validates; a fresh one does (and is
	// consumed by the validation itself).
	assert.False(t, services.ConsumeRecoveryCode(db, user.ID, oldCodes[0]),
		"a code from the superseded set must be rejected")
	assert.True(t, services.ConsumeRecoveryCode(db, user.ID, resp.RecoveryCodes[0]),
		"a code from the regenerated set must validate")
}
