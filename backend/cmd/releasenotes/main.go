// Command releasenotes assembles the operator-facing part of a release's notes
// — "Upgrade notes" and "Breaking changes" — from the pull requests merged in
// that release's range (REL-05, issue #449; docs/changelog-policy.md).
//
// It reads on stdin the JSON array `gh pr list --json number,title,url,body`
// produces and writes the assembled Markdown block to stdout. The block always
// contains an "## Upgrade notes" section (falling back to an explicit
// "no action required" line) and a "## Breaking changes" section only when a PR
// carried one.
//
// The caller is docker-publish.yml's create-release job, which prepends the
// output to the GitHub Release body above GitHub's own generated
// "What's Changed" list (.github/release.yml categorizes that half).
//
// Exit status 0: block written. 2: stdin was not a JSON array of PRs, or the
// write failed.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"mycorrhizal/internal/releasenotes"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout)) // # pragma: no cover — os.Exit terminates the process; tests exercise run() directly
}

// run reads a JSON PR array from in, writes the assembled notes block to out,
// and returns the process exit code.
func run(in io.Reader, out io.Writer) int {
	raw, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasenotes: read stdin:", err) // # pragma: no cover — stdin read failure is not reproducible in-process
		return 2
	}

	var prs []releasenotes.PR
	if err := json.Unmarshal(raw, &prs); err != nil {
		fmt.Fprintln(os.Stderr, "releasenotes: stdin must be the JSON array from `gh pr list --json number,title,url,body`:", err)
		return 2
	}

	if _, err := io.WriteString(out, releasenotes.Assemble(prs)); err != nil {
		fmt.Fprintln(os.Stderr, "releasenotes: write:", err) // # pragma: no cover — os.Stdout write failure is not reproducible in-process
		return 2
	}
	return 0
}
