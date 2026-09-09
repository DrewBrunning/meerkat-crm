package controllers

// TestDeleteCascadeCoverage is the delete-cascade completeness pin (issue
// #611): every table in the real migrated schema must have a declared deletion
// policy, in the same self-maintaining, fail-in-both-directions shape as
// routes/authorization_matrix_test.go.
//
// CLAUDE.md backend trap #6 is that cascade deletes are manual — DeleteContact
// and DeleteUser each enumerate their dependent tables explicitly, and an audit
// once found 14 tables those enumerations had missed. Nothing failed when that
// happened, because nothing compared the enumerations to the schema. This test
// closes that hole:
//
//   - the schema is the subject list (enumerated from a real
//     database.InitDB-migrated schema, never AutoMigrate — trap #1);
//   - every table must be declared in exactly one bucket, in the table below;
//   - a schema table with no declared bucket FAILS ("a new table needs a
//     declared deletion policy"), and a declared row with no matching schema
//     table FAILS as stale — so the check cannot rot in either direction;
//   - a table declared `fk-cascade-user` must actually carry an `ON DELETE
//     CASCADE` foreign key to `users` in the schema — the declaration's word
//     is not enough;
//   - a table declared `fk-cascade-*` must not rely on a cascade from a
//     soft-deleted parent (trap #6's asymmetry: soft delete never fires SQL
//     CASCADE), so a contact-scoped table declared `fk-cascade-user` fails
//     outright;
//   - a table whose schema FK cascade points at `contacts` (soft-deleted) must
//     be declared `go-cascade-contact` — the only mechanism that actually
//     cleans it on contact delete.
//
// The two behavioral sweeps below (DeleteContact / DeleteUser) then verify the
// declared contact/user-scoped enumerations actually empty every declared
// table, which is what catches a line *removed* from either function.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// cascadeBucket is the declared deletion policy for a table.
type cascadeBucket int

const (
	// goCascadeUser: the table's rows are deleted by DeleteUser's enumeration.
	goCascadeUser cascadeBucket = iota
	// goCascadeContact: the table's rows are deleted by deleteContactAssociations.
	goCascadeContact
	// fkCascadeUser: the table's rows are removed by the `ON DELETE CASCADE`
	// foreign key from the hard-deleted `users` row (DeleteUser's Unscoped),
	// and the table is NOT in either manual enumeration. The FK must be
	// asserted to exist — the declaration is not the schema's word.
	fkCascadeUser
	// exempt: the table needs no deletion policy — FTS5 shadow tables,
	// infrastructure, or deliberately retained. Requires a written reason.
	exempt
)

