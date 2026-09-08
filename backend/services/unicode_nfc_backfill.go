package services

import (
	"fmt"
	"reflect"
	"time"

	"mycorrhizal/contactmodel"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"gorm.io/gorm"
)

// Contact NFC normalization backfill (I18N-02, issue #485).
//
// Stored contact text is canonicalized to Unicode NFC at every write boundary
// (models.ApplyRecordToContact -> contactmodel.NormalizeRecord), but rows
// written before this change can still hold decomposed (NFD) bytes — macOS/iOS
// produce NFD on some paths — which byte-sensitive consumers (duplicate
// detection, the LIKE search arms, sort keys, CardDAV comparison, the Android
// offline mirror) cannot reconcile with the NFC spelling of the same person.
//
// This function rewrites those pre-existing rows. It must be a Go job, not a
// SQL migration: this SQLite build has no Unicode normalization (migration
// 000021's KNOWN LIMITATION), and the contacts.card/crm/passthrough columns
// are AES-GCM-encrypted at rest when at-rest encryption is armed, so a raw
// SQL UPDATE would corrupt them. It is the same shape of "migration provides
// the schema, a Go step provides the data transform" as atrest.Backfill and
// models.RecomputeAuditChain.
//
// It is idempotent (NFC(NFC(x)) == NFC(x)), row-count-preserving, and runs
// exactly once per database: migration 000052 creates the data_backfills
// completion ledger, and a completed row short-circuits every later call, so
// a server that boots daily does not re-scan the whole contacts table each
// time. If the process dies mid-run, already-committed pages stay normalized
// and the uncommitted remainder is simply reprocessed on the next boot — the
// ledger row is only written after every page committed.
//
// Rows are rewritten through the Contact model (ApplyRecordToContact + save),
// so the derived flat search columns (firstname/lastname/nickname/org/
// addresses_flat/phones_normalized/sort_name) are recomputed from the NFC
// card and the contacts_fts triggers keep the search index in sync — no
// separate FTS rebuild is needed (asserted by the tests via
// CheckSearchIndexConsistency). Soft-deleted rows are normalized too: they
// are someone's undo button (CLAUDE.md), and restoring one should restore NFC
// text.
//
// Callers: main.go at startup (after at-rest encryption is armed, before the
// audit chain recompute), and cmd/backfill-unicode-nfc for a migrate-up-only
// workflow. Run after migration 000052; on a pre-000052 schema it fails closed
// ("no such table: data_backfills") rather than silently doing nothing.
const (
	// ContactNFCBackfillName is the data_backfills.name ledger key for this
	// backfill.
	ContactNFCBackfillName = "contacts.unicode_nfc"
	// ContactNFCBackfillVersion bumps when a future normalization rule change
	// needs to re-run over already-processed rows.
	ContactNFCBackfillVersion = 1

	nfcBackfillPageSize = 200
)

// NFCBackfillStats reports what a run did.
type NFCBackfillStats struct {
	ContactsScanned    int64 `json:"contacts_scanned"`
	ContactsNormalized int64 `json:"contacts_normalized"`
	AlreadyDone        bool  `json:"already_done"`
}

// NormalizeContactRecordsToNFC rewrites every stored contact whose canonical
// text is not yet Unicode NFC. See the file doc comment for the full contract.
func NormalizeContactRecordsToNFC(db *gorm.DB) (*NFCBackfillStats, error) {
	stats := &NFCBackfillStats{}
	if db == nil {
		return stats, nil
	}

	done, err := nfcBackfillAlreadyDone(db)
	if err != nil {
		return stats, err
	}
	if done {
		stats.AlreadyDone = true
		logger.Info().Msg("unicode NFC normalization backfill: already complete, skipping")
		return stats, nil
	}

	start := time.Now()
	var lastID uint
	for {
		var ids []uint
		if err := db.Raw(
			`SELECT id FROM contacts WHERE id > ? ORDER BY id LIMIT ?`,
			lastID, nfcBackfillPageSize,
		).Scan(&ids).Error; err != nil {
			return stats, fmt.Errorf("unicode NFC backfill (page ids): %w", err)
		}
		if len(ids) == 0 {
			break
		}
		lastID = ids[len(ids)-1]

		var page []models.Contact
		if err := db.Unscoped().Where("id IN ?", ids).Order("id").Find(&page).Error; err != nil {
			return stats, fmt.Errorf("unicode NFC backfill (load page): %w", err)
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			for i := range page {
				c := &page[i]
				stats.ContactsScanned++

				changed, err := normalizeContactRecordToNFC(tx, c)
				if err != nil {
					return err
				}
				if changed {
					stats.ContactsNormalized++
				}
			}
			return nil
		})
		if err != nil {
			return stats, err
		}

		if len(ids) < nfcBackfillPageSize {
			break
		}
	}

	// All pages committed — record completion so later boots skip the scan.
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec(
			`INSERT INTO data_backfills (name, version, completed_at) VALUES (?, ?, ?)`,
			ContactNFCBackfillName, ContactNFCBackfillVersion, time.Now().UTC(),
		).Error
	}); err != nil {
		return stats, fmt.Errorf("unicode NFC backfill (record completion): %w", err)
	}

	logger.Info().
		Int64("contacts_scanned", stats.ContactsScanned).
		Int64("contacts_normalized", stats.ContactsNormalized).
		Int64("duration_ms", time.Since(start).Milliseconds()).
		Msg("unicode NFC normalization backfill complete")
	return stats, nil
}

// nfcBackfillAlreadyDone reports whether a completed ledger row exists. A
// missing data_backfills table is a hard error (see file doc) rather than a
// silent skip.
func nfcBackfillAlreadyDone(db *gorm.DB) (bool, error) {
	var n int64
	if err := db.Table("data_backfills").
		Where("name = ? AND version = ?", ContactNFCBackfillName, ContactNFCBackfillVersion).
		Count(&n).Error; err != nil {
		return false, fmt.Errorf("unicode NFC backfill (read ledger): %w", err)
	}
	return n > 0, nil
}

// normalizeContactRecordToNFC rewrites one contact's canonical record to NFC
// and reports whether any byte changed. It loads nothing (the caller passes a
// fully-loaded contact with its decrypted Card), recomputes the normalized
// form, and — when the card or CRM envelope actually differs — reinstates it
// through ApplyRecordToContact and saves, so the derived flat search columns
// and the FTS index follow through the normal write path.
func normalizeContactRecordToNFC(tx *gorm.DB, c *models.Contact) (bool, error) {
	// RecordForContact reads the persisted canonical record (the Card when
	// present, a flat-field fallback for pre-card rows), decrypting through
	// the at-rest serializer. No db handle: relationship-edge projection is a
	// read-only extra that would add a query per row for no benefit here.
	rec := models.RecordForContact(c, "", nil)
	nrec := contactmodel.NormalizeRecord(rec)

	if reflect.DeepEqual(rec.Card, nrec.Card) && reflect.DeepEqual(rec.Envelope, nrec.Envelope) {
		return false, nil
	}

	// ApplyRecordToContact marks the save as card-set-directly, so BeforeSave
	// derives the flat columns from the normalized Record instead of merging
	// the (still-NFD) flat fields back over it — the exact ingress semantics.
	// Unscoped so a soft-deleted row (someone's undo button) is rewritten too.
	models.ApplyRecordToContact(c, nrec, "")
	if err := tx.Unscoped().Save(c).Error; err != nil {
		return false, fmt.Errorf("unicode NFC backfill (save contact id=%d): %w", c.ID, err)
	}
	return true, nil
}
