// Command deprecations is the freshness gate for the deprecation register
// (MAINT-01, issue #490).
//
// docs/deprecation-policy.md makes a specific promise: anything retired on a
// covered surface (the /api/v1 contract, the schema third parties read,
// configuration variables, CLI commands and flags, export formats, documented
// behaviour) is announced with a replacement and a window — at least one minor
// release and never less than 90 days — before it is removed, and every such
// deprecation is recorded in docs/deprecations.md before it ships. An
// unenforced policy is a document, so this command re-verifies the register on
// every run:
//
//  1. Every row in the register block parses as the ten-field format the
//     file's own header documents, with a known surface, a known status,
//     valid versions, and valid dates.
//  2. The window is real: not-before is at least 90 days after deprecated-on,
//     and earliest-removal is a strictly later minor than deprecated-in.
//  3. A `deprecated` row names a replacement (a deprecation without one is a
//     removal).
//  4. A `removed` row carries removed-in / removed-on in its tracking field,
//     and neither is earlier than the row's own earliest-removal / not-before
//     — nothing is removed before its window expires, which is the case the
//     whole policy exists to prevent.
//
// It also prints an advisory (without failing) for each `deprecated` row whose
// not-before has passed: eligible for removal, still supported until someone
// removes it deliberately.
//
// Exit status 0 means every row is well-formed and no row was removed early
// (including zero rows — nothing to report is not a failure); 1 means at least
// one row is malformed or violates the policy; 2 means the check itself could
// not run.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// registerFile is the register this command checks, repo-root-relative.
const registerFile = "docs/deprecations.md"

// dateLayout is the register's date format for deprecated-on / not-before /
// removed-on.
const dateLayout = "2006-01-02"

// minWindowDays is the calendar floor on a deprecation window, measured from
// deprecated-on to not-before. 90 days matches cmd/depexceptions' maxWindowDays
// and the dependency-upgrade-policy default — one project-wide number for
// "give people time," not a second one invented here.
const minWindowDays = 90

// beginMarker / endMarker bracket the register table in registerFile, so the
// format-spec table earlier in the same document is never parsed as data.
const (
	beginMarker = "<!-- deprecations:begin -->"
	endMarker   = "<!-- deprecations:end -->"
)

// validSurfaces are the covered surfaces from docs/deprecation-policy.md.
var validSurfaces = map[string]bool{
	"api":      true,
	"schema":   true,
	"config":   true,
	"cli":      true,
	"export":   true,
	"behavior": true,
}

// validStatuses are the register's row states.
var validStatuses = map[string]bool{
	"deprecated": true,
	"removed":    true,
	"withdrawn":  true,
}

// versionRE matches vMAJOR.MINOR or vMAJOR.MINOR.PATCH.
var versionRE = regexp.MustCompile(`^v(\d+)\.(\d+)(?:\.(\d+))?$`)

// removedInRE / removedOnRE pull the removed-in / removed-on facts a `removed`
// row must carry in its tracking field.
var (
	removedInRE = regexp.MustCompile(`removed-in=(v\d+\.\d+(?:\.\d+)?)`)
	removedOnRE = regexp.MustCompile(`removed-on=(\d{4}-\d{2}-\d{2})`)
)

func main() {
	os.Exit(run(os.Stdout, time.Now().UTC())) // # pragma: no cover — os.Exit terminates the process; tests exercise run() directly
}

// row is one parsed, structurally valid register row.
type row struct {
	line            int
	id              string
	surface         string
	summary         string
	deprecatedIn    version
	deprecatedOn    time.Time
	replacement     string
	earliestRemoval version
	notBefore       time.Time
	status          string
	tracking        string
}

// version is a parsed vMAJOR.MINOR[.PATCH], compared on (major, minor).
type version struct{ major, minor, patch int }

func (v version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
}

// laterMinorThan reports whether v is a strictly later minor line than other.
func (v version) laterMinorThan(other version) bool {
	if v.major != other.major {
		return v.major > other.major
	}
	return v.minor > other.minor
}

// atLeastMinorOf reports whether v is on the same minor line as other or a
// later one.
func (v version) atLeastMinorOf(other version) bool {
	return v.major == other.major && v.minor == other.minor || v.laterMinorThan(other)
}

// finding is one problem, reported as `line N: message`. Every finding traces
// to a specific register line.
type finding struct {
	line int
	msg  string
}

// run performs the whole check against the real repository and writes a
// summary to w. Split out of main, with root/now injected, so every exit path
// is covered by tests without touching the process environment or the clock.
func run(w io.Writer, now time.Time) int {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(w, "deprecations:", err)
		return 2
	}
	code, err := check(w, root, now)
	if err != nil {
		fmt.Fprintln(w, "deprecations:", err)
		return 2
	}
	return code
}

