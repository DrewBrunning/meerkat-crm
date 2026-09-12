package services

import (
	"testing"
	"time"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// N1's merge is destructive and irreversible: it moves every association off
// the loser and then deletes it. The pure resolution/union helpers were
// already covered (contact_merge_service_test.go), but the DB-backed half —
// RepointContactAssociations, ComputeContactMergeAssociationCounts,
// ComputeFieldValueConflicts — had none, despite being where the data actually
// moves.
//
// Real migrated schema throughout: the repoint runs raw SQL naming
// member_vcard_uid / contact_vcard_uid / activity_contacts directly, so an
// AutoMigrate-backed test could pass against column names that do not exist in
// the real database.

func newMergeDB(t *testing.T) (*gorm.DB, uint) {
	t.Helper()

	db := dbtest.New(t)

	user := models.User{Username: "merge-user", Email: "merge@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	return db, user.ID
}

func makeMergeContact(t *testing.T, db *gorm.DB, userID uint, name string) *models.Contact {
	t.Helper()
	c := models.Contact{UserID: userID, Firstname: name}
	require.NoError(t, db.Create(&c).Error)
	return &c
}

func TestRepointContactAssociations_MovesNotesRemindersAndActivities(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	require.NoError(t, db.Create(&models.Note{UserID: userID, ContactID: &loser.ID, Content: "loser note"}).Error)
	require.NoError(t, db.Create(&models.Reminder{
		UserID: userID, ContactID: &loser.ID, Message: "call them", RemindAt: time.Now().AddDate(0, 0, 3),
	}).Error)

	activity := models.Activity{UserID: userID, Title: "Dinner", Date: time.Now()}
	require.NoError(t, db.Create(&activity).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO activity_contacts (activity_id, contact_id) VALUES (?, ?)", activity.ID, loser.ID).Error)

	_, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)

	var noteCount, reminderCount, linkCount int64
	require.NoError(t, db.Model(&models.Note{}).Where("contact_id = ?", keeper.ID).Count(&noteCount).Error)
	require.NoError(t, db.Model(&models.Reminder{}).Where("contact_id = ?", keeper.ID).Count(&reminderCount).Error)
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM activity_contacts WHERE contact_id = ?", keeper.ID).Scan(&linkCount).Error)

	assert.Equal(t, int64(1), noteCount, "the loser's note must move to the keeper")
	assert.Equal(t, int64(1), reminderCount, "the loser's reminder must move to the keeper")
	assert.Equal(t, int64(1), linkCount, "the loser's activity link must move to the keeper")

	// Nothing may be left pointing at the loser.
	var orphanNotes, orphanLinks int64
	require.NoError(t, db.Model(&models.Note{}).Where("contact_id = ?", loser.ID).Count(&orphanNotes).Error)
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM activity_contacts WHERE contact_id = ?", loser.ID).Scan(&orphanLinks).Error)
	assert.Zero(t, orphanNotes)
	assert.Zero(t, orphanLinks)
}

// The dedup-then-repoint shape exists because activity_contacts has a
// composite PK: if both contacts are on the same activity, blindly repointing
// the loser's row violates it and the whole merge transaction rolls back.
func TestRepointContactAssociations_DedupsWhenBothOnSameActivity(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	activity := models.Activity{UserID: userID, Title: "Group dinner", Date: time.Now()}
	require.NoError(t, db.Create(&activity).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO activity_contacts (activity_id, contact_id) VALUES (?, ?)", activity.ID, keeper.ID).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO activity_contacts (activity_id, contact_id) VALUES (?, ?)", activity.ID, loser.ID).Error)

	_, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err, "both contacts on one activity must not violate the composite PK")

	var linkCount int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM activity_contacts WHERE activity_id = ?", activity.ID).Scan(&linkCount).Error)
	assert.Equal(t, int64(1), linkCount, "the duplicate link must collapse to one, not error or double up")
}

