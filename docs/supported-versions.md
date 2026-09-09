---
title: Supported versions
nav_order: 18
---

# Supported versions

This page is the operator-facing half of the [supported runtime
matrix](development/supported-runtime-matrix.md) (COMPAT-01, issue #472): what
Mycorrhizal CRM runs on, what is *tested* rather than merely expected to work,
and what happens if your environment is outside that set. It states the
requirements an operator actually hits — host, Docker, browser, Android — and
deliberately leaves the contributor-facing toolchain floors (Go, Node, Yarn)
to the matrix, which is the engineering source of truth. A change to any floor
below is a breaking change under [the breaking-change policy](breaking-change-policy.md)
(MAINT-02, issue #491); this page and the matrix are kept in agreement by a
structural check that runs on every pull request (DOC-01, issue #486 / DOC-04,
issue #489).

## What "supported" means

"Supported" is doing a lot of work in this project, and it does not mean the
same thing for every row. The matrix and this page use three tiers:

| Tier | Meaning | Rows that are there today |
|---|---|---|
| **Tested in CI** | A job builds or runs against this exact floor on a schedule and fails if it breaks | Go/Node/Yarn floors, browser floor (weekly Firefox 111 / Chrome 115 runs), Android `minSdk` floor (weekly API-26 E2E), Docker Compose floor, Linux amd64/arm64 image builds |
| **Expected to work** | The floor is enforced at build time (or by the platform) but no CI job drives it every release | Safari/iOS ≥ 16.4 (no WebKit CI runner), host OS/docker daemon behavior on a network filesystem |
| **Unsupported** | Known not to work, or not built/tested — do not expect a fix if it breaks | Windows/macOS hosts, non-Docker runtimes, bare-metal install, browsers below the ES-modules era, Android below API 26, the database on a network filesystem |

When an operator reads "supported" they usually assume the first tier; the
table below says which tier each row actually is.

## Storage constraint — read this first

**The database directory must be on local disk.** The database runs in SQLite
WAL mode, which depends on advisory byte-range locks that network filesystems
— NFS, SMB/CIFS, and similar — do not reliably implement across clients.
Running the database over one is a known **corruption risk**, and it is the
most common self-hosted mistake this project anticipates: mounting a NAS share
and pointing the data directory at it.

The server logs a startup warning if it detects a known network-filesystem type
under the database path, but that detection cannot catch every case (see the
matrix's fail-clearly section for what it does and does not detect). The
warning is **advisory, not fatal** — the corruption is a risk, not a certainty,
and refusing to boot could brick a working instance. Treat it as a red flag and
move the database to local disk. Photos and attachments have **no** such
constraint — only the database matters; see [Deployment → Storage
requirements](deployment.html#storage-requirements).

## The supported environment

| Component | Supported | Tier | Notes |
|---|---|---|---|
| **Deployment shape** | Linux, **x86_64 or arm64**, running **Docker** (`docker compose`), as the single all-in-one container | Tested in CI (multi-arch images built and published per release) | This is the **only** deployment shape this project builds, tests, and documents. Everything else — bare metal, a non-Docker container runtime, Windows/macOS hosts — is unsupported, not merely undocumented. See [the deployment shape](#the-deployment-shape) below. |
| **Docker Engine** | `>= 22.0` | Tested in CI (weekly; the compose-file syntax floor is validated against the earliest V2 client) | 23.0 is when **Compose V2** became the Engine-bundled default, and **Compose V2** is what every doc and script here invokes (`docker compose`, never the deprecated hyphenated v1 binary). |
| **Docker Compose** | V2 (whatever ships with Engine ≥ 23.0) | Tested in CI (weekly) | No independent Compose-only floor beyond the Engine minimum. |
| **Web browser** | Chrome, Edge, Firefox ≥ 111; Safari / iOS ≥ 16.4 | Chrome/Edge/Firefox: tested in CI. Safari/iOS: expected to work (no WebKit CI runner) | The floor is set by Web Push support (Safari shipped it in 16.4). The bundle is built against this floor, so a browser at or above it gets a parse-correct build. Supported browsers below the floor but above ~2017/18 ES-module support fail loudly if at all — never silently wrong. See [what happens on an unsupported version](#what-happens-on-an-unsupported-version). |
| **Android app** | Android **8.0 (API 26)** and later | Tested in CI (real instrumented suite on an API-26 emulator, weekly) | `minSdk 26` is the app's floor; below it the OS refuses to install (`INSTALL_FAILED_OLDER_SDK`). The app also declares a server floor it talks to — see [the client compatibility policy](client-compatibility-policy.md). |
| **Database** | No version to install | — | SQLite ships inside the binary via the pure-Go driver; there is no separate host SQLite. The constraint is **where** it lives, not which version: [local filesystem only](#storage-constraint--read-this-first). |
Rows **not** on this page because they are contributor-facing, not operator-
facing: the minimum **Go** toolchain (`backend/go.mod`), **Node.js** and
**Yarn** floors (`frontend/package.json` `engines`). You only need them if you
are building from source; the shipped Docker image needs none of them. See the
[supported runtime matrix](development/supported-runtime-matrix.md) for those
rows and the reason each exists.

## What happens on an unsupported version

Each row fails differently; none is silent-wrong where this project can help it:

- **Docker Engine/Compose below 23.0 / V2.** The compose file's syntax may not
  parse or run. This is outside anything the server can check at boot — the
  failure mode is a compose error at bring-up, which names itself.
- **Non-Linux host, or a non-`x86_64`/`arm64` CPU.** Unsupported: no image is
  published for it, and no documentation assumes it. Nothing here detects it;
  it is a documented boundary, not a runtime check.
- **Browser below the floor but with ES-module support.** The build target
  (derived from the same `browserslist` array that defines the floor above)
  guarantees the bundle either runs correctly or fails with a real parse or
  runtime error. Between ES-module support and the floor is the gap the build
  covers.
- **Browser without ES-module support (roughly pre-2018).** The app serves a
  plain-language "please update your browser" fallback page instead of a blank
  white screen.
- **Safari/iOS below 16.4.** Web Push (browser notifications) does not exist on
  that platform; the app degrades by not offering that channel. The rest of the
  web app is built to the same floor.
- **Android below API 26.** The OS refuses the install with
  `INSTALL_FAILED_OLDER_SDK`.
- **Database on a network filesystem.** The startup warning names the
  filesystem; see the storage constraint above.
- **A database below the migration floor (`v0.6.0`).** The server refuses to
  migrate on startup with a two-step instruction — never a best-effort single
  hop. See [Upgrade compatibility](upgrade-compatibility.md).
- **Building from source on an unsupported toolchain.** `go` refuses per
  `go.mod`'s toolchain directive; `yarn install` refuses per `engines` +
  `engine-strict`. Both are exact, named failures.

## The deployment shape

**One process, one container, one host.** Mycorrhizal CRM is designed to run as
a single all-in-one Docker container: nginx serving the web app and proxying
`/api/`, `/carddav/`, and the calendar/DAV endpoints to the backend on
`127.0.0.1` inside the same container. There is no replica, no clustering, and
no failover; recovery means restore (see [Disaster recovery
boundaries](operations/disaster-recovery.md)). If you point more than one
instance at the same database, the in-memory rate limiters and job locks are
per-process — that is a supported configuration only in the sense that each
process is safe; it is not the documented deployment shape.

**Multi-user-per-instance is supported from `1.0.0`** (issue #558). Until then
the same isolation mechanism runs and is tested, but the *guarantee* — that a
stranger accepting an account on your instance can rely on the boundary — is
made from `1.0.0`. The isolation guarantee, in terms you can evaluate before
accepting an account on someone else's instance:

- Every account's data is separated at the query layer: every table carries a
  `user_id`, every request is scoped to the authenticated user, and the graph
  entities keyed by a contact UID rather than directly by `user_id`
  (relationship edges, circle/household/tag memberships, custom field values,
  sync links) are resolved with **both** clauses so naming another user's
  contact UID does not reach it. There are no cross-account references. The
  separation is enforced mechanically, not by review alone:
  `backend/routes/authorization_matrix_test.go` probes **every registered
  route** with a "user B → user A's resource" persona and fails CI on a route
  that is unscoped or has no declared authorization row; `backend/cmd/bolacheck`
  is the companion cross-account sweep; `TestProfileMultiUserIsolation` checks a
  populated multi-user dataset has no cross-user rows. Contacts, relationships,
  notes, activities, reminders, life events, custom fields, integrations,
  photos and attachments of one user are invisible to another.
- **What the admin can see.** The operator has an **admin** role that can
  create, edit, and delete user accounts, reset a second factor, and trigger
  maintenance jobs (backup/restore drills, search-index rebuilds, integrity
  checks, diagnostics). That is the whole of it: **the admin role cannot read
  another user's contacts, notes, or activities through the application** —
  there is no API for it. This is the answer issue #371 established, and it is a
  *tested* property: in `authorization_matrix_test.go` the `admin` persona gets
  the same `404`/`403` as any other non-owner on every non-admin item route,
  never a `2xx`.
- **What the admin inevitably can see anyway.** The person who runs the host
  has the filesystem, the database file, and the backups — no self-hosted
  application can prevent that, and it is a deployment decision, not a product
  guarantee. Decide whom you host for accordingly.
- **What other users can see.** Usernames are visible to every user on the same
  instance (they must be, for the sharing model to work); no other data is.
- **The one sanctioned cross-user path is contact sharing.** A user picks one
  of their contacts, chooses which sections to include, and the server freezes a
  filtered snapshot addressed to another user, who accepts or declines. It is
  always sender-initiated, and `private` / `secret` items are included only on
  explicit opt-in (issue #555).

### What is per-user versus per-instance

| Per-user (private to the account) | Per-instance (shared) |
|---|---|
| Contacts and everything hung off them — notes, activities, reminders, life events, preferences, gifts, custom field **values**, photos, attachments | The single SQLite database file and its one writer |
| The relationship graph — edges, circles, households, tags, and their memberships | The in-process `gocron` scheduler. It fires once per interval; the work inside a tick (cadence, CardDAV/CalDAV sync, reach-out scanning) iterates over **all** users, so its cost scales with user count independently of any one user's data volume |
| Custom **field definitions** (each user defines their own) | `REMINDER_TIME` / `REMINDER_TIMEZONE` — one clock for the whole deployment |
| Integrations and their stored credentials + cached remote data; notification channels and registered devices | IP-based auth rate limiters; OIDC configuration; outbound email transport |
| Account settings, language, API tokens, 2FA secret and recovery codes | `JWT_SECRET_KEY`, `COOKIE_*`, `FRONTEND_URL`, the uploaded-files directory, the admin role |
| Full-text search rows and audit-trail rows (both carry `user_id`) | Operator backups — they contain every user's data; the app deliberately cannot expire them |

### Registration

`POST /api/v1/register`, as implemented in
`backend/controllers/user_controller.go`:

| | |
|---|---|
| **Default** | Open — anyone who can reach the instance can create an account. |
| **`DISABLE_REGISTRATION=true`** | Registration returns `403` with `code: registration_disabled`. Existing users still log in; an admin can still create accounts. |
| **First account** | Automatically an admin (set when the user table is empty). |
| **Every later account** | A normal user. `is_admin` in the request body is ignored — the input DTO excludes it (no mass assignment). |
| **Protections** | Auth rate-limited; minimum-client-version enforced; optional [HIBP](https://haveibeenpwned.com/) breached-password check when `HIBP_CHECK_ENABLED=true`. |
| **SSO** | With OIDC configured, `OIDC_AUTO_PROVISION=true` creates an account on first SSO login; otherwise an unmatched SSO user must be registered first. |

Admins can also create accounts directly from the admin panel
(`POST /api/v1/admin/users`). These behaviours are covered by tests in
`backend/controllers/user_controller_test.go`.

**Intended scale.** Mycorrhizal CRM is designed for a **small group of
operator-vetted accounts** — a household, or a handful of people the operator
knows and chooses to host. The isolation guarantee protects against accident and
curiosity between people who broadly trust each other; the resource limits
(issue #415) are calibrated for that, not for defending a shared instance
against its own account holders. Running an instance open to arbitrary strangers
is possible — the guarantee still holds and is still tested — but it puts the
operator in the position of data controller for people they have never met (see
[Privacy](privacy.md)). **For any instance with more than one user, run with
`DISABLE_REGISTRATION=true`** and create each account deliberately from the
admin panel. Such an instance should also enable the app-layer SSRF guard (the
`*_BLOCK_PRIVATE_URLS` set, off by default for trusted-LAN self-hosting) — see
the "SSRF hardening" row in
[the deployment security baseline](security/deployment-baseline.md). Mycorrhizal
CRM is MIT-licensed: how you run your instance is ultimately your call, and this
is a recommendation, not a restriction.

## Version-support lifecycle

How long a given runtime version stays supported after a newer one appears:

- **Security fixes land on the latest tagged release only** — see
  [SECURITY.md](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/SECURITY.md)
  → Supported Versions. Pre-`1.0` there are no long-term-support branches and
  older tags are not backported.
- **Raising any floor above is a breaking change** under
  [the breaking-change policy](breaking-change-policy.md) (MAINT-02, issue
  #491), and the **removal window** — the deprecation notice, the minimum time
  between announcement and removal — is governed by the deprecation policy
  (MAINT-01, issue #490). A runtime minimum is never raised in the same release
  it is first announced in.
- **The database upgrade floor moves only at a major version.** Post-`1.0`, any
  `1.x` upgrades from any earlier `1.x` and from the final `0.9.x`; a floor
  above that needs the next major. See [Upgrade compatibility](upgrade-compatibility.md).
- **Client floors are a separate policy.** Whether a given server version still
  accepts your (older) web client or Android app is the client/server
  compatibility policy's question, not this page's — see [Client/server
  compatibility policy](client-compatibility-policy.md). In short: an old
  client keeps working against a new server until the server explicitly
  declares a floor; that happens only as a deliberate, documented event.

## See also

- [Supported runtime matrix](development/supported-runtime-matrix.md) — the
  engineering source of truth (issue #472), including the contributor-facing
  toolchain rows, the fail-clearly mechanics, and the CI jobs that exercise
  each floor.
- [Getting Started](getting-started.md) — installing the supported shape.
- [Deployment](deployment.html) — the production hardening, storage, and backup
  companion to this page.
- [Client/server compatibility policy](client-compatibility-policy.md) — which
  client versions a server accepts (ANDROID-01, issue #478).
- [Upgrade compatibility](upgrade-compatibility.md) — which server versions an
  instance can be upgraded from (issue #529).
