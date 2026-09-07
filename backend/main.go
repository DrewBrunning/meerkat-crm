package main

import (
	"context"
	"errors"
	"fmt"
	"mycorrhizal/atrest"
	"mycorrhizal/buildinfo"
	"mycorrhizal/config"
	"mycorrhizal/database"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/i18n"
	"mycorrhizal/internal/fsguard"
	"mycorrhizal/logger"
	"mycorrhizal/metrics"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/routes"
	"mycorrhizal/services"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-co-op/gocron"
	"gorm.io/gorm"
)

// runJob executes fn under a per-run correlation ID ("job:<name>:<uuid>") with
// panic recovery and standardized start/finish logging (issue #425), and
// persists one job_runs row per invocation (issue #391): job name, trigger,
// duration, and outcome — success, failure (a returned error or a recovered
// panic), or skipped (fn returns services.ErrJobSkipped: the job lock was held
// or it ran too recently). A recovered panic is additionally recorded as a
// job_failed operational event so it lands on the admin timeline (issue #424);
// the ordinary completion stays a log line + the job_runs row, not a
// system_events row, since the scheduler ticks often and a per-tick event
// would swamp that stream.
//
// jobName is the canonical models.JobName* token (so job_runs history groups
// per job regardless of which trigger fired it); trigger is
// models.JobTrigger{Scheduled,Initial}.
func runJob(db *gorm.DB, jobName, trigger string, fn func() error) {
	execJob(db, jobName, trigger, func() (*int, error) { return nil, fn() })
}

// runJobReport is runJob for a job that also reports how many items the run
// acted on (reminders sent, suggestions created) — persisted as
// job_runs.items_processed.
func runJobReport(db *gorm.DB, jobName, trigger string, fn func() (int, error)) {
	execJob(db, jobName, trigger, func() (*int, error) {
		n, err := fn()
		return &n, err
	})
}

// execJob is the shared implementation behind runJob / runJobReport.
func execJob(db *gorm.DB, jobName, trigger string, fn func() (*int, error)) {
	ctx := logger.JobContext(jobName)
	start := time.Now()
	logger.Ctx(ctx).Info().
		Str(logger.FieldEvent, models.SysEventJobStarted).
		Str(logger.FieldOperation, jobName).
		Str("trigger", trigger).
		Msg("scheduled job started")

	var (
		items  *int
		jobErr error
		panicV any
	)
	func() {
		defer func() { panicV = recover() }()
		items, jobErr = fn()
	}()

	durMS := time.Since(start).Milliseconds()
	result := logger.ResultSuccess
	errStr := ""

	switch {
	case panicV != nil:
		result = logger.ResultFailure
		errStr = fmt.Sprintf("panic: %v", panicV)
		items = nil
		logger.Ctx(ctx).Error().
			Str(logger.FieldEvent, models.SysEventJobFailed).
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Str(logger.FieldOperation, jobName).
			Str(logger.FieldResult, logger.ResultFailure).
			Int64(logger.FieldDurationMS, durMS).
			Interface("panic", panicV).
			Str("stack", string(debug.Stack())).
			Msg("scheduled job panicked — recovered")
		models.RecordSystemEvent(ctx, db, models.SystemEvent{
			EventType:  models.SysEventJobFailed,
			Component:  logger.ComponentScheduler,
			Operation:  jobName,
			Result:     models.SysResult(logger.ResultFailure),
			DurationMS: &durMS,
			Error:      errStr,
		})
	case errors.Is(jobErr, services.ErrJobSkipped):
		result = logger.ResultSkipped
		items = nil
		logger.Ctx(ctx).Info().
			Str(logger.FieldEvent, models.SysEventJobCompleted).
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Str(logger.FieldOperation, jobName).
			Str(logger.FieldResult, logger.ResultSkipped).
			Int64(logger.FieldDurationMS, durMS).
			Msg("scheduled job skipped")
	case jobErr != nil:
		result = logger.ResultFailure
		errStr = jobErr.Error()
		items = nil
		logger.Ctx(ctx).Error().
			Str(logger.FieldEvent, models.SysEventJobFailed).
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Str(logger.FieldOperation, jobName).
			Str(logger.FieldResult, logger.ResultFailure).
			Int64(logger.FieldDurationMS, durMS).
			Err(jobErr).
			Msg("scheduled job failed")
	default:
		logger.Ctx(ctx).Info().
			Str(logger.FieldEvent, models.SysEventJobCompleted).
			Str(logger.FieldComponent, logger.ComponentScheduler).
			Str(logger.FieldOperation, jobName).
			Str(logger.FieldResult, logger.ResultSuccess).
			Int64(logger.FieldDurationMS, durMS).
			Msg("scheduled job completed")
	}

	// Prometheus counters (issue #389): job_runs_total{job,result} +
	// job_duration_seconds{job}. result is already the folded outcome
	// (success / failure / skipped).
	metrics.JobRun(jobName, result, float64(durMS)/1000.0)

	models.RecordJobRun(ctx, db, models.JobRun{
		JobName:        jobName,
		Trigger:        trigger,
		StartedAt:      start,
		FinishedAt:     start.Add(time.Duration(durMS) * time.Millisecond),
		DurationMS:     durMS,
		Result:         result,
		Error:          errStr,
		ItemsProcessed: items,
	})
}

