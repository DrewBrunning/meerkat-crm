package services

import (
	"testing"

	apperrors "mycorrhizal/errors"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/emersion/go-webdav/carddav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- snapshot / diff primitives ---------------------------------------------

func TestSyncConflictFieldSnapshot_NormalizesEmptySlices(t *testing.T) {
	c := models.Contact{Firstname: "Ada", Lastname: "Lovelace"}
	snap := syncConflictFieldSnapshot(&c)

	assert.Equal(t, "Ada", snap[models.SyncConflictFieldFirstname])
	assert.Equal(t, "Lovelace", snap[models.SyncConflictFieldLastname])
	// An empty (nil) array must encode canonically as `[]`, never `null`, so
	// it compares equal to a `[]`-loaded JSON column.
	assert.Equal(t, "[]", snap[models.SyncConflictFieldEmail])
	assert.Equal(t, "[]", snap[models.SyncConflictFieldPhone])
	assert.Equal(t, "[]", snap[models.SyncConflictFieldCircles])
	assert.Equal(t, "", snap[models.SyncConflictFieldBirthday])
}

func TestDiffSyncConflictFields_DetectsOverwrittenLocalEdit(t *testing.T) {
	local := map[string]string{models.SyncConflictFieldPhone: `[{"value":"555-0100"}]`}
	synced := map[string]string{models.SyncConflictFieldPhone: "[]"}
	remote := map[string]string{models.SyncConflictFieldPhone: "[]"}

	conflicts := diffSyncConflictFields(local, synced, remote)
	require.Len(t, conflicts, 1)
	assert.Equal(t, models.SyncConflictFieldPhone, conflicts[0].Field)
	assert.Equal(t, `[{"value":"555-0100"}]`, conflicts[0].LocalValue)
	assert.Equal(t, "[]", conflicts[0].RemoteValue)
	assert.Equal(t, models.SyncConflictStatusPending, conflicts[0].Status)
}

func TestDiffSyncConflictFields_NoConflictOnPlainRemoteChange(t *testing.T) {
	// No local edit (local == synced): a remote change is applied silently.
	local := map[string]string{models.SyncConflictFieldEmail: "[]"}
	synced := map[string]string{models.SyncConflictFieldEmail: "[]"}
	remote := map[string]string{models.SyncConflictFieldEmail: `[{"value":"new@example.com"}]`}

	assert.Empty(t, diffSyncConflictFields(local, synced, remote))
}

func TestDiffSyncConflictFields_NoConflictWhenRemoteMatchesLocal(t *testing.T) {
	// The user edited a field to the same value the remote now carries: the
	// edit survives, nothing is lost, no notice needed.
	local := map[string]string{models.SyncConflictFieldPhone: `[{"value":"555-0100"}]`}
	synced := map[string]string{models.SyncConflictFieldPhone: "[]"}
	remote := map[string]string{models.SyncConflictFieldPhone: `[{"value":"555-0100"}]`}

	assert.Empty(t, diffSyncConflictFields(local, synced, remote))
}

// --- reconcileContactSync integration ----------------------------------------

// TestReconcileContactSyncRecordsConflictOnLocalEditOverwrite is the issue
// #395 end-to-end detection check: a local edit made through the array path
// (the modern editing surface), followed by a remote change to an unrelated
// field, must produce a ContactSyncConflict naming the field and both values
// instead of losing the edit silently.
func TestReconcileContactSyncRecordsConflictOnLocalEditOverwrite(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/carol.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "carol-uid", "Carol", "Danvers", "carol@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)

	// Local edit through the array path (the same shape the nested REST API
	// writes): a phone the remote vCard never carries.
	var editTarget models.Contact
	require.NoError(t, db.First(&editTarget, link.ContactID).Error)
	editTarget.Phones = []models.ContactPhone{{Value: "555-0100"}}
	editTarget.JobTitle = "Local Edit"
	require.NoError(t, db.Save(&editTarget).Error)

	// A remote change to an unrelated field (email) triggers a sync.
	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "carol-uid", "Carol", "Danvers", "carol.new@example.com")}
	stats, err := reconcileContactSync(db, sub, []carddav.AddressObject{second}, nil, false, "")
	require.NoError(t, err)
	require.Equal(t, ContactSyncStats{Updated: 1}, stats)

	var conflicts []models.ContactSyncConflict
	require.NoError(t, db.Where("user_id = ? AND contact_id = ?", user.ID, link.ContactID).Order("field ASC").Find(&conflicts).Error)
	require.Len(t, conflicts, 2, "phone + job_title were both local edits that got discarded")

	fields := map[string]models.ContactSyncConflict{}
	for _, conflict := range conflicts {
		fields[conflict.Field] = conflict
	}

	phone := fields[models.SyncConflictFieldPhone]
	assert.Equal(t, `[{"type":"","value":"555-0100"}]`, phone.LocalValue)
	assert.Equal(t, "[]", phone.RemoteValue)

	jobTitle := fields[models.SyncConflictFieldJobTitle]
	assert.Equal(t, "Local Edit", jobTitle.LocalValue)
	assert.Equal(t, "", jobTitle.RemoteValue)

	assert.Equal(t, sub.ID, conflicts[0].SubscriptionID)
	assert.Equal(t, models.SyncConflictStatusPending, conflicts[0].Status)

	// A re-sync of identical remote content is a no-op skip (content hash
	// unchanged), so the conflict is not spammy.
	stats, err = reconcileContactSync(db, sub, []carddav.AddressObject{second}, nil, false, "")
	require.NoError(t, err)
	require.Equal(t, ContactSyncStats{Skipped: 1}, stats)
}

