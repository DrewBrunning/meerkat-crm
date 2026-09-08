package controllers

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // test double mirroring HIBP's own k-anonymity wire format, see newHIBPServer
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	strongPassword        = "CorrectHorseBattery42!"
	strongPasswordAlt     = "TrulySecurePassphrase99#"
	strongPasswordAnother = "UltraSafePassphrase88$"
)

func TestRegisterUser(t *testing.T) {
	_, router := setupRouter()
	cfg := &config.Config{}
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))

	// Create a new user using the registration DTO
	newUser := models.UserRegistrationInput{
		Username: "testuser",
		Email:    "testuser@example.com",
		Password: strongPassword,
	}

	jsonValue, _ := json.Marshal(newUser)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "User registered successfully", responseBody["message"])
}

func TestRegisterUser_InvalidInput(t *testing.T) {
	_, router := setupRouter()
	cfg := &config.Config{}
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))

	// Invalid input (no email)
	invalidUser := models.UserRegistrationInput{
		Username: "invaliduser",
		Password: strongPassword,
	}

	jsonValue, _ := json.Marshal(invalidUser)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "VALIDATION_ERROR", errorDetail["code"])
}

func TestLoginUser(t *testing.T) {
	config := config.Config{
		JWTSecretKey:   "mysecretkey",
		JWTExpiryHours: 24,
	}

	db, router := setupRouter()
	router.POST("/login", func(c *gin.Context) {
		LoginUser(c, &config)
	})

	// First, register a user to test login
	newUser := models.User{
		Username: "testuser",
		Email:    "testuser@example.com",
		Password: strongPassword,
	}
	hashedPassword, _ := services.HashPassword(newUser.Password)
	newUser.Password = hashedPassword
	db.Create(&newUser)

	// Now try to login with email
	loginData := map[string]string{
		"identifier": "testuser@example.com",
		"password":   strongPassword,
	}

	jsonValue, _ := json.Marshal(loginData)
	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Contains(t, responseBody, "language")
	assert.Contains(t, responseBody, "date_format")
	assert.NotEmpty(t, w.Result().Cookies())
	var authCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "auth_token" {
			authCookie = c
			break
		}
	}
	assert.NotNil(t, authCookie, "auth_token cookie should be set")
	assert.NotEmpty(t, authCookie.Value)
}

func TestLoginUser_WithUsername(t *testing.T) {
	config := config.Config{
		JWTSecretKey:   "mysecretkey",
		JWTExpiryHours: 24,
	}

	db, router := setupRouter()
	router.POST("/login", func(c *gin.Context) {
		LoginUser(c, &config)
	})

	// First, register a user to test login
	newUser := models.User{
		Username: "testuser",
		Email:    "testuser@example.com",
		Password: strongPassword,
	}
	hashedPassword, _ := services.HashPassword(newUser.Password)
	newUser.Password = hashedPassword
	db.Create(&newUser)

	// Now try to login with username
	loginData := map[string]string{
		"identifier": "testuser",
		"password":   strongPassword,
	}

	jsonValue, _ := json.Marshal(loginData)
	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Contains(t, responseBody, "language")
	assert.Contains(t, responseBody, "date_format")
	var authCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "auth_token" {
			authCookie = c
			break
		}
	}
	assert.NotNil(t, authCookie, "auth_token cookie should be set")
	assert.NotEmpty(t, authCookie.Value)
}

