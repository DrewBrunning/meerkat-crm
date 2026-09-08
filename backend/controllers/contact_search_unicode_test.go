package controllers

import (
	"testing"

	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// I18N-02 (issue #485): the contacts-LIST search path (GET /contacts?search=,
// applyContactSearch) gets the same Unicode guarantees as /search — stored
// NFC text is found by NFD/NFC/ASCII and case/accent-varied queries, because
// the query term is NFC-folded (NormalizeSearchTerm) before it reaches the
// LIKE arms (byte comparisons) and the FTS5 clause. These run against the real
// migrated schema (ftsRealRouter), so the contacts_fts virtual table +
// triggers are live.

func createNamed(t *testing.T, db *gorm.DB, userID uint, firstname, lastname string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Contact{UserID: userID, Firstname: firstname, Lastname: lastname}).Error)
}

func TestGetContactsSearch_CrossEncodingAndDiacriticFold(t *testing.T) {
	db, router, user := ftsRealRouter(t, "i18n-cross.db")

	// Stored via direct create in NFC (the write boundary guarantees this in
	// production; the ingress tests in models/ pin that).
	createNamed(t, db, user.ID, "José", "García")

	for _, q := range []string{
		"search=José",
		"search=Jos\u0065\u0301", // NFD query
		"search=Jose",
		"search=jose",
		"search=garcia", // accent-insensitive on the surname token
		"search=GARCÍA", // case+accent-insensitive
		"search=Garci\u0301a",
	} {
		items := ftsSearch(t, router, q)
		require.Len(t, items, 1, "query %q must find the contact", q)
		assert.Equal(t, "José", items[0]["firstname"])
		assert.Equal(t, "García", items[0]["lastname"])
	}
}

// TestGetContactsSearch_NormalizedTermFeedsLIKEArm pins the query-side fold
// at a spot where ONLY the LIKE arm can match, so the fold is load-bearing
// rather than redundant with the FTS tokenizer: the stored value is NFC
// ("josé@example.com") and the term is a mid-token substring of it
// ("osé"). FTS is token-prefix — "os*" never prefix-matches the token
// "jose" — so this resolves purely through `email LIKE '%osé%'`, a byte
// comparison that requires the NFD query to be folded to NFC first.
func TestGetContactsSearch_NormalizedTermFeedsLIKEArm(t *testing.T) {
	db, router, user := ftsRealRouter(t, "i18n-likeonly.db")

	require.NoError(t, db.Create(&models.Contact{
		UserID: user.ID, Firstname: "Ana", Lastname: "Café", Email: "josé@example.com",
	}).Error)

	for _, q := range []string{
		"search=osé",            // NFC term, mid-token substring
		"search=os\u0065\u0301", // NFD term — must be folded to NFC before the LIKE byte comparison
	} {
		items := ftsSearch(t, router, q)
		require.Len(t, items, 1, "query %q must find the contact via the LIKE arm", q)
		assert.Equal(t, "Ana", items[0]["firstname"])
	}
}

// TestGetContactsSearch_DeliberateNonFolds lists the pins that deliberately do
// NOT fold, so the LIKE/FTS behavior matches /search (see
// docs/development/unicode-search.md): German ß is not ss, and Turkish
// dotless ı is not dotted i.
func TestGetContactsSearch_DeliberateNonFolds(t *testing.T) {
	db, router, user := ftsRealRouter(t, "i18n-nofold.db")

	createNamed(t, db, user.ID, "Straße", "Q")
	createNamed(t, db, user.ID, "İstanbul", "R") // Turkish dotted capital I

	for _, q := range []string{"search=strasse", "search=STRASSE", "search=\u0131stanbul"} {
		items := ftsSearch(t, router, q)
		assert.Empty(t, items, "query %q must NOT match (documented non-fold)", q)
	}
	// The positive controls in the same table.
	for _, q := range []string{"search=Straße", "search=istanbul"} {
		items := ftsSearch(t, router, q)
		require.Len(t, items, 1, "query %q must match", q)
	}
}

// TestGetContactsSearch_PreBackfillNFDStillFindable pins that a decomposed
// row that exists only because it predates migration 000052 (direct column
// write, bypassing the ingress normalizer) is still found by an NFC query on
// the list path — search never regresses relative to today while the startup
// backfill is pending.
func TestGetContactsSearch_PreBackfillNFDStillFindable(t *testing.T) {
	db, router, user := ftsRealRouter(t, "i18n-nfdlegacy.db")

	createNamed(t, db, user.ID, "Jos\u0065\u0301", "Lovelace")

	for _, q := range []string{"search=José", "search=Jos\u0065\u0301", "search=jose"} {
		items := ftsSearch(t, router, q)
		require.Len(t, items, 1, "query %q must find the legacy NFD row", q)
		assert.Equal(t, "Jos\u0065\u0301", items[0]["firstname"], "list rows echo the stored (still NFD) bytes until the backfill runs")
	}
}