// TestReconcileContactSync_CrossClientRevisionAdvances is the CON-02 (issue
// #457) cross-client integrity check: a CardDAV sync that overwrites a local
// edit (the documented full-replace policy) advances the contact's monotonic
// revision token, so a REST client still holding the pre-sync revision is
// correctly stale on its next conditional write (CON-01, ADR 0008) — the two
// mechanisms agree instead of the CardDAV overwrite being invisible to REST
// optimistic concurrency. The discarded local value is still preserved in a
// contact_sync_conflicts row for the user to restore.
func TestReconcileContactSync_CrossClientRevisionAdvances(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/dana.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "dana-uid", "Dana", "Scully", "dana@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)

	var contact models.Contact
	require.NoError(t, db.First(&contact, link.ContactID).Error)
	revAfterImport := contact.Revision

	// A REST client reads the contact here, at revAfterImport.
	contact.JobTitle = "Local Edit Only REST Knows"
	require.NoError(t, db.Save(&contact).Error)
	require.Greater(t, contact.Revision, revAfterImport, "the local REST-shaped edit bumps the revision")
	revAfterLocalEdit := contact.Revision

	// The CardDAV sync overwrites that local edit (full-replace policy).
	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "dana-uid", "Dana", "Scully", "dana.new@example.com")}
	stats, err := reconcileContactSync(db, sub, []carddav.AddressObject{second}, nil, false, "")
	require.NoError(t, err)
	require.Equal(t, ContactSyncStats{Updated: 1}, stats)

	var afterSync models.Contact
	require.NoError(t, db.First(&afterSync, link.ContactID).Error)

	// The CardDAV overwrite advanced the revision past what the REST client
	// last saw — so its next `If-Match: revAfterLocalEdit` write is stale and
	// checkIfMatch would 412 it (proven at the HTTP layer in
	// controllers/conditional_write*_test.go).
	require.Greater(t, afterSync.Revision, revAfterLocalEdit,
		"a CardDAV full-replace must advance the revision so REST optimistic concurrency sees it")
	assert.Empty(t, afterSync.JobTitle,
		"documented full-replace policy: the local edit is discarded from the contact itself")

	// The discarded local value is recoverable.
	var conflict models.ContactSyncConflict
	require.NoError(t, db.Where("contact_id = ? AND field = ?", link.ContactID, models.SyncConflictFieldJobTitle).First(&conflict).Error)
	assert.Equal(t, "Local Edit Only REST Knows", conflict.LocalValue)
}

