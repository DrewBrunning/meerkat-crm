package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mycorrhizal/contactmodel"
	"mycorrhizal/internal/faults"
	"mycorrhizal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// faultImportSource is the failure-injection seam (issue #434 / #498) for the
// shared source-import engine (Meerkat, Monica, any future mapper). Armed via
// the faults package, it fails the whole import at the transaction boundary:
// every contact and graph entity rolls back, no source-link row survives, and
// a retry re-runs cleanly. The injection test asserts that fail-closed
// outcome. See docs/development/fault-injection.md.
const faultImportSource = "services.import.source"

// This file is the shared source-import framework (issues #351 + #353): one
// transactional engine that applies a mapped ImportSourcePlan — contacts and
// graph entities alike — into the local schema, so a third importer (Monica
// v3, a future Nextcloud/OSM export, ...) is a mapper, not a third pipeline.
//
// Design rules this framework exists to enforce, each pinned by a test:
//
//  1. Contacts land through ApplyRecordToContact (CLAUDE.md backend trap #2:
//     direct field mutation before Create skips BeforeSave and silently drops
//     data). The mappers produce neutral contactmodel.Record values; only the
//     CRM-local flags with no neutral home (Archived/IsFavorite) are set
//     directly afterwards.
//  2. Nothing is dropped silently. Every mapped record that cannot land — or
//     that lands degraded — is named in ImportReport.Issues with its record,
//     field, and category (the #442 diagnostic shape over ADR-0002's tiers).
//  3. A failing record leaves no partial contact: the whole import runs in one
//     transaction, and a contact that fails validation or creation is skipped
//     whole — its dependent graph entities are dropped with a named issue, not
//     silently orphaned.
//  4. Re-running the same import does not duplicate (CON-04 / #459): every
//     row produced is recorded in models.ImportSourceLink keyed by its source
//     identity, and an already-imported row is skipped on re-run.
//
// The existing session-based wizard (import_session.go) is the UI surface for
// CSV/VCF/JSContact/records; source imports bypass it by design (a Meerkat DB
// or Monica snapshot is not a file upload with per-row merge previews yet —
// that is the deferred assistant tickets #549/#550). A future wizard can wrap
// this engine, or feed a plan's contacts through the records pipeline.

// ImportIssue categories — the #442 "which contact, which field, why" shape
// over ADR-0002's two degradation tiers. "invalid" is a record rejected at
// validation (input is not a valid instance), matching ADR-0002's rule that
// error is reserved for non-instances. "skipped" is a deliberate policy
// skip (soft-deleted source row, already imported) — not a fidelity loss,
// and reported distinctly so it is never read as one (#442 item 7's policy
// exclusion).
const (
	ImportIssueCategoryUnsupported = "unsupported" // genuinely unmappable, preserved nowhere
	ImportIssueCategoryLossy       = "lossy"       // mapped but degraded (e.g. a value truncated or flattened)
	ImportIssueCategoryTransformed = "transformed" // mapped with a semantic transformation (e.g. date normalized)
	ImportIssueCategoryInvalid     = "invalid"     // record rejected — cannot be represented at all
	ImportIssueCategorySkipped     = "skipped"     // deliberately not imported (deleted source row, already imported)
)

