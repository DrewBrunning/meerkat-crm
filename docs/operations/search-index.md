# Rebuilding the full-text search index

Full-text search (`GET /api/v1/search`, and `GET /contacts?search=`) is served
by three SQLite FTS5 virtual tables — `contacts_fts`, `notes_fts`,
`activities_fts` — kept in sync with the base tables by SQL triggers
(migrations `000007`, `000010`, `000020`). The index is **derived data**: it
holds nothing that is not reconstructable from `contacts`, `notes`, and
`activities`, so it can be rebuilt at any time and a rebuild always converges
on exactly what the triggers would have produced.

This document is the operator procedure for that rebuild (SEARCH-01, issue
[#461](https://github.com/DrewBrunning/mycorrhizal-crm/issues/461)).

## When to rebuild

The triggers keep the index correct for every write that goes through the
application. The index only drifts when a write **bypasses** the triggers:

- **A bulk import** that inserts rows with raw SQL.
- **A hand-written migration that updates a base table directly.** This
  project writes migrations by hand, so this is the standing risk, not a
  hypothetical — the reason `cmd/backfill-search-index` exists at all. A
  migration that touches `contacts`, `notes`, or `activities` content columns
  should be followed by a rebuild.
- **A restore from backup**, as a precaution — see below.
- **A change to how content is indexed** (e.g. a tokenizer or normalization
  change): the stored index is now in the old form and every existing row
  needs re-indexing. The Unicode NFC normalization change (I18N-02, issue
  #485) does **not** require one: its startup backfill
  (`services.NormalizeContactRecordsToNFC`, `cmd/backfill-unicode-nfc`)
  rewrites rows *through the Contact model*, so the `contacts_fts` triggers
  fire on every rewrite and the index stays in lockstep (asserted by
  `CheckSearchIndexConsistency` in `services/unicode_nfc_backfill_test.go`).
  Only a raw-SQL rewrite of base content columns — like a hand-edited
  migration — needs the manual rebuild below.

You do **not** need a rebuild after ordinary create/update/delete/soft-delete
traffic, or after an archive/unarchive — those go through the triggers.

Detection of drift (a routine check that says whether the index currently
matches canonical data) is a separate capability — SEARCH-02, issue
[#462](https://github.com/DrewBrunning/mycorrhizal-crm/issues/462). This
document is the repair.

## How to rebuild

### Stock deployment — the admin endpoint

A Docker deployment has no Go toolchain and no direct shell access to the
database. Use the admin-gated endpoint (session cookie of an admin user, or an
admin bearer token — API tokens are rejected on `/admin`):

```sh
curl -fsS -X POST https://<host>/api/v1/admin/search/rebuild \
  -H "Authorization: Bearer <admin token>"
```

```json
{
  "message": "Search index rebuilt",
  "indexed": { "contacts": 1284, "notes": 4021, "activities": 903 }
}
```

`indexed` is the number of live rows written to each index. The call is
synchronous: it returns when the rebuild has committed. A rebuild already in
progress returns **409**; a failure returns **500** and leaves the previous
index untouched.

The run is also recorded as a `search_index_rebuild` job run — `GET
/api/v1/admin/job-runs?job_name=search_index_rebuild` shows its duration and
outcome on the same timeline as the scheduled jobs (issue
[#391](https://github.com/DrewBrunning/mycorrhizal-crm/issues/391)), so a
rebuild that ran three weeks ago is still visible without anyone having kept a
terminal open.

### With a Go toolchain and database access

```sh
cd backend && go run ./cmd/backfill-search-index -db "$SQLITE_DB_PATH"
# → Search index rebuilt successfully: contacts=1284 notes=4021 activities=903
```

Same rebuild, same guarantees. This path is for a developer or an operator who
runs the binary directly rather than in the shipped container.

## Guarantees

- **All three indexes, every time.** `contacts_fts`, `notes_fts`, and
  `activities_fts` are rebuilt together. A zero count means "no live rows of
  that kind", never "that index was skipped".

- **Atomic — never a partial index.** The whole rebuild (clear + repopulate
  all three tables) runs in one transaction. A crash, a cancelled request, or
  a SQL error rolls it back, leaving the previously-good index exactly as it
  was. On commit, WAL switches every reader to the new index at once. There is
  no moment where search sees a half-built index, and an interrupted rebuild
  never reports success.

- **Search stays up.** Readers keep using the pre-rebuild snapshot until the
  rebuild commits, so search returns results (briefly stale ones) throughout.
  What *does* wait is other **writes**: the rebuild holds SQLite's single
  write lock for its whole duration, so create/update/delete requests queue
  behind it. At the scale this project targets this is sub-second; see
  `docs/development/scale-testing.md` for measured figures.

- **Safe to run twice.** Idempotent. Running it when the index is already
  correct is wasted work, not a hazard.

- **Concurrency.** A second rebuild started while one is running in the same
  process is refused immediately (`ErrJobSkipped` → HTTP 409), not queued.
  Across processes (a multi-instance deployment), SQLite serialises the
  rebuild transactions: they cannot corrupt the index, though a second one
  racing a long first may fail with a lock timeout rather than waiting. Do not
  script concurrent rebuilds.

## Verifying a rebuild

- The endpoint / CLI reports per-index row counts; compare them against
  `SELECT count(*) FROM contacts WHERE deleted_at IS NULL` (and the same for
  `notes`, `activities`).
- Run a search for a record you know exists.
- The regression oracle is `backend/services/search_rebuild_test.go`
  (`ftsDivergence`): it rebuilds and then compares every FTS row against a
  direct base-table scan — missing rows, orphan rows, stale content, and
  `user_id` mismatches all fail it. `TestFtsDivergence_DetectsEachClass` is
  its negative control.
