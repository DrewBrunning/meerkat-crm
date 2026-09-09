package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// --- liveness ---------------------------------------------------------------

func TestLiveness_AlwaysUpAndCheap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New() // deliberately NO db / cfg middleware
	r.GET("/health/live", LivenessCheck)

	start := time.Now()
	code, body := getJSON(t, r, "/health/live")
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "live", body["status"])
	require.Less(t, elapsed, 100*time.Millisecond, "liveness must not do real work")
}

func TestLiveness_UpEvenWhenDatabaseIsDown(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	code, body := getJSON(t, r, "/health/live")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "live", body["status"])
}

// --- readiness ------------------------------------------------------------

func TestReadiness_HappyPath(t *testing.T) {
	_, _, r := migratedHealthRouter(t)

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "ready", body["status"])

	checks, _ := body["checks"].(map[string]any)
	for _, name := range []string{"database", "migrations", "filesystem"} {
		facet, _ := checks[name].(map[string]any)
		require.Equal(t, "ok", facet["status"], "facet %s", name)
	}
}

func TestReadiness_DatabaseDown(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	sqlDB, _ := db.DB()
	require.NoError(t, sqlDB.Close())

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "not_ready", body["status"])
}

func TestReadiness_DirtyMigration(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	require.NoError(t, db.Exec("UPDATE schema_migrations SET dirty = 1").Error)

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	checks, _ := body["checks"].(map[string]any)
	facet, _ := checks["migrations"].(map[string]any)
	require.Equal(t, "failed", facet["status"])
	require.Contains(t, facet["reason"], "dirty")
}

func TestReadiness_PendingMigration(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	// Rewind the recorded version so applied < latest (a database that a newer
	// binary has not finished migrating).
	require.NoError(t, db.Exec("UPDATE schema_migrations SET version = version - 1").Error)

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	checks, _ := body["checks"].(map[string]any)
	facet, _ := checks["migrations"].(map[string]any)
	require.Equal(t, "failed", facet["status"])
	require.Contains(t, facet["reason"], "pending migrations")
}

// TestReadiness_SchemaAheadOfBinary is DEPLOY-03 (issue #452) action 4: a
// database whose applied version is HIGHER than this binary's latest migration
// (a rolled-back binary — issue #439 state 2) must gate traffic off, not fall
// through to "ok". The startup path refuses to boot on this state; readiness is
// the runtime backstop and must agree with services/deep_health.go.
func TestReadiness_SchemaAheadOfBinary(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	require.NoError(t, db.Exec("UPDATE schema_migrations SET version = version + 5").Error)

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "not_ready", body["status"])
	checks, _ := body["checks"].(map[string]any)
	facet, _ := checks["migrations"].(map[string]any)
	require.Equal(t, "failed", facet["status"])
	require.Contains(t, facet["reason"], "ahead of the binary")
}

// TestReadiness_NotReadyThroughEveryInterruptedMigrationState is DEPLOY-03
// action 4: through every interrupted-migration state an operator can hit —
// dirty, behind the binary, ahead of the binary — /health/ready is 503
// not_ready with the migrations facet failed, while /health/live stays 200
// (a stuck or half-done migration must never fail liveness, or the orchestrator
// kills a container that just needs to finish migrating).
func TestReadiness_NotReadyThroughEveryInterruptedMigrationState(t *testing.T) {
	states := map[string]string{
		"dirty":  "UPDATE schema_migrations SET dirty = 1",
		"behind": "UPDATE schema_migrations SET version = version - 1",
		"ahead":  "UPDATE schema_migrations SET version = version + 3",
	}
	for name, stmt := range states {
		t.Run(name, func(t *testing.T) {
			db, _, r := migratedHealthRouter(t)
			require.NoError(t, db.Exec(stmt).Error)

			code, body := getJSON(t, r, "/health/ready")
			require.Equal(t, http.StatusServiceUnavailable, code, "%s must not be ready", name)
			require.Equal(t, "not_ready", body["status"])
			checks, _ := body["checks"].(map[string]any)
			facet, _ := checks["migrations"].(map[string]any)
			require.Equal(t, "failed", facet["status"], "%s migrations facet", name)
			require.NotEmpty(t, facet["reason"])

			liveCode, liveBody := getJSON(t, r, "/health/live")
			require.Equal(t, http.StatusOK, liveCode, "%s must stay live", name)
			require.Equal(t, "live", liveBody["status"])
		})
	}
}

