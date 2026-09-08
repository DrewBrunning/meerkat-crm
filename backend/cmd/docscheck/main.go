// Command docscheck is the DOC-04 (issue #489) structural test for the docs
// site: it verifies that the prose under docs/ cannot silently drift from
// reality. Unlike the substring-guard family of *test files elsewhere in this
// repo, it runs unconditionally in CI (unit-tests.yml, the docs-citations
// job), because a docs-only PR would otherwise never exercise it.
//
// Checks (each finding is a human-actionable line on stdout; any finding is an
// exit 1):
//
//  1. Internal links and anchors. Every markdown link inside docs/**/*.md must
//     resolve to a file that exists and, when it carries a #fragment, to a
//     heading id or <a id> the target actually has. Both link spellings the
//     site uses are covered: GitHub-style .md relative links and the
//     just-the-docs-rendered .html form (deployment.html#backups), plus
//     github.com/DrewBrunning/mycorrhizal-crm/blob/main/<path> links mapped
//     back onto the checkout.
//
//  2. Repo-file references. A path-shaped reference to a tracked file
//     (backend/..., frontend/..., docs/..., cmd/<tool>, ...) must exist.
//
//  3. Operator-facing command blocks must not assume a toolchain their
//     audience does not have (issue #461 / DOC-04 step 5). A shell fence that
//     runs `go run`, `make`, `cd backend`, or a JS/Gradle tool must be
//     annotated — a shell comment in the block or a note in the section that
//     names the Go toolchain / a repo checkout — or the document must declare
//     up front that its commands assume one.
//
//  4. DOC-01 drift (issue #486). The operator-facing supported-versions page
//     states the same floors the engineering matrix
//     (development/supported-runtime-matrix.md) does. Both are prose, so the
//     consistency is pinned by token; changing a floor means changing both
//     documents (or this table).
//
//  5. DOC-03 structure (issue #488). Every outbound integration registered in
//     backend/integrations Registry() has an operator-facing section
//     (<a id="<id>">) in docs/integration-ownership.md, so a new integration
//     without a documentation section fails the build.
//
//  6. CI follows the documented commands. deploy-smoke CI and
//     docs/getting-started.md must state the same bring-up command, and
//     docs/deployment.md's upgrade block must state the same command pair the
//     operator is told to run.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"mycorrhizal/integrations"
)

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "docscheck:", err) // # pragma: no cover — main is not exercised by tests; run() is
		os.Exit(1)                                 // # pragma: no cover
	}
	if err := runAt(root); err != nil {
		fmt.Fprintln(os.Stderr, "docscheck:", err) // # pragma: no cover — main is not exercised by tests; run() is
		os.Exit(1)                                 // # pragma: no cover
	}
}

// runAt executes every check against the repository rooted at root and returns
// the first hard error (I/O) or a findings error listing every problem. It is
// separate from main so tests can drive it against a fixture tree.
func runAt(root string) error {
	findings := collectFindings(root)
	if len(findings) == 0 {
		return nil
	}
	sort.Strings(findings)
	for _, f := range findings {
		fmt.Fprintln(os.Stdout, f) // # pragma: no cover — output path; tests assert on collectFindings
	}
	return fmt.Errorf("%d documentation finding(s) — see output above", len(findings)) // # pragma: no cover — error path; tests assert on collectFindings
}

// collectFindings runs every check and returns the human-readable findings.
func collectFindings(root string) []string {
	var findings []string
	findings = append(findings, checkLinks(root)...)
	findings = append(findings, checkCommandBlocks(root)...)
	findings = append(findings, checkSupportedVersionsDrift(root)...)
	findings = append(findings, checkIntegrationOwnership(root)...)
	findings = append(findings, checkCICommandBinding(root)...)
	return findings
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err // # pragma: no cover — Getwd fails only if the cwd was deleted
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate repository root (no backend/go.mod) from %s", dir)
		}
		dir = parent
	}
}

func mustCompile(pat string) *regexp.Regexp { return regexp.MustCompile(pat) }

var (
	mdHeadingRe = mustCompile(`^(#{1,6})\s+(.*)$`)
	anchorIDRe  = mustCompile(`<a\s+id="([^"]+)"`)
)

