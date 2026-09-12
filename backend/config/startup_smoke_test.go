package config_test

// Binary-level assertions for DEPLOY-01 (issue #450): a misconfigured clean
// install must fail *at startup* with a message that names the offending
// environment variable, not a generic crash three subsystems later.
//
// config_test.go already unit-tests Config.Validate() field by field. These
// tests are one layer out: they build the real `mycorrhizal` binary and run
// it with a broken environment, proving that ValidateOrPanic's output
// actually reaches a operator's console (stderr) and that the process exits
// non-zero before it touches the database or binds a port. The docker-compose
// startup path is covered a layer further out again, in
// .github/workflows/deploy-smoke.yml.

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
	buildDir    string
)

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// backendBinary builds ../ (package main, the mycorrhizal server) once per
// test run and returns the path to the executable. The build cache is warm on
// any machine that has already run `go build ./...`, so this is a few seconds
// cold and near-instant warm.
func backendBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "deploysmoke-bin-")
		if buildErr != nil {
			return
		}
		builtBinary = filepath.Join(buildDir, "mycorrhizal")
		// -o an explicit path; build the parent module main package.
		cmd := exec.Command("go", "build", "-o", builtBinary, "..")
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = &buildFailure{out: string(out), err: err}
		}
	})
	if buildErr != nil {
		t.Fatalf("build mycorrhizal binary: %v", buildErr)
	}
	return builtBinary
}

type buildFailure struct {
	out string
	err error
}

func (b *buildFailure) Error() string { return b.err.Error() + "\n" + b.out }

// runWithEnv starts the binary with exactly env (no inherited variables
// except PATH/HOME/GOCACHE, which the Go runtime and loader need), waits up to
// 20s, kills it if it is still running, and returns the combined output plus
// whether it exited non-zero.
func runWithEnv(t *testing.T, env map[string]string) (output string, failed bool) {
	t.Helper()
	bin := backendBinary(t)

	full := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	for k, v := range env {
		full = append(full, k+"="+v)
	}

	cmd := exec.Command(bin)
	cmd.Env = full
	out, err := combinedOutputWithTimeout(cmd, 20*time.Second)
	return string(out), err != nil
}

func combinedOutputWithTimeout(cmd *exec.Cmd, d time.Duration) ([]byte, error) {
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return []byte(buf.String()), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return []byte(buf.String()), err
	case <-time.After(d):
		_ = cmd.Process.Kill()
		<-done
		// Reaching the timeout means the server booted and started serving —
		// i.e. the config was accepted. The caller asserts on `failed`, so a
		// timeout correctly reads as "did not fail at startup".
		return []byte(buf.String()), nil
	}
}

func strongJWTSecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// validEnv is an environment the binary accepts far enough to get past
// ValidateOrPanic; individual tests break exactly one key.
func validEnv(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	return map[string]string{
		"JWT_SECRET_KEY":    strongJWTSecret(t),
		"SQLITE_DB_PATH":    filepath.Join(dir, "x.db"),
		"PROFILE_PHOTO_DIR": filepath.Join(dir, "photos"),
		"ATTACHMENTS_DIR":   filepath.Join(dir, "attachments"),
		"FRONTEND_URL":      "http://localhost:7300",
		"GIN_MODE":          "test",
	}
}

func TestStartup_MissingJWTSecretKey_FailsNamingTheVariable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the server binary; skipped under -short")
	}
	env := validEnv(t)
	delete(env, "JWT_SECRET_KEY")

	out, failed := runWithEnv(t, env)
	if !failed {
		t.Fatalf("server did not exit non-zero with JWT_SECRET_KEY unset\noutput:\n%s", out)
	}
	if !strings.Contains(out, "JWT_SECRET_KEY") {
		t.Fatalf("startup failure did not name JWT_SECRET_KEY\noutput:\n%s", out)
	}
}

func TestStartup_RelativeAttachmentsDir_FailsNamingTheVariable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the server binary; skipped under -short")
	}
	env := validEnv(t)
	env["ATTACHMENTS_DIR"] = "relative/attachments"

	out, failed := runWithEnv(t, env)
	if !failed {
		t.Fatalf("server did not exit non-zero with a relative ATTACHMENTS_DIR\noutput:\n%s", out)
	}
	if !strings.Contains(out, "ATTACHMENTS_DIR") || !strings.Contains(strings.ToLower(out), "absolute") {
		t.Fatalf("startup failure did not name ATTACHMENTS_DIR and say it must be absolute\noutput:\n%s", out)
	}
}

func TestStartup_EmptyFrontendURL_FailsNamingTheVariable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the server binary; skipped under -short")
	}
	env := validEnv(t)
	env["FRONTEND_URL"] = ""

	out, failed := runWithEnv(t, env)
	if !failed {
		t.Fatalf("server did not exit non-zero with FRONTEND_URL empty\noutput:\n%s", out)
	}
	if !strings.Contains(out, "FRONTEND_URL") {
		t.Fatalf("startup failure did not name FRONTEND_URL\noutput:\n%s", out)
	}
}

// Issue #971: a negative DELETED_RETENTION_DAYS puts the purge cutoff in the
// future, so the job would hard-delete the entire soft-delete undo window on
// boot. It must fail at startup naming the variable rather than arm that job.
func TestStartup_NegativeDeletedRetentionDays_FailsNamingTheVariable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the server binary; skipped under -short")
	}
	env := validEnv(t)
	env["DELETED_RETENTION_DAYS"] = "-1"

	out, failed := runWithEnv(t, env)
	if !failed {
		t.Fatalf("server did not exit non-zero with DELETED_RETENTION_DAYS=-1\noutput:\n%s", out)
	}
	if !strings.Contains(out, "DELETED_RETENTION_DAYS") {
		t.Fatalf("startup failure did not name DELETED_RETENTION_DAYS\noutput:\n%s", out)
	}
}
