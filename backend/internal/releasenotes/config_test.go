package releasenotes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReleaseConfigHasRequiredCategories pins the committed .github/release.yml
// against RequiredCategories: a section cannot be dropped, and the catch-all
// cannot lose its "*" label, without failing this build.
func TestReleaseConfigHasRequiredCategories(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "release.yml"))
	require.NoError(t, err)
	require.NoError(t, ValidateReleaseConfig(data))
}

func TestValidateReleaseConfigRejectsMissingCategory(t *testing.T) {
	cfg := []byte(`changelog:
  categories:
    - title: Added
      labels: [enhancement]
    - title: Other Changes
      labels: ["*"]
`)
	err := ValidateReleaseConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required categories")
	assert.Contains(t, err.Error(), "Security")
}

func TestValidateReleaseConfigRejectsCatchAllWithoutStar(t *testing.T) {
	var body string
	for _, c := range RequiredCategories {
		labels := `["misc"]`
		if c == catchAllCategory {
			labels = `[not-a-star]`
		}
		body += "    - title: " + c + "\n      labels: " + labels + "\n"
	}
	err := ValidateReleaseConfig([]byte("changelog:\n  categories:\n" + body))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"*" label`)
}

func TestValidateReleaseConfigRejectsMalformedYAML(t *testing.T) {
	err := ValidateReleaseConfig([]byte("changelog: [this is not: a map"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse release config")
}

// repoRoot walks up from the test's working directory to the repository root,
// identified by backend/go.mod — the sentinel cmd/docscheck uses.
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
			t.Fatalf("could not locate repository root (no backend/go.mod) from %s", dir)
		}
		dir = parent
	}
}
