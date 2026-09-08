package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// header is the register table's header + separator rows, shared by every
// fixture body.
const header = "| id | surface | summary | deprecated-in | deprecated-on | replacement | earliest-removal | not-before | status | tracking |\n" +
	"|---|---|---|---|---|---|---|---|---|---|\n"

// body wraps zero or more data rows in the surrounding document a real
// docs/deprecations.md has: prose, a format-spec table (whose first cell is
// "Field", never "id"), and the begin/end markers.
func body(dataRows string) string {
	return "---\ntitle: Deprecation Register\n---\n\n# Deprecation register\n\n" +
		"| Field | Meaning |\n|---|---|\n| id | identifier |\n\n" +
		beginMarker + "\n\n" + header + dataRows + "\n" + endMarker + "\n\n_prose after._\n"
}

// fixtureRoot builds a throwaway repository with backend/go.mod (the
// findRepoRoot anchor) and, unless registerBody is nil, docs/deprecations.md.
func fixtureRoot(t *testing.T, registerBody *string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "go.mod"), []byte("module mycorrhizal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if registerBody != nil {
		if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, registerFile), []byte(*registerBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func checkBody(t *testing.T, dataRows string, now time.Time) (int, string) {
	t.Helper()
	b := body(dataRows)
	root := fixtureRoot(t, &b)
	var out strings.Builder
	code, err := check(&out, root, now)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return code, out.String()
}

var now = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)

// TestRealRegister is the gate itself, run as a unit test: the real committed
// docs/deprecations.md must parse cleanly with nothing removed early, so a
// code move or a bad edit fails the backend suite too, not only CI's step.
func TestRealRegister(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	var out strings.Builder
	code, err := check(&out, root, time.Now().UTC())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if code != 0 {
		t.Fatalf("deprecations found problems in the real register:\n%s", out.String())
	}
	if !strings.Contains(out.String(), registerFile) {
		t.Fatalf("expected the summary to name %s, got:\n%s", registerFile, out.String())
	}
}