// ---------------------------------------------------------------------------
// 1 + 2. Links, anchors, and repo-file references
// ---------------------------------------------------------------------------

func checkLinks(root string) []string {
	var findings []string
	docsDir := filepath.Join(root, "docs")
	files := listMarkdown(docsDir)
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(docsDir, rel)) // #nosec G304 -- rel comes from the filepath.Walk over docs/, never request input
		if err != nil {
			// # pragma: no cover — the file was listed by the walk above, so a
			// read failure is a racing deletion, not a normal state.
			findings = append(findings, fmt.Sprintf("%s: unreadable: %v", rel, err))
			continue
		}
		findings = append(findings, checkFileLinks(root, docsDir, rel, string(body))...)
		findings = append(findings, checkFileReferences(root, docsDir, rel, string(body))...)
	}
	return findings
}

func listMarkdown(docsDir string) []string {
	var files []string
	_ = filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".md") {
			rel, rerr := filepath.Rel(docsDir, path)
			if rerr == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func checkFileLinks(root, docsDir, rel, text string) []string {
	var findings []string
	for _, ref := range extractLinks(text) {
		dest := ref.dest
		if dest == "" {
			continue
		}
		where := fmt.Sprintf("%s:%d", rel, ref.line)
		if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
			if blobPath := githubBlobRepoPath(dest); blobPath != "" && !pathExists(root, blobPath) {
				findings = append(findings, fmt.Sprintf("%s: github link target missing: %s (no local %s)", where, dest, blobPath))
			}
			continue
		}
		if strings.HasPrefix(dest, "mailto:") || strings.HasPrefix(dest, "tel:") || dest == "#" {
			continue
		}
		pathPart, frag := splitFragment(dest)
		thisFile := filepath.Join(docsDir, rel)
		if pathPart == "" {
			if frag != "" && !headingIDs(thisFile)[frag] {
				findings = append(findings, fmt.Sprintf("%s: anchor not found in this file: #%s", where, frag))
			}
			continue
		}
		if strings.HasPrefix(pathPart, "/") {
			cand := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pathPart, "/")))
			if !pathExists(root, cand) {
				findings = append(findings, fmt.Sprintf("%s: absolute link target missing: %s", where, dest))
			}
			continue
		}
		target := filepath.Clean(filepath.Join(filepath.Dir(thisFile), filepath.FromSlash(pathPart)))
		probe := target
		if strings.HasSuffix(probe, ".html") {
			probe = strings.TrimSuffix(probe, ".html") + ".md"
		}
		if !pathExists(root, probe) && !pathExists(root, target) {
			findings = append(findings, fmt.Sprintf("%s: link target missing: %s", where, dest))
			continue
		}
		if frag != "" && strings.HasSuffix(probe, ".md") && !headingIDs(probe)[frag] {
			findings = append(findings, fmt.Sprintf("%s: anchor not found: %s", where, dest))
		}
	}
	return findings
}