// declaredCascadeCoverage is the authoritative table→bucket classification.
// Every key must exist in the migrated schema and every schema table must be
// keyed here (both directions enforced by the test).
//
// The `users` row itself is goCascadeUser (DeleteUser's Unscoped removes it);
// `contacts` is goCascadeUser because its rows are hard-deleted by DeleteUser
// and soft-deleted by DeleteContact — neither is "deleteContactAssociations",
// which removes a contact's *associations*, never the contact row.
//
// Soft-delete note: `contacts` is the only soft-deleted parent a cascade could
// (wrongly) be attached to. Tables that genuinely carry an `ON DELETE CASCADE`
// FK to `contacts` in the schema (activity_contacts, notes, reminders,
// reminder_completions) are declared goCascadeContact below, and the test
// asserts that invariant — a cascade to a soft-deleted parent never fires.
var declaredCascadeCoverage = map[string]cascadeBucket{
	// --- user-scoped, enumerated in DeleteUser --------------------------
	"activities":             goCascadeUser,
	"api_tokens":             goCascadeUser,
	"calendar_event_links":   goCascadeUser,
	"calendar_subscriptions": goCascadeUser,
	"carddav_sync":           goCascadeUser,
	"circles":                goCascadeUser,
	"contact_shares":         goCascadeUser,
	"contact_subscriptions":  goCascadeUser,
	"contacts":               goCascadeUser,
	"device_grants":          goCascadeUser,
	"device_registrations":   goCascadeUser,
	"field_definitions":      goCascadeUser,
	"households":             goCascadeUser,
	"idempotency_keys":       goCascadeUser,
	"immich_configs":         goCascadeUser,
	"import_runs":            goCascadeUser,
	"import_source_links":    fkCascadeUser,
	"link_field_types":       goCascadeUser,
	"notification_configs":   goCascadeUser,
	"paperless_configs":      goCascadeUser,
	"push_subscriptions":     goCascadeUser,
	"reach_out_cursors":      goCascadeUser,
	"recovery_codes":         goCascadeUser,
	"seafile_configs":        goCascadeUser,
	"sessions":               goCascadeUser,
	"tags":                   goCascadeUser,
	"users":                  goCascadeUser,
	"webdav_configs":         goCascadeUser,
	"webhooks":               goCascadeUser,
	"webhook_deliveries":     goCascadeUser,
	// --- contact-scoped, enumerated in deleteContactAssociations ---------
	"activity_contacts":         goCascadeContact,
	"attachments":               goCascadeContact,
	"cadence_policies":          goCascadeContact,
	"circle_members":            goCascadeContact,
	"contact_sync_conflicts":    goCascadeContact,
	"contact_sync_links":        goCascadeContact,
	"contact_tags":              goCascadeContact,
	"conversation_agenda":       goCascadeContact,
	"dismissed_duplicate_pairs": goCascadeContact,
	"external_activities":       goCascadeContact,
	"external_identities":       goCascadeContact,
	"field_values":              goCascadeContact,
	"gifts":                     goCascadeContact,
	"household_members":         goCascadeContact,
	"life_events":               goCascadeContact,
	"notes":                     goCascadeContact,
	"notification_deliveries":   goCascadeContact,
	"preferences":               goCascadeContact,
	"reach_out_suggestions":     goCascadeContact,
	"relationship_edges":        goCascadeContact,
	"reminder_completions":      goCascadeContact,
	"reminders":                 goCascadeContact,
	// --- rely on FK CASCADE from the hard-deleted users row ---------------
	"audit_events":                    fkCascadeUser, // FK user_id → users CASCADE, not in DeleteUser
	"dismissed_household_suggestions": fkCascadeUser, // FK user_id → users CASCADE, not in DeleteUser
	// --- exempt: FTS5 shadow tables --------------------------------------
	"activities_fts":         exempt,
	"activities_fts_config":  exempt,
	"activities_fts_content": exempt,
	"activities_fts_data":    exempt,
	"activities_fts_docsize": exempt,
	"activities_fts_idx":     exempt,
	"contacts_fts":           exempt,
	"contacts_fts_config":    exempt,
	"contacts_fts_content":   exempt,
	"contacts_fts_data":      exempt,
	"contacts_fts_docsize":   exempt,
	"contacts_fts_idx":       exempt,
	"notes_fts":              exempt,
	"notes_fts_config":       exempt,
	"notes_fts_content":      exempt,
	"notes_fts_data":         exempt,
	"notes_fts_docsize":      exempt,
	"notes_fts_idx":          exempt,
	// --- exempt: infrastructure / operational, no user-data parent --------
	"alert_states":              exempt, // alerting state (issue #427/#428), no user FK
	"data_backfills":            exempt, // one-shot maintenance-backfill ledger (issue #485), no user FK, no user data
	"data_encryption_keys":      exempt, // at-rest DEK envelope (issue #380), no user FK
	"job_executions":            exempt, // scheduler bookkeeping, no user FK
	"job_runs":                  exempt, // background-job run history (issue #391), admin-only, retention-purged
	"operational_check_results": exempt, // DB-integrity diagnostics (issue #273), no user FK
	"schema_migrations":         exempt, // migration bookkeeping
	"server_settings":           exempt, // instance-wide settings, no user FK
	"storage_samples":           exempt, // storage-growth history (issue #652), no user FK, retention-purged
	"system_events":             exempt, // operational diagnostics (issue #424), admin-only, retention-purged
}

