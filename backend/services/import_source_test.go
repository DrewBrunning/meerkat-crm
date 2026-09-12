package services

import (
	"testing"

	"mycorrhizal/contactmodel"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSourceImportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return dbtest.New(t)
}

func createSourceImportUser(t *testing.T, db *gorm.DB) models.User {
	t.Helper()
	user := models.User{Username: "import-test", Password: "password123!A", Email: "import@example.com"}
	require.NoError(t, db.Create(&user).Error)
	return user
}

// ref is a tiny helper to write plan contact refs tersely.
func ref(id string) SourceRef { return SourceRef{System: "test", ExternalID: id} }

// minimalRecord builds a neutral Record with the given name parts.
func minimalRecord(given, surname string) *contactmodel.Record {
	return &contactmodel.Record{Card: contactmodel.Card{
		Name: &contactmodel.Name{Components: []contactmodel.NameComponent{
			{Kind: "given", Value: given},
			{Kind: "surname", Value: surname},
		}},
	}}
}

func TestExecuteSourceImport_CreatesContactsAndGraph(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{
		{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")},
		{Ref: ref("contact/2"), Record: minimalRecord("Ben", "Babbage"), Favorite: true, Archived: true},
	}
	plan.Notes = []MappedNote{{Ref: ref("note/1"), Contact: ref("contact/1"), Content: "hello", Date: "2023-01-01T00:00:00Z"}}
	plan.Relationships = []MappedRelationship{{
		Ref: ref("relationship/1"), Source: ref("contact/2"), Target: ref("contact/1"),
		Type: "spouse_of", Directional: false,
	}}
	plan.Circles = []MappedCircle{{Ref: ref("circle/1"), Name: "Family", Members: []SourceRef{ref("contact/1"), ref("contact/2")}}}
	plan.Preferences = []MappedPreference{{Ref: ref("pref/1"), Contact: ref("contact/1"), Category: models.PreferenceCategoryFood, Value: "oysters"}}
	plan.Gifts = []MappedGift{{Ref: ref("gift/1"), Contact: ref("contact/1"), Status: models.GiftStatusIdea, Description: "engine model"}}
	plan.CustomFields = []MappedCustomField{{Ref: ref("field/1"), Contact: ref("contact/1"), Key: "zodiac", Label: "Zodiac", Value: "Pisces"}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)

	assert.Equal(t, 2, report.ContactsCreated)
	assert.Equal(t, 1, report.NotesCreated)
	assert.Equal(t, 1, report.RelationshipsCreated)
	assert.Equal(t, 1, report.CirclesCreated)
	assert.Equal(t, 1, report.PreferencesCreated)
	assert.Equal(t, 1, report.GiftsCreated)
	assert.Equal(t, 1, report.CustomFieldsCreated)
	assert.Empty(t, report.Issues)

	var contacts []models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&contacts).Error)
	require.Len(t, contacts, 2)

	// Contacts landed through ApplyRecordToContact (CLAUDE.md trap #2): the
	// neutral Card was preserved and the flat projection derived.
	byName := map[string]models.Contact{}
	for _, c := range contacts {
		byName[c.Firstname] = c
	}
	ada := byName["Ada"]
	assert.Equal(t, "Lovelace", ada.Lastname)
	assert.NotEmpty(t, ada.VCardUID)
	assert.NotEmpty(t, ada.Card.Name)
	ben := byName["Ben"]
	assert.True(t, ben.IsFavorite)
	assert.True(t, ben.Archived)

	// Graph entities keyed by VCardUID (the graph invariant).
	var note models.Note
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&note).Error)
	assert.Equal(t, ada.ID, *note.ContactID)

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&edges).Error)
	require.Len(t, edges, 1)
	assert.Equal(t, ben.VCardUID, edges[0].SourceID)
	assert.Equal(t, ada.VCardUID, edges[0].TargetID)
	assert.Equal(t, "spouse_of", edges[0].Type)
	assert.Equal(t, models.RelationshipSourceImported, edges[0].Source)
	assert.Equal(t, models.RelationshipStatusConfirmed, edges[0].Status)

	var circle models.Circle
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&circle).Error)
	assert.Equal(t, "Family", circle.Name)
	var members []models.CircleMember
	require.NoError(t, db.Where("circle_id = ?", circle.ID).Find(&members).Error)
	assert.Len(t, members, 2)

	var pref models.Preference
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&pref).Error)
	assert.Equal(t, models.PreferenceCategoryFood, pref.Category)
	assert.Equal(t, ada.VCardUID, pref.EntityID)

	var gift models.Gift
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&gift).Error)
	assert.Equal(t, ada.VCardUID, gift.EntityID)

	var def models.FieldDefinition
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&def).Error)
	assert.Equal(t, "zodiac", def.Key)
	var fv models.FieldValue
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&fv).Error)
	assert.Equal(t, ada.VCardUID, fv.EntityID)
}