// Same hazard for the (container, member_vcard_uid) unique indexes.
func TestRepointContactAssociations_DedupsSharedCircleAndTagMemberships(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	circle := models.Circle{UserID: userID, Name: "Family"}
	require.NoError(t, db.Create(&circle).Error)
	require.NoError(t, db.Create(&models.CircleMember{CircleID: circle.ID, UserID: userID, MemberVCardUID: keeper.VCardUID}).Error)
	require.NoError(t, db.Create(&models.CircleMember{CircleID: circle.ID, UserID: userID, MemberVCardUID: loser.VCardUID}).Error)

	soloCircle := models.Circle{UserID: userID, Name: "Climbing"}
	require.NoError(t, db.Create(&soloCircle).Error)
	require.NoError(t, db.Create(&models.CircleMember{CircleID: soloCircle.ID, UserID: userID, MemberVCardUID: loser.VCardUID}).Error)

	tag := models.Tag{UserID: userID, Name: "vip"}
	require.NoError(t, db.Create(&tag).Error)
	require.NoError(t, db.Create(&models.ContactTag{TagID: tag.ID, UserID: userID, ContactVCardUID: keeper.VCardUID}).Error)
	require.NoError(t, db.Create(&models.ContactTag{TagID: tag.ID, UserID: userID, ContactVCardUID: loser.VCardUID}).Error)

	_, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)

	names, err := CircleNamesForContact(db, userID, keeper.VCardUID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Climbing", "Family"}, names,
		"the shared circle must not duplicate, and the loser's own circle must transfer")

	var tagCount int64
	require.NoError(t, db.Model(&models.ContactTag{}).
		Where("contact_vcard_uid = ?", keeper.VCardUID).Count(&tagCount).Error)
	assert.Equal(t, int64(1), tagCount, "the shared tag must collapse to one row")

	var loserRows int64
	require.NoError(t, db.Model(&models.CircleMember{}).
		Where("member_vcard_uid = ?", loser.VCardUID).Count(&loserRows).Error)
	assert.Zero(t, loserRows, "no membership may still reference the merged-away contact")
}

func TestComputeContactMergeAssociationCounts_CountsWhatWillMove(t *testing.T) {
	db, userID := newMergeDB(t)
	loser := makeMergeContact(t, db, userID, "Loser")

	require.NoError(t, db.Create(&models.Note{UserID: userID, ContactID: &loser.ID, Content: "n1"}).Error)
	require.NoError(t, db.Create(&models.Note{UserID: userID, ContactID: &loser.ID, Content: "n2"}).Error)
	require.NoError(t, db.Create(&models.Reminder{
		UserID: userID, ContactID: &loser.ID, Message: "r1", RemindAt: time.Now().AddDate(0, 0, 1),
	}).Error)

	circle := models.Circle{UserID: userID, Name: "Family"}
	require.NoError(t, db.Create(&circle).Error)
	require.NoError(t, db.Create(&models.CircleMember{CircleID: circle.ID, UserID: userID, MemberVCardUID: loser.VCardUID}).Error)

	counts, err := ComputeContactMergeAssociationCounts(db, userID, loser.ID, loser.VCardUID)
	require.NoError(t, err)

	assert.Equal(t, int64(2), counts.Notes)
	assert.Equal(t, int64(1), counts.Reminders)
	assert.Equal(t, int64(1), counts.CircleMemberships)
}

