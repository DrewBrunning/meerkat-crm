package canonicalfixture

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"mycorrhizal/contactmodel"
	"mycorrhizal/middleware"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// i18nContactNames are the I18N-01 international fixture records (issue #484).
// Each is load-bearing: deleting any one of them must fail a suite, exactly
// like the trap records — these tests assert their presence by name and their
// distinctive conventions field by field, and the generic
// TestRoundTripReproducesEveryDeclaredField plus the TEST-03/TEST-08 consumers
// flow every declared field through the DB and the three serialized formats.
var i18nContactNames = []string{
	"naoki",   // Japanese, Latin phonetic readings, Japanese address
	"wei",     // Chinese, pinyin phonetic readings
	"minjun",  // Korean (Hangul), romanized phonetic readings
	"layla",   // Arabic (RTL), address with no postal code
	"yael",    // Hebrew (RTL)
	"bjork",   // Icelandic patronymic, farm address with no street/postcode
	"carmen",  // Spanish compound surname
	"joao",    // Portuguese compound surname + two given names
	"jan",     // Dutch tussenvoegsel
	"somchai", // Thai, no surname recorded
	"priya",   // Devanagari combining marks, ZWJ emoji
	"aoife",   // Irish (Gaelic fada), Irish Eircode address
}

// sortExpectations pins the derived contacts.sort_name key for each convention
// (issue #484): the key is lower(trim(surname)) — from the dedicated surname
// component — else lower(trim(given)) for a contact with no surname, and is
// NEVER inferred from the display name's token order.
var sortExpectations = map[string]string{
	"naoki":   "佐藤",
	"wei":     "王",
	"minjun":  "김",
	"layla":   "حسن",
	"yael":    "כהן",
	"bjork":   "guðmundsdóttir",
	"carmen":  "garcía",
	"joao":    "costa",
	"jan":     "van der berg",
	"somchai": "สมชาย",
	"priya":   "शर्मा",
	"aoife":   "ó conaill",
}

// TestI18NManifestDeclaresInternationalRecords pins that the international
// records exist in the manifest (the presence net — deleting one fails here
// even though the per-contact round-trip test cannot tell a deletion from a
// renumbering). None may be soft-deleted: they must flow through the live
// migration/interop/search/export paths.
func TestI18NManifestDeclaresInternationalRecords(t *testing.T) {
	m := readManifest(t)
	byName := map[string]ContactEntry{}
	for _, c := range m.Contacts {
		byName[c.Name] = c
	}
	for _, name := range i18nContactNames {
		entry, ok := byName[name]
		require.Truef(t, ok, "manifest must declare the international record %q (I18N-01, issue #484)", name)
		assert.False(t, entry.SoftDeleted, "%s: international records must be live, not tombstoned", name)
		require.NotNil(t, entry.Card.Name, "%s: an international record must carry a structured name", name)
		require.NotEmpty(t, entry.Card.Name.Components, "%s: a structured name must have components", name)
	}
}

