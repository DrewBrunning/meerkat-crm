package largedata

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"mycorrhizal/database"
	"mycorrhizal/internal/canonicalfixture"
	"mycorrhizal/middleware"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// largeTestsEnabled gates the slow populate-at-scale test the same way
// internal/schemafixture's large tests are gated: MYCORRHIZAL_LARGE_TESTS=1
// (set by the nightly/main-push large-dataset CI job). See
// docs/development/scale-testing.md.
func largeTestsEnabled() bool {
	return os.Getenv("MYCORRHIZAL_LARGE_TESTS") == "1"
}

// readManifest loads the canonical TEST-02 manifest once per test.
func readManifest(t *testing.T) *canonicalfixture.Manifest {
	t.Helper()
	m, err := canonicalfixture.Read()
	require.NoError(t, err)
	return m
}

func TestScaleRoundsUpToWholeBlocks(t *testing.T) {
	m := readManifest(t)
	base := len(m.Contacts)
	require.Equal(t, BlocksOfManifest, base, "the test relies on the canonical manifest being %d contacts", BlocksOfManifest)

	for _, target := range []int{1, base, base + 1, 2 * base, 2000, 100_000} {
		t.Run(fmt.Sprintf("target=%d", target), func(t *testing.T) {
			scaled, err := Scale(m, target)
			require.NoError(t, err)
			want := ((target + base - 1) / base) * base
			assert.Equal(t, want, len(scaled.Contacts), "contact count must be the smallest multiple of %d >= %d", base, target)
			assert.True(t, len(scaled.Contacts) >= target)
		})
	}
}

func TestScaleValidates(t *testing.T) {
	m := readManifest(t)
	scaled, err := Scale(m, 2000)
	require.NoError(t, err)
	require.NoError(t, scaled.Validate(), "a scaled manifest must pass the same cross-reference validation the canonical one does")

	// Every section scaled in lockstep with the contacts: the block multiplier
	// is the same for all of them, so a section that stopped being replicated
	// (a rename bug) shows up as a ratio mismatch.
	blocks := len(scaled.Contacts) / len(m.Contacts)
	for _, section := range []struct {
		name string
		base int
		got  int
	}{
		{"notes", len(m.Notes), len(scaled.Notes)},
		{"life_events", len(m.LifeEvents), len(scaled.LifeEvents)},
		{"gifts", len(m.Gifts), len(scaled.Gifts)},
		{"relationships", len(m.Relationships), len(scaled.Relationships)},
		{"households", len(m.Households), len(scaled.Households)},
		{"circles", len(m.Circles), len(scaled.Circles)},
		{"tags", len(m.Tags), len(scaled.Tags)},
		{"custom_fields", len(m.CustomFields), len(scaled.CustomFields)},
		{"preferences", len(m.Preferences), len(scaled.Preferences)},
		{"external_identities", len(m.ExternalIdentities), len(scaled.ExternalIdentities)},
		{"attachments", len(m.Attachments), len(scaled.Attachments)},
		{"activities", len(m.Activities), len(scaled.Activities)},
	} {
		assert.Equal(t, blocks*section.base, section.got, "%s must scale with the contacts", section.name)
	}
}

func TestScalePreservesPathologicalRecordsAtScale(t *testing.T) {
	m := readManifest(t)
	scaled, err := Scale(m, 2000)
	require.NoError(t, err)

	soft := 0
	recreate := 0
	unique := map[string]bool{}
	for _, c := range scaled.Contacts {
		if c.SoftDeleted {
			soft++
		}
		if c.RecreatesVCardUIDOf != "" {
			recreate++
			assert.True(t, strings.HasSuffix(c.RecreatesVCardUIDOf, c.Name[len(c.Name)-7:]), "julie-type contact must re-key to its own block's gina")
		}
		assert.NotEmpty(t, c.Card.UID)
		unique[c.Card.UID] = true
	}
	blocks := len(scaled.Contacts) / len(m.Contacts)

	// The gina/julie soft-delete + vcard-uid-recreate trap exists per block —
	// pathological records are present at scale, not just plain ones.
	assert.Equal(t, blocks, soft, "one soft-deleted gina per block")
	assert.Equal(t, blocks, recreate, "one julie recreating a block gina's uid per block")
	assert.Equal(t, len(scaled.Contacts), len(unique), "every scaled contact must carry a distinct card UID (partial unique index)")

	// The ~1687-char pathological note exists per block.
	longNotes := 0
	for _, n := range scaled.Notes {
		if len(n.Content) > 1000 {
			longNotes++
		}
	}
	assert.Equal(t, blocks, longNotes, "the very-long note must be present at scale")

	// The duplicate-detection pair (hugo/ida share email+phone) survives per
	// block as a pair of distinctly named contacts.
	hugos, idas := 0, 0
	for _, c := range scaled.Contacts {
		if strings.HasPrefix(c.Name, "hugo_") {
			hugos++
		}
		if strings.HasPrefix(c.Name, "ida_") {
			idas++
		}
	}
	assert.Equal(t, blocks, hugos)
	assert.Equal(t, blocks, idas)
}