func TestLoginUser_LegacyEmailField(t *testing.T) {
	config := config.Config{
		JWTSecretKey:   "mysecretkey",
		JWTExpiryHours: 24,
	}

	db, router := setupRouter()
	router.POST("/login", func(c *gin.Context) {
		LoginUser(c, &config)
	})

	// First, register a user to test login
	newUser := models.User{
		Username: "testuser",
		Email:    "testuser@example.com",
		Password: strongPassword,
	}
	hashedPassword, _ := services.HashPassword(newUser.Password)
	newUser.Password = hashedPassword
	db.Create(&newUser)

	// Now try to login with legacy email field (backward compatibility)
	loginData := map[string]string{
		"email":    "testuser@example.com",
		"password": strongPassword,
	}

	jsonValue, _ := json.Marshal(loginData)
	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Contains(t, responseBody, "language")
	assert.Contains(t, responseBody, "date_format")
	var authCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "auth_token" {
			authCookie = c
			break
		}
	}
	assert.NotNil(t, authCookie, "auth_token cookie should be set")
	assert.NotEmpty(t, authCookie.Value)
}

func TestLoginUser_InvalidCredentials(t *testing.T) {
	config := config.Config{
		JWTSecretKey:   "mysecretkey",
		JWTExpiryHours: 24,
	}
	_, router := setupRouter()
	router.POST("/login", func(c *gin.Context) {
		LoginUser(c, &config)
	})
	// Try to login with unregistered user
	loginData := map[string]string{
		"identifier": "wronguser@example.com",
		"password":   "wrongpassword",
	}

	jsonValue, _ := json.Marshal(loginData)
	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "INVALID_CREDENTIALS", errorDetail["code"])
}

func TestLoginUser_InvalidInput(t *testing.T) {
	config := config.Config{
		JWTSecretKey: "mysecretkey",
	}
	_, router := setupRouter()
	router.POST("/login", func(c *gin.Context) {
		LoginUser(c, &config)
	})

	// Trying to login with invalid input (missing identifier)
	invalidData := map[string]string{
		"password": strongPassword,
	}

	jsonValue, _ := json.Marshal(invalidData)
	req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "MISSING_FIELD", errorDetail["code"])
}

func TestRequestPasswordReset_Succeeds(t *testing.T) {
	cfg := config.Config{
		FrontendURL: "http://localhost:3000",
		UseResend:   false,
	}

	db, router := setupRouter()

	hashed, _ := services.HashPassword(strongPassword)
	user := models.User{
		Username: "resetuser",
		Email:    "reset@example.com",
		Password: hashed,
	}
	db.Create(&user)

	router.POST("/password-reset/request", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetRequestInput{Email: "reset@example.com"})
		RequestPasswordReset(c, &cfg)
	})

	req, _ := http.NewRequest("POST", "/password-reset/request", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updated models.User
	db.Where("email = ?", "reset@example.com").First(&updated)
	if assert.NotNil(t, updated.PasswordResetTokenHash) {
		assert.NotEmpty(t, *updated.PasswordResetTokenHash)
	}
	if assert.NotNil(t, updated.PasswordResetExpiresAt) {
		assert.True(t, updated.PasswordResetExpiresAt.After(time.Now().Add(-time.Minute)))
	}
	if assert.NotNil(t, updated.PasswordResetRequestedAt) {
		assert.True(t, updated.PasswordResetRequestedAt.After(time.Now().Add(-time.Minute)))
	}
}

func TestConfirmPasswordReset_Succeeds(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()

	user := models.User{
		Username:                 "confirmuser",
		Email:                    "confirm@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
	}
	db.Create(&user)

	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updated models.User
	db.Where("email = ?", "confirm@example.com").First(&updated)
	assert.Nil(t, updated.PasswordResetTokenHash)
	assert.Nil(t, updated.PasswordResetExpiresAt)
	assert.Nil(t, updated.PasswordResetRequestedAt)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte(strongPasswordAlt)))
}

