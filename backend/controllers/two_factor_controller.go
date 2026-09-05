package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"mycorrhizal/config"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recoveryCodeCount is how many single-use fallback codes are minted at
// enrollment and regeneration.
const recoveryCodeCount = 10

// oidcUserErr is the error text for OIDC-provisioned accounts, whose second
// factor belongs to their identity provider (N8 — 2FA is unavailable for
// them, not silently bypassed).
const oidcUserErr = "Two-factor authentication is not available for accounts that sign in through an identity provider"

// ---------------------------------------------------------------------------
// N8 — 2FA / TOTP (issue #158). Enrollment + management endpoints, all under
// an authenticated session (so the caller has already passed 2FA). The
// step-2 LOGIN endpoint (Complete2FALogin) is public and lives here too.
//
// OIDC-provisioned users (OIDCSubject set) are blocked from enrolling: their
// second factor is the IdP's, and a local TOTP that is never enforced on the
// OIDC path would be security theater.
// ---------------------------------------------------------------------------

// GetTwoFactorStatus reports whether the caller has 2FA enabled. Purely a UI
// switch — never exposes the secret or recovery-code presence.
func GetTwoFactorStatus(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"enabled": user.TOTPEnabled})
}

// SetupTwoFactor begins enrollment: mints a fresh TOTP secret, stores its
// encrypted form, and returns the plaintext secret + otpauth URL so the client
// can render a QR code. 2FA is NOT yet enforced — ConfirmTwoFactor flips the
// switch once the caller proves they can generate codes.
func SetupTwoFactor(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	if user.TOTPEnabled {
		apperrors.AbortWithError(c, apperrors.ErrConflict("Two-factor authentication is already enabled"))
		return
	}
	if user.OIDCSubject != nil && *user.OIDCSubject != "" {
		apperrors.AbortWithError(c, apperrors.ErrForbidden(oidcUserErr))
		return
	}

	secret, otpauthURL, err := services.GenerateTOTPSecret(user.Email)
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to generate TOTP secret")
		apperrors.AbortWithError(c, apperrors.ErrInternal("Could not generate TOTP secret").WithError(err))
		return
	}

	encrypted, err := services.EncryptCredential(currentConfig(c).JWTSecretKey, secret)
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to encrypt TOTP secret")
		apperrors.AbortWithError(c, apperrors.ErrInternal("Could not store TOTP secret").WithError(err))
		return
	}

	user.TOTPSecretEncrypted = &encrypted
	if err := db.Model(&user).Select("TOTPSecretEncrypted").Updates(&user).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to persist pending TOTP secret")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret":      secret,
		"otpauth_url": otpauthURL,
	})
}

// ConfirmTwoFactor completes enrollment. Requires a valid TOTP code from the
// secret minted by SetupTwoFactor; on success 2FA is enabled, recovery codes
// are minted and returned (plaintext, exactly once), and every existing
// session is invalidated (TokenVersion bump) so other devices must
// re-authenticate — now through the second factor.
func ConfirmTwoFactor(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	if user.TOTPEnabled {
		apperrors.AbortWithError(c, apperrors.ErrConflict("Two-factor authentication is already enabled"))
		return
	}
	if user.TOTPSecretEncrypted == nil || *user.TOTPSecretEncrypted == "" {
		apperrors.AbortWithError(c, apperrors.ErrConflict("Run 2FA setup first to generate a secret"))
		return
	}

	var input struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Code == "" {
		apperrors.AbortWithError(c, apperrors.ErrMissingField("code"))
		return
	}

	secret, err := services.DecryptCredential(currentConfig(c).JWTSecretKey, *user.TOTPSecretEncrypted)
	if err != nil || !services.ValidateTOTP(secret, input.Code) {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("code", "Invalid code. Please try again."))
		return
	}

	recoveryCodes, err := services.GenerateRecoveryCodes(recoveryCodeCount)
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to generate recovery codes")
		apperrors.AbortWithError(c, apperrors.ErrInternal("Could not generate recovery codes").WithError(err))
		return
	}

	now := time.Now()
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&user).Updates(map[string]any{
			"totp_enabled":      true,
			"totp_confirmed_at": now,
			"token_version":     gorm.Expr("token_version + 1"),
		}).Error; err != nil {
			return err
		}
		if err := services.StoreRecoveryCodes(tx, user.ID, recoveryCodes); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to confirm 2FA enrollment")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	// The token_version bump above was done via a SQL expression, so the local
	// struct still holds the OLD version. Reissue the caller's session from a
	// reloaded row — a token minted from the stale struct would carry a
	// token_version the DB no longer has and AuthMiddleware would reject it,
	// silently signing the caller out of their own enrollment.
	if err := db.First(&user, userID).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", userID).Msg("Failed to reload user after 2FA enrollment")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	// Issue #722: enabling 2FA ends every device grant. A grant minted under
	// the pre-2FA posture let a stolen device mint sessions without the new
	// second factor; remembered devices re-enroll on their next interactive
	// login, which now stands behind the freshly-enabled 2FA.
	if _, err := services.RevokeAllDeviceGrants(db, user.ID); err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to revoke device grants after 2FA enrollment")
	}
	// T18 audit: 2FA enabled (issue #381).
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpTOTPEnable, user.ID)
	reissueSessionToken(c, user)

	c.JSON(http.StatusOK, gin.H{
		"message":        "Two-factor authentication enabled",
		"recovery_codes": recoveryCodes,
	})
}

