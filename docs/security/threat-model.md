# Threat model + security architecture

The answer to "what happens if an attacker gets X?" for every asset this app holds, and the record of
every gating security decision this project has deliberately made rather than left implied. This doc is
the ASVS V1.2 (threat modeling) and V1.14 (security architecture) artifact; `docs/security/asvs-l2.md`
and `docs/security/masvs-l1.md` are the per-control checklists this doc's claims cite into — this doc
does not re-derive `file:line` evidence that already lives there. `docs/security/pii-inventory.md`
covers the orthogonal *minimization* question — not "is this asset protected?" but "should it
exist, and is it more or kept longer than needed?".

| | |
|---|---|
| **Last updated** | 2026-09-01 (issue [#377](https://github.com/DrewBrunning/mycorrhizal-crm/issues/377); gating decision 3 revised by issue [#507](https://github.com/DrewBrunning/mycorrhizal-crm/issues/507); self-hosted boundary given a concrete operator checklist by issue [#417](https://github.com/DrewBrunning/mycorrhizal-crm/issues/417); backup immutability / ransomware resistance resolved by issue [#505](https://github.com/DrewBrunning/mycorrhizal-crm/issues/505)) |
| **Scope** | Backend (Go/Gin + SQLite), frontend (React SPA), Android client, CardDAV/CalDAV sync, self-hosted deployment. |
| **Companion docs** | `docs/security/asvs-l2.md` (backend/frontend/deployment controls, OWASP ASVS 4.0.3 + API Top 10), `docs/security/masvs-l1.md` (Android client controls, OWASP MASVS 1.5.0) |

"Answered in the doc" means: pick any asset below, and this doc plus its two companions tells you what
protects it, who could threaten it, and what happens if that control fails.

## The self-hosted boundary, as a first-class assumption

**The operator owns the host, the disk, the network, and the logs.** State it once here and every other
section derives from it:

- There is no multi-tenant cloud boundary to defend — a compromised host is a compromised deployment,
  full stop. Controls in this doc therefore target *network* and *application* attackers (someone who
  doesn't already have host access), not an operator attacking their own instance.
- This is the same position `masvs-l1.md` P1 already argues for the Android client (no certificate
  pinning against a self-hosted origin the operator controls) and that `asvs-l2.md` uses to mark
  mTLS/KMS/centralized-log-shipping `not-applicable` (V1.2.2, V1.6.2, V1.7.2, V6.4.2) — this doc just
  names the assumption instead of leaving five separate rows to each imply it.
- It bounds what "at rest" protection means: field-level encryption (below) defends a *stolen disk or
  backup*, not a *live, logged-in root shell on the box* — no software control defends against the
  second, by definition, for a single-process self-hosted app.
- It does **not** excuse anything reachable over the network without host access: authentication,
  authorization, session handling, SSRF, and input validation are full-strength regardless of
  self-hosting, and are covered like any other app (`asvs-l2.md` V2–V5, V9, V12, V13).
- `docs/security/deployment-baseline.md` (issue #417) is this assumption's concrete, operator-facing
  form: the reference topology, a recommended baseline (TLS/HSTS, secure cookies, no Docker-socket
  mount, minimal capabilities, resource limits, encrypted backups, …), and the explicit list of what
  the application does not secure (host OS, Docker daemon, reverse proxy, TLS certs, DNS, firewall,
  host filesystem, external backup storage, host admins, host compromise) — this section states the
  assumption once; that doc is the checklist an operator actually runs against.

## Assets

| Asset | Where it lives | What "compromised" means |
|---|---|---|
| Contact/note/activity/life-event content (PII) | SQLite, field-level AES-256-GCM encrypted at rest (`backend/atrest`, issue #380) except FTS-indexed columns (plaintext by necessity — see Gating decision 1) | Disclosure of a user's personal relationship data; on a *live* DB this is scoped per-user by every query (`asvs-l2.md` V4/API1) |
| Password hashes | `users.password_hash`, bcrypt cost 10 | Offline cracking attempt only — bcrypt cost 10 is the throttle (`asvs-l2.md` P1) |
| JWT signing key (`JWT_SECRET_KEY`) | Operator env var/file | Forge any session for any user; boot-time strength validation is the only defense (`asvs-l2.md` P2, V1.6.1) |
| TOTP seeds + recovery codes | AES-256-GCM encrypted, key HKDF-derived from `JWT_SECRET_KEY` (`backend/services/credential_crypto.go`) | Bypass a user's second factor |
| API tokens (`full`/`carddav` scope) | Hashed at rest (`asvs-l2.md` V3.5.2) | Scoped account takeover — bounded by scope, see the persona gap in Gap 2 below |
| Integration credentials (WebDAV/Immich/Seafile) | AES-256-GCM encrypted (`credential_crypto.go`) | Pivot into a user's external services |
| Attachments/photos | Filesystem, UUID-named, 0700/0750 perms (`asvs-l2.md` V12.4.1) | Disclosure of uploaded files; traversal/SSRF guarded (V12.3) |
| At-rest master key / wrapped DEK | `DATA_ENCRYPTION_KEY`/`_FILE`, `data_encryption_keys` table | Losing it makes every encrypted column undecryptable, **by design** (`asvs-l2.md` P4) — the flip side, gaining it, decrypts everything it wraps |
| Android session token | `EncryptedSharedPreferences` behind a Keystore `MasterKey` (`masvs-l1.md` STORAGE-1) | Session takeover from a compromised/rooted device — accepted risk, see Gating decision 3 |
| Android local mirror (Room DB) | SQLCipher whole-DB encrypted (`masvs-l1.md` P4, issue #385) | Offline PII disclosure from a lost/stolen device |
| Backups | Operator-managed storage the operator chooses (`make backup` → `VACUUM INTO` snapshot + `rsync`'d photos/attachments, `docs/deployment.md`) | Disclosure of everything the live DB holds. Confidentiality (issue #420): the snapshot inherits the DB's field-level at-rest encryption — encrypted columns travel as ciphertext, the master key that unwraps the DEK is operator env and never in the backup, so a stolen backup alone yields ciphertext + hashes + the FTS-plaintext set. Retention is operator-owned by design (an app-side expirer would hand an attacker running as the app the same power). Immutability (issue #505): the app's backup-write surface is write-new-only — no overwrite, no rotate/expire/delete path anywhere, pinned by trying it (`backend/database/backup_immutability_test.go`); on-host that is a bug barrier only, so immutability against a *compromised host* is the operator putting backups off-host where the app holds no credential to reach them (pull-based copy = documented default, or object-locked storage), `docs/deployment.md` → "Backup immutability & ransomware resistance" |

## Trust boundaries

The chain data crosses, narrowest to widest:

```
browser → API → database → filesystem → integrations → Android local DB → backups
```

| Hop | Enforced by |
|---|---|
| browser → API | TLS at the operator's reverse proxy (`docs/deployment.md:32`); CORS strict origin allowlist, `"*"` refused in release (`backend/main.go:429-447`, `backend/config/config.go:681-692`); CSP/HSTS/`nosniff`/frame-ancestors (`backend/middleware/security_headers.go`) |
| API → database | Every query AND-scoped by `user_id`/`VCardUID` (`asvs-l2.md` V4, API1); parameterized SQL only (V5.3.4) |
| API → filesystem | UUID filenames, traversal guards, 0700/0750 perms (`asvs-l2.md` V12.3–V12.4) |
| API → integrations | Public-IP-only SSRF dialer with DNS-rebinding pinning, per-service opt-in (`backend/httputil/safedial.go:27-47`, `asvs-l2.md` V5.2.6/API7) |
| API → Android local DB | Android is a thin client; the API is the only path to data, and everything it returns lands in the SQLCipher-encrypted Room mirror (`masvs-l1.md` V2, V4) |
| database/filesystem → backups | `make backup` snapshot is operator-owned storage; the snapshot inherits field-level at-rest encryption but the unwrap key is operator env, never in the backup — confidentiality/retention documented (issue #420, `docs/deployment.md` "Backup confidentiality & retention"); immutability is write-new-only in the app + off-host by architecture against a compromised host (issue #505, `docs/deployment.md` "Backup immutability & ransomware resistance") — see Assets table |

## Actors × trust boundaries

Each actor sits on a boundary, is neutralized by a control, and is verified by a test or issue.

| Actor | Boundary | Neutralizing control | Verification |
|---|---|---|---|
| Unauthenticated network attacker | browser→API | authz middleware, TLS, rate limiting, security headers | `asvs-l2.md` V2, V9, V14.4; issues #371/#551, #373, #374, #368, #369 |
| Authenticated ordinary user (BOLA/IDOR) | API | `user_id`/`VCardUID` scoping | `asvs-l2.md` V4.2.1/API1; exhaustive route × six-persona matrix, issue #371/#551 |
| Compromised authenticated session | browser→API | `TokenVersion` revocation, httpOnly cookie, CSRF mitigation | `asvs-l2.md` V3.3.3, V4.2.2; issues #372, #392, #419 |
| Malicious/misconfigured CardDAV client | sync→API | CardDAV Basic auth, per-user collections | **gap** — the #371/#551 persona matrix covers cookie-JWT actors only, not a CardDAV Basic-auth credential or a `carddav`-scoped API token; tracked in issue [#566](https://github.com/DrewBrunning/mycorrhizal-crm/issues/566) |
| Malicious imported vCard/JSContact | import parser→DB | parser validation, fuzzing, hostile-input neutralization | issues #375, #376, `controllers/hostile_input_e2e_test.go` |
| Malicious attachment | →filesystem | magic-byte validation, randomized names, SSRF-safe proxy | issue #375, `asvs-l2.md` V12.2.1/V12.3.2 |
| Malicious API client | →API | token scopes, rate limiting | issues #371/#551, #413, #415; scope-enforcement gap shares issue #566 |
| Compromised external integration | →integrations | SSRF dialer, fail-secure | `asvs-l2.md` V5.2.6/API7; issues #373, #465, #366 |
| Compromised host/container | →host | non-root, minimal caps, hardening; backups made unreachable to the app's own uid by off-host architecture (write-new-only in the app is a bug barrier only) | `asvs-l2.md` V1.2.1, V1.14.5, P5; issues #362, #417, #505 |
| Filesystem access to deployment (stolen disk) | →filesystem | field-level at-rest encryption | `asvs-l2.md` V6.1.1/P4, issue #380 |
| Obtains a backup | →backups | encrypted columns are field-level AES-256-GCM (the unwrap key is operator env, never in the backup), so a stolen backup alone yields only ciphertext + hashes + the FTS-plaintext set; restore security + retention documented (issue #420) | issue #420 (confidentiality/retention documented and verified by the restore drill); issue #505 (immutability: write-new-only in the app pinned by `backend/database/backup_immutability_test.go`, off-host architecture against a compromised host documented in `docs/deployment.md`) |
| Obtains JWT/API credentials | →session | secret strength validation, `TokenVersion`, token expiry/revocation | `asvs-l2.md` V1.6.1, V3.3.3; issues #393, #372, #413, #411 |
| Malicious data via CardDAV/CalDAV sync (reconcile path) | integrations→DB | parser validation on reconcile, same bar as import | **gap** — tracked in open issue [#512](https://github.com/DrewBrunning/mycorrhizal-crm/issues/512); the import assistant (#375) neutralizes hostile input, the sync reconcile path does not yet have equivalent E2E coverage |
| Lost/stolen Android device | device→local DB | SQLCipher-encrypted Room mirror, Keystore-backed session token, logout purge | `masvs-l1.md` STORAGE-1/P4, issue #385 |

## Controls → threat mapping

Rather than re-list individual `file:line`s (they're already cited row-by-row in the companion
checklists), this maps each actor class above to the *chapter* that answers it in detail:

- **Network/unauthenticated attackers** → `asvs-l2.md` V2 (Authentication), V9 (Communication), V14.4
  (security headers).
- **Authenticated-user BOLA/IDOR** → `asvs-l2.md` V4 (Access Control), API Top 10 → API1.
- **Session/credential compromise** → `asvs-l2.md` V3 (Session Management), V1.6 (key management).
- **Hostile input (import, upload, sync)** → `asvs-l2.md` V5 (Validation/Sanitization), V12 (Files and
  Resources).
- **SSRF / malicious integrations** → `asvs-l2.md` V5.2.6, API Top 10 → API7/API10.
- **Data-at-rest (stolen disk/backup, lost device)** → `asvs-l2.md` V6 (Stored Cryptography), V8 (Data
  Protection); `masvs-l1.md` V2 (Data Storage and Privacy), V3 (Cryptography).
- **Misconfiguration / deployment** → `asvs-l2.md` V14 (Configuration), V1.14 (Architecture).

## Gating decisions

The five decisions #377 asked to be explicitly revisited. All five were already resolved by prior work
(#380, #385, and the original security-hardening pass); this section is the written-down record the
issue asked for, each with a **keep** or **reverse** and why.

### 1. Data-at-rest encryption (ASVS V6.4/V8.3)

**Keep — encrypted, not just "operator owns the disk."** SQLite stores user-authored PII plaintext
by default; issue #380 added field-level AES-256-GCM encryption for everything sensitive that isn't
searched (contact free text, the neutral card, life events, reminders, gifts, preferences, conversation
notes, audit snapshots, sync-conflict copies), via a single wrapped-DEK envelope
(`backend/atrest`, `asvs-l2.md` V6.1.1 + P4). **Deliberate exception:** FTS5-indexed columns
(`notes.content`, `activities.*`, flat contact search fields) stay plaintext because SQL triggers can't
decrypt for indexing — this is the searchable-plaintext set referenced throughout this doc as the one
gap in an otherwise encrypted-at-rest dataset. The compensating control for that exception is still the
self-hosted boundary: those columns are protected the same way the *whole* DB was before #380 (operator
owns the disk), not left unprotected relative to some higher bar.

### 2. JWT key management (ASVS V6.4.1)

**Keep — env var + boot-time validation + revocation, not a vault.** `JWT_SECRET_KEY` is validated at
boot for length (≥ 32 bytes), placeholder rejection, and minimum entropy (`backend/config/config.go:434-501`).
There is no key-vault/KMS (`asvs-l2.md` V1.6.2 — not-applicable, self-hosted single process). Rotation
works via `TokenVersion`: bumping it invalidates every existing session immediately
(`backend/middleware/auth.go:141-154`); rotating the key itself is a restart with a new env var, at the
cost of invalidating the TOTP/integration-credential encryption that's HKDF-derived from it
(`credential_crypto.go:20-21`, `asvs-l2.md` P2). This coupling is accepted, not accidental: introducing
a dedicated key-derivation abstraction is deferred to when a second algorithm is actually adopted (P2).

### 3. MASVS-L2 resilience items (root detection, certificate pinning, screenshot prevention, tapjacking)

**Split decision, re-evaluated by issue [#507](https://github.com/DrewBrunning/mycorrhizal-crm/issues/507):
two kept declined, two reversed.** All four were re-examined individually against this doc's actors
(not as a block) — `masvs-l1.md` P1/P3/P6 record the full cost/benefit for each:

- **Certificate pinning — keep declined** (`masvs-l1.md` P1). Every user runs their own self-hosted
  server with a certificate the app cannot know in advance, frequently self-signed or from an internal
  CA; a naive pin would be wrong on day one for most installs. The MITM actor it would answer is real
  (Actors × trust boundaries, above) but is already neutralized by standard TLS + the KeyChain
  import flow for self-signed certs. Trust-on-first-use/user-managed pinning remains a distinct,
  uncosted feature this issue does not adopt.
- **Root detection (+ SafetyNet) — keep declined** (`masvs-l1.md` P3). This is the audience-honesty
  weighing the issue specifically asked for: self-hosted users are disproportionately likely to root
  their devices deliberately, and the actor this control answers (attacker already has the device) is
  already covered by data-at-rest controls (SQLCipher Room mirror, Keystore session token) that hold
  regardless of root status. Blocking on root would punish this project's own audience for a threat
  it doesn't add protection against. SafetyNet is additionally deprecated.
- **Screenshot prevention — reversed** (`masvs-l1.md` P6, MSTG-STORAGE-9). Every screen renders
  relationship PII; the recent-apps thumbnail is a concrete, zero-sophistication disclosure vector
  (anyone with a moment's access to an unlocked-but-idle phone). `MainActivity` now sets
  `FLAG_SECURE` unconditionally.
- **Tapjacking protection — reversed** (`masvs-l1.md` P6, MSTG-PLATFORM-9). Near-zero cost —
  `filterTouchesWhenObscured` only changes behavior when an overlay is actually present — against a
  real actor class (malicious overlay apps tricking a tap on a destructive confirmation). `MainActivity`
  now sets `filterTouchesWhenObscured = true` on its decor view.

**Assurance-level consequence, stated explicitly (the issue's ask):** this does **not** move the
claim to MASVS-L2. Root detection and certificate pinning stay declined, and other L2-only rows
(`masvs-l1.md` STORAGE-10/11/13/14/15, and the entire V8 Resiliency chapter, which this doc's Android
scope doesn't track at all) remain out of scope or unaddressed. **The Android client's assurance
target is still MASVS-L1** — a deliberate scope decision, not an oversight — now with two L2 controls
satisfied as a documented bonus rather than a level claim. None of MASVS-L2's resilience rows are
counted toward "satisfied" for level-claiming purposes anywhere in `masvs-l1.md`.

### 4. mTLS / KMS / centralized log anomaly detection

**Keep not-applicable.** All three assume infrastructure this app doesn't have: mTLS assumes
service-to-service traffic (`asvs-l2.md` V1.2.2 — single process, nginx→app is loopback), KMS assumes a
cloud key-management API (V1.6.2/V6.4.2 — secrets are env vars by design, see Gating decision 2),
centralized log shipping assumes a remote log pipeline (V1.7.2 — self-hosted, logs go to stdout → the
operator's own docker log driver). Each row already states this as a decision, not a silent gap; this
section exists so the *reason* — the self-hosted boundary — is stated once instead of implied five
times.

### 5. Session cookie flags (ASVS V3.4: HttpOnly/Secure/SameSite)

**Keep, verified against the exact code.** `backend/controllers/user_controller.go:234-243` sets the
session cookie `HttpOnly` (always), `Secure = cfg.CookieSecure`, and `SameSite=Strict` — tightened
from `Lax` by issue #392, which closed the residual sibling-subdomain/CSRF gap `Lax` left open. The
only cookies still deliberately `Lax` are the transient OIDC `oidc_state`/`oidc_nonce`/`oidc_pkce`
handshake cookies, which must survive the cross-site redirect back from the provider
(`backend/controllers/oidc_controller.go:87-90`). Boot-time config
validation refuses the insecure combination `FRONTEND_URL=https` + `COOKIE_SECURE=false`
(`backend/config/config.go:527-532`), so a misconfigured deployment can't accidentally serve an
HTTPS-fronted cookie without `Secure`. No `__Host-` prefix (`asvs-l2.md` V3.4.4, partial) — the cookie
must remain host-only rather than prefix-locked while plain-HTTP LAN deployments are supported
(`.env.example:131-145`); revisit if/when HTTP-only self-hosting is dropped.

## How to keep this honest

- A reviewer can answer "what happens if an attacker gets X?" for every asset in the Assets table by
  reading this doc plus the two companion checklists it cites into.
- Each of the five gating decisions above has an explicit **keep** or **reverse** and a recorded reason
  — none is left as an implication. Decision 3 is the one split decision: two items kept declined
  (root detection, certificate pinning), two reversed (screenshot prevention, tapjacking protection).
- `asvs-l2.md`'s V1.1.2, V1.1.4, and V6.1.1 rows cite this doc instead of re-deriving trust-boundary or
  threat-modeling detail.
- The two open gaps found while drafting this doc are tracked, not just noted: sync-path hostile input
  (issue #512) and the CardDAV/API-token persona gap in the authorization matrix (issue #566). If either
  closes, update its row in the Actors × trust boundaries table above.
- A design change that adds a new trust boundary (a new integration, a new sync direction, a new client)
  updates this doc in the same PR — the same "living document" convention `asvs-l2.md` already holds
  itself to.
