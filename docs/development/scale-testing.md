# Scale testing: the large-dataset migration test

This is the written-down definition of **"large"** and the recorded resource
requirements the v0.6.4 milestone's "large datasets have been tested" and "no
tested resource exhaustion scenario causes database corruption" criteria hang
on (issue #495). Migration is the riskiest place for those claims to be false:
it is the one operation that touches every row, it runs at startup before the
app is available, and on SQLite it competes with itself for a single write
lock and for disk.

The same generator serves PERF-01 (issue #468, benchmark fixtures) and issue
#498 (capacity testing under constrained resources) — one dataset generator,
not three. The full profile catalogue (`smoke`/`typical`/`large`/`stress`,
their user counts, graph shape and resource requirements) lives in
[scale-profiles.md](scale-profiles.md). The migration test measures over the
`--contacts N` slice of that generator: **one user, no hub/chain graph edges**,
so the row counts below stay exactly predictable for the integrity assertion.
The `large` PERF-01 profile is the same block-replicated data with a user count
and the traversal graph added on top.

## What "large" means (written number, not an adjective)

The large migration profile is derived from the intended MVP scale and stated
as a shape, not a size. It is **~100,000 contacts**, generated as 3,704 replicas
of the canonical TEST-02 pathological manifest (issue #430; the manifest carries
27 records since the I18N-01 international records landed, issue #484), so
every data shape — including the pathological ones — exists at scale. The exact
row counts are the canonical manifest's own ratios times 3,704 blocks
(3,704 × 27 = 100,008 contacts):

| Entity | Per block | At ~100,000 contacts (3,704 blocks) |
|---|---|---|
| contacts | 27 | **100,008** |
| relationship edges | 10 → 8 | 29,632 (10 per block minus the 2 per block hard-deleted with each tombstoned contact's cascade) |
| notes | 6 | 22,224 |
| life events | 9 | 33,336 |
| gifts | 5 | 18,520 |
| activities | 4 | 14,816 |
| preferences | 5 | 18,520 |
| external identities | 3 | 11,112 |
| attachments (metadata) | 4 | 14,816 |
| households / circles / tags | 2 / 2 / 2 | 7,408 each |
| custom-field values | 7 | 25,928 |
| **soft-deleted contacts** | 1 | **3,704** |
| **vcard-uid-recreating contacts** | 1 | **3,704** |
| **very-long (~1700-char) notes** | 1 | **3,704** |

The pathological records are present at scale, not just plain ones: each block
carries the soft-deleted `gina` + `julie`-recreates-her-uid pair (the partial
unique index `idx_contacts_vcard_uid_user`), the very-long note, the Unicode
data, the duplicate-detection pair, and the sensitive records. A migration or
exporter that mishandles any trap at canonical size does the same at 100k.

The scaling unit is the **manifest block** (27 contacts + their derived
content). `internal/largedata.Scale(manifest, N)` rounds `N` up to a whole
number of blocks, so the scaled dataset is always an exact integer multiple of
the pathological manifest — never a trimmed copy that accidentally drops the
trap rows.

The generator **is** the TEST-02 manifest scaled, not a separate fixture: the
scaled manifest passes the same cross-reference validation and populates
through the same code paths (`canonicalfixture.Populate` →
`models.ApplyRecordToContact`), so anything the canonical dataset exercises,
the large dataset exercises with more rows.

## The measurement harness

`cmd/migratebench` builds and measures large databases. Each subcommand is its
own process, so the migration measurement (`measure`) never shares a process —
or a memory footprint — with the seed:

```bash
cd backend

# 1. seed: build a large CURRENT-schema database (the scaled manifest through
#    the real REST code paths).
go run ./cmd/migratebench seed --contacts 100000 --db /tmp/current.db

# 2. checkpoint: copy its rows into a database whose schema is exactly a
#    historical migration version (v0.6.0 = 31, the longest supported skip).
go run ./cmd/migratebench checkpoint --db /tmp/current.db --version 31 --out /tmp/floor.db

# 3. measure: migrate that database to the current schema, sampling peak RSS
#    and peak disk continuously, and print the report line. Because this is a
#    fresh process, its peak RSS is the migration's alone.
go run ./cmd/migratebench measure --db /tmp/floor.db
```

There is deliberately no `run` convenience subcommand that execs `measure`:
the security posture bans `os/exec` in non-test packages (ASVS 12.3.6), and a
same-process measure would report the seed's in-memory manifest as the
migration's peak RSS. The three commands above are the workflow.

- `seed` builds a large **current-schema** database (the scaled manifest
  through the real REST code paths).
- `checkpoint` copies its rows into a fresh database whose schema is exactly a
  historical migration version (the same version-appropriate column
  intersection the upgrade fixtures use) — the "large database at release X"
  artifact a migration path is measured over.
- `measure` migrates a database to the current schema while sampling the
  process's resident memory (`/proc/self/status` VmRSS) and the on-disk size of
  the database plus its `-wal`/`-shm` sidecars (immediately, every 20 ms, and
  once more at the end). It prints one machine-readable line:

  ```
  migratebench_report from_version=31 to_version=45 duration_ms=... peak_rss_kb=... peak_db_bytes=... initial_db_bytes=... peak_extra_db_bytes=... final_db_bytes=... integrity=ok dirty=false
  ```

  The **peak additional disk** figure (`peak_db_bytes - initial_db_bytes`) is
  what matters for SQLite's table-rebuild hazard: a migration can grow the
  database halfway through a rebuild before collapsing it back, and the final
  size hides that transient need.

`measure` runs `PRAGMA integrity_check` at the end and the per-migration
progress logs described below are visible while it runs.

## Recorded resource requirements

Measured on a 16-core / 30 GB Linux x86_64 dev machine, Go 1.26, SQLite via
the glebarez/modernc pure-Go driver. Numbers are the `migratebench_report`
output (the `measure` phase only — seed/checkpoint cost is separate and also
reported). Re-measure before publishing the number in a release note; the
methodology above is the reproducibility contract.

| Path | Contacts | Migration duration | Peak RSS | Peak additional disk | Final DB size |
|---|---|---|---|---|---|
| v0.6.0 → current (longest skip) | 2,010 | ~0.15 s | ~30 MB | ~0.6 MB | ~10 MB |
| v0.6.0 → current | 10,005 | ~0.6 s | ~32 MB | ~16 MB | ~46 MB |
| v0.6.0 → current | 20,010 | ~1.2 s | ~32 MB | ~33 MB | ~91 MB |
| v0.6.0 → current | 100,005 | ~6.5 s | ~35 MB | **~184 MB** | ~450 MB |

> These figures were recorded against the pre-I18N-01 manifest (15-record
> blocks, issue #484). The canonical manifest now carries 27 records, so the
> `--contacts` targets round to different actual row counts (2,000 → 2,025,
> 100,000 → 100,008) and every row-touching migration touches a different row
> distribution. Re-run `migratebench measure` below and update the table before
> quoting any number from it.

Seed and checkpoint cost at the same sizes (build-time, one-off): 2,010
contacts ≈ 2.4 s seed / 0.25 s checkpoint; 10,005 ≈ 18 s / 1.1 s; 20,010 ≈
55 s / 2.2 s; 100,005 ≈ **17.6 min seed** / 12 s checkpoint. The seed at full
scale is the slow part (one transaction, ~700k rows through the FTS-trigger
writes); it is a build step, never a runtime path.

Notes:

- Peak memory is dominated by the SQLite page cache and the migration's own
  working set, not the data itself — the migration engine reads through the
  DB, it does not hold it in memory. The seed (which does hold the in-memory
  manifest) peaks far higher; that is a build-time cost, not a migration cost,
  and the two figures stay separate because `measure` runs as its own process.
- Peak additional disk is the transient WAL/table-rebuild growth, and it is
  where disk-constrained deployments exhaust storage. A deployment should size
  its disk for `final size + peak additional disk`, not the final size alone.
- Every supported path (v0.6.1/0.6.2/0.6.3 → current) is strictly cheaper than
  the v0.6.0 longest skip on the same data; the CI large-dataset job runs all
  four and asserts the same invariants (row counts + integrity).

## Resource exhaustion (issue #498 coordination)

The disk-exhaustion case is exercised as an **external-fault CI job**
(`large-migration-disk-full` in `.github/workflows/chaos-tests.yml`): a real
10,000-contact floor database on a tmpfs sized to hold the file plus only 1 MiB
— enough for the file, not for the ~16 MB of WAL growth the migration needs —
migrated until it runs out of space. Two defined outcomes, asserted by the job:

1. **Backup target on the same full disk** (the default `pre-migration/`
   sibling): the mandatory pre-migration backup (issue #530) cannot be written,
   so `migrate up` fails non-zero and **refuses before touching the database** —
   it stays clean at the floor version. Fail-closed, nothing dirtied.
2. **Backup target with room** (`MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` elsewhere
   — the realistic prod layout): the snapshot is written, then the migration
   itself hits ENOSPC. `migrate up` fails non-zero; the database is left
   **dirty at some migration with `PRAGMA integrity_check` ok** — never
   truncated, never a healthy-looking half-migration — and the next startup run
   **refuses** on the dirty flag (MIG-04, issue #439), naming the version and
   the restore-from-backup recovery.

Note that `RLIMIT_FSIZE` is deliberately NOT used as an in-process proxy for
this: Linux's `ftruncate` ignores the limit (a documented kernel quirk) and
SQLite pre-extends files with it, so the limit is not enforced against
SQLite's write pattern. A real filesystem that fills up is the honest test,
which is why this lives in the external-fault job.

The **low-memory** case is the same fail-closed mechanism (an OOM kill is a
SIGKILL with a different cause): a killed migration leaves the in-flight
transaction rolled back and the dirty flag set, and the next start refuses.
This is exercised in-process via the migration-statement fault seam on a large
database (`TestLargeDatasetInterruptedMigrationFailsClosed`) and externally by
the `migration-kill` chaos job's real SIGKILL. A dedicated low-memory CI job
(coordinating with issue #498) can run `migratebench measure` under a container
memory cap; the assertion to hold is the same: non-zero exit, then
`dbinspect` shows `integrity_check=ok ... dirty=true`, and a restart refuses.

## Interruptibility at scale

- **In-process** (`internal/schemafixture`'s
  `TestLargeDatasetInterruptedMigrationFailsClosed`): an armed fault at the
  migration statement seam fails the first pending migration on a 2,025-contact
  floor database, and the test asserts the full MIG-04 dance — dirty at the
  interrupted version, integrity ok, restart refuses with the typed
  `ErrDirtyMigration` naming restore-from-backup, and the operator-only
  `force` recovers to a clean latest schema.
- **External** (`migration-kill` chaos job): a real SIGKILL mid-migration and
  the same refusal + force-recovery, asserted against `cmd/migrate` +
  `cmd/dbinspect` on the deployed binaries.

## Backup restore at full size

`TestLargeDatasetBackupRestoresAtScale` (issue #495's "a backup strategy that
works on a 10 MB database and not a 10 GB one has not been tested"): a
`database.BackupSnapshot` (VACUUM INTO, the pre-upgrade backup primitive that
issue #530 makes mandatory on the startup migration path) of the large floor
database must

- produce a single self-contained file that passes `PRAGMA integrity_check` at
  full size;
- present the floor version (the pre-upgrade state);
- migrate to the current schema cleanly after restore (restore → upgrade).

## Migration progress is observable

Every migrator (server startup, `cmd/migrate`, `migratebench`) emits a
structured heartbeat per migration step: `event=migration_step_started` before
a migration body runs and `event=migration_step_completed` (with
`duration_ms`) after it commits and is marked clean, both naming the
`NNNNNN_name.up.sql` file. A migration that runs for twenty minutes shows a
new line per step instead of silence, so a hung-looking upgrade is
distinguishable from a working one (`database/migrate.go`,
`migrationProgressLogger`; pinned by `TestMigrationEmitsPerStepProgress` and
`TestLargeDatasetUpgradeEmitsProgress`). The batch-level
`migration_completed`/`migration_failed` events and the persisted
`system_events` rows are unchanged.

## Where the tests run

The heavy scale tests are gated behind `MYCORRHIZAL_LARGE_TESTS=1` and
**skipped** in the default Go suite: at 2,025 contacts each they take minutes
under `-race`, which every PR would pay. The named migration-tests
`large-dataset` job sets the variable and runs them on a main merge and
nightly with a 60-minute timeout. The fast pieces (the generator's pure-manifest
tests, the migration-progress test) run in the normal suite on every PR.

| Test | Location | CI |
|---|---|---|
| Generator units (scaling, validation, pathological-at-scale) | `internal/largedata` | rest leg (every PR) |
| Generator populate at 2,000 contacts (2,025 after block round-up) | `internal/largedata` `TestScalePopulatesLargeDataset` | migration-tests `large-dataset` job (main merge + nightly, `MYCORRHIZAL_LARGE_TESTS=1`) |
| Upgrade every supported release at scale (row counts + integrity) | `internal/schemafixture/large_mig_test.go` | migration-tests `large-dataset` job (main merge + nightly) |
| Interrupted migration fails closed at scale | `internal/schemafixture` | migration-tests `large-dataset` job (main merge + nightly) |
| Backup restores at scale | `internal/schemafixture` | migration-tests `large-dataset` job (main merge + nightly) |
| Migration progress emitted | `database/migrate_progress_test.go` | rest leg (every PR) |
| ENOSPC during large migration | `.github/workflows/chaos-tests.yml` | nightly + chaos filter |
| SIGKILL mid-migration | `.github/workflows/chaos-tests.yml` | nightly + chaos filter |

To run the gated tests locally: `MYCORRHIZAL_LARGE_TESTS=1 go test
./internal/schemafixture/ -run TestLargeDataset`.

To re-record the numbers: build the binaries, run the `migratebench` commands
above at the desired contact count, and update the table in this file — the
table and the report line format are the reproducibility contract.
