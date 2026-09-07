# Migration recovery runbook

This is the migration-specific chapter of the incident-response runbook
(`docs/security/incident-response.md`): what an operator does **after** the
server refuses to start because of the database's migration state. It is written
to be followed under stress, by one person, on their own instance — so it is
commands and decisions, not background reading. The milestone bar is that
recovery has been exercised by following the document, not from memory — the
procedure for that is [The drill](#the-drill).

Commands in this runbook assume a **checkout of this repo with a Go toolchain on
the host** (`make`/`go run` targets run from `backend/`; the shipped Docker image
has neither). Where a step has a toolchain-free equivalent (`sqlite3`), it is
given inline.

| | |
|---|---|
| **Scope** | The three fail-closed migration states (MIG-04, issue [#439](https://github.com/DrewBrunning/mycorrhizal-crm/issues/439)): dirty schema, schema ahead of the binary, and schema below the supported floor — plus the mandatory pre-migration backup, [rolling back a bad release](#rolling-back-a-bad-release-n1--n), and what `make migrate-down` is and is not for. |
| **Companion docs** | `docs/operations/disaster-recovery.md` (BACKUP-03, issue [#455](https://github.com/DrewBrunning/mycorrhizal-crm/issues/455) — the map: every recovery scenario, its RPO/RTO, and what is simply not recoverable; this runbook is the migration-specific route it points at), `docs/deployment.md` (backup/restore/upgrade *procedures* — the three-piece backup, and "Recovery objectives (RPO and RTO)" for how much a rollback loses and how long it takes), `docs/upgrade-compatibility.md` (the supported-upgrade range and the refusal states), `docs/security/incident-response.md` (containment/rotation for a security incident), `docs/security/data-retention-lifecycle.md` §10 (what a backup contains and how long backups survive). |
| **Policy anchor** | Downgrade is unsupported; rollback is **restore-the-pre-upgrade-backup** (issue [#530](https://github.com/DrewBrunning/mycorrhizal-crm/issues/530)). The server (and `make migrate-up`) takes the pre-migration backup **automatically and fail-closed** — it refuses to migrate if it cannot write one — see [The pre-migration backup](#the-pre-migration-backup). |

## Reading the state

The server refuses to start by exiting with a `Failed to initialize database`
fatal log line. The line **names the state and the remedy** — the three states
never share a message, so you do not have to read source. Confirm which of the
three you are in:

| Boot log says… | State | Section |
|---|---|---|
| `database schema version N predates the supported upgrade floor (v0.6.0, migration 31)` | Below the floor | [Below the floor](#below-the-floor) |
| `database is in a dirty migration state at version N: … Refusing to start (fail-closed). Restore the pre-migration backup and start again …` | Dirty schema | [Dirty schema](#dirty-schema) |
| `database schema version N is ahead of this binary (latest known migration M): … Deploy a binary that knows migration N (or newer) and start again, or restore the backup taken before the newer release ran …` | Schema ahead of the binary | [Schema ahead of the binary](#schema-ahead-of-the-binary) |

To read the version straight off disk (works even when the server will not
start):

```sh
# from a checkout of the repo with a Go toolchain, pointed at the live DB
make migrate-status                      # or: go run ./cmd/migrate/main.go version
# → Current migration status for <db>:
#   Current version on <db>: 45 (dirty: false)
```

If the state is not yet obvious, capture the evidence first (snapshot + logs)
before changing anything — the same "preserve evidence before you touch" rule as
`docs/security/incident-response.md`:

```sh
make backup                              # safe while the server is down or up
docker compose logs --no-color --timestamps mycorrhizal > evidence-migration-<timestamp>.txt
```

## The pre-migration backup

Every recovery in this document ends in "restore the pre-migration backup." A
runbook that says that without saying **which file** is not a runbook — so here
is exactly where it lives.

- **The server takes it for you, automatically.** Before applying any pending
  migration, `database.InitDB` (server startup) and `make migrate-up` write a
  verified `VACUUM INTO` snapshot of the database and **refuse to migrate if
  they cannot** (`ErrPreMigrationBackupFailed` — issue #530). It lands in a
  `pre-migration/` subdirectory beside `SQLITE_DB_PATH`, named
  `<db-stem>-pre-migration-<from-version>-to-<to-version>-<YYYYMMDD-HHMMSS>.db`
  (e.g. `mycorrhizal-pre-migration-31-to-44-20260901-120000.db`).
  `MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` moves the directory; nothing disables
  the backup. The snapshot is skipped only when there is nothing to protect: a
  fresh install (no schema yet) or a database already at the current schema.
- **It is the database only.** Migrations never touch `PROFILE_PHOTO_DIR` or
  `ATTACHMENTS_DIR`, so the automatic snapshot does not copy them. A *full*
  rollback still restores those two directories from the routine three-piece
  backup — see the next bullet and `docs/deployment.md` → Backups.
- **Where the file directories come from.** A complete backup is three pieces:
  the SQLite snapshot (above), the profile-photo directory (`PROFILE_PHOTO_DIR`),
  and the attachment directory (`ATTACHMENTS_DIR`). Restoring only the `.db`
  silently loses photos and attachments. See `docs/deployment.md` → Backups for
  the full inventory and the offline (server-stopped) alternative.
- **Taking one by hand** (a pre-floor two-step, or any time you want an
  explicit copy). `make backup` (from `backend/`, with the same environment the
  server uses) writes a timestamped snapshot **beside the live database**:
  `<db-stem>-<YYYYMMDD-HHMMSS>.db`. It refuses to overwrite an existing file,
  and verifies the result with `PRAGMA integrity_check` before reporting
  success. For an explicit, findable location:
  ```sh
  BACKUP_PATH=/backups/mycorrhizal-pre-v0.6.4-$(date +%F).db make backup
  ```
  No Makefile/Go? `sqlite3 <db> "PRAGMA wal_checkpoint(TRUNCATE); VACUUM INTO '/backups/mycorrhizal-<date>.db';"`
  is the equivalent statement.
- **How long it is kept.** Retention is entirely operator-owned — the app has
  no backup rotation or auto-deletion (deliberately; see
  `docs/deployment.md` → Backup confidentiality & retention). The **only**
  retention rule that is a policy, not a preference, comes from issue #530:
  **a scheduled purge must never delete the last rollback point.** The
  automatic snapshots land in their own `pre-migration/` directory precisely so
  a rotation cron pointed at the routine backup directory cannot reach them —
  keep that directory off any purge path, or prune it by *keeping the newest
  N*, never by age alone. A pre-migration filename (`…-pre-migration-<from>-to-<to>-….db`)
  still matches the routine glob `mycorrhizal-*.db`, so a **recursive**
  `find … -mtime +N -delete` rooted at a parent of `pre-migration/` would delete
  it once it aged. Keep the sweep non-recursive
  (`find /backups -maxdepth 1 -name 'mycorrhizal-*.db' -mtime +30 -delete`), or
  add `-not -path '*/pre-migration/*'`. This is pinned by
  `backend/database/backup_immutability_test.go`'s
  `TestBackupImmutability_PreMigrationRollbackPointSurvivesRoutineRotation`
  (issue #505 action 4). If you also take pre-upgrade snapshots by hand into
  the routine directory, name each with the version you are moving **from** and
  do not let such a purge remove the most recent one until the upgrade has been
  verified and the next pre-upgrade snapshot exists.
- **Encryption caveat.** A restore under a different at-rest master key
  (`DATA_ENCRYPTION_KEY`, or the `JWT_SECRET_KEY` fallback) fails closed at
  boot instead of serving garbage — a pre-upgrade snapshot must be restored
  with the same key the upgrade ran under. See `docs/deployment.md` →
  Restore.

## Rolling back a bad release (N+1 → N)

**The situation.** You deployed release N+1, it booted, migrations ran — and
now N+1 is misbehaving. You want to get back to N.

**Downgrade is not a thing.** There is no `.down.sql` sweep across releases and
no "start N against the N+1 database." A down migration that drops a column N+1
added would silently discard whatever was written into it while N+1 ran — data
loss wearing a safety-feature costume. The supported path is: **install N,
restore the snapshot taken before the N→N+1 upgrade.** You lose the data
written while N+1 was up — visibly, at a known-good point, by your choice — not
silently.

**Procedure.**

1. **Stop N+1.** `docker compose down` (or stop the service).
2. **Deploy the N binary/image.** The tag/image you were on before the upgrade.
   Do **not** skip this step (see the caveat below).
3. **Restore the database** from the `pre-migration/` snapshot written just
   before the N→N+1 upgrade — the one named `…-pre-migration-<N-version>-to-<N+1-version>-….db`
   (see [The pre-migration backup](#the-pre-migration-backup) for the location).
   Restore it exactly as `docs/deployment.md` → Restore describes: stop the
   server, replace the `.db` in place, clear stale `-wal`/`-shm` sidecars.
4. **Restore the file directories** (`PROFILE_PHOTO_DIR`, `ATTACHMENTS_DIR`)
   from the routine three-piece backup taken around the same time — the
   automatic snapshot is the database only.
5. **Start N** and confirm with [After recovery](#after-recovery): the applied
   migration version is N's, `integrity_check=ok`, `/health/ready` is `ready`.

**Caveat — restoring the snapshot alone is not a rollback.** If you restore the
N-era snapshot but leave the N+1 binary running, N+1 sees a database behind its
schema and **migrates it forward again** — that is the upgrade path repeating,
not a rollback. The binary cannot tell "the operator wants N" from "a normal
pending upgrade," so it does the safe, ordinary thing. You must deploy the N
binary (step 2) for the restore to mean rollback. The mirror case — an N+1
database opened by an N binary — *is* detected and refused
([Schema ahead of the binary](#schema-ahead-of-the-binary),
`ErrSchemaAheadOfBinary`).

## At-rest backfill vs. the audit-events trigger

**Applies to:** an instance upgraded from **≤ v0.6.0** into **v0.6.1 – v0.6.8**
that already had audit history. **Fixed in v0.6.9** — a `v0.6.0 → v0.6.9`
upgrade does not hit this. See `docs/upgrade-compatibility.md` → "Known defect".

**What it is.** Migrations apply and commit, then the backend exits at startup
with

```
atrest: backfill audit_events.before_snapshot: constraint failed: audit_events is append-only: UPDATE is not allowed (1811)
Failed to backfill at-rest encryption
```

and crash-loops (every request `502`s — nginx is up, the backend is not). The
at-rest-encryption backfill (issue #380) rewrites pre-existing plaintext rows in
place, and its `UPDATE` of `audit_events.before_snapshot` is rejected by the
append-only trigger it failed to drop around its own writes.
`schema_migrations` is at the new version and **not** dirty — this is a
startup-job failure, not one of the migration-state refusals above.

**Recovery — pick one.**

**A. Roll back to the pre-migration snapshot** (safest; you stay on `v0.6.0`
until `v0.6.9`). Follow [Rolling back a bad
release](#rolling-back-a-bad-release-n1--n): stop the backend, deploy the
`v0.6.0` image, restore the `…-pre-migration-31-to-<N>-….db` snapshot in place,
clear stale `-wal`/`-shm`, start. Verify with [After recovery](#after-recovery).

**B. Clear the trigger for one boot** (stay on the upgraded release). Once the
backfill completes, startup recreates the trigger itself (`atrest.Backfill`
finishes, then `RecomputeAuditChain` re-adds it).

```sh
# server stopped
sqlite3 "$SQLITE_DB_PATH" "DROP TRIGGER IF EXISTS audit_events_no_update;"
# start the server; watch it reach a clean boot with no 'Failed to backfill' line
sqlite3 "$SQLITE_DB_PATH" \
  "SELECT name FROM sqlite_master WHERE type='trigger' AND name='audit_events_no_update';"
# expect one row: audit_events_no_update
```

If that query comes back empty, recreate the trigger by hand:

```sh
sqlite3 "$SQLITE_DB_PATH" "CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT, 'audit_events is append-only: UPDATE is not allowed'); END;"
```

Then run [After recovery](#after-recovery). Both the at-rest backfill and the
hash-chain backfill wrote to `audit_events` on that boot, so also confirm
`/health` reports the expected migration version and the audit trail reads back.

## Dirty schema

**What it is.** A migration started and did not finish, leaving the migration
version marked dirty — the schema may be the failed migration's, partially
applied. The server refuses to start rather than migrating on top of it
(MIG-04, issue #439; the old force-and-continue behavior was the bug in issue
[#546](https://github.com/DrewBrunning/mycorrhizal-crm/issues/546)).

**Primary recovery — restore the pre-migration backup.** The schema state is
unknown by definition; the backup is the only state you can trust.

1. **Do not retry in a loop.** Each retry of a migration that is going to fail
   again costs another restore. If a migration genuinely failed (not just was
   interrupted), a second attempt is likely to fail the same way.
2. **Restore the pre-upgrade backup** — the snapshot taken before *this*
   upgrade began (see [The pre-migration backup](#the-pre-migration-backup) for
   which file, and `docs/deployment.md` → Restore for the exact commands:
   stop the server, replace the three pieces, start it again).
3. **Retry the upgrade** — the server runs pending migrations on startup, so
   starting it *is* the retry.
4. **If it fails again at the same migration: capture the migration logs and
   stop.** Save the evidence (`docker compose logs --no-color --timestamps
   mycorrhizal > migration-failure-<timestamp>.txt`) and file a bug with the
   failing migration name and those logs.

**Operator-only escape hatch: `make migrate-force`.** Each migration runs inside
one transaction (SQLite DDL is transactional), so an interrupted migration's DDL
rolls back cleanly rather than landing torn — the schema is either fully the
previous version or fully the interrupted version, never somewhere between.
That makes the dirty state recoverable in place *if* you verify which of those
two the schema actually matches: `make migrate-force` (or
`go run ./cmd/migrate/main.go force`) marks the **current dirty version**
complete and re-runs pending migrations from there. It **prompts**
(`Type 'yes' to continue:`) and refuses up front when the database is not dirty,
so it cannot be triggered non-interactively by accident — and it is the **only**
path that can clear the flag. The startup path has no equivalent: nothing the
server does on boot ever force-clears a dirty database.

## Schema ahead of the binary

**What it is.** The database's applied migration version is higher than this
binary's latest known migration — the database was migrated by a newer version
and this (older) binary cannot read columns it does not know about. It usually
means a binary was rolled back after an upgrade. Starting anyway is the same
silent-corruption class as the dirty-force bug, so the server refuses (MIG-04).

**Recovery** — two options; do the first if you have the newer version:

1. **Reinstall the newer binary.** This is the trivial fix: the database is
   fine, it is simply ahead of the binary you deployed. Reinstall the image/tag
   that produced this schema (the one you upgraded to before the rollback) and
   start. Verify with [After recovery](#after-recovery).
2. **Restore the backup taken before the upgrade that produced this schema.**
   Use this when you cannot or will not go back forward: restore the
   pre-upgrade snapshot (see [The pre-migration backup](#the-pre-migration-backup)),
   which puts the database back to a version this binary understands.

## Below the floor

**What it is.** The database's schema predates the supported upgrade floor
(`v0.6.0`, migration `000031`). The server refuses to migrate it best-effort;
the message names `v0.6.0` as the required intermediate.

**Recovery** — the documented two-step (from `docs/upgrade-compatibility.md`):

1. Back up the database and the two file directories.
2. Deploy the `v0.6.0` release. Its startup migrations move the database to the
   `v0.6.0` schema (`000031`).
3. Verify it boots and serves.
4. Deploy the current release. It now sees a database at or above the floor and
   continues normally.

There is exactly one known sub-floor instance (the maintainer's
`v0.2.0-alpha-candidate`), which has a **one-time, documented bridge** — see
`docs/upgrade-compatibility.md` → "The one-time `v0.2.0-alpha-candidate`
bridge". It is not a standing support path; run it against a **copy** first.

## After recovery

Confirm the restored database is the state the app expects, before trusting it:

```sh
# From a checkout with the live DB path (works while the server is stopped):
go run ./cmd/dbinspect/main.go "$SQLITE_DB_PATH"
# → integrity_check=ok version=45 dirty=false
# (any other line is a failure — a dirty flag, a version mismatch, or a
# non-ok integrity check)
```

No Go toolchain? The underlying check is a plain SQLite statement:

```sh
sqlite3 "$SQLITE_DB_PATH" "PRAGMA integrity_check;"
# → ok
```

Then start the server and check the two health endpoints that report migration
state (`docs/deployment.md` → Health, liveness & readiness):

- `GET /health/ready` → `200 {"status":"ready", …}` (it is `not_ready` while
  migrations are pending or dirty).
- `GET /health` → `healthy` with the applied migration version in the payload.

Finally, let the automated restore drill (`DB_RESTORE_DRILL_ENABLED`, default
on, weekly — issue #275) run once against the recovered instance, or trigger a
manual round-trip on a throwaway copy: back up, restore, and compare row counts
(`frontend/e2e/backupRestore.spec.ts` automates exactly that). A recovery you
have not verified this way is a hypothesis.

## Interrupted startup

Server startup runs every pending migration before it binds its HTTP listener,
so an interruption during startup is an interruption of a schema change. DEPLOY-03
(issue [#452](https://github.com/DrewBrunning/mycorrhizal-crm/issues/452))
tested a real `SIGKILL` at each point that matters; the outcome is always one of
the states above, never something in between. What to expect, by *when* the
process died:

| Killed… | State on restart | What to do |
|---|---|---|
| **Before any migration ran** (after the pre-migration backup, before the first statement) | Schema untouched — same version, `dirty=false` | Nothing. Just start again; it migrates normally. |
| **During a migration** (mid-statement) | [Dirty schema](#dirty-schema) at that version | Follow [Dirty schema](#dirty-schema): restore the pre-migration backup, or `make migrate-force` after verifying. |
| **Between two migrations** (one finished, the next not started) | Clean at an intermediate version, `dirty=false` | Nothing. Start again; it resumes from the next migration. No force needed. |
| **After all migrations, before the server was ready** | Clean at the latest version | Nothing. Start again; migrations are a no-op and it comes up. |

An orchestrator that keeps restarting a crashed container **cannot make this
worse**: every restart of a dirty database refuses identically (the refusal is
not a write), and every restart of a clean-but-incomplete database resumes — N
restarts converge on the same state as one. The pre-migration backup is taken
once and **reused**, not rewritten, on each restart, so a crash loop does not
churn it.

Confirm the outcome with [After recovery](#after-recovery) (`dbinspect` →
`integrity_check=ok`, the expected version, `dirty=false`), then the health
endpoints.

## What `make migrate-down` is (and is not) for

`make migrate-down` runs **exactly one** migration's `.down.sql`, on the
database at `SQLITE_DB_PATH`, after printing a destructive-change warning and
requiring you to type `yes`. It is the same single-step primitive the
up/down/up round-trip tests exercise.

- **What it destroys.** Whatever that migration's `.up.sql` created: the
  objects it added (tables, columns, indexes, triggers) and the data in them.
  There is no undo, and `MigrateDown` deliberately rolls back one step — never
  several, never "all the way down" (the CLI used to do that and it dropped the
  whole schema; see the note in `database/migrate.go`). A few migrations are
  deliberate no-ops on the way down (e.g. `000022` is a one-way data recovery;
  its down file states why). The exact destruction is specified by the
  migration's `.down.sql` and pinned in CI: every migration round-trips
  up → down → up on a **populated** fixture and must restore the pre-up schema
  exactly, with pre-up rows intact (`TestEveryMigrationRoundTripsUpDownUp` in
  `backend/internal/schemafixture/`; a migration without a `.down.sql` fails CI
  outright). Before you run it on a real database, read that migration's
  `.down.sql` — a directory listing is not a specification.
- **When it is the right tool.** Development (rebuild a schema leg) and the
  operator-initiated reversal of a single **already-applied** migration — e.g.
  you ran `make migrate-up` outside the server and need to back that one step
  out. Note that it is **not** usable on a dirty database (golang-migrate
  refuses to operate on one), so it is not the mid-upgrade failure tool — for
  a failed/interrupted upgrade that is `make migrate-force` after verification,
  or restore-from-backup (see [Dirty schema](#dirty-schema)).
- **When it is the wrong tool.** As a **version rollback**. Downgrade is
  unsupported; rolling back a deployed instance means restoring the
  pre-upgrade backup, not stepping `.down.sql` across releases — a running
  instance has new data a down migration will silently drop. Reach for
  `make migrate-down` during an incident only when a recovery procedure in this
  document says to. And never on a dirty database as a shortcut past the
  failure — that is the dirty-state recovery, above.

## The drill

The milestone bar is "recovery procedures are documented and **have been
exercised by following the documentation**." To re-verify this runbook, induce
each state on a throwaway database and recover, following only the sections
above — not from memory:

| State | Induce it by… | Recover by… | Passes when… |
|---|---|---|---|
| Dirty schema | Start a migration and kill the process mid-run (SIGKILL, or the TEST-06 fault-injection harness's `migration-kill` scenario), or set `UPDATE schema_migrations SET dirty = 1` | The [Dirty schema](#dirty-schema) restore | The restored database reports `integrity_check=ok`, the expected version, `dirty=false`, and `/health/ready` is `ready` |
| Ahead of the binary | Point an older binary at a database migrated by a newer one (e.g. a schema dump from `backend/database/testdata/schemas/` at a higher version) | The [Schema ahead of the binary](#schema-ahead-of-the-binary) reinstall or restore | Same as above |
| Below the floor | Point the current binary at a `v0.5.x`-schema database | The [Below the floor](#below-the-floor) two-step | The refusal message names `v0.6.0`, and the two-step lands at the current schema |
| Interrupted startup | Park `make migrate-up` at a fault seam and SIGKILL it (`MYCORRHIZAL_FAULTS=database.migration.before_batch:pause:120s` for "before any migration"; `database.migration.statement:pause:120s` for "during"), or stop the container mid-`docker compose up` | The matching row in [Interrupted startup](#interrupted-startup) | `dbinspect` shows the state that section predicts for the kill point, and a plain restart (or the named recovery for the dirty case) lands clean at the latest version with `/health/ready` `ready` |

A step that is missing or cannot be followed from this document is a bug in
this runbook — fix it here. The round-trip leg of the drill (every migration
up → down → up on populated data) runs in CI on every PR; it needs no manual
re-run.
