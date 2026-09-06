package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"mycorrhizal/attachments"
	"mycorrhizal/config"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recordManualJobRun persists one job_runs row (trigger "manual") for an
// operator-forced run via the /admin/trigger-* endpoints, so a hand-triggered
// run shows up in the background-job monitor alongside the scheduled ones
// (issue #391).
func recordManualJobRun(ctx context.Context, db *gorm.DB, jobName string, start time.Time, items *int, err error) {
	result := models.JobRunResultSuccess
	errStr := ""
	if err != nil {
		result = models.JobRunResultFailure
		errStr = err.Error()
		items = nil
	}
	models.RecordJobRun(ctx, db, models.JobRun{
		JobName:        jobName,
		Trigger:        models.JobTriggerManual,
		StartedAt:      start,
		FinishedAt:     time.Now(),
		Result:         result,
		Error:          errStr,
		ItemsProcessed: items,
	})
}

// TriggerReminders manually triggers the reminder email job (admin only)
func TriggerReminders(c *gin.Context, cfg config.Config) {
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)

	start := time.Now()
	sent, err := services.SendReminders(db, cfg)
	recordManualJobRun(c.Request.Context(), db, models.JobNameDailyReminders, start, &sent, err)
	if err != nil {
		log.Error().Err(err).Msg("Failed to trigger reminder emails")
		apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to send reminder emails").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Reminder emails sent successfully"})
}

// TriggerPurge manually triggers the delete-purge job (admin only, T26).
func TriggerPurge(c *gin.Context, cfg config.Config) {
	db := c.MustGet("db").(*gorm.DB)
	start := time.Now()
	services.PurgeSoftDeletedRows(db, cfg)
	services.PurgeExpiredContactShares(db, cfg)
	services.PurgeExpiredWebhookDeliveries(db, cfg)
	recordManualJobRun(c.Request.Context(), db, models.JobNamePurgeDeleted, start, nil, nil)
	c.JSON(http.StatusOK, gin.H{"message": "Purge completed"})
}

// GetCurrentUser returns the current authenticated user's information
func GetCurrentUser(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("User"))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	// T90: lazy self-contact backfill. Migration 000018 documented this as
	// handled lazily "when they first hit an endpoint that needs it," but no
	// endpoint ever called it — every account created before that migration
	// has had NULL self_contact_vcard_uid forever. This is that promised
	// backfill: EnsureSelfContact is idempotent (a no-op once set), so it is
	// safe to call unconditionally. It does *create a contact*, so a failure
	// here is logged and the user is served without a self contact rather
	// than 500ing the whole session — /users/me must never become a login
	// failure because a backfill failed.
	prevSelfContact := user.SelfContactVCardUID
	if err := services.EnsureSelfContact(db, &user); err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", userID).Msg("Failed to ensure self contact during GetCurrentUser")
		// Defensive: EnsureSelfContact only mutates user.SelfContactVCardUID
		// on success, but restore the pre-call value anyway so a future
		// change to it can't make this response claim a self contact the
		// database doesn't hold.
		user.SelfContactVCardUID = prevSelfContact
	}

	c.JSON(http.StatusOK, models.CurrentUserResponse{
		AdminUserResponse: models.AdminUserResponse{
			ID:         user.ID,
			Username:   user.Username,
			Email:      user.Email,
			Language:   user.Language,
			DateFormat: user.DateFormat,
			IsAdmin:    user.IsAdmin,
			CreatedAt:  user.CreatedAt,
			UpdatedAt:  user.UpdatedAt,
		},
		EnabledContactFields: user.EnabledContactFields,
		SelfContactVCardUID:  user.SelfContactVCardUID,
	})
}

