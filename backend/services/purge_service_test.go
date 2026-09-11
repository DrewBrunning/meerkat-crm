package services

import (
	"fmt"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// T26's retention purge is the only code in the app that HARD-deletes user
// content, and it had zero test coverage. It also runs raw SQL against 15
// table/column pairs by name, which nothing else verifies — a renamed column
// would fail silently, since every failure path here logs and continues rather
// than returning an error.
//
// Real migrated schema (database.InitDB), not AutoMigrate: the raw SQL names
// real columns (member_vcard_uid, contact_vcard_uid, entity_id), which is
// exactly the class GORM's derived names get wrong.

func newPurgeDB(t *testing.T) (*gorm.DB, uint) {
	t.Helper()

	db := dbtest.New(t)

	user := models.User{Username: "purge-user", Email: "purge@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	return db, user.ID
}

func purgeConfig() config.Config {
	return config.Config{DeleteRetentionDays: 30}
}

// softDeleteAt back-dates a row's deleted_at so it falls on the chosen side of
// the retention cutoff. GORM's Delete stamps "now", which is always inside the
// window, so tests must set it explicitly.
func softDeleteAt(t *testing.T, db *gorm.DB, model any, id any, when time.Time) {
	t.Helper()
	require.NoError(t, db.Unscoped().Model(model).Where("id = ?", id).
		Update("deleted_at", when).Error)
}

func TestPurgeSoftDeletedRows_DeletesContactsPastRetention(t *testing.T) {
	db, userID := newPurgeDB(t)

	contact := models.Contact{UserID: userID, Firstname: "Old"}
	require.NoError(t, db.Create(&contact).Error)
	softDeleteAt(t, db, &models.Contact{}, contact.ID, time.Now().AddDate(0, 0, -60))

	PurgeSoftDeletedRows(db, purgeConfig())

	var count int64
	require.NoError(t, db.Unscoped().Model(&models.Contact{}).Where("id = ?", contact.ID).Count(&count).Error)
	assert.Zero(t, count, "a contact soft-deleted 60 days ago must be purged at a 30-day retention")
}

// The retention window is the whole point: purging too eagerly destroys data
// the user could still restore.
func TestPurgeSoftDeletedRows_KeepsContactsInsideRetention(t *testing.T) {
	db, userID := newPurgeDB(t)

	contact := models.Contact{UserID: userID, Firstname: "Recent"}
	require.NoError(t, db.Create(&contact).Error)
	softDeleteAt(t, db, &models.Contact{}, contact.ID, time.Now().AddDate(0, 0, -5))

	PurgeSoftDeletedRows(db, purgeConfig())

	var count int64
	require.NoError(t, db.Unscoped().Model(&models.Contact{}).Where("id = ?", contact.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "a contact soft-deleted 5 days ago is still inside a 30-day window")
}

func TestPurgeSoftDeletedRows_NeverTouchesLiveRows(t *testing.T) {
	db, userID := newPurgeDB(t)

	live := models.Contact{UserID: userID, Firstname: "Live"}
	require.NoError(t, db.Create(&live).Error)
	note := models.Note{UserID: userID, ContactID: &live.ID, Content: "keep me"}
	require.NoError(t, db.Create(&note).Error)

	PurgeSoftDeletedRows(db, purgeConfig())

	var contactCount, noteCount int64
	require.NoError(t, db.Model(&models.Contact{}).Where("id = ?", live.ID).Count(&contactCount).Error)
	require.NoError(t, db.Model(&models.Note{}).Where("id = ?", note.ID).Count(&noteCount).Error)
	assert.Equal(t, int64(1), contactCount)
	assert.Equal(t, int64(1), noteCount)
}

// Every raw-SQL cleanup names a table and column directly. This walks the ones
// keyed by a purged contact's VCardUID and asserts each is actually reached —
// a typo or a renamed column would otherwise leave orphans behind forever,
// silently, because the purge logs errors instead of returning them.
func TestPurgeSoftDeletedRows_CleansUpEdgeRowsForPurgedContacts(t *testing.T) {
	db, userID := newPurgeDB(t)

	contact := models.Contact{UserID: userID, Firstname: "Doomed"}
	require.NoError(t, db.Create(&contact).Error)
	other := models.Contact{UserID: userID, Firstname: "Survivor"}
	require.NoError(t, db.Create(&other).Error)

	circle := models.Circle{UserID: userID, Name: "Family"}
	require.NoError(t, db.Create(&circle).Error)
	require.NoError(t, db.Create(&models.CircleMember{
		CircleID: circle.ID, UserID: userID, MemberVCardUID: contact.VCardUID,
	}).Error)

	tag := models.Tag{UserID: userID, Name: "vip"}
	require.NoError(t, db.Create(&tag).Error)
	require.NoError(t, db.Create(&models.ContactTag{
		TagID: tag.ID, UserID: userID, ContactVCardUID: contact.VCardUID,
	}).Error)

	require.NoError(t, db.Create(&models.RelationshipEdge{
		UserID: userID, SourceID: contact.VCardUID, TargetID: other.VCardUID,
		Type: "friend_of", Source: models.RelationshipSourceUserConfirmed,
		Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
		Sensitivity: models.RelationshipSensitivityNormal,
	}).Error)

	softDeleteAt(t, db, &models.Contact{}, contact.ID, time.Now().AddDate(0, 0, -60))

	PurgeSoftDeletedRows(db, purgeConfig())

	var memberCount, tagCount, edgeCount int64
	require.NoError(t, db.Model(&models.CircleMember{}).
		Where("member_vcard_uid = ?", contact.VCardUID).Count(&memberCount).Error)
	require.NoError(t, db.Model(&models.ContactTag{}).
		Where("contact_vcard_uid = ?", contact.VCardUID).Count(&tagCount).Error)
	require.NoError(t, db.Model(&models.RelationshipEdge{}).
		Where("source_id = ? OR target_id = ?", contact.VCardUID, contact.VCardUID).Count(&edgeCount).Error)

	assert.Zero(t, memberCount, "circle_members must not outlive the purged contact")
	assert.Zero(t, tagCount, "contact_tags must not outlive the purged contact")
	assert.Zero(t, edgeCount, "relationship_edges must not outlive the purged contact")

	// The surviving contact and its own grouping rows are untouched.
	var survivor int64
	require.NoError(t, db.Model(&models.Contact{}).Where("id = ?", other.ID).Count(&survivor).Error)
	assert.Equal(t, int64(1), survivor)
}

// Soft-deleted child content past retention is purged in its own right, not
// only as a side effect of its parent contact being purged.
func TestPurgeSoftDeletedRows_PurgesSoftDeletedChildContent(t *testing.T) {
	db, userID := newPurgeDB(t)

	contact := models.Contact{UserID: userID, Firstname: "Alive"}
	require.NoError(t, db.Create(&contact).Error)

	note := models.Note{UserID: userID, ContactID: &contact.ID, Content: "old note"}
	require.NoError(t, db.Create(&note).Error)
	softDeleteAt(t, db, &models.Note{}, note.ID, time.Now().AddDate(0, 0, -60))

	fresh := models.Note{UserID: userID, ContactID: &contact.ID, Content: "recent note"}
	require.NoError(t, db.Create(&fresh).Error)
	softDeleteAt(t, db, &models.Note{}, fresh.ID, time.Now().AddDate(0, 0, -2))

	PurgeSoftDeletedRows(db, purgeConfig())

	var oldCount, freshCount, parentCount int64
	require.NoError(t, db.Unscoped().Model(&models.Note{}).Where("id = ?", note.ID).Count(&oldCount).Error)
	require.NoError(t, db.Unscoped().Model(&models.Note{}).Where("id = ?", fresh.ID).Count(&freshCount).Error)
	require.NoError(t, db.Model(&models.Contact{}).Where("id = ?", contact.ID).Count(&parentCount).Error)

	assert.Zero(t, oldCount, "a note soft-deleted past retention must be purged")
	assert.Equal(t, int64(1), freshCount, "a recently soft-deleted note must survive")
	assert.Equal(t, int64(1), parentCount, "purging a child must not touch its live parent")
}

// DELETED_RETENTION_DAYS=0 is the documented "disable the purge and keep
// soft-deleted rows forever" value (.env.example), and a negative value is a
// misconfiguration the purge service treats the same way. Without the guard the
// cutoff is computed as now minus the window, so it lands on/after now and
// `deleted_at < cutoff` matches the ENTIRE undo window — the issue #971
// data-loss bug: the daily/boot job hard-deletes every soft-deleted row.
//
// This pins that a non-positive window purges nothing. Both the daily cron and
// the boot-time Initial trigger call this function (via PurgeDeletedRows), so
// the guard covers both paths.
func TestPurgeSoftDeletedRows_NonPositiveRetentionDisablesPurge(t *testing.T) {
	for _, retention := range []int{0, -1} {
		t.Run(fmt.Sprintf("retention=%d", retention), func(t *testing.T) {
			db, userID := newPurgeDB(t)

			contact := models.Contact{UserID: userID, Firstname: "Doomed"}
			require.NoError(t, db.Create(&contact).Error)
			softDeleteAt(t, db, &models.Contact{}, contact.ID, time.Now().AddDate(0, 0, -60))

			note := models.Note{UserID: userID, ContactID: &contact.ID, Content: "old note"}
			require.NoError(t, db.Create(&note).Error)
			softDeleteAt(t, db, &models.Note{}, note.ID, time.Now().AddDate(0, 0, -60))

			other := models.Contact{UserID: userID, Firstname: "Other"}
			require.NoError(t, db.Create(&other).Error)
			require.NoError(t, db.Create(&models.RelationshipEdge{
				UserID: userID, SourceID: contact.VCardUID, TargetID: other.VCardUID,
				Type: "friend_of", Source: models.RelationshipSourceUserConfirmed,
				Confidence: 1.0, Status: models.RelationshipStatusConfirmed,
				Sensitivity: models.RelationshipSensitivityNormal,
			}).Error)

			PurgeSoftDeletedRows(db, config.Config{DeleteRetentionDays: retention})

			var contactCount, noteCount, edgeCount int64
			require.NoError(t, db.Unscoped().Model(&models.Contact{}).Where("id = ?", contact.ID).Count(&contactCount).Error)
			require.NoError(t, db.Unscoped().Model(&models.Note{}).Where("id = ?", note.ID).Count(&noteCount).Error)
			require.NoError(t, db.Model(&models.RelationshipEdge{}).
				Where("source_id = ? OR target_id = ?", contact.VCardUID, contact.VCardUID).Count(&edgeCount).Error)

			assert.Equal(t, int64(1), contactCount, "a non-positive retention must keep the soft-deleted contact")
			assert.Equal(t, int64(1), noteCount, "a non-positive retention must keep the soft-deleted child content")
			assert.Equal(t, int64(1), edgeCount, "a non-positive retention must keep the contact's edge rows")
		})
	}
}

func TestPurgeSoftDeletedRows_IsIdempotent(t *testing.T) {
	db, userID := newPurgeDB(t)

	contact := models.Contact{UserID: userID, Firstname: "Old"}
	require.NoError(t, db.Create(&contact).Error)
	softDeleteAt(t, db, &models.Contact{}, contact.ID, time.Now().AddDate(0, 0, -60))

	PurgeSoftDeletedRows(db, purgeConfig())
	PurgeSoftDeletedRows(db, purgeConfig())

	var count int64
	require.NoError(t, db.Unscoped().Model(&models.Contact{}).Count(&count).Error)
	assert.Zero(t, count)
}

// PurgeDeletedRows is the cron entry point; the job lock is what keeps two
// instances from purging concurrently. A second immediate call must be a
// no-op rather than a second pass.
func TestPurgeDeletedRows_SecondRunIsRateLimitedByJobLock(t *testing.T) {
	db, userID := newPurgeDB(t)

	first := models.Contact{UserID: userID, Firstname: "First"}
	require.NoError(t, db.Create(&first).Error)
	softDeleteAt(t, db, &models.Contact{}, first.ID, time.Now().AddDate(0, 0, -60))

	PurgeDeletedRows(db, purgeConfig())

	var count int64
	require.NoError(t, db.Unscoped().Model(&models.Contact{}).Where("id = ?", first.ID).Count(&count).Error)
	require.Zero(t, count, "the first run must actually purge")

	// A second contact goes past retention, but the lock's min-interval has
	// not elapsed, so this run must skip it entirely.
	second := models.Contact{UserID: userID, Firstname: "Second"}
	require.NoError(t, db.Create(&second).Error)
	softDeleteAt(t, db, &models.Contact{}, second.ID, time.Now().AddDate(0, 0, -60))

	PurgeDeletedRows(db, purgeConfig())

	require.NoError(t, db.Unscoped().Model(&models.Contact{}).Where("id = ?", second.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "the job lock must rate-limit a second run within the interval")
}
