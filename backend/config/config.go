// Package config loads and validates application configuration from
// environment variables, exposing it as a single Config value the rest of
// the backend reads from (server/DB settings, auth secrets, mail/OIDC/
// CardDAV sync options, feature flags).
package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OIDCConfig holds optional OIDC provider settings.
type OIDCConfig struct {
	Enabled               bool
	ProviderURL           string
	ClientID              string
	ClientSecret          string
	RedirectURL           string // derived from FrontendURL, not configurable
	AllowAutoProvision    bool
	TrustEmail            bool // skip email_verified requirement when linking accounts (for trusted self-hosted providers)
	Scopes                []string
	PostLogoutRedirectURL string // derived from FrontendURL, not configurable — see RedirectURL
	// BlockPrivateURLs routes the discovery/token/JWKS/UserInfo calls through
	// the SSRF-guarded dialer (httputil.SafeDialContext). Default off: a LAN
	// identity provider (Authentik/Keycloak on the same Docker network) is a
	// common self-hosted setup and must keep working. Same opt-in shape as the
	// other *_BLOCK_PRIVATE_URLS knobs (INT-02, issue #465).
	BlockPrivateURLs bool
}

// Config is the fully-loaded application configuration, populated once by
// LoadConfig from environment variables at process start.
type Config struct {
	DBPath                       string
	ReminderTime                 string
	ReminderTimezone             string
	FrontendURL                  string
	Port                         string
	TrustedProxies               []string
	UseResend                    bool
	ResendAPIKey                 string
	ResendFromEmail              string
	ResendToEmail                string
	UseSMTP                      bool
	SMTPHost                     string
	SMTPPort                     int
	SMTPUsername                 string
	SMTPPassword                 string
	SMTPFromEmail                string
	SMTPUseTLS                   bool // implicit TLS (e.g. port 465); otherwise STARTTLS is used when available
	JWTSecretKey                 string
	JWTExpiryHours               int
	ReadTimeout                  int    // HTTP server read timeout in seconds
	WriteTimeout                 int    // HTTP server write timeout in seconds
	IdleTimeout                  int    // HTTP server idle timeout in seconds
	ProfilePhotoDir              string // Directory for storing profile photos (must be absolute path)
	AttachmentsDir               string // Directory for storing contact attachments (N7; alongside the photo dir, must be absolute path)
	CardDAVEnabled               bool   // Enable CardDAV server for contact sync
	CalDAVEnabled                bool   // Enable CalDAV server for Interaction/LifeEvent sync (T12b)
	CalDAVTwoWayEnabled          bool   // Allow calendar sync to push local edits back out (T13)
	CookieSecure                 bool   // Set Secure flag on auth cookie (requires HTTPS)
	CookieDomain                 string // Domain for auth cookie (empty = current domain only)
	RegistrationDisabled         bool   // Disable new user registration
	WebhookBlockPrivateURLs      bool   // Block webhook deliveries to private/loopback addresses (useful for cloud deployments)
	CalDAVSyncIntervalHours      int    // Interval in hours for the scheduled calendar sync job
	CalDAVBlockPrivateURLs       bool   // Block calendar sync requests to private/loopback addresses (useful for cloud deployments)
	DeleteRetentionDays          int    // Days soft-deleted rows survive before the purge job hard-deletes them (T26)
	AuditRetentionDays           int    // Days audit events survive before the retention purge removes them (T18, default 90)
	ContactShareRetentionDays    int    // Days a ContactShare snapshot survives before the purge job hard-deletes it (issue #574, default 30)
	SystemEventRetentionDays     int    // Days system_events rows survive before the retention purge removes them (issue #424, default 30)
	WebhookDeliveryRetentionDays int    // Days webhook_deliveries rows survive before the purge job hard-deletes them (issue #622, default 30)
	JobRunRetentionDays          int    // Days job_runs rows survive before the retention purge removes them (issue #391, default 30)
	IdempotencyKeyRetentionHours int    // Hours idempotency_keys rows survive before the TTL purge removes them (issue #459, CON-04, default 24; <=0 disables)

	// General-API rate limiting, per client IP. Configurable because the
	// hardcoded values had already been raised once to stop a full Playwright
	// run exhausting the bucket, and because a deployment where several
	// people share one egress IP (a household behind NAT, or a reverse proxy
	// without correct X-Forwarded-For) shares a single bucket between them.
	// Defaults preserve the previous hardcoded behaviour exactly.
	APIRateLimitInterval          time.Duration // Sustained refill interval, one token per interval
	APIRateLimitBurst             int           // Bucket size, i.e. the largest instantaneous burst allowed
	ImmichSyncIntervalHours       int           // Interval in hours for the scheduled Immich enrichment sync (T16)
	ImmichBlockPrivateURLs        bool          // Block Immich fetches to private/loopback addresses (useful for cloud deployments)
	PaperlessBlockPrivateURLs     bool          // Block Paperless-ngx fetches to private/loopback addresses (useful for cloud deployments)
	SeafileBlockPrivateURLs       bool          // Block Seafile fetches to private/loopback addresses (useful for cloud deployments)
	WebDAVBlockPrivateURLs        bool          // Block Nextcloud/ownCloud WebDAV fetches to private/loopback addresses (useful for cloud deployments)
	MonicaBlockPrivateURLs        bool          // Block Monica import-assistant fetches to private/loopback addresses (useful for cloud deployments; issue #549)
	FCMServiceAccountFile         string        // Path to the Firebase service-account JSON for FCM mobile push delivery (M2)
	DBIntegrityCheckEnabled       bool          // Enable the scheduled live-DB PRAGMA integrity_check job (issue #273)
	DBIntegrityCheckIntervalHours int           // Interval in hours for the scheduled DB integrity check
	DBRestoreDrillEnabled         bool          // Enable the scheduled backup-restore drill job (issue #275)
	DBRestoreDrillIntervalHours   int           // Interval in hours for the scheduled restore drill (default weekly)
	// DBRestoreDrillMaxDurationSeconds is the operator's RTO budget for the
	// database piece of a restore (issue #506). When > 0, a restore-drill run
	// whose measured wall-clock exceeds it still passes but logs a WARN and
	// annotates the restore_test_completed timeline row — restore-time drift
	// visible before an incident, not during one. 0 (default) = no budget; the
	// drill's duration_ms is recorded on every run regardless.
	DBRestoreDrillMaxDurationSeconds int

	// Alerting on state transitions (issue #428). The scheduled evaluator
	// (services.EvaluateAlerts) detects failure/recovery transitions on the
	// tracked subsystems (#427) plus a few threshold checks, and dispatches one
	// notification per transition through the existing webhook + notification
	// channels. Personal channels (email/ntfy/Gotify/push) go to admin users
	// only; webhooks broadcast as usual.
	AlertingEnabled             bool // Master switch for the alert evaluator
	AlertEvalIntervalMinutes    int  // How often the evaluator runs
	AlertDiskUsagePercent       int  // Raise disk_space when used% >= this; 0 disables the condition
	AlertSyncFailureThreshold   int  // Consecutive sync failures before sync:* fires
	AlertNotifyFailureThreshold int  // Consecutive notification failures before the notifications condition fires
	AlertBackupMaxAgeHours      int  // Raise backup_stale when the last backup success is older than this; 0 => 2 * DBRestoreDrillIntervalHours
	AlertJobStaleMultiplier     int  // Raise job_stopped when a job's last successful run is older than interval * this
	AlertIncidentQuietHours     int  // integrations recovers when no new integration_failed event lands within this window
	AlertBackupEnabled          bool // Enable the backup / backup_stale conditions
	AlertDBIntegrityEnabled     bool // Enable the db_integrity condition
	AlertJobStoppedEnabled      bool // Enable the job_stopped condition
	HIBPCheckEnabled            bool // Check new/changed passwords against HIBP's k-anonymity range API (issue #376). Off by default: an outbound call on a self-hosted app is a deliberate opt-in, not a safe default — see docs/security/asvs-l2.md's P3.
	UpdateCheckEnabled          bool // Compare the running build against the latest GitHub release (issue #650). Off by default: an outbound call on a self-hosted app is a deliberate opt-in, not a safe default — see docs/security/asvs-l2.md's P6.

	// Storage-growth trend thresholds (issue #652). The /admin/system-status
	// storage block folds usage_percent against these two tiers into
	// ok | warning | critical (with -5% hysteresis), and the daily storage
	// sampler retains StorageSampleRetentionDays of history.
	StorageWarnPercent         int // usage% >= this turns the storage threshold warning (default 75)
	StorageCriticalPercent     int // usage% >= this turns it critical (default 90)
	StorageSampleRetentionDays int // days of storage_samples history kept (default 180)
	OIDC                       OIDCConfig

	// DataEncryptionKey is the base64-encoded 32-byte master key for
	// field-level at-rest encryption (issue #380, ASVS V6.4/V8.3). When unset,
	// atrest falls back to DATA_ENCRYPTION_KEY_FILE, then to an HKDF-SHA256
	// derivation from JWT_SECRET_KEY so existing deployments get encryption
	// with zero config. See backend/atrest/atrest.go.
	DataEncryptionKey     string // base64, 32 bytes
	DataEncryptionKeyFile string // path to a file whose trimmed contents are the base64 key

	// MetricsToken gates the Prometheus GET /metrics endpoint (issue #389).
	// Opt-in: when empty the route is not registered at all. When set, every
	// scrape must carry `Authorization: Bearer <MetricsToken>`. Minimum 16
	// characters (enforced in Validate) — a short scrape credential is worse
	// than none.
	MetricsToken string

	// MinClientVersion is the oldest Android client `versionName` this server
	// still supports, advertised verbatim on GET /health as
	// `min_client_version` (issue #528 —
	// docs/client-compatibility-policy.md, ANDROID-01/#478). Empty (the
	// default) means no floor has been declared: every released client stays
	// compatible. It is set via MIN_CLIENT_VERSION only when a MAINT-02
	// breaking change actually strands older clients — a floor is a deliberate,
	// reviewed event, never a side effect of a release.
	MinClientVersion string
}

