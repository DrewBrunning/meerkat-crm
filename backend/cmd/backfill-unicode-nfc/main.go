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
	dbPath := flag.String("db", "mycorrhizal.db", "path to the SQLite database file")
	flag.Parse()

	db, err := database.InitDB(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	kek, err := atrest.EncryptionKey()
	if err != nil {
		log.Fatalf("failed to resolve at-rest encryption master key: %v", err)
	}
	if err := atrest.Initialize(db, kek); err != nil {
		log.Fatalf("failed to initialize at-rest encryption: %v", err)
	}

	stats, err := services.NormalizeContactRecordsToNFC(db)
	if err != nil {
		log.Fatalf("failed to backfill Unicode NFC normalization: %v", err)
	}
	if stats.AlreadyDone {
		log.Println("Unicode NFC normalization already complete (data_backfills ledger present); nothing to do")
		return
	}
	log.Printf("Unicode NFC normalization complete: scanned=%d normalized=%d",
		stats.ContactsScanned, stats.ContactsNormalized)
}
