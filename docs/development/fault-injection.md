---
title: Failure Injection & Chaos
parent: Development
nav_order: 7
---

# Failure Injection & Chaos (TEST-06)

This page is issue #434's written-down deliverable: the split-harness rule, the
in-process injection mechanism, the external-fault CI job, and **the fault
catalog** — the table the `v0.8.0` adversarial audit (issue #500) reviews to
find the faults this list omits.

## The split-harness rule

There are two mechanisms, and a hard rule for choosing between them:

> **If the fault can be expressed as an error returned across an existing
> interface, inject it in-process. If it requires the process or the filesystem
> to actually misbehave, it belongs in the external-fault CI job.**

The failure mode of a split harness is people guessing which side a fault
belongs on. Don't. A fault that fits through an interface (a DB error
mid-transaction, a sentinel from an integration client) is injected
in-process via the `faults` package and tested in the normal Go suite. A fault
that genuinely needs the process to die or the disk to fill (SIGKILL
mid-migration, `ENOSPC` during backup) is driven by the external job
(`.github/workflows/chaos-tests.yml`), which is slow, few, and high-value.

## The in-process mechanism: `backend/internal/faults`

Faults are inert unless armed. Production code checks a fault at a real seam
with one `faults.Hook(name)` call and routes the returned error through its
existing error path — **that path is what the injection test asserts**. An
unarmed hook is a read-lock map lookup returning nil; it changes nothing.

Arming is programmatic (the test suite) or environment-driven (the external
job):

```bash
# A subprocess the test cannot reach from inside (the external job):
MYCORRHIZAL_FAULTS=fault.name,fault:err:message,fault:pause:5s
```

| Entry form | Effect |
|---|---|
| `name` | error fault, standard `injected fault: <name>` |
| `name:err:<text>` | error fault with custom text (may contain colons) |
| `name:pause:<duration>` | pause fault: block for the duration, log a greppable marker, return nil |

In-process, tests use `faults.ArmError(name, err)`, `faults.ArmPause(name, d)`,
`faults.Disarm(name)`, and `faults.Reset()` (in `t.Cleanup`). `errors.Is`
against the armed error (or `faults.ErrInjected`) is the portable assertion.

Seams are deliberately few and at real boundaries — the migration driver's
per-migration point, an import confirm's transaction, an integration client's
request path. They are not sprinkled through business logic. Where a listed
fault has no seam yet, adding one is the work of the ticket that needs it.

## The external-fault job

`.github/workflows/chaos-tests.yml` runs the faults that must actually break a
process or a filesystem. It is gated to its own `chaos` paths filter (see
`.github/filters.yaml`), so it does **not** run on every push — only when the
injection harness itself changes, on a nightly schedule, or on manual dispatch.

The external job does not observe "it errored"; it asserts **the outcome is
defined**: recovered, or failed closed with the data intact. `PRAGMA
integrity_check` passes on the database after every interruption it performs.

## Fault catalog

Each row names the fault, its seam, what it simulates, the **defined outcome**
that must hold, and where each outcome is pinned. `in-process` tests run in the
normal Go suite; `external` tests run in the chaos job. A fault with no row
here is a gap to file, never something to silently absorb.

