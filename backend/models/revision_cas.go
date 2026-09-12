package models

import (
	"fmt"

	"gorm.io/gorm"
)

// ErrRevisionConflict is returned by bumpRevisionCAS (and therefore surfaces
// as the error from the enclosing Save()/Updates() call) when the atomic
// compare-and-swap could not find a row that was both at the expected
// revision and not soft-deleted.
//
// CON-01 follow-up (issues #920, #924; ADR 0008 §"Consequences" named this
// exact residual and ADR 0018 closes it): before this type existed, every
// revision-bearing model's AfterSave hook bumped the counter as a pure
// read-modify-write -- `c.Revision++` on the in-memory struct, then an
// *unconditioned* `UPDATE ... WHERE id = ?` with no check that the row was
// still at the revision it was loaded at, and no check that it had not been
// concurrently soft-deleted. Two writers that both passed the pre-write
// checkIfMatch precondition (ADR 0008) because they both loaded the row
// before either committed could both reach that statement and both
// "succeed" -- the second silently clobbering the first's change while
// reusing the same next revision number (#924). And because GORM's own
// Save() falls back to an *unconditional upsert* whenever its primary
// UPDATE affects zero rows (gorm.io/gorm@v1.31.2 finisher_api.go, guarded
// only by `updateTx.Error == nil`), a concurrent soft-delete landing between
// a caller's load and its Save() could resurrect the row outright, writing
// the caller's stale field values over the tombstone (#920).
//
// Folding the bump into one atomic
//
//	UPDATE <table> SET revision = ?, etag = ?
//	WHERE id = ? AND revision = ? AND deleted_at IS NULL
//
// closes both: a lost race is now detectable (RowsAffected == 0), and
// returning a non-nil error from AfterSave makes GORM's own Error-gated
// upsert fallback unreachable -- there is no remaining path in this
// codebase where a plain Save()/Updates() on one of these models can write
// an undeleted state back over a row that was concurrently soft-deleted, or
// silently advance the revision counter without persisting the content that
// revision is supposed to represent.
type ErrRevisionConflict struct {
	// Entity is the model name, for logging/error messages ("Contact", …).
	Entity string
	// ID is the row's primary key: uint for the gorm.Model entities
	// (Contact, Note, Activity, Reminder), string for LifeEvent's UUID PK.
	ID any
	// ExpectedRevision is the revision the in-memory struct was loaded at --
	// for every AfterSave caller in this codebase that is the same value a
	// REST handler's checkIfMatch precondition (if any) was already checked
	// against, since nothing between load and save ever mutates .Revision.
	ExpectedRevision int64
	// Deleted is true when the row is gone or was concurrently soft-deleted
	// (the caller should treat this as 404); false when it is still live
	// but has moved to a different revision (412 -- a genuine lost-update
	// race). controllers/helpers.go's handleRevisionConflict makes this
	// split for every REST handler.
	Deleted bool
}

func (e *ErrRevisionConflict) Error() string {
	if e.Deleted {
		return fmt.Sprintf("%s id=%v: not found or soft-deleted (expected revision %d)", e.Entity, e.ID, e.ExpectedRevision)
	}
	return fmt.Sprintf("%s id=%v: revision moved past %d concurrently", e.Entity, e.ID, e.ExpectedRevision)
}

// bumpRevisionCAS is the shared body of every revision-bearing model's
// AfterSave hook (Contact, Note, Activity, LifeEvent, Reminder). tx is the
// hook's own transaction handle -- GORM wraps Save()/Updates()/Create() in
// an implicit transaction by default, so a non-nil error returned here rolls
// back the primary field write that triggered this hook along with it.
// table is the literal SQL table name (bypassing tx.Model(...) lets one
// helper serve every entity regardless of primary-key type). id is the
// row's primary key. expectedRevision is the revision the struct was loaded
// at. etagOf computes the entity's ETag string from a candidate new
// revision (the two ID-formatting shapes -- "%d" for the uint-PK entities,
// "%s" for LifeEvent's UUID PK -- live at each call site, not here).
//
// Returns the new revision on success. On CAS failure it runs one more read
// -- still inside tx, before the transaction this hook is part of rolls
// back -- to tell a concurrent soft-delete apart from a concurrent edit, and
// returns *ErrRevisionConflict with that classification.
func bumpRevisionCAS(tx *gorm.DB, entity, table string, id any, expectedRevision int64, etagOf func(newRevision int64) string) (int64, error) {
	newRevision := expectedRevision + 1
	result := tx.Table(table).
		Where("id = ? AND revision = ? AND deleted_at IS NULL", id, expectedRevision).
		UpdateColumns(map[string]any{"revision": newRevision, "etag": etagOf(newRevision)})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		return newRevision, nil
	}

	var live int64
	if err := tx.Table(table).Where("id = ? AND deleted_at IS NULL", id).Count(&live).Error; err != nil {
		return 0, err
	}
	return 0, &ErrRevisionConflict{
		Entity:           entity,
		ID:               id,
		ExpectedRevision: expectedRevision,
		Deleted:          live == 0,
	}
}