// The counts drive the preview the user confirms against, so they must not
// count another user's rows.
func TestComputeContactMergeAssociationCounts_ScopedToOwner(t *testing.T) {
	db, userID := newMergeDB(t)
	other := models.User{Username: "other-merge", Email: "other-merge@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)

	loser := makeMergeContact(t, db, userID, "Loser")
	require.NoError(t, db.Create(&models.Note{UserID: other.ID, ContactID: &loser.ID, Content: "not yours"}).Error)

	counts, err := ComputeContactMergeAssociationCounts(db, userID, loser.ID, loser.VCardUID)
	require.NoError(t, err)
	assert.Zero(t, counts.Notes, "another user's note must not be counted")
}

// --- ComputeFieldValueConflicts -----------------------------------------------

func makeFieldDef(t *testing.T, db *gorm.DB, userID uint, key, label string) *models.FieldDefinition {
	t.Helper()
	def := models.FieldDefinition{
		UserID: userID, Key: key, Label: label, Target: "contact", Type: "string",
	}
	require.NoError(t, db.Create(&def).Error)
	return &def
}

// A custom field both contacts have set, with different values, is the only
// case that needs the user to choose — the merge cannot guess.
func TestComputeFieldValueConflicts_ReportsOnlyOverlappingDefinitions(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	shared := makeFieldDef(t, db, userID, "shoe_size", "Shoe Size")
	loserOnly := makeFieldDef(t, db, userID, "allergy", "Allergy")

	require.NoError(t, db.Create(&models.FieldValue{
		FieldDefinitionID: shared.ID, UserID: userID, EntityID: keeper.VCardUID, Value: []byte(`"42"`),
	}).Error)
	require.NoError(t, db.Create(&models.FieldValue{
		FieldDefinitionID: shared.ID, UserID: userID, EntityID: loser.VCardUID, Value: []byte(`"43"`),
	}).Error)
	// Only the loser has this one — nothing to resolve, it just transfers.
	require.NoError(t, db.Create(&models.FieldValue{
		FieldDefinitionID: loserOnly.ID, UserID: userID, EntityID: loser.VCardUID, Value: []byte(`"peanuts"`),
	}).Error)

	conflicts, err := ComputeFieldValueConflicts(db, userID, keeper.VCardUID, loser.VCardUID)
	require.NoError(t, err)

	require.Len(t, conflicts, 1, "only a field BOTH contacts set is a conflict")
	assert.Equal(t, shared.ID, conflicts[0].Field)
	assert.Equal(t, "Shoe Size", conflicts[0].Label)
	assert.Equal(t, `"42"`, conflicts[0].KeeperValue)
	assert.Equal(t, `"43"`, conflicts[0].LoserValue)
}

func TestComputeFieldValueConflicts_NoConflictWhenOneSideHasNoValues(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	def := makeFieldDef(t, db, userID, "shoe_size", "Shoe Size")
	require.NoError(t, db.Create(&models.FieldValue{
		FieldDefinitionID: def.ID, UserID: userID, EntityID: keeper.VCardUID, Value: []byte(`"42"`),
	}).Error)

	conflicts, err := ComputeFieldValueConflicts(db, userID, keeper.VCardUID, loser.VCardUID)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestComputeFieldValueConflicts_ScopedToOwner(t *testing.T) {
	db, userID := newMergeDB(t)
	other := models.User{Username: "other-fv", Email: "other-fv@example.com", Password: "x"}
	require.NoError(t, db.Create(&other).Error)

	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")
	def := makeFieldDef(t, db, userID, "shoe_size", "Shoe Size")

	require.NoError(t, db.Create(&models.FieldValue{
		FieldDefinitionID: def.ID, UserID: userID, EntityID: keeper.VCardUID, Value: []byte(`"42"`),
	}).Error)
	// The loser's value belongs to a different user — invisible to this merge.
	require.NoError(t, db.Create(&models.FieldValue{
		FieldDefinitionID: def.ID, UserID: other.ID, EntityID: loser.VCardUID, Value: []byte(`"43"`),
	}).Error)

	conflicts, err := ComputeFieldValueConflicts(db, userID, keeper.VCardUID, loser.VCardUID)
	require.NoError(t, err)
	assert.Empty(t, conflicts, "another user's field value must not become a conflict")
}

// --- relationship edge repointing ---------------------------------------------

// The edge half of the repoint is the subtlest: moving the loser's edges onto
// the keeper can create a self-loop (an edge between keeper and loser) or an
// exact/inverse duplicate of one the keeper already has.
func TestRepointContactAssociations_DropsSelfLoopBetweenKeeperAndLoser(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	// The two contacts being merged were recorded as related to each other —
	// after the merge that edge would point at itself.
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: userID, SourceID: keeper.VCardUID, TargetID: loser.VCardUID,
		Type: "friend_of", Source: models.RelationshipSourceUserConfirmed,
		Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
		Sensitivity: models.RelationshipSensitivityNormal,
	}).Error)

	dropped, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, 1, dropped, "the self-loop must be reported as dropped")

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", userID).Find(&edges).Error)
	assert.Empty(t, edges, "a self-referential edge must not survive the merge")
}

func TestRepointContactAssociations_MovesEdgesToKeeper(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")
	third := makeMergeContact(t, db, userID, "Third")

	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: userID, SourceID: loser.VCardUID, TargetID: third.VCardUID,
		Type: "parent_of", Source: models.RelationshipSourceUserConfirmed,
		Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
		Sensitivity: models.RelationshipSensitivityNormal,
	}).Error)

	_, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", userID).Find(&edges).Error)
	require.Len(t, edges, 1)
	assert.Equal(t, keeper.VCardUID, edges[0].SourceID, "the edge must now originate from the keeper")
	assert.Equal(t, third.VCardUID, edges[0].TargetID)
	assert.Equal(t, "parent_of", edges[0].Type)
}

// Both contacts recorded the same relationship to the same third party. After
// repointing they would be exact duplicates, so one must be dropped.
func TestRepointContactAssociations_DropsDuplicateEdgeAfterRepoint(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")
	third := makeMergeContact(t, db, userID, "Third")

	for _, sourceUID := range []string{keeper.VCardUID, loser.VCardUID} {
		require.NoError(t, db.Create(&models.RelationshipEdge{
			UserID: userID, SourceID: sourceUID, TargetID: third.VCardUID,
			Type: "friend_of", Source: models.RelationshipSourceUserConfirmed,
			Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
			Sensitivity: models.RelationshipSensitivityNormal,
		}).Error)
	}

	dropped, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, 1, dropped)

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", userID).Find(&edges).Error)
	require.Len(t, edges, 1, "the duplicated fact must collapse to a single edge")
	assert.Equal(t, keeper.VCardUID, edges[0].SourceID)
}

