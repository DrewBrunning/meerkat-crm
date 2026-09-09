package controllers

import (
	"net/http"
	"time"

	"mycorrhizal/buildinfo"
	"mycorrhizal/database"
	"mycorrhizal/logger"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The health surface is three endpoints, each answering a different operator
// question (issue #421):
//
//   - GET /health/live  — liveness. Is the process up? Answers instantly,
//     touches nothing. This is what a restart policy hits; a slow or failing
//     dependency must never make it fail, or the orchestrator restarts a
//     healthy app.
//   - GET /health/ready — readiness. Can THIS instance serve? Checks DB
//     connectivity, migration state, and required filesystem access. 503 while
//     any of those is not satisfied. This is what a load balancer gates traffic
//     on.
//   - GET /health       — deep health. Is the CRM actually operational?
//     Everything /ready checks, plus persisted integrity-check / restore-drill
//     outcomes, background-job locks, and server-scoped integration
//     reachability, rolled up into a single healthy | degraded | unhealthy
//     word. Only a database read failure yields 503. "degraded" (an optional
//     integration is down, a scheduled job is stale) is still 200 —
//     degraded-but-alive is not down.
//
// All three are unauthenticated and carry no secrets, matching the original
// single /health. The deep endpoint reports only the rolled-up status plus
// build/compatibility identity — NOT the per-facet breakdown (job names,
// integrity-check / restore-drill / data-integrity reasons, integration
// reachability). That breakdown names internal components and operational
// state, so it is admin-only: GET /api/v1/admin/system-status returns the
// full services.DeepHealth snapshot as its "health" field (issue #864,
// pen-test engagement #860 finding F-4).

// HealthResponse is the deep GET /health body. The flat database/version
// fields are retained for backward compatibility with the pre-split endpoint.
// The per-facet services.DeepHealth breakdown is deliberately not included
// here — it is admin-only at GET /api/v1/admin/system-status (issue #864).
type HealthResponse struct {
	Status    string         `json:"status"` // healthy | degraded | unhealthy
	Timestamp string         `json:"timestamp"`
	Database  DatabaseHealth `json:"database"`
	Version   string         `json:"version"`
	Commit    string         `json:"commit,omitempty"`
	BuildDate string         `json:"build_date,omitempty"`
	// MinClientVersion is the oldest client versionName this server still
	// supports, declared via the MIN_CLIENT_VERSION env knob. Absent (the
	// default) means no floor has ever been raised — every released client
	// stays compatible (docs/client-compatibility-policy.md, issue #528).
	// It is a MAINT-02 breaking-change surface: a floor moves only when a
	// genuinely breaking API change strands older clients.
	MinClientVersion string `json:"min_client_version,omitempty"`
	// APIContractVersion is the API contract generation this server speaks.
	// "v1" while the API is on /api/v1; it exists so a future "v2" can be
	// announced here before any client is required to react to it
	// (docs/client-compatibility-policy.md §The API versioning promise).
	// Deliberately not omitempty: it is part of the compatibility contract
	// and must always be present so a client can distinguish "v1" from a
	// server that predates the field entirely.
	APIContractVersion string `json:"api_contract_version"`
}

// DatabaseHealth represents the database health status
type DatabaseHealth struct {
	Status       string  `json:"status"`
	ResponseTime float64 `json:"response_time_ms"`
}

// LivenessResponse is the GET /health/live body.
type LivenessResponse struct {
	Status string `json:"status"` // always "live"
}

// ReadinessResponse is the GET /health/ready body.
type ReadinessResponse struct {
	Status string                          `json:"status"` // ready | not_ready
	Checks map[string]ReadinessCheckDetail `json:"checks"`
}

// ReadinessCheckDetail is one readiness facet.
type ReadinessCheckDetail struct {
	Status string `json:"status"` // ok | failed
	Reason string `json:"reason,omitempty"`
}

// LivenessCheck handles GET /health/live. It must not touch the database, the
// filesystem, or config — only that the process is running and serving.
func LivenessCheck(c *gin.Context) {
	c.JSON(http.StatusOK, LivenessResponse{Status: "live"})
}

// ReadinessCheck handles GET /health/ready.
func ReadinessCheck(c *gin.Context) {
	checks := map[string]ReadinessCheckDetail{
		"database":   readinessDatabase(c),
		"migrations": readinessMigrations(c),
		"filesystem": readinessFilesystem(c),
	}

	status := "ready"
	httpStatus := http.StatusOK
	for _, ck := range checks {
		if ck.Status != "ok" {
			status = "not_ready"
			httpStatus = http.StatusServiceUnavailable
			break
		}
	}

	c.JSON(httpStatus, ReadinessResponse{Status: status, Checks: checks})
}

func readinessDatabase(c *gin.Context) ReadinessCheckDetail {
	db, ok := dbFromContext(c)
	if !ok {
		return ReadinessCheckDetail{Status: "failed", Reason: "no database handle"}
	}
	h := checkDatabaseHealth(db)
	if h.Status != "healthy" {
		return ReadinessCheckDetail{Status: "failed", Reason: "database is unreachable"}
	}
	return ReadinessCheckDetail{Status: "ok"}
}

func readinessMigrations(c *gin.Context) ReadinessCheckDetail {
	db, ok := dbFromContext(c)
	if !ok {
		return ReadinessCheckDetail{Status: "failed", Reason: "no database handle"}
	}
	applied, dirty, ok, err := database.AppliedMigrationVersion(db)
	if err != nil {
		logger.Error().Err(err).Msg("readiness: cannot read migration state")
		return ReadinessCheckDetail{Status: "failed", Reason: "cannot read migration state"}
	}
	if !ok {
		return ReadinessCheckDetail{Status: "failed", Reason: "no migrations have been applied"}
	}
	if dirty {
		return ReadinessCheckDetail{Status: "failed", Reason: "migration is in a dirty state"}
	}
	latest, err := database.LatestMigrationVersion()
	if err != nil {
		logger.Error().Err(err).Msg("readiness: cannot resolve latest migration version")
		return ReadinessCheckDetail{Status: "failed", Reason: "cannot resolve latest migration version"}
	}
	if applied < latest {
		return ReadinessCheckDetail{Status: "failed", Reason: "pending migrations (schema is behind the binary)"}
	}
	if applied > latest {
		// The database knows migrations this binary does not — a rollback in
		// progress (issue #439 state 2). The startup path refuses to boot on
		// this state; if it is somehow reached at runtime, readiness must gate
		// traffic off, matching services/deep_health.go migrationLagCheck
		// rather than silently reporting ready.
		return ReadinessCheckDetail{Status: "failed", Reason: "schema is ahead of the binary (this binary was rolled back)"}
	}
	return ReadinessCheckDetail{Status: "ok"}
}

func readinessFilesystem(c *gin.Context) ReadinessCheckDetail {
	cfg := currentConfig(c)
	for label, dir := range map[string]string{
		"profile photo directory": cfg.ProfilePhotoDir,
		"attachments directory":   cfg.AttachmentsDir,
	} {
		if dir == "" {
			continue
		}
		if reason := services.ProbeWritableDir(dir); reason != "" {
			// The absolute path and errno go to the log, not the
			// unauthenticated response body (ASVS 7.4.1).
			logger.Error().Str("dir", dir).Msg("readiness: " + label + " " + reason)
			return ReadinessCheckDetail{Status: "failed", Reason: label + " " + reason}
		}
	}
	return ReadinessCheckDetail{Status: "ok"}
}

// apiContractVersion is the API contract generation the current route table
// speaks (routes.go registers the whole surface under /api/v1). Kept as a
// single documented constant next to the response field it populates:
// changing it announces a new contract generation on GET /health before any
// client is required to react to it (docs/client-compatibility-policy.md).
const apiContractVersion = "v1"

// HealthCheck handles the deep health check endpoint, GET /health.
//
// The full services.DeepHealth snapshot is still computed here — it drives the
// rolled-up status word and the 503-only-on-database-read-failure semantics —
// but the per-facet breakdown is NOT serialized: it names internal jobs,
// integrity/restore-drill state and integration reachability, which is
// admin-only (GET /api/v1/admin/system-status, issue #864).
func HealthCheck(c *gin.Context) {
	db, _ := dbFromContext(c)
	cfg := currentConfig(c)

	dbHealth := DatabaseHealth{Status: "unhealthy"}
	var deep services.DeepHealth
	if db != nil {
		dbHealth = checkDatabaseHealth(db)
		deep = services.DeepHealthSnapshot(db, cfg)
	} else {
		deep.Status = services.DeepStatusUnhealthy
		deep.Database = services.HealthCheckDetail{Status: services.DeepStatusUnhealthy, Reason: "no database handle"}
	}

	httpStatus := http.StatusOK
	if deep.Status == services.DeepStatusUnhealthy {
		httpStatus = http.StatusServiceUnavailable
	}

	build := buildinfo.Get()
	c.JSON(httpStatus, HealthResponse{
		Status:             deep.Status,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Database:           dbHealth,
		Version:            build.Version,
		Commit:             build.Commit,
		BuildDate:          build.BuildDate,
		MinClientVersion:   cfg.MinClientVersion,
		APIContractVersion: apiContractVersion,
	})
}

// dbFromContext safely pulls the *gorm.DB the middleware injects, without the
// panic c.MustGet would raise on a misconfigured router — a health endpoint
// should report "unavailable", not 500.
func dbFromContext(c *gin.Context) (*gorm.DB, bool) {
	v, exists := c.Get("db")
	if !exists {
		return nil, false
	}
	db, ok := v.(*gorm.DB)
	return db, ok
}

// checkDatabaseHealth checks if the database is accessible and responsive
func checkDatabaseHealth(db *gorm.DB) DatabaseHealth {
	start := time.Now()

	sqlDB, err := db.DB()
	if err != nil {
		return DatabaseHealth{
			Status:       "unhealthy",
			ResponseTime: 0,
		}
	}

	// Ping the database
	err = sqlDB.Ping()
	duration := time.Since(start).Milliseconds()

	if err != nil {
		return DatabaseHealth{
			Status:       "unhealthy",
			ResponseTime: float64(duration),
		}
	}

	return DatabaseHealth{
		Status:       "healthy",
		ResponseTime: float64(duration),
	}
}
