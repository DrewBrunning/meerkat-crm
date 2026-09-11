# Disaster recovery boundaries

This document states **what recovery guarantees Mycorrhizal gives, and where they
stop.** Its value is as much in the stated limits as in the procedures: an
operator who knows backups are theirs to schedule knows they can lose whatever
was written since the last one. An operator who assumes continuous protection
finds out at the worst moment.

It is written to be followed under stress, by one person, on their own instance —
so each scenario is exact commands and exact expected output, not prose
describing the idea. Where another runbook owns the detailed steps, this document
points at it rather than restating them.

| | |
|---|---|
| **Scope** | A self-hosted single instance — the shipped all-in-one Docker image. There is no replica and no failover; recovery means **restore**. See [The single-instance reality](#the-single-instance-reality). |
| **Companion docs** | `docs/deployment.md` → Backups (the authoritative backup/restore/verify *procedures* — the three-piece backup, `make backup`, `make backup-verify`, Restore); `docs/operations/migration-recovery.md` (MIG-05, issue [#440](https://github.com/DrewBrunning/mycorrhizal-crm/issues/440) — the migration-specific chapter: dirty schema, schema ahead of the binary, sub-floor, rolling back a bad release); `docs/security/incident-response.md` (issue [#509](https://github.com/DrewBrunning/mycorrhizal-crm/issues/509) — containment and credential/key rotation for a *compromise*, as opposed to a *loss*); `docs/upgrade-compatibility.md` (issue [#529](https://github.com/DrewBrunning/mycorrhizal-crm/issues/529) — the supported-upgrade range and the refusal states); `docs/security/data-retention-lifecycle.md` §10 (what a backup contains and how long copies of each data type survive). |
| **Policy anchors** | Upgrade floor is `v0.6.0` (issue [#529](https://github.com/DrewBrunning/mycorrhizal-crm/issues/529)). Downgrade is unsupported; rollback is **install the previous version, restore the pre-upgrade backup**, and the upgrade path takes that backup automatically and fail-closed (issue [#530](https://github.com/DrewBrunning/mycorrhizal-crm/issues/530)). Three fail-closed migration refusal states (MIG-04, issue [#439](https://github.com/DrewBrunning/mycorrhizal-crm/issues/439)) plus the pre-migration-backup gate (issue [#530](https://github.com/DrewBrunning/mycorrhizal-crm/issues/530)). `make backup` owns the database snapshot; the operator owns the photo/attachment directories (BACKUP-02, issue [#454](https://github.com/DrewBrunning/mycorrhizal-crm/issues/454)). |

## The single-instance reality

Mycorrhizal ships as one all-in-one container: nginx and the Go backend in a
single process group, one SQLite database, two plain directories for
photos and attachments. There is:

- **No replica.** Nothing holds a second live copy of the data.
- **No failover.** If the instance is down, it is down until you bring it back.
- **No point-in-time replay.** SQLite runs in WAL mode for crash-consistency, not
  for log shipping; there is no transaction log you can replay forward to a
  chosen instant.

So every recovery in this document is the same shape: **stop the instance,
put a known-good copy of the data in place, start the instance, verify.** Saying
so plainly is more useful than implying resilience that does not exist. The two
things the application *does* run for you are the automatic
[pre-migration backup](#the-recovery-inputs) before every schema change and the
[scheduled integrity and restore drills](#the-recovery-inputs) that catch a
silently-broken backup before you need it.

## RPO and RTO

**RPO** (recovery point objective) is how much data a recovery may lose.
**RTO** (recovery time objective) is how long recovery takes. Both are scoped to
the single-instance reality above — no replica, no failover — and stated per
mechanism, because the mechanisms genuinely differ.

The **derivation** — the numbers traced back to the shipped defaults, the levers
that move each one, the measured restore-time figures, and the one freshness
alarm the app enforces itself (`backup_stale`) — lives in `docs/deployment.md` →
"Recovery objectives (RPO and RTO)" (issue [#506](https://github.com/DrewBrunning/mycorrhizal-crm/issues/506)),
and `TestRecoveryObjectivesDocMatchesShippedDefaults` pins it to the code. This
section is the per-scenario consolidation: which objective applies to each
recovery path below, and where it bites.

| Recovery case | RPO (data loss) | RTO (time to recover) |
|---|---|---|
| **Planned rollback of a bad release** (issue [#530](https://github.com/DrewBrunning/mycorrhizal-crm/issues/530)) | **0.** The server writes a verified snapshot immediately before it applies any migration and refuses to migrate if it cannot. The rollback point is the instant before the upgrade ran — you lose only what was written while the bad release was up, and you lose it *visibly, by choice*. | Minutes. Deploy the previous image, restore the `pre-migration/` snapshot (a file copy), restore the photo/attachment directories from the routine backup, start. See `docs/operations/migration-recovery.md` → Rolling back a bad release. |
| **Database corruption, host loss, a lost photo/attachment directory** | **= your backup interval.** The application ships **no backup scheduler** — routine backups are a cron *you* add (`docs/deployment.md` → Backups). Your backup interval *is* your RPO. The recommended daily cron means up to ~24 h of loss; an hourly cron means ~1 h. There is no default number to quote because there is no default schedule. | Under 30 minutes at MVP scale (100k contacts / ~450 MB) for a prepared operator with the backup set already on the host — the derivation and its per-step table are in `docs/deployment.md`. Add off-host fetch time on top. |
| **Accidental deletion by the user** | **= your backup interval** (same as host loss). There is no self-service "undelete" — the supported recovery is a point-in-time restore. The soft-delete window keeps the row for `DELETED_RETENTION_DAYS` (default 30) as a sync tombstone, but the delete also hard-deletes join rows and removes attachment/photo files from disk immediately. See [Accidental deletion by the user](#scenario-accidental-deletion-by-the-user). | As above. A single accidental *edit* to a contact is different: `POST /api/v1/audit/:id/undo` reverts it in one call, no downtime. |

### What dominates the restore clock

For anything that is a full restore (the middle and bottom rows above), the time
is almost entirely **outside** the application:

1. **Obtain the backup media** — pull from off-host storage. Bounded by your
   network and where you keep backups; usually the largest term, and the
   application cannot make it smaller. If backups lived only on the lost host,
   this term is *infinite* — see [Loss of the host entirely](#scenario-loss-of-the-host-entirely).
2. **Copy the three pieces into place** — `cp` the `.db`, `rsync` the photo and
   attachment directories. The file directories dominate at scale (they are
   files, not rows, and are not compressed by the backup).
3. **Startup migration**, if the snapshot predates the running binary — measured
   at seconds even for large databases (`docs/development/scale-testing.md`:
   `v0.6.0 → current` at ~100k contacts is ~6.5 s; the longest supported skip at
   2,010 contacts is ~0.15 s).
4. **Verify** — [Verifying a recovered instance](#verifying-a-recovered-instance).
   Minutes.

There is no sub-minute RTO, because there is no failover: every recovery includes
a cold start.

### The gaps, stated rather than rounded away

- **The recommended daily backup means up to 24 hours of loss.** This is the
  honest RPO for the host-loss case and it is a property of the operator's
  schedule, not a defect. To shorten it: run `make backup` more often (it is
  cheap — a `VACUUM INTO` of a 10 MB database is milliseconds), and configure an
  off-host copy (`docs/deployment.md` → Backup immutability & ransomware
  resistance) so the copy survives host loss.
- **The app enforces only a loose freshness floor, not your RPO.** The
  `backup_stale` alert fires when no successful backup or restore-drill run has
  been seen for `ALERT_BACKUP_MAX_AGE_HOURS` (default `2 ×
  DB_RESTORE_DRILL_INTERVAL_HOURS` = **336 h** / 14 days) — a "something is badly
  wrong" alarm, deliberately far looser than a daily cron. Set it to roughly
  twice your actual backup interval to have the app hold you to a tighter number.
- **The upgrade case is already at RPO 0** and needs nothing from you — the
  pre-migration snapshot is mandatory and automatic. The gap is entirely in the
  *routine* backup cadence, which is why that is the lever worth pulling.
- **RTO drift is tracked, not estimated once.** The weekly restore drill records
  its wall-clock as `duration_ms` on each `restore_test_completed` row in the
  System-events timeline; set `DB_RESTORE_DRILL_MAX_DURATION_SECONDS` to a budget
  to get a `WARN` (and a note on that row) when a run runs long — restore-time
  drift visible *before* an incident. Re-measure against
  `docs/development/scale-testing.md`'s methodology before quoting a fresh number
  in a release note.

### Resource requirements (free space, memory, write-lock)

The bulk operations that bracket a recovery — **backup** (`VACUUM INTO`) and
**restore** — each write **a full second copy of the database**, so both need
**free disk space at least equal to the live database size** before they start.
An operator at 60% disk usage on the database volume cannot safely take a
backup. Photos and attachments are copied on top of that (they are files, not
rows, and are not compressed).

The measured per-operation envelope — duration, peak memory, peak extra disk,
and how long each holds SQLite's single write lock — is in
`docs/development/data-movement-benchmarks.md` (PERF-03, issue
[#470](https://github.com/DrewBrunning/mycorrhizal-crm/issues/470)), regenerated
by `make gen-perf-baseline`. The load-bearing facts for an operator:

- **Backup does not block writes.** `VACUUM INTO` reads through the WAL, so the
  server keeps serving reads *and* writes for the snapshot's duration — it is a
  background task, safe to run against a live instance.
- **An FTS rebuild does block writes** for its whole single transaction
  (`docs/operations/search-index.md`). At MVP scale that is well under a second;
  it is not a backup-style background task.
- **Two operations have super-linear peak memory.** Duplicate detection's pair
  list is O(n²) in the size of a duplicate cluster; a bulk **import**'s preview
  pass holds the whole batch *and* compares each row against every prior row
  (also O(n²)). Both are capped in the product — dedup is a review surface,
  imports cap at 20000 rows — but a genuinely huge address book should be
  imported in chunks.
- **Exports buffer the whole result in server memory** before the first byte is
  sent. Peak RAM scales with the export size; there is no per-request disk cost.

## The recovery inputs

Every procedure below draws on the same small set of artifacts. Know where each
one is before you need it.

| Input | What it is | Where it lives | Who maintains it |
|---|---|---|---|
| **Routine backup — database** | A `VACUUM INTO` snapshot, WAL-safe, verified with `PRAGMA integrity_check` before it is considered valid. | `make backup` writes `<db-stem>-<YYYYMMDD-HHMMSS>.db` beside `SQLITE_DB_PATH`, or wherever `BACKUP_PATH` points. | **You** — via a cron you add. The app has no scheduler. |
| **Routine backup — files** | Plain-directory copies of `PROFILE_PHOTO_DIR` and `ATTACHMENTS_DIR`. `make backup` does **not** touch these (BACKUP-02, issue [#454](https://github.com/DrewBrunning/mycorrhizal-crm/issues/454)); a file copy is already the right tool. | Wherever your `rsync`/`cp` puts them. | **You.** |
| **Pre-migration snapshot** | A verified database-only `VACUUM INTO` taken automatically before the server (or `make migrate-up`) applies any pending migration. The server **refuses to migrate if it cannot write one** (`ErrPreMigrationBackupFailed`, issue [#530](https://github.com/DrewBrunning/mycorrhizal-crm/issues/530)). | A `pre-migration/` subdirectory beside `SQLITE_DB_PATH`, named `<db-stem>-pre-migration-<from>-to-<to>-<timestamp>.db`. `MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` moves it. | **The application** writes it; **you** must keep a rotation cron away from the `pre-migration/` directory (issue [#530](https://github.com/DrewBrunning/mycorrhizal-crm/issues/530): a purge must never delete the last rollback point). |
| **Scheduled integrity check** | `PRAGMA integrity_check` against the live database, on a cadence; logs and fires the `db.integrity_check_failed` webhook on failure. | `DB_INTEGRITY_CHECK_ENABLED` (default on), `DB_INTEGRITY_CHECK_INTERVAL_HOURS` (default 24). | The application. |
| **Scheduled restore drill** | Takes a fresh snapshot, restores it into a scratch database, compares every table's row count with live, reconciles attachment/photo rows against the live directories, and verifies the wrapped encryption key still unwraps. Fires `db.restore_drill_failed` on any mismatch, and records `duration_ms` on the `restore_test_completed` timeline row (the RTO drift signal — see [RPO and RTO](#rpo-and-rto)). | `DB_RESTORE_DRILL_ENABLED` (default on), `DB_RESTORE_DRILL_INTERVAL_HOURS` (default 168, i.e. weekly), `DB_RESTORE_DRILL_MAX_DURATION_SECONDS` (optional RTO budget). | The application. |
| **The secrets** | `JWT_SECRET_KEY`, and `DATA_ENCRYPTION_KEY` / `DATA_ENCRYPTION_KEY_FILE` if set. **Never in any backup** — they live only in your environment. | Your `.env` / orchestrator secret store. | **You**, stored separately from the data backup. See [A lost secret](#scenario-a-lost-jwt_secret_key-or-other-secret). |

## Scenarios

Each scenario states **what is recoverable**, **what is lost**, and **the
procedure**. Commands assume the shipped Docker layout and a checkout of this
repo with a Go toolchain on the host — the same "a clone of this repo used to
operate the instance" assumption `docs/deployment.md` already makes. Where a step
has a toolchain-free equivalent (`sqlite3`), it is given inline.

### Scenario: database corruption

The database file is damaged — `PRAGMA integrity_check` returns rows other than
`ok`, the server logs read errors, or `/health` reports the database subsystem
unhealthy. Storage-level damage (failing disk, a torn write from a host crash, a
truncated file).

**Recoverable:** everything up to your most recent good backup.

**Lost:** everything written since that backup (your RPO — see
[RPO and RTO](#rpo-and-rto)). If corruption is caught by the scheduled integrity
check, you also have the webhook timestamp bounding when it started.

**Procedure.**

1. **Stop the instance.** `docker compose stop`
2. **Confirm the corruption** — do not restore on a guess.
   ```sh
   sqlite3 "$SQLITE_DB_PATH" "PRAGMA integrity_check;"
   # → anything other than a single "ok" line is corruption
   ```
3. **Preserve the damaged file** before overwriting it — it is the only evidence
   of what went wrong.
   ```sh
   cp "$SQLITE_DB_PATH" /path/off-host/corrupt-$(date +%Y%m%d-%H%M%S).db
   ```
4. **Restore the three pieces** exactly as `docs/deployment.md` → Restore
   describes: remove the live `.db` and any stale `-wal`/`-shm` sidecars, copy
   the snapshot into place, `rsync --delete` the photo and attachment directories
   from the same backup set.
   ```sh
   rm -f "$SQLITE_DB_PATH" "$SQLITE_DB_PATH-wal" "$SQLITE_DB_PATH-shm"
   cp /backups/mycorrhizal.db "$SQLITE_DB_PATH"
   rsync -a --delete /backups/photos/      "$PROFILE_PHOTO_DIR"/
   rsync -a --delete /backups/attachments/ "$ATTACHMENTS_DIR"/
   ```
5. **Start the instance.** `docker compose start` — migrations run automatically
   if the snapshot is older than the binary.
6. **Verify** — [Verifying a recovered instance](#verifying-a-recovered-instance).

If `PRAGMA integrity_check` on the *backup* also fails, that snapshot is
unusable — see [A corrupted or incomplete backup](#scenario-a-corrupted-or-incomplete-backup)
and fall back to an older one.

### Scenario: a migration interrupted partway

The server refuses to start after an upgrade with a `dirty` migration state (a
migration began and did not finish — the process was killed, the container
OOM-killed, the host lost power).

This scenario is owned in full by **`docs/operations/migration-recovery.md` →
[Dirty schema](migration-recovery.md#dirty-schema)**. In summary:

**Recoverable:** everything, from the automatic pre-migration snapshot taken
before this upgrade began. **Lost:** nothing beyond that snapshot's point (RPO 0
for the upgrade case). **Procedure:** restore the `pre-migration/` snapshot,
start again — starting the server *is* the retry. Do not retry in a loop; if it
fails again at the same migration, capture the logs and file a bug. An
operator-only escape hatch (`make migrate-force`, prompted) exists for the case
where you have verified the schema actually matches a known version.

### Scenario: a bad release requiring rollback

Release N+1 booted, migrations ran, and now N+1 is misbehaving. You want N back.

This scenario is owned in full by **`docs/operations/migration-recovery.md` →
[Rolling back a bad release](migration-recovery.md#rolling-back-a-bad-release-n1--n)**.
In summary:

**Recoverable:** the instance as of the instant before the N→N+1 upgrade, from
the automatic pre-migration snapshot. **Lost:** everything written while N+1 was
up — visibly, at a known-good point, by your choice. Downgrade via `.down.sql` is
**not** the mechanism (a down migration that drops a column N+1 added would
silently discard whatever was written into it). **Procedure:** stop N+1, deploy
the N image, restore the `…-pre-migration-<N>-to-<N+1>-….db` snapshot, restore
the file directories from the routine backup, start N. **Restoring the snapshot
without deploying the N binary is not a rollback** — N+1 will migrate the
restored database forward again.

### Scenario: accidental deletion by the user

A contact and its notes, activities, reminders, and life events were deleted by
mistake (`DELETE /api/v1/contacts/:id`). Know what the soft-delete window does
and does not give you, because it is easy to over-read.

**What the soft-delete window *is*.** The primary user-authored rows (contact,
notes, activities, reminders, life events) are **soft-deleted** — `deleted_at`
is set, the row stays in the database for `DELETED_RETENTION_DAYS` (default
**30**), and then the purge job (`backend/services/purge_service.go`)
**hard-deletes** it. During the window the row is a sync tombstone and it keeps
the *content* present in the live database and in every new backup. That is the
entire guarantee. (`DELETED_RETENTION_DAYS=0` disables the purge and keeps the
rows forever; a negative value is rejected at startup.)

**What it is *not*: a self-service undo.** There is **no undelete endpoint.**
`POST /api/v1/audit/:id/undo` reverts an accidental **update** to a contact and
**explicitly rejects delete events**. Recovering a deleted contact means a
**point-in-time restore from a backup taken before the deletion**.

**Recoverable** (via that restore): everything in the pre-deletion backup — the
primary rows, the **join rows** (relationship edges, circle/household/tag
memberships, sync links) which the delete **hard-deleted with no window at all**,
and the **attachment and profile-photo files**, which `DeleteContact` removes
from disk at delete time (outside the transaction — "file deletion cannot be
rolled back"), *provided* you also have a photo/attachment directory backup from
before the deletion.

**Lost:**

- Everything written anywhere on the instance since that backup (your RPO).
- Attachment/photo **file content** if no directory backup predates the deletion
  — a database-only restore brings the rows back pointing at missing files.
- Everything, if you are past `DELETED_RETENTION_DAYS` *and* every backup that
  predates the purge has also been deleted.

Clearing `deleted_at` by hand on the soft-deleted rows is a partial, unsupported
last resort: it does not bring back the hard-deleted join rows or the on-disk
files, and it leaves the FTS index and change-feed cursors to reconcile. Prefer
the restore.

**Procedure.**

1. **Identify the backup** whose timestamp is the latest one *before* the
   deletion (check the audit log — `GET /api/v1/audit` — for the delete event's
   `created_at`).
2. **Restore that three-piece set** per `docs/deployment.md` → Restore (stop
   server / replace `.db` + clear sidecars / `rsync --delete` both directories /
   start). This is a point-in-time rollback: everything since that backup is
   lost.
3. **Verify** — [Verifying a recovered instance](#verifying-a-recovered-instance)
   — and confirm the specific contact, its relationships, and its attachments are
   back.

For an accidental **edit** (not a delete) to a contact, no restore is needed:

```sh
curl -s -b cookies.txt "https://<host>/api/v1/audit?limit=50"        # find the update event's id
curl -s -X POST -b cookies.txt "https://<host>/api/v1/audit/<id>/undo"
```

### Scenario: loss of the host entirely

The machine is gone — hardware failure, a destroyed VM, a cloud account locked
out, ransomware that reached everything the app's uid could write.

**Recoverable:** exactly what you copied off the host, and nothing else. This is
the scenario that decides whether your backup strategy was real.

**Lost:**

- Everything written since the last **off-host** backup (your RPO — and note it
  is the *off-host* copy that counts here, not a snapshot that sat on the dead
  disk).
- The secrets, if they lived only on that host — see
  [A lost secret](#scenario-a-lost-jwt_secret_key-or-other-secret). A lost
  `DATA_ENCRYPTION_KEY` makes the encrypted columns in every backup
  unrecoverable.
- Any backup that was stored only on the lost host. An on-host `pre-migration/`
  directory does not survive host loss; that is why a full rollback also needs
  the routine off-host backup.

**Procedure.**

1. **Provision a new host** and install the same Mycorrhizal version the backup
   was taken under (or newer — a newer binary migrates the restored database
   forward on boot; an *older* binary refuses, `ErrSchemaAheadOfBinary`).
2. **Restore the environment first.** Put `JWT_SECRET_KEY` and, if you used one,
   `DATA_ENCRYPTION_KEY` / `DATA_ENCRYPTION_KEY_FILE` back in place from wherever
   you keep secrets. The database backup does not contain them and a restore
   under a *different* `DATA_ENCRYPTION_KEY` fails closed at boot rather than
   serving garbage.
3. **Restore the three pieces** from the off-host backup, per
   `docs/deployment.md` → Restore (stop server / replace `.db` + clear sidecars /
   `rsync --delete` both directories / start).
4. **Start**, let startup migrations run, and **verify** —
   [Verifying a recovered instance](#verifying-a-recovered-instance).
5. Users log in again (a new or rotated `JWT_SECRET_KEY` invalidates old
   sessions; harmless). If `JWT_SECRET_KEY` changed *and* it was the at-rest key
   fallback, stored integration credentials and TOTP secrets must be re-entered —
   see `docs/security/incident-response.md` → `JWT_SECRET_KEY`.

### Scenario: loss of an attachment or photo directory while the database survives

The database is intact but `PROFILE_PHOTO_DIR` or `ATTACHMENTS_DIR` was lost,
partially deleted, or restored stale — a wrong `rsync`, a detached volume, a full
disk that dropped writes. The database now has attachment/photo rows whose files
are missing. The failure is silent until someone opens the affected attachment —
or until the next scheduled restore drill or `make backup-verify` run names it.

**Recoverable:** every file present in your most recent photo/attachment
directory backup. These directories are plain files; a file-level restore is
exactly right.

**Lost:** any file added since that directory backup and not elsewhere. The
database rows for those files remain — the app does not delete a row because its
file vanished — so the entity still lists the attachment; it just cannot be
downloaded. There is no way to reconstruct a file's *content* from the database.

**Procedure.**

1. **Identify the holes** — do not restore blind.
   ```sh
   SQLITE_DB_PATH="$SQLITE_DB_PATH" \
   PROFILE_PHOTO_DIR="$PROFILE_PHOTO_DIR" \
   ATTACHMENTS_DIR="$ATTACHMENTS_DIR" \
     make backup-verify
   # names every live row with no backing file (exit non-zero); orphan files are
   # reported but do not fail the command.
   ```
2. **Stop the instance.** `docker compose stop`
3. **Restore only the affected directory** from backup:
   ```sh
   rsync -a --delete /backups/attachments/ "$ATTACHMENTS_DIR"/
   # or /backups/photos/ → "$PROFILE_PHOTO_DIR"
   ```
   The database is untouched — do **not** restore the `.db` for this scenario, or
   you will roll every row back to the directory backup's age for no reason.
4. **Start**, then **re-run `make backup-verify`** — it should now report no
   missing files (orphans are fine).
5. For any file still missing (added after the last directory backup, gone
   everywhere): decide per row — delete the attachment through the app so the row
   stops promising a file that does not exist, or accept the dangling row and
   note it. The DB-01 checker (issue [#460](https://github.com/DrewBrunning/mycorrhizal-crm/issues/460))
   reports these as a standing invariant violation.

### Scenario: a corrupted or incomplete backup

You went to restore and the backup itself is bad — `PRAGMA integrity_check` on
the snapshot fails, the file is truncated, or `make backup-verify` reports
missing photo/attachment files in the set.

**Recoverable:** whatever an *older, valid* backup holds. A backup that fails
verification is not a recovery input — treat it as absent.

**Lost:** the delta between the newest valid backup and the failed one. If *no*
backup verifies, you are at [what is not recoverable](#what-is-not-recoverable):
the live database (if it still exists and is intact) is your only copy.

**Procedure.**

1. **Verify candidates newest-first** and stop at the first that passes both
   checks:
   ```sh
   sqlite3 /backups/<candidate>.db "PRAGMA integrity_check;"        # → ok
   SQLITE_DB_PATH=/backups/<candidate>.db \
   PROFILE_PHOTO_DIR=/backups/<photos-for-that-set> \
   ATTACHMENTS_DIR=/backups/<attachments-for-that-set> \
     make backup-verify                                             # → no missing files
   ```
2. Restore that set per `docs/deployment.md` → Restore.
3. **Verify the recovered instance** —
   [Verifying a recovered instance](#verifying-a-recovered-instance).
4. **Fix the backup pipeline** before trusting it again: this is what the
   scheduled restore drill exists to catch *before* a disaster
   (`DB_RESTORE_DRILL_ENABLED`). If the drill was firing `db.restore_drill_failed`
   and the alert was missed, that is the gap to close, not the backup.

**Prevention is the real control here.** Run `make backup-verify` against every
backup set as it is taken, not only at restore time — a hole found the moment the
backup is made is far cheaper than one found mid-disaster.

### Scenario: a lost `JWT_SECRET_KEY` or other secret

A secret is *gone* (not leaked — leak/compromise is
`docs/security/incident-response.md`). Which secret decides whether this is a
shrug or unrecoverable.

| Secret | Lost with no copy anywhere → | Recovery |
|---|---|---|
| **`JWT_SECRET_KEY`** | **Harmless to the data.** Generate a new one (`openssl rand -base64 32`), set it, restart. Every session and 2FA challenge is invalidated — users log in again. | Set a fresh value and restart. |
| **`JWT_SECRET_KEY`, when it is *also* the at-rest key** (no `DATA_ENCRYPTION_KEY` set — the zero-config fallback) | **Not recoverable.** The field-encryption DEK was wrapped under a key derived from this secret; losing it means the encrypted columns cannot be unwrapped and the server fails closed at boot. | None for the encrypted columns. Restore a backup *and the secret it was taken under* together, or accept the loss of those columns. This is why `docs/security/incident-response.md` tells you to decouple the at-rest key onto a dedicated `DATA_ENCRYPTION_KEY` *now*, before any incident. |
| **`DATA_ENCRYPTION_KEY` / `DATA_ENCRYPTION_KEY_FILE`** | **Not recoverable.** No escrow, by design. Every `encv1:` column in the live database and in every snapshot taken under it is permanently unreadable. | None. The restore drill verifies weekly that the *live* key still unwraps the DEK precisely because there is no fallback. Keep this key backed up wherever you keep the rest of your secrets, separate from the data backup. |
| **`OIDC_CLIENT_SECRET`, SMTP / `RESEND_API_KEY`, notification tokens** | No data impact. | Re-issue at the provider, set the new value, restart. Verify one login / one send. |

**What travels *with* the database backup, so is not a separate secret to
track:** TOTP seeds and recovery-code hashes (rows in the `users` table),
password hashes, API-token hashes. A restore brings these back as they were at
the snapshot's point.

## What is not recoverable

State this without hedging. Nothing below has a procedure — it is loss by design
or by physics, and the mitigations are all *before* the fact.

- **Data written after your last backup**, bounded by your backup interval (RPO).
  For the upgrade case only, the automatic pre-migration snapshot brings this to
  0; everywhere else it is whatever your cron cadence is.
- **A `DATA_ENCRYPTION_KEY` lost with no copy** — and, if you never decoupled it,
  a `JWT_SECRET_KEY` lost with no copy. The `encv1:` columns are gone. No escrow.
- **Soft-deleted data past the purge window** (`DELETED_RETENTION_DAYS`, default
  30) once every backup predating the purge has also been deleted.
- **Attachment and photo file *content*** that exists in no directory backup. The
  database never stored the bytes; it stored a path.
- **Audit-trail history more recent than the restored snapshot.** A restored
  audit log is only as fresh as the backup; events between the snapshot and the
  incident are not in it.
- **Anything on a lost host that was never copied off it** — including on-host
  `pre-migration/` snapshots and on-host-only routine backups.
- **A precise pre-incident instant.** There is no point-in-time replay; you
  recover to a backup's point, not to "5 minutes before the mistake".

Derived state that looks like loss but is not: the **FTS search index** and the
**denormalized `contacts.*` columns** (the flat name/email/phone projection,
`sort_name`, `addresses_flat`, `phones_normalized`) are both fully rebuildable
from canonical data. After a restore, run **both** rebuilds and then the DB-01
checker:

```sh
curl -s -XPOST https://<host>/api/v1/admin/search/rebuild            # or: go run ./cmd/backfill-search-index
curl -s -XPOST https://<host>/api/v1/admin/contacts/rebuild-derived  # or: go run ./cmd/backfill-derived-columns
```

A `VACUUM INTO` snapshot is transactionally consistent, so on a healthy backup
these are no-ops — but a snapshot taken from a database that was itself mid-repair
might not be, and the checker (verification step 2 below) proves it either way.
The full catalogue of derived / cached state is
[`derived-data-inventory.md`](../derived-data-inventory.md). **In-flight sessions /
2FA challenges** dropped by a key change are expected, not data loss.

## Verifying a recovered instance

A recovery you have not verified is a hypothesis. Run all four checks; each rules
out a different class of failure.

1. **Storage integrity** — the file is a valid, up-to-date SQLite database.
   ```sh
   go run ./cmd/dbinspect/main.go "$SQLITE_DB_PATH"
   # → integrity_check=ok version=<N> dirty=false
   # any other line — a dirty flag, a version mismatch, a non-ok integrity check — is a failure
   ```
   No Go toolchain:
   ```sh
   sqlite3 "$SQLITE_DB_PATH" "PRAGMA integrity_check;"   # → ok
   ```
2. **Application invariants** — the data is meaningful, not just structurally
   intact. The DB-01 checker (issue [#460](https://github.com/DrewBrunning/mycorrhizal-crm/issues/460))
   reports relationships pointing at missing contacts, orphaned join rows,
   attachment/photo rows with no file, a denormalized `contacts.*` column that
   disagrees with its Card (issue [#497](https://github.com/DrewBrunning/mycorrhizal-crm/issues/497)),
   and similar logical holes, distinctly from the storage pass. Run it (or
   `make backup-verify` for the file-reconciliation subset) and confirm it is
   clean — a `derived_contact_column.divergent` finding here means the
   rebuild-derived step above has not been run.
3. **Health and readiness** — the server agrees it is serving.
   ```sh
   curl -s https://<host>/health/ready   # → {"status":"ready", …}   (not_ready while migrations pending/dirty)
   curl -s https://<host>/health         # → healthy, with the applied migration version in the payload
   ```
4. **One end-to-end workflow** — the same basic flow the clean-install smoke test
   asserts (issue [#450](https://github.com/DrewBrunning/mycorrhizal-crm/issues/450),
   `backend/cmd/deploysmoke`): log in with a **pre-existing** account (the bcrypt
   hash must still validate), open a known contact, read a recent note and a
   reminder, open a contact photo, download an attachment, run a search, export
   the contact and read it back. Each touches a subsystem a restore can get
   wrong (auth, FTS, the two file directories, the exporters).

Finally, let the scheduled restore drill (`DB_RESTORE_DRILL_ENABLED`, weekly by
default) run once against the recovered instance, or trigger a manual round-trip
on a throwaway copy (`frontend/e2e/backupRestore.spec.ts` automates exactly
that): back up, restore, compare row counts.

## The drill

The milestone bar is "recovery procedures are documented **and have been
exercised by following the documentation**." Documentation written by the person
who built the system encodes what they already know; the only way to find the
missing step is to follow it as written, on a machine you did not set up.

To exercise this document: for each scenario below, induce the failure on a
throwaway instance and recover **following only the section above — not from
memory or from the code.** A step that is missing, wrong, or impossible to follow
from this document is a bug in this document; fix it here.

| Scenario | Induce it by… | Recover by… | Passes when… |
|---|---|---|---|
| Database corruption | `printf '\0\0\0\0' \| dd of="$SQLITE_DB_PATH" bs=1 seek=1024 count=4 conv=notrunc` on a stopped instance (or restore a deliberately truncated file) | [Database corruption](#scenario-database-corruption) | `dbinspect` → `integrity_check=ok version=<N> dirty=false`; `/health/ready` → `ready`; the workflow in [Verifying a recovered instance](#verifying-a-recovered-instance) completes |
| Migration interrupted | SIGKILL the process mid-migration, or `UPDATE schema_migrations SET dirty = 1` | `docs/operations/migration-recovery.md` → [Dirty schema](migration-recovery.md#dirty-schema), reached via [this doc's summary](#scenario-a-migration-interrupted-partway) | Restored DB reports `integrity_check=ok`, the expected version, `dirty=false` |
| Bad release rollback | Deploy a newer image against a populated database (let it migrate), then decide to roll back | `docs/operations/migration-recovery.md` → [Rolling back a bad release](migration-recovery.md#rolling-back-a-bad-release-n1--n) | The instance serves the previous version's schema; row counts match the pre-migration snapshot's point |
| Accidental edit (undoable) | Update a contact through the API; note the update event's ID from `GET /api/v1/audit` | [Accidental deletion](#scenario-accidental-deletion-by-the-user) → the edit-undo call | `POST /api/v1/audit/<id>/undo` returns 200 and the contact's previous field values are restored |
| Accidental deletion | Delete a contact through the API; note the delete event's `created_at` | [Accidental deletion](#scenario-accidental-deletion-by-the-user) → point-in-time restore from a backup predating the delete | The contact, its relationships, and its attachments are back; you can state exactly what else the rollback cost. Confirm `POST /api/v1/audit/<delete-id>/undo` is refused ("undo supports update events only") — there is no undelete |
| Host loss | Provision a fresh machine with none of the original config or volumes; restore from an off-host backup only | [Loss of the host entirely](#scenario-loss-of-the-host-entirely) | New instance passes all four checks in [Verifying a recovered instance](#verifying-a-recovered-instance); pre-existing account logs in |
| File directory loss | `rm -rf "$ATTACHMENTS_DIR"/*` on a stopped instance | [Loss of an attachment or photo directory](#scenario-loss-of-an-attachment-or-photo-directory-while-the-database-survives) | `make backup-verify` reports no missing files after the directory-only restore; database rows unchanged |
| Corrupted backup | Truncate the newest backup `.db`; keep an older valid one | [A corrupted or incomplete backup](#scenario-a-corrupted-or-incomplete-backup) | The newest snapshot is rejected by `PRAGMA integrity_check`; the older one restores and verifies |
| Lost secret | Unset `JWT_SECRET_KEY`; separately, on an instance with no `DATA_ENCRYPTION_KEY`, unset it and try to boot | [A lost secret](#scenario-a-lost-jwt_secret_key-or-other-secret) | A fresh `JWT_SECRET_KEY` boots and serves (users re-login); the no-`DATA_ENCRYPTION_KEY` case is confirmed to fail closed at boot, matching the table |

Hand-verify per [CLAUDE.md](/CLAUDE.md): remove a step from one of the procedures
above and confirm the drill for that scenario cannot be completed from the
document alone. Restore the step.

DOC-04 (issue [#489](https://github.com/DrewBrunning/mycorrhizal-crm/issues/489))
brings this document under automated documentation testing — the command
sequences above are written to be extracted and run, so a doc that drifts from
reality fails a build rather than an operator.
