package controllers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"mycorrhizal/attachments"
	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- DeleteUser: guard rails (the branches TestDeleteUser_CleansUpAllOwnedRows
// does not reach) ---

func TestDeleteUser_CannotDeleteSelf(t *testing.T) {
	db, router := setupRouter()

	var me models.User
	require.NoError(t, db.First(&me).Error)

	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/"+strconv.Itoa(int(me.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	var stillThere int64
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", me.ID).Count(&stillThere).Error)
	assert.EqualValues(t, 1, stillThere, "the acting user must never be able to delete their own account")
}

func TestDeleteUser_InvalidID(t *testing.T) {
	_, router := setupRouter()

	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/not-a-number", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestDeleteUser_NotFound(t *testing.T) {
	_, router := setupRouter()

	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/999999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestDeleteUser_LastAdminProtected(t *testing.T) {
	db, router := setupRouter()

	// The seeded "tester" user is NOT an admin, so a brand-new admin target
	// is the instance's only admin and must not be deletable. Post-#871 this
	// is caught by the peer-admin guard (any admin target is undeletable),
	// which strictly subsumes the old last-admin-delete check.
	require.NoError(t, db.Create(&models.User{Username: "sole-admin", Email: "sole-admin@example.com", Password: "password123", IsAdmin: true}).Error)
	var adminCount int64
	require.NoError(t, db.Model(&models.User{}).Where("is_admin = ?", true).Count(&adminCount).Error)
	require.EqualValues(t, 1, adminCount, "test setup requires exactly one admin")

	var target models.User
	require.NoError(t, db.Where("username = ?", "sole-admin").First(&target).Error)

	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/"+strconv.Itoa(int(target.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	var stillThere int64
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", target.ID).Count(&stillThere).Error)
	assert.EqualValues(t, 1, stillThere, "the last admin must not be deletable")
}

// An admin cannot delete a PEER admin, even when other admins remain (issue
// #871). The account must remove its own admin status first.
func TestDeleteUser_CannotDeletePeerAdmin(t *testing.T) {
	db, router := setupRouter()

	// Acting user (seeded "tester") becomes an admin.
	var actingUser models.User
	require.NoError(t, db.First(&actingUser).Error)
	actingUser.IsAdmin = true
	require.NoError(t, db.Save(&actingUser).Error)

	// Target admin plus a third admin, so this is not a last-admin situation.
	target := models.User{Username: "peer-admin", Email: "peer-admin@example.com", Password: "password123", IsAdmin: true}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&models.User{Username: "third-admin", Email: "third-admin@example.com", Password: "password123", IsAdmin: true}).Error)

	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/"+strconv.Itoa(int(target.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "another admin")

	var stillThere int64
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", target.ID).Count(&stillThere).Error)
	assert.EqualValues(t, 1, stillThere, "a peer admin must not be deletable")
}

// Once an account is no longer an admin it can be deleted normally -- the
// #871 guard keys off current admin status, not history.
func TestDeleteUser_DeletesFormerAdmin_AfterDemotion(t *testing.T) {
	db, router := setupRouter()

	var actingUser models.User
	require.NoError(t, db.First(&actingUser).Error)
	actingUser.IsAdmin = true
	require.NoError(t, db.Save(&actingUser).Error)

	target := models.User{Username: "former-admin", Email: "former-admin@example.com", Password: "password123", IsAdmin: true}
	require.NoError(t, db.Create(&target).Error)

	// The target steps down (self-service in the real API; a direct write here).
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", target.ID).Update("is_admin", false).Error)

	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/"+strconv.Itoa(int(target.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var gone int64
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", target.ID).Count(&gone).Error)
	assert.EqualValues(t, 0, gone, "a former admin (now an ordinary account) is deletable")
}

func TestDeleteUser_RemovesAttachmentFilesFromDisk(t *testing.T) {
	db := realDB(t)
	user := models.User{Username: "del-user", Email: "del-user@example.com", Password: "password123"}
	require.NoError(t, db.Create(&user).Error)

	attachmentsDir := filepath.Join(t.TempDir(), "attachments")
	storedName, err := attachments.Save([]byte("payload"), attachmentsDir)
	require.NoError(t, err)
	filePath := filepath.Join(attachmentsDir, storedName)
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	require.NoError(t, db.Create(&models.Attachment{
		UserID: user.ID, StoredName: storedName, OriginalName: "doc.txt",
		ContentType: "text/plain", SizeBytes: 7,
	}).Error)

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", uint(999999)) // acting admin is someone else
		c.Set("cfg", config.Config{AttachmentsDir: attachmentsDir})
		c.Next()
	})
	router.DELETE("/users/:id", DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/"+strconv.Itoa(int(user.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = os.Stat(filePath)
	assert.True(t, os.IsNotExist(err), "deleting a user must remove their attachment files from disk")
}

// realDB opens a real migrated schema database (AutoMigrate would silently
// disagree with the hand-written migration columns — CLAUDE.md backend trap
// #1).
func realDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := dbtest.New(t)
	return db
}

// --- deleteUserAttachmentFiles (unit) ---

func deleteContextWithDir(dir string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	c.Set("cfg", config.Config{AttachmentsDir: dir})
	return c
}

func TestDeleteUserAttachmentFiles_EmptyDirIsNoop(t *testing.T) {
	// An unset AttachmentsDir (the default local-dev config until N7 lands)
	// must never attempt any file deletion.
	c := deleteContextWithDir("")
	assert.NotPanics(t, func() { deleteUserAttachmentFiles(c, []string{"some-uuid"}) })
}

func TestDeleteUserAttachmentFiles_EmptyNamesIsNoop(t *testing.T) {
	c := deleteContextWithDir(t.TempDir())
	assert.NotPanics(t, func() { deleteUserAttachmentFiles(c, nil) })
}

func TestDeleteUserAttachmentFiles_RemovesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	names := []string{}
	for _, content := range []string{"a", "b"} {
		name, err := attachments.Save([]byte(content), dir)
		require.NoError(t, err)
		names = append(names, name)
	}

	c := deleteContextWithDir(dir)
	deleteUserAttachmentFiles(c, names)

	for _, name := range names {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.True(t, os.IsNotExist(err), "file %q should have been removed", name)
	}
}

func TestDeleteUserAttachmentFiles_SkipsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	// A stored name whose row was already purged must not fail the cleanup.
	c := deleteContextWithDir(dir)
	assert.NotPanics(t, func() {
		deleteUserAttachmentFiles(c, []string{"missing-but-valid-uuid"})
	})
}

func TestDeleteUserAttachmentFiles_SkipsInvalidStoredName(t *testing.T) {
	dir := t.TempDir()
	// A corrupt stored name (traversal) must be logged and skipped, never
	// turned into a path that escapes the attachments directory.
	c := deleteContextWithDir(dir)
	assert.NotPanics(t, func() {
		deleteUserAttachmentFiles(c, []string{"../escape.txt", "ok-file"})
	})
	_, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt"))
	assert.True(t, os.IsNotExist(err), "a traversal stored name must never reach the filesystem")
}