// The natural-key unique index (migration 000055) makes the old
// UPDATE-then-dedup order fail: the bulk repoint would land the loser's edge
// on a tuple the keeper already holds before the duplicate was removed. This
// pins the recomputed order — the duplicate is resolved in Go first, and the
// more authoritative (loser's confirmed) edge wins and is repointed.
func TestRepointContactAssociations_MoreAuthoritativeLoserEdgeSurvives(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")
	third := makeMergeContact(t, db, userID, "Third")

	// The keeper carries only a low-confidence household suggestion; the loser
	// carries the user-confirmed fact. It is one (keeper, third, friend_of)
	// tuple after the merge, and the confirmed row must survive.
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: userID, SourceID: keeper.VCardUID, TargetID: third.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.6, Status: models.RelationshipStatusSuggested,
	}).Error)
	loserEdge := models.RelationshipEdge{
		UserID: userID, SourceID: loser.VCardUID, TargetID: third.VCardUID, Type: "friend_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&loserEdge).Error)

	dropped, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, 1, dropped)

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", userID).Find(&edges).Error)
	require.Len(t, edges, 1, "the duplicated fact must collapse to a single edge")
	assert.Equal(t, loserEdge.ID, edges[0].ID, "the more authoritative loser edge's row must survive")
	assert.Equal(t, keeper.VCardUID, edges[0].SourceID, "the survivor must be repointed onto the keeper")
	assert.Equal(t, models.RelationshipStatusConfirmed, edges[0].Status)
	assert.Equal(t, 1.0, edges[0].Confidence)
}

// A mutual-inverse pair (same fact stored two ways) is still deduplicated by
// the recomputed repoint, even though the unique index only constrains exact
// tuples.
func TestRepointContactAssociations_DropsInverseDuplicate(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")
	third := makeMergeContact(t, db, userID, "Third")

	// "third is keeper's parent" and, after repointing, "keeper is third's
	// child" — the inverse of the same fact.
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: userID, SourceID: third.VCardUID, TargetID: keeper.VCardUID, Type: "parent_of",
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
	}).Error)
	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: userID, SourceID: loser.VCardUID, TargetID: third.VCardUID, Type: "child_of",
		Source: models.RelationshipSourceHouseholdInferred, Confidence: 0.6, Status: models.RelationshipStatusSuggested,
	}).Error)

	dropped, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, 1, dropped)

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", userID).Find(&edges).Error)
	require.Len(t, edges, 1, "a mutual-inverse pair is one fact and must collapse")
	assert.Equal(t, "parent_of", edges[0].Type)
}

func TestEdgesConflict_ExactAndInverseDuplicates(t *testing.T) {
	a := models.RelationshipEdge{SourceID: "uid-a", TargetID: "uid-b", Type: "parent_of"}

	exact := models.RelationshipEdge{SourceID: "uid-a", TargetID: "uid-b", Type: "parent_of"}
	assert.True(t, edgesConflict(a, exact), "identical edges are the same fact")

	// "A is B's parent" and "B is A's child" are one fact stored two ways.
	inverse := models.RelationshipEdge{SourceID: "uid-b", TargetID: "uid-a", Type: "child_of"}
	assert.True(t, edgesConflict(a, inverse), "mutual inverses are the same fact")

	unrelated := models.RelationshipEdge{SourceID: "uid-a", TargetID: "uid-c", Type: "parent_of"}
	assert.False(t, edgesConflict(a, unrelated), "a different target is a different fact")

	sameDirectionDifferentType := models.RelationshipEdge{SourceID: "uid-a", TargetID: "uid-b", Type: "friend_of"}
	assert.False(t, edgesConflict(a, sameDirectionDifferentType))
}