func TestDeleteCascadeCoverage(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	schemaTables := schemaTables(t, db)

	// --- bidirectional completeness --------------------------------------
	var missing, stale []string
	for tb := range schemaTables {
		if _, ok := declaredCascadeCoverage[tb]; !ok {
			missing = append(missing, tb)
		}
	}
	for name := range declaredCascadeCoverage {
		if !schemaTables[name] {
			stale = append(stale, name)
		}
	}
	require.Empty(t, missing,
		"schema tables with no declared deletion policy — add a bucket to declaredCascadeCoverage:\n  %v", missing)
	require.Empty(t, stale,
		"declared deletion policies with no matching schema table:\n  %v", stale)

	// --- the soft-delete asymmetry (trap #6) ------------------------------
	// softDeletedParents are tables whose rows are soft-deleted (have a
	// deleted_at column) — a cascade to any of them never fires. users is the
	// single deliberate hard-delete exception (DeleteUser Unscoped, T26), so
	// it is the only legitimate cascade parent.
	softDeleted := map[string]bool{}
	for tb := range schemaTables {
		if tableHasColumn(t, db, tb, "deleted_at") && tb != "users" {
			softDeleted[tb] = true
		}
	}

	// fk-cascade-user tables: the FK must actually exist, and must cascade
	// only from users (never from a soft-deleted parent).
	for name, bucket := range declaredCascadeCoverage {
		if bucket != fkCascadeUser {
			continue
		}
		fks := foreignKeys(t, db, name)
		var cascadesToUsers bool
		for _, fk := range fks {
			if fk.parent == "users" && fk.onDelete == "CASCADE" {
				cascadesToUsers = true
			}
			if softDeleted[fk.parent] && fk.onDelete == "CASCADE" {
				t.Errorf("table %s is declared fk-cascade-user but carries an ON DELETE CASCADE FK to %s, which is soft-deleted — "+
					"the cascade never fires (CLAUDE.md trap #6: soft delete does not fire SQL CASCADE); "+
					"declare it go-cascade-contact instead", name, fk.parent)
			}
		}
		if !cascadesToUsers {
			t.Errorf("table %s is declared fk-cascade-user but the schema has no ON DELETE CASCADE FK to users", name)
		}
	}

	// A table whose schema FK cascade points at `contacts` (soft-deleted)
	// must be go-cascade-contact — nothing else actually cleans it on contact
	// delete. This is the mechanical form of "a contact-scoped table may not
	// rely on FK cascade".
	for tb := range schemaTables {
		for _, fk := range foreignKeys(t, db, tb) {
			if fk.parent == "contacts" && fk.onDelete == "CASCADE" {
				if got := declaredCascadeCoverage[tb]; got != goCascadeContact {
					t.Errorf("table %s has an ON DELETE CASCADE FK to contacts (soft-deleted) but is declared %v — "+
						"that cascade never fires; it must be go-cascade-contact (CLAUDE.md trap #6)", tb, got)
				}
			}
		}
	}
}

// schemaTables returns every real table in the migrated schema, excluding
// SQLite's internal sqlite_* tables.
func schemaTables(t *testing.T, db *gorm.DB) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	var rows []struct{ Name string }
	require.NoError(t, db.Raw(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&rows).Error)
	for _, r := range rows {
		out[r.Name] = true
	}
	return out
}

// fk is one row of PRAGMA foreign_key_list.
type fk struct {
	parent   string
	onDelete string
}

// foreignKeys returns every FK constraint declared on table.
func foreignKeys(t *testing.T, db *gorm.DB, table string) []fk {
	t.Helper()
	var rows []struct {
		Table    string
		OnDelete string `gorm:"column:on_delete"`
	}
	require.NoError(t, db.Raw(fmt.Sprintf("PRAGMA foreign_key_list(%q)", table)).Scan(&rows).Error)
	var out []fk
	for _, r := range rows {
		out = append(out, fk{parent: r.Table, onDelete: r.OnDelete})
	}
	return out
}

// tableHasColumn reports whether table has the named column.
func tableHasColumn(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var rows []struct {
		Name string
	}
	require.NoError(t, db.Raw(fmt.Sprintf("PRAGMA table_info(%q)", table)).Scan(&rows).Error)
	for _, r := range rows {
		if r.Name == column {
			return true
		}
	}
	return false
}

// --- behavioral sweeps: the declared enumeration must actually empty each
// declared table ------------------------------------------------------------

