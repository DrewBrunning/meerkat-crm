package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Reminder struct {
	gorm.Model
	UserID                uint       `gorm:"not null;index" json:"-"`
	Message               string     `gorm:"not null type:text;serializer:encrypted" json:"message" validate:"required,min=1,max=500"`
	ByMail                *bool      `gorm:"default:false" json:"by_mail"`
	RemindAt              time.Time  `gorm:"not null" json:"remind_at" validate:"required"`
	Recurrence            string     `gorm:"not null" json:"recurrence" validate:"required,oneof=once weekly monthly quarterly six-months yearly"`
	ReoccurFromCompletion *bool      `gorm:"default:true" json:"reoccur_from_completion"`
	Completed             bool       `gorm:"default:false" json:"completed"`
	EmailSent             bool       `gorm:"default:false" json:"email_sent"`
	LastSent              *time.Time `gorm:"default:null" json:"last_sent"`
	ContactID             *uint      `gorm:"not null" json:"contact_id" validate:"required"`
	LifeEventID           *string    `gorm:"index" json:"life_event_id,omitempty"`
	Contact               Contact    `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"contact,omitempty" validate:"-"`

	// Revision is the monotonic per-row write counter (issue #591, CON-01a —
	// docs/adrs/0006-revision-token-schema.md): starts at 1 on create,
	// incremented on every persisted write. Migration 000044 adds the column.
	// Exposed read-only on the wire as `revision`.
	Revision int64 `gorm:"column:revision;<-:create;not null;default:1" json:"revision"`

	// ETag is the CardDAV/CalDAV-style sync-conflict token derived from
	// Revision (ADR 0006): e-{id}-{revision}. Reminders have no DAV surface
	// yet, but the token exists for the sync/merge surface that will need
	// it. Explicit column tag guards the `etag` vs GORM-derived `e_tag`
	// mismatch (CLAUDE.md backend trap 1). Migration 000044 adds the column.
	// `<-:create` (CON-01, ADR 0018): see Contact.ETag's comment
	// (models/contact.go) for why an ordinary Save()/Updates() must never be
	// able to write this column.
	ETag string `gorm:"column:etag;<-:create" json:"-"`

	// revisionStampedOnCreate: transient marker set by AfterCreate, consumed
	// by the AfterSave GORM fires right after on a Create (see
	// Contact.revisionStampedOnCreate). Keeps a create at revision 1.
	revisionStampedOnCreate bool
}

// AfterCreate stamps the initial revision and derives the ETag from it (ADR
// 0006), mirroring Contact.AfterCreate. UpdateColumns bypasses GORM's update
// hooks, so this cannot recursively trigger AfterSave. The marker tells the
// AfterSave that follows on a Create not to bump (a create is revision 1).
func (r *Reminder) AfterCreate(tx *gorm.DB) error {
	r.Revision = 1
	r.ETag = fmt.Sprintf("e-%d-%d", r.ID, r.Revision)
	r.revisionStampedOnCreate = true
	// tx.Table(...), not tx.Model(r): revision/etag are `<-:create` (CON-01,
	// ADR 0018), so a Model-scoped statement would have this write filtered
	// out by GORM's own field-permission check.
	return tx.Table("reminders").Where("id = ?", r.ID).UpdateColumns(map[string]any{"revision": r.Revision, "etag": r.ETag}).Error
}

type ReminderCompletion struct {
	gorm.Model
	UserID      uint      `gorm:"not null;index" json:"-"`
	ReminderID  *uint     `gorm:"index" json:"reminder_id,omitempty"`
	ContactID   uint      `gorm:"not null;index" json:"contact_id"`
	Message     string    `gorm:"not null;type:text;serializer:encrypted" json:"message"`
	CompletedAt time.Time `gorm:"not null" json:"completed_at"`
}
