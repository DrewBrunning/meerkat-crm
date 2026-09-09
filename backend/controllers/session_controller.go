package controllers

import (
	"net/http"
	"time"

	apperrors "mycorrhizal/errors"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// currentSessionID returns the sid of the session making this request, set by
// AuthMiddleware. Empty only for an API-token caller (those carry no session
// row) — such a caller has no "current" device in the list and cannot be the
// one being revoked.
func currentSessionID(c *gin.Context) string {
	if v, ok := c.Get("sessionID"); ok {
		if sid, ok := v.(string); ok {
			return sid
		}
	}
	return ""
}

// ListSessions returns the caller's still-active sessions (not revoked, not
// past their absolute expiry), newest first. Issue #866 / ASVS 3.3.4.
func ListSessions(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var sessions []models.Session
	if err := db.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, time.Now()).
		Order("last_seen_at DESC").
		Find(&sessions).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query"))
		return
	}

	current := currentSessionID(c)
	response := make([]models.SessionResponse, len(sessions))
	for i, s := range sessions {
		response[i] = models.SessionResponse{
			ID:         s.ID,
			CreatedAt:  s.CreatedAt,
			LastSeenAt: s.LastSeenAt,
			ExpiresAt:  s.ExpiresAt,
			UserAgent:  s.UserAgent,
			IP:         s.IP,
			Current:    s.ID == current,
		}
	}

	c.JSON(http.StatusOK, gin.H{"sessions": response})
}

// RevokeSession ends one of the caller's sessions by id. Revoking the current
// session is allowed and is equivalent to logging out this device (the client
// clears local state and the next request 401s). Issue #866.
func RevokeSession(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	id := c.Param("id")

	// Scope by user_id (backend trap #5 — no IDOR). A row that exists but
	// belongs to someone else is a 404, same as one that doesn't exist.
	var session models.Session
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&session).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("session"))
		return
	}

	if err := services.RevokeSession(db, session.ID); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Session revoked"})
}

// RevokeOtherSessions ends every one of the caller's sessions except the one
// making the request — "log out everywhere else". Issue #866 / ASVS 3.3.4.
func RevokeOtherSessions(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	current := currentSessionID(c)
	result := db.Model(&models.Session{}).
		Where("user_id = ? AND revoked_at IS NULL AND id <> ?", userID, current).
		Update("revoked_at", time.Now())
	if result.Error != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Other sessions revoked", "revoked": result.RowsAffected})
}