// check reads and validates the register at root/registerFile.
func check(w io.Writer, root string, now time.Time) (int, error) {
	// #nosec G304 -- registerFile is a fixed internal constant, never request
	// input, joined with root from findRepoRoot walking up from the process's
	// own working directory. Same posture as cmd/depexceptions' ledger read
	// and cmd/citecheck's readRepoFile.
	body, err := os.ReadFile(filepath.Join(root, registerFile))
	if err != nil {
		return 0, err
	}

	if !strings.Contains(string(body), beginMarker) || !strings.Contains(string(body), endMarker) {
		fmt.Fprintf(w, "\n%s is missing the %s / %s register markers\n", registerFile, beginMarker, endMarker)
		return 1, nil
	}

	rows, parseFindings := parseRegister(string(body))
	findings := append([]finding{}, parseFindings...)
	advisories, validateFindings := validate(rows, now)
	findings = append(findings, validateFindings...)

	for _, a := range advisories {
		fmt.Fprintf(w, "  note: %s\n", a)
	}

	if len(findings) == 0 {
		fmt.Fprintf(w, "%s: %d recorded deprecation(s), none removed early.\n", registerFile, len(rows))
		return 0, nil
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].line < findings[j].line })
	fmt.Fprintf(w, "\n%d problem(s) in %s:\n", len(findings), registerFile)
	for _, f := range findings {
		fmt.Fprintf(w, "  line %d: %s\n", f.line, f.msg)
	}
	return 1, nil
}

// parseRegister reads every table row between the begin/end markers as a
// ten-field pipe-delimited entry, skipping the header and separator rows. A
// row that fails to parse is reported as a finding rather than skipped, so a
// typo cannot silently disappear.
func parseRegister(body string) ([]row, []finding) {
	var rows []row
	var findings []finding

	lines := strings.Split(body, "\n")
	inBlock := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == beginMarker:
			inBlock = true
			continue
		case line == endMarker:
			inBlock = false
			continue
		}
		if !inBlock || !strings.HasPrefix(line, "|") {
			continue
		}
		if isSeparatorRow(line) {
			continue
		}
		fields := splitRow(line)
		// The header row: first cell is literally "id".
		if len(fields) > 0 && fields[0] == "id" {
			continue
		}
		lineNo := i + 1
		if len(fields) != 10 {
			findings = append(findings, finding{lineNo, fmt.Sprintf(
				"expected 10 pipe-delimited fields (id | surface | summary | deprecated-in | deprecated-on | replacement | earliest-removal | not-before | status | tracking), got %d", len(fields))})
			continue
		}
		r, bad := parseRow(lineNo, fields)
		if len(bad) > 0 {
			findings = append(findings, finding{lineNo, strings.Join(bad, "; ")})
			continue
		}
		rows = append(rows, r)
	}
	return rows, findings
}

// parseRow validates the ten cells of one data row.
func parseRow(lineNo int, f []string) (row, []string) {
	r := row{
		line:        lineNo,
		id:          f[0],
		surface:     f[1],
		summary:     f[2],
		replacement: f[5],
		status:      f[8],
		tracking:    f[9],
	}
	var bad []string
	if r.id == "" {
		bad = append(bad, "id is empty")
	}
	if !validSurfaces[r.surface] {
		bad = append(bad, fmt.Sprintf("surface %q is not one of api/schema/config/cli/export/behavior", r.surface))
	}
	if r.summary == "" {
		bad = append(bad, "summary is empty")
	}
	if !validStatuses[r.status] {
		bad = append(bad, fmt.Sprintf("status %q is not one of deprecated/removed/withdrawn", r.status))
	}
	if r.tracking == "" {
		bad = append(bad, "tracking is empty")
	}
	if v, err := parseVersion(f[3]); err != nil {
		bad = append(bad, fmt.Sprintf("deprecated-in %q: %v", f[3], err))
	} else {
		r.deprecatedIn = v
	}
	if v, err := parseVersion(f[6]); err != nil {
		bad = append(bad, fmt.Sprintf("earliest-removal %q: %v", f[6], err))
	} else {
		r.earliestRemoval = v
	}
	if t, err := time.Parse(dateLayout, f[4]); err != nil {
		bad = append(bad, fmt.Sprintf("deprecated-on %q is not a YYYY-MM-DD date", f[4]))
	} else {
		r.deprecatedOn = t
	}
	if t, err := time.Parse(dateLayout, f[7]); err != nil {
		bad = append(bad, fmt.Sprintf("not-before %q is not a YYYY-MM-DD date", f[7]))
	} else {
		r.notBefore = t
	}
	return r, bad
}

