// Command releasegatecheck is the structural gate for the release-gate registry
// (REL-03, issue #447).
//
// docs/development/release-gates.md is the canonical list of what must pass
// before a tag publishes; .github/release-gates.json is its machine-readable
// twin, read by the `release-gate` job in docker-publish.yml. An unenforced
// list is a wish, so this command re-verifies on every run:
//
//  1. The JSON parses and every gate obeys the registry's structural rules
//     (known tier / check_kind, unique name, non-empty criterion, and the
//     release_gate:true invariants).
//  2. Every gate names a workflow file that actually exists under
//     .github/workflows/.
//  3. The human-readable table in docs/development/release-gates.md has exactly
//     one row per registry gate, with a matching tier and mandatory flag — so a
//     gate cannot be added to one without the other, and a listed gate can
//     never lack a real job.
//
// Exit 0: everything lines up. Exit 1: at least one finding. Exit 2: the check
// itself could not run.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"mycorrhizal/internal/releasegates"
)

const (
	registryFile = ".github/release-gates.json"
	docFile      = "docs/development/release-gates.md"
	workflowsDir = ".github/workflows"
)

func main() {
	os.Exit(run(os.Stdout)) // # pragma: no cover — os.Exit ends the process; tests drive run()
}

func run(w io.Writer) int {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasegatecheck:", err) // # pragma: no cover
		return 2                                          // # pragma: no cover
	}

	regBytes, err := os.ReadFile(filepath.Join(root, registryFile))
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasegatecheck: read", registryFile, err)
		return 2
	}
	docBytes, err := os.ReadFile(filepath.Join(root, docFile))
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasegatecheck: read", docFile, err)
		return 2
	}

	reg, findings := releasegates.Parse(regBytes)
	findings = append(findings, releasegates.CheckWorkflows(reg, func(name string) bool {
		_, statErr := os.Stat(filepath.Join(root, workflowsDir, name))
		return statErr == nil
	})...)
	findings = append(findings, releasegates.CrossCheckDoc(reg, string(docBytes))...)

	if len(findings) == 0 {
		fmt.Fprintf(w, "release gates OK: %d gates, all workflows exist, doc table matches (%s)\n",
			len(reg.Gates), registryFile)
		return 0
	}
	sort.Strings(findings)
	for _, f := range findings {
		fmt.Fprintln(w, f)
	}
	fmt.Fprintf(w, "\n%d release-gate finding(s). Fix %s and/or %s.\n", len(findings), registryFile, docFile)
	return 1
}

// findRepoRoot walks up from the working directory until it finds backend/go.mod
// — the sentinel cmd/docscheck and cmd/deprecations use.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err // # pragma: no cover
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "backend", "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate repository root (no backend/go.mod) from working directory")
		}
		dir = parent
	}
}