func TestChangePassword_Succeeds(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	user := models.User{
		Username: "changeme",
		Email:    "change@example.com",
		Password: initialPassword,
	}
	db.Create(&user)

	router.POST("/change-password", func(c *gin.Context) {
		c.Set("username", "changeme")
		c.Set("validated", &models.ChangePasswordInput{
			CurrentPassword: strongPassword,
			NewPassword:     strongPasswordAnother,
		})
		ChangePassword(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/change-password", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updated models.User
	db.Where("username = ?", "changeme").First(&updated)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte(strongPasswordAnother)))
	assert.Nil(t, updated.PasswordResetTokenHash)
	assert.Nil(t, updated.PasswordResetExpiresAt)
	assert.Nil(t, updated.PasswordResetRequestedAt)
}

// --- HIBP breach check gating (issue #376) -------------------------------
//
// newHIBPServer builds an httptest.Server standing in for the real HIBP
// range API, reporting exactly one password as breached: whichever one the
// caller names. It computes the k-anonymity prefix/suffix with the same
// crypto/sha1 call hibp_service.go itself uses — hashing correctness is
// already pinned independently by
// TestCheckPasswordBreached_KnownBreachedPassword in
// services/hibp_service_test.go; these tests are only about the
// controller-level wiring (gating on cfg.HIBPCheckEnabled, 400 on breach,
// pass-through otherwise).
func newHIBPServer(t *testing.T, breachedPassword string) *httptest.Server {
	t.Helper()
	sum := sha1.Sum([]byte(breachedPassword)) //nolint:gosec // test double mirroring HIBP's own wire format
	hex := strings.ToUpper(fmt.Sprintf("%x", sum))
	prefix, suffix := hex[:5], hex[5:]
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.URL.Path, "/range/") == prefix {
			fmt.Fprintf(w, "%s:1\r\n", suffix)
			return
		}
		fmt.Fprint(w, "0000000000000000000000000000000000:1\r\n")
	}))
}

func TestRegisterUser_HIBPCheckEnabled_RejectsBreachedPassword(t *testing.T) {
	server := newHIBPServer(t, strongPassword)
	defer server.Close()
	t.Cleanup(services.SetHIBPAPIBaseURLForTest(server.URL))

	_, router := setupRouter()
	cfg := &config.Config{HIBPCheckEnabled: true}
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))

	newUser := models.UserRegistrationInput{Username: "breacheduser", Email: "breached@example.com", Password: strongPassword}
	jsonValue, _ := json.Marshal(newUser)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterUser_HIBPCheckEnabled_AllowsCleanPassword(t *testing.T) {
	// Server only reports strongPasswordAlt as breached; the registration
	// below uses a different password, so it must succeed.
	server := newHIBPServer(t, strongPasswordAlt)
	defer server.Close()
	t.Cleanup(services.SetHIBPAPIBaseURLForTest(server.URL))

	_, router := setupRouter()
	cfg := &config.Config{HIBPCheckEnabled: true}
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))

	newUser := models.UserRegistrationInput{Username: "cleanuser", Email: "clean@example.com", Password: strongPassword}
	jsonValue, _ := json.Marshal(newUser)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRegisterUser_HIBPCheckDisabled_SkipsBreachedPassword(t *testing.T) {
	// Default (HIBPCheckEnabled: false, the zero value): a known-breached
	// password must still succeed, and no HIBP call is ever attempted —
	// hibpAPIBaseURL is left at its real default, which would fail/hang if
	// this test actually reached it. If registration returns 201 here, the
	// check was skipped as intended.
	_, router := setupRouter()
	cfg := &config.Config{}
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))

	newUser := models.UserRegistrationInput{Username: "defaultuser", Email: "default@example.com", Password: strongPassword}
	jsonValue, _ := json.Marshal(newUser)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestChangePassword_HIBPCheckEnabled_RejectsBreachedPassword(t *testing.T) {
	server := newHIBPServer(t, strongPasswordAnother)
	defer server.Close()
	t.Cleanup(services.SetHIBPAPIBaseURLForTest(server.URL))

	db, router := setupRouter()
	cfg := &config.Config{HIBPCheckEnabled: true}

	initialPassword, _ := services.HashPassword(strongPassword)
	user := models.User{Username: "changebreached", Email: "changebreached@example.com", Password: initialPassword}
	db.Create(&user)

	router.POST("/change-password", func(c *gin.Context) {
		c.Set("username", "changebreached")
		c.Set("validated", &models.ChangePasswordInput{
			CurrentPassword: strongPassword,
			NewPassword:     strongPasswordAnother,
		})
		ChangePassword(c, cfg)
	})

	req, _ := http.NewRequest("POST", "/change-password", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var updated models.User
	db.Where("username = ?", "changebreached").First(&updated)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte(strongPassword)),
		"password must be unchanged after a rejected breach check")
}