// safeGo runs fn in a goroutine via runJob (panic recovery + correlation ID +
// job_runs row), so an unhandled panic in a background task doesn't crash the
// server.
func safeGo(db *gorm.DB, jobName, trigger string, fn func() error) {
	go runJob(db, jobName, trigger, fn)
}

// safeGoReport is safeGo for a job that reports an item count (see runJobReport).
func safeGoReport(db *gorm.DB, jobName, trigger string, fn func() (int, error)) {
	go runJobReport(db, jobName, trigger, fn)
}

// recoverJob wraps fn for a recurring gocron registration (s.Every(...).Do(...)).
// gocron invokes the function it's given directly and, in the pinned v1.37.0,
// does not recover a job's own panics unless gocron.SetPanicHandler is set
// globally (it isn't here). Without this, a panic on any scheduled invocation
// would crash the whole process on its next scheduled tick. Always pass the
// wrapped result to .Do(), never fn itself.
func recoverJob(db *gorm.DB, jobName, trigger string, fn func() error) func() {
	return func() { runJob(db, jobName, trigger, fn) }
}

// recoverJobReport is recoverJob for a job that reports an item count.
func recoverJobReport(db *gorm.DB, jobName, trigger string, fn func() (int, error)) func() {
	return func() { runJobReport(db, jobName, trigger, fn) }
}

