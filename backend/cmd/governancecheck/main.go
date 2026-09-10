// Command governancecheck is the offline gate for the repository-governance
// settings (#508) and the #513 cosign-identity pin.
//
// It verifies:
//
//  1. Every .github/rulesets/*.json parses and has a name / target /
//     enforcement / rules.
//  2. main-protection.json's required status checks are EXACTLY the per-PR
//     mandatory gates from .github/release-gates.json that have a stable check
//     context -- the branch-protection required list is generated from the
//     #447 gate registry, not hand-maintained.
//  3. release-branches.json (the release/* ruleset, RC-02 / #446) requires the
//     same status checks as main-protection.json -- "RC gates match release
//     gates" enforced, not aspirational.
//  4. docs/development/repo-governance.md references each ruleset file and its
//     marked table lists the same required checks.
//  5. docs/security/release-verification.md pins every cosign verify identity
//     to a specific workflow (#513), never the repo-wide `.../<repo>/.*` form.
//
// Exit 0: consistent. Exit 1: at least one finding. Exit 2: could not run.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"mycorrhizal/internal/governance"
)

var rulesetFiles = []string{
	".github/rulesets/main-protection.json",
	".github/rulesets/main-hard-checks.json",
	".github/rulesets/tags-v.json",
	".github/rulesets/release-branches.json",
}

const (
	releaseGatesFile       = ".github/release-gates.json"
	governanceDocFile      = "docs/development/repo-governance.md"
	releaseVerificationDoc = "docs/security/release-verification.md"
)

func main() {
	os.Exit(run(os.Stdout)) // # pragma: no cover — os.Exit ends the process; tests drive run()
}

func run(w io.Writer) int {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "governancecheck:", err) // # pragma: no cover
		return 2                                         // # pragma: no cover
	}
	return runAt(w, root)
}

// runAt is the testable core: it reads the governance files under root, runs
// every check, and returns the process exit code.
func runAt(w io.Writer, root string) int {
	read := func(rel string) ([]byte, bool) {
		// #nosec G304 -- root is findRepoRoot's output (or a test temp dir), the leaf is a constant
		b, e := os.ReadFile(filepath.Join(root, rel))
		if e != nil {
			fmt.Fprintln(w, "cannot read "+rel+": "+e.Error())
			return nil, false
		}
		return b, true
	}

	var findings []string
	rulesets := map[string]governance.Ruleset{}
	ok := true
	for _, rf := range rulesetFiles {
		data, got := read(rf)
		if !got {
			ok = false
			continue
		}
		rs, f := governance.ParseRuleset(rf, data)
		findings = append(findings, f...)
		rulesets[rf] = rs
	}
	gatesJSON, g1 := read(releaseGatesFile)
	govDoc, g2 := read(governanceDocFile)
	relVerDoc, g3 := read(releaseVerificationDoc)
	if !ok || !g1 || !g2 || !g3 {
		return 2
	}

	mainProt := rulesets[".github/rulesets/main-protection.json"]
	relBranches := rulesets[".github/rulesets/release-branches.json"]
	findings = append(findings, governance.CheckMainProtectionMatchesGates(mainProt, gatesJSON)...)
	findings = append(findings, governance.CheckReleaseBranchesMatchMain(relBranches, mainProt)...)
	findings = append(findings, governance.CrossCheckGovernanceDoc(string(govDoc), rulesetFiles, mainProt)...)
	findings = append(findings, governance.CheckCosignIdentityPinned(string(relVerDoc))...)

	if len(findings) == 0 {
		fmt.Fprintf(w, "governance OK: %d rulesets, required checks match %s, cosign identities pinned\n",
			len(rulesetFiles), releaseGatesFile)
		return 0
	}
	sort.Strings(findings)
	for _, f := range findings {
		fmt.Fprintln(w, f)
	}
	fmt.Fprintf(w, "\n%d governance finding(s).\n", len(findings))
	return 1
}

// findRepoRoot walks up from the working directory to backend/go.mod, the
// sentinel cmd/docscheck / cmd/releasegatecheck use.
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
			return "", fmt.Errorf("could not locate repository root (no backend/go.mod)") // # pragma: no cover
		}
		dir = parent
	}
}