// Defaults for the storage-trend thresholds (issue #652). Exported so the
// storage threshold computation can reuse them for a raw config.Config built
// without LoadConfig (tests) — zero-value Config values resolve to these.
const (
	// DefaultDBRestoreDrillIntervalHours is the shipped cadence of the
	// scheduled restore drill: weekly. The backup-freshness ceiling stated in
	// docs/deployment.md's "Recovery objectives (RPO and RTO)" section is
	// derived from it (2 x this = the default ALERT_BACKUP_MAX_AGE_HOURS), and
	// TestRecoveryObjectivesDocMatchesShippedDefaults pins that the documented
	// number still follows from this constant (issue #506).
	DefaultDBRestoreDrillIntervalHours = 168

	// DefaultStorageWarnPercent is the used-percent at which the storage
	// threshold on /admin/system-status turns "warning" (default 75).
	DefaultStorageWarnPercent = 75
	// DefaultStorageCriticalPercent is the used-percent at which it turns
	// "critical" (default 90).
	DefaultStorageCriticalPercent = 90
	// DefaultStorageSampleRetentionDays is how long storage_samples rows
	// survive before the sampler prunes them (default 180).
	DefaultStorageSampleRetentionDays = 180
)

// LoadConfig reads environment variables (with sensible defaults) into a
// new Config.
func LoadConfig() *Config {

	defaultJWTExpiry := 96
	jwtExpiryHours, err := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", strconv.Itoa(defaultJWTExpiry)))
	if err != nil {
		log.Println("WARN: Invalid JWT expiry set. Please provide an integer value.")
		jwtExpiryHours = defaultJWTExpiry
	}

	// Parse timeout values with defaults
	readTimeout := getIntEnv("HTTP_READ_TIMEOUT", 15)
	writeTimeout := getIntEnv("HTTP_WRITE_TIMEOUT", 15)
	idleTimeout := getIntEnv("HTTP_IDLE_TIMEOUT", 60)

	cfg := &Config{
		DBPath:                        getEnv("SQLITE_DB_PATH", "mycorrhizal.db"),
		ReminderTime:                  getEnv("REMINDER_TIME", "06:00"),
		ReminderTimezone:              getEnv("REMINDER_TIMEZONE", "UTC"),
		FrontendURL:                   getEnv("FRONTEND_URL", "*"),
		Port:                          getEnv("PORT", "8080"),
		ResendAPIKey:                  getEnv("RESEND_API_KEY", ""),
		ResendFromEmail:               getEnv("RESEND_FROM_EMAIL", ""),
		SMTPHost:                      getEnv("SMTP_HOST", ""),
		SMTPPort:                      getIntEnv("SMTP_PORT", 587),
		SMTPUsername:                  getEnv("SMTP_USERNAME", ""),
		SMTPPassword:                  getEnv("SMTP_PASSWORD", ""),
		SMTPFromEmail:                 getEnv("SMTP_FROM_EMAIL", ""),
		SMTPUseTLS:                    getBoolEnv("SMTP_USE_TLS", false),
		JWTSecretKey:                  getEnv("JWT_SECRET_KEY", ""),
		JWTExpiryHours:                jwtExpiryHours,
		TrustedProxies:                getProxies(getEnv("TRUSTED_PROXIES", "")),
		ReadTimeout:                   readTimeout,
		WriteTimeout:                  writeTimeout,
		IdleTimeout:                   idleTimeout,
		ProfilePhotoDir:               getEnv("PROFILE_PHOTO_DIR", ""),
		AttachmentsDir:                getEnv("ATTACHMENTS_DIR", filepath.Join(filepath.Dir(getEnv("PROFILE_PHOTO_DIR", "")), "attachments")),
		CardDAVEnabled:                getBoolEnv("CARDDAV_ENABLED", false),
		CalDAVEnabled:                 getBoolEnv("CALDAV_ENABLED", false),
		CalDAVTwoWayEnabled:           getBoolEnv("CALDAV_TWO_WAY_ENABLED", false),
		CookieSecure:                  getBoolEnv("COOKIE_SECURE", false),
		CookieDomain:                  getEnv("COOKIE_DOMAIN", ""),
		RegistrationDisabled:          getBoolEnv("DISABLE_REGISTRATION", false),
		WebhookBlockPrivateURLs:       getBoolEnv("WEBHOOK_BLOCK_PRIVATE_URLS", false),
		CalDAVSyncIntervalHours:       getIntEnv("CALDAV_SYNC_INTERVAL_HOURS", 6),
		CalDAVBlockPrivateURLs:        getBoolEnv("CALDAV_BLOCK_PRIVATE_URLS", false),
		DeleteRetentionDays:           getIntEnv("DELETED_RETENTION_DAYS", 30),
		AuditRetentionDays:            getIntEnv("AUDIT_RETENTION_DAYS", 90),
		ContactShareRetentionDays:     getIntEnv("CONTACT_SHARE_RETENTION_DAYS", 30),
		SystemEventRetentionDays:      getIntEnv("SYSTEM_EVENT_RETENTION_DAYS", 30),
		WebhookDeliveryRetentionDays:  getIntEnv("WEBHOOK_DELIVERY_RETENTION_DAYS", 30),
		JobRunRetentionDays:           getIntEnv("JOB_RUN_RETENTION_DAYS", 30),
		IdempotencyKeyRetentionHours:  getIntEnv("IDEMPOTENCY_KEY_RETENTION_HOURS", 24),
		APIRateLimitInterval:          time.Duration(getIntEnv("API_RATE_LIMIT_INTERVAL_MS", 600)) * time.Millisecond,
		APIRateLimitBurst:             getIntEnv("API_RATE_LIMIT_BURST", 1000),
		ImmichSyncIntervalHours:       getIntEnv("IMMICH_SYNC_INTERVAL_HOURS", 6),
		ImmichBlockPrivateURLs:        getBoolEnv("IMMICH_BLOCK_PRIVATE_URLS", false),
		PaperlessBlockPrivateURLs:     getBoolEnv("PAPERLESS_BLOCK_PRIVATE_URLS", false),
		SeafileBlockPrivateURLs:       getBoolEnv("SEAFILE_BLOCK_PRIVATE_URLS", false),
		WebDAVBlockPrivateURLs:        getBoolEnv("WEBDAV_BLOCK_PRIVATE_URLS", false),
		MonicaBlockPrivateURLs:        getBoolEnv("MONICA_BLOCK_PRIVATE_URLS", false),
		FCMServiceAccountFile:         getEnv("FCM_SERVICE_ACCOUNT_FILE", ""),
		DBIntegrityCheckEnabled:       getBoolEnv("DB_INTEGRITY_CHECK_ENABLED", true),
		DBIntegrityCheckIntervalHours: getIntEnv("DB_INTEGRITY_CHECK_INTERVAL_HOURS", 24),
		DBRestoreDrillEnabled:         getBoolEnv("DB_RESTORE_DRILL_ENABLED", true),
		DBRestoreDrillIntervalHours:   getIntEnv("DB_RESTORE_DRILL_INTERVAL_HOURS", DefaultDBRestoreDrillIntervalHours),
		AlertingEnabled:               getBoolEnv("ALERTING_ENABLED", true),
		AlertEvalIntervalMinutes:      getIntEnv("ALERT_EVAL_INTERVAL_MINUTES", 15),
		AlertDiskUsagePercent:         getIntEnv("ALERT_DISK_USAGE_PERCENT", 90),
		AlertSyncFailureThreshold:     getIntEnv("ALERT_SYNC_FAILURE_THRESHOLD", 3),
		AlertNotifyFailureThreshold:   getIntEnv("ALERT_NOTIFY_FAILURE_THRESHOLD", 3),
		AlertBackupMaxAgeHours:        getIntEnv("ALERT_BACKUP_MAX_AGE_HOURS", 0),
		AlertJobStaleMultiplier:       getIntEnv("ALERT_JOB_STALE_MULTIPLIER", 3),
		AlertIncidentQuietHours:       getIntEnv("ALERT_INCIDENT_QUIET_HOURS", 6),
		AlertBackupEnabled:            getBoolEnv("ALERT_BACKUP_ENABLED", true),
		AlertDBIntegrityEnabled:       getBoolEnv("ALERT_DB_INTEGRITY_ENABLED", true),
		AlertJobStoppedEnabled:        getBoolEnv("ALERT_JOB_STOPPED_ENABLED", true),
		HIBPCheckEnabled:              getBoolEnv("HIBP_CHECK_ENABLED", false),
		UpdateCheckEnabled:            getBoolEnv("UPDATE_CHECK_ENABLED", false),
		StorageWarnPercent:            getIntEnv("STORAGE_WARN_PERCENT", DefaultStorageWarnPercent),
		StorageCriticalPercent:        getIntEnv("STORAGE_CRITICAL_PERCENT", DefaultStorageCriticalPercent),
		StorageSampleRetentionDays:    getIntEnv("STORAGE_SAMPLE_RETENTION_DAYS", DefaultStorageSampleRetentionDays),
		DataEncryptionKey:             getEnv("DATA_ENCRYPTION_KEY", ""),
		DataEncryptionKeyFile:         getEnv("DATA_ENCRYPTION_KEY_FILE", ""),
		MetricsToken:                  getEnv("METRICS_TOKEN", ""),
		MinClientVersion:              getEnv("MIN_CLIENT_VERSION", ""),
	}

	// Assigned outside the aligned literal above so a longer key name does not
	// reflow every line in it. Opt-in RTO budget for the restore drill (#506).
	cfg.DBRestoreDrillMaxDurationSeconds = getIntEnv("DB_RESTORE_DRILL_MAX_DURATION_SECONDS", 0)

	if cfg.CalDAVSyncIntervalHours < 1 {
		log.Println("WARN: CALDAV_SYNC_INTERVAL_HOURS must be at least 1, using 1")
		cfg.CalDAVSyncIntervalHours = 1
	}

	if cfg.ImmichSyncIntervalHours < 1 {
		log.Println("WARN: IMMICH_SYNC_INTERVAL_HOURS must be at least 1, using 1")
		cfg.ImmichSyncIntervalHours = 1
	}

	if cfg.DBIntegrityCheckIntervalHours < 1 {
		log.Println("WARN: DB_INTEGRITY_CHECK_INTERVAL_HOURS must be at least 1, using 1")
		cfg.DBIntegrityCheckIntervalHours = 1
	}

	if cfg.DBRestoreDrillIntervalHours < 1 {
		log.Println("WARN: DB_RESTORE_DRILL_INTERVAL_HOURS must be at least 1, using 1")
		cfg.DBRestoreDrillIntervalHours = 1
	}

	if cfg.DBRestoreDrillMaxDurationSeconds < 0 {
		log.Println("WARN: DB_RESTORE_DRILL_MAX_DURATION_SECONDS cannot be negative, using 0 (no budget)")
		cfg.DBRestoreDrillMaxDurationSeconds = 0
	}

	if cfg.AlertEvalIntervalMinutes < 1 {
		log.Println("WARN: ALERT_EVAL_INTERVAL_MINUTES must be at least 1, using 1")
		cfg.AlertEvalIntervalMinutes = 1
	}

	if cfg.AlertDiskUsagePercent < 0 || cfg.AlertDiskUsagePercent > 99 {
		log.Println("WARN: ALERT_DISK_USAGE_PERCENT must be between 0 and 99, using 90")
		cfg.AlertDiskUsagePercent = 90
	}

	if cfg.AlertSyncFailureThreshold < 1 {
		cfg.AlertSyncFailureThreshold = 1
	}
	if cfg.AlertNotifyFailureThreshold < 1 {
		cfg.AlertNotifyFailureThreshold = 1
	}
	if cfg.AlertJobStaleMultiplier < 2 {
		log.Println("WARN: ALERT_JOB_STALE_MULTIPLIER must be at least 2, using 2")
		cfg.AlertJobStaleMultiplier = 2
	}
	if cfg.AlertIncidentQuietHours < 1 {
		cfg.AlertIncidentQuietHours = 1
	}
	if cfg.AlertBackupMaxAgeHours < 0 {
		cfg.AlertBackupMaxAgeHours = 0
	}

	// Storage-trend thresholds (issue #652): warn must be a sane 1..99 and
	// critical strictly above warn (otherwise the two tiers collapse and the
	// threshold is meaningless). Same "clamp to a working value, don't refuse
	// to boot" posture as the ALERT_* knobs above.
	if cfg.StorageWarnPercent < 1 || cfg.StorageWarnPercent > 99 {
		log.Println("WARN: STORAGE_WARN_PERCENT must be between 1 and 99, using 75")
		cfg.StorageWarnPercent = DefaultStorageWarnPercent
	}
	if cfg.StorageCriticalPercent <= cfg.StorageWarnPercent || cfg.StorageCriticalPercent > 100 {
		log.Println("WARN: STORAGE_CRITICAL_PERCENT must be above STORAGE_WARN_PERCENT and at most 100, using 90")
		cfg.StorageCriticalPercent = DefaultStorageCriticalPercent
	}
	if cfg.StorageSampleRetentionDays < 7 {
		log.Println("WARN: STORAGE_SAMPLE_RETENTION_DAYS must be at least 7, using 180")
		cfg.StorageSampleRetentionDays = DefaultStorageSampleRetentionDays
	}

	// An email channel is enabled only when it is fully configured
	cfg.UseResend = cfg.ResendAPIKey != "" && cfg.ResendFromEmail != ""
	cfg.UseSMTP = cfg.SMTPHost != "" && cfg.SMTPFromEmail != ""

	oidcProviderURL := getEnv("OIDC_PROVIDER_URL", "")
	oidcClientID := getEnv("OIDC_CLIENT_ID", "")
	oidcClientSecret := getEnv("OIDC_CLIENT_SECRET", "")
	cfg.OIDC = OIDCConfig{
		Enabled:               oidcProviderURL != "" && oidcClientID != "" && oidcClientSecret != "",
		ProviderURL:           oidcProviderURL,
		ClientID:              oidcClientID,
		ClientSecret:          oidcClientSecret,
		RedirectURL:           cfg.FrontendURL + "/api/v1/auth/oidc/callback",
		AllowAutoProvision:    getBoolEnv("OIDC_AUTO_PROVISION", false),
		TrustEmail:            getBoolEnv("OIDC_TRUST_EMAIL", false),
		Scopes:                getScopesEnv(getEnv("OIDC_SCOPES", "")),
		PostLogoutRedirectURL: cfg.FrontendURL + "/login",
		BlockPrivateURLs:      getBoolEnv("OIDC_BLOCK_PRIVATE_URLS", false),
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		intValue, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("WARN: Invalid integer value for %s: %s. Using default: %d", key, value, fallback)
			return fallback
		}
		return intValue
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		boolValue, err := strconv.ParseBool(value)
		if err != nil {
			log.Printf("WARN: Invalid boolean value for %s: %s. Using default: %v", key, value, fallback)
			return fallback
		}
		return boolValue
	}
	return fallback
}