// UpdateSelfContact sets or clears the caller's "Me" contact pointer (T90).
// A non-empty vcard_uid must resolve to a non-deleted Contact owned by the
// caller — otherwise 404, so a user can never point at someone else's contact
// (ownership scoping, /CLAUDE.md backend trap #5). An explicit null/empty
// clears the link. Setting it is a pointer move: the previously linked contact
// is neither deleted nor modified.
func UpdateSelfContact(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	input, appErr := middleware.GetValidated[models.SelfContactInput](c)
	if appErr != nil {
		apperrors.AbortWithError(c, appErr)
		return
	}

	// Clear the link. Scope to the caller's own row by ID, never by username.
	if input.VCardUID == "" {
		if err := db.Model(&models.User{}).Where("id = ?", userID).
			Update("self_contact_vcard_uid", nil).Error; err != nil {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("clear self contact").WithError(err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Self contact cleared", "self_contact_vcard_uid": nil})
		return
	}

	// The uid must resolve to a non-deleted Contact owned by the caller.
	var contact models.Contact
	if err := db.Where("user_id = ? AND vcard_uid = ?", userID, input.VCardUID).First(&contact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("vcard_uid", input.VCardUID))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("lookup self contact").WithError(err))
		return
	}

	if err := db.Model(&models.User{}).Where("id = ?", userID).
		Update("self_contact_vcard_uid", input.VCardUID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("set self contact").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Self contact updated", "self_contact_vcard_uid": input.VCardUID})
}

// UserDirectoryEntry is the thin per-user shape ListUserDirectory returns —
// deliberately just id+username, unlike admin-only ListUsers/GetUser's full
// AdminUserResponse, since any authenticated user (not just admins) can call
// this endpoint.
type UserDirectoryEntry struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
}

// ListUserDirectory returns every OTHER user on the instance (id + username
// only) for any authenticated user — unlike ListUsers, which is admin-only
// and returns the full user record. This exists to populate the recipient
// picker for P1 contact sharing: sharing a contact needs to name a recipient, and there was
// previously no way for a non-admin user to discover who else is on the
// instance.
func ListUserDirectory(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var users []models.User
	if err := db.Select("id, username").Where("id != ?", userID).Order("username ASC").Find(&users).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve user directory").WithError(err))
		return
	}

	entries := make([]UserDirectoryEntry, len(users))
	for i, u := range users {
		entries[i] = UserDirectoryEntry{ID: u.ID, Username: u.Username}
	}

	c.JSON(http.StatusOK, gin.H{"users": entries})
}

// ListUsers returns a paginated list of all users (admin only)
func ListUsers(c *gin.Context) {
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)
	pagination := GetPaginationParams(c)

	var total int64
	if err := db.Model(&models.User{}).Count(&total).Error; err != nil {
		log.Error().Err(err).Msg("Failed to count users")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("count users").WithError(err))
		return
	}

	var users []models.User
	if err := db.Order("id ASC").Offset(pagination.Offset).Limit(pagination.Limit).Find(&users).Error; err != nil {
		log.Error().Err(err).Msg("Failed to list users")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("list users").WithError(err))
		return
	}

	userResponses := make([]models.AdminUserResponse, len(users))
	for i, user := range users {
		userResponses[i] = models.AdminUserResponse{
			ID:         user.ID,
			Username:   user.Username,
			Email:      user.Email,
			Language:   user.Language,
			DateFormat: user.DateFormat,
			IsAdmin:    user.IsAdmin,
			CreatedAt:  user.CreatedAt,
			UpdatedAt:  user.UpdatedAt,
		}
	}

	totalPages := int(total) / pagination.Limit
	if int(total)%pagination.Limit > 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, models.AdminUsersListResponse{
		Users:      userResponses,
		Total:      total,
		Page:       pagination.Page,
		Limit:      pagination.Limit,
		TotalPages: totalPages,
	})
}

// GetUser returns a single user by ID (admin only)
func GetUser(c *gin.Context) {
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("id", "Invalid user ID"))
		return
	}

	var user models.User
	if err := db.First(&user, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("User"))
			return
		}
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to get user")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("get user").WithError(err))
		return
	}

	c.JSON(http.StatusOK, models.AdminUserResponse{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Language:   user.Language,
		DateFormat: user.DateFormat,
		IsAdmin:    user.IsAdmin,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	})
}

