package database

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"time"

	"mycorrhizal/internal/faults"
	"mycorrhizal/logger"
	"strconv"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// openDSN appends this app's standard connection pragmas to a plain file
// path:
//   - journal_mode(WAL): the SQLite concurrency mode this app runs in. It is
//     what allows readers and a writer to coexist without blocking, but it is
//     also why a plain `cp` of the .db file is NOT a valid online backup --
//     committed writes can sit in a -wal sidecar that the copy never sees.
//     The documented online backup uses VACUUM INTO (see database/backup.go
//     and docs/deployment.md's Backups section), which snapshots the live
//     database consistently. Persisted in the database file itself once set,
//     so this only needs to run against a real file, never ":memory:".
//   - foreign_keys(1): turns on real FK enforcement  --
//     unlike journal_mode this is a per-connection setting, not persisted in
//     the file, so it must be supplied via the DSN (applied by the driver on
//     every new physical connection it opens) rather than a one-time
//     PRAGMA statement, which would only affect whichever single connection
//     ran it.
//   - busy_timeout(5000): wait up to 5s for a write lock instead of
//     immediately returning SQLITE_BUSY. The scheduled jobs fire concurrently
//     at startup and the first one to INSERT into job_executions fails the
//     others; a timeout lets them queue instead.
//   - _txlock=immediate: begin every transaction with BEGIN IMMEDIATE rather
//     than SQLite's default deferred BEGIN.
//
// The last one is not cosmetic, and busy_timeout alone does not cover it. A
// deferred transaction takes a read lock first and only tries to upgrade to a
// write lock at its first write; SQLite's busy handler is NOT invoked for that
// upgrade (it cannot safely wait — another writer may already hold a snapshot),
// so the upgrade fails instantly with SQLITE_BUSY no matter how long the
// timeout is. GORM wraps even a single Create in an implicit transaction, so
// two concurrent POSTs to a write endpoint could produce an immediate 500:
//
//	POST /api/v1/contacts -> 500 "database is locked (5) (SQLITE_BUSY)" in 4.8ms
//
// observed intermittently under the e2e suite's parallel workers, and equally
// reachable by a real client issuing concurrent writes. BEGIN IMMEDIATE takes
// the write lock up front, which IS a case the busy handler retries, so
// concurrent writers queue for up to busy_timeout instead of erroring.
// Readers are unaffected: WAL keeps them lock-free.
func openDSN(dbPath string) string {
	return dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
}

// newGormLogger returns the GORM logger every connection opened through this
// package uses.
//
// GORM's default logger (logger.Default) writes the full SQL statement with
// literal values interpolated on every query that errors, is slow, or returns
// ErrRecordNotFound, and it is not gated by LOG_LEVEL — it writes straight to
// os.Stdout via its own log.New, independently of zerolog. Because this is an
// instance-wide log with no redaction layer, a missing row or a constraint
// failure echoed its WHERE/VALUES clause verbatim — a password-reset miss
// logged `SELECT ... WHERE email = "<address>"` (issue #510, filed as #621).
// Two flags close it:
//   - ParameterizedQueries: log `?` placeholders instead of interpolated
//     values, so the email/vcard_uid/column value never reaches the log.
//   - IgnoreRecordNotFoundError: stop logging the common, benign not-found
//     SELECTs entirely.
//
// The writer is injectable so a test can point it at a *bytes.Buffer and
// assert on exactly what GORM would emit.
func newGormLogger(w io.Writer) gormLogger.Interface {
	return gormLogger.New(
		log.New(w, "", log.LstdFlags),
		gormLogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormLogger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		},
	)
}

// InitDB initializes the database connection and runs migrations
func InitDB(dbPath string) (*gorm.DB, error) {
	// Probe the file's structural integrity first (issue #921), then take the
	// mandatory pre-migration backup (issue #530), then run every pending
	// migration. migrateFileWithPreBackup is fail-closed at each step: a corrupt
	// file returns ErrDatabaseCorrupt before anything is written, and a backup
	// that cannot be written returns ErrPreMigrationBackupFailed and nothing is
	// migrated.
	if err := migrateFileWithPreBackup(dbPath); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Open GORM connection
	db, err := gorm.Open(sqlite.Open(openDSN(dbPath)), &gorm.Config{Logger: newGormLogger(os.Stdout)})
	if err != nil {
		return nil, fmt.Errorf("failed to connect with GORM: %w", err)
	}

	return db, nil
}

// OpenMigratedFile opens dbPath with this app's standard connection pragmas
// (journal_mode(WAL), foreign_keys(1), busy_timeout(5000), _txlock=immediate --
// see openDSN) but WITHOUT running migrations. dbPath must already be at the
// current schema: this is for callers that copied a pre-migrated database file
// (internal/dbtest builds the migrated schema once per test binary and hands
// each test a byte copy) or are reopening a database the same process already
// migrated. Use InitDB for a fresh or possibly-stale database.
func OpenMigratedFile(dbPath string) (*gorm.DB, error) {
	return OpenMigratedFileWithLogger(dbPath, nil)
}

