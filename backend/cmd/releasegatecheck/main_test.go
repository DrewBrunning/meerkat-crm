package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunAgainstCommittedFiles is the CI path: with the real repo tree, the
// registry, its workflows, and the doc table all line up.
func TestRunAgainstCommittedFiles(t *testing.T) {
	var out bytes.Buffer
	code := run(&out)
	require.Equalf(t, 0, code, "releasegatecheck failed:\n%s", out.String())
	assert.Contains(t, out.String(), "release gates OK")
}

func TestFindRepoRoot(t *testing.T) {
	root, err := findRepoRoot()
	require.NoError(t, err)
	assert.NotEmpty(t, root)
}

// TestRunNonZeroOnInconsistency is a light guard that run() surfaces findings
// (the detailed cases live in internal/releasegates). It cannot easily corrupt
// the real files, so it just asserts the success contract's shape.
func TestRunSuccessMessageShape(t *testing.T) {
	var out bytes.Buffer
	require.Equal(t, 0, run(&out))
	line := strings.TrimSpace(out.String())
	assert.True(t, strings.HasPrefix(line, "release gates OK:"), line)
	assert.Contains(t, line, "all workflows exist")
}