// TestI18NRecordsSurviveToTheDatabase is the DB-level net for the international
// conventions: every declared name/note/address must come back byte-identical
// through Record -> Contact -> real migrated DB -> reload -> Record, with no
// replacement characters, no byte truncation, and no normalization loss.
func TestI18NRecordsSurviveToTheDatabase(t *testing.T) {
	m, ds, db := populatedDB(t)
	byName := map[string]ContactEntry{}
	for _, c := range m.Contacts {
		byName[c.Name] = c
	}

	for _, name := range i18nContactNames {
		entry := byName[name]
		t.Run("record_"+name, func(t *testing.T) {
			c := reloadContact(t, db, ds.Contacts[name])
			got := models.RecordForContact(&c, "", nil)
			require.NotNil(t, got)

			// No mojibake anywhere in the record's JSON.
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "\uFFFD", "%s: replacement character leaked into the stored record", name)

			// The full name and every component survive; the manifest's
			// declared components are the expectation (byte-exact).
			want := entry.Card.Name
			require.NotNil(t, got.Card.Name)
			assert.Equal(t, want.Full, got.Card.Name.Full, "%s: display name mangled", name)
			assert.Equal(t, want.Components, got.Card.Name.Components, "%s: name components mangled", name)

			// Multi-byte reality check: non-ASCII full names must be genuinely
			// multi-byte (more bytes than runes), so a length limit that
			// counts bytes instead of characters would truncate them. The one
			// ASCII-only record (jan) is exempt — the Devanagari tripwire
			// record carries the assertion that matters.
			if strings.ContainsFunc(got.Card.Name.Full, func(r rune) bool { return r > utf8.RuneSelf }) {
				bytes := len([]byte(got.Card.Name.Full))
				runes := utf8.RuneCountInString(got.Card.Name.Full)
				assert.Greater(t, bytes, runes, "%s: full name should be multi-byte to exercise byte-vs-rune truncation", name)
			}
		})
	}
}