// DisableTwoFactor turns 2FA off. Requires a valid TOTP code (the caller is
// already authenticated, so a working authenticator is the expected proof);
// clears the secret and recovery codes, and invalidates all other sessions.
func DisableTwoFactor(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	if !user.TOTPEnabled {
		apperrors.AbortWithError(c, apperrors.ErrConflict("Two-factor authentication is not enabled"))
		return
	}

	var input struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Code == "" {
		apperrors.AbortWithError(c, apperrors.ErrMissingField("code"))
		return
	}

	if !valid2FAProof(db, &user, input.Code, currentConfig(c).JWTSecretKey) {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("code", "Invalid code. Please try again."))
		return
	}

	err := db.Transaction(func(tx *gorm.DB) error {
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
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to disable 2FA")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	// Reload for the same reason as ConfirmTwoFactor: the token_version bump
	// went through a SQL expression, so the struct is stale; a reissued token
	// minted from it would be rejected by AuthMiddleware.
	if err := db.First(&user, userID).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", userID).Msg("Failed to reload user after disabling 2FA")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	// Issue #722: disabling 2FA revokes every device grant — the TOTP that
	// used to back a stolen grant is gone, so remembered devices must
	// re-enroll under the weakened posture rather than quietly keeping a
	// passwordless door open.
	if _, err := services.RevokeAllDeviceGrants(db, user.ID); err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to revoke device grants after disabling 2FA")
	}
	// T18 audit: 2FA disabled (issue #381).
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpTOTPDisable, user.ID)
	reissueSessionToken(c, user)
	c.JSON(http.StatusOK, gin.H{"message": "Two-factor authentication disabled"})
}

// RegenerateRecoveryCodes replaces the user's unused recovery codes with a
// fresh set (returned plaintext, exactly once) and invalidates the old ones.
// Requires a valid TOTP code — regeneration is the recovery path, so it must
// be gated on the strongest proof available.
func RegenerateRecoveryCodes(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	if !user.TOTPEnabled {
		apperrors.AbortWithError(c, apperrors.ErrConflict("Two-factor authentication is not enabled"))
		return
	}

	var input struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Code == "" {
		apperrors.AbortWithError(c, apperrors.ErrMissingField("code"))
		return
	}

	if !valid2FAProof(db, &user, input.Code, currentConfig(c).JWTSecretKey) {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("code", "Invalid code. Please try again."))
		return
	}

	recoveryCodes, err := services.GenerateRecoveryCodes(recoveryCodeCount)
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to generate recovery codes")
		apperrors.AbortWithError(c, apperrors.ErrInternal("Could not generate recovery codes").WithError(err))
		return
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.RecoveryCode{}).Error; err != nil {
			return err
		}
		if err := services.StoreRecoveryCodes(tx, user.ID, recoveryCodes); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to regenerate recovery codes")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	// T18 audit: recovery codes regenerated — the old set is dead (issue #381).
	models.RecordAuditEvent(models.AuditEntityUser, fmt.Sprintf("%d", user.ID), models.AuditOpRecoveryRegen, user.ID)

	c.JSON(http.StatusOK, gin.H{"recovery_codes": recoveryCodes})
}

