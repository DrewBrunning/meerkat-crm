# ADR 0018: Atomic revision compare-and-swap for concurrent writes

- **Status:** accepted
- **Date:** 2026-09-11
- **Depends on:** ADR 0006 (monotonic per-row revision tokens), ADR 0008 (REST conditional-write
  enforcement / `If-Match`).
- **Implements:** issues #920 and #924 (CON-01 follow-up) — closes the exact residual ADR 0008's
  "Consequences" section named and left open: "two genuinely simultaneous last-writer-wins writers
  can still both read revision *N* and both stamp *N+1* ... Closing that window (atomic increment +
  `UPDATE ... WHERE revision = ?` compare-and-swap in one statement) is a follow-up if the race
  proves real in practice." Both issues are hand-verified proof that it is.

## Context

ADR 0008 gave every revision-bearing entity (`Contact`, `Note`, `Activity`, `LifeEvent`, `Reminder`)
an opt-in `If-Match` precondition, checked once, right after the row is loaded and before any
mutation. That check is necessary but not sufficient: it compares the client's claimed revision
against whatever the row's revision happened to be at load time, and nothing stops the row from
changing again between that check and the eventual write. The actual bump, in every one of these
models' `AfterSave` hooks, was a pure read-modify-write:

```go
c.Revision++
c.ETag = fmt.Sprintf("e-%d-%d", c.ID, c.Revision)
return tx.Model(c).Where("id = ?", c.ID).UpdateColumns(map[string]any{"revision": c.Revision, "etag": c.ETag}).Error
```

Two independently hand-verified findings followed directly from this:

- **#920 (resurrection):** GORM's own `Save()` silently falls back to an *unconditional upsert*
  (`Session{SkipHooks: true}.Clauses(clause.OnConflict{UpdateAll: true}).Create(value)`) whenever its
  primary `UPDATE` affects zero rows and the call's own `Error` is still `nil`
  (`gorm.io/gorm@v1.31.2` `finisher_api.go`). A contact soft-deleted between a caller's load and its
  `Save()` makes the primary `UPDATE ... WHERE id = ? AND deleted_at IS NULL` match zero rows; with no
  error raised anywhere in the chain, GORM's fallback then re-inserted the row with the caller's stale
  field values and `deleted_at = NULL` — the soft delete undone outright.
- **#924 (lost update):** two writers that both load revision *N* both pass the `If-Match` precondition
  (both correctly see the row at *N*), and both then reach the unconditioned revision bump. The second
  to commit overwrites the first's content while reusing the same next revision number — the very
  thing the token exists to make detectable, defeated by the increment being computed from an
  in-memory value nobody re-checked against the database.

## Decision

### 1. The revision bump becomes a single atomic statement, not a read-then-write

Every one of the five models' `AfterSave` hook now calls a shared helper,
`bumpRevisionCAS` (`backend/models/revision_cas.go`):

```sql
UPDATE <table> SET revision = ?, etag = ? WHERE id = ? AND revision = ? AND deleted_at IS NULL
```

using the revision the in-memory struct was loaded at (verified to never be mutated by any caller in
this codebase before `AfterSave` sees it) as both the `WHERE` predicate and the basis for the new
value. `RowsAffected == 0` is now a first-class, detectable outcome instead of silent success.

On a miss, one more read — still inside the same transaction, before it rolls back — tells a
concurrent soft-delete apart from a concurrent edit, and the hook returns a typed
`*models.ErrRevisionConflict{Deleted bool}` instead of `nil`.

### 2. `Revision` and `ETag` are tagged `<-:create` — writable only by `AfterCreate`/`AfterSave`

This is necessary, not cosmetic. `Save()`/`Updates()` on one of these models select `"*"` by
default, so the ordinary primary-content `UPDATE` — the one issued *before* `AfterSave` runs —
would otherwise blindly re-write `revision`/`etag` with the caller's stale in-memory values as
part of updating every other column. That silently resets the row's revision back to what the
caller loaded, which erases the very CAS condition `bumpRevisionCAS` is about to check moments
later in the same transaction. (This was caught empirically, not by inspection: an early version
of this fix without the tag passed the resurrection tests but failed every lost-update test — the
primary update was undoing the bump.)

