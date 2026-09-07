package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mycorrhizal/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResidentKBReadsProc(t *testing.T) {
	kb := residentKB()
	assert.Greater(t, kb, int64(0), "the process must have a measurable resident set on Linux")
}

func TestStartSamplerTracksPeaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.db")
	db, err := database.InitDB(path)
	require.NoError(t, err)
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}

	stop := startSampler(path)
	// Hold the sampler open long enough to tick several times, and write a row
	// so the database file grows past its initial size.
	db2, err := database.OpenMigratedFile(path)
	require.NoError(t, err)
	require.NoError(t, db2.Exec("INSERT INTO users (username, email, password) VALUES ('sampler','sampler@example.com','x')").Error)
	time.Sleep(300 * time.Millisecond)
	if sqlDB, err := db2.DB(); err == nil {
		_ = sqlDB.Close()
	}

	s := stop()
	assert.Greater(t, s.peakRSSKb, int64(0), "the sampler must observe the process's resident memory")
	assert.Greater(t, s.peakDB, s.initialDB, "the sampler must observe the database growing past its initial size")
	assert.Greater(t, s.peakDB-s.initialDB, int64(0), "peak additional disk must be positive")
}

func TestMeasureReportsMigration(t *testing.T) {
	// Build a small floor database and measure migrating it: the report must
	// name the from/to versions, report a duration, pass integrity, and stay
	// clean.
	src := filepath.Join(t.TempDir(), "seed.db")
	require.NoError(t, runSeed([]string{"--contacts", "30", "--db", src}))

	floor := filepath.Join(t.TempDir(), "floor.db")
	require.NoError(t, runCheckpoint([]string{"--db", src, "--version", "31", "--out", floor}))

	out := captureStdout(t, func() {
		require.NoError(t, runMeasure([]string{"--db", floor}))
	})
	require.Contains(t, out, "migratebench_report")
	require.Contains(t, out, "from_version=31")
	require.Contains(t, out, "integrity=ok")
	require.Contains(t, out, "dirty=false")
	require.Contains(t, out, "peak_rss_kb=")
}

func TestRunCLIUsageErrors(t *testing.T) {
	assert.Equal(t, 2, runCLI([]string{}), "no subcommand is a usage error")
	assert.Equal(t, 2, runCLI([]string{"bogus"}), "an unknown subcommand is a usage error")
}

func TestRunCLISuccess(t *testing.T) {
	// Drive every subcommand through the CLI dispatcher so the exit-code
	// wiring (not just the subcommand functions) is exercised.
	src := filepath.Join(t.TempDir(), "src.db")
	assert.Equal(t, 0, runCLI([]string{"seed", "--contacts", "15", "--db", src}))

	floor := filepath.Join(t.TempDir(), "floor.db")
	assert.Equal(t, 0, runCLI([]string{"checkpoint", "--db", src, "--version", "31", "--out", floor}))

	captureStdout(t, func() {
		assert.Equal(t, 0, runCLI([]string{"measure", "--db", floor}))
	})
}

func TestMeasureFallsBackToEnvPath(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.db")
	require.NoError(t, runSeed([]string{"--contacts", "15", "--db", src}))
	floor := filepath.Join(t.TempDir(), "floor.db")
	require.NoError(t, runCheckpoint([]string{"--db", src, "--version", "31", "--out", floor}))

	t.Setenv("SQLITE_DB_PATH", floor)
	captureStdout(t, func() {
		require.NoError(t, runMeasure([]string{}), "with no --db, measure must read SQLITE_DB_PATH")
	})
}

func TestBadFlagIsAParseError(t *testing.T) {
	require.Error(t, runCheckpoint([]string{"--bogus"}))
	require.Error(t, runMeasure([]string{"--bogus"}))
}

func TestRunCLIReportsSubcommandFailure(t *testing.T) {
	// A subcommand that fails must print its error to stderr and exit 1, not
	// panic or exit 0.
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	code := runCLI([]string{"seed", "--contacts", "5"}) // --db missing
	_ = w.Close()
	os.Stderr = old
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	assert.Equal(t, 1, code)
	assert.Contains(t, string(buf[:n]), "--db is required")
}

