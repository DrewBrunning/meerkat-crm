package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// newAuthAuditRouter builds a real-schema router (issue #381's requirement:
// the widened audit operation CHECK and the hash chain only exist in the real
// migrated schema) with the audit recorder registered, and the auth/admin
// endpoints wired to a fixed acting user. Context-based auth (userID/username
// in context) stands in for AuthMiddleware, exactly like setupAuditRouter.
func newAuthAuditRouter(t *testing.T) (*gorm.DB, *gin.Engine, models.User) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	cfg := config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24}

	db := dbtest.New(t)
	models.RegisterAuditDB(db)
	t.Cleanup(func() {
		models.AuditFlush()
		models.RegisterAuditDB(nil)
	})

	actor := models.User{Username: "auditactor", Password: "password123!A", Email: "auditactor@example.com", IsAdmin: true}
	require.NoError(t, db.Create(&actor).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", cfg)
		c.Set("userID", actor.ID)
		c.Set("username", actor.Username)
		c.Next()
	})

	// Public routes.
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(&cfg))
	router.POST("/login", func(c *gin.Context) { LoginUser(c, &cfg) })

	router.POST("/password-reset/request", middleware.ValidateJSONMiddleware(&models.PasswordResetRequestInput{}), func(c *gin.Context) { RequestPasswordReset(c, &cfg) })
	router.POST("/password-reset/confirm", middleware.ValidateJSONMiddleware(&models.PasswordResetConfirmInput{}), func(c *gin.Context) { ConfirmPasswordReset(c, &cfg) })

	// Protected routes (context-authenticated as `actor`).
	router.POST("/users/change-password", middleware.ValidateJSONMiddleware(&models.ChangePasswordInput{}), func(c *gin.Context) { ChangePassword(c, &cfg) })
	router.POST("/api-tokens", middleware.ValidateJSONMiddleware(&models.ApiTokenInput{}), CreateApiToken)
	router.POST("/api-tokens/revoke-all", RevokeAllApiTokens)
	router.DELETE("/api-tokens/:id", RevokeApiToken)
	router.POST("/api-tokens/:id/rotate", RotateApiToken)
	router.POST("/users/2fa/setup", SetupTwoFactor)
	router.POST("/users/2fa/confirm", ConfirmTwoFactor)
	router.POST("/users/2fa/disable", DisableTwoFactor)
	router.POST("/users/2fa/recovery-codes/regenerate", RegenerateRecoveryCodes)

	// Admin routes (context-authenticated as `actor`, who is admin).
	router.POST("/admin/users", middleware.ValidateJSONMiddleware(&models.AdminUserCreateInput{}), CreateUser)
	router.PATCH("/admin/users/:id", middleware.ValidateJSONMiddleware(&models.AdminUserUpdateInput{}), UpdateUser)
	router.DELETE("/admin/users/:id", DeleteUser)

	return db, router, actor
}

// auditDoJSON posts a JSON body and returns the recorder.
func auditDoJSON(router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// countAudit returns how many events match the triple.
func countAudit(t *testing.T, db *gorm.DB, entityType, entityID, operation string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.AuditEvent{}).
		Where("entity_type = ? AND entity_id = ? AND operation = ?", entityType, entityID, operation).
		Count(&n).Error)
	return n
}

func TestAuthAuditEvents_LoginSuccessAndFailure(t *testing.T) {
	db, router, actor := newAuthAuditRouter(t)

	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	user := models.User{Username: "loginuser", Email: "loginuser@example.com", Password: hashed}
	require.NoError(t, db.Create(&user).Error)

	// Success.
	w := auditDoJSON(router, "POST", "/login", map[string]string{"identifier": "loginuser", "password": strongPassword})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// Failure for a known account (wrong password).
	w = auditDoJSON(router, "POST", "/login", map[string]string{"identifier": "loginuser", "password": "definitelyWrongPassword1"})
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAuth, "loginuser", models.AuditOpLogin))
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAuth, "loginuser", models.AuditOpLoginFailed))

	// The events chain up (the auth events must be part of a valid chain).
	models.RecomputeAuditChain(db)
	gaps, err := models.VerifyAuditChain(db)
	require.NoError(t, err)
	assert.Empty(t, gaps, "auth events must form a valid hash chain")
	assert.NotEqual(t, actor.ID, user.ID, "sanity: login user differs from the router actor")
}