// OpenMigratedFileWithLogger is OpenMigratedFile with a caller-supplied GORM
// logger. A nil logger keeps the standard os.Stdout logger every other
// connection uses. The one caller that needs this is internal/perfbench, which
// attaches a database.QueryCounter to the connection a benchmarked operation
// runs on so it can pin that operation's query shape (issue #469) — the same
// two-connection pattern controllers/hot_paths_perf_test.go uses, but through
// the real openDSN pragmas rather than a bare file path.
func OpenMigratedFileWithLogger(dbPath string, l gormLogger.Interface) (*gorm.DB, error) {
	if l == nil {
		l = newGormLogger(os.Stdout)
	}
	db, err := gorm.Open(sqlite.Open(openDSN(dbPath)), &gorm.Config{Logger: l})
	if err != nil {
		return nil, fmt.Errorf("failed to connect with GORM: %w", err)
	}
	return db, nil
}

// newMigrator builds a golang-migrate instance over the EMBEDDED migrations FS
// for an already-open database handle. Every migration entry point in this
// package goes through it, so the source of migration SQL can never differ
// between them — cmd/migrate previously built its own instance from
// `file://database/migrations`, which meant it only worked from one working
// directory and could disagree with the binary's embedded copy.
func newMigrator(db *sql.DB) (*migrate.Migrate, error) {
	driver, err := withInstance(db, &sqliteConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration instance: %w", err)
	}
	// Emit per-step progress (issue #495 action 6): without this, a migration
	// batch that runs for twenty minutes logs nothing until it finishes — a
	// long startup migration is indistinguishable from a hung one. See
	// migrationProgressLogger.
	m.Log = migrationProgressLogger{}
	return m, nil
}

// migrationProgressLogger is the golang-migrate Logger attached to every
// migrator (newMigrator). golang-migrate calls Printf at two per-step moments:
//
//   - "Read and execute <migration>" immediately BEFORE a migration body is
//     sent to the driver — the "this migration has started, it is the long
//     pole right now" signal a hung-looking upgrade needs;
//   - "Finished <migration> (read X, ran Y)" right after the body committed
//     AND its version row was marked clean — the step-done signal with a
//     duration.
//
// The step events are named distinctly from the batch-level
// migration_completed / migration_failed events so a log stream can tell a
// per-step heartbeat from the whole-run outcome. Both step lines are INFO so
// they are visible at the default log level, not buried behind verbose.
type migrationProgressLogger struct{}

// Verbose is true so golang-migrate emits the "Read and execute" (starting)
// line; it is false by default, which would leave only the finish line.
func (migrationProgressLogger) Verbose() bool { return true }

func (migrationProgressLogger) Printf(format string, v ...interface{}) {
	switch {
	case strings.HasPrefix(format, "Read and execute"):
		logger.Info().
			Str(logger.FieldEvent, "migration_step_started").
			Str(logger.FieldComponent, "migration").
			Str("migration", migrationLogName(v)).
			Msg("migration step started")
	case strings.HasPrefix(format, "Finished "):
		logger.Info().
			Str(logger.FieldEvent, "migration_step_completed").
			Str(logger.FieldComponent, "migration").
			Str("migration", migrationLogName(v)).
			Int64(logger.FieldDurationMS, migrationStepElapsed(v)).
			Msg("migration step completed")
	default:
		// "Start buffering"/"Scheduled"/"Closing source and database" and the
		// golang-migrate "error: ..." lines — noise for the operator heartbeat.
		logger.Debug().
			Str(logger.FieldComponent, "migration").
			Msg(strings.TrimSpace(fmt.Sprintf(format, v...)))
	}
}

// migrationLogName extracts the NNNNNN_name.up.sql filename from a
// golang-migrate LogString ("32/u 000032_contact_sync_conflicts"), or "" when
// it cannot be parsed. Reuses migrationFileForVersion so the per-step log
// field matches the migration_failed event's field exactly.
func migrationLogName(v []interface{}) string {
	if len(v) == 0 {
		return ""
	}
	s, ok := v[0].(string)
	if !ok {
		return ""
	}
	ver, _, found := strings.Cut(s, "/")
	if !found {
		return ""
	}
	n, err := strconv.ParseUint(ver, 10, 32)
	if err != nil {
		return ""
	}
	return migrationFileForVersion(uint(n))
}

// migrationStepElapsed sums the read+run durations golang-migrate reports on
// its "Finished <migration> (read X, ran Y)" line.
func migrationStepElapsed(v []interface{}) int64 {
	var total time.Duration
	for _, d := range v[1:] {
		if dur, ok := d.(time.Duration); ok {
			total += dur
		}
	}
	return total.Milliseconds()
}

