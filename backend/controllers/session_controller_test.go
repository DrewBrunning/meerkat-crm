package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Issue #866 / ASVS 3.3.4: the /sessions inventory. Scoped by user_id (no
// IDOR), "current" flag reflects the requesting session, and revoke-others
// keeps the caller's own session.

func sessionCtrlEnv(t *testing.T) (*gorm.DB, uint, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := dbtest.New(t)
	a := models.User{Username: "sess-a", Email: "sess-a@example.com", Password: "x"}
	b := models.User{Username: "sess-b", Email: "sess-b@example.com", Password: "x"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	return db, a.ID, b.ID
}

func seedCtrlSession(t *testing.T, db *gorm.DB, userID uint, id string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&models.Session{
		ID: id, UserID: userID, CreatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}).Error)
}

func sessionRouter(db *gorm.DB, userID uint, sid string) *gin.Engine {
	return sessionRouterOpts(db, &userID, sid)
}

// sessionRouterOpts builds the /sessions router; a nil userID omits the
// context value entirely, so the handlers' currentUserID !ok branch runs.
func sessionRouterOpts(db *gorm.DB, userID *uint, sid string) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		if userID != nil {
			c.Set("userID", *userID)
		}
		if sid != "" {
			c.Set("sessionID", sid)
		}
		c.Next()
	})
	r.GET("/sessions", ListSessions)
	r.DELETE("/sessions", RevokeOtherSessions)
	r.DELETE("/sessions/:id", RevokeSession)
	return r
}

func TestListSessions_ScopedToUserWithCurrentFlag(t *testing.T) {
	db, a, b := sessionCtrlEnv(t)
	seedCtrlSession(t, db, a, "a-desktop")
	seedCtrlSession(t, db, a, "a-phone")
	seedCtrlSession(t, db, b, "b-laptop")
	// A revoked and an expired row must not appear.
	seedCtrlSession(t, db, a, "a-old")
	require.NoError(t, db.Model(&models.Session{}).Where("id = ?", "a-old").Update("revoked_at", time.Now()).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/sessions", nil)
	sessionRouter(db, a, "a-desktop").ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Sessions []models.SessionResponse `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Sessions, 2)

	byID := map[string]models.SessionResponse{}
	for _, s := range body.Sessions {
		byID[s.ID] = s
	}
	require.Contains(t, byID, "a-desktop")
	require.Contains(t, byID, "a-phone")
	assert.True(t, byID["a-desktop"].Current)
	assert.False(t, byID["a-phone"].Current)
}

// An API-token caller has a userID but no session row, so currentSessionID
// falls back to "" and no row is flagged current.
func TestListSessions_NoCurrentSessionForAPITokenCaller(t *testing.T) {
	db, a, _ := sessionCtrlEnv(t)
	seedCtrlSession(t, db, a, "a-desktop")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/sessions", nil)
	sessionRouter(db, a, "").ServeHTTP(w, req) // "" -> sessionID not set in context

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Sessions []models.SessionResponse `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Sessions, 1)
	assert.False(t, body.Sessions[0].Current)
}

func TestRevokeSession_OwnSessionOK_OtherUserIs404(t *testing.T) {
	db, a, b := sessionCtrlEnv(t)
	seedCtrlSession(t, db, a, "a-phone")
	seedCtrlSession(t, db, b, "b-laptop")

	// A revokes their own.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/sessions/a-phone", nil)
	sessionRouter(db, a, "a-desktop").ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var aPhone models.Session
	require.NoError(t, db.First(&aPhone, "id = ?", "a-phone").Error)
	assert.NotNil(t, aPhone.RevokedAt)

	// A cannot revoke B's session — 404, and B's row is untouched.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/sessions/b-laptop", nil)
	sessionRouter(db, a, "a-desktop").ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	var bLaptop models.Session
	require.NoError(t, db.First(&bLaptop, "id = ?", "b-laptop").Error)
	assert.Nil(t, bLaptop.RevokedAt)
}

func TestSessionEndpoints_RequireAuthAndSurfaceDBErrors(t *testing.T) {
	db, a, _ := sessionCtrlEnv(t)
	seedCtrlSession(t, db, a, "a-desktop")

	// --- no userID in context -> currentUserID !ok -> 401 on every endpoint ---
	noAuth := sessionRouterOpts(db, nil, "")
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/sessions"},
		{http.MethodDelete, "/sessions"},
		{http.MethodDelete, "/sessions/a-desktop"},
	} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		noAuth.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}

	// --- a dead DB -> the query/update error branches -> 500 ---
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	dead := sessionRouter(db, a, "a-desktop")

	wList := httptest.NewRecorder()
	reqList, _ := http.NewRequest(http.MethodGet, "/sessions", nil)
	dead.ServeHTTP(wList, reqList)
	assert.Equal(t, http.StatusInternalServerError, wList.Code)

	wAll := httptest.NewRecorder()
	reqAll, _ := http.NewRequest(http.MethodDelete, "/sessions", nil)
	dead.ServeHTTP(wAll, reqAll)
	assert.Equal(t, http.StatusInternalServerError, wAll.Code)
}

func TestRevokeOtherSessions_KeepsCurrentRevokesRest(t *testing.T) {
	db, a, b := sessionCtrlEnv(t)
	seedCtrlSession(t, db, a, "a-current")
	seedCtrlSession(t, db, a, "a-phone")
	seedCtrlSession(t, db, a, "a-tablet")
	seedCtrlSession(t, db, b, "b-laptop")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/sessions", nil)
	sessionRouter(db, a, "a-current").ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Revoked int64 `json:"revoked"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.EqualValues(t, 2, body.Revoked)

	activeFor := func(id string) bool {
		var s models.Session
		require.NoError(t, db.First(&s, "id = ?", id).Error)
		return s.RevokedAt == nil
	}
	assert.True(t, activeFor("a-current"), "the requesting session survives")
	assert.False(t, activeFor("a-phone"))
	assert.False(t, activeFor("a-tablet"))
	assert.True(t, activeFor("b-laptop"), "another user's session is untouched")
}
