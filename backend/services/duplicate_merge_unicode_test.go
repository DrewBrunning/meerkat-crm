package services

import (
	"testing"

	"mycorrhizal/contactmodel"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// I18N-02 (issue #485): duplicate detection and contact merge operate on
// byte comparisons (SQL LOWER keys / kv == lv), so two spellings of one name
// in different Unicode encodings only resolve to one person once the stored
// bytes are NFC. These tests prove that end to end: an NFD-named record
// entering through the write boundary is stored NFC, so it pairs and merges
// with an existing NFC contact of the same person.

func contactFromRecord(t *testing.T, userID uint, full, given, surname string) models.Contact {
	t.Helper()
	rec := &contactmodel.Record{
		Card: contactmodel.Card{
			Name: &contactmodel.Name{
				Full: full,
				Components: []contactmodel.NameComponent{
					{Kind: "given", Value: given},
					{Kind: "surname", Value: surname},
				},
			},
			Emails: []contactmodel.Email{{Address: "x@example.com"}},
		},
	}
	var c models.Contact
	models.ApplyRecordToContact(&c, rec, "")
	c.UserID = userID
	return c
}

// TestFindDuplicatePairs_NDFAndNFCSpellingsPair proves the headline duplicate
// claim: a contact created from an NFD spelling ("José" decomposed) and one
// from the NFC spelling of the same name are detected as duplicates, because
// the write boundary normalizes the NFD bytes to NFC before storage. They
// share no email/phone, so only the name tier can pair them.
func TestFindDuplicatePairs_NDFAndNFCSpellingsPair(t *testing.T) {
	db := newSearchDB(t)
	user := newUnicodeUser(t, db, "dupenc")

	keep := contactFromRecord(t, user.ID, "José García", "José", "García") // NFC
	loser := contactFromRecord(t, user.ID,
		"Jos\u0065\u0301 Garci\u0301a", // NFD, same person
		"Jos\u0065\u0301", "Garci\u0301a")
	require.NoError(t, db.Create(&keep).Error)
	require.NoError(t, db.Create(&loser).Error)

	// Sanity: both are NFC on disk, so the byte-level tier sees one key.
	assert.Equal(t, "José", keep.Firstname)
	assert.Equal(t, "José", loser.Firstname, "NFD ingress must be stored NFC")

	pairs, err := FindDuplicatePairs(db, user.ID)
	require.NoError(t, err)
	require.Len(t, pairs, 1, "the two encodings of one name must be one duplicate pair")
	assert.Contains(t, pairs[0].Reasons, "name")
}

// TestDetectDuplicate_NDFImportNameFindsNFCStored pins the import preview
// path: DetectDuplicate receives the ApplyRecordToContact-normalized incoming
// name, so an NFD-named vCard matches an existing NFC-stored contact (the
// historical gap — pre-normalization the byte-distinct keys never met).
func TestDetectDuplicate_NDFImportNameFindsNFCStored(t *testing.T) {
	db := newSearchDB(t)
	user := newUnicodeUser(t, db, "dupimp")

	existing := contactFromRecord(t, user.ID, "José García", "José", "García")
	require.NoError(t, db.Create(&existing).Error)

	// The import service parses + applies the incoming vCard before running
	// DetectDuplicate, so the incoming name has already been normalized here.
	match := DetectDuplicate(db, user.ID,
		"Jos\u0065\u0301", "Garci\u0301a", "", "")
	require.NotNil(t, match, "an NFD-named import must match the NFC-stored contact")
	assert.Equal(t, existing.ID, match.ExistingContactID)
	assert.Equal(t, "name", match.MatchReason)
}

// TestComputeContactMergeResolution_NDFAndNFCIsNotAConflict pins the merge
// side: with both rows NFC the scalar names compare byte-equal, so merging the
// two encodings auto-resolves instead of surfacing a spurious
// firstname/lastname conflict the user has to click through.
func TestComputeContactMergeResolution_NDFAndNFCIsNotAConflict(t *testing.T) {
	db := newSearchDB(t)
	user := newUnicodeUser(t, db, "dupmerge")

	keeper := contactFromRecord(t, user.ID, "José García", "José", "García")
	loser := contactFromRecord(t, user.ID,
		"Jos\u0065\u0301 Garci\u0301a",
		"Jos\u0065\u0301", "Garci\u0301a")
	require.NoError(t, db.Create(&keeper).Error)
	require.NoError(t, db.Create(&loser).Error)

	// Reload as the merge path would see them (stored bytes, decrypted card).
	var keepRow, loseRow models.Contact
	require.NoError(t, db.First(&keepRow, keeper.ID).Error)
	require.NoError(t, db.First(&loseRow, loser.ID).Error)

	res := ComputeContactMergeResolution(&keepRow, &loseRow)
	require.Empty(t, res.Conflicts,
		"two encodings of one name must merge without a name conflict: %+v", res.Conflicts)
}