// TestI18NConventionsByName asserts each record's distinctive convention after
// the real-DB round trip, so a regression in a single convention fails naming
// the record rather than hiding behind the generic equality check.
func TestI18NConventionsByName(t *testing.T) {
	_, ds, db := populatedDB(t)
	read := func(name string) *contactmodel.Record {
		c := reloadContact(t, db, ds.Contacts[name])
		return models.RecordForContact(&c, "", nil)
	}
	addr := func(rec *contactmodel.Record) []contactmodel.AddressComponent {
		require.Len(t, rec.Card.Addresses, 1)
		return rec.Card.Addresses[0].Components
	}

	t.Run("japanese_phonetic_components", func(t *testing.T) {
		rec := read("naoki")
		assert.Equal(t, "直樹", rec.Card.Name.Components[0].Value)
		assert.Equal(t, "Naoki", rec.Card.Name.Components[0].Phonetic)
		assert.Equal(t, "佐藤", rec.Card.Name.Components[1].Value)
		assert.Equal(t, "Satō", rec.Card.Name.Components[1].Phonetic)
		comp := addr(rec)
		assert.Contains(t, comp, contactmodel.AddressComponent{Kind: "region", Value: "東京都"})
		assert.Contains(t, comp, contactmodel.AddressComponent{Kind: "locality", Value: "渋谷区"})
	})

	t.Run("chinese_pinyin_phonetic", func(t *testing.T) {
		rec := read("wei")
		assert.Equal(t, "王芳", rec.Card.Name.Full)
		assert.Equal(t, "Wáng", rec.Card.Name.Components[1].Phonetic)
	})

	t.Run("korean_hangul_phonetic", func(t *testing.T) {
		rec := read("minjun")
		assert.Equal(t, "김민준", rec.Card.Name.Full)
		assert.Equal(t, "Minjun", rec.Card.Name.Components[0].Phonetic)
		assert.Equal(t, "Kim", rec.Card.Name.Components[1].Phonetic)
	})

	t.Run("rtl_arabic", func(t *testing.T) {
		rec := read("layla")
		assert.Equal(t, "ليلى حسن", rec.Card.Name.Full)
		require.Len(t, rec.Card.Notes, 1)
		// The note mixes RTL Arabic with an embedded LTR phrase; both must
		// survive. The bidi characters are the point.
		assert.Contains(t, rec.Card.Notes[0].Note, "(6 pm)")
	})

	t.Run("rtl_hebrew", func(t *testing.T) {
		rec := read("yael")
		assert.Equal(t, "יעל כהן", rec.Card.Name.Full)
		assert.Contains(t, rec.Card.Notes[0].Note, "תל אביב")
	})

	t.Run("icelandic_patronymic_and_streetless_address", func(t *testing.T) {
		rec := read("bjork")
		assert.Equal(t, "Guðmundsdóttir", rec.Card.Name.Components[1].Value)
		comp := addr(rec)
		// Farm address: locality + region + country only — no street-name and
		// no postcode, which the ADR must survive without.
		vals := map[string]string{}
		for _, c := range comp {
			vals[c.Kind] = c.Value
		}
		assert.NotContains(t, vals, "postcode")
		assert.NotContains(t, vals, "name")
		assert.Equal(t, "Hrunamannahreppur", vals["locality"])
		assert.Equal(t, "Suðurland", vals["region"])
	})

	t.Run("spanish_compound_surname", func(t *testing.T) {
		rec := read("carmen")
		assert.Equal(t, []contactmodel.NameComponent{
			{Kind: "given", Value: "Carmen"},
			{Kind: "surname", Value: "García"},
			{Kind: "surname2", Value: "Rodríguez"},
		}, rec.Card.Name.Components)
	})

	t.Run("portuguese_compound_surname", func(t *testing.T) {
		rec := read("joao")
		assert.Equal(t, []contactmodel.NameComponent{
			{Kind: "given", Value: "João"},
			{Kind: "given2", Value: "Pedro"},
			{Kind: "surname", Value: "Costa"},
			{Kind: "surname2", Value: "Martins"},
		}, rec.Card.Name.Components)
	})

	t.Run("dutch_tussenvoegsel", func(t *testing.T) {
		rec := read("jan")
		require.NotNil(t, rec.Card.Name)
		assert.Equal(t, "van der Berg", rec.Card.Name.Components[1].Value)
		// The person's own email collapses the tussenvoegsel; the surname
		// component does not.
		assert.Equal(t, "Jan van der Berg", rec.Card.Name.Full)
	})

	t.Run("thai_no_surname", func(t *testing.T) {
		rec := read("somchai")
		kinds := []string{}
		for _, c := range rec.Card.Name.Components {
			kinds = append(kinds, c.Kind)
		}
		assert.NotContains(t, kinds, "surname", "somchai is filed under a given name with no surname component")
		comp := addr(rec)
		vals := map[string]string{}
		for _, c := range comp {
			vals[c.Kind] = c.Value
		}
		assert.Equal(t, "วัฒนา", vals["district"])
		assert.Equal(t, "คลองเตยเหนือ", vals["subdistrict"])
	})

	t.Run("devanagari_combining_marks_and_zwj", func(t *testing.T) {
		rec := read("priya")
		// Devanagari is dense with combining marks (vowel signs, virama) —
		// a byte-counting length limit truncates it, a rune-counting one does
		// not. Assert the record really carries that shape.
		full := rec.Card.Name.Full
		marks := 0
		for _, r := range full {
			if unicode.Is(unicode.Mn, r) {
				marks++
			}
		}
		assert.Greater(t, marks, 0, "priya's name must carry combining marks (the byte-vs-rune tripwire)")
		assert.Less(t, utf8.RuneCountInString(full), len([]byte(full)), "priya's name must be multi-byte")
		// The zero-width-joiner family emoji survives the note byte-exact.
		assert.Contains(t, rec.Card.Notes[0].Note, "👨‍👩‍👧‍👦")
		assert.Contains(t, rec.Card.Nicknames[0].Name, "🐘")
	})

	t.Run("irish_eircode_address", func(t *testing.T) {
		rec := read("aoife")
		comp := addr(rec)
		vals := map[string]string{}
		for _, c := range comp {
			vals[c.Kind] = c.Value
		}
		assert.Equal(t, "D02 X285", vals["postcode"], "the Irish Eircode is an alphanumeric postcode shape")
	})
}