// TestReconcileContactSyncNoConflictOnPlainRemoteChange pins that a sync
// touching only fields the user never edited surfaces no conflict.
func TestReconcileContactSyncNoConflictOnPlainRemoteChange(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/dave.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "dave-uid", "Dave", "Grohl", "dave@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "dave-uid", "Dave", "Grohl", "dave.new@example.com")}
	_, err = reconcileContactSync(db, sub, []carddav.AddressObject{second}, nil, false, "")
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Model(&models.ContactSyncConflict{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Zero(t, count, "a remote-only change must not be reported as a conflict")
}

// TestReconcileContactSyncNoBaselineNoConflicts covers links that predate
// migration 000032 (SyncedValues empty): the first sync establishes the
// baseline without retroactively reporting conflicts, because there is no
// way to distinguish a local edit from prior sync state.
func TestReconcileContactSyncNoBaselineNoConflicts(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/erin.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "erin-uid", "Erin", "Esurance", "erin@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)
	assert.NotEmpty(t, link.SyncedValues, "a freshly-created link gets a baseline on its first sync")

	// Simulate a pre-migration link with no baseline.
	require.NoError(t, db.Model(&link).Update("synced_values", "").Error)

	var editTarget models.Contact
	require.NoError(t, db.First(&editTarget, link.ContactID).Error)
	editTarget.Phones = []models.ContactPhone{{Value: "555-0100"}}
	require.NoError(t, db.Save(&editTarget).Error)

	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "erin-uid", "Erin", "Esurance", "erin.new@example.com")}
	_, err = reconcileContactSync(db, sub, []carddav.AddressObject{second}, nil, false, "")
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Model(&models.ContactSyncConflict{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Zero(t, count, "a missing baseline must not produce retroactive conflicts")

	require.NoError(t, db.First(&link, link.ID).Error)
	assert.NotEmpty(t, link.SyncedValues, "the sync must re-establish the baseline")
}

// TestCapturePreSyncBaselines covers the issue #919 pre-fetch capture: links
// with a parseable stored baseline are skipped (no extra contact read), while
// links whose baseline is empty or corrupt are captured by contact ID, scoped
// to the subscription.
func TestCapturePreSyncBaselines(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")
	otherSub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/other/", "", "")

	good := models.Contact{UserID: user.ID, Firstname: "Good", JobTitle: "Engineer"}
	missing := models.Contact{UserID: user.ID, Firstname: "Missing", JobTitle: "Doctor"}
	corrupt := models.Contact{UserID: user.ID, Firstname: "Corrupt", JobTitle: "Lawyer"}
	foreign := models.Contact{UserID: user.ID, Firstname: "Foreign", JobTitle: "Pilot"}
	for _, c := range []*models.Contact{&good, &missing, &corrupt, &foreign} {
		require.NoError(t, db.Create(c).Error)
	}

	require.NoError(t, db.Create(&models.ContactSyncLink{
		SubscriptionID: sub.ID, UserID: user.ID, Href: "/good.vcf", ContactID: good.ID,
		ContentHash: "h-good", SyncedValues: syncConflictSnapshotJSON(syncConflictFieldSnapshot(&good)),
	}).Error)
	require.NoError(t, db.Create(&models.ContactSyncLink{
		SubscriptionID: sub.ID, UserID: user.ID, Href: "/missing.vcf", ContactID: missing.ID,
		ContentHash: "h-missing", SyncedValues: "",
	}).Error)
	require.NoError(t, db.Create(&models.ContactSyncLink{
		SubscriptionID: sub.ID, UserID: user.ID, Href: "/corrupt.vcf", ContactID: corrupt.ID,
		ContentHash: "h-corrupt", SyncedValues: "{not-json",
	}).Error)
	require.NoError(t, db.Create(&models.ContactSyncLink{
		SubscriptionID: otherSub.ID, UserID: user.ID, Href: "/foreign.vcf", ContactID: foreign.ID,
		ContentHash: "h-foreign", SyncedValues: "",
	}).Error)

	baselines, err := capturePreSyncBaselines(db, sub)
	require.NoError(t, err)
	require.Len(t, baselines, 2, "only the empty + corrupt links need a fallback baseline")
	assert.Equal(t, "Doctor", baselines[missing.ID][models.SyncConflictFieldJobTitle])
	assert.Equal(t, "Lawyer", baselines[corrupt.ID][models.SyncConflictFieldJobTitle])
	_, hasGood := baselines[good.ID]
	assert.False(t, hasGood, "a parseable baseline needs no pre-fetch snapshot")
	_, hasForeign := baselines[foreign.ID]
	assert.False(t, hasForeign, "another subscription's links must not leak in")

	// Every link parseable: nil, so the common-case sync pays no contact read.
	require.NoError(t, db.Model(&models.ContactSyncLink{}).
		Where("subscription_id = ?", sub.ID).Updates(map[string]any{
		"synced_values": syncConflictSnapshotJSON(syncConflictFieldSnapshot(&good)),
	}).Error)
	empty, err := capturePreSyncBaselines(db, sub)
	require.NoError(t, err)
	assert.Nil(t, empty)
}

// TestReconcileContactSyncMissingBaselineConcurrentEdit is the issue #919
// core-property test: a legacy link with no stored baseline, whose contact is
// edited during the network-fetch window. The pre-fetch snapshot captured
// before the edit is used as this run's baseline, so the overwritten edit is
// recorded — and, crucially, the unrelated remote change that applied
// silently is *not* misreported as a conflict.
func TestReconcileContactSyncMissingBaselineConcurrentEdit(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/legacy.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "legacy-uid", "Grace", "Hopper", "grace@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)
	// Legacy shape: synced before migration 000032, no baseline.
	require.NoError(t, db.Model(&link).Update("synced_values", "").Error)

	// Pre-fetch capture happens here, before the concurrent edit.
	preBaselines, err := capturePreSyncBaselines(db, sub)
	require.NoError(t, err)
	require.Len(t, preBaselines, 1)

	// The concurrent REST edit lands during the network fetch.
	var contact models.Contact
	require.NoError(t, db.First(&contact, link.ContactID).Error)
	contact.JobTitle = "Concurrent Edit"
	contact.Phones = []models.ContactPhone{{Value: "555-0100"}}
	require.NoError(t, db.Save(&contact).Error)

	// The remote change (email) triggers the full-replace update.
	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "legacy-uid", "Grace", "Hopper", "grace.new@example.com")}
	stats, err := reconcileContactSyncWithBaselines(db, sub, []carddav.AddressObject{second}, nil, false, "", preBaselines)
	require.NoError(t, err)
	require.Equal(t, ContactSyncStats{Updated: 1}, stats)

	conflicts := map[string]models.ContactSyncConflict{}
	var rows []models.ContactSyncConflict
	require.NoError(t, db.Where("user_id = ? AND contact_id = ?", user.ID, link.ContactID).Find(&rows).Error)
	for _, c := range rows {
		conflicts[c.Field] = c
	}
	require.Contains(t, conflicts, models.SyncConflictFieldJobTitle, "the concurrent edit must be surfaced")
	assert.Equal(t, "Concurrent Edit", conflicts[models.SyncConflictFieldJobTitle].LocalValue)
	assert.Equal(t, "", conflicts[models.SyncConflictFieldJobTitle].RemoteValue)
	require.Contains(t, conflicts, models.SyncConflictFieldPhone)
	assert.Equal(t, `[{"type":"","value":"555-0100"}]`, conflicts[models.SyncConflictFieldPhone].LocalValue)
	assert.NotContains(t, conflicts, models.SyncConflictFieldEmail,
		"the plain remote email change must not be reported as a conflict")

	var after models.Contact
	require.NoError(t, db.First(&after, link.ContactID).Error)
	assert.Equal(t, "grace.new@example.com", after.Email, "the remote change still applies (full-replace)")

	var reloadedLink models.ContactSyncLink
	require.NoError(t, db.First(&reloadedLink, link.ID).Error)
	assert.NotEmpty(t, reloadedLink.SyncedValues, "the sync re-establishes the baseline")
}

