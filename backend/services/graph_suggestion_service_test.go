package services

import (
	"testing"

	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// createGraphContact is a thin wrapper over createHouseholdTestContact for the
// graph-suggestion tests (same package helper; humans only here, no kind).
func createGraphContact(t *testing.T, db *gorm.DB, userID uint, firstname string) models.Contact {
	t.Helper()
	return createHouseholdTestContact(t, db, userID, firstname, "")
}

// confirmEdge creates a status: confirmed edge directly, the shape a
// user-confirmed or accepted edge has in production. source/confidence follow
// the user-confirmed convention (Source: user-confirmed, Confidence: 1.0).
func confirmEdge(t *testing.T, db *gorm.DB, userID uint, source, target, edgeType string) {
	t.Helper()
	edge := models.RelationshipEdge{
		UserID:      userID,
		SourceID:    source,
		TargetID:    target,
		Type:        edgeType,
		Directional: !models.IsSymmetricRelationType(edgeType),
		Source:      models.RelationshipSourceUserConfirmed,
		Confidence:  1.0,
		Status:      models.RelationshipStatusConfirmed,
		Sensitivity: models.RelationshipSensitivityNormal,
	}
	require.NoError(t, db.Create(&edge).Error)
}

// acceptAllSuggested flips every suggested edge for the user to confirmed —
// the test's stand-in for the user hitting accept on every inbox item.
func acceptAllSuggested(t *testing.T, db *gorm.DB, userID uint) {
	t.Helper()
	var suggested []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ? AND status = ?", userID, models.RelationshipStatusSuggested).Find(&suggested).Error)
	for _, e := range suggested {
		e.Status = models.RelationshipStatusConfirmed
		require.NoError(t, db.Save(&e).Error)
	}
}

// edgeSet returns the set of (source_id, target_id, type) triples currently
// stored for the user, so tests can assert exact membership without caring
// about order.
func edgeSet(t *testing.T, db *gorm.DB, userID uint) map[string]bool {
	t.Helper()
	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", userID).Find(&edges).Error)
	set := make(map[string]bool, len(edges))
	for _, e := range edges {
		set[e.SourceID+"|"+e.TargetID+"|"+e.Type] = true
	}
	return set
}

// hasSymmetricEdge reports whether the set contains the symmetric fact
// (a, b, edgeType) in EITHER storage direction. Symmetric types (sibling_of,
// spouse_of) are the same relationship either way, and suggestEdgeIfNew
// canonicalizes their stored direction (source_id < target_id), so an
// assertion must not depend on which contact happens to sort first.
func hasSymmetricEdge(set map[string]bool, a, b, edgeType string) bool {
	return set[a+"|"+b+"|"+edgeType] || set[b+"|"+a+"|"+edgeType]
}

// TestGenerateGraphSuggestions_WorkedExampleThreePressSaturation replays the
// ticket's worked example exactly: 2 parents, 4 children, minimal chaining,
// 5 confirmed edges, then three press rounds. Press 1 yields 4 suggestions,
// press 2 yields 5, press 3 yields 1, press 4 yields 0 (saturated at
// 1 spouse + 8 parent + 6 sibling = 15 edges).
func TestGenerateGraphSuggestions_WorkedExampleThreePressSaturation(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	p1 := createGraphContact(t, db, user.ID, "P1")
	p2 := createGraphContact(t, db, user.ID, "P2")
	c1 := createGraphContact(t, db, user.ID, "C1")
	c2 := createGraphContact(t, db, user.ID, "C2")
	c3 := createGraphContact(t, db, user.ID, "C3")
	c4 := createGraphContact(t, db, user.ID, "C4")

	confirmEdge(t, db, user.ID, p1.VCardUID, p2.VCardUID, "spouse_of")
	confirmEdge(t, db, user.ID, p1.VCardUID, c1.VCardUID, "parent_of")
	confirmEdge(t, db, user.ID, c1.VCardUID, c2.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, c2.VCardUID, c3.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, c3.VCardUID, c4.VCardUID, "sibling_of")

	// Press 1.
	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	require.Len(t, created, 4, "press 1: 4 suggestions (R3 + R2 + 2x R1)")
	set := edgeSet(t, db, user.ID)
	assert.True(t, set[p2.VCardUID+"|"+c1.VCardUID+"|parent_of"], "R3 spouse·parent: P2 parent_of C1")
	assert.True(t, set[p1.VCardUID+"|"+c2.VCardUID+"|parent_of"], "R2 parent·sibling: P1 parent_of C2")
	assert.True(t, hasSymmetricEdge(set, c1.VCardUID, c3.VCardUID, "sibling_of"), "R1: C1 sibling_of C3")
	assert.True(t, hasSymmetricEdge(set, c2.VCardUID, c4.VCardUID, "sibling_of"), "R1: C2 sibling_of C4")

	// Every suggestion carries the graph-inferred provenance.
	var suggested []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ? AND status = ?", user.ID, models.RelationshipStatusSuggested).Find(&suggested).Error)
	for _, e := range suggested {
		assert.Equal(t, models.RelationshipSourceGraphInferred, e.Source)
		assert.Equal(t, models.RelationshipSensitivityNormal, e.Sensitivity)
	}

	acceptAllSuggested(t, db, user.ID)

	// Press 2.
	created, err = GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	require.Len(t, created, 5, "press 2: 5 suggestions")
	set = edgeSet(t, db, user.ID)
	assert.True(t, set[p2.VCardUID+"|"+c2.VCardUID+"|parent_of"])
	assert.True(t, set[p2.VCardUID+"|"+c3.VCardUID+"|parent_of"])
	assert.True(t, set[p1.VCardUID+"|"+c3.VCardUID+"|parent_of"])
	assert.True(t, set[p1.VCardUID+"|"+c4.VCardUID+"|parent_of"])
	assert.True(t, hasSymmetricEdge(set, c1.VCardUID, c4.VCardUID, "sibling_of"))

	acceptAllSuggested(t, db, user.ID)

	// Press 3.
	created, err = GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	require.Len(t, created, 1, "press 3: 1 suggestion")
	set = edgeSet(t, db, user.ID)
	assert.True(t, set[p2.VCardUID+"|"+c4.VCardUID+"|parent_of"])

	acceptAllSuggested(t, db, user.ID)

	// Press 4: saturated.
	created, err = GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "press 4: nothing new — 15 edges total")
	assert.EqualValues(t, 15, len(edgeSet(t, db, user.ID)))
}