func TestExecuteSourceImport_IsIdempotent(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{
		{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")},
	}
	plan.Notes = []MappedNote{{Ref: ref("note/1"), Contact: ref("contact/1"), Content: "hello", Date: "2023-01-01T00:00:00Z"}}

	first, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, first.ContactsCreated)
	assert.Equal(t, 1, first.NotesCreated)

	// Re-run the same plan: nothing duplicates (CON-04 / issue #459 applied
	// to source imports). Every row is reported as already imported.
	second, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, second.ContactsCreated)
	assert.Equal(t, 1, second.ContactsSkipped)
	assert.Equal(t, 0, second.NotesCreated)

	var contacts []models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&contacts).Error)
	require.Len(t, contacts, 1)
	var notes []models.Note
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&notes).Error)
	require.Len(t, notes, 1)
}

func TestExecuteSourceImport_RejectsNamelessContactAndDropsItsGraph(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	// A contact with no usable name at all (not even a nickname) cannot exist
	// locally and must be rejected whole, naming the record and field.
	empty := &contactmodel.Record{}
	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: empty}}
	plan.Notes = []MappedNote{{Ref: ref("note/1"), Contact: ref("contact/1"), Content: "orphan", Date: "2023-01-01T00:00:00Z"}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, report.ContactsCreated)
	assert.Equal(t, 0, report.NotesCreated)

	var found []ImportIssue
	for _, iss := range report.Issues {
		if iss.Category == ImportIssueCategoryInvalid && iss.Record == "test contact/1" {
			found = append(found, iss)
		}
	}
	require.Len(t, found, 1, "the nameless contact must be reported as invalid with its field")
	assert.Equal(t, "name", found[0].Field)

	// The note referencing the rejected contact is dropped with a named issue,
	// never orphaned.
	var orphanNote bool
	for _, iss := range report.Issues {
		if iss.Record == "test note/1" && iss.Category == ImportIssueCategoryUnsupported {
			orphanNote = true
		}
	}
	assert.True(t, orphanNote, "the note referencing a non-imported contact must be reported")

	var contacts []models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&contacts).Error)
	assert.Len(t, contacts, 0, "a failed record must leave no partial contact")
}

func TestExecuteSourceImport_GraphEntityWithBadDateIsNamedNotHalfCreated(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.Reminders = []MappedReminder{{
		Ref: ref("reminder/1"), Contact: ref("contact/1"), Message: "hi",
		RemindAt: "not-a-date", Recurrence: "once",
	}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, report.ContactsCreated)
	assert.Equal(t, 0, report.RemindersCreated)

	var named bool
	for _, iss := range report.Issues {
		if iss.Record == "test reminder/1" && iss.Field == "reminder.remind_at" && iss.Category == ImportIssueCategoryInvalid {
			named = true
		}
	}
	assert.True(t, named, "the bad-date reminder must be reported with its field")

	var reminders []models.Reminder
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&reminders).Error)
	assert.Len(t, reminders, 0)
}

func TestExecuteSourceImport_ScopesByUser(t *testing.T) {
	db := setupSourceImportTestDB(t)
	userA := createSourceImportUser(t, db)
	userB := models.User{Username: "import-test-b", Password: "password123!A", Email: "importb@example.com"}
	require.NoError(t, db.Create(&userB).Error)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}

	_, err := ExecuteSourceImport(db, userA.ID, plan)
	require.NoError(t, err)

	// User B's idempotency ledger is empty: importing the same plan for a
	// different user must not skip it.
	report, err := ExecuteSourceImport(db, userB.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, report.ContactsCreated)

	var links []models.ImportSourceLink
	require.NoError(t, db.Where("system = ?", "test").Find(&links).Error)
	require.Len(t, links, 2)
	assert.Equal(t, userA.ID, links[0].UserID)
	assert.Equal(t, userB.ID, links[1].UserID)
}