// closeMigrator releases the resources newMigrator opened. golang-migrate's
// source driver leaves a goroutine parked forever waiting to hand off the
// next migration until Close() runs, and Close()'s database half closes the
// *sql.DB passed into newMigrator (sqliteDriver.Close(), see
// sqlite_driver.go) -- both are silent leaks otherwise. Every real-DB test
// (CLAUDE.md's "test against the real migrated schema" trap) calls InitDB,
// so skipping this piles up one leaked goroutine + one open sqlite handle
// per test; across the full suite that was enough to exhaust file
// descriptors and blow the CI job's 20-minute timeout (issue #415 PR
// review). *sql.DB.Close is idempotent, so this is safe even for callers
// that also defer-close their own sqlDB.
func closeMigrator(m *migrate.Migrate) {
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		logger.Warn().AnErr("source", srcErr).AnErr("database", dbErr).Msg("failed to close migrator")
	}
}

// SupportedUpgradeFloorVersion is the migration version of the oldest release
// supported for in-place upgrade (issue #529): v0.6.0, whose schema is
// migrations 000001-000031. Everything at or above it is covered by the
// schema-fixture set (internal/schemafixture). A database whose applied
// version is below this refuses to migrate (see checkSupportedUpgradeFloor):
// the policy is "upgrade to v0.6.0 first, then continue" as a documented
// two-step, never a best-effort single hop.
const SupportedUpgradeFloorVersion uint = 31

// SupportedUpgradeFloorTag is the release tag that defined the floor.
const SupportedUpgradeFloorTag = "v0.6.0"

// faultMigrationBeforeBatch is the failure-injection seam for the "before any
// migration begins" window (DEPLOY-03, issue #452). It fires in
// runPendingMigrations immediately before m.Up(), AFTER the fail-closed
// preflight has passed and AFTER the mandatory pre-migration backup (issue
// #530) has been taken, but BEFORE the first migration statement runs — the
// crash signature of a process killed in exactly that gap: the schema is
// completely untouched, no dirty flag, and a restart migrates normally.
//
// It complements faultMigrationStatement (sqlite_driver.go), which fires AFTER
// the first migration body commits and leaves the database dirty. The
// external-fault CI job parks a subprocess here with
// `MYCORRHIZAL_FAULTS=database.migration.before_batch:pause:<dur>` and then
// SIGKILLs it. Unarmed, faults.Hook is a nil-returning map lookup. See
// docs/development/fault-injection.md.
const faultMigrationBeforeBatch = "database.migration.before_batch"

// ErrDirtyMigration is the error RunMigrations returns when the database is in
// a dirty migration state (issue #439 state 1 / issue #546). golang-migrate
// marks a migration dirty when it starts and does not finish — the process was
// killed, the container was OOM-killed, the host lost power, or the SQL failed
// partway — so the schema is in an unknown, partially-applied state. The old
// behavior force-cleared the flag at boot and re-ran from the next migration,
// presenting a half-applied schema as healthy; that is the fail-open bug #546
// exists to close. This refusal names the dirty version, what it means, and the
// recovery path (restore the pre-migration backup), so an operator is never
// left guessing. It is a stable, assertable sentinel the server start path logs
// via logger.Fatal and the migrate CLI prints.
type ErrDirtyMigration struct {
	Version uint
}

func (e *ErrDirtyMigration) Error() string {
	return fmt.Sprintf(
		"database is in a dirty migration state at version %d: a migration started and did not finish, "+
			"so the schema may be only partially applied and does not match any known version. "+
			"Refusing to start (fail-closed). Restore the pre-migration backup and start again — "+
			"see docs/deployment.md (Backups → Restore). If you have verified the schema actually matches "+
			"version %d, the operator-only escape hatch is `make migrate-force` (or `go run cmd/migrate force`), "+
			"which prompts for explicit confirmation; it is never applied on the startup path.",
		e.Version, e.Version,
	)
}

// ErrSchemaAheadOfBinary is the error RunMigrations returns when the database's
// schema is ahead of this binary (issue #439 state 2): the database carries
// migrations this binary does not know about, meaning it was migrated by a
// newer release and this binary has been rolled back. Downgrade is unsupported
// (issue #530), so starting anyway would have the binary misread columns it
// does not know about — the same silent-corruption class as the dirty-force
// bug. The refusal names both versions and the recovery path so the operator
// knows it is a bad rollback in progress, not a random boot failure.
type ErrSchemaAheadOfBinary struct {
	Version       uint
	BinaryVersion uint
}

func (e *ErrSchemaAheadOfBinary) Error() string {
	return fmt.Sprintf(
		"database schema version %d is ahead of this binary (latest known migration %d): the database "+
			"was migrated by a newer release and this binary has been rolled back. Downgrade is unsupported, "+
			"so refusing to start. Deploy a binary that knows migration %d (or newer) and start again, or "+
			"restore the backup taken before the newer release ran — see docs/deployment.md (Backups → Restore).",
		e.Version, e.BinaryVersion, e.Version,
	)
}