func pathExists(root, p string) bool {
	if p == "" {
		return false
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	_, err := os.Stat(abs)
	return err == nil
}

// githubBlobRepoPath maps a github.com/DrewBrunning/mycorrhizal-crm blob/tree
// link to a repo-relative path, or "" if the URL is not one of ours.
func githubBlobRepoPath(u string) string {
	const prefix = "https://github.com/DrewBrunning/mycorrhizal-crm/"
	for _, kind := range []string{"blob/main/", "tree/main/", "blob/HEAD/", "tree/HEAD/"} {
		if idx := strings.Index(u, prefix+kind); idx >= 0 {
			return u[idx+len(prefix+kind):]
		}
	}
	return ""
}

func splitFragment(dest string) (pathPart, frag string) {
	if idx := strings.Index(dest, "#"); idx >= 0 {
		return dest[:idx], dest[idx+1:]
	}
	return dest, ""
}

// linkRef is one link occurrence with its raw destination and start line.
type linkRef struct {
	dest string
	line int
}

// extractLinks returns every markdown link destination in text, ignoring links
// inside fenced code blocks, HTML comments, and inline code spans. Link text
// and destinations may span lines.
func extractLinks(text string) []linkRef {
	cleaned := scrub(text)
	runes := []rune(cleaned)
	n := len(runes)
	var refs []linkRef
	i := 0
	for i < n {
		if runes[i] != '[' {
			i++
			continue
		}
		labelEnd, ok := matchBracket(runes, i, '[', ']')
		if !ok {
			i++
			continue
		}
		if labelEnd+1 < n && runes[labelEnd+1] == '(' {
			destEnd, ok := matchBracket(runes, labelEnd+1, '(', ')')
			if ok {
				dest := string(runes[labelEnd+2 : destEnd])
				line := 1 + strings.Count(string(runes[:i]), "\n")
				refs = append(refs, linkRef{dest: strings.TrimSpace(dest), line: line})
				i = destEnd + 1
				continue
			}
		}
		i = labelEnd + 1
	}
	return refs
}

// matchBracket finds the index of the matching close of an open rune at open
// index, honoring nesting. Returns the close index and whether one was found.
func matchBracket(runes []rune, open int, o, c rune) (int, bool) {
	depth := 0
	for j := open; j < len(runes); j++ {
		switch runes[j] {
		case o:
			depth++
		case c:
			depth--
			if depth == 0 {
				return j, true
			}
		}
	}
	return 0, false
}

// scrub removes fenced code blocks, HTML comments, and inline code spans from
// the text, preserving newlines so reported line numbers stay accurate.
func scrub(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, len(lines))
	copy(out, lines)
	// Fences and HTML comments in one pass.
	inFence := false
	inComment := false
	fenceMarker := ""
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(t, fenceMarker) {
				inFence = false
			}
			out[i] = ""
			continue
		}
		if strings.HasPrefix(t, "```") {
			inFence = true
			fenceMarker = "```"
			out[i] = ""
			continue
		}
		if strings.HasPrefix(t, "~~~") {
			inFence = true
			fenceMarker = "~~~"
			out[i] = ""
			continue
		}
		if strings.Contains(line, "<!--") && !inComment {
			before := line[:strings.Index(line, "<!--")]
			if afterIdx := strings.Index(line, "-->"); afterIdx >= 0 {
				out[i] = before + line[afterIdx+3:]
				continue
			}
			inComment = true
			out[i] = before
			continue
		}
		if inComment {
			if strings.Contains(line, "-->") {
				out[i] = line[strings.Index(line, "-->")+3:]
				inComment = false
			} else {
				out[i] = ""
			}
			continue
		}
	}
	return blankInlineCode(strings.Join(out, "\n"))
}

// blankInlineCode blanks the contents of inline code spans (backtick runs),
// preserving newlines.
func blankInlineCode(text string) string {
	runes := []rune(text)
	out := append([]rune(nil), runes...)
	n := len(runes)
	i := 0
	for i < n {
		if runes[i] != '`' {
			i++
			continue
		}
		runLen := 0
		for i+runLen < n && runes[i+runLen] == '`' {
			runLen++
		}
		closeAt := -1
		j := i + runLen
		for j < n {
			if runes[j] == '`' {
				k := 0
				for j+k < n && runes[j+k] == '`' {
					k++
				}
				if k == runLen {
					closeAt = j
					break
				}
				j += k
				continue
			}
			j++
		}
		end := n
		if closeAt >= 0 {
			end = closeAt + runLen
		}
		for k := i; k < end; k++ {
			if runes[k] != '\n' {
				out[k] = ' '
			}
		}
		if closeAt < 0 {
			break
		}
		i = end
	}
	return string(out)
}

// headingIDs returns every anchor id a rendered .md page will carry: a
// kramdown auto-id for each heading plus any literal <a id="..."> markers.
func headingIDs(path string) map[string]bool {
	// #nosec G304 -- path is built by the caller from docs/ walk results or
	// resolved link targets, never request input.
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	ids := map[string]bool{}
	counts := map[string]int{}
	for _, line := range strings.Split(string(body), "\n") {
		if m := mdHeadingRe.FindStringSubmatch(line); m != nil {
			base := kramdownID(removeBackticks(m[2]))
			if base == "" {
				base = "section"
			}
			counts[base]++
			if counts[base] == 1 {
				ids[base] = true
			} else {
				ids[fmt.Sprintf("%s-%d", base, counts[base]-1)] = true
			}
		}
		if am := anchorIDRe.FindAllStringSubmatch(line, -1); am != nil {
			for _, m := range am {
				ids[m[1]] = true
			}
		}
	}
	return ids
}

