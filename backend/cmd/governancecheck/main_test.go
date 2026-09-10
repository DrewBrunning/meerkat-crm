package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAgainstCommittedFiles(t *testing.T) {
	var out bytes.Buffer
	require.Equalf(t, 0, run(&out), "governancecheck failed:\n%s", out.String())
	assert.Contains(t, out.String(), "governance OK")
}

func TestFindRepoRoot(t *testing.T) {
	root, err := findRepoRoot()
	require.NoError(t, err)
	assert.NotEmpty(t, root)
}

// TestRunAtReportsFindings drives runAt against a fixture tree whose
// main-protection.json omits a required check, exercising the finding-print
// path and the non-zero exit.
func TestRunAtReportsFindings(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write(".github/rulesets/main-protection.json", `{"name":"main-protection","target":"branch","enforcement":"active","rules":[
	  {"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"Backend (Go)"}]}}]}`)
	write(".github/rulesets/main-hard-checks.json", `{"name":"h","target":"branch","enforcement":"active","rules":[{"type":"deletion"}]}`)
	write(".github/rulesets/tags-v.json", `{"name":"v","target":"tag","enforcement":"active","rules":[{"type":"deletion"}]}`)
	// Same required set as main-protection so CheckReleaseBranchesMatchMain adds
	// no finding here -- this fixture exercises the main-protection<->gates path.
	write(".github/rulesets/release-branches.json", `{"name":"release-branch-protection","target":"branch","enforcement":"active","rules":[
	  {"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"Backend (Go)"}]}}]}`)
	write(".github/release-gates.json", `{"gates":[
	  {"name":"Backend (Go)","workflow":"unit-tests.yml","check_context":"Backend (Go)","check_kind":"check_run","tier":"per-pr","mandatory":true,"release_gate":true,"criterion":"x"},
	  {"name":"E2E","workflow":"e2e-tests.yml","check_context":"Run E2E Tests","check_kind":"check_run","tier":"per-pr","mandatory":true,"release_gate":true,"criterion":"x"}]}`)
	write("docs/development/repo-governance.md", "no markers, no file refs")
	write("docs/security/release-verification.md", "--certificate-identity-regexp 'https://github.com/o/r/.*'")

	var out bytes.Buffer
	code := runAt(&out, root)
	require.Equal(t, 1, code)
	s := out.String()
	assert.Contains(t, s, `missing required check "Run E2E Tests"`)
	assert.Contains(t, s, "does not reference .github/rulesets/main-protection.json")
	assert.Contains(t, s, "not pinned to a workflow")
	assert.Contains(t, s, "governance finding(s).")
}

func TestRunAtMissingFileExits2(t *testing.T) {
	var out bytes.Buffer
	assert.Equal(t, 2, runAt(&out, t.TempDir()))
	assert.Contains(t, out.String(), "cannot read")
}

func TestRunSuccessShape(t *testing.T) {
	var out bytes.Buffer
	require.Equal(t, 0, run(&out))
	assert.True(t, strings.HasPrefix(strings.TrimSpace(out.String()), "governance OK:"))
}