// ErrSubFloorMigration is the error RunMigrations returns when the database's
// schema predates the supported-upgrade floor. It is deliberately a stable,
// assertable sentinel carrying the required-intermediate message: the server
// start path logs it via logger.Fatal (main.go's "Failed to initialize
// database") and the migrate CLI prints it — both loud, both before any
// migration runs.
type ErrSubFloorMigration struct {
	Version uint
}

func (e *ErrSubFloorMigration) Error() string {
	return fmt.Sprintf(
		"database schema version %d predates the supported upgrade floor (%s, migration %d). "+
			"In-place upgrade is supported only from %s and later; this version refuses to migrate a pre-floor database. "+
			"Upgrade this instance to %s first, then run this version again — see docs/upgrade-compatibility.md.",
		e.Version, SupportedUpgradeFloorTag, SupportedUpgradeFloorVersion, SupportedUpgradeFloorTag, SupportedUpgradeFloorTag,
	)
}

// ErrPopulatedVersionlessDatabase is returned when the applied-migration version
// is unknown (no schema_migrations row) but application tables already exist
// (issue #926). A populated database whose version row was removed — disk
// corruption, a hand-edit, a restore from a tool that dropped the bookkeeping
// table — used to read as version 0 and be treated as a *fresh install*: the
// mandatory pre-migration backup (issue #530) was skipped, the preflight passed
// (0 ≤ latest, clean), and m.Up() replayed 000001 against existing tables,
// failing partway and leaving the database dirty at a low version with no
// rollback snapshot — defeating the #530 guarantee exactly when it matters.
//
// The fix is fail-closed: an empty schema_migrations row set beside real tables
// is a tampered or corrupted database, not a clean install, and is refused with
// its own typed error. cmd/migrate prints it and the server start path logs it
// via logger.Fatal before any migration runs.
type ErrPopulatedVersionlessDatabase struct {
	// Tables is a bounded sample of the tables found, for the operator's "yes,
	// that is really my database" recognition. Never the full list.
	Tables []string
}

func (e *ErrPopulatedVersionlessDatabase) Error() string {
	return fmt.Sprintf(
		"database carries application tables (%s) but no applied-migration version: "+
			"the schema_migrations record is missing while the database is not empty. Treating this as a "+
			"fresh install would replay every migration against existing tables and silently skip the "+
			"mandatory pre-migration backup. Refusing to start (fail-closed); the database is untouched. "+
			"Restore it from a known-good backup, or if you are certain the schema is current, restore its "+
			"schema_migrations row — see docs/operations/migration-recovery.md.",
		strings.Join(e.Tables, ", "),
	)
}