// removeBackticks strips the inline-code delimiters from a heading but keeps
// the code's text — that is the text kramdown computes an auto-id from ("The
// `/api/v1` promise" -> "the-apiv1-promise").
func removeBackticks(s string) string {
	return strings.ReplaceAll(s, "`", "")
}

// kramdownID reproduces kramdown's basic_generate_id: keep word characters
// (letters, digits, underscore), hyphen and space; delete everything else;
// spaces become hyphens. Empirically verified against ids this site's own
// cross-links use (e.g. "Rolling back a bad release (N+1 → N)" ->
// rolling-back-a-bad-release-n1--n).
func kramdownID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isWordRune(r) || r == '-' || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(strings.ReplaceAll(b.String(), " ", "-"))
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// checkFileReferences verifies path-shaped references to tracked files.
func checkFileReferences(root, docsDir, rel, text string) []string {
	var findings []string
	clean := stripFences(text)
	// The leading boundary keeps "cmd/actionlint" inside a Go module path
	// (github.com/rhysd/actionlint/cmd/actionlint) or "backend/" inside
	// ".../patch/backend/..." from matching.
	pathRe := mustCompile(`(?m)(?:^|[^A-Za-z0-9_./*-])((?:backend|frontend|android|docs|cmd|\.github|testdata)/[A-Za-z0-9_./{}\-*?]+)`)
	seen := map[string]bool{}
	for _, m := range pathRe.FindAllStringSubmatch(clean, -1) {
		cand := strings.TrimRight(m[1], ".,;:])},")
		if strings.ContainsAny(cand, "{}*?") || strings.Contains(cand, "...") {
			continue // a glob, a template, or an intentionally-abbreviated path
		}
		cand = trimLineColSuffix(cand)
		if seen[cand] {
			continue
		}
		seen[cand] = true
		resolved := cand
		if strings.HasPrefix(cand, "cmd/") {
			resolved = "backend/" + cand
		}
		if strings.HasPrefix(cand, "testdata/") {
			continue // ambiguous: backend/testdata, internal/*/testdata, ...
		}
		if fileReferenceResolves(root, resolved) {
			continue
		}
		// Only flag clearly file-like references. Area enumerations in prose
		// ("backend/frontend/android") and a wrapped reference truncated at a
		// line break have no file extension and would be noise.
		if !strings.Contains(filepath.Base(cand), ".") {
			continue
		}
		findings = append(findings, fmt.Sprintf("%s: file reference missing: %s", rel, cand))
	}
	return findings
}

// fileReferenceResolves reports whether the path exists, allowing a trailing
// Go package-qualified symbol (backend/internal/fsguard.NetworkFilesystemWarning
// resolves because backend/internal/fsguard is a package).
func fileReferenceResolves(root, p string) bool {
	if pathExists(root, p) {
		return true
	}
	// Strip trailing dot-qualified segments one at a time.
	for {
		base := filepath.Base(p)
		dot := strings.LastIndex(base, ".")
		if dot <= 0 {
			return false
		}
		p = p[:len(p)-len(base)+dot]
		if pathExists(root, p) {
			return true
		}
		if !strings.Contains(p, "/") {
			return false
		}
	}
}

// trimLineColSuffix removes a trailing ":<line>" or ":<line>-<col>" that cites
// code, keeping the file path.
func trimLineColSuffix(s string) string {
	idx := strings.LastIndex(s, ":")
	if idx <= 0 {
		return s
	}
	suffix := s[idx+1:]
	if suffix == "" {
		return s
	}
	for _, r := range suffix {
		if !(r >= '0' && r <= '9') && r != '-' {
			return s
		}
	}
	return s[:idx]
}

