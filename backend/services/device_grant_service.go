package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"mycorrhizal/models"

	"gorm.io/gorm"
)

// generateDeviceGrantToken mints a new plaintext device grant (32 random
// bytes, base64url) and its SHA-256 hash for storage. The plaintext is the
// one secret a device holds to prove it enrolled; it is returned exactly once
// (at creation) and must never be logged or persisted.
func generateDeviceGrantToken() (plaintext, hash string, err error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", "", err // # pragma: no cover — crypto/rand failure only fires on catastrophic OS entropy exhaustion
	}
	plaintext = base64.RawURLEncoding.EncodeToString(rawBytes)
	hash = fmt.Sprintf("%x", sha256.Sum256([]byte(plaintext)))
	return plaintext, hash, nil
}

// HashDeviceGrantToken derives the stored hash for a submitted plaintext grant
// — the same SHA-256 the generator produces, so exchange and creation can
// never disagree about how a hash is formed.
func HashDeviceGrantToken(token string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
}

// CreateDeviceGrant issues a new grant for a user and returns the model row
// plus the one-time plaintext token. The user is expected to have just
// authenticated interactively (password / 2FA / OIDC) — a grant must never be
// minted for an unauthenticated caller.
func CreateDeviceGrant(db *gorm.DB, userID uint, label string) (*models.DeviceGrant, string, error) {
	plaintext, hash, err := generateDeviceGrantToken()
	if err != nil {
		return nil, "", err // # pragma: no cover — mirrors the crypto/rand failure in generateDeviceGrantToken
	}
	grant := models.DeviceGrant{
		UserID:    userID,
		TokenHash: hash,
		Label:     label,
	}
	if err := db.Create(&grant).Error; err != nil {
		return nil, "", err
	}
	return &grant, plaintext, nil
}

// RevokeAllDeviceGrants revokes every currently-active grant for a user and
// returns how many were revoked. Shared by every call site that must end a
// user's standing grants as part of a bigger action: password change/reset
// and 2FA enable/disable/regenerate (the same sites that bump TokenVersion),
// so a stolen password or a 2FA change cannot silently re-mint sessions from
// a remembered device.
func RevokeAllDeviceGrants(db *gorm.DB, userID uint) (int64, error) {
	result := db.Model(&models.DeviceGrant{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now())
	return result.RowsAffected, result.Error
}
