---
title: Upgrade Compatibility
nav_order: 12
---

# Upgrade compatibility

**This is the canonical supported-upgrade statement (issue #529).** The upgrade
test matrix (MIG-01 fixtures in `backend/internal/schemafixture`, DEPLOY-02's
automated harness) and the startup migration gate (`database.RunMigrations`) are
all defined against this document; a change to the policy below is a change to
those, and vice versa. This page publishes the decided policy; it does not
re-derive it.

## The supported range

**In-place upgrade is supported from `v0.6.0` and later.**

- `v0.6.0` is the floor. It is the last release before the hardening series
  began, and every release at or above it is covered by a committed schema
  fixture (see "How upgrades are tested" below).
- **Version-skipping within the range is supported.** `v0.6.0 → current`
  directly must work, not only `v0.6.0 → v0.6.1 → …`. Self-hosted operators
  skip versions routinely.
- **Post-`1.0`:** any `1.x` upgrade from any earlier `1.x`, and from the final
  `0.9.x`. The floor moves only at a major version — which is itself a
  breaking change under [MAINT-02](breaking-change-policy.md): raising a
  supported-version minimum requires the major version and process that page
  describes. The upgrade floor is a covered surface of that policy.
- **Downgrade is unsupported.** Rolling back means installing the previous
  version and restoring the pre-upgrade backup (see below).

### Upgrading

```sh
docker compose pull
docker compose up -d
```

Database migrations run automatically on startup. **Before applying any pending
migration the server takes a verified SQLite snapshot** into a `pre-migration/`
directory beside the database (`MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` moves it)
and **refuses to migrate if it cannot write one** (issue #530). That snapshot is
the rollback point — downgrade is unsupported, so getting back to the previous
version means installing it and restoring this snapshot. It covers the database
only; photos and attachments are unchanged by migrations, so a full rollback
still restores those from the routine three-piece backup
(`docs/deployment.md` → Backups).

Upgrading is unattended; that is exactly why the supported range is bounded and
why the refusal cases below fail loudly instead of best-effort.

### The client side of an upgrade

Upgrading the server does not, by itself, require anyone to update their
clients. Backward compatibility is the norm here: **a server upgrade never
breaks an existing client until the server explicitly declares a floor**, and
shipping a floor is a deliberate, documented, breaking event — not a side
effect of a routine upgrade. The full statement, including the supported
server×client combinations, is the [client/server compatibility
policy](client-compatibility-policy.md) (ANDROID-01, issue #478); this section
is the operator-facing summary:

- **Web client.** Nothing to do. Every user's next page load fetches the
  current build through the service worker; the app handles asset-skew and a
  stale-`/health`-contract window itself (prompting or force-reloading the tab
  as needed). See [Service-worker updates](service-worker-updates.md) for the
  mechanics and the `/_recovery.html` escape hatch.
- **Android app.** A newer server keeps working with older app releases until
  the server declares a floor. If a future server release does raise
  `MIN_CLIENT_VERSION` above the app someone is running, that app shows a
  blocking **"Update required"** screen at the next login and cannot proceed
  until updated — it does not fail mid-sync or corrupt anything. There is
  currently **no floor declared** (the supported combinations table in the
  policy has a single row: every released client works with every released
  server), so today an upgrade needs no app update.
- **What to do when the release notes say otherwise.** The only case an
  operator acts on is a server release whose notes declare a new client floor —
  a MAINT-02 breaking change that ships through the deprecation process, never
  silently. Then update the Android app before (or promptly after) the server;
  the app's own "Update required" screen is the signal that matters. A *newer*
  app on an *older* server is a separate, non-breaking direction: the app hides
  or disables features the older server does not have (see the policy's
  "newer client, older server" section).

### Known defect — upgrading into v0.6.1–v0.6.8 with existing audit history

**Affected:** a direct upgrade from **≤ v0.6.0** to any of **v0.6.1 – v0.6.8**
on an instance that has ever written an audit event with a `before_snapshot`
(every contact edit or delete writes one). **Fixed in v0.6.9** — upgrading
straight from `v0.6.0` to `v0.6.9` or later does not hit this.

**Symptom.** Migrations apply and commit, then the backend exits at startup and
crash-loops, so every request returns `502` (nginx is up, the backend is not):

```
atrest: backfill audit_events.before_snapshot: constraint failed: audit_events is append-only: UPDATE is not allowed (1811)
Failed to backfill at-rest encryption
```

**Cause.** The at-rest-encryption backfill (issue #380) encrypts pre-existing
plaintext rows with in-place `UPDATE`s. `audit_events` carries a `BEFORE UPDATE`
trigger that makes it append-only, and the backfill did not drop that trigger
around its own writes the way the other sanctioned audit-table writers
(migration `000034`, `RecomputeAuditChain`) do. `schema_migrations` is left at
the new version and not dirty — this is a startup-job failure, not a
migration-state refusal, so none of the four refusal states below describe it.

**Recovery.** [migration-recovery.md → At-rest backfill vs. the audit-events
trigger](operations/migration-recovery.md#at-rest-backfill-vs-the-audit-events-trigger):
roll back to the pre-migration snapshot and wait for v0.6.9, or — to stay on the
upgraded release — drop the `audit_events_no_update` trigger for one successful
boot and let startup recreate it.

## What happens below the floor

A database whose schema predates `v0.6.0` **refuses to migrate**. This is a
deliberate, loud refusal — never a partial migration, never a crash, never a
best-effort single hop:

- The server prints a message naming `v0.6.0` as the required intermediate and
  exits (the `Failed to initialize database` fatal log line).
- `cmd/migrate up` prints the same message and exits nonzero.
- Nothing is written to the database; the version is untouched and the database
  is left clean.

The exact message:

```
database schema version 30 predates the supported upgrade floor (v0.6.0, migration 31).
In-place upgrade is supported only from v0.6.0 and later; this version refuses to migrate a pre-floor database.
Upgrade this instance to v0.6.0 first, then run this version again — see docs/upgrade-compatibility.md.
```

### Two-step upgrade for a pre-floor instance

1. Back up the database and the file directories (`docs/deployment.md` →
   Backups — the three-piece backup: database, `PROFILE_PHOTO_DIR`,
   `ATTACHMENTS_DIR`).
2. Deploy the `v0.6.0` release (image `ghcr.io/<org>/mycorrhizal-crm:0.6.0` —
   the published tag drops the leading `v` — or equivalent). Its startup
   migrations move the database to the `v0.6.0` schema
   (`000031`).
3. Verify it boots and serves.
4. Deploy the current release. It now sees a database at or above the floor and
   continues normally.

This is a **documented two-step, not a supported single hop**: only `v0.6.0`
(and later) is guaranteed to read the pre-floor schema and preserve its data.

## The one-time `v0.2.0-alpha-candidate` bridge

There is exactly one known sub-floor installation: the maintainer's own,
deployed at `v0.2.0-alpha-candidate` (2026-08-04). It gets a **one-time,
documented bridge**, not a standing support promise:

1. Back up everything (database, `PROFILE_PHOTO_DIR`, `ATTACHMENTS_DIR`; see
   `docs/deployment.md`).
2. Copy the database and run the bridge against the **copy** first:
   `MYCORRHIZAL_ALLOW_SUB_FLOOR_MIGRATION=1 make migrate-up` (or set the same
   env var when booting the current server against the copy). This env var is
   the only code-level way a pre-floor database can be migrated in one binary;
   it is deliberate, logged, and only for this bridge. A second exception
   would be a policy change, not a config option.
3. Verify the copy (boot it, log in, search, export; check the audit trail).
4. If the copy is clean, run the same step against the real database.

The bridge exists because the two-step path above depends on producing the
`v0.6.0` binary's exact startup migration run against a real database; if that
cannot be reproduced, the env var is the fallback. It is exercised in CI by
`database.TestFullChainMigrationPreservesRealData` (which asserts the refusal,
then runs the full chain through the override and asserts the seed data
survived).

## Refusal states

Four startup states are enforced, all **fail-closed**: the server refuses to
start, logs at error level, and names the condition and its recovery. A message
that names the condition but not the remedy sends the operator to the source;
these all state both. In no case does any configuration setting turn a refusal
into a warning — the one exception is the documented one-time
`v0.2.0-alpha-candidate` bridge above, which is a policy exception for a single
known sub-floor deployment, not a bypass knob. The step-by-step recovery for
each state — including exactly which pre-upgrade backup file to restore and how
long it must be kept — is `docs/operations/migration-recovery.md`.

The first three are the migration-state gates (MIG-04, issue #439). The fourth
is the mandatory pre-migration backup (issue #530): a snapshot the server must
be able to write before it will migrate, because downgrade is unsupported and
that snapshot is the only rollback point.

| State | Behavior | Operator action |
|---|---|---|
| Sub-floor schema (below `000031`) | **Refuse**, print the two-step message above, exit | Two-step through `v0.6.0`, or the documented bridge — see the [below-the-floor section](operations/migration-recovery.md#below-the-floor) |
| Dirty schema | **Refuse** (`ErrDirtyMigration`): a migration started and did not finish, so the schema state is unknown | Restore the pre-migration backup and start again — see the [dirty-schema section](operations/migration-recovery.md#dirty-schema). Only after verifying the schema actually matches the named version, `make migrate-force` (prompted, operator-only) — never automatic |
| Schema ahead of the binary | **Refuse** (`ErrSchemaAheadOfBinary`): the database knows migrations this binary does not, meaning a rollback is in progress | Deploy a binary that knows the newer migration, or restore the backup taken before the newer release ran — see the [ahead-of-the-binary section](operations/migration-recovery.md#schema-ahead-of-the-binary) |
| Pre-migration backup target unwritable | **Refuse** (`ErrPreMigrationBackupFailed`): pending migrations exist but the mandatory snapshot could not be written; the database is untouched | Make the backup directory writable, or set `MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` to a writable path, then start again — see [The pre-migration backup](operations/migration-recovery.md#the-pre-migration-backup) |

### Dirty schema — interrupted migration

The `schema_migrations.dirty` flag is set when a migration **starts and does
not finish**: the process was killed, the container was OOM-killed, the host
lost power, or the SQL itself failed partway. The flag exists to say "the
schema is in an unknown, partially-applied state; a human must look at it."

Previously the server force-cleared the flag at every boot and migrated on top
of the torn schema — presenting a half-applied database as healthy (issue
#546). It does **not** do that anymore. On a dirty database the server refuses
to start with a message naming the dirty version, what it means, and the
recovery:

```
database is in a dirty migration state at version 43: a migration started and did not finish, so the schema may be only partially applied and does not match any known version. Refusing to start (fail-closed). Restore the pre-migration backup and start again — see docs/deployment.md (Backups → Restore). If you have verified the schema actually matches version 43, the operator-only escape hatch is `make migrate-force` (or `go run cmd/migrate force`), which prompts for explicit confirmation; it is never applied on the startup path.
```

**Primary recovery: restore the pre-migration backup.** The schema state is
unknown by definition; the backup is the only state you can trust. Restore
per `docs/deployment.md` → Restore, then start again — the restored snapshot
is clean and migrations run normally.

**Operator-only escape hatch: `make migrate-force`.** Each individual
migration runs inside one transaction (SQLite DDL is transactional), so an
interrupted migration's DDL rolls back cleanly rather than landing torn — the
schema is either fully the previous version or fully the interrupted version,
never somewhere between. The recovery is therefore: verify which of those two
the schema actually matches, then clear the dirty flag at that version and let
the pending migrations run. `migrate-force` does exactly this for the **current
dirty version**, and it **prompts** (`Type 'yes' to continue:`) before doing
anything — it cannot be triggered non-interactively by accident, and it is the
only path that can clear the flag. The startup path has no equivalent.

### Schema ahead of the binary — bad rollback in progress

A database whose applied version is *higher* than this binary's newest
migration means the database was migrated by a newer release and this binary
has been rolled back. Downgrade is unsupported (see "The supported range"), so
starting anyway would have the binary misread columns it does not know about —
the same silent-corruption class as the dirty-force bug above. The server
refuses to start with `ErrSchemaAheadOfBinary`, naming both versions and the
recovery:

```
database schema version 45 is ahead of this binary (latest known migration 44): the database was migrated by a newer release and this binary has been rolled back. Downgrade is unsupported, so refusing to start. Deploy a binary that knows migration 45 (or newer) and start again, or restore the backup taken before the newer release ran — see docs/deployment.md (Backups → Restore).
```

### How each refusal is surfaced

Each refusal is its own typed error (`ErrSubFloorMigration`,
`ErrDirtyMigration`, `ErrSchemaAheadOfBinary`, and `ErrPreMigrationBackupFailed`
for the pre-migration backup gate) so health/readiness and the diagnostics run
can report *which* state an install is in rather than a generic boot failure,
and so the migrate CLI prints the same message. The server start path logs it
via `logger.Fatal` (`Failed to initialize database`); nothing is written to the
database by any refusal.

## How upgrades are tested

- **Schema fixtures (MIG-01, issue #436):** one committed schema-only dump per
  release at or above the floor lives in
  `backend/database/testdata/schemas/` (see that directory's README). Each is
  generated from the embedded migration chain (frozen and append-only) and
  populated at test time from the canonical TEST-02 manifest.
- **Chain upgrades (MIG-02, issue #437):** `internal/schemafixture`'s upgrade
  tests run every adjacent hop (`v0.6.0 → v0.6.1 → … → current`) and the
  longest supported skip (`v0.6.0 → current`) against real migrated fixture
  databases, asserting row counts and search consistency survive. The
  migration-tests CI workflow matrixes one job per supported release
  (`v0.6.0 → current`, `v0.6.1 → current`, …) — the legs are derived from
  `schemafixture.SupportedReleases` (`cmd/releaselist`), not hand-listed, so a
  release added by `release.yml` is covered without a workflow edit. Each
  migrates its fixture through `database.InitDB` (the path the server boots
  through) and asserts the final version and row counts, plus a down-direction
  job that round-trips every migration up → down → up against a populated
  fixture and gates on every migration shipping its `.down.sql`. A new release
  without a fixture fails CI (the completeness test plus the docker-publish
  gate).
- **Full-stack upgrades (DEPLOY-02, issue #451):**
  `internal/schemafixture`'s `deploy02_test.go` upgrades a real three-piece
  install — the database file beside real `PROFILE_PHOTO_DIR` /
  `ATTACHMENTS_DIR` directories, the shape a Docker volume actually holds —
  IN PLACE through `database.InitDB` for every supported release (the v0.6.0
  case is the longest skip), and validates the whole install rather than the
  database alone: row counts and the MIG-03 semantic content survive, every
  live attachment/photo row still resolves to a real file after the in-place
  migrate, and the mandatory pre-migration backup (#530) is confirmed to have
  actually been written during the upgrade and to be a valid, restorable
  snapshot at the PRE-upgrade schema. The migrated instance is then driven
  through the real HTTP stack (`routes.RegisterRoutes`, exactly as the server
  wires it up): logging in with the PRE-EXISTING account (the bcrypt hash must
  still validate after the upgrade), an FTS search for a pre-upgrade contact,
  reading and editing a contact, and exporting. Sub-floor and dirty databases
  are refused on this same full-install path (issues #529/#439/#546).
- **Large datasets (issue #495):** the same chain-upgrade path is also tested
  against databases populated at 134x the canonical manifest (2,010 contacts,
  pathological records included) — every supported release migrates to the
  current schema with row counts and integrity intact, and the measured
  resource requirements (duration / peak memory / peak disk per path) are
  recorded in `docs/development/scale-testing.md`. Disk exhaustion during a
  large migration is asserted to fail closed in the chaos job.
- **Cross-version restore (BACKUP-01, issue #453):**
  `internal/schemafixture`'s `cross_version_restore_test.go` takes a real
  `VACUUM INTO` snapshot of each supported-release fixture and restores the
  three pieces (database + `PROFILE_PHOTO_DIR` + `ATTACHMENTS_DIR`) under the
  matrix of restoring-release outcomes: `M == N` serves the snapshot with no
  migration; `M > N` migrates it forward on startup and the restored data is
  compared semantically (MIG-03, issue #438), with every attachment/photo row
  asserted to resolve to a real file; `M < N` (a newer snapshot under an older
  binary) is refused with `ErrSchemaAheadOfBinary`, which names the recovery
  path. A companion test takes snapshots under concurrent write load and
  asserts each is a transactionally consistent cut (`integrity_check = ok`, no
  foreign-key violations, no torn writes). This is the automated backing for
  the roll-back-a-bad-release procedure in
  `docs/operations/migration-recovery.md`.

## Document consistency

The floor (`v0.6.0`, migration `000031`) is defined in
`backend/database/migrate.go` (`SupportedUpgradeFloorVersion` /
`SupportedUpgradeFloorTag`) and re-exported by `internal/schemafixture`.
`schemafixture.TestDocsStateTheFloor` asserts this document still names both,
so the published policy cannot drift from the code.