func TestExecuteSourceImport_AlreadyExistingContactKeepsVCardUID(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	uid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.Contacts[0].Record.Card.UID = uid

	_, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)

	var c models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&c).Error)
	assert.Equal(t, uid, c.VCardUID)
}

func TestExecuteSourceImport_HouseholdsAndTags(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{
		{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")},
		{Ref: ref("contact/2"), Record: minimalRecord("Ben", "Babbage")},
	}
	plan.Households = []MappedHousehold{{
		Ref: ref("household/1"), Name: "The Lovelaces", Type: models.HouseholdTypeFamilyUnit,
		Members: []MappedHouseholdMember{
			{Contact: ref("contact/1"), Role: models.HouseholdRoleAdult},
			{Contact: ref("contact/2"), Role: models.HouseholdRoleChild, Since: "2020-01-01"},
		},
	}}
	plan.Tags = []MappedTag{{
		Ref: ref("tag/1"), Name: "favorite", Contacts: []SourceRef{ref("contact/1"), ref("contact/2")},
	}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, report.HouseholdsCreated)
	assert.Equal(t, 1, report.TagsCreated)

	var hh models.Household
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&hh).Error)
	assert.Equal(t, "The Lovelaces", hh.Name)
	var members []models.HouseholdMember
	require.NoError(t, db.Where("household_id = ?", hh.ID).Find(&members).Error)
	assert.Len(t, members, 2)

	var tag models.Tag
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&tag).Error)
	assert.Equal(t, "favorite", tag.Name)
	var taggings []models.ContactTag
	require.NoError(t, db.Where("tag_id = ?", tag.ID).Find(&taggings).Error)
	assert.Len(t, taggings, 2)
}

func TestExecuteSourceImport_ActivityWithoutImportedAttendeeIsNamed(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.Activities = []MappedActivity{{
		Ref: ref("activity/1"), Title: "Orphan gathering",
		Contacts: []SourceRef{ref("contact/9")}, Date: "2023-01-01T00:00:00Z",
	}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, report.ActivitiesCreated)

	var named bool
	for _, iss := range report.Issues {
		if iss.Record == "test activity/1" && iss.Field == "activity.attendees" && iss.Category == ImportIssueCategoryUnsupported {
			named = true
		}
	}
	assert.True(t, named, "an activity whose only attendee was not imported must be reported, not half-created")

	var activities []models.Activity
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&activities).Error)
	assert.Len(t, activities, 0)
}

func TestExecuteSourceImport_NilAndEmptyPlans(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	report, err := ExecuteSourceImport(db, user.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, report.ContactsCreated)

	report, err = ExecuteSourceImport(db, user.ID, &ImportSourcePlan{})
	require.NoError(t, err)
	assert.Equal(t, 0, report.ContactsCreated)
}

func TestAppendIssue_Deduplicates(t *testing.T) {
	r := &ImportReport{}
	issue := ImportIssue{Record: "x", Field: "y", Category: ImportIssueCategoryLossy, Message: "m"}
	r.appendIssue(issue)
	r.appendIssue(issue)
	assert.Len(t, r.Issues, 1)
}

func TestSourceRef_String(t *testing.T) {
	assert.Equal(t, "meerkat", (SourceRef{System: "meerkat"}).String())
	assert.Equal(t, "meerkat contact/1", (SourceRef{System: "meerkat", ExternalID: "contact/1"}).String())
}

// TestExecuteSourceImport_DuplicateSourceRefSkipsSecond pins the ledger's
// role as a hard guarantee: a plan that repeats a source ref (the same
// external_id twice — a malformed plan) imports the first and treats the
// second as already imported rather than duplicating it.
func TestExecuteSourceImport_DuplicateSourceRefSkipsSecond(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{
		{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")},
		{Ref: ref("contact/1"), Record: minimalRecord("Eve", "Other")}, // same source ref
	}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, report.ContactsCreated)
	assert.Equal(t, 1, report.ContactsSkipped)

	var contacts []models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&contacts).Error)
	assert.Len(t, contacts, 1)
}

