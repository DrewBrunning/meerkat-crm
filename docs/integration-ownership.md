---
title: Integrations (ownership & diagnostics)
nav_order: 20
---

# Integrations: ownership, diagnostics, and what breaks

Mycorrhizal CRM connects to a lot of software you run or subscribe to: CardDAV
and CalDAV servers, Immich, Paperless, Seafile, generic WebDAV, webhook
receivers, ntfy/Gotify/Web Push, transactional email (Resend or SMTP), an OIDC
provider, and the HIBP/update checks. Each of those can break for reasons that
have nothing to do with Mycorrhizal. This page is the operator-facing half of
the [integration classification matrix](int-01-integration-classification-matrix.md)
(INT-01, issue #464): who owns what, what a failure looks like, where to look
first, and what happens to your data if you remove an integration. The matrix
is the engineering half — retry budgets, timeouts, and the per-error
transient/permanent classification live there, not here.

**The one-sentence ownership model:** Mycorrhizal is responsible for calling an
integration correctly, retrying and classifying failures correctly, keeping
your local data intact, and *telling you* something is wrong. You (the operator)
are responsible for running or subscribing to the external service, its
credentials, network reachability from the server, and keeping the remote
software within the tested range. And where a connection is configured per
**user** (a remote address book, an Immich/Paperless/Seafile/WebDAV account, a
notification channel), it is that user who owns the connection — the operator
only owns the server-wide knobs that apply to everyone.

A structural check fails the build if a new integration is added to the
classification registry without a section on this page (DOC-03, issue #488 /
DOC-04, issue #489).

## The configuration surfaces

There are two places an integration gets configured, and the ownership boundary
follows them:

| Where | Who configures it | Examples |
|---|---|---|
| **Server environment (`.env`)** | The operator | Email (Resend/SMTP), OIDC, HIBP, update-check, inbound CardDAV/CalDAV *serve* enablement, the SSRF knobs, FCM (Android push), retention windows |
| **Per user, in the app** | The user, in Settings | Remote CardDAV/CalDAV subscriptions, Immich/Paperless/Seafile/WebDAV connections, webhooks, ntfy/Gotify/push channels, push/device registrations |

Two operator facts shape everything below:

- **`JWT_SECRET_KEY` is the encryption master key for stored integration
  secrets.** CardDAV/CalDAV passwords, Immich/Paperless/Seafile API keys,
  WebDAV app passwords, and Gotify tokens are stored encrypted at rest using a
  key derived from it. Rotating `JWT_SECRET_KEY` without re-entering every
  stored credential makes them undecryptable — plan for that when you rotate
  (see the [incident response runbook](security/incident-response.md) for the
  key-decoupling step).
- **Private addresses are refused by design — sometimes always, sometimes only
  if you say so.** Whether a given integration will dial an address on your LAN
  depends on its SSRF posture (see [The SSRF boundary](#the-ssrf-boundary)
  below). If an integration "can't reach" a LAN address, check that section
  first: it may be the deliberate refusal, not a bug.

## Reading the table below

Each section states the ownership boundary, the failure symptoms (in the same
language the UI uses), the diagnostic path, and the data-on-removal behavior.
Where this project has *real* evidence of which external software it is tested
against, that is stated explicitly — where it does not, it says so rather than
claiming "works with X".

## The integrations

<!-- The anchors below are load-bearing: the DOC-03 structural check keys each
integration in the classification registry to its <a id> on this page. -->

| Integration | Who configures it | Data lives | If it is down, you see |
|---|---|---|---|
| [CardDAV sync](integration-ownership.html#carddav) | User (Settings) | Local contacts + remote address book | Sync failure / terminal alert in Settings → Data |
| [CalDAV sync](integration-ownership.html#caldav) | User (Settings) | Local activities + remote calendar | Sync failure / terminal alert in Settings → Data |
| [Immich](integration-ownership.html#immich) | User (Settings) | Remote; links + timeline locally | Missing enrichment / dead photo links |
| [Paperless](integration-ownership.html#paperless) | User (Settings) | Remote; links + cached titles locally | Links stop resolving |
| [Seafile](integration-ownership.html#seafile) | User (Settings) | Remote; links locally | Links stop resolving |
| [WebDAV/Nextcloud](integration-ownership.html#webdav) | User (Settings) | Remote; links locally | Links stop resolving |
| [Webhooks](integration-ownership.html#webhooks) | User (Settings) | Delivery rows locally (bounded) | "Will not retry" delivery badge |
| [ntfy](integration-ownership.html#ntfy) | User (Settings) | None locally | No notifications |
| [Gotify](integration-ownership.html#gotify) | User (Settings) | None locally | No notifications |
| [Web Push](integration-ownership.html#webpush) | User (browser) | None locally | No browser notifications |
| [Email — Resend/SMTP](integration-ownership.html#email-resend) | Operator (`.env`) | Delivery rows locally | No email; "not configured" in channel health |
| [OIDC SSO](integration-ownership.html#oidc) | Operator (`.env`) | Identity mapping on the user row | "Sign in with provider" fails / missing |
| [HIBP](integration-ownership.html#hibp) | Operator (`.env`) | None (off by default) | Nothing (fail-open) |
| [Update check](integration-ownership.html#update-check) | Operator (`.env`) | None (off by default) | No "newer release" line |

### CardDAV contact sync

<a id="carddav"></a>

- **What it does.** Two-way sync between a remote address book (Radicale,
  Nextcloud, Baikal, …) and the user's local contacts. A user subscribes to a
  remote address book; its entries are imported as ordinary contacts, and local
  edits sync back. On a remote change the local copy is overwritten — remote
  wins (see `docs/carddav.md` for the sync behavior). Separately, when the
  operator sets `CARDDAV_ENABLED`, the app also *serves* the user's own
  contacts to phones via CardDAV (`docs/carddav.md`); that served direction is
  not part of the classification registry.
- **Optional.** Yes.
- **Configured by.** The **user**, per subscription (URL, username, password) in
  Settings → Data. The operator's only knob is the SSRF guard
  (`CALDAV_BLOCK_PRIVATE_URLS`, shared with CalDAV). The served direction is
  enabled by the operator (`CARDDAV_ENABLED`).
- **Ownership.** Mycorrhizal owns: importing/reconciling correctly, honoring the
  sync token and ETags, and classifying auth-expiry / auth-revoked /
  remote-deleted as permanent (stopping retries). The user owns their
  credentials and the remote account. The operator owns running/reaching the
  remote server.
- **If it is unavailable.** Syncing is user-triggered for subscriptions; a
  failed sync leaves the subscription with a "sync failed" chip or, once
  classified as permanent, a **"Sync stopped — action needed"** alert naming
  the reason: the saved password/token was rejected, access was revoked, or the
  remote address book no longer exists. Staleness is shown as "Last successful
  sync: N days ago".
- **Diagnosing it.** Settings → Data on the subscription (status, terminal
  reason, last success) → fix the credential at the source and use **Sync now**.
  A subscription edit clears a terminal state. For the served direction,
  `GET /admin/diagnostics` and the CardDAV logs (`docker compose logs`).
- **Removing it.** Deleting the subscription deletes its sync links and pending
  conflicts. **Imported contacts stay** — they are ordinary user-owned contacts
  by design; only the link to the remote book goes. The remote address book is
  never touched.
- **Tested versions.** The outbound client is exercised in CI against real
  servers: **Radicale** (digest-pinned container), **Baikal** (latest), and
  **Nextcloud** (`stable`). The served direction is exercised against the
  **vdirsyncer 0.20.0** reference client (hash-pinned). Apple Contacts,
  Thunderbird, and DAVx5 are documented reference clients that have not yet been
  run in CI — treat them as expected-to-work, not tested.

### CalDAV calendar sync

<a id="caldav"></a>

- **What it does.** A user subscribes to a remote calendar; its events are
  imported into local **activities** (and matched to contacts by attendee
  email). Optional two-way write-back is gated on the operator's
  `CALDAV_TWO_WAY_ENABLED`. Separately, `CALDAV_ENABLED` serves the user's own
  activities and lifecycle dates out as an iCalendar collection to calendar
  apps — again not part of the classification registry.
- **Optional.** Yes.
- **Configured by.** The **user**, per calendar subscription. Operator knobs:
  `CALDAV_ENABLED`, `CALDAV_SYNC_INTERVAL_HOURS` (default 6), `CALDAV_TWO_WAY_ENABLED`,
  `CALDAV_BLOCK_PRIVATE_URLS`.
- **Ownership.** As CardDAV: Mycorrhizal owns import correctness, the sync
  interval job, and permanent-failure classification; the user owns the remote
  calendar and credentials; the operator runs/reaches the server.
- **If it is unavailable.** The scheduled import fails; the subscription shows a
  failed-sync chip or the terminal "Sync stopped — action needed" state. Imported
  events already in your local activities are unaffected.
- **Diagnosing it.** Settings → Data on the calendar subscription (terminal
  reason, last success, `calendar sync` system events), then the remote
  calendar's own health.
- **Removing it.** Deleting the subscription deletes its event links; **imported
  activities stay** as ordinary activities. Nothing remote is deleted.
- **Tested versions.** Calendar/ICS *format* correctness is pinned against the
  `golang-ical` reference implementation, but **no real CalDAV server product is
  exercised in CI**. Treat "works with CalDAV" as expected-to-work against
  current stable servers, not a tested-version claim.

### Immich photo enrichment

<a id="immich"></a>

- **What it does.** Matches the user's contacts to people in an Immich library
  and pulls enrichment in: a person link on the contact and "photo appearance"
  entries on the contact's timeline. The face thumbnail shown on the contact
  page is **proxied live from Immich under the user's credentials** — no photo
  bytes are stored locally (unless the user explicitly picks an Immich asset as
  their profile photo, which downloads it normally).
- **Optional.** Yes.
- **Configured by.** The **user** (Settings): their Immich base URL + API key,
  stored encrypted. Operator knob: `IMMICH_SYNC_INTERVAL_HOURS` (default 6).
  SSRF posture: `guarded-always` — see [The SSRF boundary](#the-ssrf-boundary).
- **Ownership.** Mycorrhizal owns the match/import job and failure handling;
  the user owns the Immich account and key; the operator runs/reaches the
  server.
- **If it is unavailable.** Enrichment stops; person links and photo-appearance
  entries stay but go stale; the contact page's thumbnail proxy fails with "no
  Immich connection configured" (or the API-key error). Nothing local is
  corrupted.
- **Diagnosing it.** Settings → the Immich config's sync state, `immich` system
  events / `integration_failed`, and `GET /admin/diagnostics` (`integration_immich`).
- **Removing it.** Deleting the config stops future enrichment **and keeps every
  person link and timeline entry** — they are your data. Removing the
  per-contact link removes that link but keeps its history. A photo you saved as
  a profile picture stays (it is a normal local file). The Immich library is
  never modified.
- **Tested versions.** No real Immich server is exercised in CI (in-process
  fakes only). Treat as expected-to-work against current stable Immich.

### Paperless document links

<a id="paperless"></a>

- **What it does.** Links contacts to documents in a Paperless-ngx instance.
  Read-only: at link time the document's title/file name/date are fetched under
  the user's token and cached locally so links survive without re-fetching.
- **Optional.** Yes.
- **Configured by.** The **user** (Settings): base URL + API token, stored
  encrypted. SSRF posture: `guarded-always` — see
  [The SSRF boundary](#the-ssrf-boundary).
- **Ownership.** As Immich. Paperless remains the system of record; nothing is
  ever written or deleted there by this app.
- **If it is unavailable.** Links stay but stop resolving live once the config
  is gone or the token fails; a linked document that no longer exists is a
  stale reference, not an error state that retries forever.
- **Diagnosing it.** Settings → the config's state, `GET /admin/diagnostics`
  (`integration_paperless`).
- **Removing it.** Deleting the config keeps the links (dead references).
  Deleting an individual link removes that link only. Deleting the contact
  removes its links. Remote documents are never touched.
- **Tested versions.** No real Paperless instance in CI (in-process fakes).
  Expected-to-work against current stable Paperless-ngx.

### Seafile file links

<a id="seafile"></a>

- **What it does.** Links contacts to files/folders in a Seafile library
  (read-only). A link stores the library path, a web URL, and metadata (name,
  type, size, mtime). **It is not an attachment backend**: file *bytes* are
  never stored or uploaded by Mycorrhizal, and real attachments are a separate,
  always-local feature (see Deployment → Backups). If you were expecting Seafile
  to *store* your attachments, that is a different feature than this link.
- **Optional.** Yes.
- **Configured by.** The **user** (Settings): server URL + API token, encrypted.
  SSRF posture: `guarded-always` — see [The SSRF boundary](#the-ssrf-boundary).
- **Ownership.** As Immich. Remote files are Seafile-owned.
- **If it is unavailable.** Links stop resolving. Nothing local breaks.
- **Diagnosing it.** Settings → config state; `GET /admin/diagnostics`
  (`integration_seafile`).
- **Removing it.** Config deletion keeps links; individual link deletion removes
  that link; remote files are never touched.
- **Tested versions.** No real Seafile server in CI (fakes). Expected-to-work.

### WebDAV / Nextcloud file links

<a id="webdav"></a>

- **What it does.** The same read-only linking for generic WebDAV servers —
  primarily the Nextcloud/ownCloud pair (their files app URLs differ, hence one
  connection type covering both). Links store the normalized path, a files-app
  URL, and metadata. Like Seafile, **not an attachment backend**.
- **Optional.** Yes.
- **Configured by.** The **user** (Settings): URL + username + an **app
  password** (enforced — a real password is rejected). SSRF posture:
  `guarded-always` — see [The SSRF boundary](#the-ssrf-boundary).
- **Ownership / unavailable / removing it / tested versions.** Identical to
  Seafile: links only, config deletion keeps them, remote files untouched, no
  real WebDAV server in CI (fakes). (A real **Nextcloud** container does appear
  in CI — but as the *CardDAV* reference server, not this client.)

### Outbound webhooks

<a id="webhooks"></a>

- **What it does.** POSTs a JSON envelope to operator- or user-chosen receiver
  URLs when configured events fire. Each webhook has its own signing secret.
  Delivery is retried on a schedule and stops when a failure is classified
  permanent.
- **Optional.** Yes.
- **Configured by.** The **user** (Settings → Webhooks): URL, events, secret.
  Operator knobs: `WEBHOOK_BLOCK_PRIVATE_URLS` (default off — LAN receivers are
  legitimate), `WEBHOOK_DELIVERY_RETENTION_DAYS` (default 30).
- **Ownership.** Mycorrhizal owns retrying, honoring `Retry-After`, classifying
  permanent failures, and bounding the delivery history. The user/operator owns
  the receiver (it must be up, reachable, and correctly parsing the payload).
- **If it is unavailable.** The receiver is down/misconfigured → deliveries show
  "Will not retry" and "Deliveries are failing permanently — fix the receiver or
  the URL" once classified permanent; failed/retrying deliveries are visible per
  webhook.
- **Diagnosing it.** Settings → Webhooks: per-webhook delivery health and recent
  deliveries, the "Test" button (reports the real response/error), and the
  `webhook_delivery_failed` system events. Retention: delivery rows carry a copy
  of the triggering entity and are **deleted after the retention window** — that
  is a PII-bounding decision, not just housekeeping.
- **Removing it.** Deleting a webhook stops new deliveries but keeps its delivery
  history until the retention purge removes it.
- **Tested versions.** No external receiver product is tested; deliveries are
  exercised against local test servers. The wire contract (headers, payload,
  retry semantics) is what is pinned.

### ntfy / Gotify / Web Push notifications

<a id="ntfy"></a>
<a id="gotify"></a>
<a id="webpush"></a>

- **What they do.** Deliver reminder notifications to a user-chosen ntfy topic,
  a Gotify server, or the user's browser (Web Push / VAPID). All three are
  per-user channels under Settings → Notifications (see `docs/notifications.md`
  for how-to).
- **Optional.** Yes, individually and collectively.
- **Configured by.** The **user**, per channel. Operator impact is minimal:
  browser push needs HTTPS; mobile push can use FCM
  (`FCM_SERVICE_ACCOUNT_FILE`); the VAPID keypair is generated automatically and
  stored in the database (no env). ntfy/Gotify tokens are stored encrypted.
- **Ownership.** Mycorrhizal owns sending, the delivery record, and surfacing
  the real reason a send failed. The user owns the channel credentials/topic;
  the operator owns network reachability to ntfy/Gotify servers and the HTTPS
  requirement for Web Push.
- **If it is unavailable.** Reminders silently do not arrive on that channel.
  The **Send test notification** button reports the real failure reason; the
  admin channel-health panel (`/system-events`) shows per-channel
  configured/failing/no-devices/unconfigured. A push endpoint that stops
  existing (404/410) auto-prunes the subscription.
- **Diagnosing it.** Settings → Notifications → Send test notification first;
  then `notification_<channel>` checks in `GET /admin/diagnostics` and the
  notification channel health panel.
- **Removing it.** Disabling is per-channel (clearing the config / toggling the
  channel off); there is no separate "remove" endpoint. Nothing user-authored is
  affected — only future notifications stop.
- **Tested versions.** No external push product is exercised in CI (local test
  receivers only). ntfy/Gotify are expected-to-work against current stable
  releases of each.

### Transactional email — Resend / SMTP

<a id="email-resend"></a> <a id="email-smtp"></a>

- **What it does.** Sends the transactional email this app produces — password
  resets, invites, reminder digests, alerts — through either Resend's API or
  your own SMTP server. With no email channel configured, **those emails cannot
  be delivered at all** (this is the one "optional" channel whose absence has a
  real product consequence).
- **Optional.** As a channel, yes; but password-reset/invite delivery depends on
  one being configured.
- **Configured by.** The **operator only** — `.env`: `RESEND_API_KEY` +
  `RESEND_FROM_EMAIL`, or `SMTP_HOST/PORT/USERNAME/PASSWORD/FROM_EMAIL/USE_TLS`.
  There is no per-user email server.
- **Ownership.** Mycorrhizal owns message construction, delivery, and recording
  delivery outcomes. The operator owns the mail account, its credentials, SPF/DKIM
  deliverability, and the SMTP host's reachability.
- **If it is unavailable.** Reminders that should email do not; password-reset /
  invite links cannot be sent. The channel-health panel and the diagnostics run
  (`notification_email`) report it. Mailer failures are recorded per delivery.
- **Diagnosing it.** `GET /admin/diagnostics` → `notification_email`; the
  channel-health panel; the mail server's own logs for a relay that accepts then
  bounces.
- **Removing it.** Unset the env vars; nothing local is deleted except that
  future sends stop. Delivery rows are bounded like webhook history.
- **Tested versions.** Delivery logic is exercised against local SMTP test
  doubles. Resend/SMTP are expected-to-work; no external mail provider is a CI
  fixture.

### OIDC single sign-on

<a id="oidc"></a>

- **What it does.** Lets users sign in with an external OpenID Connect provider
  (Keycloak, Authentik, Authelia, Google, …). **Per instance, not per user**: one
  provider for the whole deployment, configured entirely by the operator. The
  user row stores the identity mapping (`oidc_subject`/`oidc_provider`).
- **Optional.** Yes — but it is the **only non-optional row once enabled** in
  the sense that a broken provider blocks the login route it backs.
- **Configured by.** The **operator**: `OIDC_PROVIDER_URL`, `OIDC_CLIENT_ID`,
  `OIDC_CLIENT_SECRET` (all three required), plus
  `OIDC_AUTO_PROVISION`/`OIDC_TRUST_EMAIL`/`OIDC_SCOPES`/`OIDC_BLOCK_PRIVATE_URLS`.
  The redirect URL is derived from `FRONTEND_URL`.
- **Ownership.** Mycorrhizal owns the OIDC flow (discovery, PKCE, ID-token
  verification) and the account-linking rules. The operator owns the provider's
  configuration, its client registration, and its reachability.
- **If it is unavailable.** The "Sign in with provider" flow fails or the button
  disappears (routes are disabled when the env vars are incomplete). Password
  users are unaffected.
- **Diagnosing it.** `GET /admin/diagnostics` (`integration_oidc`), provider
  logs, and the incident-response runbook's provider section.
- **Removing it.** Clear the `OIDC_*` env vars. Existing users keep their
  `oidc_subject`/`oidc_provider` mapping (harmless) and can still use passwords
  only if they had one — SSO-provisioned accounts have no password and would
  need an operator to reset access.
- **Tested versions.** No real provider is exercised in CI (in-process fakes
  only, including an SSRF and attack-matrix suite). Expected-to-work against
  standards-conformant providers.

### HIBP breached-password check

<a id="hibp"></a>

- **What it does.** When enabled, checks a password against Have I Been Pwned's
  k-anonymity range API during registration/password change. Off by default;
  only a 5-character SHA-1 prefix ever leaves the process. **Stores nothing.**
- **Optional.** Yes (opt-in).
- **Configured by.** The **operator**: `HIBP_CHECK_ENABLED`.
- **Ownership / symptoms / removal.** Mycorrhizal owns calling it fail-open (a
  HIBP outage must not block registration). If HIBP is down, the check is simply
  skipped. Disabling the env var is the whole removal; there is no data to clean
  up.
- **Tested versions.** No external calls in CI (local fixtures). Expected-to-work
  against the HIBP API.

### Update-availability check

<a id="update-check"></a>

- **What it does.** When enabled, asks the GitHub releases API whether a newer
  release exists and shows it on the admin system-status page. **Stores nothing
  persistent** (a short in-memory cache only, gone on restart). Off by default.
- **Optional.** Yes (opt-in).
- **Configured by.** The **operator**: `UPDATE_CHECK_ENABLED`.
- **Ownership / symptoms / removal.** Mycorrhizal owns calling it; the symptom
  of it being down is simply no "newer release" line. Disabling the env var is
  the whole removal.

## The SSRF boundary

Server-side URL handling refuses private/loopback destinations so a
misconfigured or malicious integration cannot be used to reach inside your
network (issue #373 / #609). The classification matrix's SSRF table
(`docs/int-01-integration-classification-matrix.md`) is the authoritative
per-integration posture; what an operator experiences is one of three:

- **`guarded-always`** — every connection goes through the safe dialer,
  unconditionally. There is no off-switch. Point one of these at a private
  address and it is refused, deliberately. Rows in this posture today:
  **Immich, Paperless, Seafile, WebDAV/Nextcloud, update-check**.
- **`guarded-when-enabled`** — guarded only when the operator opts in with the
  family's `*_BLOCK_PRIVATE_URLS` knob (default **off**, so LAN self-hosted
  targets work out of the box). Enable the knob and a URL that resolves to a
  private address is refused with a clear error before any request is made.
  Rows in this posture today: **CardDAV subscriptions, CalDAV, webhooks, ntfy,
  Gotify, Web Push, OIDC**. The webhook/notification/push family share the
  `WEBHOOK_BLOCK_PRIVATE_URLS` knob; CardDAV/CalDAV share `CALDAV_BLOCK_PRIVATE_URLS`;
  OIDC has `OIDC_BLOCK_PRIVATE_URLS`.
- **`fixed-endpoint`** — no user-supplied URL at all (compiled-in vendor/operator
  host), so there is no dialer to guard: **Resend, SMTP, HIBP**.

The refusal is the intended behavior, not a bug: if an integration "can't
reach" something, the first thing to check is which posture applies to it and
what its knob is set to.

**Exposed and multi-tenant deployments must flip the `guarded-when-enabled`
knobs on.** The default (off) assumes a trusted LAN where webhooks and
integrations legitimately target private hosts. On an instance reachable from
the internet, or one hosting accounts you do not personally vet, leaving them
off means an authenticated user's webhook or integration URL can reach
loopback, other LAN hosts, and the cloud-metadata endpoint
(`169.254.169.254`); a network egress policy is a second layer, not a
substitute. Set `WEBHOOK_BLOCK_PRIVATE_URLS`, `CALDAV_BLOCK_PRIVATE_URLS`,
`IMMICH_BLOCK_PRIVATE_URLS`, `PAPERLESS_BLOCK_PRIVATE_URLS`,
`SEAFILE_BLOCK_PRIVATE_URLS`, `WEBDAV_BLOCK_PRIVATE_URLS`,
`MONICA_BLOCK_PRIVATE_URLS`, and `OIDC_BLOCK_PRIVATE_URLS` to `true` — the
operator checklist is the "SSRF hardening" row in
[the deployment security baseline](security/deployment-baseline.md).

## Where to look first (the diagnostic path)

One sweep, then per-integration surfaces:

1. **`GET /admin/diagnostics`** (web: Settings → System events → **Run
   diagnostics**; Android: Settings → System events) probes every configured
   integration's reachability in one pass, honoring the private-URL knobs, and
   reports per-check `ok`/`warning`/`error` — the fastest "is it them or is it
   us?" answer.
2. **Settings → Data** for CardDAV/CalDAV subscriptions: status, terminal
   reason, staleness, **Sync now**.
3. **Settings → Webhooks**: per-webhook delivery health and receipts; **Test**.
4. **Settings → Notifications**: **Send test notification** reports the real
   channel failure.
5. **The system-event timeline** (`/system-events`): `sync_failed`,
   `integration_failed`, and webhook-delivery events give the *when* and the
   correlation ID to grep the server log with (`docs/operations/observability.md`).
6. **`/health`** reports server-scoped integration reachability (OIDC, email,
   FCM) as `degraded`-but-`200` — a down integration is degraded, not down.

Per-integration detail (which surface, what to look at first) is stated in each
section above. When a symptom does not match any section, the classification
matrix's per-integration failure-behavior tables
(`docs/int-01-integration-classification-matrix.md`) are the engineering
reference for what "handled correctly" means.

## See also

- [CardDAV & contact sync](carddav.md) — serving your contacts to phones, and
  the sync behavior of subscriptions.
- [Notifications](notifications.md) — setting up each notification channel,
  including the private-address knobs explained operator-side.
- [Settings](settings.md) — where users configure their per-user connections.
- [Observability](operations/observability.md) — the diagnostics sweep, the
  system-event timeline, channel health, and alerting on integration state.
- [Integration classification matrix](int-01-integration-classification-matrix.md)
  — the engineering half: retry budgets, timeouts, and the per-error
  transient/permanent classification (INT-01, issue #464).