// applicationTables returns up to five non-internal table names other than
// schema_migrations, ordered for determinism. An empty result means a genuinely
// fresh database (or an empty schema_migrations-only file); a non-empty result
// means the database has been migrated before (issue #926). Internal SQLite
// tables (`sqlite_%`) and the migration bookkeeping table are excluded.
func applicationTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(
		`SELECT name FROM sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> ?
		 ORDER BY name LIMIT 5`, defaultMigrationsTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// subFloorMigrationEnvVar is the documented escape hatch for the one-time
// sub-floor bridge (issue #529 action 5, docs/upgrade-compatibility.md). The
// normal path is a two-step upgrade through a v0.6.0 release binary, which
// never needs this; the bridge exists so the maintainer's own
// v0.2.0-alpha-candidate deployment (and the chain-preservation regression
// test that exercises it) can run the full chain in one binary when the
// v0.6.0 intermediate cannot be produced. Setting it is a deliberate,
// logged decision, never silent.
const subFloorMigrationEnvVar = "MYCORRHIZAL_ALLOW_SUB_FLOOR_MIGRATION"

// subFloorMigrationAllowed reports whether the one-time bridge override is
// set. A true value is logged loudly at the call site, not swallowed.
func subFloorMigrationAllowed() bool {
	return os.Getenv(subFloorMigrationEnvVar) == "1"
}

// checkSupportedUpgradeFloor refuses to migrate a database whose schema
// predates the v0.6.0 floor (issue #529 action 4). A fresh database (no
// schema_migrations row, version 0) is not a sub-floor database and always
// passes. A CLEAN sub-floor database is a real pre-floor deployment and returns
// an ErrSubFloorMigration that names v0.6.0 as the required intermediate — a
// partial migration or a crash is the alternative the policy explicitly
// rejects. A DIRTY sub-floor database never reaches this check: the dirty
// refusal (checkMigrationPreflight) fires first, because a dirty flag means the
// schema state is unknown regardless of the version (issue #546).
func checkSupportedUpgradeFloor(version uint) error {
	if version == 0 || version >= SupportedUpgradeFloorVersion {
		return nil
	}
	if subFloorMigrationAllowed() {
		logger.Warn().
			Str(logger.FieldComponent, "migration").
			Uint("version", version).
			Msg("ALLOW_SUB_FLOOR_MIGRATION is set: migrating a pre-v0.6.0 database. This is the documented one-time bridge (docs/upgrade-compatibility.md), not a supported upgrade path.")
		return nil
	}
	return &ErrSubFloorMigration{Version: version}
}

// checkMigrationPreflight applies the fail-closed startup gates (MIG-04, issue
// #439) in order, before ANY migration is applied:
//
//  1. a dirty database (state 1, issue #546) — a migration started and did not
//     finish, so the schema is in an unknown, partially-applied state. Refuse.
//  2. a database ahead of the binary (state 2) — the database knows migrations
//     this binary does not, meaning a rollback is in progress. Refuse.
//  3. a sub-floor database (state 3, issue #529) — predates the supported
//     upgrade floor. Refuse, naming the v0.6.0 intermediate.
//
// Each returns its own typed error so health/readiness and the diagnostics run
// can report WHICH state an install is in rather than a generic boot failure.
// Each refusal leaves the database exactly as it was — nothing is written. A
// fresh database (version 0, clean) passes all three.
func checkMigrationPreflight(version uint, dirty bool, latest uint) error {
	if dirty {
		return &ErrDirtyMigration{Version: version}
	}
	if version > latest {
		return &ErrSchemaAheadOfBinary{Version: version, BinaryVersion: latest}
	}
	return checkSupportedUpgradeFloor(version)
}

// RunMigrations runs all pending database migrations.
//
// It is fail-closed (MIG-04, issue #439): before applying anything it refuses
// a dirty database (state 1 — issue #546), a database ahead of this binary
// (state 2), and a sub-floor database (state 3 — issue #529), each with its
// own typed error naming the state and its recovery. There is no "start anyway
// and hope" path and no configuration setting turns any refusal into a
// warning. The operator-only escape hatch for a dirty database lives in the
// migrate CLI (`force`, which prompts), never on the startup path.
//
// This is the no-backup primitive: it takes a *sql.DB and has no path, so it
// cannot snapshot. The mandatory pre-migration backup (issue #530) is taken by
// migrateFileWithPreBackup, the path-taking wrapper behind InitDB and
// MigrateUp. Callers that hold only a handle (tests, the schema-fixture
// tooling) use this directly and are responsible for their own backup policy.
func RunMigrations(db *sql.DB) error {
	m, err := newMigrator(db)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	// Get current version
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	// A version-less database that still carries application tables is not a
	// fresh install (issue #926): refuse before the preflight treats version 0
	// as a clean install and before m.Up() replays the chain against existing
	// tables. A genuinely fresh database has no such tables (only an optional
	// empty schema_migrations bookkeeping table).
	if err == migrate.ErrNilVersion {
		tables, terr := applicationTables(db)
		if terr != nil {
			return fmt.Errorf("failed to inspect database tables: %w", terr)
		}
		if len(tables) > 0 {
			return &ErrPopulatedVersionlessDatabase{Tables: tables}
		}
	}

	// Fail-closed preflight (issue #439): dirty -> refuse; ahead of binary ->
	// refuse; sub-floor -> refuse. Must run BEFORE m.Up() so no migration is
	// ever applied to a state whose schema is unknown or not covered by the
	// supported upgrade matrix.
	latest, err := LatestMigrationVersion()
	if err != nil { // # pragma: no cover -- the embedded migrations FS always has at least one migration
		return fmt.Errorf("failed to resolve latest migration version: %w", err)
	}
	if err := checkMigrationPreflight(version, dirty, latest); err != nil {
		return err
	}

	return runPendingMigrations(m, db)
}

// runPendingMigrations applies every pending migration from the database's
// current version and reports the outcome (the issue #532 failure diagnostics
// and the issue #424 operational event). Shared by RunMigrations and the
// operator-only force path (MigrateForce): after the force clears the dirty
// flag, both continue from the same place through the same code.
func runPendingMigrations(m *migrate.Migrate, db *sql.DB) error {
	startVersion, _, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion { // # pragma: no cover -- a migration driver the version read just succeeded on
		return fmt.Errorf("failed to get migration version: %w", err)
	}
	version := startVersion

	// DEPLOY-03 (issue #452) failure-injection seam: the "before any migration
	// begins" window. The preflight has passed and the mandatory pre-migration
	// backup (issue #530) is already taken; nothing in the schema has changed
	// yet. An armed error fault aborts here with the database completely
	// untouched — the crash signature of a process killed in this gap, whose
	// defined outcome is "a restart just migrates normally". A pause fault
	// parks a subprocess here for the external-fault CI job to SIGKILL.
	// Unarmed, this is a nil-returning map lookup. See faultMigrationBeforeBatch.
	if err := faults.Hook(faultMigrationBeforeBatch); err != nil {
		return fmt.Errorf("migration aborted before applying any migration: %w", err)
	}

	start := time.Now()

	// Run migrations
	upErr := m.Up()
	if upErr != nil && upErr != migrate.ErrNoChange {
		// Milestone v0.6.2 gate (issue #532): a migration failure must
		// identify WHICH migration failed, not just carry the SQL error.
		// golang-migrate leaves the failed version dirty, so m.Version()
		// names it; the filename comes from the embedded FS. The returned
		// error, the structured log line, and the system_events row (when the
		// table exists) all carry it.
		version, _, verr := m.Version()
		name := migrationFileForVersion(version)
		elapsed := time.Since(start)

		logger.Error().
			Err(upErr).
			Str(logger.FieldEvent, "migration_failed").
			Str(logger.FieldComponent, "migration").
			Uint("version", version).
			Str("migration", name).
			Int64(logger.FieldDurationMS, elapsed.Milliseconds()).
			Msg("migration failed")
		recordMigrationFailedEvent(db, startVersion, version, elapsed, upErr)

		if verr == nil {
			return fmt.Errorf("failed to apply migrations: version %d (%s): %w", version, name, upErr)
		}
		return fmt.Errorf("failed to apply migrations: %w", upErr) // # pragma: no cover -- a version read that just succeeded cannot fail twice
	}

	// Get final version
	version, _, err = m.Version()
	if err != nil && err != migrate.ErrNilVersion { // # pragma: no cover -- a migration driver the version read just succeeded on
		return fmt.Errorf("failed to get final version: %w", err)
	}

	elapsed := time.Since(start)
	schemaAdvanced := upErr != migrate.ErrNoChange && version != startVersion

	switch {
	case err == migrate.ErrNilVersion: // # pragma: no cover -- m.Up() always writes a version row
		logger.Info().Msg("No migrations applied (database is empty)")
	case schemaAdvanced:
		logger.Info().
			Str(logger.FieldEvent, "migration_completed").
			Str(logger.FieldComponent, "migration").
			Uint("from_version", startVersion).
			Int64(logger.FieldDurationMS, elapsed.Milliseconds()).
			Uint("version", version).
			Str(logger.FieldResult, logger.ResultSuccess).
			Msg("Migrations applied successfully")
	default:
		logger.Info().Uint("version", version).Msg("No pending migrations")
	}

	// Best-effort operational event when migrations actually advanced the
	// schema (issue #424). Written by raw SQL on the same *sql.DB — the
	// system_events table exists once migration 000038 has run, and any
	// earlier-schema path where it does not yet exist simply drops the row.
	if schemaAdvanced {
		recordMigrationEvent(db, startVersion, version, elapsed)
	}

	return nil
}

// recordMigrationEvent inserts one migration_completed row, swallowing every
// error: a diagnostic write must never be able to fail a boot.
func recordMigrationEvent(db *sql.DB, from, to uint, elapsed time.Duration) {
	defer func() { _ = recover() }()
	now := time.Now().UTC()
	ms := elapsed.Milliseconds()
	_, err := db.Exec(
		`INSERT INTO system_events (created_at, occurred_at, event_type, severity, component, operation, duration_ms, result, detail)
		 VALUES (?, ?, 'migration_completed', 'info', 'migration', 'run_migrations', ?, 'success', ?)`,
		now, now, ms, fmt.Sprintf("from_version=%d to_version=%d", from, to),
	)
	if err != nil {
		logger.Debug().Err(err).Msg("could not record migration_completed system event (pre-000038 schema?)")
	}
}

// recordMigrationFailedEvent inserts one migration_failed row, swallowing every
// error: the table may not exist yet (a migration before 000038 failed) and a
// diagnostic write must never be able to fail a boot. The error text is
// sanitized and length-capped like every other persisted diagnostic field —
// the raw golang-migrate error embeds the whole failing migration body, which
// would otherwise bloat the row.
func recordMigrationFailedEvent(db *sql.DB, from, version uint, elapsed time.Duration, upErr error) {
	defer func() { _ = recover() }()
	now := time.Now().UTC()
	ms := elapsed.Milliseconds()
	errText := logger.SanitizeLogField(upErr.Error())
	if runes := []rune(errText); len(runes) > maxMigrationEventErrorLen {
		errText = string(runes[:maxMigrationEventErrorLen])
	}
	_, err := db.Exec(
		`INSERT INTO system_events (created_at, occurred_at, event_type, severity, component, operation, duration_ms, result, error, detail)
		 VALUES (?, ?, 'migration_failed', 'error', 'migration', 'run_migrations', ?, 'failure', ?, ?)`,
		now, now, ms, errText, fmt.Sprintf("from_version=%d to_version=%d", from, version),
	)
	if err != nil {
		logger.Debug().Err(err).Msg("could not record migration_failed system event (pre-000038 schema?)")
	}
}

// maxMigrationEventErrorLen caps the persisted migration_failed error text,
// matching the 1024-rune discipline the models package applies to system_event
// free-text fields.
const maxMigrationEventErrorLen = 1024

// migrationFileForVersion returns the NNNNNN_name.up.sql filename for the
// given migration version from the embedded FS, or "" when no file matches.
// Used by the migration-failure path (issue #532) so a failed boot names the
// migration that failed rather than only echoing the SQL error.
func migrationFileForVersion(version uint) string {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, found := strings.Cut(name, "_")
		if !found {
			continue
		}
		v, err := strconv.ParseUint(prefix, 10, 32)
		if err != nil {
			continue
		}
		if uint(v) == version {
			return name
		}
	}
	return ""
}