func TestConfirmPasswordReset_HIBPCheckEnabled_RejectsBreachedPassword(t *testing.T) {
	server := newHIBPServer(t, strongPasswordAlt)
	defer server.Close()
	t.Cleanup(services.SetHIBPAPIBaseURLForTest(server.URL))

	db, router := setupRouter()
	cfg := &config.Config{HIBPCheckEnabled: true}

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()

	user := models.User{
		Username:                 "resetbreached",
		Email:                    "resetbreached@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
	}
	db.Create(&user)

	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c, cfg)
	})

	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var updated models.User
	db.Where("username = ?", "resetbreached").First(&updated)
	assert.NotNil(t, updated.PasswordResetTokenHash, "reset token must survive a rejected breach check, not be consumed")
}

// TestRequestPasswordReset_UnknownEmail_SameResponseAsKnown pins issue #411's
// account-enumeration-resistance requirement: /password-reset/request must
// return the identical status and body regardless of whether the email
// belongs to a real account.
func TestRequestPasswordReset_UnknownEmail_SameResponseAsKnown(t *testing.T) {
	cfg := &config.Config{}
	db, router := setupRouter()

	hashed, _ := services.HashPassword(strongPassword)
	db.Create(&models.User{
		Username: "knownuser",
		Email:    "known@example.com",
		Password: hashed,
	})

	router.POST("/password-reset/request", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetRequestInput{Email: c.Query("email")})
		RequestPasswordReset(c, cfg)
	})

	doRequest := func(email string) (int, string) {
		req, _ := http.NewRequest("POST", "/password-reset/request?email="+email, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	knownStatus, knownBody := doRequest("known@example.com")
	unknownStatus, unknownBody := doRequest("nobody-here@example.com")

	assert.Equal(t, http.StatusOK, knownStatus)
	assert.Equal(t, knownStatus, unknownStatus, "known/unknown email must not be distinguishable by status code")
	assert.Equal(t, knownBody, unknownBody, "known/unknown email must not be distinguishable by response body")
}

// TestConfirmPasswordReset_RejectsSecondUse pins the single-use invariant: a
// reset token is consumed on first confirm, so replaying it must fail and
// must not touch the password set by the first confirm.
func TestConfirmPasswordReset_RejectsSecondUse(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()

	user := models.User{
		Username:                 "reusetoken",
		Email:                    "reusetoken@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
	}
	db.Create(&user)

	var confirmPassword string
	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: confirmPassword})
		ConfirmPasswordReset(c, &config.Config{})
	})

	// First use succeeds.
	confirmPassword = strongPasswordAlt
	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Replaying the same token must fail.
	confirmPassword = strongPasswordAnother
	req, _ = http.NewRequest("POST", "/password-reset/confirm", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var updated models.User
	db.Where("email = ?", "reusetoken@example.com").First(&updated)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte(strongPasswordAlt)),
		"password must still be what the first (legitimate) use set")
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte(strongPasswordAnother)),
		"the replayed token's password must never have taken effect")
}

// TestConfirmPasswordReset_RejectsExpiredToken pins the TTL invariant: a
// token past its expiry is rejected and cleared, not honored.
func TestConfirmPasswordReset_RejectsExpiredToken(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expired := time.Now().Add(-time.Minute)
	requested := time.Now().Add(-2 * time.Hour)

	user := models.User{
		Username:                 "expiredtoken",
		Email:                    "expiredtoken@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expired,
		PasswordResetRequestedAt: &requested,
	}
	db.Create(&user)

	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var updated models.User
	db.Where("email = ?", "expiredtoken@example.com").First(&updated)
	assert.Nil(t, updated.PasswordResetTokenHash, "expired token must be cleared, not left around for a later guess")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte(strongPassword)),
		"password must be unchanged")
}

