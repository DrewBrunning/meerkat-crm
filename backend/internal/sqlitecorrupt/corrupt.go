// Package sqlitecorrupt provides a test-only helper that physically corrupts a
// real SQLite database file so the fail-closed corruption paths (the startup
// integrity probe, cmd/doctor -repair's storage gate) can be exercised against
// genuine corruption rather than a mock error.
//
// It lives in its own package so both the database package's own tests and the
// operator-CLI tests can use one implementation: the database package's tests
// cannot import internal/dbtest (import cycle), and the CLI tests must not
// re-derive the page-layout probing independently.
package sqlitecorrupt

import (
	"bytes"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/glebarez/sqlite" // registers the "sqlite" database/sql driver
)

// pageSize is SQLite's default page size for the databases this app builds.
const pageSize = 4096

// Page overwrites page number n (1-indexed) with 0xFF bytes in place, without
// probing. Callers use it when they need a specific corruption shape — e.g. a
// structural page whose corruption makes PRAGMA integrity_check itself abort
// rather than report findings — instead of DataPage's graceful candidate search.
func Page(tb testing.TB, path string, n int64) {
	tb.Helper()
	writePage(tb, path, n, bytes.Repeat([]byte{0xFF}, pageSize))
}

// DataPage overwrites one whole data page of the closed database at path with
// 0xFF bytes, in place, so the file size never changes (a truncated file can
// fail to open at all, which is a different failure mode from a corrupt page).
//
// Which page offset is a leaf data page depends on the exact schema layout, so
// this probes candidate pages outward from the middle of the file and keeps the
// first corruption that PRAGMA integrity_check reports as findings rather than
// aborting with SQLITE_CORRUPT. It fails the test if none is found (the caller
// must seed enough multi-page data).
func DataPage(tb testing.TB, path string) {
	tb.Helper()
	info, err := os.Stat(path)
	if err != nil {
		tb.Fatalf("sqlitecorrupt: stat %s: %v", path, err)
	}
	pageCount := info.Size() / pageSize
	if pageCount <= 4 {
		tb.Fatalf("sqlitecorrupt: %s has only %d pages; seed more data before corrupting", path, pageCount)
	}

	mid := pageCount / 2
	if probePageReportsFindings(tb, path, mid) {
		return
	}
	for delta := int64(1); delta < pageCount/2; delta++ {
		for _, candidate := range []int64{mid + delta, mid - delta} {
			if candidate < 2 || candidate >= pageCount {
				continue
			}
			if probePageReportsFindings(tb, path, candidate) {
				return
			}
		}
	}
	tb.Fatalf("sqlitecorrupt: could not find a page whose corruption integrity_check reports as findings in %s", path)
}

// probePageReportsFindings corrupts page `target` (1-indexed), checks whether
// an openable connection reports integrity findings (a text result other than
// "ok" with no error), and restores the original bytes on any other outcome so
// the caller can try the next page.
func probePageReportsFindings(tb testing.TB, path string, target int64) bool {
	tb.Helper()

	original := readPage(tb, path, target)
	writePage(tb, path, target, bytes.Repeat([]byte{0xFF}, pageSize))

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		writePage(tb, path, target, original)
		return false
	}

	var result string
	scanErr := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&result)
	_ = sqlDB.Close()

	if scanErr == nil && result != "" && !strings.EqualFold(result, "ok") {
		return true
	}
	writePage(tb, path, target, original)
	return false
}

func readPage(tb testing.TB, path string, target int64) []byte {
	tb.Helper()
	f, err := os.Open(path)
	if err != nil {
		tb.Fatalf("sqlitecorrupt: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	page := make([]byte, pageSize)
	if _, err := f.ReadAt(page, (target-1)*pageSize); err != nil {
		tb.Fatalf("sqlitecorrupt: read page %d of %s: %v", target, path, err)
	}
	return page
}

func writePage(tb testing.TB, path string, target int64, data []byte) {
	tb.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		tb.Fatalf("sqlitecorrupt: open %s for write: %v", path, err)
	}
	if _, err := f.WriteAt(data, (target-1)*pageSize); err != nil {
		_ = f.Close()
		tb.Fatalf("sqlitecorrupt: write page %d of %s: %v", target, path, err)
	}
	if err := f.Close(); err != nil {
		tb.Fatalf("sqlitecorrupt: close %s: %v", path, err)
	}
}