func TestAuthAuditEvents_Registration(t *testing.T) {
	db, router, _ := newAuthAuditRouter(t)

	w := auditDoJSON(router, "POST", "/register", models.UserRegistrationInput{
		Username: "newcomer", Email: "newcomer@example.com", Password: strongPassword,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	models.AuditFlush()

	var registered models.User
	require.NoError(t, db.Where("username = ?", "newcomer").First(&registered).Error)
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, fmt.Sprintf("%d", registered.ID), models.AuditOpRegister))
}

func TestAuthAuditEvents_PasswordChange(t *testing.T) {
	db, router, actor := newAuthAuditRouter(t)

	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	actor.Password = hashed
	require.NoError(t, db.Save(&actor).Error)

	w := auditDoJSON(router, "POST", "/users/change-password", models.ChangePasswordInput{
		CurrentPassword: strongPassword,
		NewPassword:     strongPasswordAlt,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, fmt.Sprintf("%d", actor.ID), models.AuditOpPasswordChange))
}

// TestAuthAuditEvents_PasswordReset pins issue #411's audit requirement: both
// the reset *request* and the successful *confirm* are audited, distinctly,
// against the real migrated schema (the widened operation CHECK from
// migration 000035 only exists there, not under AutoMigrate).
func TestAuthAuditEvents_PasswordReset(t *testing.T) {
	db, router, _ := newAuthAuditRouter(t)

	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	user := models.User{Username: "resetsubject", Email: "resetsubject@example.com", Password: hashed}
	require.NoError(t, db.Create(&user).Error)

	w := auditDoJSON(router, "POST", "/password-reset/request", models.PasswordResetRequestInput{Email: user.Email})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpPasswordResetRequested))

	var withToken models.User
	require.NoError(t, db.First(&withToken, user.ID).Error)
	require.NotNil(t, withToken.PasswordResetTokenHash)

	// The controller only ever sees the hashed token; recover the raw one the
	// same way the confirm test helpers elsewhere in this package do -- by
	// generating it ourselves and overwriting the stored hash, since the
	// service never returns the raw token back out through a real HTTP call.
	rawToken, tokenHash, err := services.GeneratePasswordResetToken()
	require.NoError(t, err)
	require.NoError(t, db.Model(&withToken).Update("password_reset_token_hash", tokenHash).Error)

	w = auditDoJSON(router, "POST", "/password-reset/confirm", models.PasswordResetConfirmInput{
		Token:    rawToken,
		Password: strongPasswordAlt,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpPasswordReset))

	// The events chain up like every other audited operation.
	models.RecomputeAuditChain(db)
	gaps, err := models.VerifyAuditChain(db)
	require.NoError(t, err)
	assert.Empty(t, gaps, "password-reset events must form a valid hash chain")
}