func TestScaleIsFaithfulPerBlock(t *testing.T) {
	m := readManifest(t)
	scaled, err := Scale(m, BlocksOfManifest*3) // exactly 3 blocks
	require.NoError(t, err)

	// Compare a single base contact shape against its three replicas: every
	// data field must be identical, and only the name/uid references may
	// differ. Do it on a deep field that the rewriter must NOT touch.
	base := m.Contacts[0] // ada — the richest card
	for b := 0; b < 3; b++ {
		replica := scaled.Contacts[b*len(m.Contacts)]
		assert.Equal(t, fmt.Sprintf("ada_%06d", b), replica.Name)
		assert.Equal(t, base.Card.Kind, replica.Card.Kind)
		assert.Equal(t, base.Card.Language, replica.Card.Language)
		assert.Equal(t, base.Card.Name, replica.Card.Name)
		assert.Equal(t, base.Card.Emails, replica.Card.Emails)
		assert.Equal(t, base.Card.Addresses, replica.Card.Addresses)
		assert.Equal(t, base.Card.SpeakToAs, replica.Card.SpeakToAs)
		assert.Equal(t, base.Card.PersonalInfo, replica.Card.PersonalInfo)
		assert.Equal(t, base.CRM, replica.CRM)
		assert.Equal(t, base.Passthrough, replica.Passthrough)
		// The UID references inside the card were re-mapped to this block's
		// UIDs — the reference target must now be a uid this block owns.
		blockUIDs := map[string]bool{}
		for i := 0; i < len(m.Contacts); i++ {
			blockUIDs[scaled.Contacts[b*len(m.Contacts)+i].Card.UID] = true
		}
		for _, rel := range replica.Card.RelatedTo {
			assert.Contains(t, blockUIDs, strings.TrimPrefix(rel.Target, "urn:uuid:"), "relatedTo target must be re-mapped within the block")
		}
	}
}

func TestScalePopulatesLargeDataset(t *testing.T) {
	// A 2,010-contact populate takes ~50s under -race, so it runs in the
	// nightly/main-push large-dataset CI job (MYCORRHIZAL_LARGE_TESTS=1), not
	// in the default suite every PR pays.
	if !largeTestsEnabled() {
		t.Skip("populate-at-scale runs in the nightly/main-push large-dataset CI job (set MYCORRHIZAL_LARGE_TESTS=1 to run it locally)")
	}

	// The real-migrated-schema requirement (CLAUDE.md trap #1): populate into
	// database.InitDB, never AutoMigrate.
	dbPath := t.TempDir() + "/large.db"
	db, err := database.InitDB(dbPath)
	require.NoError(t, err)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	m := readManifest(t)
	scaled, err := Scale(m, 2000)
	require.NoError(t, err)
	ds, err := canonicalfixture.Populate(db, scaled)
	require.NoError(t, err)
	require.Equal(t, len(scaled.Contacts), len(ds.Contacts))

	// The gina/julie trap at scale: every block has its gina soft-deleted and
	// its julie live, sharing one vcard_uid — the partial unique index must
	// have admitted all of them.
	blocks := len(scaled.Contacts) / len(m.Contacts)
	liveJulie, deletedGina := 0, 0
	for b := 0; b < blocks; b++ {
		gs := fmt.Sprintf("gina_%06d", b)
		js := fmt.Sprintf("julie_%06d", b)
		gina, ok := ds.Contacts[gs]
		require.True(t, ok, "block %d has a gina", b)
		julie, ok := ds.Contacts[js]
		require.True(t, ok, "block %d has a julie", b)
		if !gina.DeletedAt.Valid {
			liveJulie++ // gina must be tombstoned
		}
		if gina.VCardUID != julie.VCardUID {
			deletedGina++ // julie must re-use gina's uid
		}
	}
	assert.Zero(t, liveJulie, "every gina must be soft-deleted")
	assert.Zero(t, deletedGina, "every julie must re-use her block gina's vcard_uid")

	// Row counts land where the manifest ratios predict. Relationship edges
	// are the one table whose count is LESS than the manifest's: each block's
	// soft-deleted gina has her incident edges hard-deleted by the phase-B
	// cascade, exactly as DeleteContact does to a real tombstoned contact.
	ginaEdges := 0
	for _, r := range m.Relationships {
		if r.Source == "gina" || r.Target == "gina" {
			ginaEdges++
		}
	}
	var total int64
	require.NoError(t, db.Table("contacts").Count(&total).Error)
	assert.Equal(t, int64(len(scaled.Contacts)), total)
	require.NoError(t, db.Table("notes").Count(&total).Error)
	assert.Equal(t, int64(len(scaled.Notes)), total)
	require.NoError(t, db.Table("relationship_edges").Count(&total).Error)
	assert.Equal(t, int64(len(scaled.Relationships)-blocks*ginaEdges), total, "each block's gina cascade hard-deletes %d edges", ginaEdges)
	require.NoError(t, db.Table("attachments").Count(&total).Error)
	assert.Equal(t, int64(len(scaled.Attachments)), total)

	// Integrity after a full-scale-style population.
	var integrity string
	require.NoError(t, db.Raw("PRAGMA integrity_check").Scan(&integrity).Error)
	assert.Equal(t, "ok", integrity)
}

