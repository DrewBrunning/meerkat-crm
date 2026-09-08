# PII inventory & data-minimization review

The answer to a question the other security docs do not ask. `threat-model.md` asks *is it
protected?* `data-retention-lifecycle.md` (issue [#414](https://github.com/DrewBrunning/mycorrhizal-crm/issues/414))
asks *how long does it live and how is it deleted?* This document asks the **minimization**
question, per store: *should this data exist at all, is it more than we need, and is it kept
longer than we need?* — and records the answer.

| | |
|---|---|
| **Last updated** | 2026-08-27 (issues [#510](https://github.com/DrewBrunning/mycorrhizal-crm/issues/510), [#621](https://github.com/DrewBrunning/mycorrhizal-crm/issues/621)) |
| **Scope** | Backend (Go/Gin + SQLite), CardDAV/CalDAV server role, structured + access logs, operator backups, Android offline mirror, browser storage. |
| **Companion docs** | `data-retention-lifecycle.md` (retention/deletion, cited here rather than repeated), `asvs-l2.md` V7 (logging) / V8 (data protection), `deployment-baseline.md` (operator boundary), `../privacy.md` (the plain-language operator/adopter summary), `../supported-configuration.md` (the multi-user isolation guarantee and admin-capability statement, issue [#558](https://github.com/DrewBrunning/mycorrhizal-crm/issues/558)). |
| **Method** | Schema walked table-by-table from `backend/database/migrations/*.up.sql`; logs checked against **real captured output**, not by reading the logging code (see [How this was verified](#how-this-was-verified)). |

## Why this review exists

The data here is unusually sensitive even by CRM standards. It is a record of one person's
relationships, and much of it is **about third parties who never consented to being in it and
cannot see what is written about them** — free-text notes, inferred preferences, relationship
labels. Self-hosting means the data never leaves the operator's machine, which is a strong
starting point; it is not the same property as *"we store the minimum,"* and only the second one
survives scrutiny.

Two structural facts shape every row below:

- **Multi-user-per-instance is a supported `1.0.0` configuration** (issue
  [#558](https://github.com/DrewBrunning/mycorrhizal-crm/issues/558)). The operator is data
  controller not only over their own relationship data but over *other users'* — including notes
  those users wrote about people twice removed from the operator. Any **instance-wide** store
  (logs, audit trail, metrics) that attributes personal data to a specific user is a cross-user
  disclosure even though every API handler is correctly `user_id`-scoped.
- **There is no telemetry.** No analytics SDK, no crash reporter, no "usage statistics", no
  phone-home of any kind — `grep -rniE 'analytics|telemetry|sentry|posthog|mixpanel|phone.?home'
  backend/` is empty, and nothing in the frontend or Android client does it either. The only data
  that leaves the instance is what the operator explicitly configures: CardDAV/CalDAV sync,
  outbound email, push, webhooks, and the optional external integrations. This is the single
  biggest minimization result and it is a deliberate, defended position.

## How to read the tables

Each row carries a **necessity verdict**:

| Verdict | Meaning |
|---|---|
| `necessary` | The feature cannot work without it; the data is the minimum the feature needs. |
| `deliberate, documented` | More than the strict minimum, but a considered product decision with a written reason (here or in a linked doc). |
| `kept-longer-than-necessary → #NNN` | Retention exceeds need; remediation filed. |
| `more-than-necessary → #NNN` | Collection exceeds need; remediation filed. |

and a **scope**: `per-user` (rows carry `user_id`, reachable only by the owner's session) or
`instance-wide` (one store for the whole deployment).

---

## 1. Per-user application data

The contact graph and everything hung off it. All of this is content a user authored or imported;
all of it is `user_id`-scoped in every query (CLAUDE.md backend trap #5) and served only to the
authenticated owner.

| Store | Personal data | Subject | Necessity | Retention → lifecycle doc |
|---|---|---|---|---|
| `contacts` (+ nested `card`/`crm`/`passthrough` JSON) | Names, nicknames, emails, phones, addresses, birthdays, org/title, photos, free-text `how_we_met` / `work_information` / `contact_information`, gender, IM handles, URLs | The contact (a third party) | `necessary` — this is the product | Soft-delete + `DELETE_RETENTION_DAYS` (30) purge — §1 |
| `notes` | Free-text notes about a contact — the highest-sensitivity field in the system; written about someone who cannot see it | Third party | `necessary` | §1 |
| `activities` (+ `activity_contacts`) | `title` / `description` / `location` of meetings, calls, events; which contacts were present | Third party + user | `necessary` | §1 |
| `reminders`, `reminder_completions` | `message` free text, cadence, which contact | Third party + user | `necessary` | §1 |
| `life_events` | `type` / `date` / `description` of a contact's life events (birth, marriage, death, job change) | Third party | `necessary` | §1 |
| `preferences` | Inferred/observed likes, dislikes, dietary, gift ideas, free-text `notes`; `sensitivity` column (`normal`/`private`/`secret`) | Third party | `necessary` — but see `sensitivity` filtering below | §1 |
| `conversation_agenda` | Free-text things to raise next time; `reference_url` | Third party | `necessary` | §1 |
| `gifts` | Gift ideas/history, `occasion`, `value_cents`, free-text `description`/`notes` | Third party | `necessary` | §1 |
| `households` / `household_members`, `circles` / `circle_members`, `tags` / `contact_tags` | Grouping labels; `households.address`; membership by contact UID | Third party | `necessary` | §2 (edge rows hard-delete) |
| `relationship_edges` | Relationship type between two contacts, `metadata`, `sensitivity`; only `status=confirmed` is fact | Two third parties | `necessary` | §2 |
| `field_definitions` / `field_values` | Operator-defined custom fields and their per-entity values — arbitrary user-chosen data, `sensitivity` + `projection` columns | Third party | `necessary` (open-ended by design; `sensitivity` is the control) | §2 |
| `contact_sync_conflicts` | `local_value` / `remote_value` (encrypted) of a field that diverged during CardDAV sync | Third party | `necessary` for conflict resolution | §2 |
| `reach_out_suggestions` (+ `reach_out_cursors`) | `old_value` / `new_value` of an org/title/address change that triggered a "reach out" nudge; references an `audit_event_id` | Third party | `deliberate, documented` — derived from the audit trail, ages with it | tied to `AUDIT_RETENTION_DAYS` (90) — §3 |
| `dismissed_household_suggestions` | `address_hash` + `member_hash` — **hashed**, not the address or the members | — | `necessary` and already minimized (a good example) | hard-delete with user |
| `dismissed_duplicate_pairs` | Two contact UIDs the user said "not a duplicate" | Third party (UIDs only) | `necessary` | hard-delete with user |

**`sensitivity` classification.** `preferences`, `relationship_edges`, `field_definitions`, and
`contacts`' nested fields carry a `normal`/`private`/`secret` marker. Anything above `normal` is
excluded from exports and external sync **in the query, not in the caller** (CLAUDE.md backend
conventions). This is the mechanism that keeps the most sensitive third-party data from leaving
the instance even when the user turns on CardDAV.

**At-rest encryption (protection note, not minimization).** Contact free-text, `preferences`,
`conversation_agenda`, `gifts`, `reminders.message`, `life_events.description`, and the audit
`before_snapshot` travel as `encv1:` ciphertext when at-rest encryption is armed (issue
[#380](https://github.com/DrewBrunning/mycorrhizal-crm/issues/380)). `notes.content`,
`activities.title/description/location`, and `webhooks.secret` are **not** yet on that list — a
known gap owned by #380, out of scope here.

---

## 2. Identity & authentication

| Store | Personal data | Scope | Necessity | Notes |
|---|---|---|---|---|
| `users` | `username`, `email` (both unique), `language`, `date_format`, `is_admin` | per-user row, but see §4 | `necessary` | `email` is the reset/notification address and the login identifier |
| `users.password` | bcrypt hash (cost 10) | per-user | `necessary` | never logged, never exported, deny-listed from audit snapshots (`models/audit.go`) |
| `users.password_reset_token_hash` / `*_expires_at` / `*_requested_at` | SHA-256 of a reset token + timing | per-user | `necessary` | single-use; consumed in the same `WHERE` that reads it |
| `users.totp_secret_encrypted` / `totp_enabled` / `totp_confirmed_at`, `recovery_codes.code_hash` | 2FA secret (encrypted) and recovery-code hashes | per-user | `necessary` | recovery codes deleted in the `WHERE` that consumes them (`services/twofactor.go`) |
| `users.oidc_subject` / `oidc_provider` | External IdP subject identifier | per-user | `necessary` when OIDC is configured; `NULL` otherwise | |
| `users.self_contact_vcard_uid` | Which contact row *is* the user | per-user | `necessary` | |
| `api_tokens` | `name`, `token_hash`, `last_used_at`, `scope` | per-user | `necessary` | hash only; plaintext shown once at creation |

---

## 3. Derived & incidental stores

The stores a conventional privacy review forgets, because nobody *decided* to keep the data —
it accumulated as a side effect.

### 3.1 Full-text search index (`contacts_fts`, `notes_fts`, `activities_fts`)

- **Contents**: tokenized copy of contact names/emails/phones/addresses, note bodies, and activity
  text — held **independently of the source rows**, and **in plaintext even when at-rest
  encryption is armed** (the FTS5 index cannot be encrypted column-wise; documented in
  `data-retention-lifecycle.md` §6/§10).
- **Scope**: per-user (`user_id UNINDEXED` column scopes every query).
- **Necessity**: `necessary` for search; it is a rebuildable index, not a second source of truth.
- **Deletion**: `AFTER UPDATE`/`AFTER DELETE` triggers remove the FTS row the instant `deleted_at`
  is set — a soft-deleted contact is unsearchable **immediately**, before the 30-day purge. This
  is the one place deletion is *faster* than the primary table. Verified below.

### 3.2 Audit trail (`audit_events`)

- **Contents**: `entity_type` / `entity_id` / `operation` / `user_id`, plus `before_snapshot` — a
  **redacted JSON snapshot** of the pre-change row. `auditDenyList` (`models/audit.go`) strips
  passwords, TOTP secrets, token hashes, OIDC codes, SMTP/Resend credentials at any depth. Contact
  snapshots include the nested card (T82). No credential data; full contact PII, yes.
- **Scope**: per-user (`user_id` column, indexed). Instance-wide *table*, per-user *rows*.
- **Necessity**: `deliberate, documented`. The snapshot is what powers the Undo button and
  post-hoc investigation. #381 made it tamper-evident, which also makes it **effectively
  immutable within its window** — a deleted contact's data survives here for
  `AUDIT_RETENTION_DAYS` (default 90) after it is gone everywhere else. This is intentional and is
  called out to the operator in `../privacy.md`.
- **Deletion**: `PurgeExpiredAuditEvents` hard-deletes past the window and re-links the hash chain
  (`data-retention-lifecycle.md` §3). No external mirror — audit never syncs anywhere.

### 3.3 Request / access logs

Checked against **real captured output** (`GIN_MODE=release`, `LOG_LEVEL=info`). This review
found and **fixed four instance-wide leaks** in the same PR:

| # | Store | What leaked (before) | Fix (this PR) |
|---|---|---|---|
| F1 | zerolog, `services/mailer.go` + `services/password_reset_service.go` | Recipient email address verbatim (`"email":"alice@example.test"`) at info/warn on every send and every "no channel configured" no-op | `logger.MaskEmail` — local part reduced to `a***`, domain kept for delivery diagnostics (`backend/logger/mask.go`) |
| F2 | zerolog request log, `middleware/logging.go` `query` field | Full query string, including `?search=<a contact's name>` and `?q=<words from a private note>` | `logger.RedactQueryValues` reworked from a credential deny-list to an **allow-list** (`backend/logger/redact.go`): pagination/sort/enum params logged verbatim, everything else — search terms, ids, OIDC `state` — `[REDACTED]` |
| F3 | gin's own `Logger()` (`gin.Default()` in `main.go`) | A **second, entirely unredacted** request line: `GET /api/v1/contacts?search=<name>` | `gin.New()` + `gin.Recovery()` — the app's own redaction-aware `LoggingMiddleware` is the only request logger now |
| F4 | GORM default logger (`database/migrate.go`) | Full SQL with **interpolated literal values** on any errored/slow/not-found query: `SELECT ... WHERE email = "<address>"`. Not gated by `LOG_LEVEL` | **Fixed (issue [#621](https://github.com/DrewBrunning/mycorrhizal-crm/issues/621))**: every connection through `database/migrate.go` uses `newGormLogger` — `ParameterizedQueries: true` logs `?` placeholders, `IgnoreRecordNotFoundError: true` drops the benign not-found SELECTs — pinned by `database/migrate_test.go::TestGormLoggerDoesNotInterpolatePII` |

After F1–F4, a full scripted exercise of the API (register, login, create contact with
name+email+phone, note, search, list, password-reset, delete) produces **no personal data in the
captured logs**. Evidence in [How this was verified](#how-this-was-verified).

What the logs legitimately retain: `user_id`, `request_id`, method, path (no query), status,
duration, client IP, User-Agent. `user_id` + IP is the deliberate minimum for rate-limit and
abuse investigation. **Correlation IDs (issue [#425](https://github.com/DrewBrunning/mycorrhizal-crm/issues/425), in progress):**
this review's position is that a correlation ID may carry **only** low-cardinality identifiers and
enums — never a contact id, name, email, or raw URL — into the standardized field set. Recorded
here so #425 lands against a written rule.

### 3.4 Delivery bookkeeping

| Store | Personal data | Necessity | Notes |
|---|---|---|---|
| `notification_deliveries` | `channel`, `status`, `error` string, `reminder_id` | `necessary` (dedupe: "was this reminder sent on this channel?") | `error` can echo a provider message; low risk, no address column |
| `webhook_deliveries` | `payload` — the **full serialized entity** (a `contact.created` delivery carries the whole contact record), plaintext, plus `error` | `deliberate, documented` — 30-day window (`WEBHOOK_DELIVERY_RETENTION_DAYS`), and successful deliveries store only the event envelope, never the entity body | issue [#622](https://github.com/DrewBrunning/mycorrhizal-crm/issues/622) closed; `data-retention-lifecycle.md` §3 |
| `carddav_sync`, `contact_sync_links`, `calendar_event_links` | Sync tokens, `href`s, content hashes — no PII beyond an opaque UID/URL | `necessary` | hard-delete with parent (`data-retention-lifecycle.md` §2/§7) |
| `job_executions`, `server_settings` | None (job names, lock holder hostname; instance settings) | `necessary` | `locked_by` is a hostname, not a person |

---

## 4. Instance-wide stores & cross-user attribution

The #510 question: *can an instance-wide store name a specific user's personal data?*

| Store | Instance-wide? | Attributes PII to a user? | Recorded reason |
|---|---|---|---|
| Structured logs (zerolog) | yes | `user_id` + client IP, and (until this PR) email/search terms — **now fixed**, F1–F3 | `user_id` + IP is the minimum for rate-limit/abuse response; no contact data after the fix |
| GORM SQL echo | yes | yes, on error/slow queries — **now fixed** [#621](https://github.com/DrewBrunning/mycorrhizal-crm/issues/621) | parameterized + not-found suppressed (`backend/database/migrate.go` `newGormLogger`, pinned by `database/migrate_test.go::TestGormLoggerDoesNotInterpolatePII`) |
| `audit_events` | table is instance-wide; every row has `user_id` | yes, by design | Undo + investigation; ages out at 90 days (§3.2) |
| `users` directory (`GET /api/v1/users/directory`, `ListUserDirectory`) | yes | **usernames are visible to every other authenticated user** on the instance | `deliberate, documented` — required for contact-sharing (issue [#574](https://github.com/DrewBrunning/mycorrhizal-crm/issues/574)); the operator must know usernames are not private between co-tenants. Stated in `../privacy.md` |
| `contact_shares` | table instance-wide; rows scoped to `from_user_id`/`to_user_id` | `contact_display_name` + frozen `payload` cross a user boundary **by the sending user's explicit action** | `deliberate, documented` — 30-day window (`CONTACT_SHARE_RETENTION_DAYS`), `data-retention-lifecycle.md` §1 |
| `job_executions`, `server_settings` | yes | no | — |
| Admin API | — | **Admin cannot read another user's contacts, notes, or activities** — the admin routes (`routes/routes.go:509-529`) are user-account CRUD, 2FA reset, and job triggers only. Admin *can* delete a user (cascade) and, as operator, has filesystem/DB/backup access | This is the #371 admin-capability answer; stated plainly in `../privacy.md` |

No metrics endpoint, no Prometheus exporter, no per-user counters that outlive a request.

---

## 5. Off-database copies

Fully enumerated in `data-retention-lifecycle.md`; summarized here with the minimization verdict.

| Copy | Personal data | Verdict | Reference |
|---|---|---|---|
| Attachments & profile photos on disk | Files a user uploaded against a contact | `necessary`; file deleted immediately on contact/attachment delete (not window-delayed) | lifecycle §5 |
| Operator backups (`VACUUM INTO` snapshot + photo `rsync`) | **A complete copy at full sensitivity** — `private`/`secret` fields, email/hashes, audit trail, still-in-window soft-deleted rows, FTS plaintext | `deliberate, documented` — retention and deletion are **entirely operator-owned** (the app deliberately cannot expire backups, issue [#505](https://github.com/DrewBrunning/mycorrhizal-crm/issues/505)); restoring one resurrects soft-deleted data | lifecycle §10, `deployment.md` "Backup confidentiality & retention" |
| Android offline mirror (Room, SQLCipher) | Cached contact list + FTS for offline read | `necessary`; encrypted end to end (issue [#385](https://github.com/DrewBrunning/mycorrhizal-crm/issues/385)); wiped on logout; rows drop on next sync disagreement | lifecycle §8 |
| Browser `localStorage` | `user_info` (id/username/admin flag/self-contact UID) + UI prefs — **no auth token, no contact PII** | `necessary`; pinned by the #419 Playwright regression | lifecycle §9 |
| Exports (CSV / vCard3 / vCard4 / jSContact / audit CSV) | Whatever the user requests; sensitivity-filtered before it leaves the server | `necessary`; streamed, never written to disk server-side; no artifact outlives the request | lifecycle §11 |
| Import wizard staging | Uploaded rows pre-confirmation | `necessary`; **in-memory only**, 15-minute expiry, lost on restart | lifecycle §12 |
| External integrations (`webdav_configs`, `paperless_configs`, `immich_configs`, `seafile_configs`, `notification_configs`, `calendar_subscriptions`, `contact_subscriptions`) | One encrypted credential row per user per integration; `external_identities` / `external_activities` cache remote `payload` / `metadata` | `necessary`; credentials encrypted (`services/credential_crypto.go`), plaintext never returned by the read endpoint; deleting the connection never deletes anything in the external service (boundary, not gap) | lifecycle §13 |

---

## 6. The third-party dimension

The people described in `contacts`, `notes`, `preferences`, `life_events`, and
`relationship_edges` did not consent and cannot see their record. What the product owes them, and
what it actually provides:

| Owed | Provided? |
|---|---|
| The operator can **find everything about one person** | Yes — FTS search (`contacts_fts` + `notes_fts` + `activities_fts`) covers names, note bodies, and activity text; the contact detail view aggregates every hung-off entity |
| The operator can **remove everything about one person** | Yes for a person who is a `Contact` — `DeleteContact` cascades every dependent row in one transaction (`contact_controller.go`, the canonical checklist in CLAUDE.md trap #6), then the 30-day purge hard-deletes. **Partial** for a person mentioned only inside a free-text note or activity belonging to a *different* contact: they are findable by search but not individually deletable — the operator must edit the note. Stated as a known limit in `../privacy.md` |
| Data about them does not silently outlive a delete | Mostly — see [§8](#8-deletion-completeness). The deliberate exceptions (audit window, backups) are documented, not silent |
| Data about them is not propagated further than they'd expect | `sensitivity` ≥ `private` is excluded from exports and CardDAV/CalDAV **in the query**; `suggested` (unconfirmed) relationship edges are never projected to standards or graphed |

This is issue [#414](https://github.com/DrewBrunning/mycorrhizal-crm/issues/414)'s deletion path
viewed from the data subject's side rather than the account owner's.

---

## 7. Operator responsibilities (multi-user)

Stated in full, for adopters, in [`../privacy.md`](../privacy.md) and
[`../supported-configuration.md`](../supported-configuration.md). In brief: with more than one
user on an instance, the operator holds **other users' relationship data and their notes about
third parties twice removed from the operator**. The software does not discharge the controller's
duties for them — it provides scoping, per-user deletion, export, and a `sensitivity` filter, and
the operator owns everything the deployment boundary
(`deployment-baseline.md`) hands them: the filesystem, the database, and the backups.

---

## 8. Deletion completeness

Checked by walking a contact delete against **this inventory**, not against `DeleteContact`.
Procedure: create a contact with an email, a phone, a note, an activity, and an attachment; note
the sentinel values; `DELETE` it; then inspect each store the inventory lists.

| Store | State immediately after delete | State after `DELETE_RETENTION_DAYS` (30) |
|---|---|---|
| `contacts` + hung-off rows (`notes`, `activities`, `reminders`, `life_events`, `preferences`, `gifts`, `conversation_agenda`) | `deleted_at` set (soft) — invisible to every API query | hard-deleted by `PurgeSoftDeletedRows` |
| Edge/join rows (`relationship_edges`, `circle_members`, `contact_tags`, `household_members`, `activity_contacts`, `contact_sync_links`) | **hard-deleted synchronously** in the same transaction | — |
| `contacts_fts` / `notes_fts` / `activities_fts` | **row gone immediately** via trigger — unsearchable at once | — |
| Attachment file on disk | **deleted immediately** after the transaction commits | — |
| `audit_events.before_snapshot` | **retained** — holds a redacted snapshot of the deleted contact | hard-deleted at `AUDIT_RETENTION_DAYS` (90), i.e. ~60 days *after* the row itself is purged. **Deliberate**, documented in `../privacy.md` |
| `webhook_deliveries.payload` | **retained** if a webhook fired on this contact — a plaintext copy of the deleted contact | hard-deleted at `WEBHOOK_DELIVERY_RETENTION_DAYS` (30), i.e. ~30 days after the delivery attempt; successful (2xx) deliveries no longer hold the entity body at all (only the event envelope) |
| Operator backups | **retained** in every snapshot predating the delete; a restore resurrects the (soft-deleted, not yet purged) contact | ages out of *new* snapshots after the purge; survives in old snapshots until the operator deletes them |
| Android Room mirror | dropped on the next sync (T17 tombstone id list) or on logout wipe | — |
| CardDAV/CalDAV clients | contact stops appearing in the next full listing (no delta protocol; `data-retention-lifecycle.md` §7) | — |

Deliberate retention: **audit snapshots** and **backups** only. Both documented. Everything else
is gone at or before the 30-day mark.

---

## 9. Known over-collection & dispositions

| Finding | Verdict | Disposition | Issue |
|---|---|---|---|
| Recipient email address logged verbatim (mailer + password-reset) | `more-than-necessary` | **Fixed in this PR** — `logger.MaskEmail` | — |
| Full query string (search terms, ids) in the request log | `more-than-necessary` | **Fixed in this PR** — allow-list in `logger.RedactQueryValues` | — |
| gin's duplicate unredacted request logger | `more-than-necessary` | **Fixed in this PR** — `gin.New()` + `gin.Recovery()` | — |
| GORM SQL echo with interpolated PII on error/slow queries | `more-than-necessary` | **Fixed** — `newGormLogger` (`ParameterizedQueries: true` + `IgnoreRecordNotFoundError: true`) wired into `InitDB`/`OpenMigratedFile`, pinned by `database/migrate_test.go` | [#621](https://github.com/DrewBrunning/mycorrhizal-crm/issues/621) |
| `webhook_deliveries.payload` retained forever, full entity body | `kept-longer-than-necessary` | **Fixed** — 30-day window (`WEBHOOK_DELIVERY_RETENTION_DAYS`) + purge job; successful deliveries store only the event envelope, never the entity body | [#622](https://github.com/DrewBrunning/mycorrhizal-crm/issues/622) |
| `notes.content` / `activities.*` / `webhooks.secret` not at-rest-encrypted | protection gap, not minimization | Owned elsewhere | [#380](https://github.com/DrewBrunning/mycorrhizal-crm/issues/380) |
| User directory exposes usernames to all co-tenants | `deliberate, documented` | Stated in `../privacy.md` | — |
| Audit snapshots outlive the deleted row by ~60 days | `deliberate, documented` | Stated in `../privacy.md` and §8 | [#381](https://github.com/DrewBrunning/mycorrhizal-crm/issues/381) |
| Backups are a full-sensitivity copy with operator-owned retention | `deliberate, documented` | `deployment.md` + `../privacy.md` | [#420](https://github.com/DrewBrunning/mycorrhizal-crm/issues/420) |

Everything not listed here was walked and judged `necessary`.

## How this was verified

- **Schema**: every `CREATE TABLE` in `backend/database/migrations/*.up.sql` was enumerated (50
  tables + 3 FTS virtual tables); each is a row above or is recorded as holding no personal data
  (`job_executions`, `server_settings`, `data_encryption_keys`, the `dismissed_*` hash tables,
  sync-token tables).
- **Logs**: the backend was built and run twice with `GIN_MODE=release LOG_LEVEL=info` (the
  production profile) — once before the F1–F3 fixes, once after — with stdout captured to a file.
  A scripted run exercised `register`, `login`, `contacts` create (name + email + phone),
  `contacts/:id/notes`, `contacts?search=`, `search?q=`, `password-reset/request` (known and
  unknown address), `admin/trigger-reminders`, and `contacts/:id` delete, using sentinel values
  (`Zephyrina Testsubjectson`, `zephyrina.contact.sentinel@…`, `+15550009999`,
  `SECRET_NOTE_BODY_SENTINEL…`, `alice.pii.sentinel@…`). The captured files were then `grep`'d for
  every sentinel.
  - **Before**: recipient address in a warn line (`password_reset_service.go:51`); `query="search=Zephyrina"`
    and `query="q=SECRET_NOTE_BODY_SENTINEL…"` in the zerolog request line; the same two as raw
    `[GIN]` lines; `SELECT … WHERE email = "…"` from GORM.
  - **After**: `"email":"a***@example.test"`; `query="search=[REDACTED]"` / `query="q=[REDACTED]"`;
    no `[GIN]` lines at all; and after **#621** no GORM SELECT is logged with a literal value
    (`?` placeholders instead, and not-found lookups not at all — pinned by
    `database/migrate_test.go::TestGormLoggerDoesNotInterpolatePII`).
  - The contact name, phone, and note body from the create/note/list/delete calls appeared in
    **no** log line, before or after — only the search-term and SQL-echo paths carried them.
- **Deletion** (§8): verified by the FTS trigger coverage in `backend/database/migrate_test.go`,
  the purge-window tests in `backend/services/purge_service_test.go`, and the audit-retention
  tests in `backend/services/audit_purge_service_test.go`; the `webhook_deliveries` gap was
  found by `grep` showing no purge call site and is closed by
  `backend/services/webhook_delivery_purge_service_test.go` (issue #622).
- **Regression pins added in this PR**: `backend/logger/mask_test.go` (local part never echoed),
  `backend/logger/redact_test.go` (allow-list semantics; search terms / ids / OIDC state
  redacted), `backend/services/mailer_test.go::TestSendEmail_LogsMaskRecipientAddress`
  (hand-verified to fail before the fix), `backend/middleware/logging_test.go` (query allow-list).

## Changelog

| Date | Change |
|---|---|
| 2026-08-27 | GORM SQL echo leak (F4) closed (issue #621): every connection through `database/migrate.go` uses `newGormLogger` — `ParameterizedQueries: true` logs `?` placeholders instead of interpolated values, `IgnoreRecordNotFoundError: true` drops benign not-found SELECTs. Pinned by `database/migrate_test.go::TestGormLoggerDoesNotInterpolatePII` (hand-verified to fail against the pre-fix default logger). §3.3 / §4 / §9 dispositions updated from "pending/filed" to fixed. |
| 2026-08-27 | `webhook_deliveries` retention gap closed (issue #622): `WEBHOOK_DELIVERY_RETENTION_DAYS` (default 30) + daily purge job + admin trigger, and successful deliveries now store only the event envelope (never the entity body). Disposition updated `kept-longer-than-necessary → deliberate, documented`. |
| 2026-08-26 | Created (issue #510). Log leaks F1–F3 fixed in the same PR; F4 → #621, `webhook_deliveries` retention → #622. |
