package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/models"
	"mycorrhizal/services"

	apperrors "mycorrhizal/errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ExchangeDeviceGrant (POST /auth/device/session, public + rate-limited) is
// the possession exchange behind "fully biometric login" (issue #722): the
// device proves it holds an unrevoked device grant and receives a fresh
// session JWT — the same cookie shape as a password login. The caller is
// expected to have just passed its local biometric gate; the server cannot
// observe the biometric, only the grant, so this endpoint is rate-limited like
// /login and a revoked/unknown grant is rejected exactly like bad credentials.
func ExchangeDeviceGrant(c *gin.Context, cfg *config.Config) {
	var input models.DeviceGrantSessionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("", err.Error()))
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var grant models.DeviceGrant
	err := db.Where("token_hash = ? AND revoked_at IS NULL", services.HashDeviceGrantToken(input.DeviceToken)).First(&grant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrInvalidCredentials())
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("query").WithError(err))
		}
		return
	}

	// The grant's user must still exist and be active — a deleted account's
	// grants mint nothing. (A user row can only vanish between this lookup and
	// the grant's own cascade deleting it first — covered by the deleted-user
	// test whichever order wins, so the branch itself is belt-and-braces.)
	var user models.User
	if err := db.First(&user, grant.UserID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidCredentials()) // # pragma: no cover — race between user deletion and its grant cascade
		return
	}

	if err := db.Model(&grant).Update("last_used_at", time.Now()).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update").WithError(err)) // # pragma: no cover — a single-row UPDATE failing after two successful reads needs a failing store
		return
	}

	tokenString, err := services.IssueSession(db, user, cfg, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInternal("Could not generate token").WithError(err))
		return
	}

	// T18 audit: a session minted via a device grant counts as a login (issue
	// #381) — same event the password path records, so the trail reads the
	// same way; the grant id is lost here by design (last_used_at carries the
	// activity).
	models.RecordAuditEvent(models.AuditEntityAuth, user.Username, models.AuditOpLogin, user.ID)

	// Issue #392: Strict, matching the cookie as set at login. Secure is set
	// unconditionally here (not just when cfg.CookieSecure is on): this
	// endpoint is the mobile bearer-capture path, which always talks TLS in
	// production and has no cookie jar that would reject a Secure cookie over
	// a cleartext dev backend (the value is captured from the Set-Cookie
	// header). The session cookie never travels without the Secure attribute.
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		"auth_token",
		tokenString,
		cfg.JWTExpiryHours*3600,
		"/",
		cfg.CookieDomain,
		true,
		true,
	)

	c.JSON(http.StatusOK, gin.H{
		"language":    user.Language,
		"date_format": user.DateFormat,
	})
}

// ListDeviceGrants lists the caller's enrolled devices. Never returns the
// token — only enough to recognise and revoke a device.
func ListDeviceGrants(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var grants []models.DeviceGrant
	if err := db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&grants).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query"))
		return
	}

	response := make([]models.DeviceGrantResponse, len(grants))
	for i, g := range grants {
		response[i] = deviceGrantToResponse(g)
	}
	c.JSON(http.StatusOK, gin.H{"device_grants": response})
}

// CreateDeviceGrant (protected) mints a grant for the *authenticated* caller
// — the enrollment moment in the client flow is always right after an
// interactive login, never a standalone anonymous call. The plaintext token
// is returned exactly once.
func CreateDeviceGrant(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var input models.DeviceGrantInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("", err.Error()))
		return
	}

	grant, plaintext, err := services.CreateDeviceGrant(db, userID, input.Label)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("create").WithError(err))
		return
	}

	c.JSON(http.StatusCreated, models.DeviceGrantCreateResponse{
		DeviceGrantResponse: deviceGrantToResponse(*grant),
		Token:               plaintext,
	})
}

// RevokeDeviceGrant revokes one of the caller's grants by id. Ownership is
// scoped by user_id, so a caller can only ever revoke its own device.
func RevokeDeviceGrant(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("id", "must be a positive integer"))
		return
	}

	var grant models.DeviceGrant
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&grant).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Device grant"))
		return
	}

	if err := db.Model(&grant).Update("revoked_at", time.Now()).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update")) // # pragma: no cover — a single-row UPDATE failing after a successful read needs a failing store
		return
	}

	// T18 audit: device-grant revocation, under the auth lifecycle entity.
	models.RecordAuditEvent(models.AuditEntityAuth, fmt.Sprintf("device_grant:%d", grant.ID), models.AuditOpRevoke, userID)

	c.JSON(http.StatusOK, gin.H{"message": "Device grant revoked successfully"})
}

// RevokeAllDeviceGrants ends every standing grant for the caller at once —
// the "lost phone" path, mirroring RevokeAllApiTokens.
func RevokeAllDeviceGrants(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var ids []uint
	if err := db.Model(&models.DeviceGrant{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Pluck("id", &ids).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query"))
		return
	}

	revoked, err := services.RevokeAllDeviceGrants(db, userID)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update")) // # pragma: no cover — bulk UPDATE failing after Pluck succeeded needs a failing store
		return
	}

	for _, id := range ids {
		models.RecordAuditEvent(models.AuditEntityAuth, fmt.Sprintf("device_grant:%d", id), models.AuditOpRevoke, userID)
	}

	c.JSON(http.StatusOK, gin.H{"revoked": revoked})
}

func deviceGrantToResponse(g models.DeviceGrant) models.DeviceGrantResponse {
	return models.DeviceGrantResponse{
		ID:         g.ID,
		Label:      g.Label,
		CreatedAt:  g.CreatedAt,
		LastUsedAt: g.LastUsedAt,
		RevokedAt:  g.RevokedAt,
	}
}
