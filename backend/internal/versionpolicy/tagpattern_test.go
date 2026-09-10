package versionpolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidReleaseTag(t *testing.T) {
	accept := []string{
		"v0.0.0",
		"v0.6.12",
		"v1.0.0",
		"v1.0.0-rc.1",
		"v1.0.0-rc.2",
		"v10.20.30",
		"v0.9.0-rc.10",
	}
	reject := []string{
		"",
		"1.0.0",                  // no leading v
		"v1.0",                   // two components
		"v1",                     // one component
		"v1.0.0.0",               // four components
		"v1.0.0-alpha",           // non-rc pre-release
		"v1.0.0-rc",              // rc without a number
		"v1.0.0-rc.",             // rc with an empty number
		"v1.0.0-rc.1.2",          // rc number is not a dotted list
		"v0.2.0-alpha-candidate", // the pre-policy tag
		"v1.0.0 ",                // trailing space
		" v1.0.0",                // leading space
		"V1.0.0",                 // capital V
		"v1.0.0+build",           // build metadata not permitted
	}

	for _, tag := range accept {
		assert.Truef(t, ValidReleaseTag(tag), "%q should be a valid release tag", tag)
	}
	for _, tag := range reject {
		assert.Falsef(t, ValidReleaseTag(tag), "%q should not be a valid release tag", tag)
	}
}

func TestIsReleaseCandidate(t *testing.T) {
	assert.True(t, IsReleaseCandidate("v1.0.0-rc.1"))
	assert.True(t, IsReleaseCandidate("v0.9.0-rc.42"))
	assert.False(t, IsReleaseCandidate("v1.0.0"), "a final release is not an RC")
	assert.False(t, IsReleaseCandidate("v1.0.0-alpha"), "a malformed tag is not an RC")
	assert.False(t, IsReleaseCandidate("not-a-tag"))
}

// TestWorkflowsAndDocUseCanonicalPattern is the drift gate: the exact
// TagPattern string must appear in both release workflows and in the policy
// doc, so the shell `grep -qE` copies cannot diverge from the Go definition.
func TestWorkflowsAndDocUseCanonicalPattern(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join(".github", "workflows", "release.yml"),
		filepath.Join(".github", "workflows", "docker-publish.yml"),
		filepath.Join("docs", "versioning-policy.md"),
	} {
		body, err := os.ReadFile(filepath.Join(root, rel))
		require.NoErrorf(t, err, "read %s", rel)
		assert.Containsf(t, string(body), TagPattern,
			"%s must embed the canonical TagPattern verbatim (see backend/internal/versionpolicy/tagpattern.go)", rel)
	}
}

// TestPublicationGateUsesCanonicalPattern proves the docker-publish.yml
// publication gate actually shells out with the canonical pattern — that the
// TagPattern in the file is the argument to a `grep -qE`, not just some string
// that happens to appear. Combined with TestValidReleaseTag (which fixes the
// pattern's behaviour) this pins what the gate does.
func TestPublicationGateUsesCanonicalPattern(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "docker-publish.yml"))
	require.NoError(t, err)

	// A gate line looks like:  if ! printf '%s' "$RELEASE_TAG" | grep -qE '<ERE>'; then
	// There may be more than one `grep -qE '...'` in the file; require that one
	// of them is exactly the canonical TagPattern.
	grepLine := regexp.MustCompile(`grep -qE (?:-- )?'([^']+)'`)
	var gatePatterns []string
	for _, m := range grepLine.FindAllStringSubmatch(string(body), -1) {
		gatePatterns = append(gatePatterns, m[1])
	}
	assert.Contains(t, gatePatterns, TagPattern,
		"docker-publish.yml must gate the tag with `grep -qE '<TagPattern>'`")
}

// TestKnownVersionPlaceholdersUnchanged pins the two hand-written version
// literals the policy classifies as non-authoritative placeholders. If someone
// turns one into a real version number this fails, which is the prompt to
// update docs/versioning-policy.md rather than silently grow a second source of
// version truth.
func TestKnownVersionPlaceholdersUnchanged(t *testing.T) {
	root := repoRoot(t)

	pkg, err := os.ReadFile(filepath.Join(root, "frontend", "package.json"))
	require.NoError(t, err)
	assert.Contains(t, string(pkg), `"version": "0.1.0"`,
		"frontend/package.json version is an inert placeholder (real value: VITE_APP_VERSION at build); "+
			"if this changed, update docs/versioning-policy.md")

	plugin, err := os.ReadFile(filepath.Join(root,
		"android", "build-logic", "src", "main", "kotlin", "com", "mycorrhizal", "crm",
		"buildlogic", "MycorrhizalAndroidApplicationPlugin.kt"))
	require.NoError(t, err)
	assert.Contains(t, string(plugin), `versionCode = versionCodeOverride ?: 1`,
		"the Android versionCode fallback is a placeholder overridden by CI; if this changed, update docs/versioning-policy.md")
	assert.Contains(t, string(plugin), `versionName = versionNameOverride ?: "0.1.0"`,
		"the Android versionName fallback is a placeholder overridden by CI; if this changed, update docs/versioning-policy.md")
}

// repoRoot walks up from the test's working directory to the repository root,
// identified by backend/go.mod — the same sentinel cmd/docscheck uses.
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

// guard against an accidental edit that leaves TagPattern unanchored.
func TestTagPatternIsAnchored(t *testing.T) {
	assert.True(t, strings.HasPrefix(TagPattern, "^") && strings.HasSuffix(TagPattern, "$"),
		"TagPattern must stay fully anchored")
}