func TestSeedRefusesBadFlags(t *testing.T) {
	err := runSeed([]string{"--contacts", "0", "--db", filepath.Join(t.TempDir(), "x.db")})
	require.Error(t, err)
	err = runSeed([]string{"--contacts", "10"})
	require.Error(t, err, "--db is required")
	err = runSeed([]string{"--bogus", "10", "--db", filepath.Join(t.TempDir(), "x.db")})
	require.Error(t, err, "an unknown flag is a parse error")
}

func TestSeedContactsAndProfileAreMutuallyExclusive(t *testing.T) {
	db := filepath.Join(t.TempDir(), "x.db")
	err := runSeed([]string{"--db", db})
	require.Error(t, err, "neither --contacts nor --profile is an error")
	require.Contains(t, err.Error(), "exactly one")

	err = runSeed([]string{"--contacts", "15", "--profile", "smoke", "--db", db})
	require.Error(t, err, "both --contacts and --profile is an error")
	require.Contains(t, err.Error(), "exactly one")
}

func TestSeedRejectsUnknownProfile(t *testing.T) {
	err := runSeed([]string{"--profile", "enormous", "--db", filepath.Join(t.TempDir(), "x.db")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown --profile")
}

func TestSeedWithProfile(t *testing.T) {
	// The concrete "the same generator serves #495 and #468" wiring: seeding
	// through the PERF-01 profile catalogue builds a real multi-user,
	// graph-shaped database on the same harness the migration test uses.
	dbPath := filepath.Join(t.TempDir(), "smoke.db")
	out := captureStdout(t, func() {
		assert.Equal(t, 0, runCLI([]string{"seed", "--profile", "smoke", "--db", dbPath}))
	})
	require.Contains(t, out, "migratebench seed: profile=smoke")
	require.Contains(t, out, "users=2")
	require.Contains(t, out, "contacts=324", "smoke = 162 contacts/user x 2 users")

	db, err := database.OpenMigratedFile(dbPath)
	require.NoError(t, err)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	var users, contacts, edges int64
	require.NoError(t, db.Table("users").Count(&users).Error)
	require.NoError(t, db.Table("contacts").Count(&contacts).Error)
	require.NoError(t, db.Table("relationship_edges").Count(&edges).Error)
	assert.EqualValues(t, 2, users)
	assert.EqualValues(t, 324, contacts)
	assert.Greater(t, edges, int64(100), "the graph shape adds hub + chain edges on top of the block edges")
}

func TestCheckpointRefusesBadInputs(t *testing.T) {
	_, err := os.Stat(filepath.Join(t.TempDir(), "none.db"))
	err = runCheckpoint([]string{"--db", "src.db"})
	require.Error(t, err, "--out is required")

	src := filepath.Join(t.TempDir(), "src.db")
	require.NoError(t, runSeed([]string{"--contacts", "15", "--db", src}))

	out := filepath.Join(t.TempDir(), "out.db")
	err = runCheckpoint([]string{"--db", src, "--version", "0", "--out", out})
	require.Error(t, err, "version 0 is not a valid checkpoint target")

	require.NoError(t, runCheckpoint([]string{"--db", src, "--version", "31", "--out", out}))
	err = runCheckpoint([]string{"--db", src, "--version", "31", "--out", out})
	require.Error(t, err, "an existing --out is refused (no overwrite)")
}

func TestMeasureRefusesUnmigratedDatabase(t *testing.T) {
	// A database with no schema_migrations row has nothing to measure — build
	// one with a fresh (non-migrated) sqlite file.
	path := filepath.Join(t.TempDir(), "fresh.db")
	require.NoError(t, os.WriteFile(path, []byte("not a database"), 0o600))
	err := runMeasure([]string{"--db", path})
	require.Error(t, err)
}

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	buf := make([]byte, 1<<20)
	n, _ := r.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}