// MigrateUp applies every pending migration to the database at dbPath, after
// taking the mandatory pre-migration backup (issue #530). It is the operator's
// manual `make migrate-up` path and is fail-closed on the backup exactly like
// the server's startup path (InitDB): both go through migrateFileWithPreBackup.
// Thin path-taking wrapper so cmd/migrate does not duplicate the DSN pragmas —
// the CLI reaching for its own sql.Open and its own migration source is exactly
// how it drifted out of sync with the app before (see MigrateDown's note below).
func MigrateUp(dbPath string) error {
	return migrateFileWithPreBackup(dbPath)
}

// migrateFileWithPreBackup is the shared file-level upgrade path behind InitDB
// (server startup) and MigrateUp (`make migrate-up`): probe the file's
// structural integrity (issue #921), take the mandatory pre-migration backup
// (issue #530), then apply every pending migration.
//
// The integrity probe is first so a corrupt file is neither snapshotted as the
// rollback point nor migrated; a missing file is a fresh install and passes.
// The backup is the load-bearing half of the rollback policy — downgrade is
// unsupported, so a snapshot taken before the upgrade is the only way back to
// the previous version. It is fail-closed: a backup that cannot be written
// returns ErrPreMigrationBackupFailed and NOTHING is migrated. There is no
// configuration that disables it (the env var only moves the target).
//
// It is taken only when there is real data to lose AND migrations to run:
//   - a fresh database (no schema_migrations row) is a clean install — skip;
//   - a database already at the latest version has nothing pending — skip;
//   - a dirty database, a database ahead of this binary, and a sub-floor
//     database without the one-time bridge are all states RunMigrations
//     refuses anyway — skip the backup and let it produce the typed refusal.
//
// RunMigrations then re-runs the same fail-closed preflight before applying
// anything; the version read here is only to decide whether to snapshot.
func migrateFileWithPreBackup(dbPath string) error {
	// Fail-closed startup corruption probe (issue #921), before the backup and
	// before any migration: a structurally unsound file is neither safe to
	// snapshot as the rollback point nor safe to migrate. A missing file is a
	// fresh install and passes.
	if err := probeStartupIntegrity(dbPath); err != nil {
		return err
	}

	version, dirty, ok, err := MigrationVersion(dbPath)
	if err != nil {
		return err
	}
	latest, err := LatestMigrationVersion()
	if err != nil { // # pragma: no cover -- the embedded migrations FS always has at least one migration
		return err
	}

	if shouldTakePreMigrationBackup(version, dirty, ok, latest) {
		if _, err := takePreMigrationBackup(dbPath, version, latest); err != nil {
			return err
		}
	}

	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	if err != nil { // # pragma: no cover -- sql.Open is lazy; a file DSN does not fail here
		return fmt.Errorf("failed to open database: %w", err)
	}
	// RunMigrations closes sqlDB through the migrator's database half
	// (closeMigrator); no defer close here, matching the pre-#530 InitDB.
	return RunMigrations(sqlDB)
}