func TestPickEdgeToDrop_KeepsTheMoreAuthoritativeEdge(t *testing.T) {
	older := time.Now().Add(-48 * time.Hour)
	newer := time.Now()

	t.Run("higher confidence wins", func(t *testing.T) {
		low := models.RelationshipEdge{ID: "a", Confidence: 0.4}
		high := models.RelationshipEdge{ID: "b", Confidence: 0.9}
		assert.Equal(t, "a", pickEdgeToDrop(low, high).ID)
		assert.Equal(t, "a", pickEdgeToDrop(high, low).ID, "argument order must not matter")
	})

	t.Run("confirmed beats suggested on equal confidence", func(t *testing.T) {
		suggested := models.RelationshipEdge{ID: "a", Confidence: 1.0, Status: models.RelationshipStatusSuggested}
		confirmed := models.RelationshipEdge{ID: "b", Confidence: 1.0, Status: models.RelationshipStatusConfirmed}
		assert.Equal(t, "a", pickEdgeToDrop(suggested, confirmed).ID)
	})

	t.Run("older record survives on equal confidence and status", func(t *testing.T) {
		old := models.RelationshipEdge{ID: "a", Confidence: 1.0, Status: models.RelationshipStatusConfirmed, CreatedAt: older}
		recent := models.RelationshipEdge{ID: "b", Confidence: 1.0, Status: models.RelationshipStatusConfirmed, CreatedAt: newer}
		assert.Equal(t, "b", pickEdgeToDrop(old, recent).ID, "the longer-standing record is kept")
	})

	t.Run("deterministic with no signal left", func(t *testing.T) {
		created := time.Now()
		a := models.RelationshipEdge{ID: "aaa", Confidence: 1.0, Status: models.RelationshipStatusConfirmed, CreatedAt: created}
		b := models.RelationshipEdge{ID: "bbb", Confidence: 1.0, Status: models.RelationshipStatusConfirmed, CreatedAt: created}
		assert.Equal(t, "bbb", pickEdgeToDrop(a, b).ID)
		assert.Equal(t, "bbb", pickEdgeToDrop(b, a).ID, "result must not depend on argument order")
	})
}

// T90: if the user's "Me" pointer (users.self_contact_vcard_uid) pointed at
// the loser, the merge must move it to the keeper — otherwise it dangles on
// the soft-deleted loser row.
func TestRepointContactAssociations_RepointsSelfContactPointer(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	uid := loser.VCardUID
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", userID).
		Update("self_contact_vcard_uid", uid).Error)

	_, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)

	var user models.User
	require.NoError(t, db.First(&user, userID).Error)
	require.NotNil(t, user.SelfContactVCardUID)
	assert.Equal(t, keeper.VCardUID, *user.SelfContactVCardUID,
		"the self contact pointer must follow the merge onto the keeper")
}

// T90: a pointer that already points at the keeper (or is NULL) is left
// untouched — the repoint is a no-op, not a clobber.
func TestRepointContactAssociations_LeavesSelfContactPointerOnKeeper(t *testing.T) {
	db, userID := newMergeDB(t)
	keeper := makeMergeContact(t, db, userID, "Keeper")
	loser := makeMergeContact(t, db, userID, "Loser")

	uid := keeper.VCardUID
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", userID).
		Update("self_contact_vcard_uid", uid).Error)

	_, err := RepointContactAssociations(db, userID, keeper, loser, nil, map[string]string{})
	require.NoError(t, err)

	var user models.User
	require.NoError(t, db.First(&user, userID).Error)
	require.NotNil(t, user.SelfContactVCardUID)
	assert.Equal(t, keeper.VCardUID, *user.SelfContactVCardUID)
}

func TestBuildContactMergeNoteContent_RecordsResolutionsAndCounts(t *testing.T) {
	loser := &models.Contact{Firstname: "Bob", Lastname: "Smith"}
	loser.ID = 42

	res := &models.ContactMergeResolution{
		Conflicts: []models.ContactMergeFieldConflict{
			{Field: "job_title", Label: "Job Title", KeeperValue: "Engineer", LoserValue: "Developer"},
		},
	}
	resolutions := map[string]string{"job_title": "Developer"}
	counts := models.ContactMergeAssociationCounts{Notes: 3, Reminders: 1, ContactSyncLinks: 2}

	content := BuildContactMergeNoteContent(loser, res, resolutions, counts, 1)

	assert.Contains(t, content, "Merged contact #42 (Bob Smith)")
	assert.Contains(t, content, "Job Title: took \"Developer\" from the merged contact (was \"Engineer\")")
	assert.Contains(t, content, "3 notes")
	assert.Contains(t, content, "(1 dropped as duplicate/self-loop)")
	assert.Contains(t, content, "2 CardDAV sync link(s) on the merged contact were discarded",
		"discarded sync links must be recorded — they are not re-pointed")
}

func TestBuildContactMergeNoteContent_OmitsSyncLineWhenNoneDiscarded(t *testing.T) {
	loser := &models.Contact{Firstname: "Bob"}
	content := BuildContactMergeNoteContent(
		loser, &models.ContactMergeResolution{}, map[string]string{},
		models.ContactMergeAssociationCounts{}, 0)

	assert.NotContains(t, content, "CardDAV sync link")
}
