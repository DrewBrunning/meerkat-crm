package models

import (
	"time"

	"gorm.io/gorm"
)

// DeviceGrant is a long-lived, revocable per-install credential (issue #722's
// "fully biometric login"): minted for an authenticated device so that a
// successful local biometric unlock can be exchanged — via
// POST /auth/device/session — for a fresh session JWT past the stored token's
// expiry. Follows ApiToken exactly: only the SHA-256 hash is stored, the row
// is soft-deleted (gorm.Model), and grants are revoked with RevokedAt rather
// than removed, so revocation is auditable. The plaintext token is returned
// exactly once at creation and never persisted or logged.
type DeviceGrant struct {
	gorm.Model
	UserID     uint   `gorm:"not null"`
	TokenHash  string `gorm:"not null;unique" json:"-"`
	Label      string `gorm:"not null;default:''"`
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}