// TestReconcileContactSyncCorruptBaselineConcurrentEdit is the same property
// for the corrupt-baseline shape: an unparseable stored baseline also falls
// back to the pre-fetch snapshot instead of silently dropping the edit.
func TestReconcileContactSyncCorruptBaselineConcurrentEdit(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/corrupt.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "corrupt-uid", "Ada", "Lovelace", "ada@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)
	require.NoError(t, db.Model(&link).Update("synced_values", "{not-json").Error)

	preBaselines, err := capturePreSyncBaselines(db, sub)
	require.NoError(t, err)
	require.Len(t, preBaselines, 1)

	var contact models.Contact
	require.NoError(t, db.First(&contact, link.ContactID).Error)
	contact.JobTitle = "Concurrent Edit"
	require.NoError(t, db.Save(&contact).Error)

	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "corrupt-uid", "Ada", "Lovelace", "ada.new@example.com")}
	_, err = reconcileContactSyncWithBaselines(db, sub, []carddav.AddressObject{second}, nil, false, "", preBaselines)
	require.NoError(t, err)

	var conflict models.ContactSyncConflict
	require.NoError(t, db.Where("contact_id = ? AND field = ?", link.ContactID, models.SyncConflictFieldJobTitle).First(&conflict).Error)
	assert.Equal(t, "Concurrent Edit", conflict.LocalValue)
}

