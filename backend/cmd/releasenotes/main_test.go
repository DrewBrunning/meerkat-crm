package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAssemblesFromStdin(t *testing.T) {
	in := strings.NewReader(`[
	  {"number": 812, "title": "add column", "url": "https://x/pull/812",
	   "body": "## Summary\nx\n\n## Upgrade notes\nRebuild the search index after upgrading.\n"},
	  {"number": 813, "title": "refactor", "url": "https://x/pull/813", "body": "no-changelog: internal only\n"}
	]`)
	var out bytes.Buffer

	require.Equal(t, 0, run(in, &out))
	got := out.String()
	assert.Contains(t, got, "## Upgrade notes")
	assert.Contains(t, got, "**[#812](https://x/pull/812)** — Rebuild the search index after upgrading.")
	assert.NotContains(t, got, "#813")
}

func TestRunEmptyArrayStillEmitsUpgradeNotes(t *testing.T) {
	var out bytes.Buffer
	require.Equal(t, 0, run(strings.NewReader(`[]`), &out))
	assert.Contains(t, out.String(), "## Upgrade notes")
	assert.Contains(t, out.String(), "no action required")
}

func TestRunRejectsNonJSON(t *testing.T) {
	var out bytes.Buffer
	require.Equal(t, 2, run(strings.NewReader("not json"), &out))
	assert.Empty(t, out.String())
}
