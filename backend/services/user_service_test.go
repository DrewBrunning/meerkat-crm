package services

import (
	"mycorrhizal/config"
	"mycorrhizal/models"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	password := "mypassword123"

	hashedPassword, err := HashPassword(password)

	assert.NoError(t, err)
	assert.NotEmpty(t, hashedPassword)

	// Verify that the hashed password matches the original password
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	assert.NoError(t, err)
}

func TestHashPassword_Error(t *testing.T) {
	// Simulate an error by providing an empty password
	_, err := HashPassword("")

	assert.Error(t, err)
}

func TestSpendDummyPasswordHash(t *testing.T) {
	// Issue #862: the dummy hash must cost the same as a real account hash, or
	// the timing equalization is pointless. HashPassword uses bcrypt.DefaultCost.
	cost, err := bcrypt.Cost(dummyBcryptHash)
	assert.NoError(t, err, "dummyBcryptHash must be a valid bcrypt hash")
	assert.Equal(t, bcrypt.DefaultCost, cost, "dummy hash cost must match HashPassword's")

	// The dummy comparison must never succeed (it would be a login bypass on the
	// not-found branch if the attacker could ever guess the throwaway plaintext)
	// and must not panic on any input shape a login handler can hand it.
	assert.NotPanics(t, func() {
		SpendDummyPasswordHash("")
		SpendDummyPasswordHash("an-ordinary-password")
		SpendDummyPasswordHash(string(make([]byte, 200))) // > bcrypt's 72-byte cap
	})
	assert.Error(t, bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte("an-ordinary-password")))
}

func TestGenerateToken(t *testing.T) {
	config := config.Config{
		JWTSecretKey:   "mysecretkey",
		JWTExpiryHours: 24,
	}

	user := models.User{
		Username: "testuser",
	}

	tokenString, err := GenerateToken(user, &config, "test-sid")
	assert.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte(config.JWTSecretKey), nil
	})
	assert.NoError(t, err)
	assert.True(t, token.Valid)

	claims, ok := token.Claims.(jwt.MapClaims)
	assert.True(t, ok)
	assert.Equal(t, user.Username, claims["username"])
	assert.True(t, claims["authorized"].(bool)) // Check if claims authorize is true
	assert.Equal(t, "test-sid", claims["sid"])  // issue #866: session id claim
}

func TestGenerateToken_Error(t *testing.T) {
	config := config.Config{
		JWTSecretKey: "",
	}

	user := models.User{
		Username: "testuser",
	}

	// Attempts to generate a token should fail
	_, err := GenerateToken(user, &config, "test-sid")

	assert.Error(t, err)
}
