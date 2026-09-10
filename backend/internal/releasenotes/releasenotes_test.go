package releasenotes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssembleNoContributions(t *testing.T) {
	got := Assemble([]PR{
		{Number: 1, URL: "https://x/pull/1", Body: "## Summary\n\nJust a refactor.\n"},
		{Number: 2, URL: "https://x/pull/2", Body: "no-changelog: internal test helper only\n"},
	})

	assert.Equal(t, "## Upgrade notes\n\n"+NoUpgradeActionRequired+"\n", got)
	assert.NotContains(t, got, "## Breaking changes")
}

func TestAssembleEmpty(t *testing.T) {
	assert.Equal(t, "## Upgrade notes\n\n"+NoUpgradeActionRequired+"\n", Assemble(nil))
}

func TestAssembleHarvestsUpgradeNotes(t *testing.T) {
	prs := []PR{
		{
			Number: 812, URL: "https://github.com/o/r/pull/812",
			Body: "## Summary\n\nAdds a column.\n\n## Upgrade notes\n\nRun `make migrate-up` if you migrate out of band; the server does it on boot otherwise.\n\n## Testing\n\nunit\n",
		},
		{
			Number: 830, URL: "https://github.com/o/r/pull/830",
			Body: "## Upgrade notes\nSet `NEW_VAR` before upgrading; it has no default.\n",
		},
	}

	got := Assemble(prs)

	assert.True(t, strings.HasPrefix(got, "## Upgrade notes\n\n"))
	assert.Contains(t, got, "**[#812](https://github.com/o/r/pull/812)** — Run `make migrate-up`")
	assert.Contains(t, got, "**[#830](https://github.com/o/r/pull/830)** — Set `NEW_VAR` before upgrading")
	// The "Summary"/"Testing" sections around the block must not leak in.
	assert.NotContains(t, got, "Adds a column")
	assert.NotContains(t, got, "unit")
	assert.NotContains(t, got, NoUpgradeActionRequired)
	assert.NotContains(t, got, "## Breaking changes")
}

func TestAssembleHarvestsBreakingChanges(t *testing.T) {
	prs := []PR{
		{
			Number: 900, URL: "https://github.com/o/r/pull/900",
			Body: "## Upgrade notes\n\nNone.\n\n## Breaking changes\n\n`GET /api/v1/foo` now returns 404 instead of 200 for a missing bar.\n",
		},
	}

	got := Assemble(prs)

	assert.Contains(t, got, "## Upgrade notes\n\n**[#900]")
	assert.Contains(t, got, "## Breaking changes\n\n**[#900](https://github.com/o/r/pull/900)** — `GET /api/v1/foo` now returns 404")
	// Upgrade notes section comes before Breaking changes.
	assert.Less(t, strings.Index(got, "## Upgrade notes"), strings.Index(got, "## Breaking changes"))
}

func TestAssembleFallsBackToNumberWithoutURL(t *testing.T) {
	got := Assemble([]PR{{Number: 5, Body: "### Upgrade notes\nDo the thing.\n"}})
	assert.Contains(t, got, "**#5** — Do the thing.")
}

func TestExtractSection(t *testing.T) {
	t.Run("case-insensitive heading and level 2-4", func(t *testing.T) {
		for _, h := range []string{"## Upgrade Notes", "### upgrade notes", "#### UPGRADE NOTES"} {
			body := h + "\nbody line\n"
			assert.Equalf(t, "body line", extractSection(body, "upgrade notes"), "heading %q", h)
		}
	})

	t.Run("stops at same-or-higher-level heading, keeps deeper ones", func(t *testing.T) {
		body := "## Upgrade notes\n\nstep one\n\n#### detail\n\nmore\n\n## Next\n\nunrelated\n"
		assert.Equal(t, "step one\n\n#### detail\n\nmore", extractSection(body, "upgrade notes"))
	})

	t.Run("absent heading returns empty", func(t *testing.T) {
		assert.Empty(t, extractSection("## Summary\n\nnothing here\n", "upgrade notes"))
	})

	t.Run("empty section returns empty", func(t *testing.T) {
		assert.Empty(t, extractSection("## Upgrade notes\n\n## Testing\n", "upgrade notes"))
	})
}

func TestExtractSectionFirstMatchWins(t *testing.T) {
	body := "## Upgrade notes\n\nfirst\n\n## Upgrade notes\n\nsecond\n"
	require.Equal(t, "first", extractSection(body, "upgrade notes"))
}
