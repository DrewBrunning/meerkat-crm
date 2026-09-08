package services

import (
	"fmt"
	"testing"

	"mycorrhizal/atrest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// I18N-02 (issue #485): the startup backfill that rewrites pre-existing NFD
// contact text to NFC. These run against the real migrated schema (dbtest),
// because migration 000052's data_backfills ledger and the contacts_fts
// triggers are what the backfill's once-only gate and index-sync contract
// depend on.

// seedLegacyNFDContact writes a contact the way a pre-000052 database holds
// one: decomposed bytes in the flat columns, with the NFD card derived from
// them by the normal save path (ApplyRecordToContact is deliberately NOT used
// — that is the very boundary this backfill exists to retro-fit).
func seedLegacyNFDContact(t *testing.T, db *gorm.DB, userID uint, first, last, email string) models.Contact {
	t.Helper()
	c := models.Contact{
		UserID: userID, Firstname: first, Lastname: last, Email: email,
	}
	require.NoError(t, db.Create(&c).Error)
	// Sanity: the seeded bytes really are NFD before the backfill runs.
	require.False(t, norm.NFC.IsNormalString(c.Firstname), "fixture must hold decomposed bytes")
	return c
}

func countContacts(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.Contact{}).Unscoped().Count(&n).Error)
	return n
}

// TestNormalizeContactsToNFC_NFDPreExistingRowsNormalized pins the core
// backfill contract: a decomposed legacy row becomes NFC in both the flat
// search columns and the canonical card, the row count is preserved, the
// contacts_fts index stays consistent with the trigger-maintained contract
// (no separate rebuild needed), and the same person's NFC spelling now pairs
// as a duplicate.
func TestNormalizeContactsToNFC_NFDPreExistingRowsNormalized(t *testing.T) {
	db := newSearchDB(t)
	user := newUnicodeUser(t, db, "nfcbf")

	seedLegacyNFDContact(t, db, user.ID, "Jos\u0065\u0301", "P\u0065\u0301rez", "legacy@example.com")
	// A same-name NFC spelling with a different email — the duplicate pair the
	// byte-distinct encodings previously hid.
	require.NoError(t, db.Create(&models.Contact{
		UserID: user.ID, Firstname: "José", Lastname: "Pérez", Email: "nfc@example.com",
	}).Error)
	before := countContacts(t, db)

	stats, err := NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.False(t, stats.AlreadyDone)
	require.EqualValues(t, 2, stats.ContactsScanned)
	require.EqualValues(t, 1, stats.ContactsNormalized, "only the NFD row should need a rewrite")

	assert.Equal(t, before, countContacts(t, db), "backfill must never change row counts")

	var stored models.Contact
	require.NoError(t, db.Where("user_id = ? AND email = ?", user.ID, "legacy@example.com").First(&stored).Error)
	assert.Equal(t, "José", stored.Firstname)
	assert.Equal(t, "Pérez", stored.Lastname)
	require.True(t, norm.NFC.IsNormalString(stored.Firstname), "flat firstname must be NFC after the backfill")
	if stored.Card.Name != nil {
		for _, comp := range stored.Card.Name.Components {
			if !norm.NFC.IsNormalString(comp.Value) {
				t.Fatalf("card name component %q is not NFC after the backfill", comp.Value)
			}
		}
	}

	// FTS stays consistent — the backfill wrote through the model, so the
	// contacts_fts triggers kept the index in lockstep.
	check, err := CheckSearchIndexConsistency(db)
	require.NoError(t, err)
	assert.True(t, check.Clean(), "FTS index must be consistent after the backfill: %s", check.Summary())

	// The issue's duplicate claim: the two spellings are now one person.
	pairs, err := FindDuplicatePairs(db, user.ID)
	require.NoError(t, err)
	require.Len(t, pairs, 1, "NFD + NFC spellings must pair as duplicates after the backfill")
}