func main() {
	// Initialize logger first
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	isPretty := os.Getenv("LOG_PRETTY")
	prettyLog := isPretty == "true" || isPretty == "1"

	// In development, use pretty logs by default
	if os.Getenv("GIN_MODE") != "release" {
		prettyLog = true
	}

	logger.InitLogger(logger.Config{
		Level:  logLevel,
		Pretty: prettyLog,
	})

	logger.Info().Msg("Loading server...")

	logger.Info().Msg("Loading configuration...")
	cfg := config.LoadConfig()

	logger.Info().Msg("Validating configuration...")
	cfg.ValidateOrPanic()

	// M2: config.Validate only checks that FCM_SERVICE_ACCOUNT_FILE exists
	// (config cannot import services without an import cycle). The content
	// check — valid JSON with project_id/client_email/private_key — lives
	// here so a malformed service-account file still fails boot, per the
	// M2 design decision ("reject at boot rather than failing the first
	// send"), instead of only surfacing as a warning on the first reminder run.
	if cfg.FCMServiceAccountFile != "" {
		if _, err := services.LoadFCMServiceAccount(cfg.FCMServiceAccountFile); err != nil {
			logger.Fatal().Err(err).Msg("Invalid FCM service account file")
		}
	}

	logger.Info().Msg("Loading database and running migrations...")
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}

	// COMPAT-01 (issue #472): warn loudly, but do not refuse to boot, when
	// the database lives on a filesystem type known to risk SQLite WAL
	// corruption (NFS/SMB/CIFS/etc — see internal/fsguard's doc comment for
	// why this is advisory rather than fatal).
	if warning := fsguard.NetworkFilesystemWarning(cfg.DBPath); warning != nil {
		logger.Warn().
			Str("path", warning.Path).
			Str("filesystem", warning.FilesystemName).
			Msg(warning.String())
	}

	// T18 audit trail: give the GORM hooks a standalone session for their
	// fire-and-forget audit writes (never the hook's transaction).
	models.RegisterAuditDB(db)

	// Field-level at-rest encryption (issue #380): load the wrapped DEK under
	// the master key, then encrypt any rows written before encryption was
	// enabled (idempotent, row-count-preserving). A wrong DATA_ENCRYPTION_KEY
	// fails closed here — the wrapped DEK cannot be unwrapped and boot aborts
	// before any data is served. See backend/atrest/atrest.go. This must run
	// before the audit hash chain backfill below: the chain hashes the
	// logical (decrypted) audit_events.before_snapshot value via the GORM
	// serializer, so encryption needs to be armed first.
	{
		kek, err := atrest.EncryptionKey()
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to resolve at-rest encryption master key")
		}
		if err := atrest.Initialize(db, kek); err != nil {
			logger.Fatal().Err(err).Msg("Failed to initialize at-rest encryption")
		}
		if err := atrest.Backfill(db); err != nil {
			logger.Fatal().Err(err).Msg("Failed to backfill at-rest encryption")
		}
	}

	// I18N-02 Unicode NFC normalization (issue #485): rewrite any stored
	// contact text still in decomposed (NFD) form — written before the write
	// boundary started normalizing — to NFC, exactly once per database (see
	// migration 000052's data_backfills ledger). It reads and rewrites the
	// canonical records through the Contact model, so it must run after at-rest
	// encryption is armed (the card columns are encrypted when armed) and
	// before the audit hash chain recompute below, so the audit rows this
	// maintenance write appends are covered by the re-linked chain.
	if _, err := services.NormalizeContactRecordsToNFC(db); err != nil {
		logger.Fatal().Err(err).Msg("Failed to backfill Unicode NFC normalization")
	}

	// T18 audit hash chain (issue #381): backfill hash/prev_hash for any rows
	// written before migration 000034 and re-link after any purge. Idempotent
	// and write-free once the chain is consistent; failing closed on error
	// keeps the tamper-evidence property from silently degrading.
	if err := models.RecomputeAuditChain(db); err != nil {
		logger.Fatal().Err(err).Msg("Failed to backfill the audit hash chain")
	}

	logger.Info().Msg("Initializing i18n translations...")
	if err := i18n.Init(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize i18n")
	}

	logger.Info().Msg("Running scheduler...")
	// Schedule the reminder task daily
	if !cfg.UseResend {
		logger.Warn().Msg("No Mails to be sent since Resend configuration is not set")
	}
	s := gocron.NewScheduler(cfg.GetReminderLocation())

	// The scheduler's location is the configured reminder zone, so every
	// .At() wall-clock time below is that one server-wide clock (docs/adrs/
	// 0015-temporal-semantics.md "local wall time"). DST semantics for the
	// daily job: a fire time in a spring-forward gap fires once, normalized to
	// the first valid occurrence of the label under the post-transition
	// offset; a fire time in a fall-back fold fires once, on the earlier pass
	// — both inherited from gocron + Go time.Date and pinned for DATE-02
	// (issue #483). A wholly missed fire day is not replayed by the scheduler
	// itself; each job's paired boot-time Initial trigger below + acquireJobLock
	// implement ADR 0011's catch-up-with-de-duplication.

	// Every scheduled job runs through runJob / runJobReport (main.go's wrapper),
	// which times it, recovers a panic, and persists one job_runs row per
	// invocation (issue #391): job name, trigger, duration, and outcome. The
	// jobName is the canonical models.JobName* token so history groups per job
	// regardless of which trigger fired it.

	// Daily reminder digest + push-style channels. Reports the number of sends
	// that succeeded; a send failure marks the run failed (issue #391 item 3).
	reminderTask := func() (int, error) { return services.SendRemindersWithRateLimit(db, *cfg) }
	s.Every(1).Day().At(cfg.ReminderTime).Do(recoverJobReport(db, models.JobNameDailyReminders, models.JobTriggerScheduled, reminderTask))
	go safeGoReport(db, models.JobNameDailyReminders, models.JobTriggerInitial, reminderTask)

	s.Every(5).Minutes().Do(recoverJob(db, models.JobNameWebhookRetries, models.JobTriggerScheduled, func() error {
		services.ProcessWebhookRetries(db, *cfg)
		return nil
	}))

	// Sync calendar subscriptions regularly (rate-limited via job lock).
	calendarSyncTask := func() error {
		services.SyncCalendarsWithRateLimit(db, *cfg)
		return nil
	}
	s.Every(cfg.CalDAVSyncIntervalHours).Hours().Do(recoverJob(db, models.JobNameCalendarSync, models.JobTriggerScheduled, calendarSyncTask))
	go safeGo(db, models.JobNameCalendarSync, models.JobTriggerInitial, calendarSyncTask)

	// Purge soft-deleted rows past their retention window (T26).
	purgeDeletedTask := func() error {
		services.PurgeDeletedRows(db, *cfg)
		return nil
	}
	s.Every(24).Hours().Do(recoverJob(db, models.JobNamePurgeDeleted, models.JobTriggerScheduled, purgeDeletedTask))
	go safeGo(db, models.JobNamePurgeDeleted, models.JobTriggerInitial, purgeDeletedTask)

	// Purge expired audit events past their retention window (T18).
	auditPurgeTask := func() error {
		services.PurgeExpiredAuditEventsScheduled(db, *cfg)
		return nil
	}
	s.Every(24).Hours().Do(recoverJob(db, models.JobNameAuditPurge, models.JobTriggerScheduled, auditPurgeTask))
	go safeGo(db, models.JobNameAuditPurge, models.JobTriggerInitial, auditPurgeTask)

	// Purge expired system_events past their retention window (issue #424).
	systemEventPurgeTask := func() error {
		services.PurgeExpiredSystemEventsScheduled(db, *cfg)
		return nil
	}
	s.Every(24).Hours().Do(recoverJob(db, models.JobNameSystemEventPurge, models.JobTriggerScheduled, systemEventPurgeTask))
	go safeGo(db, models.JobNameSystemEventPurge, models.JobTriggerInitial, systemEventPurgeTask)

	// Purge expired job_runs past their retention window (issue #391).
	jobRunPurgeTask := func() error {
		services.PurgeExpiredJobRunsScheduled(db, *cfg)
		return nil
	}
	s.Every(24).Hours().Do(recoverJob(db, models.JobNameJobRunPurge, models.JobTriggerScheduled, jobRunPurgeTask))
	go safeGo(db, models.JobNameJobRunPurge, models.JobTriggerInitial, jobRunPurgeTask)

	// Purge expired webhook deliveries past their retention window (issue
	// #622). Job-lock guarded so a multi-instance deploy does not double-purge.
	webhookDeliveryPurgeTask := func() error {
		services.PurgeExpiredWebhookDeliveriesScheduled(db, *cfg)
		return nil
	}
	s.Every(24).Hours().Do(recoverJob(db, models.JobNameWebhookDeliveryPurge, models.JobTriggerScheduled, webhookDeliveryPurgeTask))
	go safeGo(db, models.JobNameWebhookDeliveryPurge, models.JobTriggerInitial, webhookDeliveryPurgeTask)

	// Purge expired idempotency keys past their (short) TTL window (issue
	// #459, CON-04). Runs more often than the daily purges because the window
	// itself is hours, not days. Job-lock guarded against multi-instance
	// double-purge.
	idempotencyKeyPurgeTask := func() error {
		services.PurgeExpiredIdempotencyKeysScheduled(db, *cfg)
		return nil
	}
	s.Every(6).Hours().Do(recoverJob(db, models.JobNameIdempotencyKeyPurge, models.JobTriggerScheduled, idempotencyKeyPurgeTask))
	go safeGo(db, models.JobNameIdempotencyKeyPurge, models.JobTriggerInitial, idempotencyKeyPurgeTask)

	// Emit overdue-cadence webhooks daily (T19). Job-lock guarded so a
	// multi-instance deploy does not double-fire. Reports the number emitted.
	cadenceOverdueTask := func() (int, error) { return services.ProcessOverdueCadences(db, *cfg) }
	s.Every(24).Hours().Do(recoverJobReport(db, models.JobNameCadenceOverdue, models.JobTriggerScheduled, cadenceOverdueTask))
	go safeGoReport(db, models.JobNameCadenceOverdue, models.JobTriggerInitial, cadenceOverdueTask)

	// Detect event-driven reach-out suggestions daily (issue #177). Job-lock
	// guarded so a multi-instance deploy does not double-fire. Reports the
	// number of suggestions created.
	reachOutTask := func() (int, error) { return services.DetectReachOutSuggestions(db, *cfg) }
	s.Every(24).Hours().Do(recoverJobReport(db, models.JobNameReachOutDetection, models.JobTriggerScheduled, reachOutTask))
	go safeGoReport(db, models.JobNameReachOutDetection, models.JobTriggerInitial, reachOutTask)

	// Sync Immich enrichment regularly (T16). Job-lock guarded so a
	// multi-instance deploy does not double-sync.
	immichSyncTask := func() error {
		services.SyncImmichWithRateLimit(db, *cfg)
		return nil
	}
	s.Every(cfg.ImmichSyncIntervalHours).Hours().Do(recoverJob(db, models.JobNameImmichSync, models.JobTriggerScheduled, immichSyncTask))
	go safeGo(db, models.JobNameImmichSync, models.JobTriggerInitial, immichSyncTask)

	// Check the live database for corruption on a schedule (issue #273).
	// Job-lock guarded, config-gated (DB_INTEGRITY_CHECK_ENABLED).
	dbIntegrityTask := func() error {
		services.CheckDBIntegrityScheduled(db, *cfg)
		return nil
	}
	s.Every(cfg.DBIntegrityCheckIntervalHours).Hours().Do(recoverJob(db, models.JobNameDBIntegrityCheck, models.JobTriggerScheduled, dbIntegrityTask))
	go safeGo(db, models.JobNameDBIntegrityCheck, models.JobTriggerInitial, dbIntegrityTask)

	// Periodically prove a backup actually restores (issue #275). Job-lock
	// guarded, config-gated (DB_RESTORE_DRILL_ENABLED).
	restoreDrillTask := func() error {
		services.RunRestoreDrillScheduled(db, *cfg)
		return nil
	}
	s.Every(cfg.DBRestoreDrillIntervalHours).Hours().Do(recoverJob(db, models.JobNameRestoreDrill, models.JobTriggerScheduled, restoreDrillTask))
	go safeGo(db, models.JobNameRestoreDrill, models.JobTriggerInitial, restoreDrillTask)

	// Evaluate alert conditions on a schedule (issue #428): detect
	// failure/recovery transitions on the tracked subsystems and notify on
	// them. Job-lock guarded, config-gated (ALERTING_ENABLED).
	alertEvalTask := func() error {
		services.EvaluateAlerts(db, *cfg)
		return nil
	}
	s.Every(cfg.AlertEvalIntervalMinutes).Minutes().Do(recoverJob(db, models.JobNameAlertEval, models.JobTriggerScheduled, alertEvalTask))
	go safeGo(db, models.JobNameAlertEval, models.JobTriggerInitial, alertEvalTask)

	// Daily storage-growth sampler (issue #652): write one storage_samples row
	// measuring the on-disk footprint, prune rows past their retention window,
	// and emit a system_events row. Job-lock guarded so a multi-instance
	// deploy does not double-write.
	storageSampleTask := func() error {
		services.RecordStorageSampleScheduled(db, *cfg)
		return nil
	}
	s.Every(24).Hours().Do(recoverJob(db, models.JobNameStorageSample, models.JobTriggerScheduled, storageSampleTask))
	go safeGo(db, models.JobNameStorageSample, models.JobTriggerInitial, storageSampleTask)

	go s.StartBlocking()

	// gin.New() rather than gin.Default(): the app installs its own
	// middleware.LoggingMiddleware() below, which is redaction-aware (query
	// values are allow-listed, see logger.RedactQueryValues). gin.Default()
	// additionally attaches gin's own Logger(), which writes a second,
	// unredacted request line ("GET /api/v1/contacts?search=<a name>") to
	// stdout — an instance-wide disclosure of the same personal data the
	// custom logger exists to keep out (issue #510). Recovery() is kept.
	r := gin.New()
	r.Use(gin.Recovery())

	// Limit multipart form memory to 10MB to prevent DoS via large request bodies
	r.MaxMultipartMemory = 10 << 20 // 10 MB

	// For production, set FRONTEND_URL to specific origin(s) like "https://yourdomain.com"
	corsConfig := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "PROPFIND", "REPORT", "MKCOL", "COPY", "MOVE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Depth", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"Content-Length", "ETag"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour, // Cache preflight for 12 hours
	}

	// Handle wildcard "*" for development: allow any origin
	if cfg.FrontendURL == "*" {
		corsConfig.AllowOriginFunc = func(origin string) bool {
			return true // Allow all origins in development
		}
	} else {
		// Production: allow specific origin(s)
		corsConfig.AllowOrigins = []string{cfg.FrontendURL}
	}

	r.Use(cors.New(corsConfig))

	// Add security headers (HSTS only when serving over HTTPS, signaled by COOKIE_SECURE)
	r.Use(middleware.SecurityHeadersMiddleware(cfg.CookieSecure))

	// Apply the configured general-API rate limits before any route is
	// registered (the middleware package installs safe defaults in its own
	// init(); this only replaces them when the environment sets them).
	middleware.ConfigureAPIRateLimiter(cfg.APIRateLimitInterval, cfg.APIRateLimitBurst)

	// Add request body size limit middleware (10MB default) to prevent DoS
	r.Use(middleware.DefaultBodySizeLimitMiddleware())

	// Add request ID middleware for tracing
	r.Use(middleware.RequestIDMiddleware())

	// Add logging middleware (after request ID)
	r.Use(middleware.LoggingMiddleware())

	// Record per-request Prometheus metrics (issue #389). Cheap and always on;
	// the /metrics endpoint that exposes them is still opt-in via METRICS_TOKEN.
	r.Use(middleware.MetricsMiddleware())

	// Add error handling middleware
	r.Use(apperrors.ErrorHandlerMiddleware())

	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		logger.Fatal().Err(err).Strs("proxies", cfg.TrustedProxies).Msg("Failed to set trusted proxies")
	}

	// Inject db and cfg into context
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})

	// Initialize OIDC provider if configured
	var oidcProvider *services.OIDCProvider
	if cfg.OIDC.Enabled {
		logger.Info().Str("provider", cfg.OIDC.ProviderURL).Msg("Initializing OIDC provider...")
		oidcCtx, oidcCancel := context.WithTimeout(context.Background(), 30*time.Second)
		var oidcErr error
		oidcProvider, oidcErr = services.InitOIDCProvider(oidcCtx, cfg)
		oidcCancel()
		if oidcErr != nil {
			logger.Fatal().Err(oidcErr).Msg("Failed to initialize OIDC provider")
		}
		logger.Info().Msg("OIDC provider initialized successfully")
	}

	// Register all routes from routes.go
	routes.RegisterRoutes(r, cfg, db, oidcProvider)

	// Create HTTP server with timeout configuration
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	logger.Info().
		Str("port", cfg.Port).
		Int("read_timeout", cfg.ReadTimeout).
		Int("write_timeout", cfg.WriteTimeout).
		Int("idle_timeout", cfg.IdleTimeout).
		Msg("Starting server")

	// Graceful shutdown handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Failed to run server")
		}
	}()

	logger.Info().Msg("Server is ready to handle requests")
	models.RecordSystemEvent(context.Background(), db, models.SystemEvent{
		EventType: models.SysEventApplicationStarted,
		Component: logger.ComponentApp,
		Detail:    "version=" + buildinfo.Get().Version,
	})

	// Block until we receive a shutdown signal
	<-quit
	logger.Info().Msg("Shutting down server...")
	models.RecordSystemEvent(context.Background(), db, models.SystemEvent{
		EventType: models.SysEventApplicationStopped,
		Component: logger.ComponentApp,
	})

	// Stop the scheduler first to prevent new jobs from starting
	logger.Info().Msg("Stopping scheduler...")
	s.Stop()

	// Stop the rate limiter's stale-entry sweeper. Its counterpart
	// StartCleanupRoutine runs from the middleware package's init(), so
	// without this the goroutine (and the cleanupDone channel that exists
	// solely to signal it) had no caller at all -- the shutdown hook was
	// written but never wired up.
	middleware.StopCleanupRoutine()

	// Create a deadline to wait for active requests to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown of HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	// Close database connection
	logger.Info().Msg("Closing database connection...")
	sqlDB, err := db.DB()
	if err == nil {
		if err := sqlDB.Close(); err != nil {
			logger.Error().Err(err).Msg("Error closing database connection")
		}
	}

	logger.Info().Msg("Server exited gracefully")
}
