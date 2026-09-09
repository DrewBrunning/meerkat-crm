package services

import (
	"regexp"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/golang-jwt/jwt/v4"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const twoFactorTestSecret = "test-jwt-secret-that-is-at-least-32-characters-long"

var recoveryCodeShape = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{5}-[A-HJ-NP-Z2-9]{5}-[A-HJ-NP-Z2-9]{5}$`)

func TestGenerateTOTPSecretAndValidate(t *testing.T) {
	secret, otpauthURL, err := GenerateTOTPSecret("alice@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	require.Contains(t, otpauthURL, "otpauth://totp/")
	require.Contains(t, otpauthURL, "secret="+secret)

	// A code minted from the secret at the current step validates.
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	assert.True(t, ValidateTOTP(secret, code))

	// Wrong code, empty secret, empty code all fail.
	assert.False(t, ValidateTOTP(secret, "000000"))
	assert.False(t, ValidateTOTP("", code))
	assert.False(t, ValidateTOTP(secret, ""))

	// A code from one step ago still validates (skew window ±1)...
	oldCode, err := totp.GenerateCode(secret, time.Now().Add(-30*time.Second))
	require.NoError(t, err)
	assert.True(t, ValidateTOTP(secret, oldCode))
	// ...but a code two steps back does not.
	staleCode, err := totp.GenerateCode(secret, time.Now().Add(-60*time.Second))
	require.NoError(t, err)
	assert.False(t, ValidateTOTP(secret, staleCode))
}

func TestGenerateRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(10)
	require.NoError(t, err)
	require.Len(t, codes, 10)

	seen := map[string]bool{}
	for _, c := range codes {
		require.Regexp(t, recoveryCodeShape, c, "recovery code must be in XXXXX-XXXXX-XXXXX shape")
		assert.False(t, seen[c], "recovery codes must be unique")
		seen[c] = true
	}
}

func TestRecoveryCodeStoreAndConsume(t *testing.T) {
	db := dbtest.New(t)

	user := models.User{Username: "rc-user", Email: "rc-user@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)

	codes, err := GenerateRecoveryCodes(3)
	require.NoError(t, err)
	require.NoError(t, StoreRecoveryCodes(db, user.ID, codes))

	// All three codes validate, and the stored hashes are not plaintext.
	for _, c := range codes {
		assert.True(t, ConsumeRecoveryCode(db, user.ID, c), "code %q should be valid", c)
	}
	var stored []models.RecoveryCode
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&stored).Error)
	assert.Len(t, stored, 0, "all codes consumed => all rows deleted")

	// Re-consuming the same code fails (single-use).
	assert.False(t, ConsumeRecoveryCode(db, user.ID, codes[0]))

	// A code that was never stored fails, and a different user's codes are
	// invisible.
	other := models.User{Username: "rc-other", Email: "rc-other@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)
	assert.False(t, ConsumeRecoveryCode(db, other.ID, "AAAAA-BBBBB-CCCCC"))
}

func TestTwoFactorChallengeTokenRoundTrip(t *testing.T) {
	cfg := &config.Config{JWTSecretKey: twoFactorTestSecret}
	user := models.User{Username: "alice", Email: "alice@example.com"}
	user.ID = 42

	token, err := Generate2FAChallengeToken(user, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	userID, username, ok := Parse2FAChallengeToken(token, cfg)
	require.True(t, ok)
	assert.Equal(t, user.ID, userID)
	assert.Equal(t, "alice", username)
}

func TestTwoFactorChallengeTokenRejectsInvalid(t *testing.T) {
	cfg := &config.Config{JWTSecretKey: twoFactorTestSecret}
	otherCfg := &config.Config{JWTSecretKey: "a-completely-different-secret-that-is-long-enough"}
	user := models.User{Username: "alice"}

	token, err := Generate2FAChallengeToken(user, cfg)
	require.NoError(t, err)

	// Wrong secret.
	_, _, ok := Parse2FAChallengeToken(token, otherCfg)
	assert.False(t, ok)

	// Empty token.
	_, _, ok = Parse2FAChallengeToken("", cfg)
	assert.False(t, ok)

	// Garbage.
	_, _, ok = Parse2FAChallengeToken("not-a-jwt", cfg)
	assert.False(t, ok)

	// A regular session token (no purpose claim) is NOT a challenge.
	sessionCfg := &config.Config{JWTSecretKey: twoFactorTestSecret, JWTExpiryHours: 24}
	sessionToken, err := GenerateToken(user, sessionCfg, "test-sid")
	require.NoError(t, err)
	_, _, ok = Parse2FAChallengeToken(sessionToken, cfg)
	assert.False(t, ok)
}

func TestTwoFactorChallengeTokenExpired(t *testing.T) {
	cfg := &config.Config{JWTSecretKey: twoFactorTestSecret}
	user := models.User{Username: "alice"}
	user.ID = 42

	// Forge an already-expired 2fa-purpose token with the same signing key —
	// the only way to control the exp claim without waiting out the TTL.
	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"authorized": true,
		"username":   user.Username,
		"user_id":    user.ID,
		"purpose":    "2fa",
		"exp":        time.Now().Add(-2 * time.Minute).Unix(),
	})
	token, err := expired.SignedString([]byte(twoFactorTestSecret))
	require.NoError(t, err)

	_, _, ok := Parse2FAChallengeToken(token, cfg)
	assert.False(t, ok, "an expired 2FA challenge must not complete a login")
}

func TestRecoveryCodeNormalization(t *testing.T) {
	code := "ABC12-DEF34-GHI56"

	// Case- and dash-insensitive: however the user retypes the code, the
	// stored hash and the login-time comparison agree.
	for _, input := range []string{
		code,
		"abc12-def34-ghi56",
		"ABC12DEF34GHI56",
		"  abc12 def34 ghi56 ",
	} {
		assert.Equal(t, HashRecoveryCode(code), HashRecoveryCode(input),
			"hash must be stable across formatting: %q", input)
	}
}