func TestScaleRejectsBadInputs(t *testing.T) {
	_, err := Scale(nil, 100)
	assert.Error(t, err)
	m := readManifest(t)
	_, err = Scale(m, 0)
	assert.Error(t, err)
	_, err = Scale(m, -5)
	assert.Error(t, err)

	empty := &canonicalfixture.Manifest{
		Version: canonicalfixture.ManifestVersion,
		User:    canonicalfixture.ManifestUser{Username: "u", Email: "u@example.com"},
	}
	_, err = Scale(empty, 100)
	assert.Error(t, err, "a manifest with no contacts must be refused")
}

// TestRegeneratedUIDsSatisfyUUID4Validation is the issue #868 regression
// test: every card UID largedata generates must pass the exact `uuid4` tag
// go-playground/validator evaluates for RelationshipEdgeInput.SourceID/
// TargetID, CircleMemberInput/HouseholdMemberInput.MemberVCardUID,
// ContactTagInput.ContactVCardUID, ContactShareInput.VCardUID, and
// BulkContactOperationInput.VCardUIDs — not a hand-rolled regex, so this
// cannot drift from what the API actually enforces. Before #868,
// regeneratedUID produced a real RFC 4122 v5 UUID (third group starting
// "5"), which uuid4 rejects; a seeded contact's vcard_uid then failed
// validation before ever reaching ownership-resolution code.
func TestRegeneratedUIDsSatisfyUUID4Validation(t *testing.T) {
	m := readManifest(t)
	scaled, err := Scale(m, 2000)
	require.NoError(t, err)
	require.NotEmpty(t, scaled.Contacts)

	for _, c := range scaled.Contacts {
		require.NotEmpty(t, c.Card.UID, "contact %q must carry a card UID", c.Name)
		assert.True(t, middleware.ValidateVar(c.Card.UID, "uuid4"),
			"contact %q's card UID %q must satisfy the uuid4 validator DTOs use", c.Name, c.Card.UID)
	}

	// Also cover the salted derivation path (the PERF-01 per-user namespacing),
	// which shares regeneratedUID with Scale.
	salted, err := scaleSalted(m, 2000, "perf-profile")
	require.NoError(t, err)
	for _, c := range salted.Contacts {
		assert.True(t, middleware.ValidateVar(c.Card.UID, "uuid4"),
			"salted contact %q's card UID %q must satisfy the uuid4 validator DTOs use", c.Name, c.Card.UID)
	}
}

// TestRewriterURIRemapping pins the card-level UID re-keying directly: a
// relatedTo/members reference that points at a manifest contact is remapped to
// the block's regenerated UID (both the urn:uuid: and bare-UID forms), while a
// reference to a foreign UID passes through untouched — a scaled dataset must
// never rewrite a reference it does not own.
func TestRewriterURIRemapping(t *testing.T) {
	m := readManifest(t)
	rw := newRewriter(m, 0)

	require.Contains(t, rw.uidOf, "ada", "the rewriter must know every base contact's block UID")
	blockADA := rw.uidOf["ada"]
	baseADA := ""
	for _, c := range m.Contacts {
		if c.Name == "ada" {
			baseADA = c.Card.UID
		}
	}
	require.NotEmpty(t, baseADA, "ada carries a card UID in the manifest")

	assert.Equal(t, "urn:uuid:"+blockADA, rw.uri("urn:uuid:"+baseADA), "urn:uuid reference to a base contact remaps")
	assert.Equal(t, blockADA, rw.uri(baseADA), "bare-UID reference to a base contact remaps")

	foreign := "99999999-0000-4000-8000-000000000099"
	assert.Equal(t, "urn:uuid:"+foreign, rw.uri("urn:uuid:"+foreign), "urn:uuid reference to a foreign UID is left alone")
	assert.Equal(t, foreign, rw.uri(foreign), "bare reference to a foreign UID is left alone")
	assert.Equal(t, "", rw.uri(""), "empty references pass through")

	_, ok := rw.remapUID(foreign)
	assert.False(t, ok, "a foreign UID is not owned by the block")
}