// TestDeleteCascadeCoverage_DeleteContactSweepsEveryDeclaredContactTable seeds
// one contact with one row in every go-cascade-contact table, runs the real
// deleteContactAssociations, and asserts every declared contact-scoped table
// is emptied. Deleting one line from deleteContactAssociations leaves that
// table's row behind and fails here — which is the point.
func TestDeleteCascadeCoverage_DeleteContactSweepsEveryDeclaredContactTable(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	user := seedCascadeUser(t, db, "contact-sweep")
	contact := models.Contact{UserID: user.ID, Firstname: "Cascade", Lastname: "Sweep"}
	require.NoError(t, db.Create(&contact).Error)
	uid := contact.VCardUID

	containers := seedContactContainers(t, db, user.ID, contact)
	activity := models.Activity{UserID: user.ID, Title: "cascade", Date: time.Now()}
	require.NoError(t, db.Create(&activity).Error)
	require.NoError(t, db.Exec("INSERT INTO activity_contacts (activity_id, contact_id) VALUES (?, ?)", activity.ID, contact.ID).Error)
	fieldDef := containers.fieldDef

	seeded := []seedRow{
		scopedCount("attachments", &models.Attachment{}, "user_id = ?", user.ID),
		scopedCount("cadence_policies", &models.CadencePolicy{}, "user_id = ?", user.ID),
		scopedCount("circle_members", &models.CircleMember{}, "user_id = ?", user.ID),
		scopedCount("contact_sync_conflicts", &models.ContactSyncConflict{}, "user_id = ?", user.ID),
		scopedCount("contact_sync_links", &models.ContactSyncLink{}, "user_id = ?", user.ID),
		scopedCount("contact_tags", &models.ContactTag{}, "user_id = ?", user.ID),
		scopedCount("conversation_agenda", &models.ConversationAgenda{}, "user_id = ?", user.ID),
		scopedCount("dismissed_duplicate_pairs", &models.DismissedDuplicatePair{}, "user_id = ?", user.ID),
		scopedCount("external_activities", &models.ExternalActivity{}, "user_id = ?", user.ID),
		scopedCount("external_identities", &models.ExternalIdentity{}, "user_id = ?", user.ID),
		scopedCount("field_values", &models.FieldValue{}, "user_id = ?", user.ID),
		scopedCount("gifts", &models.Gift{}, "user_id = ?", user.ID),
		scopedCount("household_members", &models.HouseholdMember{}, "user_id = ?", user.ID),
		scopedCount("life_events", &models.LifeEvent{}, "user_id = ?", user.ID),
		scopedCount("notes", &models.Note{}, "user_id = ?", user.ID),
		rawCount("notification_deliveries", "reminder_id IN (SELECT id FROM reminders WHERE user_id = ?)", user.ID),
		scopedCount("preferences", &models.Preference{}, "user_id = ?", user.ID),
		scopedCount("reach_out_suggestions", &models.ReachOutSuggestion{}, "user_id = ?", user.ID),
		scopedCount("relationship_edges", &models.RelationshipEdge{}, "user_id = ?", user.ID),
		scopedCount("reminder_completions", &models.ReminderCompletion{}, "user_id = ?", user.ID),
		scopedCount("reminders", &models.Reminder{}, "user_id = ?", user.ID),
	}

	// Reminder + delivery (delivery references reminder, and reminders are
	// soft-deleted so the notification_deliveries FK cascade never fires).
	reminder := models.Reminder{UserID: user.ID, ContactID: &contact.ID, Message: "m", RemindAt: time.Now().Add(24 * time.Hour), Recurrence: "once"}
	require.NoError(t, db.Create(&reminder).Error)
	require.NoError(t, db.Create(&models.NotificationDelivery{ReminderID: reminder.ID, Channel: "email", Status: "pending"}).Error)

	// Seed one row per contact-scoped table referencing the contact.
	require.NoError(t, db.Create(&models.Attachment{UserID: user.ID, ContactVCardUID: uid, StoredName: "s", OriginalName: "o", ContentType: "text/plain", SizeBytes: 1}).Error)
	require.NoError(t, db.Create(&models.CadencePolicy{UserID: user.ID, EntityID: uid, TargetIntervalDays: 30}).Error)
	require.NoError(t, db.Create(&models.CircleMember{CircleID: containers.circleID, UserID: user.ID, MemberVCardUID: uid}).Error)
	require.NoError(t, db.Create(&models.ContactSyncConflict{UserID: user.ID, SubscriptionID: containers.subID, ContactID: contact.ID, Field: "firstname", LocalValue: "a", RemoteValue: "b", Status: "pending"}).Error)
	require.NoError(t, db.Create(&models.ContactSyncLink{SubscriptionID: containers.subID, UserID: user.ID, Href: "/dav/1.vcf", ContactID: contact.ID, ContentHash: "h"}).Error)
	require.NoError(t, db.Create(&models.ContactTag{TagID: containers.tagID, UserID: user.ID, ContactVCardUID: uid}).Error)
	require.NoError(t, db.Create(&models.ConversationAgenda{UserID: user.ID, EntityID: uid, Content: "agenda"}).Error)
	require.NoError(t, db.Create(&models.DismissedDuplicatePair{UserID: user.ID, UIDLow: uid, UIDHigh: "00000000-0000-4000-8000-000000000001"}).Error)
	require.NoError(t, db.Create(&models.ExternalActivity{UserID: user.ID, EntityID: uid, SourceSystem: "immich", ExternalID: "a-1", Type: "photo-appearance", OccurredAt: time.Now()}).Error)
	require.NoError(t, db.Create(&models.ExternalIdentity{UserID: user.ID, EntityID: uid, System: "immich", ExternalID: "p-1"}).Error)
	require.NoError(t, db.Create(&models.FieldValue{FieldDefinitionID: fieldDef.ID, UserID: user.ID, EntityID: uid, Value: json.RawMessage(`"v"`)}).Error)
	require.NoError(t, db.Create(&models.Gift{UserID: user.ID, EntityID: uid, Description: "gift"}).Error)
	require.NoError(t, db.Create(&models.HouseholdMember{HouseholdID: containers.householdID, UserID: user.ID, MemberVCardUID: uid, Role: "adult"}).Error)
	require.NoError(t, db.Create(&models.LifeEvent{UserID: user.ID, EntityID: uid, Type: "custom"}).Error)
	require.NoError(t, db.Create(&models.Note{UserID: user.ID, ContactID: &contact.ID, Content: "n", Date: time.Now()}).Error)
	require.NoError(t, db.Create(&models.Preference{UserID: user.ID, EntityID: uid, Category: "food", Value: "pizza"}).Error)
	require.NoError(t, db.Create(&models.ReachOutSuggestion{UserID: user.ID, ContactVCardUID: uid, Kind: "organization", OldValue: "a", NewValue: "b", AuditEventID: 1, Status: "pending"}).Error)
	require.NoError(t, db.Create(&models.RelationshipEdge{UserID: user.ID, SourceID: uid, TargetID: uid, Type: "related_to"}).Error)
	require.NoError(t, db.Create(&models.ReminderCompletion{UserID: user.ID, ContactID: contact.ID, Message: "done", CompletedAt: time.Now()}).Error)

	// Sanity: every declared table actually received its row.
	assertSeeded(t, db, seeded)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return deleteContactAssociations(tx, contact, user.ID)
	}))
	require.NoError(t, db.Delete(&contact).Error)

	assertEmptied(t, db, seeded, "after DeleteContact")
}