func TestReadiness_FilesystemUnavailable(t *testing.T) {
	_, cfg, r := migratedHealthRouter(t)
	cfg.ProfilePhotoDir = filepath.Join(t.TempDir(), "nonexistent")

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	checks, _ := body["checks"].(map[string]any)
	facet, _ := checks["filesystem"].(map[string]any)
	require.Equal(t, "failed", facet["status"])
	reason, _ := facet["reason"].(string)
	require.Contains(t, reason, "missing")
	// No absolute path in the unauthenticated body.
	require.NotContains(t, reason, cfg.ProfilePhotoDir)
}

func TestReadiness_FilesystemPathIsAFile(t *testing.T) {
	_, cfg, r := migratedHealthRouter(t)
	f := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
	cfg.AttachmentsDir = f

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	checks, _ := body["checks"].(map[string]any)
	facet, _ := checks["filesystem"].(map[string]any)
	require.Equal(t, "failed", facet["status"])
	require.Contains(t, facet["reason"], "not a directory")
}

func TestReadiness_FilesystemDirNotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory mode bits")
	}
	_, cfg, r := migratedHealthRouter(t)
	ro := filepath.Join(t.TempDir(), "readonly")
	require.NoError(t, os.Mkdir(ro, 0o500))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	cfg.ProfilePhotoDir = ro

	code, body := getJSON(t, r, "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, code)
	checks, _ := body["checks"].(map[string]any)
	facet, _ := checks["filesystem"].(map[string]any)
	require.Equal(t, "failed", facet["status"])
	require.Contains(t, facet["reason"], "not writable")
}

// --- deep health --------------------------------------------------------
//
// GET /health is unauthenticated and reports ONLY the rolled-up status word
// (plus build/compat identity). The per-facet breakdown — job names,
// integrity-check / restore-drill / data-integrity state, integration
// reachability — is admin-only at GET /api/v1/admin/system-status (issue
// #864). These tests exercise the roll-up through the controller; the facet
// logic itself is covered in services/deep_health_test.go.

func TestDeepHealth_DegradedWhenIntegrityCheckNeverRecorded(t *testing.T) {
	_, cfg, r := migratedHealthRouter(t)
	cfg.DBIntegrityCheckEnabled = true

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusOK, code, "degraded is still 200 — degraded-but-alive is not down")
	require.Equal(t, "degraded", body["status"])
	_, hasChecks := body["checks"]
	require.False(t, hasChecks, "the facet breakdown is admin-only (issue #864)")
}

func TestDeepHealth_HealthyWithRecordedOKResults(t *testing.T) {
	db, cfg, r := migratedHealthRouter(t)
	cfg.DBIntegrityCheckEnabled = true
	cfg.DBRestoreDrillEnabled = true

	now := time.Now()
	for _, name := range []string{models.JobNameDBIntegrityCheck, models.CheckNameDataIntegrity, models.JobNameRestoreDrill} {
		require.NoError(t, db.Create(&models.OperationalCheckResult{
			CheckName: name, Status: models.OpCheckStatusOK, CheckedAt: now,
		}).Error)
	}

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "healthy", body["status"])
}