// Complete2FALogin is step 2 of interactive login (public route). The client
// first POSTs /login with correct credentials; if the account has 2FA enabled
// that response sets a short-lived 2fa_pending cookie and returns
// two_factor_required. This handler exchanges the pending challenge + a valid
// TOTP or recovery code for the real session cookie.
func Complete2FALogin(c *gin.Context, cfg *config.Config) {
	var input struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Code == "" {
		apperrors.AbortWithError(c, apperrors.ErrMissingField("code"))
		return
	}

	pending, err := c.Cookie("2fa_pending")
	if err != nil || pending == "" {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("No pending two-factor login found. Please sign in again."))
		return
	}

	userID, username, ok := services.Parse2FAChallengeToken(pending, cfg)
	if !ok {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Invalid or expired two-factor session. Please sign in again."))
		return
	}

	// Rate-limit the code step by account, not just by IP. A 6-digit code is
	// brute-forceable in minutes without it (N8). Using the username keeps the
	// budget stable regardless of whether step 1 used username or email.
	accountLimiter := middleware.GetAccountRateLimiter()
	if isLocked, remainingSecs := accountLimiter.IsLocked(username); isLocked {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":          "Account temporarily locked",
			"message":        "Too many failed login attempts. Please try again later.",
			"retry_after":    remainingSecs,
			"retry_after_at": time.Now().Add(time.Duration(remainingSecs) * time.Second).Format(time.RFC3339),
		})
		c.Abort()
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Invalid or expired two-factor session. Please sign in again."))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}
	// The account must still require 2FA: the challenge was minted against a
	// state that may have changed (2FA disabled, 2FA never enabled).
	if !user.TOTPEnabled {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Two-factor authentication is no longer enabled. Please sign in again."))
		return
	}

	if !valid2FAProof(db, &user, input.Code, currentConfig(c).JWTSecretKey) {
		// T18 audit: failed 2FA step for a known account (issue #381).
		models.RecordAuditEvent(models.AuditEntityAuth, user.Username, models.AuditOpLoginFailed, user.ID)
		_, lockoutSecs := accountLimiter.RecordFailedAttempt(username)
		if lockoutSecs > 0 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":          "Account temporarily locked",
				"message":        "Too many failed login attempts. Please try again later.",
				"retry_after":    lockoutSecs,
				"retry_after_at": time.Now().Add(time.Duration(lockoutSecs) * time.Second).Format(time.RFC3339),
			})
			c.Abort()
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("code", "Invalid code. Please try again."))
		return
	}
	accountLimiter.RecordSuccessfulLogin(username)

	tokenString, err := services.GenerateToken(user, cfg)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInternal("Could not generate token").WithError(err))
		return
	}

	// T18 audit: fully authenticated (password step + 2FA step) — only after
	// the session token is actually minted (issue #381).
	models.RecordAuditEvent(models.AuditEntityAuth, user.Username, models.AuditOpLogin, user.ID)

	// Clear the one-time challenge and issue the real session.
	// Issue #392: Strict — only ever read/set by same-origin XHR.
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("2fa_pending", "", -1, "/", cfg.CookieDomain, cfg.CookieSecure, true)
	c.SetCookie(
		"auth_token",
		tokenString,
		cfg.JWTExpiryHours*3600,
		"/",
		cfg.CookieDomain,
		cfg.CookieSecure,
		true,
	)

	c.JSON(http.StatusOK, gin.H{
		"language":    user.Language,
		"date_format": user.DateFormat,
	})
}

// valid2FAProof reports whether code is either the account's current TOTP code
// or an unused recovery code. Recovery-code consumption deletes the row, so a
// successful recovery login burns that code permanently. jwtSecret decrypts the
// stored TOTP secret.
func valid2FAProof(db *gorm.DB, user *models.User, code, jwtSecret string) bool {
	if user.TOTPSecretEncrypted != nil && *user.TOTPSecretEncrypted != "" {
		secret, err := services.DecryptCredential(jwtSecret, *user.TOTPSecretEncrypted)
		if err == nil && services.ValidateTOTP(secret, code) {
			return true
		}
	}
	return services.ConsumeRecoveryCode(db, user.ID, code)
}

// reissueSessionToken re-mints the caller's session cookie after a TokenVersion
// bump (2FA enabled/disabled), so the current session survives while all other
// sessions are invalidated. API-token callers are left alone — API tokens are
// separate credentials with no token version (same policy as ChangePassword).
func reissueSessionToken(c *gin.Context, user models.User) {
	if isAPIToken, _ := c.Get("isAPIToken"); isAPIToken == true {
		return
	}
	cfg := currentConfig(c)
	if cfg.JWTSecretKey == "" {
		logger.FromContext(c).Warn().Uint("user_id", user.ID).Msg("Cannot re-issue session token: JWT secret missing from context")
		return
	}
	tokenString, err := services.GenerateToken(user, &cfg)
	if err != nil {
		logger.FromContext(c).Error().Err(err).Uint("user_id", user.ID).Msg("Failed to re-issue token after 2FA change")
		return
	}
	// Issue #392: Strict, matching the cookie as set at login.
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		"auth_token",
		tokenString,
		cfg.JWTExpiryHours*3600,
		"/",
		cfg.CookieDomain,
		cfg.CookieSecure,
		true,
	)
}
