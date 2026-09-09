---
title: Deployment
nav_order: 8
has_children: false
---

# Deployment

For initial setup see [Getting Started](getting-started.html). This page covers production-specific concerns.

## Storage requirements

**The database directory (`DATA_PATH` / `SQLITE_DB_PATH`) must be on local disk.** SQLite's WAL
mode depends on advisory byte-range locks that network filesystems — NFS, SMB/CIFS, and similar —
do not reliably implement across clients; running the database over one is a known corruption
risk, not a theoretical one. This is the single most likely self-hosted mistake this project sees
coming (mounting a NAS share and pointing the data directory at it), so it is stated here, first,
rather than only in a forum answer.

The backend logs a startup warning if it detects a known network-filesystem type under the
database path, but that detection cannot cover every case (see
[the supported runtime matrix](development/supported-runtime-matrix.md) for what it does and does
not catch). Photos and attachments have no such constraint — see [Backups](#backups) below for where
each piece of data lives. The rest of what this software runs on — host/architecture, Docker and
Compose floors, browsers, Android, and the supported deployment shape — is stated for operators in
[Supported versions](supported-versions.html), which summarizes the same matrix rather than
restating it.

## How the Docker Setup Works

Mycorrhizal CRM runs as a single all-in-one container. Inside it, nginx serves the React SPA and proxies all `/api/`, `/carddav/`, and `/.well-known/carddav` requests to the Go backend on `127.0.0.1:8080`; the backend is never exposed to the host directly. Only the nginx port is published (default `7300`). This same-origin proxy is built in, so nothing extra is required.

Rate limiters (auth, API, CardDAV, account lockout) are in-memory and per-process; they reset on restart and are not shared across replicas if you run more than one backend instance.

You only need an external reverse proxy for TLS termination. Point it at the published port (default `7300`):

See [Deployment security baseline](security/deployment-baseline.md) for the full recommended
hardening checklist (HSTS, secure cookies, container capabilities, resource limits, firewall/DNS)
and an explicit statement of what this application does not secure (host OS, Docker daemon, reverse
proxy, TLS certs, DNS, firewall, host admins).

```nginx
server {
    listen 443 ssl;
    server_name mycorrhizal.example.com;

    location / {
        proxy_pass http://localhost:7300;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

## Production Environment

Set these variables in `.env` when running over HTTPS:

| Variable | Value |
|---|---|
| `FRONTEND_URL` | Exact origin, e.g. `https://mycorrhizal.example.com` (never `*`) |
| `COOKIE_SECURE` | `true` |
| `COOKIE_DOMAIN` | Your domain |
| `JWT_SECRET_KEY` | Generate with `openssl rand -base64 32`; the server refuses to start with the `.env.example` placeholder or a weak secret |
| `WEBHOOK_BLOCK_PRIVATE_URLS`, `CALDAV_BLOCK_PRIVATE_URLS`, `IMMICH_BLOCK_PRIVATE_URLS`, `PAPERLESS_BLOCK_PRIVATE_URLS`, `SEAFILE_BLOCK_PRIVATE_URLS`, `WEBDAV_BLOCK_PRIVATE_URLS`, `MONICA_BLOCK_PRIVATE_URLS`, `OIDC_BLOCK_PRIVATE_URLS` | `true` on any instance reachable from the internet or hosting accounts you do not personally vet. These default to `false` (trusted-LAN assumption); with them off, an authenticated user's webhook/integration URL can reach loopback, LAN hosts, and `169.254.169.254`. See the [SSRF hardening row](security/deployment-baseline.html#recommended-baseline) in the security baseline. |


## Multi-user instances

One deployment can host several independent users, each with their own contacts, graph,
integrations, and settings. This is a supported `1.0.0` configuration and cross-user
isolation is tested against every route — see
[Supported versions → The deployment shape](supported-versions.html#the-deployment-shape)
for the isolation guarantee, the per-user vs per-instance split, the registration
behaviour, and exactly what an admin can and cannot see.

**If more than one person will use the instance, set `DISABLE_REGISTRATION=true`** and
create each account yourself from the admin panel (Settings → Admin). The first account
registered on a fresh instance is automatically an admin. Open self-service registration
is fine for a single-user instance and for a small group you fully trust; it is not the
setting the abuse controls are tuned for.

A multi-tenant instance should also enable the **app-layer SSRF guard** — the
`*_BLOCK_PRIVATE_URLS` set defaults off for trusted-LAN self-hosting, and with it off an
account holder who can configure a webhook or integration URL can make the server reach
your internal network. See the [SSRF hardening row](security/deployment-baseline.html#recommended-baseline)
in the security baseline and the "SSRF hardening" block in `.env.example`.

## Single Sign-On (OIDC)

Mycorrhizal CRM supports SSO via any OpenID Connect provider (Keycloak, Google, Authentik, Authelia, etc.). When enabled, a **Sign in with provider** button appears on the login page.

### Setup

1. Register a new OAuth2 client with your provider. Set the redirect URI to:
   ```
   https://mycorrhizal.example.com/api/v1/auth/oidc/callback
   ```
   This is derived automatically from `FRONTEND_URL`, no separate variable needed. If your provider
   supports RP-Initiated Logout and requires a `post_logout_redirect_uri` to be pre-registered too, it's
   likewise derived from `FRONTEND_URL`:
   ```
   https://mycorrhizal.example.com/login
   ```

2.  Set the OIDC environment variables in the docker compose. See [Getting-Started → Environment variables](getting-started.html#environment-variables) for details. SSO is disabled if any of the first three variables are missing.

### Account linking

On first SSO login, the backend attempts to match the OIDC identity to an existing account in this order:

1. **Subject match** — if the user has logged in via this provider before, their account is found directly.
2. **Email match** — if the provider returns a *verified* email that matches an existing account, the OIDC identity is linked to that account automatically. Unverified emails are ignored to prevent account takeover (except if `OIDC_TRUST_EMAIL=true` is set).
3. **Auto-provision** — if `OIDC_AUTO_PROVISION=true` and no account matched, a new account is created using the email/name from the provider.

If auto-provisioning is disabled and no match is found, the user sees an error and must be registered manually first.

### Passwords

Accounts created through SSO have no password and can only log in via SSO. Existing password-based accounts that get linked retain their password.

## Upgrades

```sh
docker compose pull
docker compose up -d
```

Database migrations run automatically on startup. **Before applying any pending
migration the server writes a verified pre-migration snapshot** of the database
into a `pre-migration/` directory beside `SQLITE_DB_PATH` (issue #530), and
**refuses to migrate if it cannot** (`ErrPreMigrationBackupFailed`).
`MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` moves the target; nothing disables the
backup. It is the database only — photos and attachments are unchanged by
migrations — and it does not replace the routine three-piece `make backup`
below. It is retained under your control: the app never deletes it. Keep the
`pre-migration/` directory off any backup-rotation purge path, since it holds
your rollback points.

**Read `docs/upgrade-compatibility.md` before upgrading.** It is the canonical
supported-upgrade statement: in-place upgrade is supported from `v0.6.0`
(later), version-skipping within the range is supported, a database below the
floor refuses to migrate with a two-step instruction, and downgrade is
unsupported (rollback = previous version + pre-upgrade backup restore).

**If the server refuses to start after an upgrade** — a dirty migration, a
schema ahead of the binary, a sub-floor schema, or an unwritable pre-migration
backup target — do not retry blindly; the recovery for each state, the exact
pre-upgrade backup file to restore, and the full **roll-back-a-bad-release**
procedure are in `docs/operations/migration-recovery.md`.

## Verifying release artifacts

Every published Docker image is signed and carries SBOM + SLSA provenance attestations; the
Android release APK is additionally cosign co-signed and carries its own build provenance. Before
deploying a new release, or any time you want to confirm a downloaded binary/container actually
came from this project's official build pipeline unmodified, see
`docs/security/release-verification.md` for exact commands (`cosign`, `gh attestation verify`) —
no supply-chain background required.

## Health, liveness & readiness endpoints

The server exposes three unauthenticated, secret-free health endpoints (issue #421). They answer
different questions, and wiring the wrong one to the wrong consumer causes outages (e.g. a monitor
restarting a healthy app because an optional integration is down).

| Endpoint | Question | Cost | Who should hit it |
|---|---|---|---|
| `GET /health/live` | Is the process running? | Instant — touches nothing | Container / orchestrator **restart policy** (`HEALTHCHECK`, Kubernetes `livenessProbe`) |
| `GET /health/ready` | Can this instance serve? | DB ping + migration state + filesystem write probe | **Load balancer / traffic gate** (Kubernetes `readinessProbe`) |
| `GET /health` | Is the CRM actually operational? | Deep — persisted check outcomes, job locks, integration reachability (30 s cached) | **Humans and monitoring aggregators** (dashboards, alerting) |

- `/health/live` returns `200 {"status":"live"}` and never does I/O. It must not fail because a
  dependency is slow — that would make the orchestrator kill a working process.
- `/health/ready` returns `200 {"status":"ready", ...}` or `503 {"status":"not_ready", ...}`. It is
  `not_ready` while the database or a configured file directory is unreachable, or while migrations
  are pending or dirty. Gate inbound traffic on this, not on `/health`.
- `/health` returns `healthy`, `degraded`, or `unhealthy` with a per-facet `checks` breakdown
  (database read/write, migration lag, last DB-integrity-check and restore-drill outcomes,
  background-job locks, and reachability of server-scoped integrations — OIDC, email, FCM).
  **`degraded` is still HTTP 200** — an unreachable optional integration or a stale scheduled job is
  degraded-but-alive, not down. Only a database read failure returns `503`. This is the endpoint
  that reports the running build's version/commit.

The bundled Docker image's `HEALTHCHECK` and the CI boot-wait probes all use `/health/live`. The
single-container nginx is not a load balancer, so `/health/ready` is not wired to anything by
default — put it in front of your own proxy/LB if you run more than one instance.

### Startup, migration, and probe grace periods

Startup runs every pending migration **before** the process binds its listener (`database.InitDB`
in `main.go`, ahead of route registration and `ListenAndServe`). Two consequences that matter to
whoever wires up probes (DEPLOY-03, issue #452):

- **A probe can never see a false `ready` mid-migration.** While migrations run there is no HTTP
  server at all — `/health/live` and `/health/ready` are connection-refused, which every
  orchestrator reads as "not ready". Once serving, `/health/ready` still returns `503` for a
  dirty schema or a schema that is behind *or* ahead of the binary. There is no window in which
  traffic is routed at a half-migrated instance.
- **A probe grace period shorter than the longest migration is an outage.** If a
  `livenessProbe` (or Docker `HEALTHCHECK` feeding a restart policy) starts failing during a long
  startup migration, the orchestrator kills the container mid-migration — which then recovers via
  the [dirty-schema path](operations/migration-recovery.md#dirty-schema), or crash-loops if the
  probe keeps firing. Size the grace period from the measured migration durations in
  [Scale testing → Recorded resource requirements](development/scale-testing.md#recorded-resource-requirements):
  Kubernetes `startupProbe` (`failureThreshold × periodSeconds` ≥ the longest supported upgrade on
  your data size), or Docker `HEALTHCHECK --start-period`. The default image's `--start-period=10s`
  suits a small database; raise it for a large one, or gate startup on `startupProbe` and keep
  `livenessProbe` for steady state only.

All three endpoints are unauthenticated (like the original `/health`) and deliberately carry no
secrets. The deep endpoint's `reason`/`detail` strings are generic categories only — the underlying
errors, absolute paths, the SMTP host/OIDC URL, and any table names or row counts from a failed
integrity check / restore drill go to the **server log and the failure webhook**, never the
response body. It does expose the build version/commit, the applied migration number, and
scheduled-job names — same class of operational metadata the pre-split `/health` already returned.
If you consider even that too much for an unauthenticated endpoint, put `/health` behind your proxy's
auth and leave `/health/live` + `/health/ready` open. The all-in-one image's nginx serves all three
with `X-Robots-Tag: noindex` so they are never crawled.

## Diagnostics & logging

When something is failing in production, `docs/operations/observability.md` is the map: the
structured log field vocabulary, how a correlation ID threads one UI action through every background
step and outbound call it triggers (and how to grep for it), and the admin **System events**
timeline (`/system-events` on web, Settings → System events on Android) — a persisted, restart-
surviving record of scheduled job runs, sync runs, backup/restore drills, and migrations.

Quick reference: `LOG_LEVEL` (`info` default), `LOG_PRETTY` (JSON off in release), and
`SYSTEM_EVENT_RETENTION_DAYS` (`30` default) for how long the timeline keeps rows. Webhook delivery
receipts are bounded by `WEBHOOK_DELIVERY_RETENTION_DAYS` (`30` default, issue #622) — each row
carries a copy of the entity that triggered the event, so the window is a data-retention decision,
not just a log-size knob. For the live tail of what the running container is writing right now:
`docker compose logs -f mycorrhizal`.

## Backups

A backup is two (or three) independent pieces, and missing any of them means the backup is not a
backup:

| Piece | Where it lives | Docker volume |
|---|---|---|
| SQLite database | `SQLITE_DB_PATH` (default `mycorrhizal.db` / `/app/data/mycorrhizal.db` in the image) | `DATA_PATH` (default `./data`) |
| Profile photos | `PROFILE_PHOTO_DIR` (default `/app/static/photos`) | `PHOTOS_PATH` (default `./photos`) |
| Attachments (N7) | `ATTACHMENTS_DIR` (default a sibling of the photos) | `ATTACHMENTS_PATH` (default `./attachments`) |

Photos and attachments live **outside** the SQLite file, so backing up only the `.db` silently loses
them. They are plain directories; a file-level copy (`rsync`/`cp`) is exactly right for them.

**Boundary (BACKUP-02, issue #454): the tool owns the database snapshot; the operator owns the
photo/attachment directories.** `make backup` / `database.BackupSnapshot` deliberately produce and
verify the database piece only — a plain file copy is already the right tool for two ordinary
directories, and the all-in-one Docker image mounts them as separate volumes anyway. What the tool
*does* own is checking that an assembled set is consistent, regardless of who assembled it: see
"Verifying a backup set is complete" below. (`docs/operations/disaster-recovery.md` — BACKUP-03,
issue #455 — is the fuller disaster-recovery-boundaries document, covering every recovery scenario
and its RPO/RTO; this paragraph is the citable decision it references.)

**The automatic pre-migration snapshot is separate from this.** Before every schema migration the
server writes a verified database snapshot into a `pre-migration/` directory beside `SQLITE_DB_PATH`
(`MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` moves it) and refuses to migrate if it cannot — it is the
rollback point for a bad release (issue #530, `docs/operations/migration-recovery.md`). It covers
the database only, so it is not a substitute for this three-piece backup; and the app never deletes
it, so a rotation cron must leave the `pre-migration/` directory alone.

### Why you cannot just copy the `.db` file while the server runs

The database runs in WAL mode. A running server has committed writes sitting in a sidecar
`-wal` file; copying only `mycorrhizal.db` misses them, and a copy taken mid-write can be torn.
`VACUUM INTO` (below) produces a single self-contained snapshot that is valid even while the server
is running — that is the recommended online procedure.

### Online backup (no downtime)

If the backend directory and a Go toolchain are available on the host (e.g. a clone of this repo
used to operate the instance), `make backup` reads `SQLITE_DB_PATH` (exactly like `make migrate-up`)
and writes a timestamped `VACUUM INTO` snapshot beside the database:

```sh
# inside backend/ with the same environment the server uses
make backup
# → Backed up /path/to/data/mycorrhizal.db to /path/to/data/mycorrhizal-20260809-120000.db
```

It runs a best-effort WAL checkpoint first (a tidy-up, not a requirement —
`VACUUM INTO` reads through the WAL, so the snapshot is complete regardless),
refuses to overwrite an existing file, and verifies the result with
`PRAGMA integrity_check` before reporting success. Set `BACKUP_PATH` to choose
the output location instead of the timestamped default:

```sh
BACKUP_PATH=/backups/mycorrhizal-$(date +%F).db make backup
```

No Makefile/Go? Any SQLite client works — the operation is a plain SQL statement:

```sh
sqlite3 /path/to/data/mycorrhizal.db "PRAGMA wal_checkpoint(TRUNCATE); VACUUM INTO '/backups/mycorrhizal.db';"
```

(If the target already exists, `VACUUM INTO` errors rather than overwriting —
remove it or pick a fresh path first.)

Then back up the two directories (they cannot be snapshotted via SQLite):

```sh
rsync -a /path/to/photos/ /backups/photos/
rsync -a /path/to/attachments/ /backups/attachments/
```

For the all-in-one Docker image, the host paths are whatever `DATA_PATH`/`PHOTOS_PATH`/
`ATTACHMENTS_PATH` resolve to (defaults `./data`, `./photos`, `./attachments` next to your
`docker-compose.yml`), so `make backup` from the host against `SQLITE_DB_PATH=./data/mycorrhizal.db`
backups up the same file the container writes.

### Offline backup (downtime, simplest)

1. Stop the server: `docker compose stop`. A clean stop checkpoints the
   WAL, so the `.db` file is then complete on its own.
2. Copy the database, photos, and attachments:
   ```sh
   cp /path/to/data/mycorrhizal.db /backups/mycorrhizal.db
   rsync -a /path/to/photos/ /backups/photos/
   rsync -a /path/to/attachments/ /backups/attachments/
   ```
3. Start the server again: `docker compose start`.

### Verifying a backup set is complete

`make backup` verifies the database it writes (`PRAGMA integrity_check`); it says nothing about
whether the photo and attachment directories you copied alongside it still line up with that
database. A database that restores perfectly can still hold an attachment row whose file was never
copied — the failure is silent until someone opens that attachment. `make backup-verify`
(`backend/cmd/backupverify`) closes that gap: point it at an assembled set and it reconciles every
attachment and profile-photo row against the files present, regardless of who copied them.

```sh
# from a checkout of this repo (run inside backend/); the Docker image has no Go toolchain
SQLITE_DB_PATH=/backups/mycorrhizal.db PROFILE_PHOTO_DIR=/backups/photos \
  ATTACHMENTS_DIR=/backups/attachments make backup-verify
```

- A **missing** file — a live row with no backing file in the set — is a real defect: the command
  names it (e.g. `missing attachment: 3f2c…-file (owner attachment#42)`) and exits non-zero.
- An **orphan** file — present with no owning row — is reported but does not fail the command; it
  distinguishes a stale directory from a lost one (feeds DB-01, issue #460's orphan detection).
- Soft-deleted rows are excluded on purpose: `DeleteContact` removes the on-disk file at delete
  time, so a soft-deleted row with no file is the expected steady state, not a hole.

Run it against every backup set you take, not just at restore time — a hole found the moment the
backup is made is far cheaper than one found during a disaster.

### Restore

A restore is a deliberate **point-in-time rollback**: it replaces the whole instance with the
snapshot, so anything created or edited *after* the backup was taken is lost — and anything that had
been soft-deleted (but not yet purged, see T26) before the backup is resurrected. That is the
expected meaning of restoring a file-level backup; there is no partial/merge restore.

1. Stop the server: `docker compose stop`.
2. Replace the three pieces from backup. For a database backup produced by `VACUUM INTO`, the
   snapshot file is self-contained — drop any `-wal`/`-shm` files that may sit beside the live
   database first:
   ```sh
   rm -f /path/to/data/mycorrhizal.db /path/to/data/mycorrhizal.db-wal /path/to/data/mycorrhizal.db-shm
   cp /backups/mycorrhizal.db /path/to/data/mycorrhizal.db
   rsync -a --delete /backups/photos/ /path/to/photos/
   rsync -a --delete /backups/attachments/ /path/to/attachments/
   ```
   `rsync --delete` matters: it removes files that were added to the photo/attachment directories
   after the backup, so the directories match the snapshot instead of blending old and new.
3. Start the server: `docker compose start`. Migrations run automatically on startup.
4. **Rebuild the full-text search index.** A `VACUUM INTO` snapshot carries the FTS index, so a
   same-version restore usually needs nothing here — but a cross-version restore runs startup
   migrations, and a hand-written migration that touches a base table bypasses the FTS triggers.
   Rebuilding is cheap, idempotent, and removes the doubt:
   ```sh
   curl -fsS -X POST https://<host>/api/v1/admin/search/rebuild \
     -H "Authorization: Bearer <admin token>"
   # → {"message":"Search index rebuilt","indexed":{"contacts":…,"notes":…,"activities":…}}
   ```
   See [Rebuilding the full-text search index](operations/search-index.md) for the no-HTTP path and
   the guarantees.
4b. **Rebuild the denormalized contact columns.** Same reasoning for the flat `contacts.*` projection,
   `sort_name`, `addresses_flat` and `phones_normalized` — a cross-version restore's startup
   migrations, or any hook-bypassing write, can leave them stale. Also cheap and idempotent:
   ```sh
   curl -fsS -X POST https://<host>/api/v1/admin/contacts/rebuild-derived \
     -H "Authorization: Bearer <admin token>"
   # → {"message":"Derived contact columns rebuilt","contacts_scanned":…,"contacts_updated":0,…}
   ```
   No-HTTP path: `go run ./cmd/backfill-derived-columns`. Full catalogue of derived state:
   [Derived-data inventory](derived-data-inventory.md).
5. **Verify** — a restore that has never been tested is a hypothesis. Log in and check that a known
   contact, a recent note, and a reminder are present; run a search for a contact you know exists;
   for the photo/attachment directories, open a contact's photo and download an attachment. The
   automated check behind this page's procedure is `frontend/e2e/backupRestore.spec.ts`, which backs
   up a populated instance, destroys the database and both directories, restores, and asserts every
   entity type survived.

The JWT secret key lives in your environment (`.env`), not in the database — restoring a
backup does not change which key the server uses. If you rotated `JWT_SECRET_KEY` between backup and
restore, any session tokens issued under the old key are unrecognized, so users simply have to log
in again. That is harmless.

### What is NOT in any backup — keep these separately

The database and directory backups above cover every user-authored row and file. They do **not**
cover the secrets that unlock or authenticate the instance — these live only in your environment
(`.env` / your orchestrator's secret store) and a lost one is not something a restore can fix:

- **`JWT_SECRET_KEY`** — see above: harmless to lose (a rotation just logs everyone out), but it is
  not *in* the backup either way.
- **`DATA_ENCRYPTION_KEY`** / **`DATA_ENCRYPTION_KEY_FILE`** — the master key for at-rest field
  encryption (issue #380). Unlike the JWT key, losing this is **not recoverable**: the restore drill
  (below) verifies every week that the live master key still unwraps the database's encrypted
  columns specifically because there is no fallback if it doesn't. Back this up wherever you keep
  the rest of your secrets, separately from the database/photo/attachment set — see "Backup
  confidentiality & retention" below for the full key-management story.

What **is** covered, for clarity: TOTP seeds and recovery-code hashes live in the database (the
`users` table), so they travel with the database snapshot like any other row — no separate secret to
track for 2FA.

### Automated integrity & restore checks

Two scheduled background jobs automate the two checks above so a silent failure is caught rather
than discovered during a real disaster:

- **Live DB integrity check** (issue #273) — runs `PRAGMA integrity_check` against the live
  database on a schedule (`DB_INTEGRITY_CHECK_ENABLED`, default on; `DB_INTEGRITY_CHECK_INTERVAL_HOURS`,
  default `24`).
- **Restore drill** (issue #275) — takes a fresh backup snapshot, restores it into a scratch
  database, compares every table's row count against the live database, and — since issue #420 —
  verifies the snapshot's wrapped DEK unwraps under the current master key, so a rotated/lost
  at-rest key is caught here rather than at the moment of need. Since issue #454 it also runs the
  same reconciliation as `make backup-verify` above, against the snapshot's rows and the *live*
  `PROFILE_PHOTO_DIR` / `ATTACHMENTS_DIR` — a live directory that lost a file fails the drill by
  name; an orphan is logged but does not fail it
  (`DB_RESTORE_DRILL_ENABLED`, default on; `DB_RESTORE_DRILL_INTERVAL_HOURS`, default `168`, i.e.
  weekly). Every run records its wall-clock as `duration_ms` on the
  `restore_test_completed` System events row; set `DB_RESTORE_DRILL_MAX_DURATION_SECONDS` to a
  budget to get a `WARN` (and a note on that row) when a run exceeds it — see
  [Recovery objectives (RPO and RTO)](#recovery-objectives-rpo-and-rto) for how that feeds the RTO
  number.

Either job logs and fires a webhook (`db.integrity_check_failed` / `db.restore_drill_failed`, see
Settings → Webhooks) on failure, so "test restores regularly" above is now something the app does
for you rather than a manual chore.

Since issue #428 these failures also flow through **alerting on state transitions**: a scheduled
evaluator watches the per-subsystem health, disk usage, and scheduled-job liveness, and dispatches
one notification when a condition breaks (`alert.raised`) and one when it clears
(`🟢 Backup recovered after 3 failures` — `alert.cleared`), de-duplicated so a persistent failure
does not page you on every run. Webhooks go to all subscribers; email/ntfy/Gotify/push go to admin
users. Full condition list and the `ALERT_*` env vars are in
`docs/operations/observability.md` → "Alerting on state transitions".

### Recovery objectives (RPO and RTO)

**RPO** (recovery point objective) is how much data a recovery may lose; **RTO** (recovery time
objective) is how long recovery takes. Both are scoped to a **single-instance, self-hosted**
deployment: there is no replica and no failover, so every recovery is a restore, and these numbers
describe that restore. They are derived from the mechanisms above and from the measured figures in
`docs/development/scale-testing.md` (issue #495), not asserted. BACKUP-03 (issue #455) consolidates
them, per scenario, into the disaster-recovery-boundaries document alongside the full procedures;
this section is the citable derivation it references.

#### RPO — how much data a recovery can lose

| Scenario | RPO | Why |
|---|---|---|
| **Planned upgrade rollback** (a bad release) | **0** | The pre-migration snapshot is taken automatically, verified, and fail-closed immediately before the first migration runs (issue #530) — nothing is committed between the snapshot and the upgrade. Rolling back is *deploy the previous binary + restore that snapshot* (`docs/operations/migration-recovery.md` → "Rolling back a bad release"). Non-disableable: `MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR` relocates it, nothing turns it off. |
| **Host loss / database corruption / a lost photo or attachment directory** | **= your backup interval** | The app ships **no scheduled routine backup** — `make backup` plus the directory `rsync` are an operator cron (BACKUP-02, issue #454). A restore replaces the instance with the last set you captured, so everything written since is lost. With the recommended **daily** cron, RPO ≈ **24 h**. |
| **Accidental deletion by the user** | **= your backup interval** (as host loss) | Soft delete keeps the row for `DELETED_RETENTION_DAYS` (default **30**) as a sync tombstone, **but there is no undelete**: `POST /api/v1/audit/:id/undo` reverts an accidental *edit* only (update events; it rejects deletes), and `DeleteContact` hard-deletes the contact's join rows and removes its attachment/photo files from disk immediately. Recovering a deleted contact is a point-in-time restore from a backup predating the deletion — see BACKUP-03. (*Archiving* a contact, by contrast, is reversible via `/unarchive`.) |

The one freshness number the app enforces itself: the **`backup_stale` alert** fires when no
successful backup or restore-drill run has been observed for `ALERT_BACKUP_MAX_AGE_HOURS` — default
`2 × DB_RESTORE_DRILL_INTERVAL_HOURS` = **336 h** (14 days). That is a "something is badly wrong"
alarm, deliberately far looser than the recommended daily cron so one missed nightly run does not
page anyone; it is **not** an RPO SLA. To have the app hold you to a tighter number, set
`ALERT_BACKUP_MAX_AGE_HOURS` to roughly twice your actual cron interval.

**The gap, stated rather than rounded away:** the only automatic, app-guaranteed RPO is the **0**
for the planned-upgrade case. For host loss the app recommends daily backups (≈ 24 h) but neither
schedules nor enforces them — closing that gap is an operator action: a backup cron at your target
frequency, and, for host loss specifically, an **off-host copy** of the backup set (issue #505),
since a backup on the same disk as the database does not survive that disk.

#### RTO — how long recovery takes

Derived at MVP scale from `docs/development/scale-testing.md` — **100,000 contacts, ~450 MB
database** — for a prepared operator following the [Restore](#restore) procedure with the backup
set already on the host:

| Step | Time at MVP scale | Source |
|---|---|---|
| Copy the `.db` snapshot into place | seconds (~450 MB file copy) | file size |
| `rsync` the photo + attachment directories | minutes, set by directory size — the file directories can dwarf the `.db` at scale (BACKUP-02) | operator storage |
| Startup migration after restore | **~6.5 s** (v0.6.0 → current, 100k contacts) | `scale-testing.md` → "Recorded resource requirements" |
| `PRAGMA integrity_check` + `/health/ready` verification | seconds | `TestLargeDatasetBackupRestoresAtScale` |

**Stated RTO: under 30 minutes** at MVP scale. The dominant terms are the human steps (stop, swap
files, start, verify) and the directory `rsync` — the database snapshot restores and migrates in
seconds. RTO grows with database and directory size and shrinks with faster storage. It does **not**
include time to *fetch* an off-host backup (network transfer from wherever issue #505's copy lives);
that is deployment-specific and additive.

**Keeping the number honest:** the weekly restore drill records `duration_ms` on every
`restore_test_completed` row in the System events timeline (`/system-events`) — the snapshot →
restore → row-count → completeness cycle for the database piece. Compare successive rows to watch
restore time drift as data grows. Set `DB_RESTORE_DRILL_MAX_DURATION_SECONDS` to your
database-piece budget to get a `WARN` log line and a note on that timeline row when a run exceeds it
— drift visible before an incident, not during one. The drill still passes: a slow disk one week is
not a backup failure.

#### The levers

| To change… | Lever | Tradeoff |
|---|---|---|
| RPO (host loss) | Backup cron frequency | More frequent = less data lost, more IO and storage churn |
| RPO (host loss survives the host) | Off-host copy of the backup set (issue #505) | Adds transfer time to RTO; the copy is a full sensitive-data replica — see "Backup confidentiality & retention" |
| RPO (upgrade) | — | Already 0 and non-disableable |
| RTO | Database + directory size; storage speed; keeping a backup set on the host | A local set restores fastest; an off-host-only set trades RTO for host-loss durability |
| RTO alarm sensitivity | `DB_RESTORE_DRILL_MAX_DURATION_SECONDS` | Too low = noise; unset = drift only visible by reading the timeline |

### Storage-growth trend & thresholds

The admin System status page (`/admin/system-status`, issue #388) reports storage **right now**:
database footprint, filesystem free/total, per-directory sizes. Since issue #652 it also reports
storage *trend*, backed by a small time-series the app samples itself:

- **The sampler.** A daily scheduled job (`storage_sample`) writes one row to the
  `storage_samples` table: database size (main file + `-wal` + `-shm`), filesystem used/total, and
  the profile-photo / attachment directory totals. It is job-lock guarded (one row per day even on
  a multi-instance deploy), emits a `system_events` timeline entry per run, and prunes rows older
  than `STORAGE_SAMPLE_RETENTION_DAYS` (default **180**) in the same run — the history is bounded
  by construction. The table is operational bookkeeping, not user data; see
  `docs/security/data-retention-lifecycle.md` §20.
- **The trend block.** The endpoint derives `growth_7d_bytes` / `growth_30d_bytes` /
  `growth_90d_bytes` (latest sample minus the oldest within each window, null until enough history)
  and `projected_full_at` — a least-squares fit of filesystem-used over the last 30 days
  extrapolated to the filesystem total. The projection is null while the slope is flat/shrinking or
  there are fewer than 14 days of samples.
- **The threshold.** `usage_percent` is folded against two tiers —
  `STORAGE_WARN_PERCENT` (default **75**) and `STORAGE_CRITICAL_PERCENT` (default **90**) — into
  `ok | warning | critical`, with the same -5% hysteresis `diskSpaceCondition` uses (once a tier is
  entered, usage must drop 5 points below its threshold to clear). A warning/critical tier elevates
  the endpoint's `overall` status to at least `degraded` — never `unhealthy`, which stays reserved
  for a database read failure.

### Backup confidentiality & retention

A backup is a **complete copy of the CRM's sensitive data** — not a sanitized extract — so the rules
below are the same ones that apply to the database itself. This section is the confidentiality,
retention, and access statement for backups (issue #420); the per-data-type retention/deletion story,
including whether each type survives in backups, is §10 of `docs/security/data-retention-lifecycle.md`.

**What a backup actually contains.** The DB snapshot holds every user's data at full sensitivity:
`private`/`secret` contact fields, email addresses, notes, lifecycle data, attachment metadata, the
audit trail, password hashes, API-token hashes, and TOTP recovery-code hashes — plus soft-deleted
rows still inside their undo window (see below). The two plaintext directories (photos, attachments)
complete the backup. The snapshot does **not** contain the secrets that unlock it: `JWT_SECRET_KEY`
and `DATA_ENCRYPTION_KEY` live in the operator's environment, never in the database. Access to a
backup is operator access — whoever can read the filesystem or backup store it lands in can read the
backup, and the app itself never re-reads one (only the restore drill creates its own fresh
snapshot, into a scratch directory it deletes afterwards).

**Encryption & key management.** The DB snapshot inherits the database's field-level at-rest
encryption (issue #380): encrypted columns travel as `encv1:` ciphertext, and the wrapped data
encryption key (DEK) travels inside the snapshot (`data_encryption_keys`). Restoring therefore
requires the **same master key** the live instance uses — a restore under a different key fails
closed at boot (GCM authentication rejects the unwrap) instead of serving garbage. Two operator
consequences:

- Prefer a dedicated `DATA_ENCRYPTION_KEY` over the `JWT_SECRET_KEY` fallback. If the at-rest master
  key is derived from `JWT_SECRET_KEY` (the zero-config fallback), rotating that secret changes the
  derived key, so the stored wrapped DEK — in the live database *and* in every snapshot taken under
  the old key — can no longer be unwrapped: the server fails closed at boot, and restoring an old
  snapshot fails the same way. With a dedicated key, rotating the JWT secret never affects at-rest
  data or backups.
- Encrypted columns are only part of the story. The FTS-indexed columns (`notes.content`,
  `activities.*`, flat contact search fields) are plaintext by design — they must be, for FTS5
  indexing (`docs/security/asvs-l2.md` P4) — and the photos/attachments directories are plain files.
  **Protecting those at rest is the operator's job** (`age`/`gpg`/encrypted volume), exactly as it
  was before #380, and off-host movement should use the same protection in transit (`rsync` over SSH,
  TLS to object storage).

**Retention & deletion.** Retention is entirely operator-owned: this app has no backup rotation,
expiry, or auto-deletion — and deliberately won't, because an app that can expire backups gives an
attacker running as the app the same power (issue #505). A cron-scheduled `make backup` accumulates
snapshots until the operator imposes a lifecycle, e.g.:

```sh
# keep 30 days of daily snapshots. -maxdepth 1 is load-bearing: it keeps the
# sweep out of the pre-migration/ subdirectory, whose rollback points must
# never be aged out by routine rotation (issue #530; see below).
find /backups -maxdepth 1 -name 'mycorrhizal-*.db' -mtime +30 -delete
```

If your routine snapshots land in the same directory as the live database (the default `make backup`
location — a sibling of `SQLITE_DB_PATH`), that directory *also* contains the automatic `pre-migration/`
subdirectory. A recursive `find … -delete` there would match the pre-migration snapshots too (their
names fit `mycorrhizal-*.db`) and silently delete your last rollback point once it aged past the
window. Keep the sweep non-recursive (`-maxdepth 1`), or point it at a directory that holds nothing
else, or add `-not -path '*/pre-migration/*'`.

**Deleting live data never deletes backups.** Purging contacts, notes, or any other data has no
effect on already-taken backup files. A restore is a point-in-time rollback: anything soft-deleted
(but not yet purged, T26) as of the snapshot is resurrected, and anything created or edited after
the snapshot is lost. Soft-deleted data therefore ages out of backups in two steps: it stops
appearing in *new* snapshots once the purge window (default 30 days, `DELETE_RETENTION_DAYS`) passes,
but it survives in every snapshot that predates that purge until the operator deletes the snapshot.
There is no partial/selective restore.

**Restore-environment security.** A restore is a real, destructive change, so the safe order is:
(1) let the automated restore drill (issue #275) do it into a throwaway first — it restores a fresh
snapshot into a scratch database weekly by default, compares every table's row count with the live
database, and since issue #420 also verifies the snapshot's wrapped DEK unwraps under the current
master key, so a rotated/lost key is caught weekly instead of at the moment of need; (2) for a real
restore, stop the server, replace the three pieces, and verify a known contact/note/reminder plus a
photo and an attachment (or run the `frontend/e2e/backupRestore.spec.ts` round trip on a throwaway
copy first). Restoring into an environment whose master key differs from the snapshot's fails closed
at boot — that is the intended signal to check keys before touching the live instance. The audit
trail in a restored database is only as fresh as the snapshot.

### Backup immutability & ransomware resistance

Every recovery guarantee above assumes the backups still exist and are intact when you need them.
Against a **compromised host** — ransomware, or an attacker who got code execution as the application
— that assumption fails for any backup the application itself can reach: it runs as a uid that can
read and write the local backup directory, so anything it can write, it (or something running as it)
can also encrypt or delete. This section is issue #505's statement of what the application does about
that, and what it deliberately leaves to you.

**What the application guarantees: write-new-only.** `database.BackupSnapshot` (behind `make backup`
and the automatic pre-migration snapshot) is the *entire* backup-write surface, and it only ever
creates a new file. It refuses to overwrite an existing output, the only file it ever removes is the
uniquely-named temp it just created, and there is no code path anywhere in the app — no rotation, no
expiry, no "clean up old backups" job — that deletes, truncates, re-encrypts, or modifies an existing
backup or pre-migration snapshot. This is enforced and tested by trying it
(`backend/database/backup_immutability_test.go`). Retention is left entirely to you for the same
reason: an in-app expirer is a delete-backups capability, and a compromised app would inherit it.

**What the application cannot do for you.** On the host, write-new-only is only a barrier against the
app's *own bugs*. It is not a barrier against an attacker with the app's uid — that uid can `chmod`
and `rm` its own files regardless of what the app's code does. Real immutability against a compromised
host means putting the backups somewhere the application holds **no credential to reach**. Pick one,
in rough order of strength for a single-instance self-hosted deployment:

1. **Pull-based off-host copy (recommended default).** A separate backup host reaches *in* over SSH on
   a schedule and copies the snapshot + the photo/attachment directories out; the Mycorrhizal host
   holds no key, token, or mount that can reach the backup store. A compromise of the Mycorrhizal
   host cannot touch what is already off it, and cannot stop a future pull it does not know about.
   This is the cheapest to reason about: the trust flows one way, from the backup host to the app
   host, and never back.

   ```sh
   # On the BACKUP host, in its own cron — NOT on the Mycorrhizal host.
   # The Mycorrhizal host's SSH user is unprivileged and read-only.
   rsync -e 'ssh -i ~/.ssh/mycorrhizal-pull' -a --link-dest=../latest \
     mycorrhizal-pull@app-host:/srv/mycorrhizal/data/     "/backups/$(date +%F)/data/"
   rsync -e 'ssh -i ~/.ssh/mycorrhizal-pull' -a --link-dest=../latest \
     mycorrhizal-pull@app-host:/srv/mycorrhizal/photos/   "/backups/$(date +%F)/photos/"
   rsync -e 'ssh -i ~/.ssh/mycorrhizal-pull' -a --link-dest=../latest \
     mycorrhizal-pull@app-host:/srv/mycorrhizal/attachments/ "/backups/$(date +%F)/attachments/"
   ln -sfn "$(date +%F)" /backups/latest
   ```

   Run `make backup` on the app host first (cron) so the pull always finds a fresh, integrity-checked
   `.db` snapshot rather than copying a live WAL database. Retention (how many dated directories to
   keep) is the backup host's cron, never the app's.

2. **Object-locked remote storage.** An S3-compatible bucket with Object Lock in compliance mode (or
   a provider equivalent), and credentials scoped to `PutObject` only — no `DeleteObject`, no
   `PutBucketLifecycle`, no version overwrite. The app can add a backup and can never remove or alter
   one; expiry is the bucket's lifecycle policy, configured out-of-band by an identity the app does
   not have. Encrypt before upload (see "Backup confidentiality & retention" above) so moving
   off-host is not a confidentiality regression. This app ships no uploader — drive it from cron with
   `rclone`/`aws s3` and a write-only key.

3. **Filesystem snapshots (btrfs/ZFS) as a complement, not a substitute.** Read-only `.snapshot`
   subvolumes on the app host defend against accidental deletion and give near-instant local
   rollback, and a non-root retention policy an app compromise cannot rewrite. They do **not** survive
   loss of the host, and a root-level compromise can still `btrfs subvolume delete` them — so pair
   them with 1 or 2, do not rely on them alone.

**The invariant, and how to check it holds.** Whatever you choose, the property to verify is: *the
Mycorrhizal application's own credentials cannot delete or modify an existing backup.* Test it by
trying, from the app host:

- With the app's uid, attempt to `rm`, `truncate -s0`, overwrite, and `gpg`-encrypt-in-place a file
  in the off-host store (or a locked object). For options 1 and 2 every attempt must fail with a
  permission/authorization error — the app host has no path or key to that store. For a pull-based
  setup, confirm there is **no** SSH key, rsync module, or mount on the app host that reaches the
  backup host.
- After that simulated compromise, restore from an untouched off-host backup and confirm it produces
  a working instance (the `frontend/e2e/backupRestore.spec.ts` round trip, or `make backup-verify` +
  the Restore steps above).
- Confirm retention expiry cannot be triggered from the app host: the lifecycle policy / pruning cron
  lives on the backup host or in the bucket config, under an identity the app does not hold.
- Confirm a routine rotation cycle leaves the last **pre-migration** rollback point
  (`pre-migration/…-pre-migration-<from>-to-<to>-….db`, issue #530) in place — it is in its own
  subdirectory precisely so a non-recursive sweep cannot reach it (see "Retention & deletion" above).

Per CLAUDE.md's hand-verify rule: to prove the immutability check has teeth, grant the app host's
credentials delete permission on the backup target (add `DeleteObject` to the key, or give the app's
SSH user write access to the backup host), re-run the deletion attempt above, confirm it now
succeeds — then revoke it.

**Where this is written down as a decision:** `docs/security/asvs-l2.md` P5 (the control-level
statement), `docs/security/threat-model.md` (Assets → Backups), and
`docs/security/deployment-baseline.md` (the operator baseline row). This section is the runbook those
three cite.