// validate checks the policy rules that need the whole row parsed: the window
// is real, a live deprecation has a replacement, and nothing was removed
// early. It returns advisories (non-failing notes) separately from findings
// (build failures).
func validate(rows []row, now time.Time) (advisories []string, findings []finding) {
	seen := map[string]int{}
	for _, r := range rows {
		if first, dup := seen[r.id]; dup {
			findings = append(findings, finding{r.line, fmt.Sprintf("id %q already used on line %d", r.id, first)})
			continue
		}
		seen[r.id] = r.line

		if !r.earliestRemoval.laterMinorThan(r.deprecatedIn) {
			findings = append(findings, finding{r.line, fmt.Sprintf(
				"earliest-removal (%s) is not a strictly later minor than deprecated-in (%s)",
				r.earliestRemoval, r.deprecatedIn)})
		}
		if window := r.notBefore.Sub(r.deprecatedOn); window < time.Duration(minWindowDays)*24*time.Hour {
			findings = append(findings, finding{r.line, fmt.Sprintf(
				"window is %d days (not-before %s minus deprecated-on %s) — the minimum is %d",
				int(window.Hours()/24), r.notBefore.Format(dateLayout), r.deprecatedOn.Format(dateLayout), minWindowDays)})
		}

		switch r.status {
		case "deprecated":
			if isEmptyCell(r.replacement) {
				findings = append(findings, finding{r.line, "a deprecated row must name a replacement (an empty replacement is a removal)"})
			}
			if r.notBefore.Before(now) {
				advisories = append(advisories, fmt.Sprintf(
					"%s (%s) is past its not-before date %s — eligible for removal; still supported until explicitly removed",
					r.id, r.surface, r.notBefore.Format(dateLayout)))
			}
		case "removed":
			findings = append(findings, validateRemoved(r)...)
		}
	}
	return advisories, findings
}

// validateRemoved checks that a `removed` row carries removal facts and that
// the removal did not predate the row's own window.
func validateRemoved(r row) []finding {
	var out []finding
	inMatch := removedInRE.FindStringSubmatch(r.tracking)
	onMatch := removedOnRE.FindStringSubmatch(r.tracking)
	if inMatch == nil || onMatch == nil {
		out = append(out, finding{r.line, "a removed row must carry removed-in=vX.Y[.Z] and removed-on=YYYY-MM-DD in its tracking field"})
		return out
	}
	removedIn, err := parseVersion(inMatch[1])
	if err != nil {
		out = append(out, finding{r.line, fmt.Sprintf("removed-in %q: %v", inMatch[1], err)})
		return out
	}
	removedOn, err := time.Parse(dateLayout, onMatch[1])
	if err != nil {
		out = append(out, finding{r.line, fmt.Sprintf("removed-on %q is not a YYYY-MM-DD date", onMatch[1])})
		return out
	}
	if !removedIn.atLeastMinorOf(r.earliestRemoval) {
		out = append(out, finding{r.line, fmt.Sprintf(
			"removed-in (%s) is earlier than earliest-removal (%s) — removed before the window expired",
			removedIn, r.earliestRemoval)})
	}
	if removedOn.Before(r.notBefore) {
		out = append(out, finding{r.line, fmt.Sprintf(
			"removed-on (%s) is before not-before (%s) — removed before the window expired",
			removedOn.Format(dateLayout), r.notBefore.Format(dateLayout))})
	}
	return out
}

// isSeparatorRow reports whether a table line is the `|---|---|` header rule.
func isSeparatorRow(line string) bool {
	return strings.Trim(line, "|-: ") == ""
}

// splitRow splits a GFM table row into trimmed cells, dropping the empty
// leading/trailing cells the outer pipes produce.
func splitRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// isEmptyCell reports whether a cell carries no real content — blank, or one of
// the dashes a Markdown author writes for "nothing here".
func isEmptyCell(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "-", "–", "—", "n/a", "none":
		return true
	}
	return false
}

// parseVersion parses vMAJOR.MINOR or vMAJOR.MINOR.PATCH.
func parseVersion(s string) (version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return version{}, fmt.Errorf("not a vMAJOR.MINOR[.PATCH] version")
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	patch := 0
	if m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}
	return version{major: maj, minor: min, patch: patch}, nil
}

// findRepoRoot walks up from the working directory until it finds
// backend/go.mod, so the command works from backend/ (the documented `go run`
// invocation) and from backend/cmd/deprecations/ (`go test`) alike.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		// # pragma: no cover — Getwd fails only when the cwd has been deleted,
		// which no test can arrange for its own process.
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no repository root above %s (looked for backend/go.mod)", dir)
		}
		dir = parent
	}
}
