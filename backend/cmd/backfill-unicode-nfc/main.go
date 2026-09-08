// Command backfill-unicode-nfc rewrites any stored contact text that is still
// in decomposed Unicode (NFD) form to NFC (issue #485, I18N-02). It is the
// operator-side companion to the automatic startup backfill in main.go: the
// server runs services.NormalizeContactRecordsToNFC once per database (gated
// by migration 000052's data_backfills ledger) right after migrations and the
// at-rest encryption init, so a normal upgrade gets existing rows normalized
// without any manual step. This command exists for the case where migrations
// were applied without booting the server (e.g. `make migrate-up` alone).
//
// Idempotent and row-count-preserving — it only rewrites rows whose bytes are
// not yet NFC, never inserts or deletes, and skips entirely once the ledger
// row is present.
//
// Usage:
//
//	go run cmd/backfill-unicode-nfc/main.go [-db <path>]
package main

import (
	"flag"
	"log"

	"mycorrhizal/atrest"
	"mycorrhizal/database"
	"mycorrhizal/services"
)

func main() {
	dbPath := flag.String("db", "mycorrhizal.db", "path to the SQLite database file") // # pragma: no cover — thin CLI wiring; tests exercise run() directly
	flag.Parse()                                                                      // # pragma: no cover — thin CLI wiring; tests exercise run() directly
	if err := run(*dbPath); err != nil {                                              // # pragma: no cover — thin CLI wiring; tests exercise run() directly
		log.Fatalf("backfill-unicode-nfc: %v", err) // # pragma: no cover — log.Fatal terminates the process
	}
}

// run opens the database (applying pending migrations), arms field-level
// at-rest encryption, and runs the NFC normalization backfill. It is factored
// out of main so a test can drive it against a temp database.
func run(dbPath string) error {
	db, err := database.InitDB(dbPath)
	if err != nil {
		return err
	}

	kek, err := atrest.EncryptionKey()
	if err != nil {
		return err
	}
	if err := atrest.Initialize(db, kek); err != nil { // # pragma: no cover — a fresh InitDB database always initializes; the at-rest init failure branch is exercised by backend/atrest tests
		return err // # pragma: no cover — see the if-statement pragma above
	}

	stats, err := services.NormalizeContactRecordsToNFC(db)
	if err != nil { // # pragma: no cover — the backfill's error branches are covered by services/unicode_nfc_backfill_test.go against an injected database state this CLI cannot arrange
		return err // # pragma: no cover — see the if-statement pragma above
	}
	if stats.AlreadyDone {
		log.Println("Unicode NFC normalization already complete (data_backfills ledger present); nothing to do")
		return nil
	}
	log.Printf("Unicode NFC normalization complete: scanned=%d normalized=%d",
		stats.ContactsScanned, stats.ContactsNormalized)
	return nil
}