// TestConfirmPasswordReset_BumpsTokenVersion pins session invalidation: a
// successful reset must bump TokenVersion, which is what makes every
// previously-issued JWT stop validating (middleware/auth_lifecycle_test.go
// covers the middleware side of that contract).
func TestConfirmPasswordReset_BumpsTokenVersion(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()

	user := models.User{
		Username:                 "versionbump",
		Email:                    "versionbump@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
		TokenVersion:             3,
	}
	db.Create(&user)

	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var updated models.User
	db.Where("email = ?", "versionbump@example.com").First(&updated)
	assert.EqualValues(t, 4, updated.TokenVersion, "reset must bump TokenVersion to invalidate outstanding JWTs")
}

// TestConfirmPasswordReset_RevokesExistingAPITokens pins issue #411's
// credential-invalidation requirement: a reset is the recovery path for a
// suspected compromise, so standing API tokens (which carry no TokenVersion
// of their own) must be revoked too, not just JWTs.
func TestConfirmPasswordReset_RevokesExistingAPITokens(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()

	user := models.User{
		Username:                 "revoketokens",
		Email:                    "revoketokens@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
	}
	db.Create(&user)

	live := models.ApiToken{UserID: user.ID, Name: "live", TokenHash: "livehash"}
	require.NoError(t, db.Create(&live).Error)
	already := models.ApiToken{UserID: user.ID, Name: "already-revoked", TokenHash: "revokedhash"}
	require.NoError(t, db.Create(&already).Error)
	priorRevoke := time.Now().Add(-24 * time.Hour)
	require.NoError(t, db.Model(&already).Update("revoked_at", priorRevoke).Error)

	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var updatedLive models.ApiToken
	require.NoError(t, db.First(&updatedLive, live.ID).Error)
	assert.NotNil(t, updatedLive.RevokedAt, "a live API token must be revoked by a password reset")

	var updatedAlready models.ApiToken
	require.NoError(t, db.First(&updatedAlready, already.ID).Error)
	require.NotNil(t, updatedAlready.RevokedAt)
	assert.WithinDuration(t, priorRevoke, *updatedAlready.RevokedAt, time.Second,
		"an already-revoked token's original revocation time must not be overwritten")
}