// TestGenerateGraphSuggestions_InverseStorageDirection pins the inverse
// handling: the same family shape stored with the child-of edge reversed
// (C1 child_of P1 instead of P1 parent_of C1) must still infer
// parent_of(P1, C2). The engine resolves each hop's displayed relation by
// inverting as needed, exactly like graph_traversal.go.
func TestGenerateGraphSuggestions_InverseStorageDirection(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	p1 := createGraphContact(t, db, user.ID, "P1")
	c1 := createGraphContact(t, db, user.ID, "C1")
	c2 := createGraphContact(t, db, user.ID, "C2")

	// Stored as "C1 is child of P1" — the same fact as "P1 is parent of C1".
	confirmEdge(t, db, user.ID, c1.VCardUID, p1.VCardUID, "child_of")
	confirmEdge(t, db, user.ID, c1.VCardUID, c2.VCardUID, "sibling_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	require.Len(t, created, 1)
	assert.Equal(t, p1.VCardUID, created[0].SourceID)
	assert.Equal(t, c2.VCardUID, created[0].TargetID)
	assert.Equal(t, "parent_of", created[0].Type)
}

// TestGenerateGraphSuggestions_NoGrandparent pins the explicit out-of-scope
// parent·parent composition: two parent hops must not infer a grandparent
// token.
func TestGenerateGraphSuggestions_NoGrandparent(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	p := createGraphContact(t, db, user.ID, "Parent")
	c := createGraphContact(t, db, user.ID, "Child")
	g := createGraphContact(t, db, user.ID, "Grandchild")

	confirmEdge(t, db, user.ID, p.VCardUID, c.VCardUID, "parent_of")
	confirmEdge(t, db, user.ID, c.VCardUID, g.VCardUID, "parent_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "parent·parent produces no grandparent token")
}

// TestGenerateGraphSuggestions_NoAuntUncle pins the out-of-scope sibling·parent
// composition.
func TestGenerateGraphSuggestions_NoAuntUncle(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	a := createGraphContact(t, db, user.ID, "Aunt")
	p := createGraphContact(t, db, user.ID, "Parent")
	c := createGraphContact(t, db, user.ID, "Child")

	confirmEdge(t, db, user.ID, a.VCardUID, p.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, p.VCardUID, c.VCardUID, "parent_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "sibling·parent produces no aunt/uncle token")
}

// TestGenerateGraphSuggestions_NoNieceNephew pins the out-of-scope child·sibling
// composition.
func TestGenerateGraphSuggestions_NoNieceNephew(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	c := createGraphContact(t, db, user.ID, "Child")
	g := createGraphContact(t, db, user.ID, "Grandchild")
	s := createGraphContact(t, db, user.ID, "Sibling")

	confirmEdge(t, db, user.ID, c.VCardUID, g.VCardUID, "parent_of")
	confirmEdge(t, db, user.ID, c.VCardUID, s.VCardUID, "sibling_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "child·sibling produces no niece/nephew token")
}

// TestGenerateGraphSuggestions_DuplicateMiddlePaths pins that an inferred
// sibling_of(A,B) reachable through two different middle nodes is created
// exactly once.
func TestGenerateGraphSuggestions_DuplicateMiddlePaths(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	a := createGraphContact(t, db, user.ID, "A")
	b := createGraphContact(t, db, user.ID, "B")
	x := createGraphContact(t, db, user.ID, "X")
	y := createGraphContact(t, db, user.ID, "Y")

	// sibling_of(A,B) is reachable via BOTH x and y as the middle node.
	confirmEdge(t, db, user.ID, a.VCardUID, x.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, x.VCardUID, b.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, a.VCardUID, y.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, y.VCardUID, b.VCardUID, "sibling_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)

	siblingAB := 0
	for _, e := range created {
		if e.Type == "sibling_of" && ((e.SourceID == a.VCardUID && e.TargetID == b.VCardUID) || (e.SourceID == b.VCardUID && e.TargetID == a.VCardUID)) {
			siblingAB++
		}
	}
	assert.Equal(t, 1, siblingAB, "the A-B sibling fact is created once across both middle paths")
	for _, e := range created {
		assert.NotEqual(t, e.SourceID, e.TargetID, "never a self-loop")
	}
}

// TestGenerateGraphSuggestions_NoSelfLoop pins that a 2-cycle (the same pair
// connected twice) must not propose a self-loop edge — the a.uid == b.uid
// guard in the middle-node enumeration.
func TestGenerateGraphSuggestions_NoSelfLoop(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	a := createGraphContact(t, db, user.ID, "A")
	x := createGraphContact(t, db, user.ID, "X")

	confirmEdge(t, db, user.ID, a.VCardUID, x.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, x.VCardUID, a.VCardUID, "sibling_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created)
	for _, e := range created {
		assert.NotEqual(t, e.SourceID, e.TargetID)
	}
}

// TestGenerateGraphSuggestions_SecretEdgesExcluded pins the sensitivity rule:
// a secret edge must not seed a derived suggestion.
func TestGenerateGraphSuggestions_SecretEdgesExcluded(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	p1 := createGraphContact(t, db, user.ID, "P1")
	c1 := createGraphContact(t, db, user.ID, "C1")
	c2 := createGraphContact(t, db, user.ID, "C2")

	// Secret parent edge: excluded from seeding.
	secret := models.RelationshipEdge{
		UserID: user.ID, SourceID: p1.VCardUID, TargetID: c1.VCardUID, Type: "parent_of",
		Directional: true, Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0,
		Status: models.RelationshipStatusConfirmed, Sensitivity: models.RelationshipSensitivitySecret,
	}
	require.NoError(t, db.Create(&secret).Error)
	confirmEdge(t, db, user.ID, c1.VCardUID, c2.VCardUID, "sibling_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "the parent hop is secret, so parent·sibling must not fire")
}

// TestGenerateGraphSuggestions_SuggestedEdgesDoNotSeed pins that a suggested
// (non-confirmed) edge is not fact and must not seed inference — only a
// confirmed edge does.
func TestGenerateGraphSuggestions_SuggestedEdgesDoNotSeed(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	p1 := createGraphContact(t, db, user.ID, "P1")
	c1 := createGraphContact(t, db, user.ID, "C1")
	c2 := createGraphContact(t, db, user.ID, "C2")

	// A suggested parent edge plus a confirmed sibling edge *would* infer
	// p1 parent_of c2 if the suggested edge seeded inference.
	suggested := models.RelationshipEdge{
		UserID: user.ID, SourceID: p1.VCardUID, TargetID: c1.VCardUID, Type: "parent_of",
		Directional: true, Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.8,
		Status: models.RelationshipStatusSuggested, Sensitivity: models.RelationshipSensitivityNormal,
	}
	require.NoError(t, db.Create(&suggested).Error)
	confirmEdge(t, db, user.ID, c1.VCardUID, c2.VCardUID, "sibling_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "only confirmed edges seed inference, not suggested ones")
}

// TestGenerateGraphSuggestions_DoesNotResuggestExisting pins idempotency
// across statuses: a confirmed edge never gets re-suggested, and re-running
// with nothing new returns nothing.
func TestGenerateGraphSuggestions_DoesNotResuggestExisting(t *testing.T) {
	db := setupHouseholdServiceTestDB(t)
	user := createHouseholdTestUser(t, db)
	p1 := createGraphContact(t, db, user.ID, "P1")
	c1 := createGraphContact(t, db, user.ID, "C1")
	c2 := createGraphContact(t, db, user.ID, "C2")

	// The full inference is already present as a confirmed edge.
	confirmEdge(t, db, user.ID, p1.VCardUID, c1.VCardUID, "parent_of")
	confirmEdge(t, db, user.ID, c1.VCardUID, c2.VCardUID, "sibling_of")
	confirmEdge(t, db, user.ID, p1.VCardUID, c2.VCardUID, "parent_of")

	created, err := GenerateGraphSuggestions(db, user.ID)
	require.NoError(t, err)
	assert.Empty(t, created, "the inferred edge already exists as confirmed — never re-suggested")
}