// ImportIssue is one per-record loss/degradation report. Record names the
// source record ("contact 7 (Jane Doe)"), Field names the source field or
// concept, Category is one of the ImportIssueCategory* tokens.
type ImportIssue struct {
	Record   string `json:"record"`
	Field    string `json:"field"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// SourceRef identifies one source-system row: System is the source
// ("meerkat", "monica"), ExternalID is the row's identity in that system,
// namespaced per entity kind ("contact/7", "note/12") so kinds never collide
// in the import_source_links ledger.
type SourceRef struct {
	System     string
	ExternalID string
}

// String renders a human-readable record label for issues.
func (s SourceRef) String() string {
	if s.ExternalID == "" {
		return s.System
	}
	return fmt.Sprintf("%s %s", s.System, s.ExternalID)
}

// MappedContact is one mapped source contact: its neutral Record (the
// full-fidelity Card/CRM/Passthrough to land via ApplyRecordToContact) plus
// the CRM-local flags that have no neutral home. PhotoURL/PhotoSource carry an
// external avatar to fetch later (Monica photo/gravatar); the mapping records
// it here because the assistant (#549) is what actually fetches — the backend
// mapping never makes network calls.
type MappedContact struct {
	Ref         SourceRef
	Record      *contactmodel.Record
	Archived    bool
	Favorite    bool
	PhotoURL    string
	PhotoSource string // "photo" | "gravatar" | "" (deferred to the assistant)
}

// MappedRelationship is one directed relationship edge. Source/Target are the
// SourceRefs of the plan's contacts — direction matters: Type describes the
// source's role relative to the target and only one direction is stored
// (CLAUDE.md domain note). Status defaults to confirmed, Sensitivity to
// normal.
type MappedRelationship struct {
	Ref         SourceRef
	Source      SourceRef
	Target      SourceRef
	Type        string
	Directional bool
	Status      string
	Sensitivity string
}

// MappedNote is one note on a plan contact.
type MappedNote struct {
	Ref     SourceRef
	Contact SourceRef
	Content string
	Date    string // RFC3339-ish; parsed by the engine into the Note's time.Time
}

// MappedActivity is one shared activity with a set of plan contacts.
type MappedActivity struct {
	Ref         SourceRef
	Contacts    []SourceRef
	Title       string
	Description string
	Location    string
	Date        string
	Type        string // Activity.Type (InteractionType*), best-effort
}

// MappedReminder is one reminder on a plan contact. Recurrence uses the local
// vocabulary (once|weekly|monthly|quarterly|six-months|yearly). RemindAt is
// RFC3339-ish.
type MappedReminder struct {
	Ref                   SourceRef
	Contact               SourceRef
	Message               string
	RemindAt              string
	Recurrence            string
	ReoccurFromCompletion *bool
}

// MappedGift is one gift record on a plan contact.
type MappedGift struct {
	Ref         SourceRef
	Contact     SourceRef
	Status      string // idea|purchased|given|received
	Occasion    string
	Description string
	URL         string
	Notes       string
	Date        string
	ValueCents  int64
	Currency    string
}

// MappedPreference is one preference on a plan contact.
type MappedPreference struct {
	Ref      SourceRef
	Contact  SourceRef
	Category string
	Key      string
	Value    string
	Notes    string
}

// MappedHouseholdMember is one membership of a plan contact in a household.
type MappedHouseholdMember struct {
	Contact SourceRef
	Role    string
	Since   string
	Until   string
}

// MappedHousehold is one co-residence grouping.
type MappedHousehold struct {
	Ref     SourceRef
	Name    string
	Type    string // family_unit|roommates|other
	Address *contactmodel.Address
	Members []MappedHouseholdMember
}

// MappedCircle is one social grouping with member plan contacts.
type MappedCircle struct {
	Ref     SourceRef
	Name    string
	Members []SourceRef
}

// MappedTag is one attribute with tagged plan contacts.
type MappedTag struct {
	Ref      SourceRef
	Name     string
	Contacts []SourceRef
}

// MappedCustomField is one custom-field value on a plan contact. Key is the
// field's stable machine name (a FieldDefinition is created per unique key,
// Label is the human label, Value the contact's value).
type MappedCustomField struct {
	Ref     SourceRef
	Contact SourceRef
	Key     string
	Label   string
	Value   string
}

// ImportSourcePlan is the complete mapped output of one source: every record
// to import plus, implicitly, the issues its mapping produced (mappers append
// to report before returning). All contact references inside are SourceRefs
// the engine resolves to local VCardUIDs.
type ImportSourcePlan struct {
	System        string
	Contacts      []MappedContact
	Relationships []MappedRelationship
	Notes         []MappedNote
	Activities    []MappedActivity
	Reminders     []MappedReminder
	Gifts         []MappedGift
	Preferences   []MappedPreference
	Households    []MappedHousehold
	Circles       []MappedCircle
	Tags          []MappedTag
	CustomFields  []MappedCustomField

	// Report carries every issue the mapping and the execution produced.
	Report ImportReport
}

// ImportReport is the per-record outcome: created/skipped/failed counts per
// entity kind plus the named issue list (record, field, category).
type ImportReport struct {
	ContactsCreated int
	ContactsUpdated int // merged into an existing local contact (assistant "update" action)
	ContactsSkipped int

	RelationshipsCreated int
	NotesCreated         int
	ActivitiesCreated    int
	RemindersCreated     int
	GiftsCreated         int
	PreferencesCreated   int
	HouseholdsCreated    int
	CirclesCreated       int
	TagsCreated          int
	CustomFieldsCreated  int

	Issues []ImportIssue
}

// appendIssue records one issue, deduplicating exact repeats within one run
// (the same source row can surface in both the mapping and the execution
// passes).
func (r *ImportReport) appendIssue(issue ImportIssue) {
	for _, existing := range r.Issues {
		if existing == issue {
			return
		}
	}
	r.Issues = append(r.Issues, issue)
}

// SourceContactAction is a per-contact decision from an import assistant's
// review step (issues #549/#550), keyed in the actions map by the plan
// contact's SourceRef.ExternalID. The zero value (Action == "") means "add",
// so a plan with no actions map imports every contact — the behaviour
// ExecuteSourceImport keeps.
type SourceContactAction struct {
	// Action is "" / SourceActionAdd (create), SourceActionSkip (exclude, and
	// drop its dependent graph entities with a named issue), or
	// SourceActionMerge (merge the mapped record into an existing local
	// contact identified by MergeTargetUID).
	Action string
	// MergeTargetUID is the local Contact.VCardUID to merge into; required
	// when Action == SourceActionMerge, ignored otherwise.
	MergeTargetUID string
}

const (
	SourceActionAdd   = "add"
	SourceActionSkip  = "skip"
	SourceActionMerge = "merge"
)

// ExecuteSourceImport applies a mapped plan into a real migrated schema
// (database.InitDB — CLAUDE.md backend trap #1) in one transaction. It
// returns a report with per-record outcomes; a record that fails is named
// with its field and category and leaves nothing behind, and re-running the
// same plan never duplicates (#459). Every contact in the plan is created;
// callers that need per-contact skip/merge decisions use
// ExecuteSourceImportWithActions.
func ExecuteSourceImport(db *gorm.DB, userID uint, plan *ImportSourcePlan) (*ImportReport, error) {
	report, _, err := ExecuteSourceImportWithActions(context.Background(), db, userID, plan, nil, nil)
	return report, err
}

// ExecuteSourceImportWithActions is ExecuteSourceImport plus a per-contact
// action map (SourceRef.ExternalID → SourceContactAction) from an assistant's
// review step. It also returns externalID → local contact ID for every
// contact created or merged, so the caller can attach deferred work (avatar
// downloads) to the right rows. A nil or empty actions map is identical to
// ExecuteSourceImport.
//
// ctx cancels the whole import: the run happens in one transaction bound to
// ctx, so cancelling it fails the next statement and rolls everything back —
// the assistants use this for an in-flight "Cancel import" with no partial
// result. progress, when non-nil, is called as the run advances (once per
// contact in pass 1, once per graph entity-kind in pass 2) with a running
// (done, total) so the UI can show a bar.
func ExecuteSourceImportWithActions(ctx context.Context, db *gorm.DB, userID uint, plan *ImportSourcePlan, actions map[string]SourceContactAction, progress func(done, total int)) (*ImportReport, map[string]uint, error) {
	if plan == nil || plan.System == "" {
		return &ImportReport{}, map[string]uint{}, nil
	}
	// Seed the outcome with the mapping's issues so a mapper's named losses
	// (dangling relationships, skipped deleted rows, unmappable photos) are
	// part of the returned report — the execution pass appends to the same
	// list, and appendIssue deduplicates.
	report := &ImportReport{Issues: append([]ImportIssue(nil), plan.Report.Issues...)}
	refToID := make(map[string]uint, len(plan.Contacts))

	// total = one tick per contact + one per pass-2 entity kind (importGraphKinds).
	total := len(plan.Contacts) + importGraphKinds
	done := 0
	tick := func() {
		done++
		if progress != nil {
			progress(done, total)
		}
	}

	// Issue #498: disk preflight before the one big transaction, so a
	// too-full disk is a clear refusal with the local data untouched rather
	// than an ENOSPC deep in pass 1.
	if spaceErr := preflightImportDiskSpace(db, len(plan.Contacts)); spaceErr != nil {
		return nil, nil, spaceErr
	}

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return executeSourceImport(ctx, tx, userID, plan, actions, report, refToID, tick)
	})
	if err != nil {
		return nil, nil, err
	}
	// The merged outcome is the return value only — we deliberately do not
	// write it back onto the caller's plan. The async assistants keep running
	// this on a *ImportSourcePlan that a still-finishing fetch goroutine (and
	// a late Preview) may still be reading; mutating it here is a data race
	// (caught by -race in TestMonicaImport_FullControllerFlow). plan.Report
	// stays exactly what the mapper produced.
	return report, refToID, nil
}

// importGraphKinds is the number of pass-2 entity-kind import calls in
// executeSourceImport — used only to size the progress bar's total.
const importGraphKinds = 10

func executeSourceImport(ctx context.Context, tx *gorm.DB, userID uint, plan *ImportSourcePlan, actions map[string]SourceContactAction, report *ImportReport, refToID map[string]uint, tick func()) error {
	// Issue #434/#498 failure-injection seam: an armed fault fails the whole
	// source import at the transaction boundary — everything rolls back, no
	// partial state, a retry re-runs cleanly. Unarmed this is a nil return.
	if err := faults.Hook(faultImportSource); err != nil {
		return err
	}

	imported := loadSourceLinks(tx, userID, plan.System)

	// Pass 1: contacts. refToUID maps each plan contact's SourceRef to the
	// local VCardUID it landed as; graph entities resolve through it.
	// refToID carries the flat contact ID for the entities that key on it
	// (notes, reminders).
	refToUID := make(map[string]string, len(plan.Contacts))
	for i := range plan.Contacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		tick()
		mc := &plan.Contacts[i]
		if imported[mc.Ref.ExternalID] {
			report.ContactsSkipped++
			report.appendIssue(ImportIssue{
				Record:   mc.Ref.String(),
				Field:    "contact",
				Category: ImportIssueCategorySkipped,
				Message:  "already imported in an earlier run",
			})
			continue
		}

		action := actions[mc.Ref.ExternalID]
		if action.Action == SourceActionSkip {
			report.ContactsSkipped++
			report.appendIssue(ImportIssue{
				Record:   mc.Ref.String(),
				Field:    "contact",
				Category: ImportIssueCategorySkipped,
				Message:  "excluded in the import review step",
			})
			continue
		}

		contact := &models.Contact{UserID: userID}
		models.ApplyRecordToContact(contact, mc.Record, "")
		if contact.VCardUID == "" {
			contact.VCardUID = uuid.New().String()
		}
		contact.Archived = mc.Archived
		contact.IsFavorite = mc.Favorite

		if issues := validateMappedContact(contact); len(issues) > 0 {
			report.appendIssue(ImportIssue{
				Record:   mc.Ref.String(),
				Field:    "name",
				Category: ImportIssueCategoryInvalid,
				Message:  strings.Join(issues, "; "),
			})
			continue
		}

		if action.Action == SourceActionMerge {
			uid, localID, err := mergeSourceContact(tx, userID, plan.System, mc, contact, action.MergeTargetUID, report)
			if err != nil {
				return err
			}
			if uid == "" {
				continue // merge target missing/invalid — named on the report
			}
			refToUID[mc.Ref.ExternalID] = uid
			refToID[mc.Ref.ExternalID] = localID
			imported[mc.Ref.ExternalID] = true
			continue
		}

		if err := tx.Create(contact).Error; err != nil {
			report.appendIssue(ImportIssue{
				Record:   mc.Ref.String(),
				Field:    "contact",
				Category: ImportIssueCategoryInvalid,
				Message:  err.Error(),
			})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, mc.Ref.ExternalID,
			models.ImportSourceLinkKindContact, contact.VCardUID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.ContactsCreated++
		refToUID[mc.Ref.ExternalID] = contact.VCardUID
		refToID[mc.Ref.ExternalID] = contact.ID
		imported[mc.Ref.ExternalID] = true

		if mc.PhotoURL != "" {
			report.appendIssue(ImportIssue{
				Record:   mc.Ref.String(),
				Field:    "photo",
				Category: ImportIssueCategoryTransformed,
				Message:  "avatar (" + mc.PhotoSource + ") not fetched — the import assistant performs photo downloads",
			})
		}
	}

	// Pass 2: graph entities, resolved through refToUID.
	uidOf := func(record string, ref SourceRef) (string, bool) {
		uid, ok := refToUID[ref.ExternalID]
		if !ok {
			report.appendIssue(ImportIssue{
				Record:   record,
				Field:    ref.ExternalID,
				Category: ImportIssueCategoryUnsupported,
				Message:  "references a contact that was not imported",
			})
			return "", false
		}
		return uid, true
	}
	skipImported := func(record string, ref SourceRef) bool {
		if imported[ref.ExternalID] {
			report.appendIssue(ImportIssue{
				Record:   record,
				Field:    ref.String(),
				Category: ImportIssueCategorySkipped,
				Message:  "already imported in an earlier run",
			})
			return true
		}
		return false
	}

	// Pass 2: graph entity kinds, one per importGraphKinds. Each advances the
	// progress bar; ctx is re-checked between kinds so a cancel rolls back
	// promptly on a large graph.
	graphKinds := []func() error{
		func() error { return importRelationships(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importHouseholds(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importCircles(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importTags(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importGifts(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importPreferences(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importNotes(tx, userID, plan, imported, refToID, skipImported, report) },
		func() error { return importActivities(tx, userID, plan, imported, uidOf, skipImported, report) },
		func() error { return importReminders(tx, userID, plan, imported, refToID, skipImported, report) },
		func() error { return importCustomFields(tx, userID, plan, imported, refToUID, skipImported, report) },
	}
	for _, importKind := range graphKinds {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := importKind(); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		tick()
	}

	return nil
}

// validateMappedContact runs the shared flat validation after
// ApplyRecordToContact, so a plan contact that cannot exist locally is
// rejected whole with a named issue instead of being half-created.
func validateMappedContact(contact *models.Contact) []string {
	if strings.TrimSpace(contact.Firstname) == "" &&
		strings.TrimSpace(contact.Lastname) == "" &&
		strings.TrimSpace(contact.Nickname) == "" {
		return []string{"contact has no usable name"}
	}
	return ValidateImportedContact(contact)
}

// mergeSourceContact applies a mapped source contact onto an existing local
// contact (the assistant's "update" action). It reuses the same flat merge
// machinery every file-based import "update" runs (MergeImportedContact +
// CreateMergeNote, see import_session.go), so a source import merges the way
// the rest of the app does. Neutral-only content on the mapped record
// (SpeakToAs, PersonalInfo) has no flat home and is not carried onto the
// existing contact — the same limitation the CSV/VCF merge path has; it is
// named on the report when present rather than dropped silently.
//
// Returns the existing contact's VCardUID and local ID on success so pass 2
// resolves graph entities onto it; returns "" (with a named issue, no error)
// when the merge target cannot be found or saved, so the row is skipped and
// the transaction continues.
func mergeSourceContact(tx *gorm.DB, userID uint, system string, mc *MappedContact, incoming *models.Contact, targetUID string, report *ImportReport) (string, uint, error) {
	targetUID = strings.TrimSpace(targetUID)
	if targetUID == "" {
		report.appendIssue(ImportIssue{
			Record:   mc.Ref.String(),
			Field:    "contact",
			Category: ImportIssueCategoryInvalid,
			Message:  "update requested but no existing contact was identified",
		})
		return "", 0, nil
	}

	var existing models.Contact
	if err := tx.Where("user_id = ? AND vcard_uid = ?", userID, targetUID).First(&existing).Error; err != nil {
		report.appendIssue(ImportIssue{
			Record:   mc.Ref.String(),
			Field:    "contact",
			Category: ImportIssueCategoryInvalid,
			Message:  "update target contact was not found",
		})
		return "", 0, nil
	}

	if mc.Record != nil && (mc.Record.Card.SpeakToAs != nil || len(mc.Record.Card.PersonalInfo) > 0) {
		report.appendIssue(ImportIssue{
			Record:   mc.Ref.String(),
			Field:    "contact",
			Category: ImportIssueCategoryLossy,
			Message:  "merged into an existing contact — pronouns and personal-info entries were not carried over",
		})
	}

	label := system
	if len(label) > 0 {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	if err := CreateMergeNote(tx, userID, existing.ID, &existing, incoming, label); err != nil { // # pragma: no cover — defensive: a healthy notes table does not fail this insert
		report.appendIssue(ImportIssue{
			Record:   mc.Ref.String(),
			Field:    "contact",
			Category: ImportIssueCategoryLossy,
			Message:  "merge note could not be recorded: " + err.Error(),
		})
	}

	MergeImportedContact(&existing, incoming)
	if err := tx.Save(&existing).Error; err != nil { // # pragma: no cover — defensive: the row was just loaded from a healthy migrated schema
		report.appendIssue(ImportIssue{
			Record:   mc.Ref.String(),
			Field:    "contact",
			Category: ImportIssueCategoryInvalid,
			Message:  "failed to save merged contact: " + err.Error(),
		})
		return "", 0, nil
	}

	if err := recordSourceLink(tx, userID, system, mc.Ref.ExternalID,
		models.ImportSourceLinkKindContact, existing.VCardUID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
		return "", 0, err
	}
	report.ContactsUpdated++
	return existing.VCardUID, existing.ID, nil
}

// loadSourceLinks returns the set of already-imported external IDs for a
// system and user, so the import can skip without a per-row query.
func loadSourceLinks(tx *gorm.DB, userID uint, system string) map[string]bool {
	var links []models.ImportSourceLink
	if err := tx.Where("user_id = ? AND system = ?", userID, system).Find(&links).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
		return map[string]bool{}
	}
	out := make(map[string]bool, len(links))
	for _, l := range links {
		out[l.ExternalID] = true
	}
	return out
}

func recordSourceLink(tx *gorm.DB, userID uint, system, externalID, kind, uid string) error {
	return tx.Create(&models.ImportSourceLink{
		UserID:     userID,
		System:     system,
		ExternalID: externalID,
		EntityKind: kind,
		EntityUID:  uid,
	}).Error
}

func importCustomFields(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	refToUID map[string]string, skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	// One FieldDefinition per unique key; later values reuse it (values key on
	// the definition's ID, and the unique (user, key) index forbids duplicates).
	defByKey := map[string]models.FieldDefinition{}
	for _, f := range plan.CustomFields {
		record := f.Ref.String()
		if skipImported(record, f.Ref) {
			continue
		}
		uid, ok := refToUID[f.Contact.ExternalID]
		if !ok {
			report.appendIssue(ImportIssue{
				Record:   record,
				Field:    f.Contact.ExternalID,
				Category: ImportIssueCategoryUnsupported,
				Message:  "references a contact that was not imported",
			})
			continue
		}
		def, ok := defByKey[f.Key]
		if !ok {
			def = models.FieldDefinition{
				UserID:      userID,
				Label:       f.Label,
				Key:         f.Key,
				Target:      models.FieldDefinitionTargetContact,
				Type:        models.FieldTypeText,
				Projection:  "internal-only",
				Sensitivity: models.RelationshipSensitivityNormal,
			}
			if err := tx.Create(&def).Error; err != nil {
				report.appendIssue(ImportIssue{Record: record, Field: "custom_field." + f.Key, Category: ImportIssueCategoryInvalid, Message: err.Error()})
				continue
			}
			defByKey[f.Key] = def
		}
		rawValue, _ := json.Marshal(f.Value)
		fv := models.FieldValue{FieldDefinitionID: def.ID, UserID: userID, EntityID: uid, Value: json.RawMessage(rawValue)}
		if err := tx.Create(&fv).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "custom_field." + f.Key, Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, f.Ref.ExternalID,
			models.ImportSourceLinkKindCustomField, fmt.Sprintf("id:%d", fv.ID)); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.CustomFieldsCreated++
		imported[f.Ref.ExternalID] = true
	}
	return nil
}

// parseSourceTime parses the timestamp formats the source systems write.
// Meerkat stores DATETIME columns as RFC3339 or "2006-01-02 15:04:05" (its
// gorm/sqlite serialization changed over time); Monica uses RFC3339, with
// date-only strings for events. Returns a zero-time error for anything else,
// which the callers report as an invalid field rather than guessing.
//
// Zone-less assumption (docs/adrs/0015-temporal-semantics.md): time.Parse
// (not ParseInLocation) interprets the two naive layouts — "2006-01-02
// 15:04:05" and the date-only "2006-01-02" — as UTC. That is a stated
// assumption about an import source's missing zone, not a verified fact about
// the source's writer.
func parseSourceTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q", s)
}

// -- graph entity importers --------------------------------------------------

func importRelationships(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, rel := range plan.Relationships {
		record := rel.Ref.String()
		if skipImported(record, rel.Ref) {
			continue
		}
		sourceUID, ok := uidOf(record, rel.Source)
		if !ok {
			continue
		}
		targetUID, ok := uidOf(record, rel.Target)
		if !ok {
			continue
		}
		status := rel.Status
		if status == "" {
			status = models.RelationshipStatusConfirmed
		}
		sensitivity := rel.Sensitivity
		if sensitivity == "" {
			sensitivity = models.RelationshipSensitivityNormal
		}
		edge := models.RelationshipEdge{
			UserID:      userID,
			SourceID:    sourceUID,
			TargetID:    targetUID,
			Type:        rel.Type,
			Directional: rel.Directional,
			Source:      models.RelationshipSourceImported,
			Confidence:  1,
			Status:      status,
			Sensitivity: sensitivity,
		}
		if err := tx.Create(&edge).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{
				Record:   record,
				Field:    "relationship",
				Category: ImportIssueCategoryInvalid,
				Message:  err.Error(),
			})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, rel.Ref.ExternalID,
			models.ImportSourceLinkKindRelationship, edge.ID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.RelationshipsCreated++
		imported[rel.Ref.ExternalID] = true
	}
	return nil
}

func importHouseholds(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, hh := range plan.Households {
		record := hh.Ref.String()
		if skipImported(record, hh.Ref) {
			continue
		}
		household := models.Household{
			UserID:  userID,
			Name:    hh.Name,
			Type:    hh.Type,
			Address: hh.Address,
		}
		if err := tx.Create(&household).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "household", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		for _, member := range hh.Members {
			memberUID, ok := uidOf(record+" member", member.Contact)
			if !ok {
				continue
			}
			if err := tx.Create(&models.HouseholdMember{
				HouseholdID:    household.ID,
				UserID:         userID,
				MemberVCardUID: memberUID,
				Role:           member.Role,
				Since:          member.Since,
				Until:          member.Until,
			}).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
				report.appendIssue(ImportIssue{Record: record, Field: "household.member", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			}
		}
		if err := recordSourceLink(tx, userID, plan.System, hh.Ref.ExternalID,
			models.ImportSourceLinkKindHousehold, household.ID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.HouseholdsCreated++
		imported[hh.Ref.ExternalID] = true
	}
	return nil
}

func importCircles(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, c := range plan.Circles {
		record := c.Ref.String()
		if skipImported(record, c.Ref) {
			continue
		}
		circle := models.Circle{UserID: userID, Name: c.Name}
		if err := tx.Create(&circle).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "circle", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		for _, member := range c.Members {
			memberUID, ok := uidOf(record+" member", member)
			if !ok {
				continue
			}
			if err := tx.Create(&models.CircleMember{CircleID: circle.ID, UserID: userID, MemberVCardUID: memberUID}).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
				report.appendIssue(ImportIssue{Record: record, Field: "circle.member", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			}
		}
		if err := recordSourceLink(tx, userID, plan.System, c.Ref.ExternalID,
			models.ImportSourceLinkKindCircle, circle.ID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.CirclesCreated++
		imported[c.Ref.ExternalID] = true
	}
	return nil
}

func importTags(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, t := range plan.Tags {
		record := t.Ref.String()
		if skipImported(record, t.Ref) {
			continue
		}
		tag := models.Tag{UserID: userID, Name: t.Name}
		if err := tx.Create(&tag).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "tag", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		for _, contact := range t.Contacts {
			uid, ok := uidOf(record+" contact", contact)
			if !ok {
				continue
			}
			if err := tx.Create(&models.ContactTag{TagID: tag.ID, UserID: userID, ContactVCardUID: uid}).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
				report.appendIssue(ImportIssue{Record: record, Field: "tag.contact", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			}
		}
		if err := recordSourceLink(tx, userID, plan.System, t.Ref.ExternalID,
			models.ImportSourceLinkKindTag, tag.ID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.TagsCreated++
		imported[t.Ref.ExternalID] = true
	}
	return nil
}

func importGifts(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, g := range plan.Gifts {
		record := g.Ref.String()
		if skipImported(record, g.Ref) {
			continue
		}
		uid, ok := uidOf(record, g.Contact)
		if !ok {
			continue
		}
		gift := models.Gift{
			UserID:      userID,
			EntityID:    uid,
			Status:      g.Status,
			Occasion:    g.Occasion,
			Description: g.Description,
			URL:         g.URL,
			Notes:       g.Notes,
			ValueCents:  g.ValueCents,
			Currency:    g.Currency,
		}
		if g.Date != "" {
			if t, err := parseSourceTime(g.Date); err == nil {
				gift.Date = &t
			} else {
				report.appendIssue(ImportIssue{
					Record:   record,
					Field:    "gift.date",
					Category: ImportIssueCategoryLossy,
					Message:  "unparseable date dropped, gift imported without it: " + g.Date,
				})
			}
		}
		if err := tx.Create(&gift).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "gift", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, g.Ref.ExternalID,
			models.ImportSourceLinkKindGift, gift.ID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.GiftsCreated++
		imported[g.Ref.ExternalID] = true
	}
	return nil
}

func importPreferences(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, p := range plan.Preferences {
		record := p.Ref.String()
		if skipImported(record, p.Ref) {
			continue
		}
		uid, ok := uidOf(record, p.Contact)
		if !ok {
			continue
		}
		pref := models.Preference{
			UserID:      userID,
			EntityID:    uid,
			Category:    p.Category,
			Key:         p.Key,
			Value:       p.Value,
			Notes:       p.Notes,
			Source:      models.PreferenceSourceExternal,
			Sensitivity: models.RelationshipSensitivityNormal,
		}
		if err := tx.Create(&pref).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "preference", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, p.Ref.ExternalID,
			models.ImportSourceLinkKindPreference, pref.ID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.PreferencesCreated++
		imported[p.Ref.ExternalID] = true
	}
	return nil
}

func importNotes(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	refToID map[string]uint, skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, n := range plan.Notes {
		record := n.Ref.String()
		if skipImported(record, n.Ref) {
			continue
		}
		contactID, ok := refToID[n.Contact.ExternalID]
		if !ok {
			report.appendIssue(ImportIssue{
				Record:   record,
				Field:    n.Contact.ExternalID,
				Category: ImportIssueCategoryUnsupported,
				Message:  "references a contact that was not imported",
			})
			continue
		}
		date, err := parseSourceTime(n.Date)
		if err != nil {
			report.appendIssue(ImportIssue{Record: record, Field: "note.date", Category: ImportIssueCategoryInvalid, Message: "unparseable date: " + n.Date})
			continue
		}
		note := models.Note{UserID: userID, Content: n.Content, Date: date, ContactID: &contactID}
		if err := tx.Create(&note).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "note", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, n.Ref.ExternalID,
			models.ImportSourceLinkKindNote, fmt.Sprintf("id:%d", note.ID)); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.NotesCreated++
		imported[n.Ref.ExternalID] = true
	}
	return nil
}

func importActivities(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	uidOf func(string, SourceRef) (string, bool), skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, a := range plan.Activities {
		record := a.Ref.String()
		if skipImported(record, a.Ref) {
			continue
		}
		date, err := parseSourceTime(a.Date)
		if err != nil {
			report.appendIssue(ImportIssue{Record: record, Field: "activity.date", Category: ImportIssueCategoryInvalid, Message: "unparseable date: " + a.Date})
			continue
		}
		var contacts []models.Contact
		for _, ref := range a.Contacts {
			uid, ok := uidOf(record+" attendee", ref)
			if !ok {
				continue
			}
			var c models.Contact
			if err := tx.Where("user_id = ? AND vcard_uid = ?", userID, uid).First(&c).Error; err == nil {
				contacts = append(contacts, c)
			}
		}
		if len(contacts) == 0 {
			report.appendIssue(ImportIssue{Record: record, Field: "activity.attendees", Category: ImportIssueCategoryUnsupported, Message: "no attendee was imported"})
			continue
		}
		activity := models.Activity{
			UserID:      userID,
			Title:       a.Title,
			Description: a.Description,
			Location:    a.Location,
			Date:        date,
			Type:        a.Type,
		}
		if err := tx.Create(&activity).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "activity", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		if err := tx.Model(&activity).Association("Contacts").Replace(&contacts); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "activity.attendees", Category: ImportIssueCategoryInvalid, Message: err.Error()})
		}
		if err := recordSourceLink(tx, userID, plan.System, a.Ref.ExternalID,
			models.ImportSourceLinkKindActivity, activity.UUID); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.ActivitiesCreated++
		imported[a.Ref.ExternalID] = true
	}
	return nil
}

func importReminders(tx *gorm.DB, userID uint, plan *ImportSourcePlan, imported map[string]bool,
	refToID map[string]uint, skipImported func(string, SourceRef) bool, report *ImportReport,
) error {
	for _, r := range plan.Reminders {
		record := r.Ref.String()
		if skipImported(record, r.Ref) {
			continue
		}
		contactID, ok := refToID[r.Contact.ExternalID]
		if !ok {
			report.appendIssue(ImportIssue{
				Record:   record,
				Field:    r.Contact.ExternalID,
				Category: ImportIssueCategoryUnsupported,
				Message:  "references a contact that was not imported",
			})
			continue
		}
		remindAt, err := parseSourceTime(r.RemindAt)
		if err != nil {
			report.appendIssue(ImportIssue{Record: record, Field: "reminder.remind_at", Category: ImportIssueCategoryInvalid, Message: "unparseable date: " + r.RemindAt})
			continue
		}
		reminder := models.Reminder{
			UserID:                userID,
			Message:               r.Message,
			RemindAt:              remindAt,
			Recurrence:            r.Recurrence,
			ReoccurFromCompletion: r.ReoccurFromCompletion,
			ContactID:             &contactID,
		}
		if err := tx.Create(&reminder).Error; err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			report.appendIssue(ImportIssue{Record: record, Field: "reminder", Category: ImportIssueCategoryInvalid, Message: err.Error()})
			continue
		}
		if err := recordSourceLink(tx, userID, plan.System, r.Ref.ExternalID,
			models.ImportSourceLinkKindReminder, fmt.Sprintf("id:%d", reminder.ID)); err != nil { // # pragma: no cover — defensive error handling, unreachable in a healthy migrated schema
			return err
		}
		report.RemindersCreated++
		imported[r.Ref.ExternalID] = true
	}
	return nil
}
