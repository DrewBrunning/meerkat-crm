package schemafixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocsStateTheIssueClassification is the MAINT-03 (issue #492) member of
// the same "a governance doc cannot silently lose a load-bearing claim"
// family as TestDocsStateTheFloor and TestDocsStateTheBreakingChangePolicy in
// this package. MAINT-03 is not schema-related; it lives here only because
// this package is already the repo's home for that pattern and its
// findRepoRoot helper.
//
// It pins the parts of docs/issue-classification.md that other work depends
// on being stated: the resolved priority/milestone decoupling, the
// no-milestone convention, the gate-issue convention, the bug-severity ladder
// (which incident-response routing keys off), the readiness bar and its
// reference example, and what "closed" means.
//
// Phrases match a whitespace-collapsed copy so a paragraph re-wrap does not
// fail the test; only a wording change does.
func TestDocsStateTheIssueClassification(t *testing.T) {
	dir, err := findRepoRoot()
	require.NoError(t, err)
	doc, err := os.ReadFile(filepath.Join(dir, "docs", "issue-classification.md"))
	require.NoError(t, err)
	text := strings.Join(strings.Fields(string(doc)), " ")

	for _, want := range []string{
		// Priority: the commitment/scheduling-horizon framing, the four names,
		// the #544 reconciliation, and that it is not a relabel pass.
		"`p0`–`p3` = commitment and scheduling horizon",
		"Priority expresses how firmly the project intends to do something",
		"Should be prioritized",
		"Should be scheduled",
		"Should be implemented at some point",
		"Potential improvement",
		"refines the #544\nnote",
		"Not a bulk-relabel",

		// Milestone + gate conventions.
		"No milestone = not scheduled",
		"Every milestone has a gate issue",
		"closed **last**",
		"checked only with a\ncitation",

		// Bug severity — the ladder incident-response routing depends on.
		"Severity is a separate axis from priority",
		"`sev1`", "`sev2`", "`sev3`", "`sev4`",
		"incident-response.md",
		"5 business days",

		// Readiness bar and its reference example.
		"Context**", "Recommended actions**", "How to verify**",
		"`file:line` citations",
		"#368",

		// Lifecycle.
		`**"Closed" means the ticket landed**`,
		"issue body plus the commit history *is* the status\nrecord",

		// Self-reference.
		"issue #492",
	} {
		norm := strings.Join(strings.Fields(want), " ")
		assert.Contains(t, text, norm, "issue-classification.md must still state %q", norm)
	}
}