// TestReconcileContactSyncMissingBaselinePlainRemoteChangeNoConflict is the
// anti-spam regression guard: when a legacy link's contact was *not* edited
// during the fetch, the pre-fetch fallback baseline makes the remote change a
// plain apply, not a retroactive conflict (the design boundary the fix keeps).
func TestReconcileContactSyncMissingBaselinePlainRemoteChangeNoConflict(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/plain.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "plain-uid", "Erin", "Gray", "erin@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)
	require.NoError(t, db.Model(&link).Update("synced_values", "").Error)

	preBaselines, err := capturePreSyncBaselines(db, sub)
	require.NoError(t, err)
	require.Len(t, preBaselines, 1)

	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "plain-uid", "Erin", "Gray", "erin.new@example.com")}
	_, err = reconcileContactSyncWithBaselines(db, sub, []carddav.AddressObject{second}, nil, false, "", preBaselines)
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Model(&models.ContactSyncConflict{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Zero(t, count, "a remote-only change on a legacy link must not be flagged as a conflict")

	var after models.Contact
	require.NoError(t, db.First(&after, link.ContactID).Error)
	assert.Equal(t, "erin.new@example.com", after.Email)
}

// TestContactSyncLinkSyncedValuesSavesAgainstRealMigratedSchema guards the
// new column + table against GORM/ migration drift the AutoMigrate-based test
// DBs cannot see (CLAUDE.md backend trap 1).
func TestContactSyncLinkSyncedValuesSavesAgainstRealMigratedSchema(t *testing.T) {
	db := dbtest.New(t)

	user := createContactSyncTestUser(t, db)
	cfg := contactSyncTestConfig()
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Bob"}
	require.NoError(t, db.Create(&contact).Error)

	link := models.ContactSyncLink{
		SubscriptionID: sub.ID,
		UserID:         user.ID,
		Href:           "/addressbooks/test/bob.vcf",
		ContactID:      contact.ID,
		ContentHash:    "hash-1",
		SyncedValues:   `{"phone":"[]"}`,
	}
	require.NoError(t, db.Create(&link).Error)

	var reloaded models.ContactSyncLink
	require.NoError(t, db.First(&reloaded, link.ID).Error)
	assert.Equal(t, `{"phone":"[]"}`, reloaded.SyncedValues)

	// And a conflict row round-trips against the real schema too.
	conflict := models.ContactSyncConflict{
		UserID: user.ID, SubscriptionID: sub.ID, ContactID: contact.ID,
		Field: models.SyncConflictFieldPhone, LocalValue: `[{"value":"555-0100"}]`,
		RemoteValue: "[]", Status: models.SyncConflictStatusPending,
	}
	require.NoError(t, db.Create(&conflict).Error)
	require.NotEmpty(t, conflict.ID)

	var reloadedConflict models.ContactSyncConflict
	require.NoError(t, db.First(&reloadedConflict, "id = ?", conflict.ID).Error)
	assert.Equal(t, `[{"value":"555-0100"}]`, reloadedConflict.LocalValue)
}

// --- list / restore / dismiss -------------------------------------------------

func seedSyncConflict(t *testing.T, db *gorm.DB, userID, contactID, subID uint, field, local, remote string) models.ContactSyncConflict {
	t.Helper()
	conflict := models.ContactSyncConflict{
		UserID: userID, SubscriptionID: subID, ContactID: contactID,
		Field: field, LocalValue: local, RemoteValue: remote, Status: models.SyncConflictStatusPending,
	}
	require.NoError(t, db.Create(&conflict).Error)
	return conflict
}

// TestConflictPolicy_CardDAVDetectSurfaceRestore_EndToEnd is the CON-03 (issue
// #458) pin for the CardDAV side of docs/adrs/0009-rest-conflict-policy.md: the
// full-replace path stays a *detect-and-surface* policy — the overwritten local
// value is recorded in a contact_sync_conflicts row that ListContactSyncConflicts
// surfaces and RestoreContactSyncConflict can put back. This is deliberately the
// opposite of the REST reject-and-return policy, and must not regress.
func TestConflictPolicy_CardDAVDetectSurfaceRestore_EndToEnd(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	href := "/addressbooks/test/erin.vcf"
	first := carddav.AddressObject{Path: href, ETag: "\"etag-1\"", Card: testCard(t, "erin-uid", "Erin", "Gray", "erin@example.com")}
	_, err := reconcileContactSync(db, sub, []carddav.AddressObject{first}, nil, false, "")
	require.NoError(t, err)

	var link models.ContactSyncLink
	require.NoError(t, db.Where("subscription_id = ? AND href = ?", sub.ID, href).First(&link).Error)

	// Local edit to a scalar field the remote never carries.
	var contact models.Contact
	require.NoError(t, db.First(&contact, link.ContactID).Error)
	contact.JobTitle = "Chief Widget Officer"
	require.NoError(t, db.Save(&contact).Error)

	// Remote change triggers the full-replace, discarding the local job title.
	second := carddav.AddressObject{Path: href, ETag: "\"etag-2\"", Card: testCard(t, "erin-uid", "Erin", "Gray", "erin.new@example.com")}
	_, err = reconcileContactSync(db, sub, []carddav.AddressObject{second}, nil, false, "")
	require.NoError(t, err)

	// Detect + surface: the discarded value is on a conflict row the list API returns.
	surfaced, err := ListContactSyncConflicts(db, user.ID)
	require.NoError(t, err)
	require.Len(t, surfaced, 1)
	assert.Equal(t, models.SyncConflictFieldJobTitle, surfaced[0].Field)
	assert.Equal(t, "Chief Widget Officer", surfaced[0].LocalValue)

	// Restore puts it back.
	require.NoError(t, RestoreContactSyncConflict(db, user.ID, surfaced[0].ID))
	var restored models.Contact
	require.NoError(t, db.First(&restored, link.ContactID).Error)
	assert.Equal(t, "Chief Widget Officer", restored.JobTitle)
}

func TestListContactSyncConflicts_EnrichesAndFilters(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Ada", Lastname: "Lovelace"}
	require.NoError(t, db.Create(&contact).Error)
	archived := models.Contact{UserID: user.ID, Firstname: "Archived", Archived: true}
	require.NoError(t, db.Create(&archived).Error)

	pending := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldJobTitle, "Local", "Remote")
	dismissed := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldPhone, "X", "Y")
	require.NoError(t, db.Model(&dismissed).Update("status", models.SyncConflictStatusDismissed).Error)
	seedSyncConflict(t, db, user.ID, archived.ID, sub.ID, models.SyncConflictFieldPhone, "X", "Y")

	conflicts, err := ListContactSyncConflicts(db, user.ID)
	require.NoError(t, err)
	require.Len(t, conflicts, 1, "dismissed + archived-contact conflicts must not surface")
	assert.Equal(t, pending.ID, conflicts[0].ID)
	assert.Equal(t, contact.ID, conflicts[0].ContactID)
	assert.Equal(t, contact.VCardUID, conflicts[0].ContactVCardUID)
	assert.Equal(t, "Ada Lovelace", conflicts[0].ContactName)
	assert.Equal(t, sub.Name, conflicts[0].SubscriptionName)
}