func TestDeepHealth_DegradedWhenIntegrityCheckFailed(t *testing.T) {
	db, cfg, r := migratedHealthRouter(t)
	cfg.DBIntegrityCheckEnabled = true

	require.NoError(t, db.Create(&models.OperationalCheckResult{
		CheckName: models.JobNameDBIntegrityCheck,
		Status:    models.OpCheckStatusFailed,
		Detail:    "page 42 is corrupt",
		CheckedAt: time.Now(),
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "degraded", body["status"])
	// Neither the facet nor the stored detail reach the unauthenticated body.
	_, hasChecks := body["checks"]
	require.False(t, hasChecks)
	require.NotContains(t, w.Body.String(), "page 42 is corrupt")
}

func TestDeepHealth_DegradedOnStuckJobLock(t *testing.T) {
	db, _, r := migratedHealthRouter(t)

	locked := time.Now().Add(-30 * time.Minute)
	require.NoError(t, db.Create(&models.JobExecution{
		JobName:   "calendar_sync",
		LastRunAt: locked,
		LockedAt:  &locked,
		LockedBy:  "dead-worker",
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "degraded", body["status"])
	// The stuck job still degrades the roll-up, but its name does not leak.
	require.NotContains(t, w.Body.String(), "calendar_sync")
}

func TestDeepHealth_StillUnhealthyWhenDatabaseDown(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	sqlDB, _ := db.DB()
	require.NoError(t, sqlDB.Close())

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "unhealthy", body["status"])
}

// TestDeepHealth_ResponseBodyOmitsFacetBreakdown is the issue #864 regression
// guard: with every facet-degrading condition in play (integrity check
// enabled but failing, a stuck job lock, SMTP + FCM configured) the
// unauthenticated /health body still carries no per-facet breakdown at all —
// no checks object, no internal job names, no integration keys, no
// integrity/restore-drill state, and none of the operator secrets that the
// deep snapshot's reason strings are sanitized against. It stays a useful
// one-word degraded report.
func TestDeepHealth_ResponseBodyOmitsFacetBreakdown(t *testing.T) {
	db, cfg, r := migratedHealthRouter(t)
	cfg.DBIntegrityCheckEnabled = true
	cfg.UseSMTP = true
	cfg.SMTPHost = "sekret-internal-mailhost.corp.example"
	cfg.SMTPPort = 2525
	cfg.FCMServiceAccountFile = "/very/secret/path/fcm-service-account.json"

	require.NoError(t, db.Create(&models.OperationalCheckResult{
		CheckName: models.JobNameDBIntegrityCheck,
		Status:    models.OpCheckStatusFailed,
		Detail:    "contacts: live=1234 restored=1200; secret_table page 7 malformed",
		CheckedAt: time.Now(),
	}).Error)
	locked := time.Now().Add(-30 * time.Minute)
	require.NoError(t, db.Create(&models.JobExecution{
		JobName: "calendar_sync", LastRunAt: locked, LockedAt: &locked, LockedBy: "dead-worker",
	}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	raw := w.Body.String()

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &body))
	_, hasChecks := body["checks"]
	require.False(t, hasChecks, "issue #864: no per-facet checks object on the unauthenticated /health")

	for _, needle := range []string{
		"checks",                           // the breakdown container
		"background_jobs",                  // facet name
		"calendar_sync",                    // internal job name
		"integrity_check", "restore_drill", // facet names
		"data_integrity", "integrations", // facet names
		"last_run_at", "stuck", // per-job fields
		"sekret-internal-mailhost.corp.example", // SMTP host
		"/very/secret/path",                     // FCM file path
		"contacts: live=1234",                   // restore-drill row counts
		"secret_table", "page 7 malformed",      // integrity-check schema hint
		"dial tcp", // raw net error text
	} {
		require.NotContainsf(t, raw, needle, "unauthenticated /health body must not expose %q", needle)
	}
	// Still a useful report.
	require.Equal(t, "degraded", body["status"])
	require.NotEmpty(t, body["version"])
}

// Sanity: the body still decodes into the typed HealthResponse (the pre-split
// flat shape — status/database/version — is preserved; only checks is gone).
func TestDeepHealth_TypedShapeUnchanged(t *testing.T) {
	_, _, r := migratedHealthRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "healthy", resp.Status)
	require.Equal(t, "healthy", resp.Database.Status)
	require.NotEmpty(t, resp.Version)
}