// TestNormalizeContactsToNFC_LedgerMakesSecondRunANoOp pins the once-only
// gate: after a completed run, the data_backfills row short-circuits every
// later call (so a daily boot never re-scans the whole contacts table).
func TestNormalizeContactsToNFC_LedgerMakesSecondRunANoOp(t *testing.T) {
	db := newSearchDB(t)
	user := newUnicodeUser(t, db, "nfcbfonce")
	seedLegacyNFDContact(t, db, user.ID, "Caf\u0065\u0301", "Q", "a@example.com")

	first, err := NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.EqualValues(t, 1, first.ContactsNormalized)

	second, err := NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.True(t, second.AlreadyDone, "a completed ledger row must short-circuit the second run")
	require.Zero(t, second.ContactsNormalized)

	// Idempotence even if the ledger is wiped (crash between page commits and
	// the ledger write): re-running over already-NFC rows is a no-op.
	require.NoError(t, db.Exec("DELETE FROM data_backfills").Error)
	third, err := NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.False(t, third.AlreadyDone)
	require.Zero(t, third.ContactsNormalized, "re-run over NFC rows must normalize nothing")
}

// TestNormalizeContactsToNFC_AlreadyNFCRowsUntouched pins that an NFC corpus
// (the canonical post-I18N-01 state) needs no rewrites and no side effects.
func TestNormalizeContactsToNFC_AlreadyNFCRowsUntouched(t *testing.T) {
	db := newSearchDB(t)
	user := newUnicodeUser(t, db, "nfcbfclean")
	require.NoError(t, db.Create(&models.Contact{
		UserID: user.ID, Firstname: "García", Lastname: "Müller", Email: "clean@example.com",
	}).Error)
	require.NoError(t, db.Create(&models.Contact{
		UserID: user.ID, Firstname: "प्रिया", Lastname: "शर्मा", Email: "priya@example.com",
	}).Error)

	stats, err := NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.Zero(t, stats.ContactsNormalized, "NFC rows must be untouched")
	require.EqualValues(t, 2, stats.ContactsScanned)
}

// TestNormalizeContactsToNFC_EncryptedAtRestDatabase pins the at-rest path:
// when field-level encryption is armed, the backfill still reads (decrypts)
// and rewrites the card columns through the serializer and lands NFC rows.
func TestNormalizeContactsToNFC_EncryptedAtRestDatabase(t *testing.T) {
	db := newSearchDB(t)
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(0x42 + i)
	}
	require.NoError(t, atrest.Initialize(db, kek))
	t.Cleanup(atrest.ResetForTest)

	user := newUnicodeUser(t, db, "nfcbfenc")
	seedLegacyNFDContact(t, db, user.ID, "Jos\u0065\u0301", "Garc\u0069\u0301a", "enc@example.com")

	stats, err := NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.ContactsNormalized)

	var stored models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&stored).Error)
	assert.Equal(t, "José", stored.Firstname)
	assert.Equal(t, "García", stored.Lastname)
	require.True(t, norm.NFC.IsNormalString(stored.Firstname))

	// The stored column really is ciphertext (the at-rest contract) — the
	// plaintext name must not be sitting in the row's card blob.
	var raw struct{ Card string }
	require.NoError(t, db.Raw("SELECT card FROM contacts WHERE id = ?", stored.ID).Scan(&raw).Error)
	assert.NotContains(t, raw.Card, "José", "the encrypted card column must not hold plaintext")
	assert.NotContains(t, raw.Card, "Jos", "the encrypted card column must not hold plaintext")
}

// TestNormalizeContactsToNFC_MissingLedgerFailsClosed pins that running on a
// pre-000052 schema (no data_backfills table) errors loudly rather than
// silently doing nothing.
func TestNormalizeContactsToNFC_MissingLedgerFailsClosed(t *testing.T) {
	db := newSearchDB(t)
	require.NoError(t, db.Exec("DROP TABLE data_backfills").Error)

	_, err := NormalizeContactRecordsToNFC(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "data_backfills", fmt.Sprintf("got: %v", err))
}

