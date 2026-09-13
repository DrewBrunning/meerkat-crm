package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	apperrors "mycorrhizal/errors"

	"mycorrhizal/config"
	"mycorrhizal/models"
	"mycorrhizal/monica"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const monicaTestToken = "s3cr3t-monica-token"

// pngBytes is a tiny valid PNG the mock Monica serves as an avatar so
// processAvatars' decode/save path runs for real.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

type mockMonicaOptions struct {
	includeAvatar  bool
	includeExtras  bool
	includeRelated bool
	failEverything bool
	// breakOneAvatar gives both contacts an avatar but points Ada's at a URL
	// the mock 404s, so processAvatars runs a mixed success/failure batch.
	breakOneAvatar bool
}

// mockMonica serves the slice of the Monica list API the assistant fetches.
// Contact 1 is flagged is_dead with no deceased date, which the mapper reports
// as a named loss (so the preview's loss report is non-trivial).
func mockMonica(t *testing.T, opt mockMonicaOptions) *httptest.Server {
	t.Helper()
	list := func(w http.ResponseWriter, data []map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": data,
			"meta": map[string]int{"current_page": 1, "last_page": 1, "total": len(data)},
		})
	}
	var srvURL string
	build := func() (contacts, activities, notes, calls, tasks, gifts []map[string]any, rels map[string][]map[string]any) {
		ada := map[string]any{"id": 1, "first_name": "Ada", "last_name": "Lovelace", "is_dead": true}
		grace := map[string]any{"id": 2, "first_name": "Grace", "last_name": "Hopper", "contactFields": []map[string]any{
			{"content": "grace@example.com", "contact_field_type": map[string]any{"name": "Email", "type": "email"}},
		}}
		if opt.includeAvatar {
			ada["information"] = map[string]any{"avatar": map[string]any{"url": srvURL + "/storage/avatar.png", "source": "photo"}}
		}
		if opt.breakOneAvatar {
			ada["information"] = map[string]any{"avatar": map[string]any{"url": srvURL + "/storage/missing.png", "source": "photo"}}
			grace["information"] = map[string]any{"avatar": map[string]any{"url": srvURL + "/storage/avatar.png", "source": "photo"}}
		}
		contacts = []map[string]any{ada, grace}
		notes = []map[string]any{
			{"id": 10, "body": "met at the analytical engine demo", "created_at": "2023-01-02T00:00:00Z",
				"contact": map[string]any{"id": 1, "first_name": "Ada"}},
		}
		if opt.includeExtras {
			calls = []map[string]any{{"id": 1, "content": "called about the engine", "called_at": "2023-03-01", "contact": map[string]any{"id": 1}}}
			tasks = []map[string]any{{"id": 1, "title": "send notes", "completed": false, "created_at": "2023-03-02T00:00:00Z", "contact": map[string]any{"id": 2}}}
			gifts = []map[string]any{{"id": 1, "name": "a book", "status": "idea", "contact": map[string]any{"id": 1}}}
		}
		if opt.includeRelated {
			rels = map[string][]map[string]any{
				"/api/contacts/1/relationships": {{
					"relationship_type": map[string]any{"name": "colleague"},
					"contact_is":        map[string]any{"id": 1},
					"of_contact":        map[string]any{"id": 2, "first_name": "Grace"},
				}},
			}
		}
		return
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+monicaTestToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if opt.failEverything {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/storage/avatar.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes(t))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		contacts, activities, notes, calls, tasks, gifts, rels := build()
		switch {
		case r.URL.Path == "/api/contacts":
			list(w, contacts)
		case r.URL.Path == "/api/notes":
			list(w, notes)
		case r.URL.Path == "/api/activities":
			list(w, activities)
		case r.URL.Path == "/api/calls":
			list(w, calls)
		case r.URL.Path == "/api/tasks":
			list(w, tasks)
		case r.URL.Path == "/api/gifts":
			list(w, gifts)
		case r.URL.Path == "/api/reminders" || r.URL.Path == "/api/debts":
			list(w, nil)
		case strings.HasSuffix(r.URL.Path, "/relationships"):
			list(w, rels[r.URL.Path])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	srvURL = srv.URL
	return srv
}

// syncBuffer is a goroutine-safe log sink for the Monica import tests. The
// manager logs from its background goroutines while the test reads the buffer
// after the session reaches a terminal phase; a plain bytes.Buffer races there
// because the phase write and the completion log are not ordered (the log is
// emitted after the mutex-guarded phase transition). Both the logger's Write
// and the test's String go through the same mutex.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func monicaTestLogger(buf *syncBuffer) *zerolog.Logger {
	l := zerolog.New(buf)
	return &l
}

