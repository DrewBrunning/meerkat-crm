// Package governance is the code side of #508 (repository governance settings)
// and the #513 cosign-identity check.
//
// .github/rulesets/*.json holds the desired state of the branch and tag
// rulesets; docs/development/repo-governance.md is the prose. This package
// validates the JSON, asserts the main-protection required-check list is
// derived from .github/release-gates.json (#447) rather than hand-drifted, and
// asserts docs/security/release-verification.md pins every cosign verify
// identity to a specific workflow. cmd/governancecheck wires it to the
// filesystem and runs in CI; .github/workflows/governance-drift.yml diffs the
// committed JSON against the live rulesets.
package governance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"mycorrhizal/internal/releasegates"
)

// Ruleset is the subset of a GitHub repository ruleset this package reasons
// about.
type Ruleset struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Enforcement string `json:"enforcement"`
	Rules       []struct {
		Type       string          `json:"type"`
		Parameters json.RawMessage `json:"parameters"`
	} `json:"rules"`
}

var (
	validTargets      = map[string]bool{"branch": true, "tag": true, "push": true}
	validEnforcements = map[string]bool{"active": true, "evaluate": true, "disabled": true}
)

// ParseRuleset unmarshals and structurally validates one .github/rulesets/*.json.
func ParseRuleset(file string, data []byte) (Ruleset, []string) {
	var rs Ruleset
	if err := json.Unmarshal(data, &rs); err != nil {
		return rs, []string{fmt.Sprintf("%s does not parse: %v", file, err)}
	}
	var findings []string
	if strings.TrimSpace(rs.Name) == "" {
		findings = append(findings, file+": name is empty")
	}
	if !validTargets[rs.Target] {
		findings = append(findings, fmt.Sprintf("%s: target %q is not branch/tag/push", file, rs.Target))
	}
	if !validEnforcements[rs.Enforcement] {
		findings = append(findings, fmt.Sprintf("%s: enforcement %q is not active/evaluate/disabled", file, rs.Enforcement))
	}
	if len(rs.Rules) == 0 {
		findings = append(findings, file+": has no rules")
	}
	return rs, findings
}

// RequiredContexts returns the required status-check contexts of a ruleset, or
// nil if it has no required_status_checks rule.
func RequiredContexts(rs Ruleset) []string {
	for _, r := range rs.Rules {
		if r.Type != "required_status_checks" {
			continue
		}
		var p struct {
			Checks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		}
		if json.Unmarshal(r.Parameters, &p) != nil {
			return nil
		}
		out := make([]string, 0, len(p.Checks))
		for _, c := range p.Checks {
			out = append(out, c.Context)
		}
		return out
	}
	return nil
}

// CheckMainProtectionMatchesGates asserts the main-protection ruleset requires
// exactly the per-PR mandatory gates from .github/release-gates.json that have a
// stable check context. This is the #447 <-> #508 coupling: the branch-required
// list is generated from the gate registry, never edited by hand.
func CheckMainProtectionMatchesGates(mainProtection Ruleset, releaseGatesJSON []byte) []string {
	reg, findings := releasegates.Parse(releaseGatesJSON)
	if len(findings) > 0 {
		return []string{"cannot check coupling: .github/release-gates.json has findings (run releasegatecheck)"}
	}

	want := map[string]bool{}
	for _, g := range reg.Gates {
		if g.Tier == "per-pr" && g.Mandatory && g.CheckContext != "" {
			want[g.CheckContext] = true
		}
	}
	got := map[string]bool{}
	for _, c := range RequiredContexts(mainProtection) {
		got[c] = true
	}

	var out []string
	for c := range want {
		if !got[c] {
			out = append(out, fmt.Sprintf("main-protection.json is missing required check %q (a per-PR mandatory gate in release-gates.json)", c))
		}
	}
	for c := range got {
		if !want[c] {
			out = append(out, fmt.Sprintf("main-protection.json requires %q, which is not a per-PR mandatory gate in release-gates.json", c))
		}
	}
	sort.Strings(out)
	return out
}

// certIdentityRE pulls the value out of a cosign --certificate-identity-regexp
// flag in either quoting style.
var certIdentityRE = regexp.MustCompile(`--certificate-identity-regexp[= ]+['"]([^'"]+)['"]`)

// CheckCosignIdentityPinned asserts every cosign verify identity in
// release-verification.md is pinned to a specific workflow file and ref (#513),
// not the repo-wide `.../<repo>/.*` form a compromised low-privilege workflow
// could also satisfy.
func CheckCosignIdentityPinned(doc string) []string {
	matches := certIdentityRE.FindAllStringSubmatch(doc, -1)
	if len(matches) == 0 {
		return []string{"release-verification.md has no --certificate-identity-regexp -- expected the cosign verify commands"}
	}
	var out []string
	for _, m := range matches {
		id := m[1]
		// The value is an ERE, so "." is usually written "\." -- drop the
		// backslashes before checking for the workflow-pinned shape.
		bare := strings.ReplaceAll(id, `\`, "")
		if !strings.Contains(bare, "/.github/workflows/") || !strings.Contains(bare, ".yml@refs/") {
			out = append(out, fmt.Sprintf("cosign identity %q is not pinned to a workflow -- expected a `.../.github/workflows/<file>.yml@refs/...` regexp (#513)", id))
		}
	}
	return out
}

const (
	docBeginMarker = "<!-- governance-required-checks:begin -->"
	docEndMarker   = "<!-- governance-required-checks:end -->"
)

var docCheckRowRE = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|")

// CrossCheckGovernanceDoc asserts repo-governance.md references each committed
// ruleset file and that its marked required-check table lists exactly the
// contexts main-protection.json requires.
func CrossCheckGovernanceDoc(doc string, rulesetFiles []string, mainProtection Ruleset) []string {
	var findings []string
	for _, f := range rulesetFiles {
		if !strings.Contains(doc, f) {
			findings = append(findings, fmt.Sprintf("repo-governance.md does not reference %s", f))
		}
	}

	start := strings.Index(doc, docBeginMarker)
	end := strings.Index(doc, docEndMarker)
	if start == -1 || end == -1 || end < start {
		return append(findings, fmt.Sprintf("repo-governance.md is missing the %s / %s markers", docBeginMarker, docEndMarker))
	}
	block := doc[start+len(docBeginMarker) : end]
	inDoc := map[string]bool{}
	for _, line := range strings.Split(block, "\n") {
		m := docCheckRowRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if m[1] == "Check" || strings.HasPrefix(m[1], "---") {
			continue
		}
		inDoc[m[1]] = true
	}
	req := map[string]bool{}
	for _, c := range RequiredContexts(mainProtection) {
		req[c] = true
		if !inDoc[c] {
			findings = append(findings, fmt.Sprintf("repo-governance.md's required-check table is missing %q", c))
		}
	}
	for c := range inDoc {
		if !req[c] {
			findings = append(findings, fmt.Sprintf("repo-governance.md's required-check table lists %q, not in main-protection.json", c))
		}
	}
	return findings
}