// TestAuthAuditEvents_PasswordResetRequest_UnknownEmail_NotAudited pins the
// other half of issue #411's enumeration-resistance requirement: the audit
// trail itself must not become a side channel for testing which emails have
// accounts, so a request for an unknown email must record nothing.
func TestAuthAuditEvents_PasswordResetRequest_UnknownEmail_NotAudited(t *testing.T) {
	db, router, _ := newAuthAuditRouter(t)

	w := auditDoJSON(router, "POST", "/password-reset/request", models.PasswordResetRequestInput{Email: "nobody@example.com"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	var n int64
	require.NoError(t, db.Model(&models.AuditEvent{}).
		Where("operation = ?", models.AuditOpPasswordResetRequested).
		Count(&n).Error)
	assert.EqualValues(t, 0, n, "an unknown email must not produce any password_reset_requested audit row")
}

func TestAuthAuditEvents_APITokenCreateAndRevoke(t *testing.T) {
	db, router, actor := newAuthAuditRouter(t)

	w := auditDoJSON(router, "POST", "/api-tokens", models.ApiTokenInput{Name: "script"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotZero(t, created.ID)

	w = auditDoJSON(router, "DELETE", "/api-tokens/"+strconv.Itoa(int(created.ID)), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAPIToken, fmt.Sprintf("%d", created.ID), models.AuditOpCreate))
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAPIToken, fmt.Sprintf("%d", created.ID), models.AuditOpRevoke))
	assert.NotEqual(t, 0, actor.ID)
}

// Issue #413: revoke-all fires one revoke event per token actually revoked,
// named individually, not one opaque bulk event.
func TestAuthAuditEvents_APITokenRevokeAll(t *testing.T) {
	db, router, _ := newAuthAuditRouter(t)

	var created1, created2 struct {
		ID uint `json:"id"`
	}
	w := auditDoJSON(router, "POST", "/api-tokens", models.ApiTokenInput{Name: "one"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created1))

	w = auditDoJSON(router, "POST", "/api-tokens", models.ApiTokenInput{Name: "two"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created2))

	w = auditDoJSON(router, "POST", "/api-tokens/revoke-all", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAPIToken, fmt.Sprintf("%d", created1.ID), models.AuditOpRevoke))
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAPIToken, fmt.Sprintf("%d", created2.ID), models.AuditOpRevoke))
}

// Issue #413: rotate fires a revoke event for the old token and a create
// event for the new one -- the same two events CreateApiToken/RevokeApiToken
// each fire individually.
func TestAuthAuditEvents_APITokenRotate(t *testing.T) {
	db, router, _ := newAuthAuditRouter(t)

	w := auditDoJSON(router, "POST", "/api-tokens", models.ApiTokenInput{Name: "rotate-me"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	w = auditDoJSON(router, "POST", "/api-tokens/"+strconv.Itoa(int(created.ID))+"/rotate", nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var rotated struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rotated))
	require.NotEqual(t, created.ID, rotated.ID)
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAPIToken, fmt.Sprintf("%d", created.ID), models.AuditOpRevoke))
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityAPIToken, fmt.Sprintf("%d", rotated.ID), models.AuditOpCreate))
}

func TestAuthAuditEvents_TwoFactorLifecycle(t *testing.T) {
	db, router, actor := newAuthAuditRouter(t)

	// Setup mints a secret; confirm enables 2FA + mints recovery codes.
	w := auditDoJSON(router, "POST", "/users/2fa/setup", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var setup struct {
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
	require.NotEmpty(t, setup.Secret)

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w = auditDoJSON(router, "POST", "/users/2fa/confirm", map[string]string{"code": code})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Regenerate recovery codes (needs a live TOTP code).
	code, err = totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w = auditDoJSON(router, "POST", "/users/2fa/recovery-codes/regenerate", map[string]string{"code": code})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Disable (needs a live TOTP code).
	code, err = totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	w = auditDoJSON(router, "POST", "/users/2fa/disable", map[string]string{"code": code})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	id := fmt.Sprintf("%d", actor.ID)
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpTOTPEnable))
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpRecoveryRegen))
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpTOTPDisable))
}