// TestExecuteSourceImport_DuplicateRelationshipNaturalKeySkipped: a source
// that describes the same (source, target, type) fact under two rows (or a
// fact a user already created) must not fail the import on the unique index
// added by migration 000055 — the extra row is skipped and reported, and the
// import still succeeds.
func TestExecuteSourceImport_DuplicateRelationshipNaturalKeySkipped(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{
		{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")},
		{Ref: ref("contact/2"), Record: minimalRecord("Ben", "Babbage")},
	}
	dup := func(sourceRef string) MappedRelationship {
		return MappedRelationship{Ref: ref(sourceRef), Source: ref("contact/2"), Target: ref("contact/1"), Type: "spouse_of"}
	}
	plan.Relationships = []MappedRelationship{dup("relationship/1"), dup("relationship/2")}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, report.RelationshipsCreated, "the duplicate row must not create a second edge")

	var skipped bool
	for _, iss := range report.Issues {
		if iss.Record == "test relationship/2" && iss.Field == "relationship" && iss.Category == ImportIssueCategorySkipped {
			skipped = true
		}
	}
	assert.True(t, skipped, "the redundant relationship row must be reported as skipped")

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&edges).Error)
	assert.Len(t, edges, 1)
}

// TestExecuteSourceImport_DuplicateVCardUIDReportedNotHalfCreated: a mapped
// contact whose Card.UID collides with an existing contact's VCardUID fails
// creation (partial unique index) and is reported with its record, leaving
// nothing behind.
func TestExecuteSourceImport_DuplicateVCardUIDReported(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	uid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	existing := &models.Contact{UserID: user.ID, Firstname: "First", VCardUID: uid}
	require.NoError(t, db.Create(existing).Error)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.Contacts[0].Record.Card.UID = uid

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, report.ContactsCreated)

	var named bool
	for _, iss := range report.Issues {
		if iss.Record == "test contact/1" && iss.Field == "contact" && iss.Category == ImportIssueCategoryInvalid {
			named = true
		}
	}
	assert.True(t, named, "the vcard-uid collision must be reported")

	var contacts []models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&contacts).Error)
	assert.Len(t, contacts, 1, "only the pre-existing contact remains")
}

// TestExecuteSourceImport_GraphRefsToFailedContactAreAllNamed: every graph
// entity kind referencing a contact that failed to import must be dropped
// with a named issue, never silently orphaned.
func TestExecuteSourceImport_GraphRefsToFailedContactAreAllNamed(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	// A valid contact and a nameless one that will fail validation.
	plan.Contacts = []MappedContact{
		{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")},
		{Ref: ref("contact/2"), Record: &contactmodel.Record{}},
	}
	bad := ref("contact/2")
	plan.Relationships = []MappedRelationship{
		{Ref: ref("rel/1"), Source: ref("contact/1"), Target: bad, Type: "friend_of"},
		{Ref: ref("rel/2"), Source: bad, Target: ref("contact/1"), Type: "friend_of"},
	}
	plan.Households = []MappedHousehold{{Ref: ref("hh/1"), Name: "H", Type: "other", Members: []MappedHouseholdMember{{Contact: bad}}}}
	plan.Circles = []MappedCircle{{Ref: ref("c/1"), Name: "C", Members: []SourceRef{bad}}}
	plan.Tags = []MappedTag{{Ref: ref("t/1"), Name: "T", Contacts: []SourceRef{bad}}}
	plan.Gifts = []MappedGift{{Ref: ref("g/1"), Contact: bad, Status: "idea", Description: "x"}}
	plan.Preferences = []MappedPreference{{Ref: ref("p/1"), Contact: bad, Category: "food", Value: "x"}}
	plan.CustomFields = []MappedCustomField{{Ref: ref("f/1"), Contact: bad, Key: "k", Label: "K", Value: "v"}}
	plan.Reminders = []MappedReminder{{Ref: ref("r/1"), Contact: bad, Message: "m", RemindAt: "2023-01-01T00:00:00Z", Recurrence: "once"}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, report.ContactsCreated)

	// Every dependent entity referencing the failed contact reports an
	// unsupported-orphan issue (or is skipped), and nothing is created.
	var orphanIssues int
	for _, iss := range report.Issues {
		if iss.Category == ImportIssueCategoryUnsupported {
			orphanIssues++
		}
	}
	assert.GreaterOrEqual(t, orphanIssues, 8, "each graph kind referencing the failed contact is named")

	var edges []models.RelationshipEdge
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&edges).Error)
	assert.Empty(t, edges)
	var gifts []models.Gift
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&gifts).Error)
	assert.Empty(t, gifts)
	var prefs []models.Preference
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&prefs).Error)
	assert.Empty(t, prefs)
	var reminders []models.Reminder
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&reminders).Error)
	assert.Empty(t, reminders)
}

