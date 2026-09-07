package models

import (
	"fmt"
	"time"

	"mycorrhizal/contactmodel"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Conventional (not validated — same open-classifier reasoning as
// Activity.Type/InteractionType* above and HouseholdMember.Role) values for
// LifeEvent.Type. lists these with a trailing "…", signalling an open,
// extensible set.
//
// T36 added the 37
// tokens below the original seven — Monica-style categorized defaults, five
// per-category groups reproduced in full in the ticket. Each token's
// category lives in life_event_type_registry.go's LifeEventTypeCategories,
// not here — this block only owns the stored string values. The original
// seven keep their exact stored values even where T36 changed a display
// label (job_change -> "Started a new job", adopted_pet -> "Got a pet"):
// that's an i18n-only change, never a rename of what's persisted.
const (
	LifeEventTypeMarried    = "married"
	LifeEventTypeGraduated  = "graduated"
	LifeEventTypeJobChange  = "job_change"
	LifeEventTypeHadChild   = "had_child"
	LifeEventTypeAdoptedPet = "adopted_pet"
	LifeEventTypeRetired    = "retired"
	LifeEventTypeMoved      = "moved"

	// Home & Living
	LifeEventTypeBoughtAHome          = "bought_a_home"
	LifeEventTypeMadeAHomeImprovement = "made_a_home_improvement"
	LifeEventTypeWentOnHolidays       = "went_on_holidays"
	LifeEventTypeGotANewVehicle       = "got_a_new_vehicle"
	LifeEventTypeGotARoommate         = "got_a_roommate"

	// Health & Wellness
	LifeEventTypeOvercameAnIllness               = "overcame_an_illness"
	LifeEventTypeQuitAHabit                      = "quit_a_habit"
	LifeEventTypeStartedNewEatingHabits          = "started_new_eating_habits"
	LifeEventTypeLostWeight                      = "lost_weight"
	LifeEventTypeStartedWearingGlassesOrContacts = "started_wearing_glasses_or_contacts"
	LifeEventTypeBrokeABone                      = "broke_a_bone"
	LifeEventTypeRemovedBraces                   = "removed_braces"
	LifeEventTypeHadSurgery                      = "had_surgery"
	LifeEventTypeWentToTheDentist                = "went_to_the_dentist"

	// Work & Education
	LifeEventTypeStartedSchool          = "started_school"
	LifeEventTypeStudiedAbroad          = "studied_abroad"
	LifeEventTypeStartedVolunteering    = "started_volunteering"
	LifeEventTypePublishedAPaper        = "published_a_paper"
	LifeEventTypeStartedMilitaryService = "started_military_service"

	// Travel & Experiences
	LifeEventTypeStartedASport           = "started_a_sport"
	LifeEventTypeStartedAHobby           = "started_a_hobby"
	LifeEventTypeLearnedANewInstrument   = "learned_a_new_instrument"
	LifeEventTypeLearnedANewLanguage     = "learned_a_new_language"
	LifeEventTypeGotATattooOrPiercing    = "got_a_tattoo_or_piercing"
	LifeEventTypeGotALicense             = "got_a_license"
	LifeEventTypeTraveled                = "traveled"
	LifeEventTypeGotAnAchievementOrAward = "got_an_achievement_or_award"
	LifeEventTypeChangedBeliefs          = "changed_beliefs"
	LifeEventTypeSpokeForTheFirstTime    = "spoke_for_the_first_time"
	LifeEventTypeKissedForTheFirstTime   = "kissed_for_the_first_time"

	// Family & Relationships
	LifeEventTypeStartedARelationship = "started_a_relationship"
	LifeEventTypeGotEngaged           = "got_engaged"
	LifeEventTypeAnniversary          = "anniversary"
	LifeEventTypeExpectsABaby         = "expects_a_baby"
	LifeEventTypeAddedAFamilyMember   = "added_a_family_member"
	LifeEventTypeEndedARelationship   = "ended_a_relationship"
	LifeEventTypeLostALovedOne        = "lost_a_loved_one"
)

// Category values for LifeEvent.Category (T36). A closed set, unlike Type —
// validated via LifeEventInput's `oneof` struct tag.
const (
	LifeEventCategoryHomeLiving          = "home_living"
	LifeEventCategoryHealthWellness      = "health_wellness"
	LifeEventCategoryWorkEducation       = "work_education"
	LifeEventCategoryTravelExperiences   = "travel_experiences"
	LifeEventCategoryFamilyRelationships = "family_relationships"
)

// Provenance values stored on LifeEvent.Source, following
// RelationshipEdge.Source's per-entity-local-enum convention.
const (
	LifeEventSourceUser        = "user"
	LifeEventSourceImported    = "imported"
	LifeEventSourceAISuggested = "ai-suggested"
)

// LifeEvent is a permanent fact about an entity's life (docs/adrs/0001-neutral-hub-and-spoke-contact-model.md) — "what happened in *their* life", as
// opposed to Interaction/Activity ("what happened between *us*").
//
// UUID-string-primary-key entity, following Household's exact template
// (household.go): ID generated in BeforeCreate. Soft-deletes (deleted_at)
// added per T5 — LifeEvent is first-class user-authored content, same shape
// as Note, not a graph-adjacent join row.
type LifeEvent struct {
	ID        string         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	UserID uint `gorm:"not null;index" json:"-"`

	// EntityID is the subject Contact, referenced by Contact.VCardUID — the
	// same graph invariant RelationshipEdge.SourceID/TargetID and
	// HouseholdMember.MemberVCardUID follow .
	EntityID string `gorm:"column:entity_id;not null;index" json:"entity_id" validate:"required,uuid4"`

	// Type is conventional/unvalidated — see the LifeEventType* constants
	// above.
	Type string `json:"type,omitempty"`

	// Category is a closed classifier (T36) — unlike Type, it's validated
	// (LifeEventInput's `oneof` tag) against the five LifeEventCategory*
	// tokens above. Nullable: existing pre-T36 rows only got a category via
	// migration 000011's backfill where their Type mapped onto one of the
	// original seven constants; everything else stays NULL/uncategorized
	// rather than guessing (see that migration's comment). A custom
	// (free-text Type) event still requires a category from the frontend's
	// per-category "Add a new life event type" affordance.
	Category string `gorm:"column:category" json:"category,omitempty" validate:"omitempty,life_event_category"`

	// Date reuses contactmodel.PartialDate own instruction
	// ("life events are often known only to a year"), JSON-serialized like
	// Household.Address. A date-only / partial calendar date with no time and
	// no zone — never zone-converted (docs/adrs/0015-temporal-semantics.md).
	// A month/day value recurs annually (1 March for a year-less 29-Feb in a
	// non-leap year); a year-only value has no annual occurrence.
	Date *contactmodel.PartialDate `gorm:"type:text;serializer:json" json:"date,omitempty"`

	Description string `gorm:"serializer:encrypted" json:"description,omitempty" validate:"max=2000"`

	Source string `json:"source,omitempty" validate:"omitempty,oneof=user imported ai-suggested"`

	// RelatedEntityIDs holds other Contact.VCardUIDs this event involves —
	// covering both "secondary participants" (e.g. both spouses in a
	// married event) and "related_entity_ids" (the new child, the pet
	// adopted, the org joined) with a single JSON array rather than a
	// dedicated join table, since nothing needs to query from the
	// related-entity side yet. Reuses Contact.Circles' own serialization
	// style (models/contact.go).
	RelatedEntityIDs []string `gorm:"type:text;serializer:json" json:"related_entity_ids,omitempty"`

	// Remind, when true, opts this event into automatic yearly reminder
	// generation (T5b). Only meaningful when Date has month/day — year-only
	// events have nothing to anchor a yearly recurrence to.
	Remind bool `gorm:"default:false" json:"remind,omitempty"`

	// ETag is the CalDAV sync-conflict token for this LifeEvent (T12a),
	// derived from Revision (ADR 0006): e-{id}-{revision}. Explicit gorm
	// column tag is mandatory: without it GORM derives `e_tag` while
	// migration 000041 names the column `etag` — the exact silent mismatch
	// that shipped broken for ContactSyncLink.ETag and killed CardDAV
	// incremental sync.
	ETag string `gorm:"column:etag" json:"-"`

	// Revision is the monotonic per-row write counter (issue #591, CON-01a —
	// docs/adrs/0006-revision-token-schema.md): starts at 1 on create,
	// incremented on every persisted write, ETag derived from it. Exposed
	// read-only on the wire as `revision` (the model is this entity's
	// response DTO). Migration 000044 adds the column.
	Revision int64 `gorm:"column:revision;not null;default:1" json:"revision"`

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
// Hard deletes and bulk deletes are skipped via the DeletedAt guard. The PK
// is a UUID string, so no numeric conversion is needed.
func (l *LifeEvent) AfterDelete(tx *gorm.DB) error {
	if !l.DeletedAt.Valid {
		return nil
	}
	auditAfterDelete(tx, AuditEntityLifeEvent, l.ID, l.UserID, l)
	return tx.Model(&LifeEvent{}).Unscoped().Where("id = ?", l.ID).UpdateColumn("updated_at", time.Now()).Error
}

// BeforeCreate generates a UUID for new LifeEvents, mirroring Household's own
// BeforeCreate.
func (l *LifeEvent) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}

// AfterCreate assigns the initial ETag now that ID is persisted, mirroring
// Contact.AfterCreate (contact.go). LifeEvent's PK is a UUID string (not
// gorm.Model's uint), so the ETag is derived from that ID. Stamp revision 1
// and derive the ETag from it (ADR 0006). UpdateColumns bypasses GORM's
// update hooks, so this cannot recursively trigger AfterSave.
func (l *LifeEvent) AfterCreate(tx *gorm.DB) error {
	l.Revision = 1
	l.ETag = fmt.Sprintf("e-%s-%d", l.ID, l.Revision)
	l.revisionStampedOnCreate = true
	return tx.Model(l).Where("id = ?", l.ID).UpdateColumns(map[string]any{"revision": l.Revision, "etag": l.ETag}).Error
}

// AfterSave refreshes the ETag on every real write, exactly like
// Contact.AfterSave: bump the monotonic revision counter and re-derive the
// ETag from it (ADR 0006), so two writes inside the same second can no longer
// collide.
//
// Guard: a zero-value ID means this hook fired on a bulk
// Model(&LifeEvent{}).Where(...).Update/Updates call, not on a real row —
// e.g. contact merge's entity_id repoint (contact_merge_service.go). The
// receiver has no primary key, so UpdateColumns would widen to every row in
// the table. Never derive/write a revision in that case (the caller is
// responsible for load-then-save updates that should bump revisions).
func (l *LifeEvent) AfterSave(tx *gorm.DB) error {
	if l.ID == "" {
		return nil
	}
	// T18 audit fires first: the UpdateColumns below swaps in a fresh
	// statement, which would otherwise wipe the audit's instance state.
	auditAfterSave(tx, AuditEntityLifeEvent, l.ID, l.UserID)
	if l.revisionStampedOnCreate {
		l.revisionStampedOnCreate = false
		return nil
	}
	l.Revision++
	l.ETag = fmt.Sprintf("e-%s-%d", l.ID, l.Revision)
	return tx.Model(l).Where("id = ?", l.ID).UpdateColumns(map[string]any{"revision": l.Revision, "etag": l.ETag}).Error
}