func getProxies(proxies string) []string {
	if proxies == "" {
		return nil
	}

	proxyList := strings.Split(proxies, ",")
	for i, proxy := range proxyList {
		proxyList[i] = strings.TrimSpace(proxy) // Remove whitespaces
	}
	return proxyList
}

// getScopesEnv parses a comma-separated OIDC scope list, defaulting to
// openid/email/profile when unset since scopes must never end up empty.
func getScopesEnv(scopes string) []string {
	if scopes == "" {
		return []string{"openid", "email", "profile"}
	}

	scopeList := strings.Split(scopes, ",")
	for i, scope := range scopeList {
		scopeList[i] = strings.TrimSpace(scope)
	}
	return scopeList
}

// knownPlaceholderJWTSecrets are the exact secret values that ship as
// copy-paste defaults in this repo's .env.example files (the value has never
// changed since the fork). They are published constants by definition, so
// even though they clear the length floor, booting with one must fail —
// anyone who can read the repo can sign tokens with it.
var knownPlaceholderJWTSecrets = []string{
	"your-very-long-very-secret-jwt-key-change-this-in-production",
}

// placeholderJWTSecretMarkers are unambiguous fragments that indicate a
// template/example secret rather than a real one. Matched case-insensitively
// as substrings so a padded or re-cased copy of a placeholder is still
// caught. Kept deliberately narrow: a false positive here is a legitimate
// deployment refusing to boot, so only markers that no real random secret or
// passphrase would contain belong on this list.
var placeholderJWTSecretMarkers = []string{
	"change-this-in-production",
	"changeme",
	"change-me",
	"change_me",
	"your-secret",
	"yoursecret",
	"replace-me",
	"replaceme",
	"replace_me",
	"placeholder",
}

