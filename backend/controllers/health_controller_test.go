package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// migratedHealthRouter builds a gin engine with the three health routes,
// backed by a REAL migrated database (CLAUDE.md backend trap #1 — the deep and
// readiness checks read schema_migrations / job_executions /
// operational_check_results, none of which an AutoMigrate test schema has).
// cfg is returned by pointer so a test can tweak it before serving.
func migratedHealthRouter(t *testing.T) (*gorm.DB, *config.Config, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)

	cfg := &config.Config{
		ProfilePhotoDir: t.TempDir(),
		// The scheduled self-checks are off by default here so the baseline
		// deep /health is "healthy"; the tests that care flip them on.
		DBIntegrityCheckEnabled:       false,
		DBRestoreDrillEnabled:         false,
		DBIntegrityCheckIntervalHours: 24,
		DBRestoreDrillIntervalHours:   168,
	}

	// No cross-test cache bleed, and every call recomputes.
	services.ResetDeepHealthCache()
	services.SetDeepHealthCacheTTL(0)
	t.Cleanup(func() { services.SetDeepHealthCacheTTL(30 * time.Second) })

	r := gin.New()
	// Read *cfg per request so a test can mutate cfg after this helper returns
	// and the handler sees it.
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	r.GET("/health", HealthCheck)
	r.GET("/health/live", LivenessCheck)
	r.GET("/health/ready", ReadinessCheck)
	return db, cfg, r
}

func getJSON(t *testing.T, r *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body: %s", w.Body.String())
	return w.Code, body
}

func TestHealthCheck(t *testing.T) {
	_, _, r := migratedHealthRouter(t)

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "healthy", body["status"])

	db, _ := body["database"].(map[string]any)
	require.Equal(t, "healthy", db["status"])
	require.NotEmpty(t, body["version"])
	require.NotEmpty(t, body["timestamp"])

	// The per-facet deep-health breakdown is admin-only (issue #864): the
	// unauthenticated endpoint carries the rolled-up status word and build
	// identity, never the checks object.
	_, hasChecks := body["checks"]
	require.False(t, hasChecks, "deep facet breakdown must not be on the unauthenticated /health")
}

func TestHealthCheck_UnhealthyDatabase(t *testing.T) {
	db, _, r := migratedHealthRouter(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "unhealthy", body["status"])

	dbFacet, _ := body["database"].(map[string]any)
	require.Equal(t, "unhealthy", dbFacet["status"])
}