func TestListContactSyncConflicts_ScopedToOwner(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	other := models.User{Username: "other", Password: "password123!A", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Mine"}
	require.NoError(t, db.Create(&contact).Error)
	seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldPhone, "X", "Y")

	otherConflicts, err := ListContactSyncConflicts(db, other.ID)
	require.NoError(t, err)
	assert.Nil(t, otherConflicts, "another user must see nothing")

	myConflicts, err := ListContactSyncConflicts(db, user.ID)
	require.NoError(t, err)
	require.Len(t, myConflicts, 1)
}

func TestRestoreContactSyncConflict_RestoresScalarAndArrayFields(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol", Lastname: "Danvers"}
	require.NoError(t, db.Create(&contact).Error)

	jobTitle := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldJobTitle, "Local Title", "Remote Title")
	phone := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldPhone, `[{"type":"","value":"555-0100"}]`, "[]")

	require.NoError(t, RestoreContactSyncConflict(db, user.ID, jobTitle.ID))
	require.NoError(t, RestoreContactSyncConflict(db, user.ID, phone.ID))

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.Equal(t, "Local Title", reloaded.JobTitle)
	require.Len(t, reloaded.Phones, 1)
	assert.Equal(t, "555-0100", reloaded.Phones[0].Value)
	assert.Equal(t, "555-0100", reloaded.Phone, "the denormalized scalar follows the restored array via BeforeSave")

	for _, id := range []string{jobTitle.ID, phone.ID} {
		var conflict models.ContactSyncConflict
		require.NoError(t, db.First(&conflict, "id = ?", id).Error)
		assert.Equal(t, models.SyncConflictStatusDismissed, conflict.Status)
	}
}

func TestRestoreContactSyncConflict_AlreadyDismissedIsConflict(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&contact).Error)
	conflict := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldJobTitle, "A", "B")
	require.NoError(t, db.Model(&conflict).Update("status", models.SyncConflictStatusDismissed).Error)

	err := RestoreContactSyncConflict(db, user.ID, conflict.ID)
	require.Error(t, err)
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrCodeConflict, appErr.Code)
}

func TestRestoreContactSyncConflict_UnknownOrForeignIs404(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	other := models.User{Username: "other", Password: "password123!A", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&contact).Error)
	foreign := seedSyncConflict(t, db, other.ID, contact.ID, sub.ID, models.SyncConflictFieldPhone, "A", "B")

	err := RestoreContactSyncConflict(db, user.ID, foreign.ID)
	require.Error(t, err)
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrCodeNotFound, appErr.Code)

	err = RestoreContactSyncConflict(db, user.ID, "does-not-exist")
	require.Error(t, err)
	appErr, ok = err.(*apperrors.AppError)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrCodeNotFound, appErr.Code)
}

func TestDismissContactSyncConflict_IdempotentAndScoped(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	other := models.User{Username: "other", Password: "password123!A", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&contact).Error)
	conflict := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldPhone, "A", "B")

	require.NoError(t, DismissContactSyncConflict(db, user.ID, conflict.ID))
	require.NoError(t, DismissContactSyncConflict(db, user.ID, conflict.ID), "dismiss is idempotent")

	var reloaded models.ContactSyncConflict
	require.NoError(t, db.First(&reloaded, "id = ?", conflict.ID).Error)
	assert.Equal(t, models.SyncConflictStatusDismissed, reloaded.Status)

	err := DismissContactSyncConflict(db, other.ID, conflict.ID)
	require.Error(t, err)
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrCodeNotFound, appErr.Code)

	err = DismissContactSyncConflict(db, user.ID, "does-not-exist")
	require.Error(t, err)
	appErr, ok = err.(*apperrors.AppError)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrCodeNotFound, appErr.Code)
}