func TestEmptyRegister(t *testing.T) {
	code, out := checkBody(t, "", now)
	if code != 0 {
		t.Fatalf("code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "0 recorded deprecation(s)") {
		t.Fatalf("expected a zero-row summary, got:\n%s", out)
	}
}

func TestCleanDeprecatedRow(t *testing.T) {
	row := "| old-endpoint | api | GET /api/v1/old is superseded | v0.7.0 | 2026-09-08 | GET /api/v1/new | v0.8.0 | 2026-12-10 | deprecated | #900 |\n"
	code, out := checkBody(t, row, now)
	if code != 0 {
		t.Fatalf("code = %d, want 0; output:\n%s", code, out)
	}
}

func TestMalformedRow(t *testing.T) {
	code, out := checkBody(t, "| too | few | fields | here |\n", now)
	if code != 1 || !strings.Contains(out, "expected 10 pipe-delimited fields") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestUnknownSurfaceAndStatus(t *testing.T) {
	row := "| x | gadget | s | v0.7.0 | 2026-09-08 | y | v0.8.0 | 2026-12-10 | sunsetted | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, `surface "gadget"`) || !strings.Contains(out, `status "sunsetted"`) {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestShortWindow(t *testing.T) {
	// not-before is 30 days after deprecated-on — under the 90-day floor.
	row := "| x | config | OLD_VAR renamed | v0.7.0 | 2026-09-08 | NEW_VAR | v0.8.0 | 2026-10-08 | deprecated | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, "window is 30 days") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestEarliestRemovalNotLaterMinor(t *testing.T) {
	// same minor line (v0.7) — removal must be at least v0.8.
	row := "| x | cli | --old flag | v0.7.0 | 2026-09-08 | --new | v0.7.5 | 2026-12-10 | deprecated | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, "not a strictly later minor") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestDeprecatedRowNeedsReplacement(t *testing.T) {
	row := "| x | behavior | old sort order | v0.7.0 | 2026-09-08 | — | v0.8.0 | 2026-12-10 | deprecated | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, "must name a replacement") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestRemovedEarly(t *testing.T) {
	// window says v0.8.0 / 2026-12-10; tracking claims removal in v0.7.1 on 2026-10-01.
	row := "| x | api | field dropped | v0.7.0 | 2026-09-08 | new field | v0.8.0 | 2026-12-10 | removed | #1 removed-in=v0.7.1 removed-on=2026-10-01 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 ||
		!strings.Contains(out, "removed-in (v0.7.1) is earlier than earliest-removal") ||
		!strings.Contains(out, "removed-on (2026-10-01) is before not-before") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestRemovedRowNeedsRemovalFacts(t *testing.T) {
	row := "| x | api | field dropped | v0.7.0 | 2026-09-08 | new field | v0.8.0 | 2026-12-10 | removed | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, "must carry removed-in=") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestRemovedProperly(t *testing.T) {
	row := "| x | api | field dropped | v0.7.0 | 2026-01-01 | new field | v0.8.0 | 2026-04-10 | removed | #1 removed-in=v0.8.0 removed-on=2026-05-01 |\n"
	code, out := checkBody(t, row, now)
	if code != 0 {
		t.Fatalf("code = %d, want 0; output:\n%s", code, out)
	}
}

func TestPastNotBeforeAdvisory(t *testing.T) {
	// A still-`deprecated` row whose window has elapsed: a note, not a failure.
	row := "| x | api | GET /api/v1/old | v0.5.0 | 2026-01-01 | GET /api/v1/new | v0.6.0 | 2026-04-01 | deprecated | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 0 {
		t.Fatalf("code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "note:") || !strings.Contains(out, "eligible for removal") {
		t.Fatalf("expected an eligible-for-removal advisory, got:\n%s", out)
	}
}

func TestDuplicateID(t *testing.T) {
	rows := "| dup | api | a | v0.7.0 | 2026-09-08 | x | v0.8.0 | 2026-12-10 | deprecated | #1 |\n" +
		"| dup | config | b | v0.7.0 | 2026-09-08 | y | v0.8.0 | 2026-12-10 | deprecated | #2 |\n"
	code, out := checkBody(t, rows, now)
	if code != 1 || !strings.Contains(out, `id "dup" already used`) {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestMissingMarkers(t *testing.T) {
	raw := "# Deprecation register\n\n" + header + "\n_no markers here._\n"
	root := fixtureRoot(t, &raw)
	var out strings.Builder
	code, err := check(&out, root, now)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if code != 1 || !strings.Contains(out.String(), "register markers") {
		t.Fatalf("code = %d, output:\n%s", code, out.String())
	}
}

func TestBadDates(t *testing.T) {
	row := "| x | api | s | v0.7.0 | 2026-13-40 | y | v0.8.0 | soon | deprecated | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, "deprecated-on") || !strings.Contains(out, "not-before") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestBadVersions(t *testing.T) {
	row := "| x | api | s | 0.7 | 2026-09-08 | y | v0.8.x | 2026-12-10 | deprecated | #1 |\n"
	code, out := checkBody(t, row, now)
	if code != 1 || !strings.Contains(out, "deprecated-in") || !strings.Contains(out, "earliest-removal") {
		t.Fatalf("code = %d, output:\n%s", code, out)
	}
}

func TestFindRepoRoot(t *testing.T) {
	if _, err := findRepoRoot(); err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
}

func TestRun(t *testing.T) {
	t.Run("clean register exits 0", func(t *testing.T) {
		b := body("")
		root := fixtureRoot(t, &b)
		t.Chdir(root)
		var out strings.Builder
		if code := run(&out, now); code != 0 {
			t.Fatalf("code = %d, want 0; output:\n%s", code, out.String())
		}
	})

	t.Run("problems exit 1", func(t *testing.T) {
		b := body("| bad | row |\n")
		root := fixtureRoot(t, &b)
		t.Chdir(root)
		var out strings.Builder
		if code := run(&out, now); code != 1 {
			t.Fatalf("code = %d, want 1; output:\n%s", code, out.String())
		}
	})

	t.Run("no repository root exits 2", func(t *testing.T) {
		t.Chdir(t.TempDir())
		var out strings.Builder
		if code := run(&out, now); code != 2 {
			t.Fatalf("code = %d, want 2; output:\n%s", code, out.String())
		}
		if !strings.Contains(out.String(), "deprecations:") {
			t.Fatalf("expected a prefixed error, got:\n%s", out.String())
		}
	})

	t.Run("missing register exits 2", func(t *testing.T) {
		root := fixtureRoot(t, nil)
		t.Chdir(root)
		var out strings.Builder
		if code := run(&out, now); code != 2 {
			t.Fatalf("code = %d, want 2; output:\n%s", code, out.String())
		}
	})
}