// waitForPhase polls Status until the session reaches want, failing on
// timeout or on an unexpected terminal phase.
func waitForPhase(t *testing.T, m *MonicaImportManager, userID uint, sessionID, want string) *models.MonicaImportStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, appErr := m.Status(userID, sessionID)
		require.Nil(t, appErr)
		if st.Phase == want {
			return st
		}
		if st.Phase == models.MonicaPhaseFailed && want != models.MonicaPhaseFailed {
			t.Fatalf("session failed unexpectedly: %s", st.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for phase %q", want)
	return nil
}

// confirmAndWait starts the async confirm and polls to wantPhase, returning
// the final status.
func confirmAndWait(t *testing.T, mgr *MonicaImportManager, db *gorm.DB, userID uint, sid string, actions []models.RowImportAction, cfg *config.Config, log *zerolog.Logger, wantPhase string) *models.MonicaImportStatus {
	t.Helper()
	appErr := mgr.Confirm(db, userID, models.MonicaConfirmRequest{SessionID: sid, Actions: actions}, cfg, log)
	require.Nil(t, appErr)
	return waitForPhase(t, mgr, userID, sid, wantPhase)
}

func TestMonicaImportSession_FullFlow_AddAndMerge(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	grace := models.Contact{UserID: user.ID, Firstname: "Grace", Lastname: "Hopper"}
	require.NoError(t, db.Create(&grace).Error)

	var logBuf syncBuffer
	log := monicaTestLogger(&logBuf)
	mgr := NewMonicaImportManager()

	connectResp, appErr := mgr.Connect(context.Background(), user.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)
	assert.Equal(t, 2, connectResp.Totals.Contacts)
	sid := connectResp.SessionID

	require.Nil(t, mgr.StartFetch(db, user.ID, models.MonicaFetchRequest{SessionID: sid}, log))
	waitForPhase(t, mgr, user.ID, sid, models.MonicaPhaseReady)

	preview, appErr := mgr.Preview(user.ID, sid)
	require.Nil(t, appErr)
	require.Len(t, preview.Rows, 2)
	var sawDeadLoss bool
	for _, iss := range preview.LossReport {
		if strings.Contains(strings.ToLower(iss.Message), "is_dead") || strings.Contains(strings.ToLower(iss.Field), "dead") {
			sawDeadLoss = true
		}
	}
	assert.True(t, sawDeadLoss, "the bare is_dead loss must be in the loss report: %+v", preview.LossReport)

	adaRowIdx, graceRowIdx := -1, -1
	for _, row := range preview.Rows {
		switch row.ParsedContact["firstname"] {
		case "Ada":
			adaRowIdx = row.RowIndex
			assert.Equal(t, 1, row.Related.Notes, "Ada carries the imported note")
		case "Grace":
			graceRowIdx = row.RowIndex
			require.NotNil(t, row.DuplicateMatch, "Grace is a duplicate of the seeded contact")
		}
	}
	require.NotEqual(t, -1, adaRowIdx)
	require.NotEqual(t, -1, graceRowIdx)

	status := confirmAndWait(t, mgr, db, user.ID, sid, []models.RowImportAction{
		{RowIndex: adaRowIdx, Action: "add"},
		{RowIndex: graceRowIdx, Action: "update"},
	}, &config.Config{ProfilePhotoDir: t.TempDir()}, log, models.MonicaPhaseDone)

	require.NotNil(t, status.Result)
	assert.Equal(t, 1, status.Result.Created)
	assert.Equal(t, 1, status.Result.Updated)
	assert.GreaterOrEqual(t, status.Result.NotesCreated, 1)

	var contactCount int64
	db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&contactCount)
	assert.Equal(t, int64(2), contactCount)
	var mergedGrace models.Contact
	require.NoError(t, db.First(&mergedGrace, grace.ID).Error)
	assert.Equal(t, "grace@example.com", mergedGrace.Email, "the Monica email was merged in")

	var runs int64
	db.Model(&models.ImportRun{}).Where("user_id = ? AND format = ?", user.ID, models.ImportFormatMonica).Count(&runs)
	assert.Equal(t, int64(1), runs)

	assert.NotContains(t, logBuf.String(), monicaTestToken)
}