func TestAuthAuditEvents_AdminUserOperations(t *testing.T) {
	db, router, actor := newAuthAuditRouter(t)

	// Create.
	w := auditDoJSON(router, "POST", "/admin/users", models.AdminUserCreateInput{
		Username: "managed", Email: "managed@example.com", Password: strongPassword, IsAdmin: false,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created models.AdminUserResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Role change.
	w = auditDoJSON(router, "PATCH", "/admin/users/"+strconv.Itoa(int(created.ID)), models.AdminUserUpdateInput{
		IsAdmin: boolPtr(true),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Plain edit (username).
	w = auditDoJSON(router, "PATCH", "/admin/users/"+strconv.Itoa(int(created.ID)), models.AdminUserUpdateInput{
		Username: strPtr("managed2"),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Post-#871 an admin cannot delete a peer admin, so the account steps
	// itself down first. Done here as a direct write (this harness is fixed
	// to `actor`'s session); in the real API it is a self-service PATCH.
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", created.ID).Update("is_admin", false).Error)

	// Delete.
	w = auditDoJSON(router, "DELETE", "/admin/users/"+strconv.Itoa(int(created.ID)), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	id := fmt.Sprintf("%d", created.ID)
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpCreate),
		"admin create must be audited")
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpRoleChange),
		"role change must be audited")
	assert.EqualValues(t, 2, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpUpdate),
		"both admin edits must be audited")
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpDelete),
		"admin delete must be audited")

	// Every admin event's actor is the acting admin.
	var events []models.AuditEvent
	require.NoError(t, db.Where("entity_type = ? AND entity_id = ?", models.AuditEntityUser, id).Find(&events).Error)
	for _, e := range events {
		assert.EqualValues(t, actor.ID, e.UserID, "admin action must be attributed to the acting admin")
	}

	// The delete event survives the target's hard-delete cascade (UserID =
	// actor, not target).
	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, id, models.AuditOpDelete))
}

// Issue #871: an admin cannot demote or delete a PEER admin (self-service
// role changes only). The blocked attempts must produce NO audit event
// (nothing changed), while an admin's own self-demotion still emits the
// AuditOpRoleChange it always has.
func TestAuthAuditEvents_PeerAdminGuards(t *testing.T) {
	db, router, _ := newAuthAuditRouter(t)

	// A peer admin plus a third admin, so no action below is a last-admin case.
	peer := models.User{Username: "peeradmin", Password: strongPassword, Email: "peeradmin@example.com", IsAdmin: true}
	require.NoError(t, db.Create(&peer).Error)
	require.NoError(t, db.Create(&models.User{Username: "thirdadmin", Password: strongPassword, Email: "thirdadmin@example.com", IsAdmin: true}).Error)
	peerID := fmt.Sprintf("%d", peer.ID)

	// Blocked peer-demote -> 403, no role-change audit.
	w := auditDoJSON(router, "PATCH", "/admin/users/"+strconv.Itoa(int(peer.ID)), models.AdminUserUpdateInput{IsAdmin: boolPtr(false)})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// Blocked peer-delete -> 403, no delete audit.
	w = auditDoJSON(router, "DELETE", "/admin/users/"+strconv.Itoa(int(peer.ID)), nil)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 0, countAudit(t, db, models.AuditEntityUser, peerID, models.AuditOpRoleChange),
		"a blocked peer-demote must not emit a role-change audit event")
	assert.EqualValues(t, 0, countAudit(t, db, models.AuditEntityUser, peerID, models.AuditOpDelete),
		"a blocked peer-delete must not emit a delete audit event")

	// Self-demotion (peer acting on their own account) is allowed and still
	// audited. A fresh router authenticated as `peer`.
	selfRouter := gin.New()
	selfRouter.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", config.Config{JWTSecretKey: testJWTSecret, JWTExpiryHours: 24})
		c.Set("userID", peer.ID)
		c.Set("username", peer.Username)
		c.Next()
	})
	selfRouter.PATCH("/admin/users/:id", middleware.ValidateJSONMiddleware(&models.AdminUserUpdateInput{}), UpdateUser)

	w = auditDoJSON(selfRouter, "PATCH", "/admin/users/"+strconv.Itoa(int(peer.ID)), models.AdminUserUpdateInput{IsAdmin: boolPtr(false)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	models.AuditFlush()

	assert.EqualValues(t, 1, countAudit(t, db, models.AuditEntityUser, peerID, models.AuditOpRoleChange),
		"an admin's own self-demotion must emit exactly one role-change audit event")

	// That role-change event is attributed to the acting (self) admin.
	var events []models.AuditEvent
	require.NoError(t, db.Where("entity_type = ? AND entity_id = ? AND operation = ?",
		models.AuditEntityUser, peerID, models.AuditOpRoleChange).Find(&events).Error)
	require.Len(t, events, 1)
	assert.EqualValues(t, peer.ID, events[0].UserID)
}

func strPtr(s string) *string { return &s }