// CreateUser creates a new user (admin only). Mirrors RegisterUser's
// hashing/validation but is reached only through the admin-gated route
// (middleware.AdminMiddleware), so — unlike self-registration — it accepts
// IsAdmin directly and is not subject to RegistrationDisabled. Password is
// set by the admin directly, matching this app's existing admin-reset
// pattern (UpdateUser's Password field) rather than an invite-email flow
// (see T39's ticket).
func CreateUser(c *gin.Context) {
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)

	actingAdminID, ok := currentUserID(c)
	if !ok {
		return
	}

	input, appErr := middleware.GetValidated[models.AdminUserCreateInput](c)
	if appErr != nil {
		apperrors.AbortWithError(c, appErr)
		return
	}

	hashedPassword, err := services.HashPassword(input.Password)
	if err != nil {
		if errors.Is(err, services.ErrPasswordTooLong) {
			apperrors.AbortWithError(c, apperrors.ErrValidation(err.Error()))
		} else {
			log.Error().Err(err).Msg("Failed to hash password during admin create")
			apperrors.AbortWithError(c, apperrors.ErrInternal("Could not hash password").WithError(err))
		}
		return
	}

	user := models.User{
		Username: strings.ToLower(input.Username),
		Email:    strings.ToLower(input.Email),
		Password: hashedPassword,
		IsAdmin:  input.IsAdmin,
	}

	if err := db.Create(&user).Error; err != nil {
		// Surfaced clearly rather than swallowed — per T39's ticket, this is
		// also the path a collision with a soft-deleted account's email would
		// hit, though DeleteUser's hard-delete (T26) means that shouldn't
		// happen for accounts removed through this app.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			apperrors.AbortWithError(c, apperrors.ErrAlreadyExists("User").WithDetails("field", "username or email"))
			return
		}
		log.Error().Err(err).Msg("Failed to create user")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("create user").WithError(err))
		return
	}

	// Create the user's default self-contact.
	if err := services.EnsureSelfContact(db, &user); err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).
			Msg("Failed to create self contact for admin-created user")
	}

	// T18 audit: admin created an account (issue #381). The acting admin is
	// the actor; the new account is the subject.
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpCreate, actingAdminID)

	c.JSON(http.StatusCreated, models.AdminUserResponse{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Language:   user.Language,
		DateFormat: user.DateFormat,
		IsAdmin:    user.IsAdmin,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	})
}