func TestMonicaImportSession_ReRunIsIdempotent(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	log := monicaTestLogger(&syncBuffer{})
	mgr := NewMonicaImportManager()

	run := func() *models.MonicaImportResult {
		resp, appErr := mgr.Connect(context.Background(), user.ID,
			models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
		require.Nil(t, appErr)
		require.Nil(t, mgr.StartFetch(db, user.ID, models.MonicaFetchRequest{SessionID: resp.SessionID}, log))
		waitForPhase(t, mgr, user.ID, resp.SessionID, models.MonicaPhaseReady)
		preview, appErr := mgr.Preview(user.ID, resp.SessionID)
		require.Nil(t, appErr)
		actions := make([]models.RowImportAction, len(preview.Rows))
		for i, row := range preview.Rows {
			actions[i] = models.RowImportAction{RowIndex: row.RowIndex, Action: "add"}
		}
		st := confirmAndWait(t, mgr, db, user.ID, resp.SessionID, actions,
			&config.Config{ProfilePhotoDir: t.TempDir()}, log, models.MonicaPhaseDone)
		return st.Result
	}

	first := run()
	assert.Equal(t, 2, first.Created)

	second := run()
	assert.Equal(t, 0, second.Created, "a re-run creates nothing")
	assert.Equal(t, 2, second.Skipped, "both contacts skipped as already imported")

	var contactCount int64
	db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&contactCount)
	assert.Equal(t, int64(2), contactCount)
}

func TestMonicaImportSession_AvatarDownloadedAfterImport(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{includeAvatar: true})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	log := monicaTestLogger(&syncBuffer{})
	mgr := NewMonicaImportManager()

	resp, appErr := mgr.Connect(context.Background(), user.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)
	require.Nil(t, mgr.StartFetch(db, user.ID, models.MonicaFetchRequest{SessionID: resp.SessionID}, log))
	waitForPhase(t, mgr, user.ID, resp.SessionID, models.MonicaPhaseReady)
	preview, appErr := mgr.Preview(user.ID, resp.SessionID)
	require.Nil(t, appErr)
	actions := make([]models.RowImportAction, len(preview.Rows))
	for i, row := range preview.Rows {
		actions[i] = models.RowImportAction{RowIndex: row.RowIndex, Action: "add"}
	}

	status := confirmAndWait(t, mgr, db, user.ID, resp.SessionID, actions,
		&config.Config{ProfilePhotoDir: t.TempDir()}, log, models.MonicaPhaseDone)
	require.NotNil(t, status.Result)
	assert.Equal(t, 1, status.Result.PhotosQueued)
	assert.Equal(t, 1, status.Result.PhotosSaved)
	assert.Equal(t, 0, status.Result.PhotosFailed)

	var withPhoto int64
	db.Model(&models.Contact{}).Where("user_id = ? AND photo <> ''", user.ID).Count(&withPhoto)
	assert.Equal(t, int64(1), withPhoto)
}

// TestMonicaImportSession_AvatarFailurePathCounted covers processAvatars when
// one avatar fetch 404s mid-batch (issue #725): the failure is counted, the
// other avatar still saves, the import still reaches "done", and a nil photo
// body from the failed fetch never reaches the save/decode path.
func TestMonicaImportSession_AvatarFailurePathCounted(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{breakOneAvatar: true})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	var logBuf syncBuffer
	log := monicaTestLogger(&logBuf)
	mgr := NewMonicaImportManager()

	resp, appErr := mgr.Connect(context.Background(), user.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)
	require.Nil(t, mgr.StartFetch(db, user.ID, models.MonicaFetchRequest{SessionID: resp.SessionID}, log))
	waitForPhase(t, mgr, user.ID, resp.SessionID, models.MonicaPhaseReady)
	preview, appErr := mgr.Preview(user.ID, resp.SessionID)
	require.Nil(t, appErr)
	actions := make([]models.RowImportAction, len(preview.Rows))
	for i, row := range preview.Rows {
		actions[i] = models.RowImportAction{RowIndex: row.RowIndex, Action: "add"}
	}

	status := confirmAndWait(t, mgr, db, user.ID, resp.SessionID, actions,
		&config.Config{ProfilePhotoDir: t.TempDir()}, log, models.MonicaPhaseDone)
	require.NotNil(t, status.Result)
	assert.Equal(t, 2, status.Result.PhotosQueued, "both contacts carry an avatar URL")
	assert.Equal(t, 1, status.Result.PhotosSaved, "the reachable avatar still saves")
	assert.Equal(t, 1, status.Result.PhotosFailed, "the 404 avatar is counted, not fatal")
	assert.Contains(t, logBuf.String(), "Failed to fetch Monica avatar", "the per-photo failure is logged")

	var withPhoto int64
	db.Model(&models.Contact{}).Where("user_id = ? AND photo <> ''", user.ID).Count(&withPhoto)
	assert.Equal(t, int64(1), withPhoto, "only the contact whose avatar fetched has a photo")
	assert.NotContains(t, logBuf.String(), monicaTestToken)
}

