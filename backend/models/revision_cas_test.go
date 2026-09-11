package models

import (
	"testing"
	"time"

	"mycorrhizal/contactmodel"
	"mycorrhizal/internal/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are the CON-01 follow-up regression tests (issues #920, #924; ADR
// 0018) for bumpRevisionCAS. Each one reproduces, against the real migrated
// schema (dbtest, CLAUDE.md backend trap #1), the exact race described in
// its issue's hand-verification section -- not a synthetic approximation of
// it -- so a reintroduced read-modify-write bump would fail these
// deterministically, with no goroutines or timing involved: both "clients"
// are just two separately loaded copies of the same row, exactly like the
// issues' own repro code.

// --- Contact --------------------------------------------------------------

// TestBumpRevisionCAS_Contact_ResurrectionRace is issue #920's exact
// scenario: a contact is soft-deleted, then a stale in-memory copy (loaded
// before the delete) is saved. Before the fix, GORM's Save() upsert
// fallback resurrected the row with the stale edit; now the CAS must reject
// the write and leave the tombstone (and the pre-delete content) intact.
func TestBumpRevisionCAS_Contact_ResurrectionRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-resurrect", Password: "password123!A", Email: "cas-resurrect@example.com"}
	require.NoError(t, db.Create(&user).Error)
	created := Contact{UserID: user.ID, Firstname: "Ada"}
	require.NoError(t, db.Create(&created).Error)

	// The handler's copy: loaded before the concurrent delete.
	var live Contact
	require.NoError(t, db.First(&live, created.ID).Error)
	require.EqualValues(t, 1, live.Revision)

	// Concurrent delete commits.
	require.NoError(t, db.Delete(&Contact{}, live.ID).Error)

	// The stale copy, unaware of the delete, tries to persist an edit.
	live.Firstname = "edited"
	err := db.Save(&live).Error
	require.Error(t, err, "a concurrent soft-delete must reject the stale Save(), not resurrect the row")
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.True(t, conflict.Deleted, "the CAS must classify this as a deleted-row conflict")
	assert.Equal(t, "Contact", conflict.Entity)
	assert.EqualValues(t, 1, conflict.ExpectedRevision)

	// The row is still gone, and still carries its pre-delete content --
	// nothing about the rejected write leaked through.
	var reFetched Contact
	require.NoError(t, db.Unscoped().First(&reFetched, live.ID).Error)
	assert.True(t, reFetched.DeletedAt.Valid, "the row must remain soft-deleted -- no resurrection")
	assert.Equal(t, "Ada", reFetched.Firstname, "the stale edit must not have been persisted")
	assert.EqualValues(t, 1, reFetched.Revision, "a rejected write must not advance the revision counter either")

	// And the row is genuinely gone from the ordinary (scoped) query path.
	require.Error(t, db.First(&Contact{}, live.ID).Error)
}

// TestBumpRevisionCAS_Contact_LostUpdateRace is issue #924's exact
// scenario: two independently loaded copies of the same row both start at
// revision 1. The first Save() must win; the second, still holding revision
// 1, must be rejected rather than silently overwriting the first with a
// duplicate "revision 2".
func TestBumpRevisionCAS_Contact_LostUpdateRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-lostupdate", Password: "password123!A", Email: "cas-lostupdate@example.com"}
	require.NoError(t, db.Create(&user).Error)
	created := Contact{UserID: user.ID, Firstname: "Ada"}
	require.NoError(t, db.Create(&created).Error)

	// Both clients read the row at revision 1.
	var copyA, copyB Contact
	require.NoError(t, db.First(&copyA, created.ID).Error)
	require.NoError(t, db.First(&copyB, created.ID).Error)
	require.EqualValues(t, 1, copyA.Revision)
	require.EqualValues(t, 1, copyB.Revision)

	// B writes first and wins.
	copyB.Firstname = "B-change"
	require.NoError(t, db.Save(&copyB).Error)
	assert.EqualValues(t, 2, copyB.Revision)

	// A, still on its stale revision-1 copy, must be rejected -- not
	// silently clobber B's committed change while reusing revision 2.
	copyA.Firstname = "A-change"
	err := db.Save(&copyA).Error
	require.Error(t, err, "a second writer holding a stale revision must not be able to overwrite the first")
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.False(t, conflict.Deleted, "the row is still live -- this is a revision conflict, not a deletion")
	assert.EqualValues(t, 1, conflict.ExpectedRevision)

	var stored Contact
	require.NoError(t, db.First(&stored, created.ID).Error)
	assert.Equal(t, "B-change", stored.Firstname, "B's committed write must survive intact")
	assert.EqualValues(t, 2, stored.Revision, "the revision must not have been double-bumped or reused")
}