// TestConfirmPasswordReset_DoesNotDisableTOTP pins the MFA-interaction
// invariant: a password reset (an email-based recovery flow) must not turn
// off an enrolled second factor -- doing so would let compromised email
// access alone strip 2FA protection from the account.
func TestConfirmPasswordReset_DoesNotDisableTOTP(t *testing.T) {
	db, router := setupRouter()

	initialPassword, _ := services.HashPassword(strongPassword)
	token, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()
	secret := "encrypted-totp-secret-placeholder"
	confirmedAt := time.Now().Add(-24 * time.Hour)

	user := models.User{
		Username:                 "totpsurvives",
		Email:                    "totpsurvives@example.com",
		Password:                 initialPassword,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
		TOTPEnabled:              true,
		TOTPSecretEncrypted:      &secret,
		TOTPConfirmedAt:          &confirmedAt,
	}
	db.Create(&user)

	router.POST("/password-reset/confirm", func(c *gin.Context) {
		c.Set("validated", &models.PasswordResetConfirmInput{Token: token, Password: strongPasswordAlt})
		ConfirmPasswordReset(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/password-reset/confirm", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var updated models.User
	db.Where("email = ?", "totpsurvives@example.com").First(&updated)
	assert.True(t, updated.TOTPEnabled, "2FA must remain enabled across a password reset")
	if assert.NotNil(t, updated.TOTPSecretEncrypted) {
		assert.Equal(t, secret, *updated.TOTPSecretEncrypted)
	}
}

// TestChangePassword_ClearsPendingPasswordResetToken pins the invalidation
// invariant from the other direction: an unrelated password change (the
// authenticated, "I know my current password" path) must invalidate any
// reset token still outstanding for the account, so a stale token an
// attacker requested earlier can't later be used to reset the password the
// legitimate owner just changed.
func TestChangePassword_ClearsPendingPasswordResetToken(t *testing.T) {
	db, router := setupRouter()

	hashed, _ := services.HashPassword(strongPassword)
	_, tokenHash, _ := services.GeneratePasswordResetToken()
	expires := services.PasswordResetExpiry()
	requested := time.Now()

	user := models.User{
		Username:                 "pendingreset",
		Email:                    "pendingreset@example.com",
		Password:                 hashed,
		PasswordResetTokenHash:   &tokenHash,
		PasswordResetExpiresAt:   &expires,
		PasswordResetRequestedAt: &requested,
	}
	db.Create(&user)

	router.POST("/change-password", func(c *gin.Context) {
		c.Set("username", "pendingreset")
		c.Set("validated", &models.ChangePasswordInput{
			CurrentPassword: strongPassword,
			NewPassword:     strongPasswordAnother,
		})
		ChangePassword(c, &config.Config{})
	})

	req, _ := http.NewRequest("POST", "/change-password", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var updated models.User
	db.Where("email = ?", "pendingreset@example.com").First(&updated)
	assert.Nil(t, updated.PasswordResetTokenHash, "a self-service password change must invalidate any pending reset token")
	assert.Nil(t, updated.PasswordResetExpiresAt)
	assert.Nil(t, updated.PasswordResetRequestedAt)
}

// TestEnabledContactFieldsNullVsEmpty verifies the GET/PATCH endpoints preserve the
// distinction between "never configured" (null -> client applies defaults) and
// "configured empty" ([] -> no extended fields), and that the scoped column write
// round-trips concrete values.
func TestEnabledContactFieldsNullVsEmpty(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.GET("/enabled", GetEnabledContactFields)
	router.PATCH("/enabled",
		middleware.ValidateJSONMiddleware(&models.EnabledContactFieldsInput{}),
		UpdateEnabledContactFields)

	rawField := func() string {
		req, _ := http.NewRequest("GET", "/enabled", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]json.RawMessage
		json.Unmarshal(w.Body.Bytes(), &body)
		return string(body["enabled_contact_fields"])
	}

	patch := func(jsonBody string) {
		req, _ := http.NewRequest("PATCH", "/enabled", bytes.NewBufferString(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Never configured -> null (client applies its defaults).
	assert.Equal(t, "null", rawField())

	// Concrete values round-trip.
	patch(`{"fields":["emails","phones"]}`)
	assert.JSONEq(t, `["emails","phones"]`, rawField())

	// Explicitly empty stays distinct from null.
	patch(`{"fields":[]}`)
	assert.Equal(t, "[]", rawField())
}

func TestCheckPasswordStrength_WeakPassword(t *testing.T) {
	_, router := setupRouter()
	router.POST("/password-strength", CheckPasswordStrength)

	jsonValue, _ := json.Marshal(map[string]string{"password": "abc"})
	req, _ := http.NewRequest("POST", "/password-strength", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var strength middleware.PasswordStrength
	json.Unmarshal(w.Body.Bytes(), &strength)
	assert.False(t, strength.IsValid)
	assert.Equal(t, 0, strength.Score)
	assert.Equal(t, "Password must be at least 8 characters long.", strength.Feedback)
}

func TestCheckPasswordStrength_StrongPassword(t *testing.T) {
	_, router := setupRouter()
	router.POST("/password-strength", CheckPasswordStrength)

	jsonValue, _ := json.Marshal(map[string]string{"password": strongPassword})
	req, _ := http.NewRequest("POST", "/password-strength", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var strength middleware.PasswordStrength
	json.Unmarshal(w.Body.Bytes(), &strength)
	assert.True(t, strength.IsValid)
	assert.GreaterOrEqual(t, strength.Score, 3)
	assert.GreaterOrEqual(t, strength.Entropy, middleware.MinEntropyBits)
}

func TestCheckPasswordStrength_MissingPassword(t *testing.T) {
	_, router := setupRouter()
	router.POST("/password-strength", CheckPasswordStrength)

	req, _ := http.NewRequest("POST", "/password-strength", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "MISSING_FIELD", errorDetail["code"])
}

func TestUpdateLanguage_Succeeds(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/language", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateLanguage(c)
	})

	req, _ := http.NewRequest("PATCH", "/language", bytes.NewBufferString(`{"language":"de"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "de", responseBody["language"])

	var updated models.User
	db.First(&updated, user.ID)
	assert.Equal(t, "de", updated.Language)
}

// TestUpdateLanguage_RejectsUnsupportedCode is the regression test:
// i18n.IsValidLanguage used to normalize via i18n.normalizeLanguage, which
// falls back to "en" for any unrecognized input, so the rejection branch
// below was unreachable for any input at all. i18n.NormalizeSupportedLanguage
// (i18n/i18n.go) fixes this by never falling back -- a genuinely unsupported
// code must now be rejected.
func TestUpdateLanguage_RejectsUnsupportedCode(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/language", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateLanguage(c)
	})

	req, _ := http.NewRequest("PATCH", "/language", bytes.NewBufferString(`{"language":"xx"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "INVALID_INPUT", errorDetail["code"])

	// The garbage value must not have been persisted either.
	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	assert.NotEqual(t, "xx", reloaded.Language)
}

// TestUpdateLanguage_NormalizesBeforePersisting is the other half:
// UpdateLanguage used to persist input.Language raw/unnormalized even when
// valid, so e.g. "DE-AT" would be stored verbatim instead of as "de".
func TestUpdateLanguage_NormalizesBeforePersisting(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/language", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateLanguage(c)
	})

	req, _ := http.NewRequest("PATCH", "/language", bytes.NewBufferString(`{"language":"DE-AT"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "de", responseBody["language"])

	var updated models.User
	require.NoError(t, db.First(&updated, user.ID).Error)
	assert.Equal(t, "de", updated.Language)
}

// TestUpdateLanguage_RejectsMalformedJSON exercises UpdateLanguage's bind-failure
// branch.
func TestUpdateLanguage_RejectsMalformedJSON(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/language", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateLanguage(c)
	})

	req, _ := http.NewRequest("PATCH", "/language", bytes.NewBufferString(`{"language":123}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "INVALID_INPUT", errorDetail["code"])
}

func TestUpdateLanguage_RequiresAuth(t *testing.T) {
	_, router := setupRouter()
	// setupRouter's shared middleware sets "userID" but not "username", so
	// mounting UpdateLanguage directly exercises the unauthenticated path.
	router.PATCH("/language", UpdateLanguage)

	req, _ := http.NewRequest("PATCH", "/language", bytes.NewBufferString(`{"language":"de"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateDateFormat_Succeeds(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/date-format", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateDateFormat(c)
	})

	req, _ := http.NewRequest("PATCH", "/date-format", bytes.NewBufferString(`{"date_format":"iso"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "iso", responseBody["date_format"])

	var updated models.User
	db.First(&updated, user.ID)
	assert.Equal(t, "iso", updated.DateFormat)
}

func TestUpdateDateFormat_AcceptsNewFormats(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/date-format", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateDateFormat(c)
	})

	for _, format := range []string{"ca", "eu-hyphen", "us-mmm", "us-mmmm", "eu-mmm", "eu-mmmm"} {
		req, _ := http.NewRequest("PATCH", "/date-format", bytes.NewBufferString(`{"date_format":"`+format+`"}`))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "format %s should be accepted", format)

		var updated models.User
		db.First(&updated, user.ID)
		assert.Equal(t, format, updated.DateFormat, "format %s should be persisted", format)
	}
}

func TestUpdateDateFormat_RejectsUnsupportedFormat(t *testing.T) {
	db, router := setupRouter()
	var user models.User
	db.First(&user)

	router.PATCH("/date-format", func(c *gin.Context) {
		c.Set("username", user.Username)
		UpdateDateFormat(c)
	})

	req, _ := http.NewRequest("PATCH", "/date-format", bytes.NewBufferString(`{"date_format":"dd/mm"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	errorDetail := response["error"].(map[string]interface{})
	assert.Equal(t, "INVALID_INPUT", errorDetail["code"])
}

func TestUpdateDateFormat_RequiresAuth(t *testing.T) {
	_, router := setupRouter()
	router.PATCH("/date-format", UpdateDateFormat)

	req, _ := http.NewRequest("PATCH", "/date-format", bytes.NewBufferString(`{"date_format":"iso"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- registration behaviour (issue #558: "registration behaviour is documented
// and tested") ------------------------------------------------------------------
//
// These use the real migrated schema (dbtest, per CLAUDE.md backend trap #1)
// because they assert on the persisted users.is_admin column and on the row
// count, not just the HTTP status.

func newRegisterRouter(t *testing.T, cfg *config.Config) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	db := dbtest.New(t)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	router.POST("/register", middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), RegisterUser(cfg))
	return db, router
}

func postRegister(t *testing.T, router *gin.Engine, in models.UserRegistrationInput) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRegisterUser_RegistrationDisabled(t *testing.T) {
	db, router := newRegisterRouter(t, &config.Config{RegistrationDisabled: true})

	w := postRegister(t, router, models.UserRegistrationInput{
		Username: "blocked",
		Email:    "blocked@example.com",
		Password: strongPassword,
	})

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp map[string]map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "registration_disabled", resp["error"]["code"])

	var count int64
	require.NoError(t, db.Model(&models.User{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "a disabled registration must not create a user row")
}

func TestRegisterUser_FirstUserBecomesAdmin(t *testing.T) {
	db, router := newRegisterRouter(t, &config.Config{})

	w := postRegister(t, router, models.UserRegistrationInput{
		Username: "founder",
		Email:    "founder@example.com",
		Password: strongPassword,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var user models.User
	require.NoError(t, db.Where("username = ?", "founder").First(&user).Error)
	assert.True(t, user.IsAdmin, "the first registered account must be an admin")
}

func TestRegisterUser_SubsequentUsersAreNotAdmin(t *testing.T) {
	db, router := newRegisterRouter(t, &config.Config{})

	require.Equal(t, http.StatusCreated, postRegister(t, router, models.UserRegistrationInput{
		Username: "first", Email: "first@example.com", Password: strongPassword,
	}).Code)

	w := postRegister(t, router, models.UserRegistrationInput{
		Username: "second", Email: "second@example.com", Password: strongPasswordAlt,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var first, second models.User
	require.NoError(t, db.Where("username = ?", "first").First(&first).Error)
	require.NoError(t, db.Where("username = ?", "second").First(&second).Error)
	assert.True(t, first.IsAdmin, "the first registered account must be an admin")
	assert.False(t, second.IsAdmin, "every account after the first must be a normal user")

	// The registration DTO must not let a caller grant itself admin.
	body := []byte(`{"username":"sneaky","email":"sneaky@example.com","password":"` + strongPasswordAnother + `","is_admin":true}`)
	req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var sneaky models.User
	require.NoError(t, db.Where("username = ?", "sneaky").First(&sneaky).Error)
	assert.False(t, sneaky.IsAdmin, "is_admin in the registration body must be ignored (no mass assignment)")
}