func TestMonicaImportSession_ExtrasAndRelationshipsFetched(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{includeExtras: true, includeRelated: true})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	log := monicaTestLogger(&syncBuffer{})
	mgr := NewMonicaImportManager()

	resp, appErr := mgr.Connect(context.Background(), user.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)
	require.Nil(t, mgr.StartFetch(db, user.ID,
		models.MonicaFetchRequest{SessionID: resp.SessionID, IncludeExtras: true, IncludeRelationships: true}, log))
	waitForPhase(t, mgr, user.ID, resp.SessionID, models.MonicaPhaseReady)

	preview, appErr := mgr.Preview(user.ID, resp.SessionID)
	require.Nil(t, appErr)
	assert.Equal(t, 1, preview.Totals.Relationships, "the colleague edge was fetched and mapped")

	actions := make([]models.RowImportAction, len(preview.Rows))
	for i, row := range preview.Rows {
		actions[i] = models.RowImportAction{RowIndex: row.RowIndex, Action: "add"}
	}
	st := confirmAndWait(t, mgr, db, user.ID, resp.SessionID, actions,
		&config.Config{ProfilePhotoDir: t.TempDir()}, log, models.MonicaPhaseDone)
	assert.Equal(t, 1, st.Result.RelationshipsCreated)
	assert.GreaterOrEqual(t, st.Result.ActivitiesCreated, 1, "a logged call maps to an activity")
}

func TestMonicaImportSession_FetchFailureSurfaces(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{failEverything: true})
	defer srv.Close()

	mgr := NewMonicaImportManager()
	_, appErr := mgr.Connect(context.Background(), 1,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	// Connect itself fails because CountEntities hits the 500s.
	require.NotNil(t, appErr)
}

func TestMonicaImportSession_FetchFailsAfterConnect(t *testing.T) {
	monica.DisableRateLimitForTesting()
	failing := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			w.Header().Set("Retry-After", "0") // retry immediately so the fetch fails fast
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "meta": map[string]int{"current_page": 1, "last_page": 1, "total": 1}})
	}))
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	log := monicaTestLogger(&syncBuffer{})
	mgr := NewMonicaImportManager()

	resp, appErr := mgr.Connect(context.Background(), user.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)
	failing = true
	require.Nil(t, mgr.StartFetch(db, user.ID, models.MonicaFetchRequest{SessionID: resp.SessionID}, log))
	st := waitForPhase(t, mgr, user.ID, resp.SessionID, models.MonicaPhaseFailed)
	assert.NotEmpty(t, st.Error)
}

func TestMonicaImportSession_ConnectRejectsBadToken(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{})
	defer srv.Close()

	mgr := NewMonicaImportManager()
	_, appErr := mgr.Connect(context.Background(), 1,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: "wrong"}, false)
	require.NotNil(t, appErr)
	assert.Equal(t, "api_token", appErr.Details["field"])
}