// --- Note -------------------------------------------------------------

func TestBumpRevisionCAS_Note_LostUpdateRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-note", Password: "password123!A", Email: "cas-note@example.com"}
	require.NoError(t, db.Create(&user).Error)
	contact := Contact{UserID: user.ID, Firstname: "Ada"}
	require.NoError(t, db.Create(&contact).Error)
	created := Note{UserID: user.ID, Content: "original", Date: time.Now(), ContactID: &contact.ID}
	require.NoError(t, db.Create(&created).Error)

	var copyA, copyB Note
	require.NoError(t, db.First(&copyA, created.ID).Error)
	require.NoError(t, db.First(&copyB, created.ID).Error)

	copyB.Content = "B-change"
	require.NoError(t, db.Save(&copyB).Error)

	copyA.Content = "A-change"
	err := db.Save(&copyA).Error
	require.Error(t, err)
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.False(t, conflict.Deleted)

	var stored Note
	require.NoError(t, db.First(&stored, created.ID).Error)
	assert.Equal(t, "B-change", stored.Content)
	assert.EqualValues(t, 2, stored.Revision)
}

// --- Activity -----------------------------------------------------------

func TestBumpRevisionCAS_Activity_LostUpdateRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-activity", Password: "password123!A", Email: "cas-activity@example.com"}
	require.NoError(t, db.Create(&user).Error)
	created := Activity{UserID: user.ID, Title: "original", Date: time.Now()}
	require.NoError(t, db.Create(&created).Error)

	var copyA, copyB Activity
	require.NoError(t, db.First(&copyA, created.ID).Error)
	require.NoError(t, db.First(&copyB, created.ID).Error)

	copyB.Title = "B-change"
	require.NoError(t, db.Save(&copyB).Error)

	copyA.Title = "A-change"
	err := db.Save(&copyA).Error
	require.Error(t, err)
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.False(t, conflict.Deleted)

	var stored Activity
	require.NoError(t, db.First(&stored, created.ID).Error)
	assert.Equal(t, "B-change", stored.Title)
	assert.EqualValues(t, 2, stored.Revision)
}

// --- LifeEvent (UUID-string primary key, distinct from the uint-PK
// entities above) --------------------------------------------------------

func TestBumpRevisionCAS_LifeEvent_LostUpdateRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-lifeevent", Password: "password123!A", Email: "cas-lifeevent@example.com"}
	require.NoError(t, db.Create(&user).Error)
	contact := Contact{UserID: user.ID, Firstname: "Ada"}
	require.NoError(t, db.Create(&contact).Error)
	year := 2024
	created := LifeEvent{
		UserID:   user.ID,
		EntityID: contact.VCardUID,
		Type:     LifeEventTypeGraduated,
		Date:     &contactmodel.PartialDate{Year: &year},
		Source:   LifeEventSourceUser,
	}
	require.NoError(t, db.Create(&created).Error)

	var copyA, copyB LifeEvent
	require.NoError(t, db.Where("id = ?", created.ID).First(&copyA).Error)
	require.NoError(t, db.Where("id = ?", created.ID).First(&copyB).Error)

	copyB.Description = "B-change"
	require.NoError(t, db.Save(&copyB).Error)

	copyA.Description = "A-change"
	err := db.Save(&copyA).Error
	require.Error(t, err)
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.False(t, conflict.Deleted)
	assert.Equal(t, created.ID, conflict.ID, "LifeEvent's UUID string PK must round-trip through the generic CAS helper")

	var stored LifeEvent
	require.NoError(t, db.Where("id = ?", created.ID).First(&stored).Error)
	assert.Equal(t, "B-change", stored.Description)
	assert.EqualValues(t, 2, stored.Revision)
}