// updates a user's information (admin only)
func UpdateUser(c *gin.Context) {
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)

	currentUserID, ok := currentUserID(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("id", "Invalid user ID"))
		return
	}

	input, appErr := middleware.GetValidated[models.AdminUserUpdateInput](c)
	if appErr != nil {
		apperrors.AbortWithError(c, appErr)
		return
	}

	var user models.User
	if err := db.First(&user, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("User"))
			return
		}
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to get user for update")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("get user").WithError(err))
		return
	}
	// Pre-mutation snapshot so the audit trail can name what actually changed.
	wasAdmin := user.IsAdmin

	// Prevent admin from removing their own admin status
	if input.IsAdmin != nil && !*input.IsAdmin && user.ID == currentUserID {
		apperrors.AbortWithError(c, apperrors.ErrForbidden("Cannot remove your own admin status"))
		return
	}

	// Check if trying to remove the last admin
	if input.IsAdmin != nil && !*input.IsAdmin && user.IsAdmin {
		var adminCount int64
		if err := db.Model(&models.User{}).Where("is_admin = ?", true).Count(&adminCount).Error; err != nil {
			log.Error().Err(err).Msg("Failed to count admins")
			apperrors.AbortWithError(c, apperrors.ErrDatabase("count admins").WithError(err))
			return
		}
		if adminCount <= 1 {
			apperrors.AbortWithError(c, apperrors.ErrForbidden("Cannot remove the last admin"))
			return
		}
	}

	// Apply updates
	if input.Username != nil {
		user.Username = strings.ToLower(*input.Username)
	}
	if input.Email != nil {
		user.Email = strings.ToLower(*input.Email)
	}
	if input.Password != nil {
		hashedPassword, err := services.HashPassword(*input.Password)
		if err != nil {
			if errors.Is(err, services.ErrPasswordTooLong) {
				apperrors.AbortWithError(c, apperrors.ErrValidation(err.Error()))
			} else {
				log.Error().Err(err).Msg("Failed to hash password during admin update")
				apperrors.AbortWithError(c, apperrors.ErrInternal("Could not hash password").WithError(err))
			}
			return
		}
		user.Password = hashedPassword
		// An admin resetting someone's password must end that user's existing
		// sessions, otherwise the reset does not actually lock anyone out.
		user.TokenVersion++
	}
	if input.IsAdmin != nil {
		user.IsAdmin = *input.IsAdmin
	}

	if err := db.Save(&user).Error; err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to update user")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			apperrors.AbortWithError(c, apperrors.ErrAlreadyExists("User").WithDetails("field", "username or email"))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	// Issue #413: an admin password reset is the operator-side response to a
	// suspected account takeover, so it must end standing API tokens the
	// same way the self-service recovery-path reset already does (#411) --
	// TokenVersion above only covers JWTs, which carry no version of their
	// own. Shares services.RevokeAllAPITokens with that call site.
	if input.Password != nil {
		if _, err := services.RevokeAllAPITokens(db, user.ID); err != nil {
			// The password change already succeeded; a failure here would be
			// misleading to report as an update failure. Logged so it isn't silent.
			log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to revoke API tokens after admin password reset")
		}
		// Issue #722: an admin password reset must not leave remembered
		// devices able to mint fresh sessions via biometric unlock either.
		if _, err := services.RevokeAllDeviceGrants(db, user.ID); err != nil {
			log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to revoke device grants after admin password reset") // # pragma: no cover — best-effort post-success revocation; only a failing store trips this
		}
	}

	// T18 audit: admin user edit, with the security-relevant deltas spelled
	// out (issue #381). The acting admin is the actor.
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpUpdate, currentUserID)
	if input.IsAdmin != nil && *input.IsAdmin != wasAdmin {
		models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpRoleChange, currentUserID)
	}
	if input.Password != nil {
		models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpPasswordReset, currentUserID)
	}

	c.JSON(http.StatusOK, models.AdminUserResponse{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Language:   user.Language,
		DateFormat: user.DateFormat,
		IsAdmin:    user.IsAdmin,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	})
}

// ResetUserTwoFactor is the operator-side recovery path for a user locked
// out of their own account: TOTP device and recovery codes both lost, and
// (per issue #592's design pass) email delivery is optional in this
// self-hosted app, so the self-service password/2FA recovery flows cannot be
// relied on either. Unlike DisableTwoFactor (self-service, requires a live
// TOTP/recovery-code proof), this is admin-only and requires no proof from
// the target -- the acting admin's own authenticated, admin-scoped session is
// the trust boundary, same as the existing admin password reset.
//
// Disables TOTP and hard-deletes all recovery codes for the target user,
// mirroring DisableTwoFactor's own update (two_factor_controller.go). Bumps
// TokenVersion the same way an admin password reset does, so the reset
// itself can't be silently undone by a session minted before it. Idempotent:
// calling this on a user with no 2FA enabled is a no-op that still returns
// 200, not an error.
func ResetUserTwoFactor(c *gin.Context) {
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)

	currentUserID, ok := currentUserID(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("id", "Invalid user ID"))
		return
	}

	var user models.User
	if err := db.First(&user, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("User"))
			return
		}
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to get user for 2FA reset")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("get user").WithError(err))
		return
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&user).Updates(map[string]any{
			"totp_enabled":          false,
			"totp_confirmed_at":     nil,
			"totp_secret_encrypted": nil,
			"token_version":         gorm.Expr("token_version + 1"),
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.RecoveryCode{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to reset user's 2FA")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("reset two-factor authentication").WithError(err))
		return
	}

	// TokenVersion went through a SQL expression, so the in-memory struct is
	// stale -- reload before using it in the response.
	if err := db.First(&user, id).Error; err != nil {
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to reload user after 2FA reset")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("get user").WithError(err))
		return
	}

	// Issue #722: an admin 2FA reset ends every device grant too — the 2FA
	// that previously stood between a stolen grant and a full account is now
	// gone, so remembered devices must re-enroll under the new posture.
	if _, err := services.RevokeAllDeviceGrants(db, user.ID); err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to revoke device grants after 2FA reset") // # pragma: no cover — best-effort post-success revocation; only a failing store trips this
	}

	// Issue #592 audit: admin-initiated 2FA reset, attributed to the acting
	// admin, distinct from the self-service AuditOpTOTPDisable.
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpTwoFactorAdminReset, currentUserID)

	c.JSON(http.StatusOK, models.AdminUserResponse{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Language:   user.Language,
		DateFormat: user.DateFormat,
		IsAdmin:    user.IsAdmin,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	})
}

