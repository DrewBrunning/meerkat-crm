package services

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Issue #722: device-grant token minting must produce a one-way-hashable,
// collision-resistant credential — the same properties the api-token path
// relies on.
func TestGenerateDeviceGrantToken_ProducesUniqueHashedCredentials(t *testing.T) {
	a, hashA, err := generateDeviceGrantToken()
	require.NoError(t, err)
	b, hashB, err := generateDeviceGrantToken()
	require.NoError(t, err)

	require.NotEmpty(t, a)
	require.NotEqual(t, a, b, "two grants must never collide")
	require.NotEqual(t, a, hashA, "only the hash is ever stored")
	require.NotEqual(t, hashA, hashB)

	// The plaintext is 32 random bytes base64url (>= 256 bits of entropy).
	decoded, err := base64.RawURLEncoding.DecodeString(a)
	require.NoError(t, err)
	require.Len(t, decoded, 32)
}

func TestHashDeviceGrantToken_MatchesGenerator(t *testing.T) {
	plaintext, stored, err := generateDeviceGrantToken()
	require.NoError(t, err)
	require.Equal(t, stored, HashDeviceGrantToken(plaintext))
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(plaintext))), HashDeviceGrantToken(plaintext))
}

func TestCreateDeviceGrant_PersistsHashOnly(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "grantee", Email: "grantee@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)

	grant, plaintext, err := CreateDeviceGrant(db, user.ID, "Pixel 8a")
	require.NoError(t, err)
	require.Equal(t, "Pixel 8a", grant.Label)
	require.NotEmpty(t, plaintext)

	var stored models.DeviceGrant
	require.NoError(t, db.First(&stored, grant.ID).Error)
	require.Equal(t, HashDeviceGrantToken(plaintext), stored.TokenHash)
	require.NotEqual(t, plaintext, stored.TokenHash)
}

func TestRevokeAllDeviceGrants_RevokesOnlyActiveGrants(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "grantee2", Email: "grantee2@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)

	active, _, err := CreateDeviceGrant(db, user.ID, "phone")
	require.NoError(t, err)
	alreadyRevoked, _, err := CreateDeviceGrant(db, user.ID, "tablet")
	require.NoError(t, err)
	revokedAt := time.Now()
	require.NoError(t, db.Model(&models.DeviceGrant{}).
		Where("id = ?", alreadyRevoked.ID).
		Update("revoked_at", revokedAt).Error)

	count, err := RevokeAllDeviceGrants(db, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "only the still-active grant is revoked")

	var activeRow models.DeviceGrant
	require.NoError(t, db.First(&activeRow, active.ID).Error)
	require.NotNil(t, activeRow.RevokedAt)
}

func TestRevokeAllDeviceGrants_ScopedToUser(t *testing.T) {
	db := dbtest.New(t)
	alice := models.User{Username: "alice3", Email: "alice3@example.com", Password: "x"}
	bob := models.User{Username: "bob3", Email: "bob3@example.com", Password: "x"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	require.NoError(t, db.Create(&models.DeviceGrant{UserID: bob.ID, TokenHash: "h1", Label: "bob-phone"}).Error)
	_, _, err := CreateDeviceGrant(db, alice.ID, "alice-phone")
	require.NoError(t, err)

	count, err := RevokeAllDeviceGrants(db, alice.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	var bobGrant models.DeviceGrant
	require.NoError(t, db.Where("user_id = ?", bob.ID).First(&bobGrant).Error)
	require.Nil(t, bobGrant.RevokedAt, "another user's grant is untouched")
}

func TestCreateDeviceGrant_ClosedDatabaseErrors(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "grantee4", Email: "grantee4@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, _, err = CreateDeviceGrant(db, user.ID, "phone")
	require.Error(t, err)
}

func TestRevokeAllDeviceGrants_ClosedDatabaseErrors(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "grantee5", Email: "grantee5@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	_, _, err := CreateDeviceGrant(db, user.ID, "phone")
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = RevokeAllDeviceGrants(db, user.ID)
	require.Error(t, err)
}

var _ = gorm.ErrRecordNotFound
