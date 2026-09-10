// Package releasegates is the code side of REL-03, the mandatory release gates
// (issue #447, docs/development/release-gates.md).
//
// The registry is .github/release-gates.json. This package parses and
// structurally validates it, checks every gate names a real workflow file, and
// cross-checks the human-readable table in docs/development/release-gates.md
// against it. `cmd/releasegatecheck` wires those to the filesystem and runs in
// CI; the `release-gate` job in docker-publish.yml reads the same JSON with jq.
package releasegates

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Gate is one row of the registry.
type Gate struct {
	Name         string `json:"name"`
	Workflow     string `json:"workflow"`
	CheckContext string `json:"check_context"` // "" when JSON null (internal gates)
	CheckKind    string `json:"check_kind"`
	Tier         string `json:"tier"`
	Mandatory    bool   `json:"mandatory"`
	ReleaseGate  bool   `json:"release_gate"`
	Criterion    string `json:"criterion"`
}

// Registry is the parsed .github/release-gates.json.
type Registry struct {
	Gates []Gate `json:"gates"`
}

var (
	validTiers = map[string]bool{
		"per-pr":           true,
		"release-internal": true,
		"release-tier":     true,
		"advisory":         true,
	}
	validKinds = map[string]bool{
		"check_run":     true,
		"commit_status": true,
		"internal":      true,
	}
)

// Parse unmarshals data and validates every structural rule the registry
// promises. It returns the registry (usable even on error, for callers that
// want to keep going) and a slice of human-readable findings.
func Parse(data []byte) (Registry, []string) {
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return reg, []string{fmt.Sprintf(".github/release-gates.json does not parse: %v", err)}
	}

	var findings []string
	if len(reg.Gates) == 0 {
		findings = append(findings, ".github/release-gates.json has no gates")
	}

	seen := map[string]bool{}
	for i, g := range reg.Gates {
		where := fmt.Sprintf("gate %d (%q)", i, g.Name)
		if strings.TrimSpace(g.Name) == "" {
			findings = append(findings, where+": name is empty")
		}
		if seen[g.Name] {
			findings = append(findings, where+": duplicate name")
		}
		seen[g.Name] = true

		if strings.TrimSpace(g.Workflow) == "" {
			findings = append(findings, where+": workflow is empty")
		}
		if !validTiers[g.Tier] {
			findings = append(findings, fmt.Sprintf("%s: tier %q is not one of per-pr/release-internal/release-tier/advisory", where, g.Tier))
		}
		if !validKinds[g.CheckKind] {
			findings = append(findings, fmt.Sprintf("%s: check_kind %q is not one of check_run/commit_status/internal", where, g.CheckKind))
		}
		if strings.TrimSpace(g.Criterion) == "" {
			findings = append(findings, where+": criterion is empty (every gate needs an explicit pass/fail rule — issue #447)")
		}

		if g.CheckKind == "internal" {
			if g.CheckContext != "" {
				findings = append(findings, where+`: check_kind "internal" gates must have check_context null`)
			}
			if g.Tier != "release-internal" {
				findings = append(findings, where+`: check_kind "internal" only makes sense for tier "release-internal"`)
			}
		}

		if g.ReleaseGate {
			if g.Tier != "per-pr" {
				findings = append(findings, where+": release_gate:true requires tier per-pr (the release-gate job polls PR checks on the release commit)")
			}
			if g.CheckContext == "" {
				findings = append(findings, where+": release_gate:true requires a non-null check_context to poll")
			}
			if g.CheckKind != "check_run" {
				findings = append(findings, where+": release_gate:true requires check_kind check_run (the poll uses the check-runs API)")
			}
			if !g.Mandatory {
				findings = append(findings, where+": release_gate:true but mandatory:false — a polled gate must be mandatory")
			}
		}
		if g.Tier == "advisory" && g.Mandatory {
			findings = append(findings, where+": tier advisory but mandatory:true — advisory gates do not block")
		}
	}
	return reg, findings
}

// CheckWorkflows reports any gate whose workflow file does not exist. exists is
// given the bare filename (e.g. "unit-tests.yml").
func CheckWorkflows(reg Registry, exists func(workflow string) bool) []string {
	var findings []string
	for _, g := range reg.Gates {
		if g.Workflow != "" && !exists(g.Workflow) {
			findings = append(findings, fmt.Sprintf("gate %q: workflow .github/workflows/%s does not exist", g.Name, g.Workflow))
		}
	}
	return findings
}

// docRowRE matches a table row in the release-gates.md gate table:
// | `Name` | tier | yes/no | ... |
var docRowRE = regexp.MustCompile("^\\|\\s*`?([^`|]+?)`?\\s*\\|\\s*([a-z-]+)\\s*\\|\\s*(yes|no)\\s*\\|")

const (
	docBeginMarker = "<!-- release-gates:begin -->"
	docEndMarker   = "<!-- release-gates:end -->"
)

// CrossCheckDoc verifies the marked table in docs/development/release-gates.md
// has exactly one row per registry gate, with a matching tier and mandatory
// flag. This is the "every listed gate is real, and the doc cannot drift from
// the registry" guarantee.
func CrossCheckDoc(reg Registry, doc string) []string {
	start := strings.Index(doc, docBeginMarker)
	end := strings.Index(doc, docEndMarker)
	if start == -1 || end == -1 || end < start {
		return []string{fmt.Sprintf("docs/development/release-gates.md is missing the %s / %s table markers", docBeginMarker, docEndMarker)}
	}
	block := doc[start+len(docBeginMarker) : end]

	type docRow struct {
		tier      string
		mandatory bool
	}
	rows := map[string]docRow{}
	for _, line := range strings.Split(block, "\n") {
		m := docRowRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "Gate" || strings.HasPrefix(name, "---") {
			continue // header / separator
		}
		rows[name] = docRow{tier: m[2], mandatory: m[3] == "yes"}
	}

	var findings []string
	regNames := map[string]bool{}
	for _, g := range reg.Gates {
		regNames[g.Name] = true
		dr, ok := rows[g.Name]
		if !ok {
			findings = append(findings, fmt.Sprintf("release-gates.md has no table row for gate %q", g.Name))
			continue
		}
		if dr.tier != g.Tier {
			findings = append(findings, fmt.Sprintf("gate %q: table tier %q != registry tier %q", g.Name, dr.tier, g.Tier))
		}
		if dr.mandatory != g.Mandatory {
			findings = append(findings, fmt.Sprintf("gate %q: table mandatory=%v != registry mandatory=%v", g.Name, dr.mandatory, g.Mandatory))
		}
	}
	var extra []string
	for name := range rows {
		if !regNames[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		findings = append(findings, fmt.Sprintf("release-gates.md table row %q has no matching registry gate", name))
	}
	return findings
}

// ReleaseGateContexts returns the check_context of every release_gate:true gate,
// in registry order — the list the docker-publish.yml release-gate job polls.
func (r Registry) ReleaseGateContexts() []string {
	var out []string
	for _, g := range r.Gates {
		if g.ReleaseGate {
			out = append(out, g.CheckContext)
		}
	}
	return out
}
