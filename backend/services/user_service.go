package services

import (
	"errors"
	"mycorrhizal/config"
	"mycorrhizal/models"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var ErrPasswordTooLong = errors.New("password must not exceed 72 characters")

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password cannot be empty")
	}

	if len([]byte(password)) > 72 {
		return "", ErrPasswordTooLong
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

// GenerateToken signs a session JWT for user. sid is the id of the
// server-side session row this token belongs to (issue #866); it goes in the
// `sid` claim and AuthMiddleware rejects the request if the row is gone,
// revoked, or idle-timed-out. Callers mint the row first — use IssueSession,
// which does both — rather than calling this directly.
func GenerateToken(user models.User, cfg *config.Config, sid string) (string, error) {
	JWTSecretKey := cfg.JWTSecretKey
	if JWTSecretKey == "" {
		return "", errors.New("JWT secret key is empty")
	}

	JWTExpiryHours := cfg.JWTExpiryHours
	if JWTExpiryHours <= 0 {
		return "", errors.New("JWT expiry hours is invalid")
	}

	// Note: is_admin is intentionally NOT included in the JWT (AdminMiddleware handles this)
	claims := jwt.MapClaims{
		"authorized": true,
		"username":   user.Username,
		"user_id":    user.ID,
		// Checked against the user's current TokenVersion on every request, so
		// bumping that column invalidates this token immediately.
		"token_version": user.TokenVersion,
		// The server-side session row (issue #866). Empty only for tokens
		// minted before migration 000053; AuthMiddleware rejects those,
		// forcing one re-login, the same way it treats a missing token_version.
		"sid": sid,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour * time.Duration(JWTExpiryHours)).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(JWTSecretKey))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// SessionIDFromToken returns the `sid` claim of a signed session JWT, or ""
// if the token is absent, unparseable, wrong-algorithm, badly signed, or
// carries no `sid`. Used by the logout path, which runs outside AuthMiddleware
// and must find the current session row to revoke without trusting an
// unverified token. Expiry is NOT a disqualifier here — an expired token still
// names a real row worth revoking.
func SessionIDFromToken(tokenString string, cfg *config.Config) string {
	if tokenString == "" {
		return ""
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))
	token, err := parser.Parse(tokenString, func(t *jwt.Token) (any, error) {
		return []byte(cfg.JWTSecretKey), nil
	})
	if token == nil {
		return ""
	}
	// An expired-but-otherwise-valid token still yields its claims here; only
	// a signature/format failure leaves them untrustworthy.
	if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
		return ""
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	sid, _ := claims["sid"].(string)
	return sid
}

// EnsureSelfContact creates a self contact for a user if one doesn't already
// exist, then stores the VCardUID on the user record. Safe to call multiple
// times — the second call is a no-op (the contact already exists). Called at
// registration time (all three paths) so every new user starts with a "Me"
// contact. Pre-existing users without one are handled lazily when they first
// hit an endpoint that needs it.
func EnsureSelfContact(db *gorm.DB, user *models.User) error {
	if user.SelfContactVCardUID != nil && *user.SelfContactVCardUID != "" {
		return nil
	}

	// Atomic: either the contact exists and the pointer is set, or neither.
	// A contact created here but left unpointed by a failed pointer-write
	// would be an orphan forever — the next call would pass the nil check
	// again, create a second contact, and the first would never be cleaned
	// up. That window matters more now that GetCurrentUser calls this on the
	// lazy path (T90).
	return db.Transaction(func(tx *gorm.DB) error {
		contact := models.Contact{
			UserID:    user.ID,
			Firstname: user.Username,
		}
		if err := tx.Create(&contact).Error; err != nil {
			return err
		}
		vcardUID := contact.VCardUID
		if err := tx.Model(user).Update("self_contact_vcard_uid", vcardUID).Error; err != nil {
			return err
		}
		user.SelfContactVCardUID = &vcardUID
		return nil
	})
}
