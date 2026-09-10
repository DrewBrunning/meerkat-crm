package governance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mainProtJSON = `{
  "name": "main-protection", "target": "branch", "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/heads/main"], "exclude": []}},
  "rules": [
    {"type": "required_status_checks", "parameters": {"required_status_checks": [
      {"context": "Backend (Go)"}, {"context": "codecov/patch/backend"}
    ]}},
    {"type": "required_linear_history"}
  ]
}`

const gatesJSON = `{"gates": [
  {"name": "Backend (Go)", "workflow": "unit-tests.yml", "check_context": "Backend (Go)", "check_kind": "check_run", "tier": "per-pr", "mandatory": true, "release_gate": true, "criterion": "x"},
  {"name": "codecov/patch/backend", "workflow": "unit-tests.yml", "check_context": "codecov/patch/backend", "check_kind": "commit_status", "tier": "per-pr", "mandatory": true, "release_gate": false, "criterion": "x"},
  {"name": "Migration Tests", "workflow": "migration-tests.yml", "check_context": null, "check_kind": "check_run", "tier": "per-pr", "mandatory": true, "release_gate": false, "criterion": "x"},
  {"name": "release-gate", "workflow": "docker-publish.yml", "check_context": null, "check_kind": "internal", "tier": "release-internal", "mandatory": true, "release_gate": false, "criterion": "x"},
  {"name": "Scorecard", "workflow": "scorecard.yml", "check_context": "Scorecard analysis", "check_kind": "check_run", "tier": "advisory", "mandatory": false, "release_gate": false, "criterion": "x"}
]}`

func TestParseRuleset(t *testing.T) {
	rs, f := ParseRuleset("main-protection.json", []byte(mainProtJSON))
	assert.Empty(t, f)
	assert.Equal(t, "main-protection", rs.Name)

	_, f = ParseRuleset("bad.json", []byte(`{"name":"","target":"nope","enforcement":"maybe","rules":[]}`))
	joined := join(f)
	assert.Contains(t, joined, "name is empty")
	assert.Contains(t, joined, `target "nope"`)
	assert.Contains(t, joined, `enforcement "maybe"`)
	assert.Contains(t, joined, "has no rules")

	_, f = ParseRuleset("x.json", []byte("{not json"))
	require.Len(t, f, 1)
	assert.Contains(t, f[0], "does not parse")
}

func TestRequiredContexts(t *testing.T) {
	rs, _ := ParseRuleset("m", []byte(mainProtJSON))
	assert.ElementsMatch(t, []string{"Backend (Go)", "codecov/patch/backend"}, RequiredContexts(rs))

	rs2, _ := ParseRuleset("h", []byte(`{"name":"h","target":"branch","enforcement":"active","rules":[{"type":"deletion"}]}`))
	assert.Nil(t, RequiredContexts(rs2))
}

func TestCheckMainProtectionMatchesGates(t *testing.T) {
	rs, _ := ParseRuleset("m", []byte(mainProtJSON))
	// mainProtJSON requires exactly {Backend (Go), codecov/patch/backend};
	// gatesJSON's per-pr mandatory with context set is the same (Migration
	// Tests has null context, Scorecard is advisory) -> no findings.
	assert.Empty(t, CheckMainProtectionMatchesGates(rs, []byte(gatesJSON)))

	// Drop a required context -> flagged as missing.
	missing, _ := ParseRuleset("m", []byte(`{"name":"m","target":"branch","enforcement":"active","rules":[
	  {"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"Backend (Go)"}]}}]}`))
	f := CheckMainProtectionMatchesGates(missing, []byte(gatesJSON))
	require.Len(t, f, 1)
	assert.Contains(t, f[0], `missing required check "codecov/patch/backend"`)

	// Extra required context not in the registry -> flagged.
	extra, _ := ParseRuleset("m", []byte(`{"name":"m","target":"branch","enforcement":"active","rules":[
	  {"type":"required_status_checks","parameters":{"required_status_checks":[
	    {"context":"Backend (Go)"},{"context":"codecov/patch/backend"},{"context":"Ghost"}]}}]}`))
	f = CheckMainProtectionMatchesGates(extra, []byte(gatesJSON))
	require.Len(t, f, 1)
	assert.Contains(t, f[0], `requires "Ghost"`)
}

func TestCheckCosignIdentityPinned(t *testing.T) {
	good := "cosign verify --certificate-identity-regexp '^https://github\\.com/o/r/\\.github/workflows/docker-publish\\.yml@refs/tags/v'\n" +
		"cosign verify-blob --certificate-identity-regexp \"https://github.com/o/r/.github/workflows/syft-sbom.yml@refs/heads/main\"\n"
	assert.Empty(t, CheckCosignIdentityPinned(good))

	bad := "--certificate-identity-regexp 'https://github.com/o/r/.*'\n"
	f := CheckCosignIdentityPinned(bad)
	require.Len(t, f, 1)
	assert.Contains(t, f[0], "not pinned to a workflow")

	assert.Contains(t, join(CheckCosignIdentityPinned("no cosign here")), "no --certificate-identity-regexp")
}

func TestCrossCheckGovernanceDoc(t *testing.T) {
	rs, _ := ParseRuleset("m", []byte(mainProtJSON))
	files := []string{".github/rulesets/main-protection.json", ".github/rulesets/tags-v.json"}
	doc := "refs .github/rulesets/main-protection.json and .github/rulesets/tags-v.json\n" +
		docBeginMarker + "\n| Check | x |\n|---|---|\n| `Backend (Go)` | a |\n| `codecov/patch/backend` | a |\n" + docEndMarker + "\n"
	assert.Empty(t, CrossCheckGovernanceDoc(doc, files, rs))

	// missing file reference + missing table row + extra row
	doc2 := "refs .github/rulesets/main-protection.json only\n" +
		docBeginMarker + "\n| `Backend (Go)` | a |\n| `Bogus` | a |\n" + docEndMarker
	f := join(CrossCheckGovernanceDoc(doc2, files, rs))
	assert.Contains(t, f, "does not reference .github/rulesets/tags-v.json")
	assert.Contains(t, f, `missing "codecov/patch/backend"`)
	assert.Contains(t, f, `lists "Bogus"`)

	assert.Contains(t, join(CrossCheckGovernanceDoc("no markers", files, rs)), "markers")
}

// TestCommittedGovernanceIsConsistent runs the real files, like cmd/governancecheck.
func TestCommittedGovernanceIsConsistent(t *testing.T) {
	root := repoRoot(t)
	read := func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		return b
	}
	files := []string{
		".github/rulesets/main-protection.json",
		".github/rulesets/main-hard-checks.json",
		".github/rulesets/tags-v.json",
	}
	var findings []string
	var mainProt Ruleset
	for _, f := range files {
		rs, fs := ParseRuleset(f, read(f))
		findings = append(findings, fs...)
		if f == ".github/rulesets/main-protection.json" {
			mainProt = rs
		}
	}
	findings = append(findings, CheckMainProtectionMatchesGates(mainProt, read(".github/release-gates.json"))...)
	findings = append(findings, CrossCheckGovernanceDoc(string(read("docs/development/repo-governance.md")), files, mainProt)...)
	findings = append(findings, CheckCosignIdentityPinned(string(read("docs/security/release-verification.md")))...)
	assert.Empty(t, findings)
}

func join(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s + "\n"
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
			return dir
		}
		p := filepath.Dir(dir)
		if p == dir {
			t.Fatal("repo root not found")
		}
		dir = p
	}
}