// TestExecuteSourceImport_BadDatesAreNamedPerEntity: an unparseable date on
// any dated entity is reported with its field and creates nothing.
func TestExecuteSourceImport_BadDatesAreNamedPerEntity(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.Notes = []MappedNote{{Ref: ref("note/1"), Contact: ref("contact/1"), Content: "x", Date: "bogus"}}
	plan.Activities = []MappedActivity{{Ref: ref("activity/1"), Title: "x", Contacts: []SourceRef{ref("contact/1")}, Date: "bogus"}}
	plan.Gifts = []MappedGift{{Ref: ref("gift/1"), Contact: ref("contact/1"), Description: "x", Date: "bogus"}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, report.NotesCreated)
	assert.Equal(t, 0, report.ActivitiesCreated)
	// The gift itself survives; only its unparseable date is lost, and that
	// loss is named (a gift without a date is still a real gift).
	assert.Equal(t, 1, report.GiftsCreated)

	var named int
	for _, iss := range report.Issues {
		if iss.Category == ImportIssueCategoryInvalid || iss.Category == ImportIssueCategoryLossy {
			named++
		}
	}
	assert.GreaterOrEqual(t, named, 3, "note and activity bad dates + the gift date loss are each named")

	var notes []models.Note
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&notes).Error)
	assert.Empty(t, notes)

	var giftDateLoss bool
	for _, iss := range report.Issues {
		if iss.Record == "test gift/1" && iss.Field == "gift.date" && iss.Category == ImportIssueCategoryLossy {
			giftDateLoss = true
		}
	}
	assert.True(t, giftDateLoss, "the gift's dropped date must be reported")
}

// TestExecuteSourceImport_AlreadyImportedGraphSkipped: re-running a plan with
// households/tags/notes skips the already-imported graph rows too (the ledger
// is uniform across entity kinds).
func TestExecuteSourceImport_AlreadyImportedGraphSkipped(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.Households = []MappedHousehold{{Ref: ref("hh/1"), Name: "H", Type: "other", Members: []MappedHouseholdMember{{Contact: ref("contact/1")}}}}
	plan.Tags = []MappedTag{{Ref: ref("t/1"), Name: "T", Contacts: []SourceRef{ref("contact/1")}}}
	plan.Notes = []MappedNote{{Ref: ref("note/1"), Contact: ref("contact/1"), Content: "x", Date: "2023-01-01T00:00:00Z"}}

	first, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, first.HouseholdsCreated)
	assert.Equal(t, 1, first.TagsCreated)
	assert.Equal(t, 1, first.NotesCreated)

	second, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, second.HouseholdsCreated)
	assert.Equal(t, 0, second.TagsCreated)
	assert.Equal(t, 0, second.NotesCreated)

	var hh []models.Household
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&hh).Error)
	assert.Len(t, hh, 1)
	var tags []models.Tag
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&tags).Error)
	assert.Len(t, tags, 1)
	var notes []models.Note
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&notes).Error)
	assert.Len(t, notes, 1)
}

// TestExecuteSourceImport_CustomFieldKeyCollisionReported: a mapped custom
// field whose key already exists as the user's FieldDefinition fails the
// definition create (unique user+key) and is reported, leaving no value row.
func TestExecuteSourceImport_CustomFieldKeyCollisionReported(t *testing.T) {
	db := setupSourceImportTestDB(t)
	user := createSourceImportUser(t, db)

	def := models.FieldDefinition{
		UserID: user.ID, Label: "Existing", Key: "zodiac",
		Target: models.FieldDefinitionTargetContact, Type: models.FieldTypeText,
		Projection: "internal-only",
	}
	require.NoError(t, db.Create(&def).Error)

	plan := &ImportSourcePlan{System: "test"}
	plan.Contacts = []MappedContact{{Ref: ref("contact/1"), Record: minimalRecord("Ada", "Lovelace")}}
	plan.CustomFields = []MappedCustomField{{Ref: ref("field/1"), Contact: ref("contact/1"), Key: "zodiac", Label: "Zodiac", Value: "Pisces"}}

	report, err := ExecuteSourceImport(db, user.ID, plan)
	require.NoError(t, err)
	assert.Equal(t, 0, report.CustomFieldsCreated)

	var named bool
	for _, iss := range report.Issues {
		if iss.Record == "test field/1" && iss.Category == ImportIssueCategoryInvalid {
			named = true
		}
	}
	assert.True(t, named, "the colliding custom field must be reported")

	var values []models.FieldValue
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&values).Error)
	assert.Empty(t, values, "no value row lands for the failed definition")
}