// TestNormalizeContactsToNFC_EdgeBranches covers the branches the happy-path
// tests cannot: a nil db handle, a wholly empty contacts table, and the three
// failure exits (page query, save rollback atomicity, completion-record
// insert).
func TestNormalizeContactsToNFC_EdgeBranches(t *testing.T) {
	t.Run("nil db is a no-op", func(t *testing.T) {
		stats, err := NormalizeContactRecordsToNFC(nil)
		require.NoError(t, err)
		require.NotNil(t, stats)
	})

	t.Run("empty contacts table completes and records the ledger", func(t *testing.T) {
		db := newSearchDB(t) // no contacts
		stats, err := NormalizeContactRecordsToNFC(db)
		require.NoError(t, err)
		require.Zero(t, stats.ContactsScanned)
		require.Zero(t, stats.ContactsNormalized)

		second, err := NormalizeContactRecordsToNFC(db)
		require.NoError(t, err)
		require.True(t, second.AlreadyDone)
	})

	t.Run("page query failure errors loudly", func(t *testing.T) {
		db := newSearchDB(t)
		require.NoError(t, db.Exec("DROP TABLE contacts").Error) // Raw page query now has nothing to scan
		_, err := NormalizeContactRecordsToNFC(db)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "page ids", fmt.Sprintf("got: %v", err))
	})

	t.Run("save failure rolls the page back atomically", func(t *testing.T) {
		db := newSearchDB(t)
		user := newUnicodeUser(t, db, "nfcbfatomic")
		legacy := seedLegacyNFDContact(t, db, user.ID, "Jos\u0065\u0301", "P\u0065\u0301rez", "atomic@example.com")

		// Block every UPDATE to contacts — the normalize save must fail and the
		// page transaction (including the NFD->NFC rewrite) must roll back whole.
		require.NoError(t, db.Exec("CREATE TRIGGER block_contact_save BEFORE UPDATE ON contacts BEGIN SELECT RAISE(ABORT, 'blocked'); END").Error)
		_, err := NormalizeContactRecordsToNFC(db)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save contact", fmt.Sprintf("got: %v", err))

		var stored models.Contact
		require.NoError(t, db.Unscoped().First(&stored, legacy.ID).Error)
		assert.Equal(t, "Jos\u0065\u0301", stored.Firstname, "the aborted page must leave the NFD row untouched")
		require.NoError(t, db.Exec("DROP TRIGGER block_contact_save").Error)
	})

	t.Run("unreadable card fails the page load loudly", func(t *testing.T) {
		db := newSearchDB(t)
		user := newUnicodeUser(t, db, "nfcbfbadcard")
		c := seedLegacyNFDContact(t, db, user.ID, "Jos\u0065\u0301", "Q", "badcard@example.com")
		require.NoError(t, db.Exec("UPDATE contacts SET card = '{not valid json' WHERE id = ?", c.ID).Error)

		_, err := NormalizeContactRecordsToNFC(db)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load page", fmt.Sprintf("got: %v", err))
	})

	t.Run("completion-record failure leaves rows normalized and errors", func(t *testing.T) {
		db := newSearchDB(t)
		user := newUnicodeUser(t, db, "nfcbfcomplete")
		c := seedLegacyNFDContact(t, db, user.ID, "Jos\u0065\u0301", "P\u0065\u0301rez", "complete@example.com")

		// Page work commits; only the final ledger INSERT fails. The backfill must
		// error (the operator re-runs it) while the already-normalized rows stay
		// normalized — the idempotent re-run covers the rest.
		require.NoError(t, db.Exec("CREATE TRIGGER block_ledger_insert BEFORE INSERT ON data_backfills BEGIN SELECT RAISE(ABORT, 'blocked'); END").Error)
		_, err := NormalizeContactRecordsToNFC(db)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "record completion", fmt.Sprintf("got: %v", err))
		require.NoError(t, db.Exec("DROP TRIGGER block_ledger_insert").Error)

		var stored models.Contact
		require.NoError(t, db.First(&stored, c.ID).Error)
		assert.Equal(t, "José", stored.Firstname, "page work before the ledger failure must be committed")

		// Re-run now unblocked completes the ledger (no rows to rewrite — the
		// page already committed), and a third run short-circuits on it.
		stats, err := NormalizeContactRecordsToNFC(db)
		require.NoError(t, err)
		require.False(t, stats.AlreadyDone)
		require.Zero(t, stats.ContactsNormalized)
		third, err := NormalizeContactRecordsToNFC(db)
		require.NoError(t, err)
		require.True(t, third.AlreadyDone)
	})
}