// shouldTakePreMigrationBackup decides whether migrateFileWithPreBackup
// snapshots before migrating. It returns true only for a clean, non-empty
// database that is behind the latest schema and either at/above the upgrade
// floor or carrying the one-time sub-floor bridge override — i.e. a real
// pending upgrade of real data. Every other state is either nothing-to-back-up
// (fresh, already current) or a state RunMigrations will refuse (dirty, ahead
// of the binary, sub-floor without the bridge).
func shouldTakePreMigrationBackup(version uint, dirty, ok bool, latest uint) bool {
	if !ok || dirty || version >= latest {
		return false
	}
	if version < SupportedUpgradeFloorVersion && !subFloorMigrationAllowed() {
		return false
	}
	return true
}

// MigrateForce is the operator-only recovery for a dirty database (MIG-04,
// issue #439 state 1 / issue #546). It force-marks the CURRENT dirty version
// clean and then re-runs pending migrations from the next one — the crash
// recovery golang-migrate's dirty flag is for — but unlike the old startup
// path it is never invoked automatically: the migrate CLI calls it only after
// an explicit interactive confirmation, and the server's startup path
// (RunMigrations) has no equivalent.
//
// It refuses when the database is NOT dirty (nothing to force), has no
// migrations applied at all, or is dirty at a version AHEAD of this binary
// (forcing an unknown version would "confirm" a schema the binary cannot
// validate and has nothing to re-run — roll forward or restore instead). The
// operator's explicit confirmation is the policy gate here, so the sub-floor
// floor check (checkSupportedUpgradeFloor) does NOT re-apply: a dirty database
// is by definition not a stable pre-floor deployment, and the crashed-initial-
// install recovery (dirty at a low version) is exactly what force exists for.
func MigrateForce(dbPath string) error {
	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	if err != nil { // # pragma: no cover -- sql.Open is lazy; the driver does not fail until a query runs
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	version, dirty, err := m.Version()
	if err == migrate.ErrNilVersion {
		return errors.New("no migrations have been applied; there is no dirty state to force")
	}
	if err != nil { // # pragma: no cover -- a migrator the Ping of which just succeeded
		return fmt.Errorf("failed to get migration version: %w", err)
	}
	if !dirty {
		return fmt.Errorf("database is not dirty (version %d clean); force is only for an interrupted migration", version)
	}

	latest, err := LatestMigrationVersion()
	if err != nil { // # pragma: no cover -- the embedded migrations FS always has at least one migration
		return fmt.Errorf("failed to resolve latest migration version: %w", err)
	}
	if version > latest {
		return &ErrSchemaAheadOfBinary{Version: version, BinaryVersion: latest}
	}

	logger.Warn().
		Str(logger.FieldComponent, "migration").
		Uint("version", version).
		Msg("operator invoked migrate-force: marking dirty version clean and re-running pending migrations")

	if err := m.Force(int(version)); err != nil { // # pragma: no cover -- a write the version row already withstood
		return fmt.Errorf("failed to force version %d: %w", version, err)
	}

	return runPendingMigrations(m, sqlDB)
}

// MigrateUpTo migrates the database at dbPath to exactly the given migration
// version, not beyond. It is the primitive the release-schema dump generator
// (cmd/genschema) and the upgrade-fixture loader build historical schemas
// with: the migration chain is linear and append-only, so a release's schema
// is fully determined by its highest applied version (issue #436). Same DSN
// pragmas and embedded migration source as MigrateUp — a no-op (ErrNoChange)
// when the database is already at version.
func MigrateUpTo(dbPath string, version uint) error {
	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Migrate(version); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to migrate to version %d: %w", version, err)
	}
	return nil
}

