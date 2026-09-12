package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Conventional (not validated — same open-classifier reasoning as
// CRMEnvelope.Kind  and HouseholdMember.Role ) values for
// Activity.Type. docs/adrs/0001-neutral-hub-and-spoke-contact-model.md lists these
// with a trailing "…", signalling an open, extensible set rather than a
// closed system state.
const (
	InteractionTypeCall           = "call"
	InteractionTypeVideoCall      = "video_call"
	InteractionTypeVisit          = "visit"
	InteractionTypeMeal           = "meal"
	InteractionTypeGift           = "gift"
	InteractionTypePhoto          = "photo"
	InteractionTypeMessage        = "message"
	InteractionTypeSharedActivity = "shared_activity"
)

// Activity struct to represent shared activities with one or more contacts
// — "Interaction" in docs/adrs/0001-neutral-hub-and-spoke-contact-model.md ("what
// happened between us"). generalizes rather than replaces this v1:
// UUID/Type/ExternalRef are additive columns alongside the existing int PK,
// following Contact.VCardUID's own precedent (contact.go) for adding a
// stable UUID identity to a table that already has production rows.
type Activity struct {
	gorm.Model
	UserID uint `gorm:"not null;index" json:"-"`

	// UUID is Interaction's stable external identity ("id"), generated
	// in BeforeCreate for new rows; existing rows are backfilled by
	// migrations/000030_add_interaction_fields.
	UUID string `gorm:"column:uuid;index" json:"uuid,omitempty"`

	Title       string    `json:"title" validate:"required,min=1,max=200"`
	Description string    `json:"description" validate:"max=2000"`
	Location    string    `json:"location" validate:"max=300"`
	Date        time.Time `json:"date" validate:"required"`
	Contacts    []Contact `gorm:"many2many:activity_contacts;foreignKey:ID;joinForeignKey:ActivityID;References:ID;joinReferences:ContactID" json:"contacts,omitempty"`

	// Type is Interaction's classifier, conventional/unvalidated —
	// see the InteractionType* constants above.
	Type string `json:"type,omitempty"`

	// ExternalRef optionally links this Interaction to an ExternalActivity
	// or an existing calendar_event_links row
	// (services/calendar_sync_service.go) — a plain opaque string reference,
	// no FK: the referenced tables belong to different, not-yet-built or
	// separately-owned subsystems.
	ExternalRef string `json:"external_ref,omitempty"`

	// ETag is the CalDAV sync-conflict token for this Interaction (T12a),
	// derived from Revision (ADR 0006): e-{id}-{revision}. Explicit gorm
	// column tag is mandatory: without it GORM derives `e_tag` while
	// migration 000041 names the column `etag` — the exact silent mismatch
	// that shipped broken for ContactSyncLink.ETag and killed CardDAV
	// incremental sync.
	// `<-:create` (CON-01, ADR 0018): see Contact.ETag's comment
	// (models/contact.go) for why an ordinary Save()/Updates() must never be
	// able to write this column.
	ETag string `gorm:"column:etag;<-:create" json:"-"`

	// Revision is the monotonic per-row write counter (issue #591, CON-01a —
	// docs/adrs/0006-revision-token-schema.md): starts at 1 on create,
	// incremented on every persisted write, ETag derived from it. Exposed
	// read-only on the wire as `revision` (the model is this entity's
	// response DTO). Migration 000044 adds the column.
	Revision int64 `gorm:"column:revision;<-:create;not null;default:1" json:"revision"`

	// revisionStampedOnCreate: transient marker set by AfterCreate, consumed
	// by the AfterSave GORM fires right after on a Create (see
	// Contact.revisionStampedOnCreate). Keeps a create at revision 1.
	revisionStampedOnCreate bool

	// Deleted is the T17 change-feed tombstone marker, set by the list
	// handler when it reads a row with Unscoped() that has a non-null
	// deleted_at. gorm:"-" keeps it out of the schema; it exists purely so an
	// incremental sync client can apply the deletion.
	Deleted bool `gorm:"-" json:"deleted,omitempty"`
}

// AfterDelete advances updated_at on a soft delete so T17 change feeds see
// the tombstone (see Note.AfterDelete's doc comment for the full rationale).
// Hard deletes and bulk deletes are skipped via the DeletedAt guard.
func (a *Activity) AfterDelete(tx *gorm.DB) error {
	if !a.DeletedAt.Valid {
		return nil
	}
	auditAfterDelete(tx, AuditEntityActivity, a.UUID, a.UserID, a)
	return tx.Model(&Activity{}).Unscoped().Where("id = ?", a.ID).UpdateColumn("updated_at", time.Now()).Error
}

// BeforeCreate generates a UUID for new Activities/Interactions, mirroring
// Household's own BeforeCreate (household.go) even though, unlike Household,
// Activity keeps its existing int primary key — only UUID is new here.
func (a *Activity) BeforeCreate(tx *gorm.DB) error {
	if a.UUID == "" {
		a.UUID = uuid.New().String()
	}
	return nil
}

// AfterCreate assigns the initial ETag now that ID is persisted, mirroring
// Contact.AfterCreate (contact.go): stamp revision 1 and derive the ETag from
// it (ADR 0006). UpdateColumns bypasses GORM's update hooks, so this cannot
// recursively trigger AfterSave.
func (a *Activity) AfterCreate(tx *gorm.DB) error {
	a.Revision = 1
	a.ETag = fmt.Sprintf("e-%d-%d", a.ID, a.Revision)
	a.revisionStampedOnCreate = true
	// tx.Table(...), not tx.Model(a): revision/etag are `<-:create` (CON-01,
	// ADR 0018), so a Model-scoped statement would have this write filtered
	// out by GORM's own field-permission check.
	return tx.Table("activities").Where("id = ?", a.ID).UpdateColumns(map[string]any{"revision": a.Revision, "etag": a.ETag}).Error
}

// AfterSave refreshes the ETag on every real write, exactly like
// Contact.AfterSave: bump the monotonic revision counter and re-derive the
// ETag from it (ADR 0006), so two writes inside the same second can no longer
// collide. The old scheme compared the recomputed would-be value against the
// stored one and only rewrote on change — with a counter there is no
// would-be value to compare against, every persisted write is a new revision.
//
// Guard: a zero-value ID means this hook fired on a bulk
// Model(&Activity{}).Where(...).Update/Updates call, not on a real row — the
// receiver has no primary key, so UpdateColumns would widen to every row in
// the table. Never derive/write a revision in that case (the caller is
// responsible for load-then-save updates that should bump revisions).
func (a *Activity) AfterSave(tx *gorm.DB) error {
	if a.ID == 0 {
		return nil
	}
	// T18 audit fires first: the UpdateColumns below swaps in a fresh
	// statement, which would otherwise wipe the audit's instance state.
	auditAfterSave(tx, AuditEntityActivity, a.UUID, a.UserID)
	if a.revisionStampedOnCreate {
		a.revisionStampedOnCreate = false
		return nil
	}
	// CON-01 (issues #920, #924; ADR 0018): atomic compare-and-swap against
	// the revision this struct was loaded at, not a read-modify-write — see
	// bumpRevisionCAS's doc comment (models/revision_cas.go).
	newRevision, err := bumpRevisionCAS(tx, "Activity", "activities", a.ID, a.Revision, func(nr int64) string {
		return fmt.Sprintf("e-%d-%d", a.ID, nr)
	})
	if err != nil {
		return err
	}
	a.Revision = newRevision
	a.ETag = fmt.Sprintf("e-%d-%d", a.ID, newRevision)
	return nil
}

// nonQualifyingInteractionTypes are passive/social-media-like interaction
// types that do not count toward a relationship-maintenance cadence —
// everything else counts by default. No consumer exists yet
// (cadence is unbuilt), matching how defined RelationshipSource
// constants before had a consumer for them.
var nonQualifyingInteractionTypes = map[string]bool{
	InteractionTypePhoto: true,
}

// Qualifying reports whether this Interaction counts toward a cadence policy
// ("qualifying" field) — derived from Type, not stored.
func (a *Activity) Qualifying() bool {
	return !nonQualifyingInteractionTypes[a.Type]
}
