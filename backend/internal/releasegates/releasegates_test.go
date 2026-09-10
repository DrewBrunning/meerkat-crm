package releasegates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodJSON = `{
  "gates": [
    {"name": "Backend (Go)", "workflow": "unit-tests.yml", "check_context": "Backend (Go)", "check_kind": "check_run", "tier": "per-pr", "mandatory": true, "release_gate": true, "criterion": "tests pass"},
    {"name": "build-and-push", "workflow": "docker-publish.yml", "check_context": null, "check_kind": "internal", "tier": "release-internal", "mandatory": true, "release_gate": false, "criterion": "images build + sign"},
    {"name": "OpenSSF Scorecard", "workflow": "scorecard.yml", "check_context": "Scorecard analysis", "check_kind": "check_run", "tier": "advisory", "mandatory": false, "release_gate": false, "criterion": "informational"}
  ]
}`

const goodDoc = `# Release gates

<!-- release-gates:begin -->

| Gate | Tier | Mandatory | Pass criterion | Workflow |
|---|---|---|---|---|
| ` + "`Backend (Go)`" + ` | per-pr | yes | tests pass | ` + "`unit-tests.yml`" + ` |
| ` + "`build-and-push`" + ` | release-internal | yes | images build + sign | ` + "`docker-publish.yml`" + ` |
| ` + "`OpenSSF Scorecard`" + ` | advisory | no | informational | ` + "`scorecard.yml`" + ` |

<!-- release-gates:end -->
`

func allWorkflowsExist(string) bool { return true }

func TestParseGood(t *testing.T) {
	reg, findings := Parse([]byte(goodJSON))
	assert.Empty(t, findings)
	require.Len(t, reg.Gates, 3)
	assert.Equal(t, []string{"Backend (Go)"}, reg.ReleaseGateContexts())
}

func TestParseRejectsStructuralErrors(t *testing.T) {
	bad := `{"gates": [
	  {"name": "", "workflow": "", "check_context": "x", "check_kind": "bogus", "tier": "nope", "mandatory": true, "release_gate": true, "criterion": ""},
	  {"name": "dup", "workflow": "a.yml", "check_context": null, "check_kind": "internal", "tier": "advisory", "mandatory": true, "release_gate": false, "criterion": "c"},
	  {"name": "dup", "workflow": "a.yml", "check_context": null, "check_kind": "internal", "tier": "release-internal", "mandatory": true, "release_gate": false, "criterion": "c"}
	]}`
	_, findings := Parse([]byte(bad))
	joined := "\n" + join(findings)
	assert.Contains(t, joined, "name is empty")
	assert.Contains(t, joined, "workflow is empty")
	assert.Contains(t, joined, `check_kind "bogus"`)
	assert.Contains(t, joined, `tier "nope"`)
	assert.Contains(t, joined, "criterion is empty")
	assert.Contains(t, joined, "duplicate name")
	assert.Contains(t, joined, "release_gate:true requires tier per-pr")
	assert.Contains(t, joined, "tier advisory but mandatory:true")
	assert.Contains(t, joined, `check_kind "internal" only makes sense`)
}

func TestParseRejectsBadJSON(t *testing.T) {
	_, findings := Parse([]byte("{not json"))
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0], "does not parse")
}

func TestParseRejectsEmpty(t *testing.T) {
	_, findings := Parse([]byte(`{"gates": []}`))
	assert.Contains(t, join(findings), "no gates")
}

func TestCheckWorkflows(t *testing.T) {
	reg, _ := Parse([]byte(goodJSON))
	missing := CheckWorkflows(reg, func(w string) bool { return w != "scorecard.yml" })
	require.Len(t, missing, 1)
	assert.Contains(t, missing[0], "scorecard.yml does not exist")

	assert.Empty(t, CheckWorkflows(reg, allWorkflowsExist))
}

func TestCrossCheckDocGood(t *testing.T) {
	reg, _ := Parse([]byte(goodJSON))
	assert.Empty(t, CrossCheckDoc(reg, goodDoc))
}

func TestCrossCheckDocMissingRow(t *testing.T) {
	reg, _ := Parse([]byte(goodJSON))
	doc := `<!-- release-gates:begin -->
| Gate | Tier | Mandatory | x | y |
|---|---|---|---|---|
| ` + "`Backend (Go)`" + ` | per-pr | yes | a | b |
<!-- release-gates:end -->`
	findings := CrossCheckDoc(reg, doc)
	joined := join(findings)
	assert.Contains(t, joined, `no table row for gate "build-and-push"`)
	assert.Contains(t, joined, `no table row for gate "OpenSSF Scorecard"`)
}

func TestCrossCheckDocMismatchAndExtra(t *testing.T) {
	reg, _ := Parse([]byte(goodJSON))
	doc := `<!-- release-gates:begin -->
| Gate | Tier | Mandatory | x | y |
|---|---|---|---|---|
| ` + "`Backend (Go)`" + ` | release-tier | no | a | b |
| ` + "`build-and-push`" + ` | release-internal | yes | a | b |
| ` + "`OpenSSF Scorecard`" + ` | advisory | no | a | b |
| ` + "`Ghost gate`" + ` | per-pr | yes | a | b |
<!-- release-gates:end -->`
	joined := join(CrossCheckDoc(reg, doc))
	assert.Contains(t, joined, `"Backend (Go)": table tier "release-tier" != registry tier "per-pr"`)
	assert.Contains(t, joined, `"Backend (Go)": table mandatory=false != registry mandatory=true`)
	assert.Contains(t, joined, `table row "Ghost gate" has no matching registry gate`)
}

func TestCrossCheckDocMissingMarkers(t *testing.T) {
	reg, _ := Parse([]byte(goodJSON))
	findings := CrossCheckDoc(reg, "# no markers here")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0], "table markers")
}

// TestCommittedRegistryIsValid runs the real .github/release-gates.json and
// docs/development/release-gates.md through every check — the same thing
// cmd/releasegatecheck does in CI, but exercised by `go test ./...` too.
func TestCommittedRegistryIsValid(t *testing.T) {
	root := repoRoot(t)
	regBytes, err := os.ReadFile(filepath.Join(root, ".github", "release-gates.json"))
	require.NoError(t, err)
	docBytes, err := os.ReadFile(filepath.Join(root, "docs", "development", "release-gates.md"))
	require.NoError(t, err)

	reg, findings := Parse(regBytes)
	findings = append(findings, CheckWorkflows(reg, func(w string) bool {
		_, statErr := os.Stat(filepath.Join(root, ".github", "workflows", w))
		return statErr == nil
	})...)
	findings = append(findings, CrossCheckDoc(reg, string(docBytes))...)
	assert.Empty(t, findings, "committed release-gate registry / doc is inconsistent")

	// The release-gate job polls these; keep the list intentional.
	assert.NotEmpty(t, reg.ReleaseGateContexts())
	for _, ctx := range reg.ReleaseGateContexts() {
		assert.NotEmpty(t, ctx)
	}
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
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", dir)
		}
		dir = parent
	}
}
