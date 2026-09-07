package models

import (
	"fmt"
	"os"
	"strings"
	"time"

	"mycorrhizal/contactmodel"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DefaultPhotoDir is the configured profile-photo directory
// (config.Config.ProfilePhotoDir), read directly from the PROFILE_PHOTO_DIR
// environment variable (the same variable config.LoadConfig() reads) rather
// than threaded in from main.go: BeforeSave is a GORM hook with a fixed
// signature (tx *gorm.DB) error — it has no per-call parameter to receive a
// photoDir through, unlike RecordFromContact/ApplyRecordToContact's own
// explicit photoDir parameter (added  photo-bridging
// prerequisite, docs/adrs/0001-neutral-hub-and-spoke-contact-model.md). A
// package-level var populated at process-init time is the least-invasive way
// to give BeforeSave the same capability without changing its signature or
// reaching into files outside backend/models' file scope (this WP does
// not touch main.go). Environment variables are already present in the OS
// process environment before the Go binary starts (this codebase does not
// load a .env file itself — see config/config.go), so reading it here at var-
// init time is equivalent to config.LoadConfig() reading it moments later in
// main(). Empty ("") is a safe default: RecordFromContact's photo bridging
// degrades gracefully to the base64 PhotoThumbnail fallback (or is skipped
// entirely if neither Photo nor PhotoThumbnail is set), never panics.
var DefaultPhotoDir = os.Getenv("PROFILE_PHOTO_DIR")

// ContactEmail is a single typed email address (vCard EMAIL).
type ContactEmail struct {
	Type  string `json:"type" validate:"max=30"`
	Value string `json:"value" validate:"required,email"`
}

// ContactPhone is a single typed phone number (vCard TEL).
type ContactPhone struct {
	Type  string `json:"type" validate:"max=30"`
	Value string `json:"value" validate:"required,phone"`
}

// ContactURL is a single typed website URL (vCard URL).
type ContactURL struct {
	Type  string `json:"type" validate:"max=30"`
	Value string `json:"value" validate:"required,max=500,safeurl"`
}

// ContactIMPP is a single instant-messaging / social handle (vCard IMPP).
// Type holds the service (e.g. "telegram", "signal"); Value holds the handle.
type ContactIMPP struct {
	Type  string `json:"type" validate:"max=30"`
	Value string `json:"value" validate:"required,max=200,safeurl"`
}

// ContactAddress is a single structured postal address (vCard ADR).
//
// T79
// widened this from the original five fields with the sub-street slots a vCard
// ADR can carry and a person actually types: POBox (vCard ADR position 1),
// Apartment (position 2, the "extended address" / address-line-2 slot), and
// Floor (RFC 9554). The remaining nine RFC 9553 kinds (room, building, block,
// number, district, subdistrict, direction, landmark, separator) stay
// nested-only — no editor demand, and they would make the form unusable.
type ContactAddress struct {
	Type      string `json:"type" validate:"max=30"`
	Street    string `json:"street" validate:"max=500"`
	City      string `json:"city" validate:"max=200"`
	Region    string `json:"region" validate:"max=200"`
	Postal    string `json:"postal" validate:"max=30"`
	Country   string `json:"country" validate:"max=100"`
	POBox     string `json:"pobox" validate:"max=200"`
	Apartment string `json:"apartment" validate:"max=200"`
	Floor     string `json:"floor" validate:"max=100"`
}

type Contact struct {
	gorm.Model
	UserID    uint   `gorm:"not null;index" json:"-"`
	Firstname string `gorm:"type:text not null COLLATE NOCASE" json:"firstname" validate:"required,min=1,max=100"`
	Lastname  string `gorm:"type:text COLLATE NOCASE" json:"lastname" validate:"max=100"`
	Nickname  string `gorm:"type:text COLLATE NOCASE" json:"nickname" validate:"max=50"`
	Gender    string `json:"gender" validate:"omitempty,max=100"`
	Email     string `gorm:"type:text COLLATE NOCASE" json:"email" validate:"omitempty,email"`
	Phone     string `json:"phone" validate:"omitempty,phone"`
	// Birthday is a date-only calendar date with no time and no zone (never
	// zone-converted; see docs/adrs/0015-temporal-semantics.md). A full
	// YYYY-MM-DD value is date-only; a year-less --MM-DD value is the partial
	// date form. String, not time.Time, because vCard BDAY permits the
	// year-less form no timestamp type can carry; validated by the "birthday"
	// custom validator (middleware/validation.go).
	Birthday           string     `json:"birthday" validate:"omitempty,birthday"`
	Photo              string     `json:"photo"`                                                               // Path to the profile photo
	PhotoThumbnail     string     `json:"-"`                                                                   // Base64 data URL of thumbnail (not exposed in JSON directly)
	Address            string     `json:"address" validate:"max=500"`                                          // Full address as a string
	HowWeMet           string     `gorm:"serializer:encrypted" json:"how_we_met" validate:"max=1000"`          // Text field
	WorkInformation    string     `gorm:"serializer:encrypted" json:"work_information" validate:"max=1000"`    // Text field
	ContactInformation string     `gorm:"serializer:encrypted" json:"contact_information" validate:"max=1000"` // Additional contact information
	Circles            []string   `gorm:"type:text;serializer:json" json:"circles"`                            // Serialize Circles properly
	Activities         []Activity `gorm:"many2many:activity_contacts;foreignKey:ID;joinForeignKey:ContactID;References:ID;joinReferences:ActivityID" json:"activities,omitempty"`
	Notes              []Note     `json:"notes,omitempty"`     // One-to-many relationship with notes
	Reminders          []Reminder `json:"reminders,omitempty"` // One-to-many relationship with reminders

	// ImportedTags stages tag-shaped grouping values parsed out of a CSV/vCard
	// import ("tags", "labels", "categories" columns) between parsing and
	// services.MaterializeImportedGroupings turning them into real Tag +
	// ContactTag rows. NOT persisted (`gorm:"-"`) and never serialized: Tag is
	// a first-class entity, so unlike the legacy flat Circles column there is
	// no tag column on `contacts` for this to live in, and adding one would
	// recreate exactly the split-brain T3 exists to close.
	ImportedTags []string `gorm:"-" json:"-"`

	// Multi-valued vCard fields (stored as JSON arrays). The legacy Email/Phone/Address
	// scalars above are kept in sync (see BeforeSave) as the denormalized "primary" value
	// used for search and list views.
	Emails    []ContactEmail   `gorm:"column:emails;type:text;serializer:json" json:"emails"`
	Phones    []ContactPhone   `gorm:"column:phones;type:text;serializer:json" json:"phones"`
	Addresses []ContactAddress `gorm:"column:addresses;type:text;serializer:json" json:"addresses"`
	URLs      []ContactURL     `gorm:"column:urls;type:text;serializer:json" json:"urls"`
	IMPPs     []ContactIMPP    `gorm:"column:impps;type:text;serializer:json" json:"impps"`

	// Structured name parts (vCard N)
	Prefix     string `gorm:"type:text" json:"prefix" validate:"max=50"`
	MiddleName string `gorm:"type:text" json:"middle_name" validate:"max=100"`
	Suffix     string `gorm:"type:text" json:"suffix" validate:"max=50"`

	// Organizational fields (vCard ORG / TITLE / ROLE)
	Organization string `gorm:"type:text" json:"organization" validate:"max=200"`
	Department   string `gorm:"type:text" json:"department" validate:"max=200"`
	JobTitle     string `gorm:"type:text" json:"job_title" validate:"max=200"`
	Role         string `gorm:"type:text" json:"role" validate:"max=200"`

	// Anniversary date (vCard X-ANNIVERSARY), same format as Birthday
	// (date-only / partial, no time, no zone — docs/adrs/0015).
	Anniversary string `json:"anniversary" validate:"omitempty,birthday"`

	// CardDAV fields
	VCardUID   string `gorm:"column:vcard_uid;index" json:"-"` // Permanent RFC 6350 UID
	VCardExtra string `gorm:"column:vcard_extra" json:"-"`     // JSON for unmapped vCard properties

	// ETag is the CardDAV sync-conflict token, derived from Revision (see
	// ADR 0006): e-{id}-{revision}. Explicit gorm column tag is mandatory
	// (CLAUDE.md backend trap 1 — GORM would derive `e_tag`).
	ETag string `gorm:"column:etag" json:"-"`
	// Revision is the monotonic per-row write counter (issue #591, CON-01a —
	// docs/adrs/0006-revision-token-schema.md): starts at 1 on create,
	// incremented on every persisted write, and the ETag is derived from it
	// (immune to the old Unix()-resolution lost-update hole). json:"-" here:
	// surfaced read-only on ContactRecordResponse/ContactSummary, never on
	// the raw Contact JSON.
	Revision int64 `gorm:"column:revision;not null;default:1" json:"-"`

	// revisionStampedOnCreate is a transient, in-memory-only marker
	// (unexported, so GORM ignores it entirely — no column, nothing to tag)
	// set by AfterCreate and consumed by the AfterSave that GORM fires
	// immediately after on a Create (GORM runs both hooks). Without it,
	// AfterSave's unconditional revision bump would turn every create into
	// revision 2; the marker tells it "the create already stamped revision 1".
	revisionStampedOnCreate bool

	Archived bool `gorm:"default:false" json:"archived"`

	// IsFavorite is a CRM-local flag (issue #173) with the same standing as
	// Archived: it is user-set, not derived, and it has no neutral-model home
	// (favoriting is not a vCard/JSContact property and must never leak into
	// CardDAV or the vCard3/vCard4/JSContact exporters). The explicit column
	// tag guards against GORM casing drift from the migration SQL (CLAUDE.md
	// backend trap 1). Rides through BeforeSave/BeforeCreate untouched.
	IsFavorite bool `gorm:"column:is_favorite;default:false" json:"is_favorite"`

	// Neutral RFC 9553/9554/9555 representation (P1 — see
	// docs/adrs/0001-neutral-hub-and-spoke-contact-model.md). This is a second,
	// parallel representation of the same data already held in the legacy
	// flat/array fields above: purely additive, nothing existing is removed,
	// renamed, or stops being populated. Populated by RecordFromContact (see
	// contact_record.go) via BeforeSave on every save, and by the one-shot
	// cmd/backfill-contact-records tool for rows that predate this WP.
	// Nothing else reads these fields yet (hence json:"-": exposing them on
	// the wire is P2's job,  API/DTO rewrite), so adding them
	// carries no compile or behavior risk to any other package.
	Card        contactmodel.Card        `gorm:"column:card;type:text;serializer:encryptedjson" json:"-"`
	CRM         contactmodel.CRMEnvelope `gorm:"column:crm;type:text;serializer:encryptedjson" json:"-"`
	Passthrough contactmodel.Passthrough `gorm:"column:passthrough;type:text;serializer:encryptedjson" json:"-"`

	// Derived projection scalars with no existing legacy analog
	// (contactmodel.Projection.FN / .Org). Populated the same way as
	// Firstname/Lastname/Email/Phone/Birthday below, via DeriveProjection.
	FN  string `gorm:"column:fn" json:"-"`
	Org string `gorm:"column:org" json:"-"`

	// AddressesFlat is the denormalized, searchable concatenation of every
	// Addresses[] entry (see FlattenAddresses), kept in sync by BeforeSave
	// like the legacy Address scalar and indexed into contacts_fts so search
	// finds street names/cities (T38). Derived data — rebuildable from the
	// addresses JSON at any time — so it is deliberately not part of the API
	// surface. Backfilled for pre-existing rows by migration 000010.
	AddressesFlat string `gorm:"column:addresses_flat;type:text" json:"-"`

	// PhonesNormalized is the denormalized, searchable concatenation of every
	// Phones[] entry's full digit string and its PhoneKey (see FlattenPhones),
	// kept in sync by BeforeSave like the legacy Phone scalar and indexed into
	// contacts_fts so search finds a number regardless of punctuation,
	// grouping, or country-code differences between the query and the stored
	// value (T69). Covers every array entry, not just the flat primary. Derived
	// data — rebuildable from the phones JSON at any time — so it is
	// deliberately not part of the API surface. Backfilled for pre-existing
	// rows by migration 000020.
	PhonesNormalized string `gorm:"column:phones_normalized;type:text" json:"-"`

	// SortName is the denormalized, pre-lowercased key the name-sorted
	// contacts list orders by (T73): lower(trim(lastname)) when the contact
	// has a lastname, else lower(trim(firstname)) — see DeriveSortName. Kept
	// in sync by BeforeSave like the AddressesFlat/PhonesNormalized derived
	// columns; backfilled for pre-existing rows by migration 000021. Derived
	// data — rebuildable at any time — so it is deliberately not part of the
	// API surface. Never empty for a valid contact (Firstname is required).
	SortName string `gorm:"column:sort_name;type:text" json:"-"`

	// cardSetDirectly is a transient, in-memory-only marker (unexported, so
	// GORM ignores it entirely — no column, nothing to tag) set by
	// ApplyRecordToContact (contact_record_reverse.go, P2) to tell
	// BeforeSave below "Card/CRM/Passthrough were just set directly from an
	// authoritative contactmodel.Record — do not re-derive and overwrite them
	// from the flat legacy fields on this save."
	//
	// Without this, BeforeSave's original (P1) unconditional
	// `c.Card = RecordFromContact(c, photoDir).Card` would silently discard
	// any Card-only data with no flat-field home (SpeakToAs, PersonalInfo,
	// SocialProfiles, OtherOnlineServices, Keywords, extra name
	// components, additional Organizations/Titles, RelatedTo, Members,
	// Localizations, ...) on every single save of a contact created/updated
	// through the new nested REST API or the VCF/JSContact import path —
	// defeating the entire point  accepting/returning the full
	// neutral Record. Flat-field-only writers (CSV import's
	// BuildContactFromRow, MergeImportedContact's merge-by-flat-fields path,
	// and anything else that never calls ApplyRecordToContact) never set
	// this flag, so BeforeSave's original flat->Card derivation keeps running
	// for them exactly as it did  — this is what keeps their Card
	// column in sync at all, since they have no other way to populate it.
	cardSetDirectly bool
}

// renders a structured address as a single human-readable line, used to keep the legacy Address scalar in sync for search/list views.
// T79:
// the sub-street parts (PO box / apartment / floor) sit between street and
// city, the conventional ordering. Migration 000022's SQL backfill mirrors
// this exact component order.
func FormatAddress(a ContactAddress) string {
	parts := []string{}
	for _, p := range []string{a.Street, a.POBox, a.Apartment, a.Floor, a.City, a.Region, a.Postal, a.Country} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// FlattenAddresses renders every structured address as one searchable string:
// each address's non-empty components joined with ", " (FormatAddress), the
// addresses joined with a space. It feeds the denormalized AddressesFlat
// column (T38), which contacts_fts indexes — mirroring how FormatAddress
// keeps the legacy Address scalar in sync, but over the whole array rather
// than just the first entry. Since FormatAddress carries the T79 sub-street
// parts (PO box / apartment / floor), an imported apartment or PO box is
// findable by search. Migration 000022's SQL backfill mirrors this exact
// shape for pre-existing rows.
func FlattenAddresses(addresses []ContactAddress) string {
	parts := make([]string, 0, len(addresses))
	for _, a := range addresses {
		parts = append(parts, FormatAddress(a))
	}
	return strings.Join(parts, " ")
}

// FlattenPhones renders every phone entry as one searchable string: each
// entry's full digit string (NormalizePhoneDigits) plus its PhoneKey (the
// last-10-digits key, T68) when that differs, all joined with a space. It
// feeds the denormalized PhonesNormalized column (T69), which contacts_fts
// indexes. Emitting both tokens is what makes cross-format search work in
// both directions: a query of the full digits ("18005551234") hits the first
// token, a query of the canonical key ("8005551234") hits the second, for a
// number stored either way. Entries with no digits at all contribute nothing.
// Migration 000020's SQL backfill deliberately mirrors this exact shape for
// pre-existing rows, so this testable function is what keeps new-contact and
// pre-existing-contact search behavior from silently diverging.
func FlattenPhones(phones []ContactPhone) string {
	parts := make([]string, 0, 2*len(phones))
	for _, p := range phones {
		digits := NormalizePhoneDigits(p.Value)
		if digits == "" {
			continue
		}
		parts = append(parts, digits)
		if key := PhoneKey(p.Value); key != "" && key != digits {
			parts = append(parts, key)
		}
	}
	return strings.Join(parts, " ")
}

// DeriveSortName computes the denormalized name-sort key a name-ordered
// contacts list orders by (T73): lower(trim(lastname)) when non-empty, else
// lower(trim(firstname)). Pre-lowercased so SQLite collation never
// participates in the ORDER BY or its cursor predicate, and never empty for a
// valid contact (Firstname is required). Migration 000021's SQL backfill
// deliberately mirrors this exact shape for pre-existing rows, so this
// testable function is what keeps new-contact and pre-existing-contact sort
// behavior from silently diverging.
//
// Note: strings.ToLower folds Unicode, while SQLite's built-in lower() in the
// migration backfill is ASCII-only — so for a non-ASCII name the two paths
// produce slightly different sort keys until the contact is next saved. That
// is a cosmetic ordering difference only (pagination stays total, see the
// migration's KNOWN LIMITATION note), not a correctness problem.
func DeriveSortName(lastname, firstname string) string {
	if trimmed := strings.TrimSpace(lastname); trimmed != "" {
		return strings.ToLower(trimmed)
	}
	return strings.ToLower(strings.TrimSpace(firstname))
}

// BeforeSave keeps the denormalized primary scalars (Email/Phone/Address),
// the searchable AddressesFlat column (T38), PhonesNormalized column (T69),
// and the name-sort SortName column (T73) in sync with the JSON arrays and
// names, and keeps the neutral Card/CRM/Passthrough representation (and its
// own derived projection scalars) in sync with the legacy fields on every
// create/update.
//
// RecordFromContact + contactmodel.DeriveProjection is now the single source
// of truth for Firstname/Lastname/Email/Phone/Birthday/FN/Org: the old
// ad-hoc "first array entry wins" logic for Email/Phone is superseded by
// DeriveProjection's own (equivalent, Pref-aware) primary-value selection,
// so there is one derivation path, not two competing ones (see
// docs/adrs/0001-neutral-hub-and-spoke-contact-model.md ). Address has no
// neutral projection field (Address stays a free-text legacy scalar), so its
// ad-hoc sync from the first Addresses[] entry is kept as-is.
//
// Projection values only overwrite their legacy scalar when non-empty, the
// same "only sync when there's something to sync" semantics the original
// Email/Phone logic had (`if len(c.Emails) > 0 { ... }`) — this matters
// because some existing contacts only ever had the scalar Email/Phone set
// directly, without ever populating the Emails/Phones arrays; DeriveProjection
// on those rows also lands back on the same scalar value (RecordFromContact
// falls back to the scalar when its array is empty — see contact_record.go),
// so this is a no-op for them, not a silent blank-out. FN and Org are new
// columns with no prior value and no back-compat concern, so they are always
// assigned directly.
//
// T75 address-merge rule (T75): on the non-cardSetDirectly path, the fresh Card
// derivation is MERGED onto the loaded Card, not substituted for it. Flat
// fields are authoritative for what they can express; the loaded Card is
// authoritative for what they cannot. Flat-owned Card sub-structures are
// compared per-entry against the loaded Card's flat projection — an entry
// whose projection is unchanged survives whole (unprojected address
// components included); a changed or newly-appended entry is rebuilt from
// flat. Card members with no flat representation (SpeakToAs, PersonalInfo,
// CRMEnvelope.Kind, ...) are preserved unconditionally. The full rule with
// its rationale lives on mergeRecordFromFlat in contact_card_merge.go.
func (c *Contact) BeforeSave(tx *gorm.DB) error {
	c.deriveDenormalized()

	// T18 audit: mark create-vs-update and capture the pre-update snapshot.
	auditBeforeSave[Contact](tx, AuditEntityContact, c.ID, c.ID == 0)

	return nil
}

// deriveDenormalized recomputes every denormalized/derived field on the
// contact from its authoritative source — the nested Card (and its
// projection), or, on a cardSetDirectly save, the Record ApplyRecordToContact
// has just installed. It is the single derivation the write path
// (BeforeSave), the standalone rebuild (services.RebuildDerivedContactColumns)
// and the INV-A5 consistency probe (issue #497) all run, so a rebuilt or
// checked column can never disagree with what a plain re-save would have
// written.
//
// It mutates only the receiver's derived fields — the flat scalars,
// AddressesFlat / PhonesNormalized / SortName, FN / Org, and the T75-merged
// Card / CRM / Passthrough — and performs no I/O. A caller that only wants to
// inspect the result without persisting it (the probe) runs this on a copy
// and diffs the derived fields; the shallow copy is safe because every
// assignment here replaces a field wholesale rather than mutating a shared
// backing array.
func (c *Contact) deriveDenormalized() {
	if len(c.Emails) > 0 {
		c.Email = c.Emails[0].Value
	}
	if len(c.Phones) > 0 {
		c.Phone = c.Phones[0].Value
	}
	if len(c.Addresses) > 0 {
		c.Address = FormatAddress(c.Addresses[0])
	}
	c.AddressesFlat = FlattenAddresses(c.Addresses)
	c.PhonesNormalized = FlattenPhones(c.Phones)

	var record *contactmodel.Record
	if c.cardSetDirectly {
		// Card/CRM/Passthrough were just set directly by ApplyRecordToContact
		// from an authoritative Record (new nested REST input, or a VCF/
		// JSContact import) — use that Record's own values for the derived
		// projection below, but leave c.Card/c.CRM/c.Passthrough untouched
		// rather than truncating them back down to what the (necessarily
		// lossy) flat fields alone could reconstruct. See the cardSetDirectly
		// field doc above.
		record = &contactmodel.Record{Card: c.Card, Envelope: c.CRM, Passthrough: c.Passthrough}
		c.cardSetDirectly = false // one-shot: only guards the save that immediately follows ApplyRecordToContact
	} else {
		// T75: merge the fresh flat-field derivation onto the loaded
		// full-fidelity Card/CRM/Passthrough rather than replacing it
		// wholesale, so Card-only data with no flat home (SpeakToAs,
		// PersonalInfo, unprojected address components, rich per-entry
		// metadata, CRMEnvelope.Kind, Passthrough) survives a plain save.
		// The exact merge rule is documented on mergeRecordFromFlat
		// (contact_card_merge.go) — flat fields are authoritative for what
		// they can express, loaded Card for what they cannot.
		fresh := RecordFromContact(c, DefaultPhotoDir)
		merged := mergeRecordFromFlat(
			contactmodel.Record{Card: c.Card, Envelope: c.CRM, Passthrough: c.Passthrough},
			*fresh,
		)
		record = &merged
		c.Card = record.Card
		c.CRM = record.Envelope
		c.Passthrough = record.Passthrough
	}

	proj := contactmodel.DeriveProjection(record)
	if proj.Firstname != "" {
		c.Firstname = proj.Firstname
	}
	if proj.Lastname != "" {
		c.Lastname = proj.Lastname
	}
	if proj.PrimaryEmail != "" {
		c.Email = proj.PrimaryEmail
	}
	if proj.PrimaryPhone != "" {
		c.Phone = proj.PrimaryPhone
	}
	if proj.Birthday != "" {
		c.Birthday = proj.Birthday
	}
	c.FN = proj.FN
	c.Org = proj.Org

	// T73: the name-sort key must be derived from the FINAL Firstname/
	// Lastname (the projection assignments above may have just replaced
	// them), so it is computed here, after them.
	c.SortName = DeriveSortName(c.Lastname, c.Firstname)
}

// generates VCardUID for new contacts
func (c *Contact) BeforeCreate(tx *gorm.DB) error {
	// Generate VCardUID if not set (required for unique constraint)
	if c.VCardUID == "" {
		c.VCardUID = uuid.New().String()
	}
	return nil
}

func (c *Contact) AfterCreate(tx *gorm.DB) error {
	// Now we have the ID, stamp the initial revision and derive the ETag from
	// it (ADR 0006). UpdateColumn(s) bypasses GORM's update hooks, so this
	// cannot recursively trigger AfterSave. The marker tells the AfterSave
	// that follows on a Create not to bump (a create is revision 1, not 2).
	c.Revision = 1
	c.ETag = fmt.Sprintf("e-%d-%d", c.ID, c.Revision)
	c.revisionStampedOnCreate = true
	return tx.Model(c).Where("id = ?", c.ID).UpdateColumns(map[string]any{"revision": c.Revision, "etag": c.ETag}).Error
}

func (c *Contact) AfterSave(tx *gorm.DB) error {
	if c.ID == 0 {
		return nil
	}
	// T18 audit fires first: the UpdateColumns below swaps in a fresh
	// statement, which would otherwise wipe the audit's instance state
	// (is_new / before) captured in BeforeSave.
	auditAfterSave(tx, AuditEntityContact, c.VCardUID, c.UserID)
	if c.revisionStampedOnCreate {
		c.revisionStampedOnCreate = false
		return nil
	}
	// Every persisted write bumps the monotonic revision counter, and the
	// ETag is re-derived from it (ADR 0006) — two writes inside the same
	// second can no longer collide, unlike the old UpdatedAt.Unix() scheme.
	c.Revision++
	c.ETag = fmt.Sprintf("e-%d-%d", c.ID, c.Revision)
	return tx.Model(c).Where("id = ?", c.ID).UpdateColumns(map[string]any{"revision": c.Revision, "etag": c.ETag}).Error
}

// AfterDelete advances updated_at on a soft delete so T17 change feeds see
// the tombstone (see Note.AfterDelete's doc comment for the full rationale).
// Hard deletes and bulk deletes are skipped via the DeletedAt guard.
func (c *Contact) AfterDelete(tx *gorm.DB) error {
	if !c.DeletedAt.Valid {
		return nil
	}
	auditAfterDelete(tx, AuditEntityContact, c.VCardUID, c.UserID, c)
	return tx.Model(&Contact{}).Unscoped().Where("id = ?", c.ID).UpdateColumn("updated_at", time.Now()).Error
}