// DeleteUser deletes a user and all their data (admin only)
func DeleteUser(c *gin.Context) {
	// DeleteUser is the single deliberate call-site exception to the
	// soft-delete rule (T26). users.email/username are UNIQUE, so a
	// soft-deleted account blocks re-registration forever. Here, and
	// only here, we use Unscoped() to genuinely remove the user row
	// and all soft-deleting children. No sync client survives a deleted
	// account, so there is nothing to tombstone for.
	log := logger.FromContext(c)
	db := c.MustGet("db").(*gorm.DB)

	currentUserID, ok := currentUserID(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("id", "Invalid user ID"))
		return
	}

	// Prevent admin from deleting themselves
	if uint(id) == currentUserID {
		apperrors.AbortWithError(c, apperrors.ErrForbidden("Cannot delete your own account"))
		return
	}

	var user models.User
	if err := db.First(&user, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("User"))
			return
		}
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to get user for deletion")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("get user").WithError(err))
		return
	}

	// Check if trying to delete the last admin
	if user.IsAdmin {
		var adminCount int64
		if err := db.Model(&models.User{}).Where("is_admin = ?", true).Count(&adminCount).Error; err != nil {
			log.Error().Err(err).Msg("Failed to count admins")
			apperrors.AbortWithError(c, apperrors.ErrDatabase("count admins").WithError(err))
			return
		}
		if adminCount <= 1 {
			apperrors.AbortWithError(c, apperrors.ErrForbidden("Cannot delete the last admin"))
			return
		}
	}

	// Capture the user's attachment stored names before the transaction
	// removes the rows, so their files can be cleaned from disk afterwards.
	var userAttachmentNames []string
	if err := db.Model(&models.Attachment{}).Where("user_id = ?", uint(id)).Pluck("stored_name", &userAttachmentNames).Error; err != nil {
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to load user attachments for deletion")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("load user attachments").WithError(err))
		return
	}

	// Delete user's data in a transaction
	err = db.Transaction(func(tx *gorm.DB) error {
		userID := uint(id)

		// Delete attachments (N7 — hard: account gone, no tombstoning needed).
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Attachment{}).Error; err != nil {
			return err
		}

		// Delete reminders (hard — user account gone, no tombstoning needed).
		// N9: their notification delivery state is hard-deleted first (the
		// reminder rows are Unscoped()-removed here, so the FK cascade would
		// cover it — this explicit pass keeps the manual-cascade checklist
		// complete rather than relying on the constraint).
		if err := tx.Where("reminder_id IN (SELECT id FROM reminders WHERE user_id = ?)", userID).Delete(&models.NotificationDelivery{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Reminder{}).Error; err != nil {
			return err
		}

		// Delete contact shares where the user is either party (hard —
		// payload could carry data about the other party's contacts; no
		// tombstoning once the account is gone). ContactShare has no soft
		// delete of its own, so no Unscoped() needed here.
		if err := tx.Where("from_user_id = ? OR to_user_id = ?", userID, userID).Delete(&models.ContactShare{}).Error; err != nil {
			return err
		}

		// Delete notes (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Note{}).Error; err != nil {
			return err
		}

		// Delete activity_contacts associations (many-to-many)
		if err := tx.Exec("DELETE FROM activity_contacts WHERE activity_id IN (SELECT id FROM activities WHERE user_id = ?)", userID).Error; err != nil {
			return err
		}

		// Delete activities (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Activity{}).Error; err != nil {
			return err
		}

		// Delete webhook deliveries, then webhooks (child before parent;
		// WebhookDelivery has no direct UserID, only WebhookID)
		if err := tx.Exec("DELETE FROM webhook_deliveries WHERE webhook_id IN (SELECT id FROM webhooks WHERE user_id = ?)", userID).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.Webhook{}).Error; err != nil {
			return err
		}

		// Delete CardDAV contact sync links, then subscriptions (child before parent)
		if err := tx.Where("user_id = ?", userID).Delete(&models.ContactSyncLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.ContactSubscription{}).Error; err != nil {
			return err
		}

		// Delete household memberships, then households (child before parent)
		if err := tx.Where("user_id = ?", userID).Delete(&models.HouseholdMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.Household{}).Error; err != nil {
			return err
		}

		// Delete circle memberships, then circles (child before parent)
		if err := tx.Where("user_id = ?", userID).Delete(&models.CircleMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.Circle{}).Error; err != nil {
			return err
		}

		// Delete contact tags, then tags (child before parent)
		if err := tx.Where("user_id = ?", userID).Delete(&models.ContactTag{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.Tag{}).Error; err != nil {
			return err
		}

		// Delete custom field values, then field definitions (child before parent)
		if err := tx.Where("user_id = ?", userID).Delete(&models.FieldValue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.FieldDefinition{}).Error; err != nil {
			return err
		}

		// Delete CardDAV sync token
		if err := tx.Where("user_id = ?", userID).Delete(&models.CardDAVSync{}).Error; err != nil {
			return err
		}

		// Delete API tokens
		if err := tx.Where("user_id = ?", userID).Delete(&models.ApiToken{}).Error; err != nil {
			return err
		}

		// Delete device grants (issue #722) — the biometric-login credentials
		// die with the account, exactly like API tokens.
		if err := tx.Where("user_id = ?", userID).Delete(&models.DeviceGrant{}).Error; err != nil {
			return err
		}

		// Delete reminder completions
		if err := tx.Where("user_id = ?", userID).Delete(&models.ReminderCompletion{}).Error; err != nil {
			return err
		}

		// Delete calendar event links, then calendar subscriptions (child before parent)
		if err := tx.Where("user_id = ?", userID).Delete(&models.CalendarEventLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.CalendarSubscription{}).Error; err != nil {
			return err
		}

		// Delete relationship-graph edges
		if err := tx.Where("user_id = ?", userID).Delete(&models.RelationshipEdge{}).Error; err != nil {
			return err
		}

		// Delete life events (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.LifeEvent{}).Error; err != nil {
			return err
		}

		// Delete preferences (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Preference{}).Error; err != nil {
			return err
		}

		// Delete cadence policies (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.CadencePolicy{}).Error; err != nil {
			return err
		}

		// Delete conversation agenda items (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.ConversationAgenda{}).Error; err != nil {
			return err
		}

		// Delete gift records (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Gift{}).Error; err != nil {
			return err
		}

		// Delete external integration links and enrichment events (T14 —
		// hard delete, edge/join-shaped)
		if err := tx.Where("user_id = ?", userID).Delete(&models.ExternalIdentity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.ExternalActivity{}).Error; err != nil {
			return err
		}

		// Delete reach-out suggestions and the detection watermark (issue
		// #177 — hard delete, system-generated/cursor-shaped)
		if err := tx.Where("user_id = ?", userID).Delete(&models.ReachOutSuggestion{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.ReachOutCursor{}).Error; err != nil {
			return err
		}

		// Delete CardDAV sync conflicts (issue #395 — hard delete,
		// system-generated; nothing left to review once the account is gone)
		if err := tx.Where("user_id = ?", userID).Delete(&models.ContactSyncConflict{}).Error; err != nil {
			return err
		}

		// Delete the user's Immich connection config (T15/T16)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.ImmichConfig{}).Error; err != nil {
			return err
		}

		// Delete the user's file-integration connection configs (P2a/P2b/P2c —
		// Paperless-ngx, Seafile, Nextcloud/ownCloud WebDAV)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.PaperlessConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.SeafileConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.WebDAVConfig{}).Error; err != nil {
			return err
		}

		// Delete the user's notification channel config and push device
		// subscriptions (N9 — hard: account gone, no tombstoning needed)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.NotificationConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.PushSubscription{}).Error; err != nil {
			return err
		}

		// Mobile push device registrations (M2 — account gone, no tombstoning
		// needed; matches PushSubscription above)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.DeviceRegistration{}).Error; err != nil {
			return err
		}

		// Delete link field types (hard — user account gone, no
		// tombstoning needed; matches the other DeletedAt-bearing entities
		// above, e.g. CadencePolicy/Preference/LifeEvent)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.LinkFieldType{}).Error; err != nil {
			return err
		}

		// T93: duplicate-pair dismissal memory (hard, edge/join-shaped — account
		// gone, no tombstoning needed)
		if err := tx.Where("user_id = ?", userID).Delete(&models.DismissedDuplicatePair{}).Error; err != nil {
			return err
		}

		// N8: hashed 2FA recovery codes (hard, join-shaped — a code is its
		// hash; the FK cascade on recovery_codes.user_id would cover it, but
		// the manual-cascade checklist stays complete rather than relying on
		// the constraint)
		if err := tx.Where("user_id = ?", userID).Delete(&models.RecoveryCode{}).Error; err != nil {
			return err
		}

		// Delete import run history (issue #651 — hard, user-scoped
		// operational bookkeeping; the account is gone, nothing to keep).
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.ImportRun{}).Error; err != nil {
			return err
		}

		// Delete idempotency-key replay records (issue #459, CON-04 — hard,
		// user-scoped, transient dedup bookkeeping).
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.IdempotencyKey{}).Error; err != nil {
			return err
		}

		// Delete contacts (hard)
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.Contact{}).Error; err != nil {
			return err
		}

		// Delete user (hard — accounts must be re-registerable, T26)
		if err := tx.Unscoped().Delete(&user).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Uint64("user_id", id).Msg("Failed to delete user")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("delete user").WithError(err))
		return
	}

	// Remove the user's attachment files after the transaction committed
	// (file deletion can't be rolled back). Uses the admin's own context for
	// the config; the deleted user's files are addressed by stored name.
	deleteUserAttachmentFiles(c, userAttachmentNames)

	// T18 audit: admin deleted an account (issue #381). Recorded after the
	// transaction so the event itself (UserID = acting admin) survives the
	// target's hard-delete cascade.
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", id), models.AuditOpDelete, currentUserID)

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// deleteUserAttachmentFiles removes attachment files owned by a deleted user
// from disk (N7). storedNames were captured before the transaction deleted the
// rows.
func deleteUserAttachmentFiles(c *gin.Context, storedNames []string) {
	dir := currentConfig(c).AttachmentsDir
	if dir == "" || len(storedNames) == 0 {
		return
	}
	log := logger.FromContext(c)
	for _, name := range storedNames {
		path, err := attachments.StoredPath(dir, name)
		if err != nil {
			log.Warn().Err(err).Str("stored_name", name).Msg("Failed to resolve attachment path for user cleanup")
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", path).Msg("Failed to delete user attachment file")
		}
	}
}