// TestRestoreContactSyncConflict_RestoresEveryField exercises the full
// restore switch, scalar and array fields alike — the inverse of the snapshot
// encoding, so a value a sync overwrote can be written back verbatim.
func TestRestoreContactSyncConflict_RestoresEveryField(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Initial"}
	require.NoError(t, db.Create(&contact).Error)

	cases := []struct {
		field, local string
	}{
		{models.SyncConflictFieldFirstname, "Ada"},
		{models.SyncConflictFieldLastname, "Lovelace"},
		{models.SyncConflictFieldMiddlename, "Augusta"},
		{models.SyncConflictFieldPrefix, "Dr."},
		{models.SyncConflictFieldSuffix, "PhD"},
		{models.SyncConflictFieldNickname, "Ace"},
		{models.SyncConflictFieldOrganization, "Analytical Engines"},
		{models.SyncConflictFieldDepartment, "Research"},
		{models.SyncConflictFieldJobTitle, "Analyst"},
		{models.SyncConflictFieldRole, "Lead"},
		{models.SyncConflictFieldBirthday, "1815-12-10"},
		{models.SyncConflictFieldAnniversary, "1835-07-08"},
		{models.SyncConflictFieldHowWeMet, "At the museum"},
		{models.SyncConflictFieldWorkInformation, "Writes code"},
		{models.SyncConflictFieldContactInformation, "Reach via assistant"},
		{models.SyncConflictFieldEmail, `[{"type":"work","value":"ada@example.com"}]`},
		{models.SyncConflictFieldPhone, `[{"type":"","value":"555-0100"}]`},
		{models.SyncConflictFieldAddress, `[{"street":"St James Sq","city":"London"}]`},
		{models.SyncConflictFieldURL, `[{"type":"","value":"https://example.com"}]`},
		{models.SyncConflictFieldIMPP, `[{"type":"telegram","value":"@ada"}]`},
		{models.SyncConflictFieldCircles, `["close_friends"]`},
	}
	for _, tc := range cases {
		conflict := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, tc.field, tc.local, "Remote")
		require.NoError(t, RestoreContactSyncConflict(db, user.ID, conflict.ID), "restore %s", tc.field)
	}

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)

	assert.Equal(t, "Ada", reloaded.Firstname)
	assert.Equal(t, "Lovelace", reloaded.Lastname)
	assert.Equal(t, "Augusta", reloaded.MiddleName)
	assert.Equal(t, "Dr.", reloaded.Prefix)
	assert.Equal(t, "PhD", reloaded.Suffix)
	assert.Equal(t, "Ace", reloaded.Nickname)
	assert.Equal(t, "Analytical Engines", reloaded.Organization)
	assert.Equal(t, "Research", reloaded.Department)
	assert.Equal(t, "Analyst", reloaded.JobTitle)
	assert.Equal(t, "Lead", reloaded.Role)
	assert.Equal(t, "1815-12-10", reloaded.Birthday)
	assert.Equal(t, "1835-07-08", reloaded.Anniversary)
	assert.Equal(t, "At the museum", reloaded.HowWeMet)
	assert.Equal(t, "Writes code", reloaded.WorkInformation)
	assert.Equal(t, "Reach via assistant", reloaded.ContactInformation)
	require.Len(t, reloaded.Emails, 1)
	assert.Equal(t, "ada@example.com", reloaded.Emails[0].Value)
	require.Len(t, reloaded.Phones, 1)
	assert.Equal(t, "555-0100", reloaded.Phones[0].Value)
	require.Len(t, reloaded.Addresses, 1)
	assert.Equal(t, "St James Sq", reloaded.Addresses[0].Street)
	require.Len(t, reloaded.URLs, 1)
	assert.Equal(t, "https://example.com", reloaded.URLs[0].Value)
	require.Len(t, reloaded.IMPPs, 1)
	assert.Equal(t, "@ada", reloaded.IMPPs[0].Value)
	assert.Equal(t, []string{"close_friends"}, reloaded.Circles)
}

// TestRestoreContactSyncConflict_ContactMissingIs404 covers a conflict whose
// contact was deleted after the conflict was recorded: nothing to restore.
func TestRestoreContactSyncConflict_ContactMissingIs404(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	conflict := seedSyncConflict(t, db, user.ID, 99999, sub.ID, models.SyncConflictFieldPhone, "A", "B")

	err := RestoreContactSyncConflict(db, user.ID, conflict.ID)
	require.Error(t, err)
	appErr, ok := err.(*apperrors.AppError)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrCodeNotFound, appErr.Code)
}