// MigrationVersion reports the applied migration version and whether the
// database is in a dirty state. Returns ok=false when no migration has ever
// been applied.
func MigrationVersion(dbPath string) (version uint, dirty bool, ok bool, err error) {
	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	if err != nil {
		return 0, false, false, fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB)
	if err != nil {
		return 0, false, false, err
	}
	defer closeMigrator(m)

	version, dirty, err = m.Version()
	if err == migrate.ErrNilVersion {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, fmt.Errorf("failed to get migration version: %w", err)
	}
	return version, dirty, true, nil
}

// MigrateDown rolls back exactly ONE migration.
//
// It is `m.Steps(-1)`, deliberately, and must stay that way. cmd/migrate used
// to call golang-migrate's `m.Down()` directly instead of this function —
// which rolls back *every* migration and drops the whole schema — while the
// Makefile advertised the target as "Rollback the last migration". With the
// migrations squashed to a single initial schema, running the documented
// command would have destroyed the database. The CLI now delegates here so
// there is one implementation of "down" rather than two that disagree.
func MigrateDown(dbPath string) error {
	sqlDB, err := sql.Open("sqlite", openDSN(dbPath))
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Steps(-1); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	logger.Info().Msg("Migration rolled back successfully")
	return nil
}

// LatestMigrationVersion reports the highest migration version bundled in the
// embedded migrations FS — i.e. the version a freshly-migrated database should
// be at. It parses the numeric prefix of every `NNNNNN_*.up.sql` entry and
// returns the maximum. Used by the readiness/deep-health checks (issue #421)
// to detect a database that is behind the binary's schema without opening a
// second connection or running a migration.
func LatestMigrationVersion() (uint, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return 0, fmt.Errorf("failed to read embedded migrations: %w", err)
	}

	var latest uint
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, found := strings.Cut(name, "_")
		if !found {
			continue
		}
		// bitSize 32 (not 64): migration prefixes are 6-digit NNNNNN, and
		// parsing to a width no larger than uint's guaranteed minimum keeps the
		// uint(v) conversion provably in range (CodeQL go/incorrect-integer-conversion).
		v, err := strconv.ParseUint(prefix, 10, 32)
		if err != nil {
			continue
		}
		if uint(v) > latest {
			latest = uint(v)
		}
	}

	if latest == 0 {
		return 0, fmt.Errorf("no migration files found in embedded FS")
	}
	return latest, nil
}

// AppliedMigrationVersion reads the currently-applied migration version and
// dirty flag straight from the live database's schema_migrations table, using
// the app's existing *gorm.DB (no second sql.Open, unlike MigrationVersion,
// which the frequently-polled readiness probe should not pay per request).
// ok is false when no migration has ever been applied (empty table).
func AppliedMigrationVersion(db *gorm.DB) (version uint, dirty bool, ok bool, err error) {
	var row struct {
		Version uint
		Dirty   bool
	}
	res := db.Raw("SELECT version, dirty FROM " + defaultMigrationsTable + " LIMIT 1").Scan(&row)
	if res.Error != nil {
		return 0, false, false, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, false, false, nil
	}
	return row.Version, row.Dirty, true, nil
}