// TestI18NPhoneValidatorAcceptsEveryDeclaredNumber is the issue #484 phone
// surface: every phone number the manifest declares — international numbers in
// several national formats plus E.164, RTL-country numbers, mixed national
// landline conventions — must pass the same `phone` validator the REST API
// applies to flat contact phones. A validator that rejects a valid Indian or
// German number is exactly the bug this fixture exists to surface.
func TestI18NPhoneValidatorAcceptsEveryDeclaredNumber(t *testing.T) {
	m := readManifest(t)
	count := 0
	for _, c := range m.Contacts {
		for _, p := range c.Card.Phones {
			count++
			assert.Truef(t, middleware.ValidateVar(p.Number, "phone"),
				"phone %q on contact %q must be accepted by the `phone` validator (a valid international number rejected is a bug, issue #484)", p.Number, c.Name)
		}
	}
	assert.GreaterOrEqual(t, count, 25, "the manifest should carry a broad phone corpus for the validator to guard")

	// The validator has teeth: a too-short number is rejected, so the loop
	// above is not asserting a validator that accepts everything.
	assert.False(t, middleware.ValidateVar("12", "phone"), "the phone validator must still reject a too-short number")
}

// TestI18NSortNameDrivesInternationalOrdering pins issue #484's sort-order
// decision: contacts.sort_name is the dedicated key — lower(trim(surname))
// from the structured surname component, else lower(trim(given)) — and is
// never inferred from the display name. Each convention's expected key is in
// sortExpectations above; the persisted column must match it, and where the
// display order would mislead (family name not leading, surname that must not
// sort under a display token), the key must differ from the display's first
// token.
func TestI18NSortNameDrivesInternationalOrdering(t *testing.T) {
	m, ds, _ := populatedDB(t)
	byName := map[string]ContactEntry{}
	for _, c := range m.Contacts {
		byName[c.Name] = c
	}

	for _, name := range i18nContactNames {
		entry := byName[name]
		c := ds.Contacts[name]
		want, ok := sortExpectations[name]
		require.Truef(t, ok, "sortExpectations must cover every international record (%s)", name)
		assert.Equalf(t, want, c.SortName,
			"%s: contacts.sort_name must be derived from the surname (or given when there is none), not from the display name", name)

		// Projection check: DeriveProjection picks the structured given and
		// surname, so DeriveSortName over it reproduces the persisted key —
		// the derivation that drives the name-sorted list.
		proj := contactmodel.DeriveProjection(entry.Record())
		derived := models.DeriveSortName(proj.Lastname, proj.Firstname)
		assert.Equal(t, want, derived, "%s: the projection's given/surname must derive the same sort key the column holds", name)

		// Locale-specific assertion: for conventions where the family name is
		// not the display's first token, the sort key must NOT be the display
		// first token — that is the "sorted by the dedicated field, not the
		// display name" guarantee.
		if full := entry.Card.Name.Full; full != "" {
			first := strings.ToLower(strings.Fields(full)[0])
			surname := ""
			for _, comp := range entry.Card.Name.Components {
				if comp.Kind == "surname" {
					surname = comp.Value
					break
				}
			}
			if surname != "" && !strings.HasPrefix(full, surname) {
				assert.NotEqual(t, first, want, "%s: sort key must not be inferred from the display name's first token", name)
			}
		}
	}
}

// TestI18NByteTruncationWouldBreakTheTripwire demonstrates that the fixture's
// byte-vs-rune tripwire has teeth: a naive byte-count truncation of priya's
// multi-byte name lands mid-rune (producing invalid UTF-8), which is exactly
// the failure a length limit that counts bytes instead of characters would
// ship. This is the assertion form of CLAUDE.md's "add a length limit that
// counts bytes and confirm a test fails on a multi-byte name" hand-verify.
func TestI18NByteTruncationWouldBreakTheTripwire(t *testing.T) {
	m := readManifest(t)
	full := ""
	for _, c := range m.Contacts {
		if c.Name == "priya" {
			full = c.Card.Name.Full
		}
	}
	require.NotEmpty(t, full, "priya must be in the manifest")
	require.False(t, utf8.ValidString(string(full[:1])), "a byte-count truncation of a multi-byte name must split a rune")
}