`gorm:"<-:create"` (GORM's field-permission tag: creatable, not updatable) makes GORM's own
field-selection code exclude these two columns from any ordinary `Save()`/`Update()`/`Updates()`
SET clause, regardless of `"*"`. The only writers left are `AfterCreate`'s initial stamp and
`bumpRevisionCAS`, and both now go through `tx.Table(...)` rather than `tx.Model(...)` — a
`Model`-scoped statement would have the same field-permission check filter out its own write.

### 3. A conflict is 404 or 412, never a generic 500

`controllers/helpers.go`'s `handleRevisionConflict` classifies `*models.ErrRevisionConflict` for
every REST handler that saves one of these five entities:

- `Deleted: true` (the row is gone or was concurrently soft-deleted) → `404 Not Found`.
- `Deleted: false` (the row is live but moved past the expected revision) → `412 Precondition
  Failed`, the same status and error shape ADR 0008's `checkIfMatch` already uses, with
  `expected_revision` in the details.

This is deliberately not a new status code or a new contract shape: it is the *same* precondition
failure ADR 0008 defined, now also reachable from the write path itself instead of only the
pre-check.

### 4. Scope: all five revision-bearing entities, uniformly

ADR 0008 documented this residual for the whole entity family, not for `Contact` alone, and all
five share the identical `AfterSave` shape (`Contact`, `Activity`, `LifeEvent` in their own files;
`Note`, `Reminder` in `models/audit_hooks.go`). Fixing only the entity named in the filed issues
would leave four others with the same latent bug. The fix and its tests
(`models/revision_cas_test.go`, `controllers/conditional_write_race_test.go`) cover all five.

`RelationshipEdge`/`CircleMember`/`ContactTag`/`HouseholdMember`/`ContactSyncLink`/
`CalendarEventLink`/`FieldValue` are unaffected: ADR 0006 already excluded them (no `revision`
column), so there is nothing for `bumpRevisionCAS` to protect there.

### 5. `DELETE` is not changed by this ADR

Both filed issues are about the `Save()`/`Updates()` write path. A `DELETE` racing a concurrent
edit (the *other* direction — a stale `If-Match` on a delete request, checked before the row moved)
is a real, separate residual ADR 0008's own precondition does not fully close either, but it is not
the mechanism either #920 or #924 reports, and is left for a future ticket rather than folded in
here as an unrequested scope expansion.

## Consequences

- A `PUT`/`Updates()` on any of the five entities that races a concurrent write or delete of the
  same row now fails cleanly (404/412) instead of silently corrupting or resurrecting it. The
  common, non-racing case — the overwhelming majority of writes — is unaffected: one `UPDATE`
  statement either matches (it always does, absent a genuine race) or it doesn't.
- Every other `Save()`/`Updates()` call site on these five models in this codebase (imports, the
  contact-merge path, wedding-anniversary sync, CardDAV, admin operations — none of which set
  `.Revision`/`.ETag` themselves) is protected by the same mechanism automatically, since the fix
  lives in the shared model hook, not in any one controller.
- `bumpRevisionCAS` costs one extra `UPDATE` per successful save (replacing the old unconditioned
  one — not additive) and, only on an actual conflict, one extra `SELECT COUNT(*)` to classify it.
  No change to the non-conflict hot path's query count.
- The `<-:create` tag means no code anywhere may set `.Revision`/`.ETag` on one of these five
  structs and expect an ordinary `Save()`/`Updates()` call to persist it — only `AfterCreate` and
  `bumpRevisionCAS`'s own `tx.Table(...)` statements can. A test that needs to force a specific
  stored `etag`/`revision` value (for example, to defeat same-second ETag collision in a test) must
  do the same: `tx.Table(...)`, not `tx.Model(...)`.