// isKnownPlaceholderJWTSecret reports whether a JWT secret is a published
// placeholder value, matched exactly or by an unambiguous marker. Case- and
// whitespace-insensitive, since an operator copying .env.example verbatim is
// exactly the failure mode this guard exists for.
func isKnownPlaceholderJWTSecret(secret string) bool {
	lower := strings.ToLower(strings.TrimSpace(secret))
	for _, p := range knownPlaceholderJWTSecrets {
		if lower == p {
			return true
		}
	}
	for _, m := range placeholderJWTSecretMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// minJWTSecretEntropyBits is the lowest total Shannon entropy (in bits) a
// JWT secret may carry. It sits far below what any random 32-byte secret
// produces (base64: ~190 bits; random alphanumeric: ~150 bits), so the only
// secrets it rejects are degenerate values that clear the length floor but
// are still trivially guessable — a repeated character, a near-uniform
// two-character alternation, a single digit. A passphrase of random words
// clears it comfortably. It is a deliberate heuristic backstop, not a
// predictability guarantee: a string that is long, high-cardinality and
// *structured* (a word repeated, the alphabet in order) still scores high,
// which is why the placeholder markers above exist and why the primary floor
// stays the ≥32-byte length requirement.
const minJWTSecretEntropyBits = 40.0

// jwtSecretEntropyBits estimates the total Shannon entropy of a secret in
// bits. It iterates over runes so a non-ASCII passphrase is scored by its
// actual character diversity, not penalized for UTF-8 byte length. The metric
// is a cheap backstop for degenerate secrets only — see minJWTSecretEntropyBits.
func jwtSecretEntropyBits(secret string) float64 {
	counts := make(map[rune]int)
	var total int
	for _, r := range secret {
		counts[r]++
		total++
	}
	if total == 0 {
		return 0
	}
	var bits float64
	for _, n := range counts {
		p := float64(n) / float64(total)
		bits -= p * math.Log2(p)
	}
	return bits * float64(total)
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

// isValidBase64Key reports whether s is a base64 string that decodes to
// exactly 32 bytes (the AES-256 master-key size atrest requires). It exists
// so config.Validate can fail boot on a set-but-broken DATA_ENCRYPTION_KEY
// without importing atrest (config must not create an import cycle).
func isValidBase64Key(s string) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	return err == nil && len(raw) == 32
}

// clientVersionPattern matches the version shapes an Android `versionName`
// (and the server build version it is compared against) can legitimately
// take: `major`, `major.minor`, or `major.minor.patch`, optionally followed
// by a `-prerelease` or `+build` suffix, and optionally prefixed with `v`.
// The floor value in MIN_CLIENT_VERSION is compared against client version
// strings, so it must be a shape both sides can parse; anything else (a typo,
// a stray path) is rejected at boot rather than silently failing open on the
// client later. It deliberately does NOT require a full semver triple: a
// two-part `0.6` floor is a valid, meaningful declaration.
var clientVersionPattern = regexp.MustCompile(`^v?\d+(\.\d+){0,2}(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

func isValidClientVersion(s string) bool {
	return clientVersionPattern.MatchString(strings.TrimSpace(s))
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("Configuration Error [%s]: %s", e.Field, e.Message)
}

// Validate checks if the configuration is valid and returns detailed errors if not
func (c *Config) Validate() []ValidationError {
	var errors []ValidationError

	// Validate JWT Secret Key - critical for security. The checks are ordered
	// from most-certain to most-heuristic so a broken secret surfaces the
	// first specific reason, not a vague later one: empty, then too short,
	// then a known published placeholder, then too little entropy. All three
	// gates exist because every one of them makes every token forgeable: an
	// empty/short secret is brute-forceable, and a long-but-published or
	// long-but-predictable secret is forgeable by anyone who has read the
	// repo or the operator's shell history.
	if c.JWTSecretKey == "" {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET_KEY",
			Message: "JWT secret key is required for authentication. Set JWT_SECRET_KEY environment variable.",
		})
	} else if len(c.JWTSecretKey) < 32 {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET_KEY",
			Message: fmt.Sprintf("JWT secret key is too short (%d bytes). Must be at least 32 bytes for security.", len(c.JWTSecretKey)),
		})
	} else if isKnownPlaceholderJWTSecret(c.JWTSecretKey) {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET_KEY",
			Message: "JWT secret key is a known placeholder value (it ships as the default in .env.example). Every token signed with it is forgeable by anyone who has read the repo. Generate a fresh secret, e.g. `openssl rand -base64 32`.",
		})
	} else if jwtSecretEntropyBits(c.JWTSecretKey) < minJWTSecretEntropyBits {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET_KEY",
			Message: fmt.Sprintf("JWT secret key has too little entropy (est. %.0f bits). A long but predictable secret is as forgeable as a short one; generate a random secret, e.g. `openssl rand -base64 32`.", jwtSecretEntropyBits(c.JWTSecretKey)),
		})
	}

	// Validate Database Path
	if c.DBPath == "" {
		errors = append(errors, ValidationError{
			Field:   "SQLITE_DB_PATH",
			Message: "Database path cannot be empty. Set SQLITE_DB_PATH environment variable.",
		})
	}

	// Validate at-rest encryption master key, when one is explicitly set.
	// Unset is fine (atrest falls back to DATA_ENCRYPTION_KEY_FILE, then to an
	// HKDF derivation from JWT_SECRET_KEY), but a set-but-broken key must fail
	// boot: an operator who thinks they configured a key and didn't would
	// otherwise run with a weaker derivation than they believe.
	if c.DataEncryptionKey != "" {
		if !isValidBase64Key(c.DataEncryptionKey) {
			errors = append(errors, ValidationError{
				Field:   "DATA_ENCRYPTION_KEY",
				Message: "DATA_ENCRYPTION_KEY must be base64-encoded 32 random bytes, e.g. `openssl rand -base64 32`. Unset it to use the JWT-derived key.",
			})
		}
	}
	if c.DataEncryptionKeyFile != "" {
		if _, err := os.Stat(c.DataEncryptionKeyFile); err != nil {
			errors = append(errors, ValidationError{
				Field:   "DATA_ENCRYPTION_KEY_FILE",
				Message: fmt.Sprintf("DATA_ENCRYPTION_KEY_FILE '%s' does not exist: %v", c.DataEncryptionKeyFile, err),
			})
		}
	}
	if c.DataEncryptionKey != "" && c.DataEncryptionKeyFile != "" {
		errors = append(errors, ValidationError{
			Field:   "DATA_ENCRYPTION_KEY",
			Message: "Set only one of DATA_ENCRYPTION_KEY or DATA_ENCRYPTION_KEY_FILE, not both.",
		})
	}

	// Validate Profile Photo Directory - must be set and absolute path for security
	if c.ProfilePhotoDir == "" {
		errors = append(errors, ValidationError{
			Field:   "PROFILE_PHOTO_DIR",
			Message: "Profile photo directory is required. Set PROFILE_PHOTO_DIR environment variable to an absolute path.",
		})
	} else if !filepath.IsAbs(c.ProfilePhotoDir) {
		errors = append(errors, ValidationError{
			Field:   "PROFILE_PHOTO_DIR",
			Message: fmt.Sprintf("Profile photo directory '%s' must be an absolute path for security.", c.ProfilePhotoDir),
		})
	}

	// Validate attachments directory - derived from the photo dir's parent by
	// default, so it must be absolute whenever the photo dir (also required,
	// above) is.
	if c.AttachmentsDir != "" && !filepath.IsAbs(c.AttachmentsDir) {
		errors = append(errors, ValidationError{
			Field:   "ATTACHMENTS_DIR",
			Message: fmt.Sprintf("Attachments directory '%s' must be an absolute path for security.", c.AttachmentsDir),
		})
	}

	// Validate Port
	if c.Port == "" {
		errors = append(errors, ValidationError{
			Field:   "PORT",
			Message: "Server port cannot be empty. Set PORT environment variable.",
		})
	} else {
		portNum, err := strconv.Atoi(c.Port)
		if err != nil || portNum < 1 || portNum > 65535 {
			errors = append(errors, ValidationError{
				Field:   "PORT",
				Message: fmt.Sprintf("Invalid port number '%s'. Must be between 1 and 65535.", c.Port),
			})
		}
	}

	// Validate Reminder Time format (HH:MM)
	timePattern := regexp.MustCompile(`^([0-1][0-9]|2[0-3]):[0-5][0-9]$`)
	if !timePattern.MatchString(c.ReminderTime) {
		errors = append(errors, ValidationError{
			Field:   "REMINDER_TIME",
			Message: fmt.Sprintf("Invalid time format '%s'. Must be in HH:MM format (e.g., 06:00).", c.ReminderTime),
		})
	}

	// Validate Reminder Timezone (must be a valid IANA timezone name)
	if _, err := time.LoadLocation(c.ReminderTimezone); err != nil {
		errors = append(errors, ValidationError{
			Field:   "REMINDER_TIMEZONE",
			Message: fmt.Sprintf("Invalid timezone '%s'. Must be a valid IANA timezone name (e.g., 'UTC', 'Europe/Berlin', 'America/New_York').", c.ReminderTimezone),
		})
	}

	// Validate Frontend URL
	if c.FrontendURL == "" {
		errors = append(errors, ValidationError{
			Field:   "FRONTEND_URL",
			Message: "Frontend URL cannot be empty. Set FRONTEND_URL environment variable (use '*' for development).",
		})
	}

	// FRONTEND_URL="*" combined with AllowCredentials:true (see main.go) makes the
	// server reflect any Origin while still accepting the auth cookie cross-site.
	// This is currently mitigated by SameSite=Lax on that cookie, so it's not a
	// live hole today - but it's fragile defense-in-depth, not a guarantee, so
	// refuse to boot with it in release mode. "*" remains fine for local dev
	// (GIN_MODE unset or "debug").
	if c.FrontendURL == "*" && os.Getenv("GIN_MODE") == "release" {
		errors = append(errors, ValidationError{
			Field:   "FRONTEND_URL",
			Message: "FRONTEND_URL cannot be '*' when GIN_MODE=release. '*' is dev-only (see .env.example); set FRONTEND_URL to your actual frontend origin(s) in production.",
		})
	}

	// COOKIE_SECURE defaults to false so plain-HTTP self-hosting (LAN,
	// localhost, a reverse proxy that hasn't added TLS yet) keeps working
	// with zero config — unlike the FRONTEND_URL check above, this can't
	// key off GIN_MODE=release, since that's also true for the default
	// docker-compose HTTP setup and would refuse to boot the common case.
	// What's unambiguously a mistake, in dev or prod, is telling the app
	// the frontend is served over HTTPS while leaving the auth/OIDC/
	// id_token cookies without the Secure flag: there's no legitimate
	// reason to want that combination, only a forgotten setting.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.FrontendURL)), "https://") && !c.CookieSecure {
		errors = append(errors, ValidationError{
			Field:   "COOKIE_SECURE",
			Message: "COOKIE_SECURE must be true when FRONTEND_URL is https://. Set COOKIE_SECURE=true (see .env.example) — serving over HTTPS with a non-Secure auth cookie has no legitimate use, only a missed setting.",
		})
	}

	// Validate JWT Expiry Hours
	if c.JWTExpiryHours < 1 || c.JWTExpiryHours > 8760 {
		errors = append(errors, ValidationError{
			Field:   "JWT_EXPIRY_HOURS",
			Message: fmt.Sprintf("Invalid JWT expiry hours '%d'. Must be between 1 and 8760 (1 year).", c.JWTExpiryHours),
		})
	}

	// Validate HTTP Timeouts (in seconds)
	if c.ReadTimeout < 1 || c.ReadTimeout > 300 {
		errors = append(errors, ValidationError{
			Field:   "HTTP_READ_TIMEOUT",
			Message: fmt.Sprintf("Invalid read timeout '%d'. Must be between 1 and 300 seconds.", c.ReadTimeout),
		})
	}
	if c.WriteTimeout < 1 || c.WriteTimeout > 300 {
		errors = append(errors, ValidationError{
			Field:   "HTTP_WRITE_TIMEOUT",
			Message: fmt.Sprintf("Invalid write timeout '%d'. Must be between 1 and 300 seconds.", c.WriteTimeout),
		})
	}
	if c.IdleTimeout < 1 || c.IdleTimeout > 300 {
		errors = append(errors, ValidationError{
			Field:   "HTTP_IDLE_TIMEOUT",
			Message: fmt.Sprintf("Invalid idle timeout '%d'. Must be between 1 and 300 seconds.", c.IdleTimeout),
		})
	}

	// A configured metrics scrape token must be long enough to be worth
	// having (issue #389). Empty is fine — it just leaves /metrics unregistered.
	if c.MetricsToken != "" && len(c.MetricsToken) < 16 {
		errors = append(errors, ValidationError{
			Field:   "METRICS_TOKEN",
			Message: "METRICS_TOKEN must be at least 16 characters when set.",
		})
	}

	// MIN_CLIENT_VERSION (issue #528) is advertised on the unauthenticated
	// GET /health and compared against Android versionNames, so a set-but-garbled
	// floor must fail boot: an operator who thinks they raised the floor and
	// didn't would ship an unadvertised breaking change. Empty (the default)
	// is the policy's normal state — no floor declared.
	if c.MinClientVersion != "" && !isValidClientVersion(c.MinClientVersion) {
		errors = append(errors, ValidationError{
			Field:   "MIN_CLIENT_VERSION",
			Message: fmt.Sprintf("Invalid minimum client version %q. Must look like a client versionName: major, major.minor, or major.minor.patch, optionally with a -prerelease or +build suffix (e.g. \"0.6.0\" or \"0.6.0-rc.1\").", c.MinClientVersion),
		})
	}

	// Validate Trusted Proxies format (IP addresses or CIDR notation)
	for _, proxy := range c.TrustedProxies {
		if proxy == "" {
			continue
		}
		// Check if it's a valid IP address
		if ip := net.ParseIP(proxy); ip != nil {
			continue
		}
		// Check if it's a valid CIDR
		if _, _, err := net.ParseCIDR(proxy); err == nil {
			continue
		}
		errors = append(errors, ValidationError{
			Field:   "TRUSTED_PROXIES",
			Message: fmt.Sprintf("Invalid proxy '%s'. Must be a valid IP address or CIDR notation.", proxy),
		})
	}

	// Warn if OIDC is partially configured (some vars set but not all required ones)
	oidcVars := []string{c.OIDC.ProviderURL, c.OIDC.ClientID, c.OIDC.ClientSecret}
	oidcSet := 0
	for _, v := range oidcVars {
		if v != "" {
			oidcSet++
		}
	}
	if oidcSet > 0 && oidcSet < 3 {
		log.Println("WARN: OIDC is partially configured. Set OIDC_PROVIDER_URL, OIDC_CLIENT_ID, and OIDC_CLIENT_SECRET to enable SSO.")
	}

	// M2: the FCM service account file is parsed and fully validated by the
	// services package at delivery time (config cannot import services without
	// a cycle). Here we only fail fast on the unambiguous mistake of pointing
	// at a path that doesn't exist.
	if fcmPath := c.FCMServiceAccountFile; fcmPath != "" {
		if _, err := os.Stat(fcmPath); err != nil {
			errors = append(errors, ValidationError{
				Field:   "FCM_SERVICE_ACCOUNT_FILE",
				Message: fmt.Sprintf("FCM service account file '%s' does not exist: %v. Unset it to disable mobile push, or point it at a valid Firebase service-account JSON.", fcmPath, err),
			})
		}
	}

	// Validate Resend configuration if emails are enabled
	if c.UseResend {
		if c.ResendAPIKey == "" {
			errors = append(errors, ValidationError{
				Field:   "RESEND_API_KEY",
				Message: "Resend API key is required when email is enabled.",
			})
		}
		if c.ResendFromEmail == "" {
			errors = append(errors, ValidationError{
				Field:   "RESEND_FROM_EMAIL",
				Message: "Resend sender email is required when email is enabled.",
			})
		}
	}

	// Validate SMTP configuration if SMTP delivery is enabled
	if c.UseSMTP {
		if c.SMTPHost == "" {
			errors = append(errors, ValidationError{
				Field:   "SMTP_HOST",
				Message: "SMTP host is required when SMTP email is enabled.",
			})
		}
		if c.SMTPFromEmail == "" {
			errors = append(errors, ValidationError{
				Field:   "SMTP_FROM_EMAIL",
				Message: "SMTP sender email is required when SMTP email is enabled.",
			})
		}
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			errors = append(errors, ValidationError{
				Field:   "SMTP_PORT",
				Message: fmt.Sprintf("Invalid SMTP port '%d'. Must be between 1 and 65535.", c.SMTPPort),
			})
		}
	}

	return errors
}

// EmailEnabled reports whether at least one email delivery channel is configured.
func (c *Config) EmailEnabled() bool {
	return c.UseResend || c.UseSMTP
}

// GetReminderLocation returns the parsed time.Location for the configured ReminderTimezone.
// Falls back to UTC if the timezone is invalid (validation should prevent this in practice).
func (c *Config) GetReminderLocation() *time.Location {
	loc, err := time.LoadLocation(c.ReminderTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ValidateOrPanic validates the configuration and panics with detailed error message if invalid
func (c *Config) ValidateOrPanic() {
	errors := c.Validate()
	if len(errors) > 0 {
		log.Println("❌ Configuration validation failed:")
		log.Println("")
		for _, err := range errors {
			log.Printf("  • %s\n", err.Error())
		}
		log.Println("")
		log.Println("Please fix the configuration errors above and restart the server.")
		log.Println("Refer to backend/.env.example for configuration examples.")
		panic("Configuration validation failed")
	}
	log.Println("✓ Configuration validated successfully")
}