func stripFences(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
				inFence = false
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------------------
// 3. Operator command blocks must not assume an unstated toolchain
// ---------------------------------------------------------------------------

var (
	toolchainCommandRe = mustCompile(`(?:^|\s)(go\s+run|go\s+build|make\s|cd\s+backend|\./gradlew|yarn\s|npx\s|npm\s)`)
	toolchainNoteRe    = mustCompile(`(?i)(go toolchain|go\s+1\.\d|repo clone|a clone of|from source|building from source|a checkout|checkout of this repo|developer|with a go toolchain|no go toolchain)`)
	docDeclaresReposRe = mustCompile(`(?i)(requires? a checkout|assumes? .*checkout|checkout of this repo with a go toolchain|requires? .*go toolchain)`)
)

// operatorDocs is the set of pages aimed at a Docker operator following
// commands on their host. The contributor-facing pages (development/*) are
// intentionally not in this set.
var operatorDocs = []string{
	"getting-started.md",
	"deployment.md",
	"supported-versions.md",
	"upgrade-compatibility.md",
	"integration-ownership.md",
	"operations/migration-recovery.md",
	"operations/disaster-recovery.md",
	"operations/search-index.md",
}

func checkCommandBlocks(root string) []string {
	var findings []string
	for _, rel := range operatorDocs {
		// #nosec G304 -- rel is from the fixed operatorDocs list, never request input
		body, err := os.ReadFile(filepath.Join(root, "docs", filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		lines := strings.Split(string(body), "\n")
		// A document-level declaration ("commands assume a checkout of this
		// repo with a Go toolchain") licenses the blocks below it, matching how
		// migration-recovery.md and disaster-recovery.md state the assumption.
		declaration := -1
		for i, line := range lines {
			if docDeclaresReposRe.MatchString(line) {
				declaration = i
				break
			}
		}
		lastHeading := -1
		for i := 0; i < len(lines); i++ {
			t := strings.TrimSpace(lines[i])
			if !strings.HasPrefix(t, "```") || strings.HasPrefix(t, "````") {
				if mdHeadingRe.MatchString(lines[i]) {
					lastHeading = i
				}
				continue
			}
			info := strings.TrimSpace(strings.TrimPrefix(t, "```"))
			start := i
			i++
			var block []string
			for i < len(lines) {
				if strings.TrimSpace(lines[i]) == "```" {
					break
				}
				block = append(block, lines[i])
				i++
			}
			if !isShell(info) {
				continue
			}
			body := strings.Join(block, "\n")
			if !toolchainCommandRe.MatchString("\n" + body + "\n") {
				continue
			}
			if declaration >= 0 && declaration < start {
				continue
			}
			if !annotated(block, lines, lastHeading, start) {
				findings = append(findings, fmt.Sprintf("%s:%d: shell block runs %q but nothing nearby says it needs a repo checkout + Go toolchain (a Docker operator cannot run it — issue #461 / DOC-04)", rel, start+1, firstToolchainCommand(body)))
			}
		}
	}
	return findings
}

func isShell(info string) bool {
	switch info {
	case "", "sh", "bash", "shell", "console", "bash shell":
		return true
	}
	return false
}

// annotated reports whether the block, its section heading, or the prose
// between the previous heading and the block names the toolchain/checkout
// requirement.
func annotated(block, lines []string, lastHeading, blockStart int) bool {
	if toolchainNoteRe.MatchString(strings.Join(block, "\n")) {
		return true
	}
	proseStart := 0
	if lastHeading >= 0 {
		proseStart = lastHeading + 1
		if toolchainNoteRe.MatchString(lines[lastHeading]) {
			return true
		}
	}
	if blockStart-proseStart > 60 {
		proseStart = blockStart - 60
	}
	scanned := 0
	for j := blockStart - 1; j >= proseStart && scanned < 60; j-- {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		scanned++
		if toolchainNoteRe.MatchString(t) {
			return true
		}
	}
	return false
}

func firstToolchainCommand(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if m := toolchainCommandRe.FindString(line); m != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 4. DOC-01 (issue #486): supported-versions vs the engineering matrix
// ---------------------------------------------------------------------------

// operatorFloors is the operator-visible subset of the supported-runtime-matrix
// table. Each floor's token must appear in the engineering matrix and (in the
// given or alternative phrasing) in the operator page; moving a floor means
// updating both documents or this table fails.
var operatorFloors = []struct {
	label  string
	matrix string
	page   string
	alt    string
}{
	{"Docker Engine minimum", "23.0", "23.0", ""},
	{"Docker Compose", "Compose V2", "Compose V2", "Compose v2"},
	{"Browser floor (Chrome/Edge/Firefox)", "111", "111", ">=111"},
	{"Browser floor (Safari/iOS)", "16.4", "16.4", ""},
	{"Android minimum", "minSdk 26", "minSdk 26", "API 26"},
	{"Host architecture (arm)", "arm64", "arm64", ""},
	{"Host architecture (x86)", "x86_64", "x86_64", "amd64"},
	{"Host OS", "Linux", "Linux", ""},
	{"Storage constraint (network filesystems)", "network filesystems", "network filesystem", "network file system"},
	{"Storage constraint (local only)", "local filesystem", "local filesystem", "local disk"},
}

func checkSupportedVersionsDrift(root string) []string {
	// #nosec G304 -- root is the repo root from findRepoRoot, the leaf is a constant
	matrix, err := os.ReadFile(filepath.Join(root, "docs", "development", "supported-runtime-matrix.md"))
	if err != nil {
		return []string{"supported-versions drift: cannot read docs/development/supported-runtime-matrix.md: " + err.Error()}
	}
	// #nosec G304 -- root is the repo root from findRepoRoot, the leaf is a constant
	page, err := os.ReadFile(filepath.Join(root, "docs", "supported-versions.md"))
	if err != nil {
		return []string{"supported-versions drift: operator page docs/supported-versions.md missing: " + err.Error()}
	}
	mt := string(matrix)
	pt := string(page)
	var findings []string
	for _, f := range operatorFloors {
		if !strings.Contains(mt, f.matrix) {
			findings = append(findings, fmt.Sprintf("supported-versions drift: engineering matrix no longer states %q (%s) — update the matrix or drop the row", f.matrix, f.label))
		}
		inPage := strings.Contains(pt, f.page) || (f.alt != "" && strings.Contains(pt, f.alt))
		if !inPage {
			findings = append(findings, fmt.Sprintf("supported-versions drift: docs/supported-versions.md does not state %q (%s), which the engineering matrix does — update the operator page (issue #486)", f.page, f.label))
		}
	}
	return findings
}

// ---------------------------------------------------------------------------
// 5. DOC-03 (issue #488): every registered integration is documented
// ---------------------------------------------------------------------------

func checkIntegrationOwnership(root string) []string {
	// #nosec G304 -- root is the repo root from findRepoRoot, the leaf is a constant
	doc, err := os.ReadFile(filepath.Join(root, "docs", "integration-ownership.md"))
	if err != nil {
		return []string{"integration-ownership: operator page docs/integration-ownership.md missing (issue #488): " + err.Error()}
	}
	text := string(doc)
	var findings []string
	for _, entry := range integrations.Registry() {
		anchor := `<a id="` + entry.ID + `"></a>`
		if !strings.Contains(text, anchor) {
			findings = append(findings, fmt.Sprintf("integration-ownership: integration %q (%s) has no operator section in docs/integration-ownership.md — add one anchored %s (issue #488)", entry.ID, entry.Name, anchor))
		}
	}
	return findings
}

// ---------------------------------------------------------------------------
// 6. CI follows the documented commands
// ---------------------------------------------------------------------------

func checkCICommandBinding(root string) []string {
	var findings []string
	// #nosec G304 -- root is the repo root from findRepoRoot, the leaf is a constant
	gettingStarted, err := os.ReadFile(filepath.Join(root, "docs", "getting-started.md"))
	if err == nil && !strings.Contains(string(gettingStarted), "docker compose up -d --build") {
		findings = append(findings, "docs/getting-started.md no longer contains the bring-up command `docker compose up -d --build` that deploy-smoke CI runs")
	}
	// #nosec G304 -- root is the repo root from findRepoRoot, the leaf is a constant
	deployment, err := os.ReadFile(filepath.Join(root, "docs", "deployment.md"))
	if err == nil {
		dt := string(deployment)
		if !strings.Contains(dt, "docker compose pull") || !strings.Contains(dt, "docker compose up -d") {
			findings = append(findings, "docs/deployment.md no longer documents the upgrade command pair `docker compose pull` + `docker compose up -d`")
		}
	}
	// #nosec G304 -- root is the repo root from findRepoRoot, the leaf is a constant
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "deploy-smoke.yml"))
	if err == nil && !strings.Contains(string(workflow), "docker compose up -d --build") {
		findings = append(findings, "deploy-smoke.yml no longer runs the bring-up command `docker compose up -d --build` documented in docs/getting-started.md (issue #489)")
	}
	return findings
}