// --- Reminder -- the UpdateReminder handler uses db.Updates(), not
// db.Save(), so it never risked GORM's Save()-specific upsert fallback
// (#920's exact mechanism). The lost-update race (#924's mechanism) applies
// regardless of which finisher method the caller uses, since both route
// through the same AfterSave hook -- and bumpRevisionCAS's protection is a
// property of the model, not of any one caller, so a resurrection race is
// tested here too against a direct Save() the way some future caller (or a
// bulk import path) might use it.

func TestBumpRevisionCAS_Reminder_LostUpdateRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-reminder", Password: "password123!A", Email: "cas-reminder@example.com"}
	require.NoError(t, db.Create(&user).Error)
	contact := Contact{UserID: user.ID, Firstname: "Ada"}
	require.NoError(t, db.Create(&contact).Error)
	byMail := false
	reoccur := true
	created := Reminder{
		UserID:                user.ID,
		Message:               "original",
		ByMail:                &byMail,
		RemindAt:              time.Now().Add(48 * time.Hour),
		Recurrence:            "once",
		ReoccurFromCompletion: &reoccur,
		ContactID:             &contact.ID,
	}
	require.NoError(t, db.Create(&created).Error)

	var copyA, copyB Reminder
	require.NoError(t, db.First(&copyA, created.ID).Error)
	require.NoError(t, db.First(&copyB, created.ID).Error)

	copyB.Message = "B-change"
	require.NoError(t, db.Save(&copyB).Error)

	copyA.Message = "A-change"
	err := db.Save(&copyA).Error
	require.Error(t, err)
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.False(t, conflict.Deleted)

	var stored Reminder
	require.NoError(t, db.First(&stored, created.ID).Error)
	assert.Equal(t, "B-change", stored.Message)
	assert.EqualValues(t, 2, stored.Revision)
}

func TestBumpRevisionCAS_Reminder_ResurrectionRace(t *testing.T) {
	db := dbtest.New(t)
	user := User{Username: "cas-reminder-del", Password: "password123!A", Email: "cas-reminder-del@example.com"}
	require.NoError(t, db.Create(&user).Error)
	contact := Contact{UserID: user.ID, Firstname: "Ada"}
	require.NoError(t, db.Create(&contact).Error)
	byMail := false
	reoccur := true
	created := Reminder{
		UserID:                user.ID,
		Message:               "original",
		ByMail:                &byMail,
		RemindAt:              time.Now().Add(48 * time.Hour),
		Recurrence:            "once",
		ReoccurFromCompletion: &reoccur,
		ContactID:             &contact.ID,
	}
	require.NoError(t, db.Create(&created).Error)

	var live Reminder
	require.NoError(t, db.First(&live, created.ID).Error)

	require.NoError(t, db.Delete(&Reminder{}, live.ID).Error)

	live.Message = "edited"
	err := db.Save(&live).Error
	require.Error(t, err, "a plain Save() on a concurrently soft-deleted reminder must not resurrect it")
	var conflict *ErrRevisionConflict
	require.ErrorAs(t, err, &conflict)
	assert.True(t, conflict.Deleted)

	var reFetched Reminder
	require.NoError(t, db.Unscoped().First(&reFetched, live.ID).Error)
	assert.True(t, reFetched.DeletedAt.Valid)
	assert.Equal(t, "original", reFetched.Message)
}