| Fault (env name) | Seam | Simulates | Defined outcome | Pinned by |
|---|---|---|---|---|
| `database.migration.statement` | `database/sqlite_driver.go` `Run` | Process killed between a migration's commit and its clean-mark (crash leaves the version dirty, migration applied) | The run returns an error naming the failing version + file (issue #532 gate); the DB is left **dirty** at that version with the migration's schema applied; `PRAGMA integrity_check` is `ok`; the next startup run **refuses** on the dirty flag with a typed `ErrDirtyMigration` naming the recovery (MIG-04, issues #439/#546 — never a forced boot); the operator-only `MigrateForce` (the migrate CLI's prompted `force`) recovers deterministically to the latest schema, not dirty | in-process: `database/fault_injection_test.go` `TestInjectedMigrationFaultFailsClosedAndRecovers`, `...MidFlightRecordsEvent`, `...PauseBlocksThenCompletes`; `database/migrate_failclosed_test.go` `TestMigrateForce_*`; `database/interrupted_startup_test.go` `TestInterruptedStartupKillPoints/during_migration`, `TestInterruptedStartupCrashLoopConverges`; external: chaos job `migration-kill` |
| `database.migration.before_batch` | `database/migrate.go` `runPendingMigrations` (before `m.Up()`) | Process killed AFTER the mandatory pre-migration backup (issue #530) but BEFORE the first migration statement — the "before any migration begins" window (DEPLOY-03, issue #452) | The run returns an error; the schema is **completely untouched** — no version bump, no dirty flag, no partial DDL; `PRAGMA integrity_check` is `ok`; the pre-migration backup was already written and is a valid rollback point; a restart just **migrates normally**; repeated kill/restart cycles converge (the backup is reused, never rewritten) | in-process: `database/interrupted_startup_test.go` `TestInterruptedStartupKillPoints/before_migrations`, `TestMigrationBeforeBatchFaultLeavesSchemaUntouched`; external: chaos job `startup-interruption-kill-points` |
| `services.import.confirm` | `services/import_session.go` `Confirm` + `ConfirmVCF` transaction | DB error mid-import-confirm | The whole confirm **fails closed**: every contact row rolls back, the session stays unconsumed, a retry after the fault clears applies the same import cleanly — no silent partial state | in-process: `services/import_fault_injection_test.go` `TestConfirmInjectedDBErrorFailsClosed`, `TestConfirmVCFInjectedDBErrorFailsClosed`, `TestConfirmInjectedFaultDoesNotLeakToOtherSessions` |
| `services.immich.request` | `services/immich_client.go` `doRequest` | Unreachable / auth-expired / resource-deleted-remotely / arbitrary upstream failure | The armed sentinel crosses the request boundary unchanged; callers hit their documented error path (T42 classification, service degrade-to-cache), never a swallow or a panic; the seam is inert once disarmed | in-process: `services/immich_fault_injection_test.go` `TestImmichInjectedRequestFaultCrossesBoundaryUnchanged`, `...DoesNotPersistBeyondArm`, `...ReachesServiceDiagnostics` |
| `services.paperless.request` / `services.seafile.request` / `services.webdav.request` | `faultingRoundTripper` on each client's transport (`services/integration_fault_seam.go`) | Unreachable / auth-expired / resource-deleted-remotely upstream on any request the client makes | The fault presents as the client's `Err*Unreachable` class carrying the injected cause; never a swallow, panic, or success; a black-hole host is still cut off by the client timeout | in-process: `services/integration_failure_behavior_test.go` `TestIntegrationClient_InjectedFaultCrossesBoundaryUnchanged`, `...StatusMappingIsStable`, `...BlackHoleHostIsBounded`, `...ConnectionRefusedIsFast` |
| `services.contactsync.request` / `services.calendarsync.request` | `contactRoundTripper` / `calendarRoundTripper` `RoundTrip` (`services/{contact,calendar}_sync_service.go`) | A CardDAV/CalDAV request failure mid-scheduled-sync | The run records failure, releases its per-subscription mutex, leaves local contacts/activities/links untouched, advances `SyncHealthFields` (`ConsecutiveFailures`++, `IncidentFirstFailureAt`), and emits a `sync_failed` operational event. A failure classified **permanent** (401/403 → `auth-expiry`, 404/410 → `remote-resource-deleted`, via `services.classifySyncFailure`) additionally sets `TerminalFailureAt`/`TerminalReason` (INT-04 #467): `SyncAllCalendars` then **skips** the subscription until a success or a subscription edit clears it — the quiet-failure case is impossible by construction | in-process: `services/sync_failure_behavior_test.go` `TestContactSync_RequestFailureIsDefinedAndObservable`, `TestCalendarSync_RequestFailureIsDefinedAndObservable`, `TestSync_HungRemoteIsBoundedByContext`, `TestContactSync_PermanentAuthFailureIsTerminal`, `TestCalendarSync_PermanentFailureStopsScheduledRetries`, `TestCalendarSync_TerminalStateClearsOnRecovery`, `TestContactSync_TransientFailureIsNotTerminal`; `models/sync_health_test.go` `TestAdvanceForRunTerminalFailureLifecycle` |
| `services.webhook.delivery` | `services/webhook_service.go` `deliverWebhook` (before `client.Do`) | Outbound webhook POST fails | An *injected* fault takes the transient path: a `webhook_deliveries` row with the reason, `Attempts` incremented, `NextRetryAt` set per `webhookRetryPolicy` (exp backoff base 5m ×3, ±20% jitter, capped 6h) until `maxDeliveryAttempts`, then terminal with one `integration_failed` event. A **real permanent HTTP status** (401/403/404/410 or another request-level 4xx, via `integrations.DispositionForHTTPStatus`) is not retried at all — `failed_permanently`, `terminal_reason`, `NextRetryAt = NULL`, `integration_failed` on that attempt (INT-03 #466). `429`/`503` wait at least the `Retry-After` hint. The payload envelope `id` rides every retry as an `Idempotency-Key` header | in-process: `services/delivery_failure_behavior_test.go` `TestWebhookDelivery_InjectedFaultRecordsAndSchedulesRetry`, `TestWebhookDelivery_TerminalAtMaxAttempts`; `services/webhook_retry_safety_test.go` `TestWebhookDelivery_PermanentStatusIsTerminalNotRetried`, `TestWebhookDelivery_RateLimitedHonorsRetryAfter`, `TestWebhookDelivery_TransientStatusBacksOffAndIsBounded`, `TestWebhookDelivery_IdempotencyKeyIsStableAcrossRetries`, `TestWebhookDelivery_RetryProducesExactlyOneAdditionalPost` |
| `services.notification.delivery` | `services/notification_service.go` `postNotificationJSON` (ntfy/Gotify) **and** `sendPushMessage` (Web Push) | A push-style notification send fails | A `failed` `NotificationDelivery` row with the reason; the reminder is **not** marked sent for that channel, so the next run retries it — no silent drop, no double-send | in-process: `services/delivery_failure_behavior_test.go` `TestNotificationDelivery_InjectedFaultRecordsFailureAndKeepsReminderDue` |
| `services.email.send` | `services/mailer.go` top of `sendViaResend` and `sendViaSMTP` | An outbound email send fails on either transport | The error surfaces to `SendEmail`'s best-effort handling: tolerated if another channel succeeded, otherwise returned as the combined `all email channels failed` error | in-process: `services/mailer_timeout_test.go` `TestSendEmail_InjectedSendFaultSurfaces`, `TestSendViaResend_InjectedFaultSurfaces` |
| `services.oidc.request` | `oidcRoundTripper` on the OIDC HTTP client (`services/oidc_service.go`) | A discovery / token / JWKS / UserInfo call fails | The armed sentinel surfaces from the round trip; `InitOIDCProvider` / `ExchangeAndVerify` fail closed — no partial session, no partial user row | in-process: covered via the OIDC service suite; the guard + timeout the seam rides on are pinned by `services/oidc_ssrf_test.go` |
| `services.import.source` | `services/import_source.go` `executeSourceImport` (inside the one transaction) | DB error mid Meerkat/Monica source import (issue #498) | The whole import **fails closed**: every contact and graph entity rolls back, no `import_source_links` ledger row survives, a retry re-runs cleanly. `PRAGMA integrity_check` + ADR-0012 invariants stay clean throughout | in-process: `services/capacity_fault_injection_test.go` `TestSourceImport_InjectedFaultFailsClosed` |
| `services.search.rebuild` | `services/search_service.go` `RebuildSearchIndexReport` (inside the one transaction, before the DELETEs) | Crash / DB error / `ENOSPC` mid FTS rebuild (issue #498) | The whole rebuild runs in one transaction, so the rollback leaves the **previously-good index untouched** — never half-populated. `CheckSearchIndexConsistency` is still clean, storage is intact, and a later unarmed rebuild succeeds | in-process: `services/capacity_fault_injection_test.go` `TestSearchIndexRebuild_InjectedFaultLeavesPriorIndexIntact`; external: chaos job `disk-full-fts-rebuild` |
| Real `ENOSPC` during backup | `cmd/backup` → `database.BackupSnapshot` | Disk exhaustion while snapshotting | The CLI exits non-zero; the source database is untouched (`integrity_check` ok); no partial file appears at the output path (temp-then-link, fail-closed). `BackupSnapshot` now also runs a `diskspace.Require` **preflight** (issue #498) that refuses up front with a typed `ErrInsufficientSpace` before any write | external: chaos job `disk-full-backup` |
| Real `ENOSPC` during a **bulk write** | `cmd/migratebench seed` populating thousands of contact rows onto a 2 MiB tmpfs | Disk exhaustion mid ordinary bulk INSERT (issue #498) | The write exits non-zero; whatever is on disk is **never corrupt** — an empty/absent file (nothing committed) or a readable database that passes `integrity_check`; the same write to healthy storage produces a clean database | external: chaos job `disk-full-write` |
| Real `ENOSPC` during an **FTS rebuild** | `cmd/backfill-search-index` on a populated DB mounted on a 64 KiB-slack tmpfs | Disk exhaustion while the rebuild's DELETE-then-reINSERT grows the WAL (issue #498) | The rebuild exits non-zero; `integrity_check` is `ok`; the prior index is **byte-for-byte intact** (`contacts_fts` row count unchanged — the single transaction rolled back); on healthy storage the rebuild then succeeds and the index matches the live contact count | external: chaos job `disk-full-fts-rebuild` |
| Real memory cap during a **migration** | `sudo systemd-run --scope -p MemoryMax=128M` around `cmd/migrate up` on an 8k-contact floor DB | OOM pressure during the row-touching migration (issue #498) | The migration either **completes** (peak RSS ~35 MB even at 100 k contacts — the engine reads through the DB) or is OOM-killed and left **dirty**, and the next start **refuses** (MIG-04); `integrity_check` is `ok` in both cases — never a corrupt or healthy-looking half-migration | external: chaos job `mem-limited-migration` |
| Real CPU cap during seed / migrate / FTS rebuild | `sudo systemd-run --scope -p CPUQuota=20%` around the CLIs | A throttled CPU (issue #498) | Every operation stays **correct** — exact row counts, `integrity_check=ok`, `foreign_key_check` clean — just slower | external: chaos job `cpu-limited-correctness` |
| Real `ENOSPC` during a **large migration** | `cmd/migrate` on a 10k-contact floor database mounted on a 1 MiB-slack tmpfs | Disk exhaustion while the migration's row-touching backfills grow the WAL (issue #495's hand-verify: "reduce the available disk below what the migration needs") | The migration exits non-zero; the database is left **dirty at some migration with `integrity_check` ok** — never truncated, never a healthy-looking half-migration; the next startup run **refuses** on the dirty flag (MIG-04) | external: chaos job `large-migration-disk-full`. Note `RLIMIT_FSIZE` is NOT a valid in-process proxy: Linux `ftruncate` ignores it and SQLite pre-extends files with it — a real filesystem filling up is the honest test |
| `database.file.corrupt` | `database/startup_integrity.go` `probeStartupIntegrity` (startup/`make migrate-up`/`InitDB` path) | A real corrupt page that leaves `schema_migrations` readable (disk rot, torn write, a bad restore) — the shape the 24h scheduled job would notice only much later | The startup path **fails closed before the pre-migration backup and before any migration**: a typed `ErrDatabaseCorrupt` names the integrity finding and the restore path, and the file is untouched (never snapshotted as a rollback point, never migrated). A missing file is a fresh install and still migrates; a genuinely healthy file is unaffected. `cmd/doctor -repair` refuses on a corrupt input with exit code 3 rather than issuing its destructive `DELETE … NOT EXISTS` | in-process: `database/startup_integrity_test.go` `TestProbeStartupIntegrityDetectsCorruptDataPage`, `TestProbeStartupIntegrityOnUnopenableFile`, `TestMigrateUpRefusesCorruptDatabase`, `TestProbeStartupIntegrityPassesFreshAndHealthy`, `TestProbeStartupIntegrityDoesNotCreateDatabase`; `cmd/doctor/main_test.go` `TestDoctor_RepairRefusesCorruptDatabase`, `TestDoctor_RepairRefusesWhenStorageProbeCannotRun` |
| `database.migration.version_row_missing` | `database/migrate.go` `RunMigrations` (the `applicationTables` guard, reached from `InitDB` and `MigrateUp`) | A populated database whose `schema_migrations` row was removed/tampered (disk corruption, a hand-edit, a non-enforcing restore) | The migration path **refuses** with a typed `ErrPopulatedVersionlessDatabase` naming the tables it found, instead of reading version 0 as a fresh install: the mandatory pre-migration backup is never silently skipped and `000001` is never replayed against existing tables. The schema is left untouched (not dirty, `integrity_check` ok); a genuinely empty database still migrates normally | in-process: `database/versionless_populated_test.go` `TestMigrateUpRefusesPopulatedVersionlessDatabase`, `TestInitDBRefusesPopulatedVersionlessDatabase`, `TestMigrateUpStillMigratesGenuinelyFreshDatabase`, `TestApplicationTablesClassifiesBookkeepingOnlyDatabase`. (Issue #926.) |

### Planned (filed) — same technique, consumers

These are the tickets that consume the harness above; the domain milestones own
the acceptance criteria, TEST-06 owns the technique:

- **MIG-04 / MIG-05 (#439/#440)** — fail-closed + rollback/recovery procedures
  for migrations, exercising `database.migration.statement` including the
  pause/SIGKILL window.
- **CON-04 (#459)** and **#526** — retry paths and ambiguous failures (the
  write succeeded but the response was lost), injected in-process.
- ~~**INT-03 / INT-04 (#466/#467)** — retry-safe outbound operations and the
  permanent-failure terminal state~~ — **landed**. `integrations.OutboundOperations()`
  classifies each outbound write by idempotency; `integrations.RetryPolicy` /
  `DispositionForHTTPStatus` are the shared backoff / never-retry-a-permanent-status
  primitives; `SyncHealthFields.TerminalFailureAt` + the `webhook_deliveries.failed_permanently`
  column are the terminal state. Uses the `services.webhook.delivery` and
  `services.{contact,calendar}sync.request` seams above; ledger
  `backend/integrations/int03_coverage_test.go`. See ADR 0013.
- ~~**#498** — constrained resources~~ — **landed**. The `constraint × operation`
  classification table is [capacity-under-constraint.md](capacity-under-constraint.md);
  new in-process seams `services.import.source` and `services.search.rebuild`,
  new chaos jobs `disk-full-write` / `disk-full-fts-rebuild` /
  `mem-limited-migration` / `cpu-limited-correctness`, disk-space preflight
  guards (`backend/internal/diskspace`) on backup / import / (memory: export),
  and `loadsmoke`'s `LOADSMOKE_USERS` many-users axis. **#530** — the mandatory
  pre-migration backup, whose failure path fails the upgrade closed
  (`ErrPreMigrationBackupFailed`); the backup seam and now the disk preflight
  exercise it.
- ~~The remaining integration clients (Paperless, Seafile, WebDAV, CardDAV/CalDAV,
  webhooks, notification channels)~~ — **landed** (INT-02, issue #465): all take the
  same seam shape as `services.immich.request` (a `faultingRoundTripper` on the
  client transport, or a `faults.Hook` at the delivery boundary), catalogued above.
  Their per-mode failure behavior is exercised by `services/*_failure_behavior_test.go`
  and the ledger `backend/integrations/int02_coverage_test.go`, which fails if an
  integration in the classification matrix has no failure-behavior test.

## Hand-verification

Per CLAUDE.md, an injection test must prove it pins its recovery path: remove
the refusal and confirm the test fails, then restore. Done for the migration
seam: deleting the dirty refusal in `database/migrate.go`
`checkMigrationPreflight` fails `TestInjectedMigrationFaultFailsClosedAndRecovers`
on its second `MigrateUp` (the dirty database recovers instead of refusing) and
`TestDirtyDatabaseRefusesToStart` (the refusal is no longer a typed
`ErrDirtyMigration`). The equivalent exercise for the import seam is removing
the `txErr` check that returns `apperrors.ErrDatabase` — the injected error then
surfaces as a success with zero rows, and the test's "must fail closed"
assertions fail.

Done for the INT-03/04 (#466/#467) retry-safety seams: deleting the
`if !disp.Retryable` early return in `services/webhook_service.go` `deliverWebhook`
fails `TestWebhookDelivery_PermanentStatusIsTerminalNotRetried` — a `401`/`403`
delivery then gets a `NextRetryAt` and is re-picked up by `ProcessWebhookRetries`
instead of going terminal on the first attempt. Removing the
`AND terminal_failure_at IS NULL` predicate from `SyncAllCalendars`'s subscription
query fails `TestCalendarSync_PermanentFailureStopsScheduledRetries` — the
scheduler re-attempts a terminal subscription and emits a fresh `sync_failed`
event. Restore both.

Done for the `database.migration.before_batch` seam (DEPLOY-03): deleting the
`faults.Hook(faultMigrationBeforeBatch)` call in `runPendingMigrations` fails
`TestInterruptedStartupKillPoints/before_migrations` and
`TestMigrationBeforeBatchFaultLeavesSchemaUntouched` — the armed fault no longer
aborts the run, so the database migrates instead of staying untouched. Restore
it. The readiness backstop added alongside (schema-ahead-of-binary is
`not_ready`, not `ok`) is pinned by `controllers/health_endpoints_test.go`
`TestReadiness_SchemaAheadOfBinary`; reverting the `applied > latest` branch in
`readinessMigrations` fails it.

Done for the issue #498 seams and guards
([capacity-under-constraint.md](capacity-under-constraint.md) has the full
list): deleting `faults.Hook(faultSearchRebuild)` in
`services/search_service.go` `RebuildSearchIndexReport` fails
`TestSearchIndexRebuild_InjectedFaultLeavesPriorIndexIntact` — the armed fault
no longer aborts the rebuild, so it completes instead of rolling back; deleting
`faults.Hook(faultImportSource)` in `executeSourceImport` fails
`TestSourceImport_InjectedFaultFailsClosed` the same way. Deleting the
`diskspace.Require` block in `database/backup.go` `BackupSnapshot` fails
`database/backup_preflight_test.go` `TestBackupSnapshot_RefusesWhenDiskTooFull`
(the error is no longer a typed `ErrInsufficientSpace`); deleting the
`preflightImportDiskSpace` call in `services/import_session.go` `Confirm` fails
`services/import_preflight_test.go` `TestConfirm_RefusesWhenDiskTooFull`;
deleting the `guardExportContactCount` call in `ExportData` fails
`controllers/export_limit_test.go`. Restore each.

Done for the issue #921 / #926 corruption rows. Deleting the
`probeStartupIntegrity(dbPath)` call at the top of
`database/migrate.go` `migrateFileWithPreBackup` fails
`TestProbeStartupIntegrityDetectsCorruptDataPage` and
`TestMigrateUpRefusesCorruptDatabase` — `InitDB`/`MigrateUp` then return nil on
a corrupt page instead of the typed `ErrDatabaseCorrupt`. Removing the storage
gate at the top of `cmd/doctor/main.go` `runRepair` fails
`TestDoctor_RepairRefusesCorruptDatabase` and
`TestDoctor_RepairRefusesWhenStorageProbeCannotRun` — `-repair -confirm` then
runs its destructive deletes (exit 0) on a corrupt database. Removing the
`applicationTables` guard in `database/migrate.go` `RunMigrations` fails
`TestMigrateUpRefusesPopulatedVersionlessDatabase` / `...InitDB...` — the
version-less populated database is treated as fresh and replays the chain.
Deleting the `triggerWebhooksForAllUsers` call in the error branch of
`services/db_integrity_service.go` `checkSearchIndexConsistencyScheduled` fails
`TestCheckDBIntegrityScheduled_SearchErrorFiresWebhook`. Restore each.
