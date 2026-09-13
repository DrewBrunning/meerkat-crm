package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Provenance values stored on RelationshipEdge.Source.
const (
	RelationshipSourceUserConfirmed     = "user-confirmed"
	RelationshipSourceHouseholdInferred = "household-inferred"
	// RelationshipSourceGraphInferred marks edges derived by composing other
	// confirmed edges (T104 — services.GenerateGraphSuggestions). Same
	// "inferred, not fact" semantics as the household-inferred source.
	RelationshipSourceGraphInferred = "graph-inferred"
	RelationshipSourceImported      = "imported"
	RelationshipSourceAISuggested   = "ai-suggested"
)

// Status values stored on RelationshipEdge.Status.
const (
	// RelationshipStatusConfirmed edges are authoritative: they are eligible
	// to project into Card.RelatedTo on export (models/contact_record.go).
	RelationshipStatusConfirmed = "confirmed"
	// RelationshipStatusSuggested edges await user approval (
	// household-inference mechanism is the first producer of these) and are
	// never treated as fact — never projected, never read as a hard edge by
	// anything outside a review surface.
	RelationshipStatusSuggested = "suggested"
)

// Sensitivity values stored on RelationshipEdge.Sensitivity.
const (
	RelationshipSensitivityNormal  = "normal"
	RelationshipSensitivityPrivate = "private"
	RelationshipSensitivitySecret  = "secret"
)

// RelationshipEdge is the relationship graph's edge entity (docs/adrs/0001-neutral-hub-and-spoke-contact-model.md) — the internal source of truth for
// relationships between two Contacts, per D3. Distinct from the legacy models.Relationship (a flat,
// free-text table this entity is not built on top of and does not yet
// replace — that cutover is, a separate later work package).
//
// SourceID/TargetID reference Contact.VCardUID (models/contact.go), which is
// already a UUID generated for every Contact unconditionally — no new column
// on Contact was needed for this. Only one direction of an edge is ever
// stored ("parent_of" from A to B, never also "child_of" from B to A); the
// reciprocal is always derived from the type registry (relationship_type_
// registry.go's InverseRelationType), never persisted, so there is nothing
// to keep in sync.
//
// This is the first UUID-string-primary-key entity in this codebase — every
// other model uses gorm.Model's uint autoincrement PK. ID is generated in
// BeforeCreate, mirroring Contact.VCardUID's generation (models/contact.go).
//
// No soft-delete: matches ContactSyncLink/CalendarEventLink's precedent for
// edge/join-shaped rows that don't need one.
type RelationshipEdge struct {
	ID        string    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// UserID/SourceID/TargetID/Type together form the edge's natural key:
	// migration 000055 (issue #928) puts a unique index on the tuple so a
	// double-clicked or replayed create cannot silently duplicate the fact.
	// The tags mirror that index for any AutoMigrate-based test; the app's
	// schema comes from the hand-written migration (CLAUDE.md backend trap 1).
	UserID uint `gorm:"not null;index;uniqueIndex:idx_relationship_edges_natural_key,priority:1" json:"-"`

	// SourceID/TargetID are Contact.VCardUID values, not Contact.ID — the
	// graph invariant that every relationship endpoint is an entity
	// referenced by its stable UUID, never a bare string.
	SourceID string `gorm:"not null;index;uniqueIndex:idx_relationship_edges_natural_key,priority:2" json:"source_id" validate:"required,uuid4"`
	TargetID string `gorm:"not null;index;uniqueIndex:idx_relationship_edges_natural_key,priority:3" json:"target_id" validate:"required,uuid4"`

	// Type is the social role (e.g. "parent_of", "spouse_of") — see
	// relationship_type_registry.go for the full registered set. The real-
	// world nuance (biological/adoptive/step, poly descriptors, custody...)
	// belongs in Metadata, never in a new Type token ("type = role,
	// metadata = nature" rule).
	Type string `gorm:"not null;index;uniqueIndex:idx_relationship_edges_natural_key,priority:4" json:"type" validate:"required,relation_type"`

	// Directional is per-edge, not derived from the registry: most relation
	// types have one conventional default (spouse_of is normally symmetric,
	// parent_of is normally directional), but affinity edges are the
	// explicit exception — conflicts_with defaults symmetric but may be
	// asserted directionally ("Marley is aggressive toward Gimley
	// specifically"). Callers set this at creation time.
	Directional bool `gorm:"not null" json:"directional"`

	// Metadata is free-form (since/until dates, {kind: biological|adoptive|
	// step|chosen}, poly descriptors, affinity strength/context, ...) — the
	// mechanism that lets a small, stable Type vocabulary express real-world
	// variety without a new token per nuance. First map[string]interface{}
	// JSON column in this codebase; uses the same gorm serializer mechanism
	// as FieldConstraints (models/field_definition.go).
	Metadata map[string]interface{} `gorm:"type:text;serializer:json" json:"metadata,omitempty"`

	// Source is provenance: how this edge came to exist. Confidence is 0-1;
	// user-confirmed edges are always 1.0, inferred/suggested ones lower.
	Source     string  `gorm:"not null" json:"source" validate:"required,oneof=user-confirmed household-inferred imported ai-suggested graph-inferred"`
	Confidence float64 `gorm:"not null;default:1" json:"confidence" validate:"gte=0,lte=1"`

	// Status gates authority: only "confirmed" edges are ever projected to
	// Card.RelatedTo or treated as fact by anything outside a suggestion-
	// review surface. See the RelationshipStatus* constants above.
	//
	// "Projected as fact" is the operative phrase: the flat CSV backup
	// (controllers.ExportData) does carry suggested rows, but writes Status as
	// its own column, so it transcribes the pending suggestion rather than
	// asserting it (issue #861).
	Status string `gorm:"not null;index" json:"status" validate:"required,oneof=confirmed suggested"`

	// Sensitivity is the cross-cutting marker. Anything above
	// "normal" is excluded by default from every copy that leaves this
	// instance or reaches another party: the neutral-Card exports (vCard 3/4,
	// JSContact), CardDAV/CalDAV sync, contact shares, and shared views —
	// enforced in models/contact_record.go's projection step, re-includable
	// only via the explicit include_sensitive opt-in.
	//
	// It is not an access-control tier against the owning user, so the flat
	// CSV export (controllers.ExportData) deliberately carries every
	// sensitivity, labelled by its own column: that file is the user's own
	// full backup, and the only full-fidelity export offered. See issue #861
	// and controllers/export_csv_full_fidelity_test.go, which pins both
	// halves of the asymmetry.
	//
	// Ships as a column now rather than being retrofitted later.
	Sensitivity string `gorm:"not null;default:normal;index" json:"sensitivity" validate:"required,oneof=normal private secret"`
}

// BeforeCreate generates a UUID for new edges, mirroring Contact.VCardUID's
// generation (models/contact.go's BeforeCreate).
func (r *RelationshipEdge) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}
