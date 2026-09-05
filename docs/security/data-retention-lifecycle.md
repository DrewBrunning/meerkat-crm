# Data retention & deletion lifecycle

The answer to "where does every copy of this piece of data live, and when does the last one go away?"
for every data type this app produces. `docs/security/asvs-l2.md` V8.3.8/V8.3.2 cite into this doc rather
than re-deriving the same evidence; `docs/security/threat-model.md` covers what "compromised" means for
each asset, not how long it survives.

| | |
|---|---|
| **Last updated** | 2026-09-05 (issues [#414](https://github.com/DrewBrunning/mycorrhizal-crm/issues/414), [#420](https://github.com/DrewBrunning/mycorrhizal-crm/issues/420), [#424](https://github.com/DrewBrunning/mycorrhizal-crm/issues/424), [#622](https://github.com/DrewBrunning/mycorrhizal-crm/issues/622), [#391](https://github.com/DrewBrunning/mycorrhizal-crm/issues/391), [#389](https://github.com/DrewBrunning/mycorrhizal-crm/issues/389), [#651](https://github.com/DrewBrunning/mycorrhizal-crm/issues/651), [#351](https://github.com/DrewBrunning/mycorrhizal-crm/issues/351), [#353](https://github.com/DrewBrunning/mycorrhizal-crm/issues/353), [#549](https://github.com/DrewBrunning/mycorrhizal-crm/issues/549), [#505](https://github.com/DrewBrunning/mycorrhizal-crm/issues/505), [#722](https://github.com/DrewBrunning/mycorrhizal-crm/issues/722)) |
| **Scope** | Backend (Go/Gin + SQLite), CardDAV/CalDAV (server role), Android client, browser/frontend, operator backups. |
| **Companion docs** | `docs/security/pii-inventory.md` (the *minimization* lens — should each store exist, and is it more/kept-longer than needed), `docs/security/asvs-l2.md` V8 (Data Protection), `docs/deployment.md` (Backups section — the authoritative backup/restore runbook), `docs/security/masvs-l1.md` (Android storage controls). |

Every row below answers the same four questions from the issue: **where** it's stored and who can reach
it, **how long** it's retained, **how** deletion happens and whether it propagates to other copies, and
**whether it survives in backups**.

## The pattern this doc mostly confirms, not invents

[CLAUDE.md](/CLAUDE.md)'s backend trap #7 already fixes the *policy* per model (soft-delete for
user-authored content, hard-delete for edge/join rows), and the T26 purge job
(`backend/services/purge_service.go`) already hard-deletes soft-deleted rows past a retention window.
What follows enumerates every *copy* of the data — not just the primary table row — and checks each one
against the same four questions. Most rows below turn out to already be correct, wired years before this
doc; a handful of genuine gaps are called out explicitly in [Known gaps](#known-gaps).

## 1. Primary application data (user-authored content)

`Contact`, `Note`, `Activity`, `Reminder`, `LifeEvent`, `Preference`, `CadencePolicy`,
`ConversationAgenda`, `Gift`, `ImmichConfig`/`PaperlessConfig`/`SeafileConfig`/`WebDAVConfig`,
`Attachment` (metadata row only — see [§5](#5-attachments--profile-photos-files-on-disk)).

- **Where / who**: `mycorrhizal.db`, scoped by `user_id` in every query (CLAUDE.md trap #5). Reachable
  only via the authenticated owner's API session.
- **Retention**: live until the user deletes it; soft-deleted (`deleted_at` set) for
  `DELETE_RETENTION_DAYS` (default 30, `config/config.go:70,150`) as an undo window (audit `Undo`,
  `audit_controller.go`) and a sync tombstone (T17 `?since=` feed).
- **Deletion / propagation**: `DeleteContact` (`backend/controllers/contact_controller.go:829-886`)
  cascades every dependent row via `deleteContactAssociations`
  (`backend/controllers/contact_controller.go:686+`) inside one transaction; `DeleteUser`
  (`backend/controllers/admin_user_controller.go`) does the account-wide equivalent. After the retention
  window, `PurgeSoftDeletedRows` (`backend/services/purge_service.go:25-135`) hard-deletes the row and
  its remaining edge references, run daily by cron and on-demand via the admin `TriggerPurge` endpoint
  (`admin_user_controller.go:37-42`). A `?since=` cursor older than the window gets `410 Gone`
  (`controllers/helpers.go:360-370`) — deliberately the *same* `DeleteRetentionDays` config the purge job
  reads, so a client can never observe a tombstone gap; propagation to CardDAV/CalDAV and the Android
  mirror is covered in §7/§8, both of which key off this same soft-delete state.
- **Backups**: yes, full row (including still-in-window soft-deleted rows) — see [§10](#10-backups).
- **Verification**: `backend/services/purge_service_test.go` (`TestPurgeSoftDeletedRows_*`, 8 cases
  including idempotency and "never touches live rows"); `admin_user_controller_test.go` M1/M1b/M5
  (window-pinned purge, live rows untouched, `TriggerPurge` executes).

### ContactShare snapshots (issue #574)

`ContactShare` (`backend/models/contact_share.go`) is a **frozen, independent copy** of a filtered
JSContact export of a contact, serialized once at share-creation time — no `deleted_at`, no FK back to
the original `Contact` (by design: the snapshot must survive the original being edited or deleted).
It sits outside the soft-delete model above precisely because it is a copy, not the live row.

- **Where / who**: `contact_shares` table, scoped to `from_user_id`/`to_user_id`. Reachable only via the
  authenticated sender's or recipient's API session.
- **Retention**: `CONTACT_SHARE_RETENTION_DAYS` (default 30, `config/config.go:72,153`) for **every
  status**, anchored on the moment the share stopped being actionable: `pending` ages from `created_at`
  (an abandoned invite cannot materialize years later); `accepted`/`declined` age from `responded_at`
  (the recipient has already imported or rejected the snapshot).
- **Deletion / propagation**: `PurgeExpiredContactShares`
  (`backend/services/contact_share_purge_service.go`) hard-deletes rows past the window, run under the
  same daily cron + job lock as the T26 purge (`PurgeDeletedRows`, `backend/services/purge_service.go`)
  and via the admin `TriggerPurge` endpoint. Deleting the snapshot destroys no one's real data: the
  sender's original `Contact` is untouched (no FK, by design), and an accepted share's payload was
  already imported into the recipient's account. `CONTACT_SHARE_RETENTION_DAYS<=0` is treated as
  "disabled", never "delete everything".
- **Backups**: yes — a share sits in every backup taken before the purge removes it; restoring such a
  backup resurrects a share whose purpose (and both parties' copy of it) is long since served.
- **Verification**: `backend/services/contact_share_purge_service_test.go`
  (`TestPurgeExpiredContactShares_*` — window-pinned per status, ages-responded-from-responded-at,
  `NULL responded_at` never purged, disabled-when-zero, idempotent). The frozen-snapshot design itself
  (as opposed to its retention window) is pinned separately by issue #555's
  `backend/controllers/contact_share_matrix_test.go` (`TestCreateContactShare_PayloadFrozenAtCreation`,
  `TestContactShare_PayloadUnchangedAcrossLifecycleTransitions`) and surfaced to the sender by
  `frontend/src/components/ShareContactDialog.tsx`'s `frozenNotice`.

## 2. Edge- and join-shaped rows

`RelationshipEdge`, `CircleMember`, `ContactTag`, `HouseholdMember`, `ContactSyncLink`,
`CalendarEventLink`, `FieldValue`, `activity_contacts`, `NotificationDelivery`, `ReachOutSuggestion`,
`ContactSyncConflict`.

- **Where / who**: same DB, same `user_id` scoping.
- **Retention**: none — CLAUDE.md trap #7 hard-deletes these immediately (a natural-key unique index
  would otherwise block re-creating the same edge after a soft-delete ghost).
- **Deletion / propagation**: removed synchronously in the same transaction as the owning entity's delete
  (`deleteContactAssociations`, `PurgeSoftDeletedRows`'s `cleanups` slice for anything that outlives a
  purged contact as defense-in-depth).
- **Backups**: no — gone before any subsequent backup is taken (unless a backup predates the delete, in
  which case restoring it resurrects the edge along with its endpoints — see §10).

## 3. Audit trail (`AuditEvent`)

- **Where / who**: `audit_events` table, append-only, tamper-evident hash chain (issue #381,
  `models/audit_chain.go`). Deny-listed fields never enter it (`models/audit.go`).
- **Retention**: `AUDIT_RETENTION_DAYS` (default 90, `config/config.go:71,151`) — independent from the
  30-day content-deletion window, since audit's job is post-hoc investigation, not undo.
- **Deletion / propagation**: `PurgeExpiredAuditEvents` (`backend/services/audit_purge_service.go:26-45`)
  hard-deletes rows older than the window and re-links the surviving hash chain
  (`models.RecomputeAuditChain`) so tamper-evidence isn't broken by the purge itself. Runs daily via cron
  (`backend/main.go:185-191`); `AUDIT_RETENTION_DAYS<=0` is treated as "disabled", never "delete
  everything". No external mirror — audit never syncs to CardDAV/CalDAV/Android.
- **Backups**: yes, and a restored backup's audit trail is only as fresh as the snapshot.
- **Verification**: `backend/services/audit_purge_service_test.go` (`TestPurgeExpiredAuditEvents*`, 3
  cases including the re-link and a swallowed-recompute-failure case).

### System events (`SystemEvent`, issue #424)

`system_events` is the persisted operational-event timeline — application start/stop, scheduled job
runs, sync runs, notification dispatch, backup/restore drills. System-generated diagnostics, **not
user data**: no `user_id` scoping on the query (it records what happened to the *instance*), no
external mirror, admin-only over the API (`GET /admin/system-events`).

- **Where / who**: `system_events` table in `mycorrhizal.db`. Read only via an admin API session; no
  CardDAV/CalDAV projection, not in the Android offline mirror.
- **Retention**: `SYSTEM_EVENT_RETENTION_DAYS` (default 30, `config/config.go`) — short by design:
  long enough to investigate a recent incident, short enough to bound growth on a single-file
  database. Deliberately shorter than the 90-day audit window — operational noise is not an
  investigation record.
- **Deletion / propagation**: `PurgeExpiredSystemEvents`
  (`backend/services/system_event_purge_service.go`) hard-deletes rows whose `occurred_at` is older
  than the window, daily via cron (`backend/main.go`) under the `system_event_purge` job lock.
  `SYSTEM_EVENT_RETENTION_DAYS<=0` is treated as "disabled", never "delete everything". No hash chain
  (unlike audit) — this is diagnostics, not a tamper-evident record. The free-text `error`/`detail`
  fields are sanitized + length-capped by `models.RecordSystemEvent`, and the model carries no
  high-cardinality fields (no contact IDs, no raw URLs) by construction.
- **Backups**: yes, and a restored backup's timeline is only as fresh as the snapshot.
- **Verification**: `backend/models/system_event_test.go`,
  `backend/services/system_event_purge_service_test.go`,
  `backend/database/migrate_system_events_test.go`.

### Webhook deliveries (`WebhookDelivery`, issue #622)

`webhook_deliveries` is the per-attempt receipt log for outbound webhooks: event type, status code,
error, retry state, and — before this ticket — a **full plaintext copy of the serialized entity that
triggered the event**. A `contact.created` delivery carried the whole contact record (names, emails,
phones, addresses). Surfaced by the #510 privacy/data-minimization review as the one high-volume table
#414's per-table window pattern missed.

- **Where / who**: `webhook_deliveries` table, reachable only via the owning webhook's owner session
  and admin (`webhook_controller.go`'s delivery-list endpoint is scoped to the webhook the
  authenticated user owns). The `payload` column is **never exposed by the API** — it exists only for
  retry replay, so trimming it is invisible to the UI.
- **Retention**: `WEBHOOK_DELIVERY_RETENTION_DAYS` (default 30, `config/config.go`), anchored on
  `created_at` — the moment of the delivery attempt. A row older than the window is far past every
  retry a webhook can take (max 3 attempts across a ~20-minute backoff), so the purge being the last
  word is a retention decision, not a lost delivery.
- **Payload minimization (the deliberate trim)**: a successful (2xx) delivery stores only the event
  envelope (`id`/`event`/`timestamp`) — `trimSuccessfulDeliveryPayload`
  (`backend/services/webhook_service.go`) drops the entity body because a 2xx row is never re-sent and
  the full body would serve no purpose. Failed/retrying rows keep the full body because
  `ProcessWebhookRetries` replays it verbatim. So the PII copy is now bounded *and* minimized: fresh
  failures may hold the body for re-send, successful receipts never do.
- **Deletion / propagation**: `PurgeExpiredWebhookDeliveries`
  (`backend/services/webhook_delivery_purge_service.go`) hard-deletes rows past the window, daily via
  cron (`backend/main.go`) under the `webhook_delivery_purge` job lock, and via the admin `TriggerPurge`
  endpoint. `WEBHOOK_DELIVERY_RETENTION_DAYS<=0` is treated as "disabled", never "delete everything".
  Account deletion and webhook deletion still cascade as before.
- **Backups**: yes — a delivery (including any still-untrimmed failed row) sits in every snapshot
  taken before the purge; restoring such a backup resurrects it.
- **Verification**: `backend/services/webhook_delivery_purge_service_test.go`
  (`TestPurgeExpiredWebhookDeliveries_*` — window-pinned, failed-row policy, disabled-when-zero,
  idempotent, scheduled-lock) plus the trim pins in `backend/services/webhook_delivery_test.go`.

## 4. Sessions & short-lived secrets

Session/JWT cookies, TOTP recovery codes, password-reset tokens, API tokens.

- **Where / who**: httpOnly cookie (session) or DB row (API tokens, hashed); never in `localStorage`
  (pinned by the #419 Playwright regression, `frontend/e2e/`).
- **Retention**: token expiry per type (session TTL, reset-token TTL, recovery-code single-use — deleted
  in the same `WHERE` that consumes them, `services/twofactor.go:171-175`). API tokens live until
  revoked/rotated (issue #413).
- **Deletion / propagation**: logout clears the cookie server-side and `USER_INFO_KEY` client-side
  (`frontend/src/auth.ts:172`); revoke-all/rotate invalidate DB rows immediately.
- **Backups**: API token hashes yes; session cookies no (never persisted server-side to begin with).

## 5. Attachments & profile photos (files on disk)

- **Where / who**: `PROFILE_PHOTO_DIR` / `ATTACHMENTS_DIR` on the host filesystem, one file per
  attachment/photo, filenames server-generated (never user-controlled — path-traversal-safe).
- **Retention**: the metadata row (`Attachment`) follows §1's soft-delete/purge window, **but the file
  itself is deleted immediately**, not window-delayed (N7, comment at
  `backend/services/purge_service.go:48-49`): "an attachment's content IS the file; a tombstone that
  outlives a vanished file is acceptable for the change feed, an orphaned file is a leak."
- **Deletion / propagation**: `DeleteAttachment` (`backend/controllers/attachment_controller.go:244-263`)
  and `DeleteContact`'s `deleteContactPhotos`/`deleteContactAttachmentFiles`
  (`contact_controller.go:888-939`) remove the file from disk right after the DB transaction commits (file
  deletion can't be rolled back, so it happens after, not inside, the transaction). A failed file removal
  is logged as a leak but never fails the request.
- **Backups**: yes, if the operator's backup includes the photo/attachment directories — `docs/
  deployment.md`'s Backups section is explicit that a DB-only backup is *not* a complete backup for
  exactly this reason, and ships `rsync` commands for both directories alongside `make backup`.

## 6. Full-text search index (SQLite FTS5)

`contacts_fts`, `notes_fts`, `activities_fts` — derived, rebuildable indexes
(`services.RebuildSearchIndex`), not a second source of truth.

- **Where / who**: same SQLite file, `user_id UNINDEXED` column scopes every query to one user without a
  join.
- **Retention**: zero lag past the base table — `AFTER UPDATE`/`AFTER DELETE` triggers on `contacts`/
  `notes`/`activities` remove the FTS row the instant `deleted_at` is set (soft delete) or the row is hard
  deleted (`backend/database/migrations/000007_search_fts5.up.sql`,
  `000020_search_phones.up.sql`). A soft-deleted row is therefore unsearchable immediately, not merely
  after the purge window.
- **Backups**: yes, but irrelevant — a restore always includes the matching FTS state for that snapshot
  (same file), and a corrupted/missing index is always rebuildable from the base tables.

## 7. CardDAV / CalDAV (this app as the *server*)

External DAV clients (phones, desktop DAV apps) sync against `backend/carddav`, `backend/caldav`.

- **Where / who**: served straight from the live `contacts`/`activities`/`life_events` tables — no
  separate DAV-side copy exists.
- **Retention**: matches §1 exactly — a DAV client sees whatever the primary table currently shows.
- **Deletion / propagation**: `go-webdav` v0.7.0 (this project's DAV library) has **no RFC 6578
  sync-collection REPORT support** — there is no incremental delta protocol to push a tombstone through.
  Instead, `ListAddressObjects`/`ListCalendarObjects` query with GORM's default scope
  (`backend/carddav/backend.go:266-284`, no `.Unscoped()`), which — like every other query in this
  codebase — silently excludes soft-deleted rows. A deleted contact/event simply stops appearing in the
  *next* full listing a client requests (PROPFIND/REPORT), which is how these clients already detect
  removal without a native delta protocol. `DeleteAddressObject` (`backend/carddav/backend.go:385-413`)
  performs the same soft delete a REST client would, so a delete initiated *from* a DAV client propagates
  back into §1's normal lifecycle (undo window, purge, etc.) identically.
  - **Practical implication**: propagation isn't "instant" in the push sense (there is no push) — it is
    "as fresh as the client's next poll", which is the existing, already-shipped design (see the ETag
    comment at `backend/caldav/backend.go:314`), not a gap this ticket introduces or needs to close.
- **Backups**: not a separate store — see §1.

## 8. Android offline mirror (Room)

- **Where / who**: `AppDatabase`, SQLCipher-encrypted end to end (issue #385,
  `android/core/data/src/main/kotlin/.../local/RoomCacheEncryption.kt`), keyed via Android Keystore
  (`RoomPassphraseStore.kt`). Local to the device; unreadable without the app's key even with root file
  access.
- **Retention**: a rebuildable cache, not a second source of truth — rows persist only until the next
  sync disagrees with them.
- **Deletion / propagation**: `ContactRepositoryImpl.applySync` (`android/core/data/src/main/kotlin/.../
  repository/ContactRepositoryImpl.kt:248-256`) reads the T17 `sync.incremental` id list from every list
  response and calls `dao.deleteByIds(ids)` — the same tombstone mechanism §1/§7 rely on, consumed
  correctly here too. A direct on-device delete (`deleteContact`) also removes the cached row immediately
  on success (`ContactRepositoryImpl.kt:137-141`). On logout or an invalidated session,
  `LocalDataCleaner.clear()` (`android/core/data/src/main/kotlin/.../local/LocalDataCleaner.kt`) calls
  `AppDatabase.clearAllTables()` **and** deletes `context.cacheDir` recursively — wiping the FTS4 index,
  Coil's photo disk cache, and the vCard-share `FileProvider` staging area together, so a "stolen device
  after logout" story leaves nothing recoverable outside the (encrypted) DB the OS itself controls.
- **Backups**: none — this is a device-local cache with no server-visible backup; Android's own
  Auto Backup is out of scope for app-internal DB files of this kind and isn't configured for it.

**Android app-lock preference (issue #722).** The opt-in "require biometric / device PIN to open the
app" flag and its grace-timeout live in a small `local_auth_prefs` DataStore file
(`android/core/data/src/main/kotlin/.../repository/LocalAuthSettingsRepositoryImpl.kt`). That file holds
no contact data and no credential — the session JWT copy is unchanged (still `EncryptedTokenStorage`),
and the app-lock is a gate in front of it, not a second copy of it. The OS biometric prompt stores
nothing in the app (templates live in the device's own secure hardware); the last-background timestamp
that drives the relock grace period is held in memory only and dies with the process. Clearing the
app's data (or a logout, which wipes the Room mirror per §8) leaves nothing extra behind.

## 9. Browser-side storage (frontend SPA)

- **`localStorage`**: holds only `user_info` (id/username/admin flag/self-contact UID) and UI
  preferences — no auth token, no contact PII (`frontend/src/auth.ts:15,120`; pinned as a regression by
  the #419 Playwright spec asserting no long-lived credential ever lands in `localStorage`/
  `sessionStorage`/`IndexedDB`/URL). Cleared on logout (`auth.ts:172`).
- **Auth token**: httpOnly session cookie — never readable by JS or the service worker, so there is
  nothing for either to leak or need to clear.
- **Service worker cache**: `frontend/src/service-worker.ts` precaches only the static build (app shell)
  and runtime-caches same-origin `.png` requests (CRA's default scaffold route, `StaleWhileRevalidate`,
  LRU-capped at 50 entries). It never intercepts API calls — contact photos ship as base64 data URIs
  *inside* JSON API responses (`models/contact_record.go`'s `buildMedia`/`photostore`), not as separate
  image requests, so no contact PII ever passes through this cache. **No IndexedDB usage anywhere in the
  frontend.**
- **Backups**: not applicable — device-local, ephemeral, not part of any server backup.

## 10. Backups

- **Where / who**: operator-controlled, off-instance storage the operator chooses (`docs/deployment.md`
  Backups section). `make backup` (`backend/cmd/backup/main.go` → `database.BackupSnapshot`,
  `backend/database/backup.go`) takes a live `VACUUM INTO` snapshot — safe under WAL, verified with
  `PRAGMA integrity_check` before it's considered valid — of the SQLite file; photos/attachments are
  backed up separately (`rsync`, since they're plain files, not DB rows). Access is operator access:
  whoever can read the filesystem/backup store the snapshot lands in can read it, and the app itself
  never re-reads a backup file (the restore drill only ever creates its own fresh snapshot, in a
  scratch directory it deletes afterwards).
- **Automatic pre-migration snapshots (issue #530)**: separately from operator-run `make backup`,
  `database.InitDB` (server startup) and `make migrate-up` take a `VACUUM INTO` snapshot of the
  SQLite file *before* every schema migration and refuse to migrate if they cannot write one
  (`backend/database/premigration_backup.go`). They land in a `pre-migration/` directory beside
  `SQLITE_DB_PATH` (`MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` moves it), named
  `<db-stem>-pre-migration-<from>-to-<to>-<timestamp>.db`. Same content, sensitivity, and at-rest
  encryption inheritance as an operator backup (below) — but **database only**, no photos or
  attachments, since migrations don't touch those. Retention is operator-owned exactly like the
  rest of §10: the app writes these and never deletes them, so a rotation cron must exclude the
  `pre-migration/` directory (it holds the rollback points a bad-release recovery depends on —
  `docs/operations/migration-recovery.md`).
- **Confidentiality / encryption**: a snapshot is a **complete copy of sensitive data at full
  sensitivity** (issue #420) — `private`/`secret` fields, email addresses, password/API-token
  hashes, TOTP recovery-code hashes, the audit trail, and still-in-window soft-deleted rows. It
  inherits the DB's field-level at-rest encryption: encrypted columns travel as `encv1:` ciphertext
  and the wrapped DEK (`data_encryption_keys`) travels with them, but it carries the same
  FTS-plaintext exception as the live DB, and the photos/attachments directories are plaintext
  files. The master key (`DATA_ENCRYPTION_KEY`, else HKDF-derived from `JWT_SECRET_KEY`) is **never
  in the backup** — a stolen backup alone yields ciphertext + hashes + the FTS-plaintext set, and a
  restore under a different key fails closed at boot. A JWT-derived master key means rotating
  `JWT_SECRET_KEY` makes old snapshots undecryptable; a dedicated `DATA_ENCRYPTION_KEY` decouples
  them. Full operator statement: `docs/deployment.md`'s "Backup confidentiality & retention".
- **Retention**: **entirely operator-owned** — this app has no built-in backup rotation, expiry, or
  auto-deletion of old backup files, and deliberately won't (an app that can expire backups gives an
  attacker running as the app the same power — issue #505). A cron-scheduled `make backup` will
  accumulate snapshots forever unless the operator adds their own retention (e.g. a
  `find -maxdepth 1 -mtime +N -delete` outside this app — non-recursive so it never reaches the
  `pre-migration/` rollback points).
- **Immutability** (issue #505): the app's backup-write surface (`database.BackupSnapshot`, behind
  `make backup` and the automatic pre-migration snapshot) is **write-new-only** — it refuses to
  overwrite, removes only the temp it reserved, and no code path anywhere deletes, truncates,
  re-encrypts, or rotates an existing backup or pre-migration snapshot. On-host that is a barrier
  against the app's *own bugs* only; the app's uid can still `rm` its own files. Immutability against
  a **compromised host** (ransomware, code-exec as the app) is achieved by the operator putting
  backups where the app holds no credential to reach them — a pull-based off-host copy (the
  documented default) or object-locked remote storage — so a compromise of the app host cannot reach
  what is already off it. Runbook + verify-by-trying: `docs/deployment.md` → "Backup immutability &
  ransomware resistance".
- **Deletion / propagation**: **does not happen automatically, ever** — deleting/purging live data has no
  effect on already-taken backup files. This is the one place in the whole lifecycle where "deletion
  propagates" is false by design, and `docs/deployment.md`'s Restore section already documents the
  direct consequence: restoring a backup **resurrects** anything that was soft-deleted (but not yet purged) as
  of that snapshot, since a file-level restore has no concept of "these rows were mid-undo-window".
  Soft-deleted data ages out of backups in two steps: it stops appearing in *new* snapshots once the
  purge window (default 30 days, `DELETE_RETENTION_DAYS`) passes, but it survives in every snapshot
  that predates that purge until the operator deletes the snapshot. There is no partial/selective
  restore.
- **Restore-environment security**: a restore is a point-in-time rollback; the automated restore
  drill (`backend/services/restore_drill_service.go`, issue #275) proves a snapshot restores into a
  scratch DB with matching row counts — and, since issue #420, that the snapshot's wrapped DEK
  unwraps under the current master key (`atrest.VerifyBackupDecryptable`), so a rotated/lost key is
  caught weekly in a throwaway DB rather than during a real disaster. The audit trail in a restored
  database is only as fresh as the snapshot.
- **Backups of backups**: N/A — recursion stops here by definition.
- **Verification**: `frontend/e2e/backupRestore.spec.ts` (full backup → destroy → restore round trip);
  `backend/services/restore_drill_service.go` + issue #275 (scheduled restore-drill job that proves a
  backup actually restores, not just that the file exists); `atrest.VerifyBackupDecryptable` +
  `restore_drill_service_test.go` (`TestRestoreDrillFailsWhenSnapshotNotDecryptable`,
  `TestRestoreDrillPassesWithEncryptedDatabase`, issue #420); `backend/database/backup_immutability_test.go`
  (issue #505 — write-new-only proven by trying overwrite/in-place-modify/neighbour-delete through the
  app's own primitive; a source walk fails on any in-app backup expiry/rotation function; the #530
  pre-migration rollback point survives a non-recursive routine rotation sweep).

## 11. Exports (CSV / vCard3 / vCard4 / jSContact / audit log)

- **Where / who**: generated per-request and streamed directly to the HTTP response
  (`backend/controllers/export_controller.go`) — never written to disk server-side (no `os.WriteFile`
  anywhere in the export path). Once downloaded, the file is the requesting user's own device/browser —
  outside this app's retention control by definition (same boundary as any file a user saves from any
  web app). Issue #416 added a fifth export in this same category: `GET /audit/export`
  (`ExportAuditLog`), an unbounded CSV of the caller's own audit trail — same generation/no-server-copy
  shape as the other four.
- **Retention**: nothing server-side to retain.
- **Deletion / propagation**: nothing to delete — there is no export artifact that outlives the request.
  Sensitive-above-`normal` fields and CSV-formula-injection payloads are filtered/neutralized *before*
  the export leaves the server (`sensitivity` classification, ASVS 8.3.4). The audit-log export's
  `before_snapshot` column is omitted unless the caller explicitly passes `?include_snapshots=true`: it
  is already credential-redacted at write time (`auditDenyList`, `models/audit.go`) but is **not**
  filtered by contact-field sensitivity the way the other four exports are, so it is gated behind its own
  explicit opt-in rather than reusing `include_sensitive`.
- **Auditability (issue #444)**: a *sensitive* export (`?include_sensitive=true`) is **not**
  individually recorded in the audit log — a documented decision, not an oversight. The export handlers
  are strictly read-only: they never write to any audited entity, so an export leaves no audit row today,
  and the `include_sensitive` opt-in is not logged anywhere. Rationale, deliberately narrow: the audit
  log exists for entity-CRUD and auth-lifecycle events, exports are the caller's own data leaving to the
  caller's own device, and every export (sensitive or not) is already visible to the operator via the
  structured request log. The day an export is made to *move data to another party* (contact shares do
  exactly this via the same `IncludeSensitive` flag), the cross-user path is treated as the higher bar —
  see `pii-inventory.md` and ASVS 1.5.1's share coverage — and a future re-evaluation would add an
  audit op token (requiring a migration, the audit `CHECK` constraint, `openapi.yaml`, the API baseline,
  and the frontend mirror) rather than an unconstrained row.
- **Backups**: not applicable.

## 12. Import wizard staging

- **Where / who**: `ImportSessionManager` (`backend/services/import_session.go`) — **in-memory only**,
  never persisted to disk or DB.
- **Retention**: `sessionExpiry = 15 * time.Minute` (`import_session.go:18`). A tiny post-confirm
  tombstone (`confirmedImport`) survives a little longer so a client that lost the confirm response can
  retry idempotently, but it holds only the result/owner/expiry, not the original uploaded rows.
- **Deletion / propagation**: expires on its own timer; also fully lost on process restart (in-memory,
  by design — an import mid-flight during a restart must be redone, which is an acceptable trade for
  never writing uploaded-but-unconfirmed contact data to disk).
- **Backups**: never — can't be backed up if it was never persisted.

### 12a. Monica import assistant session (issue #549)

- **Where / who**: `MonicaImportManager` (`backend/services/monica_import_session.go`) — **in-memory
  only**, one session per active Monica import wizard. Unlike §12 it holds a *third-party credential*:
  the user's Monica **API token**, kept on the `*monica.Client` for the session's lifetime so the
  background fetch can page the Monica API. Also holds the fetched account snapshot and the mapped
  review plan.
- **Retention**: sliding `monicaSessionExpiry = 60m` (longer than §12 because the rate-limited fetch
  alone can take minutes), hard-capped at `monicaSessionMaxLifetime = 6h`. Expiry slides on every
  status/preview poll; a forgotten browser tab still ages the session — and the token — out.
- **Deletion / propagation**: dropped on `cancel`, on expiry, and on process restart. The token is
  **never logged** (a test asserts it never appears in the structured log) and **never persisted** —
  there is no config row, no DB column, no file. Avatars fetched with it during `processAvatars`
  become ordinary contact photos (covered by the contact-photo lifecycle, §1) once the import commits;
  the token itself is not stored alongside them.
- **Backups**: never — nothing is persisted.

### 12b. Meerkat import assistant session (issue #550)

- **Where / who**: `MeerkatImportManager` (`backend/services/meerkat_import_session.go`) — an
  in-memory session **plus the uploaded Meerkat SQLite file on disk**: `os.MkdirTemp` +
  `meerkat.sqlite` written `0600` under the OS temp dir, one directory per session. The file is a
  copy of the user's own Meerkat CRM database; the reader (`backend/meerkat/reader.go`) opens it
  `mode=ro`, validates the SQLite magic header first, and never writes/migrates/executes anything
  from it (hostile-input handling; coordinates with #432/#415). The parsed snapshot and mapped
  review plan are held in memory.
- **Retention**: sliding `meerkatSessionExpiry = 60m`, hard-capped at
  `meerkatSessionMaxLifetime = 6h`; the expiry slides on every status/preview poll.
- **Deletion / propagation**: the temp directory is `os.RemoveAll`'d on `cancel`, on expiry
  (`CleanupExpired`), and whenever the session is dropped; a process restart loses the in-memory
  session and the OS reclaims the temp dir. Nothing about the upload is written to the application
  database. Imported contacts follow §1's lifecycle once the import commits.
- **Backups**: never — the temp file lives outside any backed-up path and is deleted with the
  session.

## 13. External integration credentials (WebDAV / Paperless / Immich / Seafile)

- **Where / who**: one config row per user per integration, app-password/API-key encrypted at rest
  (`services/credential_crypto.go`); the plaintext credential is never returned by the read endpoint.
- **Retention**: soft-delete with the T26 partial-unique-index pattern (a user may remove and
  re-add a connection without a soft-deleted ghost blocking it).
- **Deletion / propagation**: hard-deleted on account removal (`admin_user_controller.go:705-720`).
  **Deleting the connection here never deletes anything in the external service** — Immich/Paperless/
  Seafile/WebDAV content lives entirely under the user's own account on their own external service; this
  app only ever stores a reference/credential, never a durable mirror of that content. That boundary is
  deliberate, not a gap: this app has no authority to delete data the user manages in a separate product.
- **Backups**: only the encrypted credential row; the external content is that service's own backup
  story.

## 14. Operational self-check results (`operational_check_results`) — not user data

- **Where / who**: one row per named self-check (`db_integrity_check`, `restore_drill`, and an
  internal `_db_write_probe`), written by the scheduled jobs and read by the deep `GET /health`
  endpoint (issue #421). No `user_id` — it is server-global operational bookkeeping, like
  `job_executions`.
- **What it contains**: a check name, an `ok`/`failed`/`error` status, a timestamp, and a short
  detail string (an `integrity_check` problem line, or a restore-drill row-count mismatch such as
  `contacts: live=10 restored=9`). **No contact data, no credentials, no PII.**
- **Retention / deletion**: hard state, upserted in place — at most one row per check name, each
  overwritten on the next run. Not tied to any user, so account deletion neither touches nor needs
  to touch it. Dropped wholesale by migration `000038`'s `down.sql`.
- **Backups**: included in the DB snapshot like any other table; carries nothing sensitive, so it
  needs no special handling in the backup-confidentiality boundary (§10).

## 15. Alert state (`alert_states`) — not user data

- **Where / who**: one row per alert condition (`backup`, `disk_space`, `sync:contact_sync`, …),
  written by the scheduled alert evaluator (`alert_eval`, issue #428) and read only by that same
  job. No `user_id` — server-global operational bookkeeping, like `operational_check_results` and
  `job_executions`.
- **What it contains**: a condition key, an `ok`/`alerting` state, the timestamp it entered that
  state, a consecutive-failure count, and a short sanitized detail string (e.g.
  `disk usage 92% of /var/lib/mycorrhizal`, or a subsystem's last error as already sanitized into
  `system_events`). **No contact data, no credentials, no PII.**
- **Retention / deletion**: hard state, upserted in place — at most one row per condition, each
  overwritten on the next evaluation. Not tied to any user, so account deletion neither touches nor
  needs to touch it. Dropped wholesale by migration `000040`'s `down.sql`; the next evaluator run
  rebuilds every row from current subsystem health.
- **Backups**: included in the DB snapshot like any other table; carries nothing sensitive, so it
  needs no special handling in the backup-confidentiality boundary (§10). Excluded from the
  restore-drill row-count comparison for the same reason as `system_events` /
  `operational_check_results` — the evaluator writes it in the snapshot-vs-live window.

## 16. Background job runs (`job_runs`) — not user data

- **Where / who**: one row per scheduled-job invocation (`daily_reminders`, `calendar_sync`,
  `reach_out_detection`, the purge jobs, …), written by `main.go`'s job wrapper and read only via
  an admin API session (`GET /admin/job-runs`, `GET /admin/job-runs/health`, issue #391). No
  `user_id` — server-global operational bookkeeping, like `job_executions` /
  `operational_check_results` / `alert_states`. Not in any CardDAV/CalDAV projection, not in the
  Android offline mirror.
- **What it contains**: job name, trigger (`scheduled`/`initial`/`manual`), start/finish timestamps,
  duration, result (`success`/`failure`/`skipped`), an optional items-processed count, a short
  `detail` string, and the `correlation_id`. The free-text `error`/`detail` fields are sanitized +
  length-capped by `models.RecordJobRun`; the model carries no high-cardinality fields (no contact
  IDs, no raw URLs) by construction — same posture as `system_events`.
- **Retention**: `JOB_RUN_RETENTION_DAYS` (default 30, `config/config.go`) — short by design, same
  reasoning as `SYSTEM_EVENT_RETENTION_DAYS`: long enough to see a slow-creep duration trend, short
  enough to bound growth on a single-file database.
- **Deletion / propagation**: `PurgeExpiredJobRuns`
  (`backend/services/job_run_purge_service.go`) hard-deletes rows whose `started_at` is older than
  the window, daily via cron (`backend/main.go`) under the `job_run_purge` job lock.
  `JOB_RUN_RETENTION_DAYS<=0` is treated as "disabled", never "delete everything". Not tied to any
  user, so account deletion neither touches nor needs to touch it. Dropped wholesale by migration
  `000041`'s `down.sql`.
- **Backups**: included in the DB snapshot like any other table; carries nothing sensitive, so it
  needs no special handling in the backup-confidentiality boundary (§10).
- **Verification**: `backend/models/job_run_test.go`, `backend/services/job_run_health_test.go`,
  `backend/services/job_run_purge_service_test.go`, `backend/database/migrate_job_runs_test.go`,
  `backend/main_test.go` (`TestRunJob_RecordsOutcome`).

## 17. Import run history (`import_runs`) — user-scoped operational bookkeeping

- **Where / who**: one row per **confirmed** contact import (issue #651), written by
  `models.RecordImportRun` from `ImportSessionManager.Confirm` / `.ConfirmVCF` after the import
  transaction commits, and read only by its owner via `GET /api/v1/contacts/import/history`
  (newest 50). Unlike §14–§16, it **is** user-scoped: `import_runs.user_id` is the user who ran the
  import. Distinct from §12 (the in-memory wizard staging that holds the *uploaded rows*) — this is
  the durable *outcome summary* only.
- **What it contains**: the source format token (`csv` / `vcf` / `jscontact` / `records`), the five
  ImportResult counts (total processed / created / updated / skipped) and an error **count** — no
  error strings, no contact names, no field values, **no PII**. It cannot answer "which contact"
  or "what changed", only "how many, when, from what format".
- **Retention**: no dedicated purge job. An import is a rare, human-initiated action, so the table's
  growth is self-bounding (a handful of rows per user per year); the read endpoint caps its own
  output at 50. Rows are immutable once written — no update path.
- **Deletion / propagation**: hard-deleted with the rest of the account by `DeleteUser`
  (`admin_user_controller.go`, `Unscoped().Where("user_id = ?", …).Delete(&models.ImportRun{})`) —
  it is in the manual-cascade checklist and pinned by `controllers/delete_cascade_coverage_test.go`
  (bucket `go-cascade-user`). Not in any CardDAV/CalDAV projection, not in the Android offline
  mirror. Dropped wholesale by migration `000042`'s `down.sql`.
- **Backups**: included in the DB snapshot like any other table; carries nothing sensitive, so it
  needs no special handling in the backup-confidentiality boundary (§10).
- **Verification**: `backend/database/migrate_import_runs_test.go`,
  `backend/services/import_session_history_test.go`,
  `backend/controllers/import_history_controller_test.go`,
  `backend/controllers/delete_cascade_coverage_test.go` (`import_runs` seeded + swept in the
  DeleteUser sweep).

## 18. Prometheus metrics (`GET /metrics`, in-process) — not user data, not persisted

- **Where / who**: process memory only. A hand-rolled registry (`backend/metrics/`) holds counters
  and gauges that the middleware, the scheduled-job wrapper, and `models.RecordSystemEvent` update
  on the hot path; the values are rendered on demand at `GET /metrics` (issue #389). The endpoint
  is registered only when `METRICS_TOKEN` is set and every scrape is bearer-authenticated.
- **What it contains**: aggregate operational numbers with **bounded-cardinality labels only** —
  HTTP method / matched route *template* / status code, background-job name + result,
  `system_events` type/component/result, DB connection-pool counts, Go-runtime stats, and storage
  byte totals (SQLite file size, filesystem free/total). No `user_id`, no contact IDs, no raw
  request paths, no free text, no credentials. The route label is `c.FullPath()` (the registered
  template, never the concrete path), so an unbounded ID space cannot leak in.
- **Retention / deletion**: none — the counters live only in RAM, start at zero on every process
  start, and are never written to the database or any file. Account deletion neither touches nor
  needs to touch them. Restarting the container is a full reset.
- **Backups**: not applicable — nothing is persisted, so nothing is in a snapshot.

## 19. Update-availability check (`UPDATE_CHECK_ENABLED`) — outbound call, sends nothing

- **Where / who**: an opt-in, admin-triggered outbound `GET` to the GitHub releases API
  (`https://api.github.com/repos/DrewBrunning/mycorrhizal-crm/releases/latest`, issue #650), made at
  most once per 6h (memoized in process memory, `backend/services/update_check.go`) when the admin
  system-status endpoint is read and `UPDATE_CHECK_ENABLED` is set. Off by default — see
  `docs/security/asvs-l2.md`'s P6 for the position.
- **What it contains**: nothing sent. The request carries no user data, no PII, no config — the body
  is empty and only an `Accept`/`User-Agent` header goes out. The response's `tag_name` is compared
  against the running build's version; the tag string is the only thing held in memory (cached 6h).
- **Retention / deletion**: nothing to delete — the fetched tag lives in a process-memory cache and
  vanishes on restart. No database rows, no files.
- **Backups**: not applicable — nothing is persisted, so nothing is in a snapshot.

## 20. Storage-growth history (`storage_samples`) — not user data

- **Where / who**: one row per daily sampling of the on-disk footprint (issue #652), written by the
  `storage_sample` scheduled job (`services/storage_sample_service.go`) and read only via the admin
  `/admin/system-status` surface. No `user_id` — instance-wide operational bookkeeping, the same
  shape as `job_runs` / `system_events`. The `alert_states` row keyed `storage_threshold` (the
  hysteresis state behind the tiered threshold) is §15's table, not this one.
- **What it contains**: `taken_at` plus five byte counts — database (main file + `-wal` + `-shm`),
  filesystem used/total, profile-photo directory, attachment directory. **No contact data, no
  credentials, no PII.**
- **Retention / deletion**: `STORAGE_SAMPLE_RETENTION_DAYS` (default 180, `config/config.go`) — the
  sampler deletes rows older than the window in the same scheduled run that writes, so the table is
  bounded by construction. `<= 0` is treated as "use the default", never "delete everything". Hard
  delete (no `deleted_at`, CLAUDE.md backend trap #7). Not tied to any user, so account deletion
  neither touches nor needs to touch it. Dropped wholesale by migration `000043`'s `down.sql`.
- **Backups**: included in the DB snapshot like any other table; carries nothing sensitive, so it
  needs no special handling in the backup-confidentiality boundary (§10). Excluded from the
  restore-drill row-count comparison for the same reason as `system_events` / `job_runs` — the
  sampler writes it in the snapshot-vs-live window.
- **Verification**: `backend/database/migrate_storage_samples_test.go`,
  `backend/services/storage_sample_service_test.go`,
  `backend/controllers/system_status_controller_test.go` (`TestGetSystemStatus_StorageTrendBlock`).

## 21. Import source links (`import_source_links`) — user-scoped source-import ledger

- **Where / who**: one row per entity produced by a *source* import — a Meerkat database or a Monica
  snapshot (ADR 0007, issues #351/#353) — written by `services.ExecuteSourceImport`
  (`backend/services/import_source.go`) inside the same transaction that creates the mapped rows, and
  read back on the next run of the same import. Keyed by `(system, external_id, user_id)`, it is the
  idempotency ledger for source imports (CON-04, issue #459): a row already present means "already
  imported" and is skipped. `user_id` is the importing user; every read/write is owner-scoped
  (CLAUDE.md trap #5). Distinct from §12 (the in-memory wizard staging that holds *uploaded rows*) and
  §17 (`import_runs`, the durable *outcome summary* of file imports) — this is the per-entity *source
  identity* ledger that makes re-runs non-duplicating.
- **What it contains**: the source system token (`system`), the source row's own identity namespaced
  per entity kind (`external_id`, e.g. `contact/7`), the local entity kind and identity (`entity_kind`
  ∈ contact|relationship|household|circle|tag|gift|preference|activity|note|reminder; `entity_uid` — a
  VCardUID for contacts, the UUID PK for UUID-PK entities, `id:<n>` for uint-PK rows), and the
  importing `user_id`. **No contact field values, no free-text content** — it records *which* source
  rows were mapped to *which* local rows, not what they said.
- **Retention**: no dedicated purge job. A source import is a rare, human-initiated action, so the
  table's growth is self-bounding (a handful of rows per imported source row, written once). Rows are
  immutable once written — no update path; a re-run only *reads* them to skip. No CardDAV/CalDAV
  projection and no Android offline mirror.
- **Deletion / propagation**: hard-delete (no `deleted_at`, per ADR 0004's edge/join-row class and the
  migration's own doc comment) — a link is a ledger fact, not user-authored content with an undo
  button. Removed with the account by `DeleteUser` via the `ON DELETE CASCADE` FK to `users` (the
  `fk-cascade-user` bucket in `controllers/delete_cascade_coverage_test.go`). Dropped wholesale by
  migration `000045`'s `down.sql` (documented there as destroying re-import idempotency, not user data).
- **Backups**: included in the DB snapshot like any other table; carries source identifiers and no
  sensitive content, so it needs no special handling in the backup-confidentiality boundary (§10).
- **Verification**: `backend/services/import_source_test.go`
  (`TestExecuteSourceImport_IsIdempotent`, `TestExecuteSourceImport_ScopesByUser`,
  `TestExecuteSourceImport_DuplicateSourceRefSkipsSecond`,
  `TestExecuteSourceImport_AlreadyImportedGraphSkipped`), `backend/services/monica_import_test.go`
  (`TestMonicaImport_ReRunIsIdempotent`), `backend/services/meerkat_import_test.go`
  (`TestMeerkatImport_ReRunIsIdempotent`), `backend/controllers/delete_cascade_coverage_test.go`
  (`import_source_links` seeded + swept in the DeleteUser sweep, bucket `fk-cascade-user`).

## 22. Idempotency keys (`idempotency_keys`) — user-scoped retry-replay bookkeeping

- **Where / who**: one row per `(user_id, idempotency_key)`, claimed by
  `middleware.IdempotencyMiddleware` on the first authenticated `POST` that carries an
  `Idempotency-Key` header (CON-04, issue #459, ADR 0010). `user_id` is the calling user; the middleware
  reads/writes only that user's own rows. Distinct from §21 (`import_source_links`, the per-source-entity
  import ledger) — this is the general inbound-mutation retry guard for the REST API.
- **What it contains**: the opaque client key, the request `method` + route template `path`, a
  SHA-256 `request_fingerprint` over method+path+body (to reject a key reused for a *different*
  request), a `state` (`pending` → `completed`), and — on a 2xx — the stored `response_status` and
  `response_body` that later retries replay verbatim. `response_body` is a copy of the handler's
  response, so for a create it holds the serialized new entity (the same class of copy as a
  `webhook_deliveries` payload, §18). No credentials; nothing the caller did not already send or
  receive.
- **Retention**: `IDEMPOTENCY_KEY_RETENTION_HOURS` (default **24**, `config/config.go`), anchored on
  `created_at` (when the key was claimed). `PurgeExpiredIdempotencyKeys` hard-deletes older rows on a
  6-hour job-locked cron (`services/idempotency_purge_service.go`), the same shape as the
  webhook-delivery purge (§18). The window is short by design: a key protects one client operation
  across its retries, which happen within seconds to minutes. `IDEMPOTENCY_KEY_RETENTION_HOURS <= 0`
  is treated as "disabled", never "delete everything". No CardDAV/CalDAV projection, no Android
  offline mirror.
- **Deletion / propagation**: hard-delete (no `deleted_at` — transient bookkeeping, ADR 0004's
  operational-row class). Removed with the account by `DeleteUser`'s explicit enumeration
  (`go-cascade-user` bucket in `controllers/delete_cascade_coverage_test.go`). Dropped wholesale by
  migration `000047`'s `down.sql` (documented there as losing only the dedup window, not user data).
- **Backups**: included in the DB snapshot like any other table; the `response_body` copy inherits the
  field-level at-rest encryption applied to the entities it echoes and is bounded by the 24h window,
  so it needs no special handling in the backup-confidentiality boundary (§10).
- **Verification**: `backend/services/idempotency_purge_service_test.go`
  (`TestPurgeExpiredIdempotencyKeys_RemovesOnlyExpired`,
  `TestPurgeExpiredIdempotencyKeys_NonPositiveRetentionDisables`,
  `TestPurgeExpiredIdempotencyKeysScheduled_JobLockGuards`),
  `backend/middleware/idempotency_test.go` (claim/replay/422/concurrent),
  `backend/controllers/delete_cascade_coverage_test.go` (`idempotency_keys` seeded + swept in the
  DeleteUser sweep, bucket `go-cascade-user`).

## Known gaps

One item surfaced by walking every data type through the four questions above. It does not block this
ticket's "at minimum" bullets (T26 purge + CardDAV/CalDAV + Android propagation are all already correct,
per §1/§7/§8), but it is a genuine, named gap rather than a silently-accepted one.

1. **API responses carry no `Cache-Control: no-store`.** Already tracked and accepted as `partial` at
   ASVS 8.1.1/8.2.1 (`docs/security/asvs-l2.md`) — noted here only because it's the one place a stale
   *cached* copy of contact PII could theoretically outlive a delete, on a shared/proxied browser. Not
   re-litigated here; that row already owns the gap and its own fix.

## Verification summary

| Data type | Retention pinned by test? |
|---|---|
| Soft-deleted rows / purge window | `backend/services/purge_service_test.go` (8 cases) |
| ContactShare snapshot purge window | `backend/services/contact_share_purge_service_test.go` (9 cases) |
| Audit retention + re-link | `backend/services/audit_purge_service_test.go` (3 cases) |
| System-event retention window | `backend/services/system_event_purge_service_test.go` |
| Webhook delivery purge window + payload trim | `backend/services/webhook_delivery_purge_service_test.go` (9 cases), `webhook_delivery_test.go` trim pins |
| Job-run retention window | `backend/services/job_run_purge_service_test.go` |
| Idempotency-key TTL window + job lock; swept on account delete | `backend/services/idempotency_purge_service_test.go` (3 cases), `backend/controllers/delete_cascade_coverage_test.go` |
| Import-run history: one row per confirmed import, swept on account delete | `backend/services/import_session_history_test.go`, `backend/controllers/import_history_controller_test.go`, `backend/controllers/delete_cascade_coverage_test.go` |
| Import source links: one row per imported source entity, idempotency ledger, swept on account delete | `backend/services/import_source_test.go`, `backend/services/monica_import_test.go`, `backend/services/meerkat_import_test.go`, `backend/controllers/delete_cascade_coverage_test.go` |
| Storage-growth history: one sample per run, pruned past retention | `backend/services/storage_sample_service_test.go`, `backend/database/migrate_storage_samples_test.go` |
| Admin purge trigger + window | `backend/controllers/admin_user_controller_test.go` M1/M1b/M5 |
| Sync-horizon 410 Gone matches purge window | `backend/controllers/cursor_feed_test.go` |
| FTS index follows soft/hard delete | `backend/database/migrate_test.go`, FTS trigger coverage |
| Android mirror wiped on logout | `LocalDataCleaner` — see Android test suite |
| Android mirror deletes tombstoned ids | `ContactRepositoryImpl` sync tests (`core/data/src/test/.../repository/`) |
| No PII/credential in browser storage | `frontend/e2e/` (#419 Playwright regression) |
| Backup restore actually restores | `frontend/e2e/backupRestore.spec.ts`, restore-drill job (#275) |
| Metrics counters are RAM-only, bounded labels, token-gated | `backend/metrics/` (`registry_test.go`, `metrics_test.go`), `backend/controllers/metrics_controller_test.go`, `backend/routes/metrics_route_test.go` |
