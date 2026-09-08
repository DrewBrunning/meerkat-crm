package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocsStateTheDeprecationPolicy is the MAINT-01 (issue #490) companion to
// schemafixture's TestDocsStateTheBreakingChangePolicy and
// cmd/depexceptions' TestDocsStateTheDependencyUpgradePolicy: the canonical
// deprecation policy must still state the load-bearing claims the issue's
// verify list names — the covered surfaces, the announcement/replacement/
// window rule, the two-part window, the per-surface runtime signal, the
// register + this command, the tie to MAINT-02, and the explicit "the
// promise starts at 1.0.0" statement — so the published policy cannot
// silently lose a guarantee a 1.0.0 gate item (#525) depends on.
//
// Phrases are matched against a whitespace-collapsed copy of the doc so a
// re-wrap of a paragraph does not fail the test; only a real wording change
// does.
func TestDocsStateTheDeprecationPolicy(t *testing.T) {
	root, err := findRepoRoot()
	require.NoError(t, err)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "deprecation-policy.md"))
	require.NoError(t, err)
	text := strings.Join(strings.Fields(string(doc)), " ")

	for _, want := range []string{
		// Covered surfaces.
		"`/api/v1` REST contract",
		"The database schema",
		"Configuration variable names and semantics",
		"CLI commands and flags",
		"Exported data formats",
		"Documented behavior",

		// The three-part rule and the two-part window.
		"an announcement, a replacement, and a window",
		"removal wearing a nicer word",
		"At least one minor release, and never less than 90 days",
		"whichever is longer",
		"keep working, unchanged, for the whole window",

		// Runtime discoverability, per surface.
		"**`Deprecation`**",
		"**`Sunset`**",
		`rel="deprecation"`,
		"**`deprecated: true`**",
		"**startup `WARN` log naming the variable and its replacement**",
		"prints a one-line notice to **stderr**",

		// The register and its enforcement.
		"[`deprecations.md`](deprecations.md) is the machine-readable list",
		"go run ./cmd/deprecations",
		"something removed before its window expired",
		"unit-tests.yml",

		// Ties to the neighbouring policies.
		"MAINT-02",
		"issue #491",
		"issue #492",

		// The data bar and the 0.x interaction — the two statements most easily lost.
		"The data bar is higher than the shape bar",
		"takes effect at `1.0.0`",
		"not read as a promise it does not yet make",

		// The retroactive audit and its one documented out-of-scope case.
		"Retroactive audit",
		"PreferenceCategoryMedia",
	} {
		assert.Contains(t, text, want, "the deprecation policy must still state %q", want)
	}
}