func TestMonicaImportSession_SessionOwnershipEnforced(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	owner := createSourceImportUser(t, db)
	mgr := NewMonicaImportManager()

	resp, appErr := mgr.Connect(context.Background(), owner.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)

	_, appErr = mgr.Status(owner.ID+999, resp.SessionID)
	require.NotNil(t, appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestMonicaImportSession_CancelDropsSession(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{})
	defer srv.Close()

	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	mgr := NewMonicaImportManager()

	resp, appErr := mgr.Connect(context.Background(), user.ID,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)

	require.Nil(t, mgr.Cancel(user.ID, resp.SessionID))
	_, appErr = mgr.Status(user.ID, resp.SessionID)
	require.NotNil(t, appErr, "the session is gone after a pre-import cancel")

	// Cancelling an unknown session is a not-found, not a panic.
	require.NotNil(t, mgr.Cancel(user.ID, "nope"))
}

func TestMonicaImportSession_CleanupExpiredRemovesSession(t *testing.T) {
	monica.DisableRateLimitForTesting()
	srv := mockMonica(t, mockMonicaOptions{})
	defer srv.Close()

	mgr := NewMonicaImportManager()
	resp, appErr := mgr.Connect(context.Background(), 7,
		models.MonicaConnectRequest{BaseURL: srv.URL, APIToken: monicaTestToken}, false)
	require.Nil(t, appErr)

	mgr.mu.Lock()
	mgr.sessions[resp.SessionID].expiresAt = time.Now().Add(-time.Minute)
	mgr.sessions[resp.SessionID].hardExpiry = time.Now().Add(-time.Minute)
	mgr.mu.Unlock()

	mgr.CleanupExpired()
	assert.Equal(t, 0, mgr.CountActive(7))
	mgr.Delete(resp.SessionID) // no-op, already gone — exercises the exists==false path
}

func TestUserSafeMonicaError_MapsEverySentinel(t *testing.T) {
	cases := []struct {
		err   error
		field string
	}{
		{monica.ErrUnauthorized, "api_token"},
		{monica.ErrInvalidURL, "base_url"},
		{monica.ErrPrivateAddress, "base_url"},
		{monica.ErrInvalidData, "base_url"},
		{monica.ErrUnreachable, ""},
		{errors.New("boom"), ""},
	}
	for _, tc := range cases {
		msg := userSafeMonicaError(tc.err)
		assert.NotEmpty(t, msg)
		assert.NotContains(t, msg, monicaTestToken)
		appErr := monicaAppError(tc.err)
		require.NotNil(t, appErr)
		if tc.field != "" {
			assert.Equal(t, tc.field, appErr.Details["field"])
		} else {
			assert.Equal(t, apperrors.ErrCodeExternal, appErr.Code)
		}
	}
}

func TestEstimateFetchSeconds_Grows(t *testing.T) {
	small := estimateFetchSeconds(monica.EntityCounts{Contacts: 10})
	big := estimateFetchSeconds(monica.EntityCounts{Contacts: 1000, Notes: 500})
	assert.Greater(t, big, small)
}

func TestMonicaImportManager_CancelInFlightImport(t *testing.T) {
	mgr := NewMonicaImportManager()
	cancelled := false
	now := time.Now()
	s := &monicaImportSession{
		id: "x", userID: 1, phase: models.MonicaPhaseImporting,
		cancel:     func() { cancelled = true },
		expiresAt:  now.Add(time.Hour),
		hardExpiry: now.Add(time.Hour),
	}
	mgr.mu.Lock()
	mgr.sessions["x"] = s
	mgr.mu.Unlock()

	require.Nil(t, mgr.Cancel(1, "x"))
	assert.True(t, cancelled, "the in-flight transaction context is cancelled")

	st, appErr := mgr.Status(1, "x")
	require.Nil(t, appErr, "the session stays so the user can retry")
	assert.Equal(t, models.MonicaPhaseCancelled, st.Phase)
}

func TestMonicaImportManager_RunImportCancelledContext(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)
	log := monicaTestLogger(&syncBuffer{})
	mgr := NewMonicaImportManager()

	now := time.Now()
	s := &monicaImportSession{id: "x", userID: user.ID, phase: models.MonicaPhaseImporting,
		expiresAt: now.Add(time.Hour), hardExpiry: now.Add(time.Hour)}
	mgr.mu.Lock()
	mgr.sessions["x"] = s
	mgr.mu.Unlock()

	plan := &ImportSourcePlan{System: "monica", Contacts: []MappedContact{
		{Ref: SourceRef{System: "monica", ExternalID: "contact/1"},
			Record: minimalRecord("Ada", "Lovelace")},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr.runImport(ctx, db, s, plan, nil, map[string]SourceContactAction{}, &config.Config{ProfilePhotoDir: t.TempDir()}, log)

	st, appErr := mgr.Status(user.ID, "x")
	require.Nil(t, appErr)
	assert.Equal(t, models.MonicaPhaseCancelled, st.Phase)

	var count int64
	db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}
