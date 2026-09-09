---
title: Integration Classification Matrix
nav_order: 15
---

# INT-01 — Integration classification matrix

> **Generated artifact — do not hand-edit.** The source of truth is
> `backend/integrations` (`Registry()` + `Dispositions()` + `OutboundOperations()`). Regenerate with
> `cd backend && go run ./cmd/genintegrationmatrix` (or `make gen-integration-matrix`);
> the drift test `backend/integrations/matrix_test.go` fails until this file and the
> registry agree, and `TestEveryOutboundClientIsClassified` fails if a new outbound
> client is added under `backend/services/` without a row here.

This product talks to a lot of software it does not control. Treating those as one
category is how failure handling goes wrong: a CalDAV server being unreachable is a
temporary degradation of a background sync; an OIDC provider being unreachable means
nobody can log in; a Paperless instance being permanently gone means a stored
reference never resolves again. Same word, three different correct behaviors. This
matrix writes the classification down once so every client's retry, timeout, and
surfacing decision is made against a shared position. DOC-03 (issue #488) publishes
the operator-facing half and cites this document for the engineering half.

## Classification axes

Every integration is placed on five axes that determine what "handle the failure
correctly" means for it.

| Axis | Values | What it decides |
|---|---|---|
| **Criticality** | `required-when-enabled` · `optional` | Whether losing it blocks a whole workflow or just degrades a feature. |
| **Direction** | `outbound` · `inbound` · `bidirectional` | Who opens the connection — and therefore who imposes the timeout. |
| **Cadence** | `interactive` · `scheduled` · `event-driven` | Whether a failure is seen by a user immediately or only by a background job. |
| **Data authority** | `remote-authoritative` · `shared` · `enrichment` · `none` | Whether the remote holds data we cannot reconstruct. |
| **Failure impact** | `degraded-feature` · `blocked-workflow` · `silent-staleness` | What actually goes wrong. `silent-staleness` is the dangerous one — nothing looks wrong. |

## Transient vs permanent — the per-error classification

The distinction #465 (INT-02), #466 (INT-03) and #467 (INT-04) depend on is a
property of the **error**, not of the integration: a 503 is transient no matter who
returns it; a 401 after a revoked token is permanent until a human acts. This table
is `integrations.Dispositions()` — those tickets assert against it and must not carry
a second copy of the judgment.

| Failure mode | Class | Retry safe? | Honor `Retry-After`? | Rationale |
|---|---|---|---|---|
| `unreachable-host` | **transient** | yes | no | DNS/refused/no-route is almost always the remote being down or a transient network fault; retry with backoff, escalate to a permanent state only after a bounded run of consecutive failures (#467). |
| `timeout` | **transient** | yes | no | A hung or slow remote clears on its own; the request must be bounded (see each integration's timeout) so a retry is possible rather than an indefinite block. |
| `auth-expiry` | **permanent-until-human** | no | no | A 401 from previously-valid credentials will not become a 200 by repetition — a token expired or a password changed. Stop, and surface an actionable re-auth prompt (#467). |
| `authz-revoked` | **permanent-until-human** | no | no | A 403 means the account is known but the grant was withdrawn (scope removed, share revoked). Retrying is waste; a human must restore access. |
| `malformed-response` | **transient** | yes | no | A truncated/garbled body is usually a mid-flight cut or a transient proxy error; retry a bounded number of times, but never partially apply what did parse. |
| `rate-limited` | **transient** | yes | yes | 429/503 is the remote asking for less traffic, not a failure. Back off for at least the Retry-After interval; ignoring it turns a rate limit into a ban. |
| `remote-resource-deleted` | **permanent-until-human** | no | no | A 404/410 on something that used to resolve means the remote deleted it. It will not come back by retrying; the local reference must be reconciled (dropped or re-linked), never used to delete local data. |

"Retry safe" means a retry is *sound* (idempotent or protected), not that the code
retries automatically today — several integrations rely on the next scheduled run
instead of an in-call loop. `permanent-until-human` failures must stop retrying and
be surfaced (#467); the transition into and out of that state, and staleness
tracking ("last successful sync: 47 days ago"), are #467/#427.

## Outbound operations — retry safety

The table above classifies *failure modes*. This one classifies the *operations*:
every write or side-effecting call this app makes to an external system, by what
happens on an **ambiguous failure** — the request left, the response never came
back, and a retry might double the effect. This is `integrations.OutboundOperations()`;
INT-03 (#466) asserts every such operation has a row, an idempotency class, and a
named safeguard, and that a new outbound-write integration cannot be added without
one. Read-only calls (Ping, discovery, HIBP, update-check) are not listed — there
is nothing to double.

| Operation | Integration | Idempotency | Safeguard on retry | Retry budget |
|---|---|---|---|---|
| `webhook-delivery` | [`webhooks`](#webhooks) | `not-idempotent` | Every attempt replays one immutable event envelope whose `id` (a UUID minted once) is sent as the `Idempotency-Key` request header, so a receiver can de-duplicate a retry; a retry never mints a new event. A permanent HTTP status is not retried at all. | webhookRetryPolicy — up to maxDeliveryAttempts (3), exponential backoff base 5m ×3 with ±20% jitter capped at 6h; 401/403/404/410 and other request-level 4xx go terminal immediately (failed_permanently, integration_failed event, no next_retry_at); 429/503 wait at least the Retry-After hint. next_retry_at is a column, so ProcessWebhookRetries re-scans after a restart — pending retries survive. |
| `caldav-push-event` | [`caldav`](#caldav) | `conditionally-idempotent` | The PUT carries the remote object's own UID and an `If-Match` ETag precondition (putCalendarObject), so a replay updates the same object in place and cannot create a duplicate. A 412 means the remote moved on and is resolved by the local-wins conflict policy, not retried blindly. | No in-call retry loop. The next scheduled calendar_sync run re-pushes while link.ContentHash still shows an unsent local edit; bounded by the job lock and, on a permanent failure, the subscription's terminal-failure state (#467). |
| `notification-ntfy` | [`ntfy`](#ntfy) | `not-idempotent` | A `sent` NotificationDelivery row for (reminder, channel) suppresses any further send for that pair; a `failed`/`pending` row leaves it due. A retry after an ambiguous send is recognized by that key and does not double-notify. | No in-call retry. The reminder stays due for the channel and the next daily reminder run retries — that 24h cadence is itself the backoff. A Retry-After hint is respected where a run would otherwise retry sooner. |
| `notification-gotify` | [`gotify`](#gotify) | `not-idempotent` | A `sent` NotificationDelivery row for (reminder, channel) suppresses any further send for that pair; a `failed`/`pending` row leaves it due. A retry after an ambiguous send is recognized by that key and does not double-notify. | No in-call retry. The reminder stays due for the channel and the next daily reminder run retries — that 24h cadence is itself the backoff. A Retry-After hint is respected where a run would otherwise retry sooner. |
| `notification-webpush` | [`webpush`](#webpush) | `not-idempotent` | A `sent` NotificationDelivery row for (reminder, channel) suppresses any further send for that pair; a `failed`/`pending` row leaves it due. A retry after an ambiguous send is recognized by that key and does not double-notify. | No in-call retry. The reminder stays due for the channel and the next daily reminder run retries — that 24h cadence is itself the backoff. A Retry-After hint is respected where a run would otherwise retry sooner. |
| `email-send-resend` | [`email-resend`](#email-resend) | `not-idempotent` | Per (reminder, channel='email') NotificationDelivery keying plus the legacy Reminder.EmailSent mirror: a reminder already emailed is skipped on the next run, so a retry after an ambiguous send cannot re-send the same digest. The two transports are backends of one best-effort SendEmail. | No in-call retry. A failed send leaves the reminder due for the email channel; the next daily reminder run retries. No tight loop against a broken mail host. |
| `email-send-smtp` | [`email-smtp`](#email-smtp) | `not-idempotent` | Per (reminder, channel='email') NotificationDelivery keying plus the legacy Reminder.EmailSent mirror: a reminder already emailed is skipped on the next run, so a retry after an ambiguous send cannot re-send the same digest. The two transports are backends of one best-effort SendEmail. | No in-call retry. A failed send leaves the reminder due for the email channel; the next daily reminder run retries. No tight loop against a broken mail host. |

`naturally-idempotent` means a bare retry converges; `conditionally-idempotent`
means it is safe only with the stated precondition (`If-Match`, a dedup key);
`not-idempotent` means a retry is only safe because a local delivery record keyed
by the logical event recognizes it as a retry. `DispositionForHTTPStatus` and
`RetryPolicy` (`backend/integrations/retry.go`) are the shared primitives every
loop uses so backoff, jitter, `Retry-After`, and never-retry-a-permanent-status are
decided in one place.

## The integrations

| Integration | Criticality | Direction | Cadence | Data authority | Failure impact | Timeout | SSRF | Retry budget |
|---|---|---|---|---|---|---|---|---|
| [CardDAV contact sync](#carddav) | optional | bidirectional | interactive | shared | silent-staleness | 60s | guarded-when-enabled | No in-call retry. |
| [CalDAV calendar sync](#caldav) | optional | bidirectional | scheduled | shared | silent-staleness | 60s | guarded-when-enabled | No in-call retry. |
| [Immich photo enrichment](#immich) | optional | outbound | scheduled | enrichment | degraded-feature | 30s | guarded-always | No in-call retry. |
| [Paperless-ngx document links](#paperless) | optional | outbound | interactive | remote-authoritative | blocked-workflow | 30s | guarded-always | No retry — the call is inline in a user request. |
| [Seafile file storage](#seafile) | optional | outbound | interactive | remote-authoritative | blocked-workflow | 30s | guarded-always | No retry — inline in a user request; the request fails with a mapped error and the user retries. |
| [Generic WebDAV storage](#webdav) | optional | outbound | interactive | remote-authoritative | blocked-workflow | 30s | guarded-always | No retry — inline in a user request; fails with a mapped error and the user retries. |
| [Outbound webhook delivery](#webhooks) | optional | outbound | event-driven | none | degraded-feature | 15s | guarded-when-enabled | webhookRetryPolicy: maxDeliveryAttempts = 3, exponential backoff base 5m ×3 with ±20% jitter capped at 6h (integrations. |
| [ntfy push notifications](#ntfy) | optional | outbound | scheduled | none | degraded-feature | 15s | guarded-when-enabled | No in-call retry. |
| [Gotify push notifications](#gotify) | optional | outbound | scheduled | none | degraded-feature | 15s | guarded-when-enabled | No in-call retry. |
| [Web Push (VAPID) browser notifications](#webpush) | optional | outbound | scheduled | none | degraded-feature | 15s | guarded-when-enabled | No in-call retry. |
| [Transactional email — Resend API](#email-resend) | optional | outbound | event-driven | none | blocked-workflow | 30s | fixed-endpoint | No retry. |
| [Transactional email — SMTP](#email-smtp) | optional | outbound | event-driven | none | blocked-workflow | 15s | fixed-endpoint | No retry. |
| [OIDC single sign-on provider](#oidc) | required-when-enabled | bidirectional | interactive | remote-authoritative | blocked-workflow | 15s | guarded-when-enabled | No retry. |
| [Have I Been Pwned — breached-password check](#hibp) | optional | outbound | interactive | enrichment | degraded-feature | 5s | fixed-endpoint | No retry. |
| [GitHub releases — update-availability check](#update-check) | optional | outbound | interactive | enrichment | degraded-feature | 3s | guarded-always | No retry. |

## SSRF is a property of this whole surface

Nearly every integration here takes a user- or operator-supplied URL, so it inherits
the SSRF constraint. The guard is `httputil.SafeDialContext`: it re-resolves the
host and pins a public address **at dial time**, so a redirect to an internal
address and DNS rebinding both fail — a pre-flight URL check alone does not. Any
new client inherits this requirement; `TestSSRFClaimsMatchSource` checks that a row
claiming a guarded posture actually references `SafeDialContext` in its source.

| Posture | Meaning | Integrations |
|---|---|---|
| `guarded-always` | every connection through `SafeDialContext`, unconditionally | `immich`, `paperless`, `seafile`, `webdav`, `update-check` |
| `guarded-when-enabled` | guarded only when the operator opts in (`*_BLOCK_PRIVATE_URLS`) | `carddav`, `caldav`, `webhooks`, `ntfy`, `gotify`, `webpush`, `oidc` |
| `fixed-endpoint` | no user-supplied URL — compiled-in vendor/operator host, no SSRF surface | `email-resend`, `email-smtp`, `hibp` |

`fixed-endpoint` rows (Resend, SMTP, HIBP) are recorded rather than omitted: they
carry no user-supplied URL, so there is no dialer to guard — the next reader sees
*why*, instead of assuming every outbound call is dial-guarded. An `unguarded` row
would be a known gap with a named fix; there are none today.

### Operator posture (issue #870)

The `guarded-when-enabled` rows are only guarded when the operator opts in. The
default (off) assumes a **trusted LAN** where webhooks and integrations
legitimately target private hosts. On a deployment reachable from the internet, or
one hosting accounts the operator does not vet, that set must be switched on —
`WEBHOOK_BLOCK_PRIVATE_URLS`, `CALDAV_BLOCK_PRIVATE_URLS`,
`IMMICH_BLOCK_PRIVATE_URLS`, `PAPERLESS_BLOCK_PRIVATE_URLS`,
`SEAFILE_BLOCK_PRIVATE_URLS`, `WEBDAV_BLOCK_PRIVATE_URLS`,
`MONICA_BLOCK_PRIVATE_URLS`, `OIDC_BLOCK_PRIVATE_URLS` all `true`. With them off,
the app-layer guard is inactive and only a network egress policy stands between an
authenticated user's integration URL and an internal or cloud-metadata address
(`169.254.169.254`). Operator checklist: the "SSRF hardening" row in
`docs/security/deployment-baseline.md`; `.env.example` groups the flags under
"SSRF hardening".

## Per-integration detail

Each integration states its concrete required behavior for all seven failure modes
from issue #464 point 2. The transient/permanent class for each mode is fixed by
the table above; these cells say what "handle it" means operationally.

### CardDAV contact sync

<a id="carddav"></a>

Two-way sync of a subscribed remote address book into contacts (full-overwrite reconcile, RFC 6352).

- **Criticality** — optional. Contacts exist and are fully editable without any subscription.
- **Direction** — bidirectional. Pull via REPORT sync-collection; push local changes back with PUT + If-Match.
- **Cadence** — interactive. Runs when the user triggers a subscription sync from the UI — there is no scheduler entry for CardDAV today (calendar sync has one; contacts do not).
- **Data authority** — shared. reconcileContactSync intentionally lets the remote overwrite local edits on change (documented, test-pinned). The remote is a co-authority, not a backup.
- **Failure impact** — silent-staleness. A subscription that stops syncing looks identical to one where nothing changed; contacts silently age. sync_health (#390) is the surface that makes it visible.
- **Timeout** — 60s. services.contactSyncRequestTimeout on the http.Client; a per-request context deadline bounds each REPORT/PUT.
- **Retry budget** — No in-call retry. The user re-triggers; the next run re-fetches from the stored sync-token (or does a full refetch if the token was rejected). No partial state is committed on failure.
- **SSRF** — guarded-when-enabled. A custom RoundTripper wraps httputil.SafeDialContext; address filtering is applied only when CALDAV_BLOCK_PRIVATE_URLS is set (shared with CalDAV). Default off so LAN DAV servers work.
- **Source** — `backend/services/contact_sync_service.go`
- **Failure behavior verified by** — #465 (INT-02) failure matrix; contact_sync_hostile_input_test.go; sync_health advance test.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Sync run fails cleanly; sync_health goes to failing with the error; local contacts and the stored sync-token are untouched; the user sees the failure on the subscription. |
| `timeout` | transient | The 60s deadline fires; the run ends as a failure (not a success); no half-applied batch — reconcile is all-or-nothing per run. |
| `auth-expiry` | permanent-until-human | 401 → terminal auth-failure state on the subscription with a re-enter-credentials prompt; stop auto-attempts until the user acts (#467). |
| `authz-revoked` | permanent-until-human | 403 → same terminal state as auth expiry, worded as 'access to this address book was revoked'. |
| `malformed-response` | transient | Unparseable/truncated multistatus → run fails; nothing imported; a truncated body must never be read as 'these contacts were deleted remotely'. |
| `rate-limited` | transient | 429/503 → back off honoring Retry-After; the run fails and is retried on the next user action / future scheduled run, not hammered. |
| `remote-resource-deleted` | permanent-until-human | A contact 404/410 on MultiGet is treated as 'removed on the server' only when the sync-collection delta says so; a bare fetch failure never deletes the local contact — it is left and reported. |

### CalDAV calendar sync

<a id="caldav"></a>

Imports a subscribed remote calendar's events as activities/life events; optionally pushes local edits back (RFC 4791).

- **Criticality** — optional. The calendar/timeline works without any subscription.
- **Direction** — bidirectional. Read-primary (calendar-query REPORT). Write-back is gated on CALDAV_TWO_WAY_ENABLED (default off); when on, local edits go out as PUT.
- **Cadence** — scheduled. Every CALDAV_SYNC_INTERVAL_HOURS (default 6h) via the calendar_sync job, plus on user trigger. Rate-limited by a job lock so a slow remote cannot overlap runs.
- **Data authority** — shared. The remote calendar is the source for imported events; two-way mode makes them co-authoritative for the fields it pushes (attendees excluded — they cannot converge).
- **Failure impact** — silent-staleness. A failing scheduled sync ages the imported timeline with nothing visibly wrong; sync_health (#390) surfaces it.
- **Timeout** — 60s. services.calendarRequestTimeout on the http.Client; the push phase also wraps each PUT in a context.WithTimeout of the same value. The timeout is far below the 6h cadence so runs cannot pile up.
- **Retry budget** — No in-call retry. The job releases its lock on failure; the next scheduled run (≤ interval) retries. Two-way push overwrites the remote unconditionally on the next run rather than tracking a retry queue.
- **SSRF** — guarded-when-enabled. Custom RoundTripper over httputil.SafeDialContext; filtering applied only when CALDAV_BLOCK_PRIVATE_URLS is set.
- **Source** — `backend/services/calendar_sync_service.go`
- **Failure behavior verified by** — #465 (INT-02); calendar_sync_hostile_input_test.go; calendar_two_way_test.go; cadence_job_lock_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Job run records failure, releases the job lock, leaves imported activities and CalendarEventLink rows intact; sync_health → failing. |
| `timeout` | transient | 60s deadline fires; run ends as failure with the lock released; no partial import — event application is transactional per event and a mid-stream cut stops cleanly. |
| `auth-expiry` | permanent-until-human | 401 → terminal auth-failure on the subscription; scheduled attempts stop bothering the user beyond the first clear signal (#467). |
| `authz-revoked` | permanent-until-human | 403 → terminal 'calendar access revoked' state, same handling as auth expiry. |
| `malformed-response` | transient | Unparseable iCalendar/multistatus → run fails; no events imported; a truncated feed must not be read as 'the remaining events were deleted'. |
| `rate-limited` | transient | 429/503 → honor Retry-After, fail the run, retry next interval; never tighten the schedule in response. |
| `remote-resource-deleted` | permanent-until-human | An event gone from the remote is applied as a local deletion only when the calendar-query result set says it is absent; a fetch 404 for one object fails that object and is reported, it does not delete the linked activity. |

### Immich photo enrichment

<a id="immich"></a>

Matches contacts to Immich people and pulls a face thumbnail as a profile photo.

- **Criticality** — optional. Pure enrichment; every contact is complete without it.
- **Direction** — outbound.
- **Cadence** — scheduled. Every IMMICH_SYNC_INTERVAL_HOURS (default 6h) via the immich_sync job, plus on user trigger from the Immich settings page.
- **Data authority** — enrichment. Immich owns the person catalog and the thumbnails; we keep only a derived photo. Removing the integration leaves contacts intact minus Immich-sourced photos.
- **Failure impact** — degraded-feature. New matches stop appearing and thumbnails stop refreshing; existing data is unaffected.
- **Timeout** — 30s. services.immichRequestTimeout on the shared http.Client (also IdleConnTimeout 30s / TLSHandshakeTimeout 10s / ResponseHeaderTimeout 15s on the transport).
- **Retry budget** — No in-call retry. A person that fails to sync is skipped and retried on the next scheduled run; nothing is deleted.
- **SSRF** — guarded-always. immichPrivateBlockingDialContext → httputil.SafeDialContext on the shared transport; every connection is re-resolved and pinned to a public address.
- **Source** — `backend/services/immich_client.go`, `backend/services/immich_service.go`
- **Failure behavior verified by** — #465 (INT-02); immich_fault_injection_test.go; immich_fake_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Sync job records failure and releases its lock; existing profile photos and match links are kept; the failure shows on the Immich settings page. |
| `timeout` | transient | 30s deadline fires; the run ends as a failure; a person mid-fetch is skipped, not half-written. |
| `auth-expiry` | permanent-until-human | 401 (API key rotated) → terminal 'reconnect Immich' state with the key re-entry prompt; stop retrying until fixed (#467). |
| `authz-revoked` | permanent-until-human | 403 → same terminal state; message names a permissions problem on the Immich side. |
| `malformed-response` | transient | Unparseable/oversized body → that person is skipped with a logged warning; enrichment already applied is not rolled back and nothing new is written from a bad body. |
| `rate-limited` | transient | 429/503 → back off honoring Retry-After; the run ends and resumes next interval. |
| `remote-resource-deleted` | permanent-until-human | A person/asset now 404 → drop the stale match link and, if the photo came from it, mark the photo for refresh; never delete the contact. |

### Paperless-ngx document links

<a id="paperless"></a>

Links contacts to documents in a Paperless-ngx instance and fetches titles/previews on demand.

- **Criticality** — optional. Only the document-linking feature depends on it.
- **Direction** — outbound.
- **Cadence** — interactive. Called inline when the user browses, links, or opens a document from a contact.
- **Data authority** — remote-authoritative. Paperless is the system of record for the document; we store only a document ID. If the instance is gone the reference cannot be reconstructed.
- **Failure impact** — blocked-workflow. The user tries to open or attach a document and cannot; already-stored links remain but do not resolve.
- **Timeout** — 30s. services.paperlessRequestTimeout on the http.Client; transport IdleConnTimeout 30s / TLSHandshakeTimeout 10s / ResponseHeaderTimeout 15s.
- **Retry budget** — No retry — the call is inline in a user request. The request fails with a mapped error and the user retries.
- **SSRF** — guarded-always. paperlessPrivateBlockingDialContext → httputil.SafeDialContext, unconditionally.
- **Source** — `backend/services/paperless_client.go`, `backend/services/paperless_service.go`
- **Failure behavior verified by** — #465 (INT-02); paperless_fake_test.go; controllers/paperless_real_db_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | The request returns a mapped 'Paperless unreachable' error; no stored links are touched. |
| `timeout` | transient | 30s deadline → mapped timeout error to the caller; no partial link written. |
| `auth-expiry` | permanent-until-human | 401 → mapped 'Paperless credentials invalid' error pointing at the settings page; stored links are kept for when it is fixed (#467). |
| `authz-revoked` | permanent-until-human | 403 → mapped 'Paperless access denied' error; same as auth expiry for stored links. |
| `malformed-response` | transient | Unparseable body → mapped 'unexpected Paperless response' error; nothing is inferred from a partial parse. |
| `rate-limited` | transient | 429/503 → mapped 'Paperless busy, try again' error surfacing Retry-After to the user where present. |
| `remote-resource-deleted` | permanent-until-human | 404 on a previously-valid document ID → surface 'this document no longer exists in Paperless' and offer to remove the dangling link; do not silently drop it. |

### Seafile file storage

<a id="seafile"></a>

Stores and retrieves contact attachments in a Seafile library via its Web API.

- **Criticality** — optional. Only attachment storage routed to Seafile depends on it.
- **Direction** — outbound.
- **Cadence** — interactive. Called inline on attachment upload, download, and listing.
- **Data authority** — remote-authoritative. Seafile holds the file bytes; we keep a repo/path reference. Losing the library loses the attachment content.
- **Failure impact** — blocked-workflow. Upload or download of a Seafile-backed attachment fails; references remain but do not resolve.
- **Timeout** — 30s. services.seafileRequestTimeout on the http.Client; transport IdleConnTimeout 30s / TLSHandshakeTimeout 10s / ResponseHeaderTimeout 15s.
- **Retry budget** — No retry — inline in a user request; the request fails with a mapped error and the user retries.
- **SSRF** — guarded-always. seafilePrivateBlockingDialContext → httputil.SafeDialContext, unconditionally.
- **Source** — `backend/services/seafile_client.go`, `backend/services/seafile_service.go`
- **Failure behavior verified by** — #465 (INT-02); seafile_fake_test.go; controllers/seafile_real_db_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Mapped 'Seafile unreachable' error to the caller; no reference rows changed. |
| `timeout` | transient | 30s deadline → mapped timeout error; a partial upload is not recorded as a stored attachment. |
| `auth-expiry` | permanent-until-human | 401 (token expired) → mapped 'reconnect Seafile' error; existing references kept (#467). |
| `authz-revoked` | permanent-until-human | 403 → mapped 'Seafile access denied'; same as auth expiry for references. |
| `malformed-response` | transient | Unparseable body → mapped 'unexpected Seafile response'; no reference inferred from a partial parse. |
| `rate-limited` | transient | 429/503 → mapped 'Seafile busy' error, Retry-After surfaced where present. |
| `remote-resource-deleted` | permanent-until-human | 404 on a stored path → 'this file is no longer in Seafile', offer to remove the reference; never drop it silently. |

### Generic WebDAV storage

<a id="webdav"></a>

Stores and retrieves contact attachments on any RFC 4918 WebDAV server.

- **Criticality** — optional. Only attachment storage routed to WebDAV depends on it.
- **Direction** — outbound.
- **Cadence** — interactive. Called inline on attachment upload, download, and listing (PROPFIND/GET/PUT).
- **Data authority** — remote-authoritative. The WebDAV server holds the file bytes; we keep a URL/path reference.
- **Failure impact** — blocked-workflow. Upload or download of a WebDAV-backed attachment fails; references remain but do not resolve.
- **Timeout** — 30s. services.webdavRequestTimeout on the http.Client; transport IdleConnTimeout 30s / TLSHandshakeTimeout 10s / ResponseHeaderTimeout 15s. XXE is blocked in the PROPFIND parser (webdav_client_xxe_test.go).
- **Retry budget** — No retry — inline in a user request; fails with a mapped error and the user retries.
- **SSRF** — guarded-always. webdavPrivateBlockingDialContext → httputil.SafeDialContext, unconditionally.
- **Source** — `backend/services/webdav_client.go`, `backend/services/webdav_service.go`
- **Failure behavior verified by** — #465 (INT-02); webdav_fake_test.go; webdav_client_xxe_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Mapped 'WebDAV unreachable' error; no reference rows changed. |
| `timeout` | transient | 30s deadline → mapped timeout error; a partial PUT is not recorded as a stored attachment. |
| `auth-expiry` | permanent-until-human | 401 → mapped 'WebDAV credentials invalid' error to the settings page; references kept (#467). |
| `authz-revoked` | permanent-until-human | 403 → mapped 'WebDAV access denied'; references kept. |
| `malformed-response` | transient | Unparseable/entity-expanded PROPFIND body → rejected by the hardened parser and surfaced as 'unexpected WebDAV response'; nothing inferred. |
| `rate-limited` | transient | 429/503 → mapped 'WebDAV busy' error, Retry-After surfaced where present. |
| `remote-resource-deleted` | permanent-until-human | 404 on a stored path → 'this file is no longer on the WebDAV server', offer to remove the reference. |

### Outbound webhook delivery

<a id="webhooks"></a>

POSTs a JSON envelope to operator-configured receiver URLs when app events fire.

- **Criticality** — optional. Nothing in the app depends on a receiver being up.
- **Direction** — outbound.
- **Cadence** — event-driven. Fired from a tracked goroutine on the event; failed deliveries are retried by the webhook_retries job every 5 minutes.
- **Data authority** — none. A transport for events we already hold; the receiver is not a data source.
- **Failure impact** — degraded-feature. The receiver's downstream automation misses events; delivery rows record every attempt and outcome.
- **Timeout** — 15s. http.Client.Timeout on both deliveryClient and guardedDeliveryClient; guarded client also caps redirects at 3.
- **Retry budget** — webhookRetryPolicy: maxDeliveryAttempts = 3, exponential backoff base 5m ×3 with ±20% jitter capped at 6h (integrations.RetryPolicy). A permanent status (401/403/404/410 or another request-level 4xx) is NOT retried — the row is marked failed_permanently on that attempt and integration_failed is raised immediately. 429/503 wait at least the Retry-After hint. Retry state is a webhook_deliveries row (NextRetryAt), so it survives a restart; ProcessWebhookRetries runs under a job lock.
- **SSRF** — guarded-when-enabled. clientFor(cfg) returns the SafeDialContext-guarded client only when WEBHOOK_BLOCK_PRIVATE_URLS is set (default off — self-hosted installs legitimately target LAN receivers). Guarding is in the dialer so redirect targets are checked too.
- **Source** — `backend/services/webhook_service.go`
- **Failure behavior verified by** — #465 (INT-02), #466 (INT-03 retry safety); webhook_delivery_test.go; webhook_ssrf_test.go; webhook_service_job_lock_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Dialer sentinel (ErrWebhookUnreachable / ErrWebhookPrivateAddress) stored in the delivery row's Error; attempt counted; NextRetryAt set per webhookRetryPolicy until attempt 3, then integration_failed. |
| `timeout` | transient | 15s deadline → delivery row records the timeout, attempt counted, retry scheduled with backoff; the goroutine never blocks the event that triggered it. |
| `auth-expiry` | permanent-until-human | A receiver 401 is terminal immediately (failed_permanently, terminal_reason=auth-expiry, integration_failed on that attempt, no retry) — webhooks carry a signing secret, not a refreshable credential, so a 401 will not self-heal; surfaced on the webhook's delivery health (#467). |
| `authz-revoked` | permanent-until-human | 403 handled exactly like 401: terminal on the failing attempt, terminal_reason=authz-revoked, no retry. |
| `malformed-response` | transient | The response body is not used; any 2xx is success. An unclassified 5xx is retried within budget; an unclassified 4xx is treated as permanent (terminal_reason=client-error). |
| `rate-limited` | transient | 429/503 → recorded and retried with exponential backoff, but never sooner than the Retry-After hint (integrations.ParseRetryAfter); still bounded by the 3-attempt budget. |
| `remote-resource-deleted` | permanent-until-human | 404/410 on the receiver URL is terminal immediately (terminal_reason=remote-resource-deleted, no retry). A deleted receiver endpoint is an operator configuration problem surfaced on the webhook. |

### ntfy push notifications

<a id="ntfy"></a>

Posts reminder notifications to a user-configured ntfy topic.

- **Criticality** — optional. One notification channel of several; reminders still exist in-app.
- **Direction** — outbound.
- **Cadence** — scheduled. From the daily reminder job (and test-notification button). Event-driven for any immediate notifications.
- **Data authority** — none. Transport for reminders we own.
- **Failure impact** — degraded-feature. A reminder push is missed; the reminder is not marked sent for that channel, so the next run retries it. notification_health (#422) surfaces the failure rate.
- **Timeout** — 15s. clientFor(cfg) — the shared webhook delivery client (15s). No dedicated ntfy timeout.
- **Retry budget** — No in-call retry. A failed send records a NotificationDelivery row with status 'failed'; the reminder stays due for that channel and the next scheduled run retries. (reminder, channel) keying de-duplicates so a retry cannot double-send a reminder that actually landed.
- **SSRF** — guarded-when-enabled. postNotificationJSON uses clientFor(cfg); the SafeDialContext guard applies when WEBHOOK_BLOCK_PRIVATE_URLS is set. URLs are validated as http(s) at save time (normalizeNotificationURL); ErrNotificationPrivateAddress is stored in the delivery row on a blocked address.
- **Source** — `backend/services/notification_service.go`
- **Failure behavior verified by** — #465 (INT-02); notification_service_test.go; notification_health_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Send fails; 'failed' NotificationDelivery row with the error; reminder stays due; retried next run. |
| `timeout` | transient | 15s deadline → 'failed' row; reminder stays due; no partial send that could be re-sent as a duplicate. |
| `auth-expiry` | permanent-until-human | ntfy topics are typically unauthenticated; a 401 (protected topic, bad token) is recorded as a failure and, on repeated failures, surfaced via notification_health as a channel that needs attention (#467). |
| `authz-revoked` | permanent-until-human | 403 handled as 401. |
| `malformed-response` | transient | The ntfy response body is not consumed for meaning; a non-2xx status is recorded as a failed delivery with the code. |
| `rate-limited` | transient | 429/503 → failed delivery row; the reminder is retried on the next scheduled run rather than immediately, which is itself the backoff. Retry-After is respected where the run would otherwise retry sooner. |
| `remote-resource-deleted` | permanent-until-human | Not applicable in the usual sense (a topic is created on first publish); a 404 is recorded as a failed delivery and, if persistent, flagged as a misconfigured channel. |

### Gotify push notifications

<a id="gotify"></a>

Posts reminder notifications to a user-configured Gotify server (X-Gotify-Key app token).

- **Criticality** — optional. One notification channel of several; reminders still exist in-app.
- **Direction** — outbound.
- **Cadence** — scheduled. From the daily reminder job (and test-notification button). Event-driven for any immediate notifications.
- **Data authority** — none. Transport for reminders we own.
- **Failure impact** — degraded-feature. A reminder push is missed; the reminder is not marked sent for that channel, so the next run retries it. notification_health (#422) surfaces the failure rate.
- **Timeout** — 15s. clientFor(cfg) — the shared webhook delivery client (15s). No dedicated ntfy timeout.
- **Retry budget** — No in-call retry. A failed send records a NotificationDelivery row with status 'failed'; the reminder stays due for that channel and the next scheduled run retries. (reminder, channel) keying de-duplicates so a retry cannot double-send a reminder that actually landed.
- **SSRF** — guarded-when-enabled. sendGotifyMessage → postNotificationJSON → clientFor(cfg); SafeDialContext guard applies when WEBHOOK_BLOCK_PRIVATE_URLS is set. The app token is decrypted from NotificationConfig at send time.
- **Source** — `backend/services/notification_service.go`
- **Failure behavior verified by** — #465 (INT-02); notification_service_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Send fails; 'failed' NotificationDelivery row; reminder stays due; retried next run. |
| `timeout` | transient | 15s deadline → 'failed' row; reminder stays due; no re-sendable partial. |
| `auth-expiry` | permanent-until-human | Gotify uses an app token; a 401 means the token was revoked/rotated — recorded as failed and surfaced through notification_health as a channel needing re-configuration (#467). |
| `authz-revoked` | permanent-until-human | 403 handled as 401. |
| `malformed-response` | transient | Response body not consumed for meaning; non-2xx recorded with the status code. |
| `rate-limited` | transient | 429/503 → failed delivery row; retried on the next scheduled run; Retry-After respected where it would otherwise retry sooner. |
| `remote-resource-deleted` | permanent-until-human | 404 (application deleted on the Gotify side) → recorded as failed; if persistent, flagged as a misconfigured channel. |

### Web Push (VAPID) browser notifications

<a id="webpush"></a>

Delivers reminder notifications to browser push endpoints (RFC 8291/8292) for registered PushSubscriptions.

- **Criticality** — optional. One notification channel; reminders still exist in-app.
- **Direction** — outbound.
- **Cadence** — scheduled. From the daily reminder job; event-driven for immediate notifications.
- **Data authority** — none. Transport for reminders we own. The push service (FCM/Mozilla/etc.) is chosen by the subscriber's browser, not configured here.
- **Failure impact** — degraded-feature. A push is missed; the reminder stays due for the channel and retries next run. A dead subscription is pruned so it stops failing.
- **Timeout** — 15s. webpush.Options.HTTPClient = clientFor(cfg) (15s). RecordSize is clamped to webpush.MaxRecordSize.
- **Retry budget** — No in-call retry. Failed send → 'failed' delivery row, reminder stays due, next run retries. A 404/410 from the push service permanently removes the PushSubscription row (it will never work again) — the one place a notification channel self-heals by dropping state.
- **SSRF** — guarded-when-enabled. clientFor(cfg); SafeDialContext guard applies when WEBHOOK_BLOCK_PRIVATE_URLS is set. The endpoint URL comes from the browser's PushManager, not user text input, but is still an arbitrary URL so the guard matters.
- **Source** — `backend/services/notification_service.go`
- **Failure behavior verified by** — #465 (INT-02); notification_service_test.go; push_subscription_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Send fails; 'failed' delivery row; reminder stays due; retried next run; the subscription is kept (could be transient). |
| `timeout` | transient | 15s deadline → 'failed' row; reminder stays due; subscription kept. |
| `auth-expiry` | permanent-until-human | A 401/403 from the push service means the VAPID keys are wrong for this endpoint — recorded as failed; if it affects all subscriptions it is a server-config problem surfaced via notification_health (#467), not a per-user re-auth. |
| `authz-revoked` | permanent-until-human | 403 handled as 401 (usually a VAPID audience/key mismatch). |
| `malformed-response` | transient | The push service response body is not meaningful; the HTTP status drives handling. An unexpected non-2xx that is not 404/410 is a retryable failure. |
| `rate-limited` | transient | 429/503 → 'failed' row; retried next run (the schedule is the backoff); Retry-After respected where present. |
| `remote-resource-deleted` | permanent-until-human | 404/410 → the subscription is permanently dead: delete the PushSubscription row so it stops being tried, and record the delivery as failed with that reason. |

### Transactional email — Resend API

<a id="email-resend"></a>

Sends transactional email (password reset, invitations, reminder digests) through the Resend HTTP API.

- **Criticality** — optional. Optional as a channel, but note: with no email channel configured, password-reset and invitation emails cannot be delivered at all. SendEmail is a no-op when unconfigured.
- **Direction** — outbound.
- **Cadence** — event-driven. On password reset / invite / verification; also from the daily reminder job for email digests.
- **Data authority** — none. Transport only.
- **Failure impact** — blocked-workflow. A user cannot complete a password reset or accept an invite until email works or the operator uses an alternate path.
- **Timeout** — 30s. services.resendRequestTimeout on an explicit http.Client passed via resend.NewCustomClient (services.newResendClient). Before INT-02 this inherited the SDK's 60s default, which was not a value this project chose.
- **Retry budget** — No retry. Best-effort: SendEmail tries every configured channel (Resend and/or SMTP) and returns success if at least one succeeds, a combined error only if all fail. The caller decides what a total failure means (password reset surfaces it; a reminder digest logs it).
- **SSRF** — fixed-endpoint. Destination is Resend's fixed API host (api.resend.com); no user-supplied URL, so there is no SSRF surface. The API key is operator config.
- **Source** — `backend/services/mailer.go`
- **Failure behavior verified by** — #465 (INT-02); mailer_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | SDK returns an error; logged as 'Failed to send email via Resend'; if SMTP is also configured it is tried; if not, SendEmail returns the combined error to the caller. |
| `timeout` | transient | Bounded by services.resendRequestTimeout (30s) on the explicit client. On error, same fallback-to-SMTP-or-combined-error path. |
| `auth-expiry` | permanent-until-human | 401 (API key revoked) → error surfaced to the caller / logged; a persistent 401 is an operator problem — the fix is a new key in config, there is no runtime re-auth. |
| `authz-revoked` | permanent-until-human | 403 (domain not verified, sending disabled) handled as 401: surfaced/logged, operator must fix the Resend account. |
| `malformed-response` | transient | Handled by the SDK; a decode error becomes a send error and the fallback/reporting path runs. |
| `rate-limited` | transient | 429 → send error this attempt; falls back to SMTP if configured. There is no queue, so a rate-limited reminder digest is simply logged as failed for that run. |
| `remote-resource-deleted` | permanent-until-human | Not applicable — there is no addressable remote resource, only the send endpoint. |

### Transactional email — SMTP

<a id="email-smtp"></a>

Sends the same transactional email through an operator-configured SMTP server (STARTTLS or implicit TLS).

- **Criticality** — optional. Same as Resend: optional as a channel, but the only email path if Resend is not configured.
- **Direction** — outbound.
- **Cadence** — event-driven. Same triggers as the Resend path.
- **Data authority** — none. Transport only.
- **Failure impact** — blocked-workflow. Same as Resend — password reset / invite delivery is blocked until it works.
- **Timeout** — 15s. services.smtpDialTimeout (15s) bounds the TCP/TLS connect via net.DialTimeout / tls.DialWithDialer; services.smtpDeadline (30s) is set on the connection for the whole SMTP conversation. Before INT-02 the path used smtp.SendMail / tls.Dial with no deadline.
- **Retry budget** — No retry. Part of the same best-effort multi-channel SendEmail: SMTP failure alone is tolerated if Resend succeeded; if SMTP is the only channel, the caller gets the error.
- **SSRF** — fixed-endpoint. SMTPHost is operator configuration read from env, not a per-request user value, and net/smtp is not routed through SafeDialContext. Treated as a fixed operator endpoint rather than an SSRF surface; if SMTP host ever becomes user-supplied this row must change.
- **Source** — `backend/services/mailer.go`
- **Failure behavior verified by** — #465 (INT-02); mailer_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Dial error → logged 'Failed to send email via SMTP'; combined-error path if SMTP is the only channel. |
| `timeout` | transient | smtpDialTimeout bounds the connect; smtpDeadline bounds a server that connects then stalls mid-dialogue. Either way the send returns an error and takes the combined-error path — it never hangs the delivery goroutine. |
| `auth-expiry` | permanent-until-human | SMTP auth failure (535) → send error, surfaced/logged; operator must fix credentials in config. |
| `authz-revoked` | permanent-until-human | Relay-denied / not-permitted (550/554) → send error, surfaced/logged; operator/relay configuration problem. |
| `malformed-response` | transient | An unexpected SMTP reply code aborts that step with a wrapped error ('smtp mail from', 'smtp data', …); nothing partial is considered sent. |
| `rate-limited` | transient | Greylisting / 421 / 4xx throttle → send error for this attempt; no queue, so a throttled reminder digest is logged as failed for the run. |
| `remote-resource-deleted` | permanent-until-human | Not applicable. |

### OIDC single sign-on provider

<a id="oidc"></a>

Authenticates users via an external OpenID Connect provider (discovery, authorization-code + PKCE, ID-token verification, optional UserInfo).

- **Criticality** — required-when-enabled. When OIDC is the login method, an unreachable provider / failed discovery / unreachable JWKS or UserInfo means no OIDC account can authenticate. Local password accounts (if any) are unaffected.
- **Direction** — bidirectional. Inbound: the browser redirect back with the code. Outbound: discovery at startup, token exchange, JWKS fetch, UserInfo.
- **Cadence** — interactive. Token exchange and UserInfo happen inline in the login callback. Discovery runs once at startup (InitOIDCProvider) and is cached for the process lifetime.
- **Data authority** — remote-authoritative. The provider is authoritative for identity (subject, email, name). We store the subject/provider mapping; we cannot mint an account without a successful exchange.
- **Failure impact** — blocked-workflow. The user reaches the login callback and cannot get a session. Immediate and obvious (unlike the sync cases), so no silent-staleness risk.
- **Timeout** — 15s. services.oidcRequestTimeout on services.newOIDCHTTPClient, threaded through discovery, token exchange, JWKS and UserInfo via oidc.ClientContext. Before INT-02 these ran on the go-oidc / x/oauth2 default transports with no timeout.
- **Retry budget** — No retry. A failed exchange fails the login; the user retries by starting the flow again. Discovery is not re-fetched after startup, so a provider that changes its endpoints needs a restart.
- **SSRF** — guarded-when-enabled. newOIDCHTTPClient wires httputil.SafeDialContext onto the transport (used for discovery/token/JWKS/UserInfo) only when OIDC_BLOCK_PRIVATE_URLS is set. Default off: a LAN identity provider (Authentik/Keycloak on the same Docker network) is a common self-hosted setup. Same opt-in shape as the other *_BLOCK_PRIVATE_URLS integrations.
- **Source** — `backend/services/oidc_service.go`
- **Failure behavior verified by** — #465 (INT-02); oidc_service_test.go; oidc_attack_matrix_test.go; oidc_userinfo_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Startup: InitOIDCProvider returns an error and OIDC is not enabled (login falls back to whatever else is configured). Runtime: the token/UserInfo call fails, the callback returns a login error, no partial session is created. With OIDC_BLOCK_PRIVATE_URLS on, a provider host that resolves to a private address is refused at dial time (ErrOIDCPrivateAddress). |
| `timeout` | transient | Bounded by oidcRequestTimeout (15s) on the client, in addition to the request context deadline; on timeout the callback fails with a login error. No session, no partial user row. |
| `auth-expiry` | permanent-until-human | Not applicable to a client credential (the client secret is operator config, a 401 there is a setup error surfaced at the callback). For the end user, an expired provider session just means they re-authenticate at the provider. |
| `authz-revoked` | permanent-until-human | Provider returns access_denied / the user's account is disabled provider-side → the callback surfaces 'sign-in was refused by the provider'; no local account is created or unlocked. |
| `malformed-response` | transient | ID-token signature/claims verification failure, UserInfo sub mismatch (OIDC Core 5.3.2), or an unparseable discovery doc → the flow aborts with a specific error; a claim is never trusted from a body that failed verification. |
| `rate-limited` | transient | 429/503 from the token or UserInfo endpoint → the login fails for that attempt; the user retries. There is no background retry to rate-limit. |
| `remote-resource-deleted` | permanent-until-human | The OIDC client/registration deleted provider-side → discovery or exchange fails; surfaced as a provider configuration error for the operator, not a per-user problem. |

### Have I Been Pwned — breached-password check

<a id="hibp"></a>

Checks a candidate password against HIBP's k-anonymity range API during registration / password change / reset (only a 5-char SHA-1 prefix ever leaves the process).

- **Criticality** — optional. Advisory only, and off unless HIBP checking is enabled (ASVS 2.1.7, issue #376).
- **Direction** — outbound.
- **Cadence** — interactive. Inline in the registration / change-password / reset-confirm request.
- **Data authority** — enrichment. Adds a 'this password is known-breached' signal; owns nothing of ours.
- **Failure impact** — degraded-feature. Fails open by design: any error means the breach check is skipped for that request and the password is allowed. A third-party outage must never block registering or changing a password on a self-hosted app.
- **Timeout** — 5s. hibpClient = &http.Client{Timeout: 5s} — deliberately shorter than the webhook client because it sits inline in an auth request and must fail fast.
- **Retry budget** — No retry. On any error the function logs a warning and returns (false, err); the caller treats that as 'not breached, HIBP unavailable' and proceeds. The next password operation re-checks.
- **SSRF** — fixed-endpoint. Destination is HIBP's fixed host (hibpAPIBaseURL — a var only so tests can point at httptest; there is no env var or config field for it). No user-supplied URL, so no SSRF surface: hibpClient is a bare http.Client{Timeout: 5s} with no custom dialer. If HIBP checking ever accepts a self-hosted mirror URL this row must move to guarded.
- **Source** — `backend/services/hibp_service.go`
- **Failure behavior verified by** — #465 (INT-02); hibp_service_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Warning logged ('allowing password, fail open'); returns (false, err); the auth request proceeds normally. |
| `timeout` | transient | 5s deadline fires → same fail-open path; the auth request is not held up. |
| `auth-expiry` | permanent-until-human | Not applicable — the range API is unauthenticated. |
| `authz-revoked` | permanent-until-human | Not applicable — unauthenticated. |
| `malformed-response` | transient | A body that does not parse as HIBP range lines → treated as 'no match found' / unavailable; fail open; never blocks on a garbled response. |
| `rate-limited` | transient | 429 → fail open for that request (the padding header is already sent per HIBP's guidance); no retry storm because there is no retry. |
| `remote-resource-deleted` | permanent-until-human | Not applicable — every prefix is a valid range query. |

### GitHub releases — update-availability check

<a id="update-check"></a>

Asks the GitHub releases API whether a newer release of this project exists, for the admin system-status page.

- **Criticality** — optional. Off by default (UPDATE_CHECK_ENABLED). Strictly informational when on — nothing blocks or errors on it.
- **Direction** — outbound.
- **Cadence** — interactive. Computed when an admin loads system-status; the result is memoized for updateCheckCacheTTL (6h). Errors are never cached.
- **Data authority** — enrichment. Adds a 'newer version available' string; owns nothing.
- **Failure impact** — degraded-feature. The system-status page omits the update line or shows it as unavailable. No other surface is affected.
- **Timeout** — 3s. updateCheckTimeout = 3s, applied as both the client timeout and a context deadline, so an injected test transport is still bounded. Fails fast because it feeds an admin snapshot.
- **Retry budget** — No retry. A failed lookup is not cached and is retried on the next system-status load.
- **SSRF** — guarded-always. newUpdateCheckClient wires httputil.SafeDialContext; the host re-resolves and pins a public address at dial time (ASVS 5.2.6). Destination is a fixed GitHub API URL (a var only for tests).
- **Source** — `backend/services/update_check.go`
- **Failure behavior verified by** — #465 (INT-02); update_check_test.go.

| Failure mode | Class | Required behavior |
|---|---|---|
| `unreachable-host` | transient | Lookup returns an error; not cached; the status page shows the update state as unavailable; retried next load. |
| `timeout` | transient | 3s deadline → same as unreachable; the admin snapshot is never held open on it. |
| `auth-expiry` | permanent-until-human | Not applicable — the call is unauthenticated (public releases endpoint). |
| `authz-revoked` | permanent-until-human | Not applicable. |
| `malformed-response` | transient | An unparseable release body → treated as 'could not determine'; never surfaced as a spurious 'update available'. |
| `rate-limited` | transient | GitHub 403/429 with rate-limit headers → treated as unavailable for this load; the 6h memoization of the last success already keeps call volume trivially low. |
| `remote-resource-deleted` | permanent-until-human | A 404 for the releases endpoint (repo moved/renamed) → treated as 'could not determine'; it is a build-time constant, so this would be a project bug, not a runtime condition. |

## Adding an integration

1. Add a constructor to `backend/integrations/entries.go` and list it in `Registry()`.
2. Fill every axis, the timeout, the retry budget, the SSRF posture, and all seven
   `Behavior` cells. `TestRegistryInvariants` and `TestEveryFailureModeHasBehavior`
   fail on a missing field.
3. List the implementing `backend/services/*.go` files in `SourceFiles`.
   `TestEveryOutboundClientIsClassified` fails if a `services` file opens an outbound
   client and no row claims it (add it here, or — only for a client that reuses
   another integration's transport — to `nonIntegrationClients` with a reason).
4. Route the transport through `httputil.SafeDialContext` unless the destination is
   a compiled-in vendor host. `TestSSRFClaimsMatchSource` enforces the claim.
5. Regenerate this doc and commit the diff.

Note the scope of the structural check: it covers `backend/services/`. A broader
semgrep rule for *any* unguarded outbound client anywhere in the tree is issue
#609; this matrix is the `services/`-scoped instance of that mechanism.

## Related

- **#465 (INT-02)** — makes each failure above actually happen and asserts the behavior.
- **#466 (INT-03)** — retry safety for the outbound operations (idempotency, backoff, restart-survival).
- **#467 (INT-04)** — the permanent-failure terminal state and staleness surfacing.
- **#488 (DOC-03)** — operator-facing integration ownership; cites this document.
- **#390 / #422 / #427 / #428** — the sync-health, delivery-health, last-known-good and alerting surfaces a failure must reach.
- **#373 / #609** — webhook SSRF and the tree-wide unguarded-client rule.
- `httputil.SafeDialContext` (`backend/httputil/safedial.go`) — the dial-time guard every guarded row relies on.