// TestRestoreContactSyncConflict_UnknownFieldFails covers the restore switch's
// default branch: a field token we don't know how to write back.
func TestRestoreContactSyncConflict_UnknownFieldFails(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&contact).Error)
	conflict := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, "not_a_real_field", "A", "B")

	err := RestoreContactSyncConflict(db, user.ID, conflict.ID)
	require.Error(t, err)

	var reloaded models.Contact
	require.NoError(t, db.First(&reloaded, contact.ID).Error)
	assert.Equal(t, "Carol", reloaded.Firstname, "an unknown field must not half-restore the contact")
}

// TestRecordSyncConflicts_NoBaselineAndCorruptBaseline covers the two
// defensive paths when there is *no* pre-sync fallback: a missing baseline is
// a silent no-op (pre-migration links), and a corrupt baseline is skipped with
// a warning rather than failing the sync. The fallback that turns a
// missing/corrupt baseline into real detection is exercised by
// TestReconcileContactSyncMissingBaselineConcurrentEdit and the pre-fetch
// capture tests.
func TestRecordSyncConflicts_NoBaselineAndCorruptBaseline(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&contact).Error)

	local := map[string]string{models.SyncConflictFieldPhone: `[{"value":"555-0100"}]`}
	remote := map[string]string{models.SyncConflictFieldPhone: "[]"}

	// No baseline and no fallback: nothing to diff against, no conflicts.
	require.NoError(t, recordSyncConflicts(db, sub, &contact, models.ContactSyncLink{SyncedValues: ""}, local, remote, nil))
	var count int64
	require.NoError(t, db.Model(&models.ContactSyncConflict{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Zero(t, count)

	// Corrupt baseline and no fallback: skipped with a warning, sync still
	// succeeds.
	require.NoError(t, recordSyncConflicts(db, sub, &contact, models.ContactSyncLink{SyncedValues: "{not-json"}, local, remote, nil))
	require.NoError(t, db.Model(&models.ContactSyncConflict{}).Where("user_id = ?", user.ID).Count(&count).Error)
	assert.Zero(t, count)
}

// TestSyncConflictSnapshotJSON_RoundTrip covers the snapshot encode/decode
// helpers directly.
func TestSyncConflictSnapshotJSON_RoundTrip(t *testing.T) {
	snap := map[string]string{
		models.SyncConflictFieldPhone:    `[{"type":"","value":"555-0100"}]`,
		models.SyncConflictFieldNickname: "Ace",
	}
	raw := syncConflictSnapshotJSON(snap)
	assert.NotEmpty(t, raw)

	parsed, err := parseSyncConflictSnapshot(raw)
	require.NoError(t, err)
	assert.Equal(t, snap, parsed)
}

// TestRestoreContactSyncConflict_InvalidArrayJSON covers the array-field
// unmarshal error branches: a corrupted stored value must fail the restore
// without touching the contact.
func TestRestoreContactSyncConflict_InvalidArrayJSON(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	for _, field := range []string{
		models.SyncConflictFieldEmail,
		models.SyncConflictFieldPhone,
		models.SyncConflictFieldAddress,
		models.SyncConflictFieldURL,
		models.SyncConflictFieldIMPP,
		models.SyncConflictFieldCircles,
	} {
		contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
		require.NoError(t, db.Create(&contact).Error)
		conflict := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, field, "{not-json", "[]")

		err := RestoreContactSyncConflict(db, user.ID, conflict.ID)
		require.Error(t, err, "restore of %s with invalid JSON must fail", field)

		var reloaded models.Contact
		require.NoError(t, db.First(&reloaded, contact.ID).Error)
		assert.Equal(t, "Carol", reloaded.Firstname, "a failed restore must leave the contact untouched")
	}
}

// TestSyncConflictServices_DBError exercises the error branches of the list /
// restore / dismiss service functions by closing the underlying *sql.DB out
// from under gorm (mirrors the controller-level DB-error tests).
func TestSyncConflictServices_DBError(t *testing.T) {
	db := setupContactSyncTestDB(t)
	cfg := contactSyncTestConfig()
	user := createContactSyncTestUser(t, db)
	sub := newContactTestSubscription(t, db, cfg, user.ID, "https://example.com/addressbooks/test/", "", "")

	contact := models.Contact{UserID: user.ID, Firstname: "Carol"}
	require.NoError(t, db.Create(&contact).Error)
	conflict := seedSyncConflict(t, db, user.ID, contact.ID, sub.ID, models.SyncConflictFieldPhone, "A", "B")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = ListContactSyncConflicts(db, user.ID)
	require.Error(t, err)

	err = RestoreContactSyncConflict(db, user.ID, conflict.ID)
	require.Error(t, err)

	err = DismissContactSyncConflict(db, user.ID, conflict.ID)
	require.Error(t, err)
}
