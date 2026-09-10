package controllers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mycorrhizal/buildinfo"
	"mycorrhizal/config"
	"mycorrhizal/database"
	"mycorrhizal/metrics"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetSystemStatus returns the authenticated, admin-scoped operational snapshot
// (issue #388): rolled-up deep-health status, the full deep-health breakdown,
// build identity, process uptime, migration numbers, a live config-validation
// read-back, the enabled feature-flag set, SQLite operational facts, storage
// sizing plus the storage-growth trend and tiered threshold (issue #652), and
// the opt-in update-availability block (issue #650).
//
// This is the counterpart to the unauthenticated GET /health surface: /health
// reports only the rolled-up status word plus build identity and the
// client-compatibility fields, and this endpoint is the sole home of the
// per-facet deep-health breakdown (job names, integrity-check /
// restore-drill / data-integrity state, integration reachability — issue
// #864), the migration numbers and the storage facts. It is also the
// counterpart to GET /metrics, which is Prometheus-only behind METRICS_TOKEN.
// Like the other /admin diagnostic endpoints it is instance-wide (not
// user-scoped), read-only, and gated by AuthMiddleware + AdminMiddleware. The
// one exception to "no writes": the storage threshold computation persists its
// current tier into the alert_states row keyed storage_threshold, so the -5%
// hysteresis band survives restarts (the same state diskSpaceCondition keeps
// there).
//
// No secret (JWT secret, metrics token, SMTP host, OIDC client secret) appears
// anywhere in the response: the feature block is booleans only, and
// config.ValidationError carries a field name plus a remediation message, never
// the offending value.
func GetSystemStatus(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	cfg := currentConfig(c)

	deep := services.DeepHealthSnapshot(db, cfg)

	storage := systemStatusStorage(db, cfg)

	// Fold the storage threshold into the overall status (issue #652 item 4):
	// warning or critical elevates a healthy overall to degraded. Never to
	// unhealthy — a full disk does not mean the DB read failed; that word is
	// reserved for DeepHealthSnapshot's own roll-up.
	overall := overallWithStorageThreshold(deep.Status, storage.Threshold)

	resp := SystemStatusResponse{
		Overall:   overall,
		Health:    deep,
		Version:   buildinfo.Get(),
		Uptime:    systemStatusUptime(),
		Migration: systemStatusMigration(db),
		Config:    systemStatusConfig(cfg),
		Database:  systemStatusDatabase(db, cfg),
		Storage:   storage,
		Update:    services.BuildUpdateCheckStatus(c.Request.Context(), cfg),
	}

	c.JSON(http.StatusOK, resp)
}

// SystemStatusResponse is the GET /admin/system-status body.
type SystemStatusResponse struct {
	Overall   string                `json:"overall"` // healthy | degraded | unhealthy
	Health    services.DeepHealth   `json:"health"`
	Version   buildinfo.Info        `json:"version"`
	Uptime    SystemStatusUptime    `json:"uptime"`
	Migration SystemStatusMigration `json:"migration"`
	Config    SystemStatusConfig    `json:"config"`
	Database  SystemStatusDatabase  `json:"database"`
	Storage   SystemStatusStorage   `json:"storage"`
	// Update is the opt-in update-availability block (issue #650): enabled is
	// the config flag, and current/latest/update_available/checked_at are only
	// populated when the flag is on and a lookup succeeded. When disabled the
	// block is just {enabled: false} and no outbound call is made.
	Update services.UpdateCheckStatus `json:"update"`
}

