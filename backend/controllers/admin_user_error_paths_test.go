package controllers

import (
	"bytes"
	"mycorrhizal/attachments"
	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// adminRouter builds a router with db set and an optional userID, so the
// handler-level 401 branches and missing-validation branches are reachable.
func adminRouter(db *gorm.DB, withUserID bool) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		if withUserID {
			c.Set("userID", uint(1))
		}
		c.Set("cfg", config.Config{})
		c.Next()
	})
	return router
}

// --- GetCurrentUser / UpdateSelfContact 401 + validation branches ---

func TestGetCurrentUser_Unauthenticated(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, false)
	router.GET("/users/me", GetCurrentUser)
	req, _ := http.NewRequest(http.MethodGet, "/users/me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestUpdateSelfContact_Unauthenticated(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, false)
	router.PUT("/users/me/self-contact", withValidated(func() any { return &models.SelfContactInput{} }), UpdateSelfContact)
	req, _ := http.NewRequest(http.MethodPut, "/users/me/self-contact", bytes.NewBufferString(`{"vcard_uid":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestUpdateSelfContact_MissingValidation(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, true)
	// No validation middleware: GetValidated must reject with 400.
	router.PUT("/users/me/self-contact", UpdateSelfContact)
	req, _ := http.NewRequest(http.MethodPut, "/users/me/self-contact", bytes.NewBufferString(`{"vcard_uid":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestUpdateSelfContact_ClearDatabaseError(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "selfcleardberr", Password: "password123!A", Email: "selfcleardberr@example.com"}
	require.NoError(t, db.Create(&user).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) { c.Set("db", db); c.Set("userID", user.ID); c.Next() })
	router.PUT("/users/me/self-contact", withValidated(func() any { return &models.SelfContactInput{} }), UpdateSelfContact)

	req, _ := http.NewRequest(http.MethodPut, "/users/me/self-contact", bytes.NewBufferString(`{"vcard_uid":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- CreateUser branches ---

func TestCreateUser_Unauthenticated(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, false)
	router.POST("/users", withValidated(func() any { return &models.AdminUserCreateInput{} }), CreateUser)
	req, _ := http.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(`{"username":"u","email":"u@example.com","password":"brandNewPassw0rd!"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestCreateUser_MissingValidation(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, true)
	router.POST("/users", CreateUser)
	req, _ := http.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(`{"username":"u","email":"u@example.com","password":"brandNewPassw0rd!"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestCreateUser_OverlongPasswordRejected(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, true)
	router.POST("/users", withValidated(func() any { return &models.AdminUserCreateInput{} }), CreateUser)

	long := strings.Repeat("a", 73)
	req, _ := http.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(`{"username":"u","email":"u@example.com","password":"`+long+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestCreateUser_DatabaseError(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "createdberr", Password: "password123!A", Email: "createdberr@example.com"}
	require.NoError(t, db.Create(&user).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) { c.Set("db", db); c.Set("userID", user.ID); c.Next() })
	router.POST("/users", withValidated(func() any { return &models.AdminUserCreateInput{} }), CreateUser)

	req, _ := http.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(`{"username":"u","email":"u@example.com","password":"brandNewPassw0rd!"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// --- ResetUserTwoFactor branches ---

func TestResetUserTwoFactor_Unauthenticated(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, false)
	router.POST("/users/:id/reset-2fa", ResetUserTwoFactor)
	req, _ := http.NewRequest(http.MethodPost, "/users/1/reset-2fa", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// --- DeleteUser branches ---

func TestDeleteUser_Unauthenticated(t *testing.T) {
	db := dbtest.New(t)
	router := adminRouter(db, false)
	router.DELETE("/users/:id", DeleteUser)
	req, _ := http.NewRequest(http.MethodDelete, "/users/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// TestDeleteUser_ReportsPluckFailure covers the attachment-name capture step:
// when loading the user's stored names fails, DeleteUser must 500 before
// deleting anything.
func TestDeleteUser_ReportsPluckFailure(t *testing.T) {
	db := dbtest.New(t)
	actor := models.User{Username: "pluckadmin", Password: "password123!A", Email: "pluckadmin@example.com"}
	require.NoError(t, db.Create(&actor).Error)
	target := models.User{Username: "plucktarget", Password: "password123!A", Email: "plucktarget@example.com"}
	require.NoError(t, db.Create(&target).Error)

	// Break the attachments table so the Pluck fails after the user lookup.
	require.NoError(t, db.Exec("DROP TABLE attachments").Error)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", actor.ID)
		c.Set("cfg", config.Config{})
		c.Next()
	})
	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest(http.MethodDelete, "/users/"+strconv.Itoa(int(target.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	// The user must survive a failed cleanup capture.
	var count int64
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", target.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count, "a Pluck failure must abort before deleting the user")
}

// TestDeleteUser_ReportsTransactionFailure covers the cascade transaction's
// error path: a mid-transaction delete failure must abort the whole cascade
// (rolling back every earlier delete) and 500 — the user row survives.
func TestDeleteUser_ReportsTransactionFailure(t *testing.T) {
	db := dbtest.New(t)
	actor := models.User{Username: "txadmin", Password: "password123!A", Email: "txadmin@example.com"}
	require.NoError(t, db.Create(&actor).Error)
	target := models.User{Username: "txtarget", Password: "password123!A", Email: "txtarget@example.com"}
	require.NoError(t, db.Create(&target).Error)
	contact := models.Contact{UserID: target.ID, Firstname: "C"}
	require.NoError(t, db.Create(&contact).Error)
	activity := models.Activity{UserID: target.ID, Title: "call", Type: "call", Date: time.Now()}
	require.NoError(t, db.Create(&activity).Error)
	require.NoError(t, db.Model(&activity).Association("Contacts").Append(&contact))

	// Dropping activity_contacts makes the cascade fail partway through —
	// after the attachment/reminder/share/note deletes have already run
	// inside the transaction.
	require.NoError(t, db.Exec("DROP TABLE activity_contacts").Error)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", actor.ID)
		c.Set("cfg", config.Config{})
		c.Next()
	})
	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest(http.MethodDelete, "/users/"+strconv.Itoa(int(target.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	// The transaction rolled back: the user and its earlier-cascaded rows all
	// survive (a partial delete is worse than no delete).
	var userCount, noteCount, activityCount int64
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", target.ID).Count(&userCount).Error)
	require.NoError(t, db.Model(&models.Activity{}).Where("user_id = ?", target.ID).Count(&activityCount).Error)
	require.NoError(t, db.Model(&models.Note{}).Where("user_id = ?", target.ID).Count(&noteCount).Error)
	assert.EqualValues(t, 1, userCount, "the user must survive a rolled-back cascade")
	assert.EqualValues(t, 1, activityCount, "activities must survive the rollback")
	assert.Zero(t, noteCount)
}

// TestDeleteUserAttachmentFiles_LogsStoredPathErrors covers the per-name
// cleanup loop's invalid-stored-name branch: a traversal name is skipped
// (never reaching the filesystem) while valid siblings are still removed.
func TestDeleteUserAttachmentFiles_LogsStoredPathErrors(t *testing.T) {
	dir := t.TempDir()
	good, err := attachments.Save([]byte("x"), dir)
	require.NoError(t, err)

	c := deleteContextWithDir(dir)
	deleteUserAttachmentFiles(c, []string{"../escape.txt", good})

	_, err = os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt"))
	assert.True(t, os.IsNotExist(err), "a traversal stored name must never reach the filesystem")
	_, err = os.Stat(filepath.Join(dir, good))
	assert.True(t, os.IsNotExist(err), "the valid stored name must still be removed")
}

func TestDeleteUser_UserLookupError(t *testing.T) {
	// A DB failure while loading the target user must 500 before any delete.
	db := dbtest.New(t)
	actor := models.User{Username: "countadmin", Password: "password123!A", Email: "countadmin@example.com"}
	require.NoError(t, db.Create(&actor).Error)
	target := models.User{Username: "counttarget", Password: "password123!A", Email: "counttarget@example.com", IsAdmin: true}
	require.NoError(t, db.Create(&target).Error)

	require.NoError(t, db.Exec("DROP TABLE users").Error)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", actor.ID)
		c.Set("cfg", config.Config{})
		c.Next()
	})
	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest(http.MethodDelete, "/users/"+strconv.Itoa(int(target.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