// TestDeleteCascadeCoverage_DeleteUserSweepsEveryDeclaredUserTable seeds a
// user with one row in every go-cascade-user table (and the user-scoped side
// of every go-cascade-contact table), runs the real DeleteUser, and asserts
// every declared user-scoped table is emptied.
func TestDeleteCascadeCoverage_DeleteUserSweepsEveryDeclaredUserTable(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	admin := seedCascadeUser(t, db, "cascade-admin")
	target := seedCascadeUser(t, db, "cascade-target")

	contact := models.Contact{UserID: target.ID, Firstname: "User", Lastname: "Sweep"}
	require.NoError(t, db.Create(&contact).Error)
	uid := contact.VCardUID

	containers := seedContactContainers(t, db, target.ID, contact)
	activity := models.Activity{UserID: target.ID, Title: "cascade", Date: time.Now()}
	require.NoError(t, db.Create(&activity).Error)
	require.NoError(t, db.Exec("INSERT INTO activity_contacts (activity_id, contact_id) VALUES (?, ?)", activity.ID, contact.ID).Error)
	fieldDef := containers.fieldDef

	calSub := models.CalendarSubscription{UserID: target.ID, Name: "cal", URL: "https://example.com/cal.ics"}
	require.NoError(t, db.Create(&calSub).Error)
	require.NoError(t, db.Create(&models.CalendarEventLink{SubscriptionID: calSub.ID, UserID: target.ID, UID: "evt-1", ActivityID: activity.ID, ContentHash: "h"}).Error)

	webhook := models.Webhook{UserID: target.ID, Name: "w", URL: "https://example.com/hook", Events: []string{"contact.created"}}
	require.NoError(t, db.Create(&webhook).Error)
	require.NoError(t, db.Create(&models.WebhookDelivery{WebhookID: webhook.ID, EventType: "contact.created", Payload: "{}"}).Error)

	require.NoError(t, db.Create(&models.ApiToken{UserID: target.ID, Name: "t", TokenHash: "h"}).Error)
	require.NoError(t, db.Create(&models.DeviceGrant{UserID: target.ID, TokenHash: "dg", Label: "Pixel"}).Error)
	require.NoError(t, db.Create(&models.CardDAVSync{UserID: target.ID, SyncToken: "tok", LastModified: time.Now()}).Error)
	require.NoError(t, db.Create(&models.DeviceRegistration{UserID: target.ID, Token: "tok", Client: "fcm"}).Error)
	require.NoError(t, db.Create(&models.DismissedHouseholdSuggestion{UserID: target.ID, AddressHash: "ah", MemberHash: "mh"}).Error)
	require.NoError(t, db.Create(&models.IdempotencyKey{UserID: target.ID, Key: "sweep-key", Method: "POST", Path: "/api/v1/contacts", RequestFingerprint: "fp", State: models.IdempotencyStateCompleted, ResponseStatus: 201, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
	require.NoError(t, db.Create(&models.ImmichConfig{UserID: target.ID, BaseURL: "https://immich.example"}).Error)
	require.NoError(t, db.Create(&models.ImportRun{UserID: target.ID, Format: models.ImportFormatCSV, TotalProcessed: 3, Created: 2, Skipped: 1}).Error)
	require.NoError(t, db.Create(&models.LinkFieldType{UserID: target.ID, Name: "x", Protocol: "https://x/{value}", Category: "other"}).Error)
	require.NoError(t, db.Create(&models.NotificationConfig{UserID: target.ID}).Error)
	require.NoError(t, db.Create(&models.PaperlessConfig{UserID: target.ID, BaseURL: "https://paperless.example"}).Error)
	require.NoError(t, db.Create(&models.PushSubscription{UserID: target.ID, Endpoint: "https://e", P256dh: "p", Auth: "a"}).Error)
	require.NoError(t, db.Create(&models.ReachOutCursor{UserID: target.ID, LastAuditEventID: 1}).Error)
	require.NoError(t, db.Create(&models.RecoveryCode{UserID: target.ID, CodeHash: "h"}).Error)
	require.NoError(t, db.Create(&models.SeafileConfig{UserID: target.ID, BaseURL: "https://seafile.example"}).Error)
	require.NoError(t, db.Create(&models.WebDAVConfig{UserID: target.ID, BaseURL: "https://nc.example", Username: "u"}).Error)

	share := models.ContactShare{FromUserID: target.ID, ToUserID: admin.ID, ContactDisplayName: "x", Payload: "[]"}
	require.NoError(t, db.Create(&share).Error)

	seeded := []seedRow{
		scopedCount("activities", &models.Activity{}, "user_id = ?", target.ID),
		rawCount("activity_contacts", "activity_id = ?", activity.ID),
		scopedCount("api_tokens", &models.ApiToken{}, "user_id = ?", target.ID),
		scopedCount("attachments", &models.Attachment{}, "user_id = ?", target.ID),
		scopedCount("cadence_policies", &models.CadencePolicy{}, "user_id = ?", target.ID),
		scopedCount("calendar_event_links", &models.CalendarEventLink{}, "user_id = ?", target.ID),
		scopedCount("calendar_subscriptions", &models.CalendarSubscription{}, "user_id = ?", target.ID),
		scopedCount("carddav_sync", &models.CardDAVSync{}, "user_id = ?", target.ID),
		scopedCount("circle_members", &models.CircleMember{}, "user_id = ?", target.ID),
		scopedCount("circles", &models.Circle{}, "user_id = ?", target.ID),
		scopedCount("contact_shares", &models.ContactShare{}, "from_user_id = ? OR to_user_id = ?", target.ID, target.ID),
		scopedCount("contact_subscriptions", &models.ContactSubscription{}, "user_id = ?", target.ID),
		scopedCount("contact_sync_conflicts", &models.ContactSyncConflict{}, "user_id = ?", target.ID),
		scopedCount("contact_sync_links", &models.ContactSyncLink{}, "user_id = ?", target.ID),
		scopedCount("contact_tags", &models.ContactTag{}, "user_id = ?", target.ID),
		scopedCount("contacts", &models.Contact{}, "user_id = ?", target.ID),
		scopedCount("conversation_agenda", &models.ConversationAgenda{}, "user_id = ?", target.ID),
		scopedCount("device_grants", &models.DeviceGrant{}, "user_id = ?", target.ID),
		scopedCount("device_registrations", &models.DeviceRegistration{}, "user_id = ?", target.ID),
		scopedCount("dismissed_duplicate_pairs", &models.DismissedDuplicatePair{}, "user_id = ?", target.ID),
		scopedCount("dismissed_household_suggestions", &models.DismissedHouseholdSuggestion{}, "user_id = ?", target.ID),
		scopedCount("external_activities", &models.ExternalActivity{}, "user_id = ?", target.ID),
		scopedCount("external_identities", &models.ExternalIdentity{}, "user_id = ?", target.ID),
		scopedCount("field_definitions", &models.FieldDefinition{}, "user_id = ?", target.ID),
		scopedCount("field_values", &models.FieldValue{}, "user_id = ?", target.ID),
		scopedCount("gifts", &models.Gift{}, "user_id = ?", target.ID),
		scopedCount("household_members", &models.HouseholdMember{}, "user_id = ?", target.ID),
		scopedCount("households", &models.Household{}, "user_id = ?", target.ID),
		scopedCount("idempotency_keys", &models.IdempotencyKey{}, "user_id = ?", target.ID),
		scopedCount("immich_configs", &models.ImmichConfig{}, "user_id = ?", target.ID),
		scopedCount("import_runs", &models.ImportRun{}, "user_id = ?", target.ID),
		scopedCount("life_events", &models.LifeEvent{}, "user_id = ?", target.ID),
		scopedCount("link_field_types", &models.LinkFieldType{}, "user_id = ?", target.ID),
		scopedCount("notes", &models.Note{}, "user_id = ?", target.ID),
		scopedCount("notification_configs", &models.NotificationConfig{}, "user_id = ?", target.ID),
		rawCount("notification_deliveries", "reminder_id IN (SELECT id FROM reminders WHERE user_id = ?)", target.ID),
		scopedCount("paperless_configs", &models.PaperlessConfig{}, "user_id = ?", target.ID),
		scopedCount("preferences", &models.Preference{}, "user_id = ?", target.ID),
		scopedCount("push_subscriptions", &models.PushSubscription{}, "user_id = ?", target.ID),
		scopedCount("reach_out_cursors", &models.ReachOutCursor{}, "user_id = ?", target.ID),
		scopedCount("reach_out_suggestions", &models.ReachOutSuggestion{}, "user_id = ?", target.ID),
		scopedCount("recovery_codes", &models.RecoveryCode{}, "user_id = ?", target.ID),
		scopedCount("relationship_edges", &models.RelationshipEdge{}, "user_id = ?", target.ID),
		scopedCount("reminder_completions", &models.ReminderCompletion{}, "user_id = ?", target.ID),
		scopedCount("reminders", &models.Reminder{}, "user_id = ?", target.ID),
		scopedCount("seafile_configs", &models.SeafileConfig{}, "user_id = ?", target.ID),
		scopedCount("tags", &models.Tag{}, "user_id = ?", target.ID),
		scopedCount("webdav_configs", &models.WebDAVConfig{}, "user_id = ?", target.ID),
		rawCount("webhook_deliveries", "webhook_id = ?", webhook.ID),
		scopedCount("webhooks", &models.Webhook{}, "user_id = ?", target.ID),
	}

	// Reminder + delivery.
	reminder := models.Reminder{UserID: target.ID, ContactID: &contact.ID, Message: "m", RemindAt: time.Now().Add(24 * time.Hour), Recurrence: "once"}
	require.NoError(t, db.Create(&reminder).Error)
	require.NoError(t, db.Create(&models.NotificationDelivery{ReminderID: reminder.ID, Channel: "email", Status: "pending"}).Error)

	// Seed the user-scoped rows referencing the contact (the contact-scoped
	// side of tables that are both, so DeleteUser empties them by user_id).
	require.NoError(t, db.Create(&models.Attachment{UserID: target.ID, ContactVCardUID: uid, StoredName: "s", OriginalName: "o", ContentType: "text/plain", SizeBytes: 1}).Error)
	require.NoError(t, db.Create(&models.CadencePolicy{UserID: target.ID, EntityID: uid, TargetIntervalDays: 30}).Error)
	require.NoError(t, db.Create(&models.CircleMember{CircleID: containers.circleID, UserID: target.ID, MemberVCardUID: uid}).Error)
	require.NoError(t, db.Create(&models.ContactSyncConflict{UserID: target.ID, SubscriptionID: containers.subID, ContactID: contact.ID, Field: "firstname", LocalValue: "a", RemoteValue: "b", Status: "pending"}).Error)
	require.NoError(t, db.Create(&models.ContactSyncLink{SubscriptionID: containers.subID, UserID: target.ID, Href: "/dav/1.vcf", ContactID: contact.ID, ContentHash: "h"}).Error)
	require.NoError(t, db.Create(&models.ContactTag{TagID: containers.tagID, UserID: target.ID, ContactVCardUID: uid}).Error)
	require.NoError(t, db.Create(&models.ConversationAgenda{UserID: target.ID, EntityID: uid, Content: "agenda"}).Error)
	require.NoError(t, db.Create(&models.DismissedDuplicatePair{UserID: target.ID, UIDLow: uid, UIDHigh: "00000000-0000-4000-8000-000000000001"}).Error)
	require.NoError(t, db.Create(&models.ExternalActivity{UserID: target.ID, EntityID: uid, SourceSystem: "immich", ExternalID: "a-1", Type: "photo-appearance", OccurredAt: time.Now()}).Error)
	require.NoError(t, db.Create(&models.ExternalIdentity{UserID: target.ID, EntityID: uid, System: "immich", ExternalID: "p-1"}).Error)
	require.NoError(t, db.Create(&models.FieldValue{FieldDefinitionID: fieldDef.ID, UserID: target.ID, EntityID: uid, Value: json.RawMessage(`"v"`)}).Error)
	require.NoError(t, db.Create(&models.Gift{UserID: target.ID, EntityID: uid, Description: "gift"}).Error)
	require.NoError(t, db.Create(&models.HouseholdMember{HouseholdID: containers.householdID, UserID: target.ID, MemberVCardUID: uid, Role: "adult"}).Error)
	require.NoError(t, db.Create(&models.LifeEvent{UserID: target.ID, EntityID: uid, Type: "custom"}).Error)
	require.NoError(t, db.Create(&models.Note{UserID: target.ID, ContactID: &contact.ID, Content: "n", Date: time.Now()}).Error)
	require.NoError(t, db.Create(&models.Preference{UserID: target.ID, EntityID: uid, Category: "food", Value: "pizza"}).Error)
	require.NoError(t, db.Create(&models.ReachOutSuggestion{UserID: target.ID, ContactVCardUID: uid, Kind: "organization", OldValue: "a", NewValue: "b", AuditEventID: 1, Status: "pending"}).Error)
	require.NoError(t, db.Create(&models.RelationshipEdge{UserID: target.ID, SourceID: uid, TargetID: uid, Type: "related_to"}).Error)
	require.NoError(t, db.Create(&models.ReminderCompletion{UserID: target.ID, ContactID: contact.ID, Message: "done", CompletedAt: time.Now()}).Error)

	assertSeeded(t, db, seeded)

	// Drive the real DeleteUser handler.
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", admin.ID)
		c.Next()
	})
	router.DELETE("/users/:id", DeleteUser)
	req, _ := http.NewRequest("DELETE", "/users/"+strconv.FormatUint(uint64(target.ID), 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "DeleteUser: %s", w.Body.String())

	// The admin's own rows must survive — DeleteUser only sweeps the target.
	var adminCount int64
	require.NoError(t, db.Model(&models.Contact{}).Where("user_id = ?", admin.ID).Count(&adminCount).Error)
	require.Zero(t, adminCount, "admin has no contacts seeded; target sweep must not touch other users")

	assertEmptied(t, db, seeded, "after DeleteUser")
}

// seedRow is one table's "must be seeded before, and emptied after, deletion".
// count returns the number of seeded rows for this table.
type seedRow struct {
	table string
	count func(t *testing.T, db *gorm.DB) int64
}

func (s seedRow) run(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	return s.count(t, db)
}

func assertSeeded(t *testing.T, db *gorm.DB, rows []seedRow) {
	t.Helper()
	for _, r := range rows {
		if n := r.run(t, db); n == 0 {
			t.Errorf("table %s was not seeded — the sweep cannot prove deletion", r.table)
		}
	}
}

func assertEmptied(t *testing.T, db *gorm.DB, rows []seedRow, phase string) {
	t.Helper()
	for _, r := range rows {
		if n := r.run(t, db); n != 0 {
			t.Errorf("table %s still has %d row(s) %s — deleteContactAssociations or DeleteUser misses it", r.table, n, phase)
		}
	}
}

// scopedCount returns a seedRow counting rows via a GORM model + where clause.
func scopedCount(table string, model any, where string, args ...any) seedRow {
	return seedRow{
		table: table,
		count: func(t *testing.T, db *gorm.DB) int64 {
			t.Helper()
			var n int64
			require.NoError(t, db.Model(model).Where(where, args...).Count(&n).Error, "counting %s", table)
			return n
		},
	}
}

// rawCount returns a seedRow counting rows via a raw WHERE expression.
func rawCount(table, where string, args ...any) seedRow {
	return seedRow{
		table: table,
		count: func(t *testing.T, db *gorm.DB) int64 {
			t.Helper()
			var n int64
			require.NoError(t, db.Table(table).Where(where, args...).Count(&n).Error, "counting %s", table)
			return n
		},
	}
}

// cascadeContainers holds the parent rows a contact-scoped table references.
type cascadeContainers struct {
	householdID string
	circleID    string
	tagID       string
	subID       uint
	fieldDef    models.FieldDefinition
}

// seedContactContainers creates one of each container (household, circle, tag,
// contact subscription, field definition) owned by user, so the dependent
// contact-scoped tables have a parent to reference.
func seedContactContainers(t *testing.T, db *gorm.DB, userID uint, contact models.Contact) cascadeContainers {
	t.Helper()

	household := models.Household{UserID: userID, Name: "h", Type: models.HouseholdTypeOther}
	require.NoError(t, db.Create(&household).Error)
	circle := models.Circle{UserID: userID, Name: "c"}
	require.NoError(t, db.Create(&circle).Error)
	tag := models.Tag{UserID: userID, Name: "t"}
	require.NoError(t, db.Create(&tag).Error)
	sub := models.ContactSubscription{UserID: userID, Name: "sub", URL: "https://example.com/dav"}
	require.NoError(t, db.Create(&sub).Error)
	fieldDef := models.FieldDefinition{UserID: userID, Label: "f", Key: "f", Target: "contact", Type: "string"}
	require.NoError(t, db.Create(&fieldDef).Error)

	return cascadeContainers{
		householdID: household.ID,
		circleID:    circle.ID,
		tagID:       tag.ID,
		subID:       sub.ID,
		fieldDef:    fieldDef,
	}
}

// seedCascadeUser creates one user with a hashed password, username derived
// from tag so distinct sweep tests don't collide on the account rate limiter.
func seedCascadeUser(t *testing.T, db *gorm.DB, tag string) models.User {
	t.Helper()
	hashed, err := services.HashPassword(strongPassword)
	require.NoError(t, err)
	username := "cascade_" + strings.ToLower(strings.ReplaceAll(tag, "-", "_"))
	u := models.User{Username: username, Email: username + "@example.com", Password: hashed}
	require.NoError(t, db.Create(&u).Error)
	return u
}