// SystemStatusUptime reports when the process started and how long ago.
type SystemStatusUptime struct {
	StartedAt     string `json:"started_at"` // RFC3339
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// SystemStatusMigration is the schema-version picture: how far the database is
// migrated, how far the binary could take it, and whether the last migration
// left the version dirty.
type SystemStatusMigration struct {
	Applied uint `json:"applied"`
	Latest  uint `json:"latest"`
	Pending int  `json:"pending"`
	Dirty   bool `json:"dirty"`
}

// SystemStatusConfig is the live config read-back: what cfg.Validate() reports
// right now, plus the set of optional subsystems currently enabled.
type SystemStatusConfig struct {
	// Validation is never nil — it marshals as [] on a clean boot (frontend
	// trap #8). Each entry is a field name and a remediation message; no
	// entry carries the offending value.
	Validation []SystemStatusConfigValidation `json:"validation"`
	Features   SystemStatusFeatures           `json:"features"`
}

// SystemStatusConfigValidation mirrors config.ValidationError on the wire.
type SystemStatusConfigValidation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// SystemStatusFeatures is the enabled-or-not state of each optional subsystem.
// Booleans only — this is a capability map, never a config dump.
type SystemStatusFeatures struct {
	CardDAV          bool `json:"carddav"`
	CalDAV           bool `json:"caldav"`
	OIDC             bool `json:"oidc"`
	Email            bool `json:"email"`
	Metrics          bool `json:"metrics"`
	DBIntegrityCheck bool `json:"db_integrity_check"`
	DBRestoreDrill   bool `json:"db_restore_drill"`
}

// SystemStatusDatabase carries SQLite operational facts that /health withholds
// from an unauthenticated caller.
type SystemStatusDatabase struct {
	SQLiteVersion string `json:"sqlite_version"`
	JournalMode   string `json:"journal_mode"`
	WALBytes      int64  `json:"wal_bytes"` // size of the -wal sidecar, 0 when absent
}

// SystemStatusStorage is on-disk sizing: the database footprint, the
// filesystem holding it, each configured storage directory, and the derived
// storage-growth trend (issue #652).
type SystemStatusStorage struct {
	DatabaseBytes int64                  `json:"database_bytes"`
	Filesystem    SystemStatusFilesystem `json:"filesystem"`
	// Directories is never nil — it marshals as [] when no storage directory
	// is configured (frontend trap #8).
	Directories []services.DirectoryUsage `json:"directories"`

	// Storage-growth trend block (issue #652), derived from the persisted
	// storage_samples series. Pointers so "not enough history" marshals as
	// null — the frontend renders an em dash for those.
	Growth7DBytes   *int64     `json:"growth_7d_bytes"`
	Growth30DBytes  *int64     `json:"growth_30d_bytes"`
	Growth90DBytes  *int64     `json:"growth_90d_bytes"`
	ProjectedFullAt *time.Time `json:"projected_full_at"`

	// UsagePercent is the filesystem holding the DB, used as a percentage of
	// total; null when the filesystem cannot be stat'ed. Threshold folds it
	// against STORAGE_WARN_PERCENT / STORAGE_CRITICAL_PERCENT (with -5%
	// hysteresis) into ok | warning | critical.
	UsagePercent *int   `json:"usage_percent"`
	Threshold    string `json:"threshold"`
}

// SystemStatusFilesystem is free/total bytes of the filesystem holding the
// database file.
type SystemStatusFilesystem struct {
	FreeBytes  int64 `json:"free_bytes"`
	TotalBytes int64 `json:"total_bytes"`
}

// overallWithStorageThreshold folds the storage threshold into the overall
// status (issue #652 item 4): warning or critical elevates a healthy overall
// to degraded. Never to unhealthy — a full disk does not mean the DB read
// failed; DeepStatusUnhealthy stays reserved for DeepHealthSnapshot's own
// roll-up. Any other deep-health status passes through untouched.
func overallWithStorageThreshold(deepStatus, threshold string) string {
	if threshold == services.StorageThresholdWarning || threshold == services.StorageThresholdCritical {
		if deepStatus == "healthy" {
			return services.DeepStatusDegraded
		}
	}
	return deepStatus
}

func systemStatusUptime() SystemStatusUptime {
	start := metrics.ProcessStart()
	return SystemStatusUptime{
		StartedAt:     start.UTC().Format(time.RFC3339),
		UptimeSeconds: int64(time.Since(start).Seconds()),
	}
}

func systemStatusMigration(db *gorm.DB) SystemStatusMigration {
	out := SystemStatusMigration{}

	applied, dirty, ok, err := database.AppliedMigrationVersion(db)
	if err == nil && ok {
		out.Applied = applied
		out.Dirty = dirty
	}

	if latest, err := database.LatestMigrationVersion(); err == nil {
		out.Latest = latest
	}

	if out.Latest > out.Applied {
		out.Pending = int(out.Latest - out.Applied)
	}
	return out
}

func systemStatusConfig(cfg config.Config) SystemStatusConfig {
	validation := make([]SystemStatusConfigValidation, 0)
	for _, ve := range cfg.Validate() {
		validation = append(validation, SystemStatusConfigValidation{
			Field:   ve.Field,
			Message: ve.Message,
		})
	}

	return SystemStatusConfig{
		Validation: validation,
		Features: SystemStatusFeatures{
			CardDAV:          cfg.CardDAVEnabled,
			CalDAV:           cfg.CalDAVEnabled,
			OIDC:             cfg.OIDC.Enabled,
			Email:            cfg.EmailEnabled(),
			Metrics:          cfg.MetricsToken != "",
			DBIntegrityCheck: cfg.DBIntegrityCheckEnabled,
			DBRestoreDrill:   cfg.DBRestoreDrillEnabled,
		},
	}
}

func systemStatusDatabase(db *gorm.DB, cfg config.Config) SystemStatusDatabase {
	var out SystemStatusDatabase

	// Best-effort reads: a diagnostic endpoint should degrade to blank fields,
	// not 500, if a PRAGMA momentarily fails.
	_ = db.Raw("SELECT sqlite_version()").Scan(&out.SQLiteVersion).Error
	_ = db.Raw("PRAGMA journal_mode").Scan(&out.JournalMode).Error

	if fi, err := os.Stat(cfg.DBPath + "-wal"); err == nil {
		out.WALBytes = fi.Size()
	}
	return out
}

func systemStatusStorage(db *gorm.DB, cfg config.Config) SystemStatusStorage {
	free, total, ok := metrics.FilesystemBytes(filepath.Dir(cfg.DBPath))
	fs := SystemStatusFilesystem{}
	if ok {
		fs.FreeBytes = int64(free)
		fs.TotalBytes = int64(total)
	}

	dirs := services.StorageUsage([]string{cfg.ProfilePhotoDir, cfg.AttachmentsDir})

	out := SystemStatusStorage{
		DatabaseBytes: metrics.DatabaseBytes(cfg.DBPath),
		Filesystem:    fs,
		Directories:   dirs,
	}

	// Storage-growth trend from the persisted samples (issue #652). Best-effort
	// reads: a diagnostic endpoint degrades to nulls, not a 500.
	trend := services.ComputeStorageTrend(context.Background(), db, time.Now())
	out.Growth7DBytes = trend.Growth7DBytes
	out.Growth30DBytes = trend.Growth30DBytes
	out.Growth90DBytes = trend.Growth90DBytes
	out.ProjectedFullAt = trend.ProjectedFullAt

	// Tiered threshold with -5% hysteresis. Only meaningful when the
	// filesystem can be stat'ed; otherwise usage_percent stays null and the
	// threshold is a plain ok.
	if ok && total > 0 {
		usedPct := int((total - free) / total * 100)
		out.UsagePercent = &usedPct
		out.Threshold = services.StorageThreshold(context.Background(), db, usedPct, cfg.StorageWarnPercent, cfg.StorageCriticalPercent)
	} else {
		out.Threshold = services.StorageThresholdOK
	}
	return out
}
