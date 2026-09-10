# ASVS L2 + MASVS-L1 verification report

`asvs-l2.md` and `masvs-l1.md` are **mappings**: every control has a status and a citation. ASVS is a
**verification** standard, so a mapping alone does not support a level claim — the claim only holds
once each line item has been checked against evidence that exists and passes. This document is that
check: a dated record of *how* each class of control was verified, what the verification found, and
what is explicitly claimed as a result.

It deliberately does **not** restate the 301 control rows. The two checklists stay the single
per-control source of truth (every security-sensitive PR is already required to update the row it
touches), and this report is the point-in-time audit over them. A re-verification edits this file's
header, findings, and changelog — not 301 duplicated rows.

| | |
|---|---|
| **Pass** | #1 (initial self-assessment) |
| **Date** | 2026-08-26 |
| **Commit verified** | `6a7cb7a2` (branch `claude/issue-378-v0-6-1-6e589d`), release line v0.6.1. Re-verified after merging `main` into the branch mid-review, which is how the workflow-citation shifts noted in §8 were caught. |
| **Standards** | OWASP ASVS 4.0.3 (V1–V14), OWASP API Security Top 10 (2023), OWASP MASVS 1.5.0 (V2–V7) |
| **Scope** | Backend (Go/Gin + SQLite), web frontend (React SPA), Android client, deployment artifacts (Docker/nginx/compose), CI |
| **Performed by** | Self-assessment (issue #378). Third-party verification is explicitly out of scope and is tracked separately as issue #511. |

## The claim

> **ASVS Level 2, with 26 documented exceptions.** **MASVS-L1, with 1 documented exception** (plus two
> L2 controls satisfied as a bonus, not as a level claim).

Stated plainly, without a silent downgrade: 191 of 257 ASVS control rows are `satisfied` with
verified evidence, 38 are `not-applicable` with a written reason, 2 are L3-only and out of scope, and
**26 are `partial`** — each naming its own gap. Every one of those 26 is enumerated in
[§7 Exception register](#7-exception-register). A reader who rejects any single exception should read
the claim as "L2 except that control", which is the point of enumerating them.

Nothing in either checklist is `fail`. That is a real property of this pass, not a definitional
dodge: `partial` here always means "the control is met in part and the shortfall is named", never
"unimplemented but softened".

### Status census

Generated from the checklists by `go run ./cmd/citecheck` (backend/), so these counts cannot drift
away from the tables they summarize.

| Chapter | satisfied | partial | not-applicable | out-of-scope |
|---|---|---|---|---|
| V1 — Architecture, Design and Threat Modeling | 32 | 2 | 4 | — |
| V2 — Authentication | 25 | 11 | 8 | — |
| V3 — Session Management | 13 | 4 | 1 | — |
| V4 — Access Control | 7 | 1 | 1 | — |
| V5 — Validation, Sanitization and Encoding | 21 | — | 5 | — |
| V6 — Stored Cryptography | 10 | 1 | 3 | 2 |
| V7 — Error Handling and Logging | 10 | 1 | 1 | — |
| V8 — Data Protection | 9 | 3 | 3 | — |
| V9 — Communication | 4 | — | 3 | — |
| V10 — Malicious Code | 4 | — | 1 | — |
| V11 — Business Logic | 4 | 3 | 1 | — |
| V12 — Files and Resources | 12 | 1 | 2 | — |
| V13 — API and Web Service | 7 | 2 | 2 | — |
| V14 — Configuration | 19 | 1 | 3 | — |
| API Security Top 10 (2023) | 9 | 1 | — | — |
| **ASVS total** | **186** | **31** | **38** | **2** |
| MASVS V2 — Data Storage and Privacy | 8 | 1 | — | — |
| MASVS V3 — Cryptography | 6 | — | — | — |
| MASVS V4 — Authentication and Session Management | 7 | — | 1 | — |
| MASVS V5 — Network Communication | 3 | — | — | — |
| MASVS V6 — Platform Interaction | 6 | — | 3 | — |
| MASVS V7 — Code Quality and Build Settings | 7 | — | 2 | — |
| **MASVS total** | **37** | **1** | **6** | — |

---

## 1. What "verified" means, per class of control

ASVS conformance is a claim about evidence, so the verification method has to differ by what the
evidence *is*. Five classes, five methods:

| Class | How it was verified this pass |
|---|---|
| **Code-cited** (`file:line`) | Two steps, because they fail differently. (a) *Resolution* — the path exists and the line range is inside the file: fully automated, `go run ./cmd/citecheck`, now a CI gate. (b) *Content* — the cited lines still say what the row claims: `go run ./cmd/citecheck -drift` produces the candidate list, then human review of every candidate. Step (b) is where this pass found almost everything (§4, F-1). |
| **Test-cited** (a `TestXxx` name, a `_test.go` file, a Kotlin `SomeTest.method`) | The identifier exists in the tree (automated, `citecheck`) **and** the suite it belongs to passes (§3). A named test that no longer exists is a gate failure, not a warning. |
| **Tool-cited** (SARIF, a CI workflow, a gate command) | The workflow file and step exist at the cited lines, the tool is a hard gate rather than report-only where the row claims it is, and its tier (PR / main-merge / nightly, per issue #578) is stated correctly. Inventoried in §3. |
| **Doc/position-cited** (a `docs/` section, one of the P1–P5 positions) | The referenced section exists and still says what the row says it says, and the position's stated revisit trigger has not fired. |
| **`not-applicable`** | Verified as a *structural* claim about the architecture, not an absence of effort: single process, no cloud IAM, no XML parser, RP-only OIDC, and so on. Each row carries its own one-line reason; this pass confirmed none of the 38 reasons has been invalidated by an architecture change (the trigger that would flip them). |

Two method notes worth writing down, because they bound how much this pass proves:

- **Resolution is exact; content is heuristic plus human — but both now gate.** `citecheck` can
  prove a citation points *somewhere real*; only a reader can confirm it points at the *right*
  thing. The drift heuristic narrows the line-range citations to a few dozen candidates by asking
  whether the prose leading up to a citation shares any vocabulary with the lines it cites. It has
  false positives by design (negative claims and pure-structure citations legitimately share no
  words with their target), so the accepted ones are written down with a reason in
  `docs/security/citation-drift.ignore` and CI fails on anything *not* in that file — and equally on
  a baseline entry that no longer matches, so dead suppressions cannot accumulate. Same
  ignore-list-with-justification shape as `.trivyignore`, `.grype.yml`, `zap/dast.ignore` and
  `schemathesis/schemathesis.ignore`. `citecheck -drift` remains the unfiltered human listing for a
  verification pass, which is exactly when an accepted suppression should be re-examined.
- **Your own edits are a drift source.** The gate proves in-bounds-ness, which survives almost any
  edit; the content check does not. Any change to a cited file — including the workflow that carries
  the gate, and including a merge from `main` — invalidates line numbers below the edit point
  silently. See §8 step 2.
- **Ambiguous basenames resolve permissively.** The checklists cite `auth.go:141-154` where the
  surrounding row makes clear whether that is `middleware/auth.go` or `carddav/auth.go`.
  `citecheck` accepts a line range that is valid for *any* candidate with that basename. The
  disambiguation is the reader's; the tool only rules out ranges that fit none of them.

### Reproducing the automated half

```bash
cd backend && go run ./cmd/citecheck        # the gate — this is what CI runs
cd backend && go run ./cmd/citecheck -drift # the human listing, for a verification pass
```

The gate exits 1 on any unresolvable citation, out-of-range line, off-legend status, empty evidence
cell, `satisfied` row with no citation, vanished test identifier, unaccepted drift candidate, or
stale entry in `citation-drift.ignore`. It runs on every PR as the `Security-doc citations` job in
`.github/workflows/unit-tests.yml`, deliberately without a path filter: a citation is orphaned by
*moving code*, not by editing the doc, and `.github/filters.yaml` maps `docs/**` to nothing.

`-drift` lists **every** candidate, baseline included, and always exits 0. Use it during a pass:
re-reading the accepted suppressions is part of re-verifying, and the gate by construction stays
silent about them.

---

## 2. Automated verification: citations and structure

`citecheck` over `asvs-l2.md`, `masvs-l1.md`, and `threat-model.md`:

| Measured | Result |
|---|---|
| Path citations | 744 (584 + 92 + 68), of which 360 carry a line or line range |
| Test-identifier citations | 39 (Go `TestXxx` and `TestFamily_*`, Kotlin `SomeTest.method`) |
| Control rows parsed | 301 (257 ASVS + 44 MASVS) |
| Citations that do not resolve | **0** (after the §4 corrections) |
| Line ranges outside their file | **0** (after the §4 corrections) |
| Rows with an off-legend status or empty evidence | **0** |
| `satisfied` rows citing nothing | **0** (13 before this pass — §4, F-2) |
| Deliberate non-file citations | 2, allowlisted with a reason in `cmd/citecheck/main.go` (`google-services.json`, `/.well-known/security.txt`) |

---

## 3. Automated evidence inventory

The suites and scanners that stand behind the `satisfied` statuses, with what each actually proves.
Tiering is issue #578's: **PR** = blocks a pull request, **main** = runs on merge to `main`,
**nightly** = scheduled.

### Suites run for this pass

| Suite | Result | Scale |
|---|---|---|
| `cd backend && go build ./... && go vet ./... && gofmt -l . && go test ./...` | pass (exit 0), 27 packages | 2,625 test functions across 364 test files |
| `cd backend && go test ./cmd/citecheck/` | pass | the gate above, plus 24 fixture cases covering each failure class |
| `cd frontend && npx tsc --noEmit && npx vitest run` | pass (exit 0) | 1,304 tests across 150 test files |
| `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/unit-tests.yml` | pass | the workflow carrying the new gate |

Not re-run in this pass, because they need a live stack rather than a checkout, and each is a CI gate
in its own right: Playwright E2E (`e2e-tests.yml`), Android instrumented E2E (`android-tests.yml`, 42
`@Test` methods), Schemathesis + `bolacheck` + `schemagate` (`schemathesis.yml`), ZAP DAST + `zapgate`
+ `dastcanary` (`zap-dast.yml`). Their evidence value for this pass is that they are hard gates whose
last runs on `main` are green, not that they were re-executed here.

### Security-relevant checks, by what they prove

| Evidence | Proves | Tier |
|---|---|---|
| `backend/routes/authorization_matrix_test.go` (#371) | Every route registered on the live router × six personas (unauth / owner / intruder / admin / disabled / expired). Enumerated from `router.Routes()`, so a new route with no declared authorization row **fails**, and a declared row with no route fails as stale. This is the strongest single piece of ASVS V4 evidence in the repo. | PR |
| `backend/routes/authorization_matrix_credentials_test.go` (#566) | The same route enumeration for the two credential types that are **not** a cookie JWT: a CardDAV/CalDAV Basic-auth credential (401 on every `/api/v1/*` route; on the DAV surface it reaches only its own collections), a `carddav`-scoped API token (403 on every REST route), and a `full`-scoped API token (matches the cookie-JWT `owner` verdict on every route). Reuses `buildTable` + the completeness guard, and carries its own guard over the registered DAV routes. | PR |
| `backend/cmd/bolacheck` (#369) | Cross-account BOLA sweep over the real HTTP surface: user B creates one resource of each of 13 entity types, user A attempts GET/PUT/DELETE on every one. 2xx or 5xx fails the run. | PR (schemathesis.yml) |
| Schemathesis + `backend/cmd/schemagate` (#369) | Spec-derived request fuzzing from `openapi.yaml`: 500s on malformed input, auth-protected operations returning data unauthenticated. `schemagate` is the gate, not the scanner's exit code. | PR |
| ZAP DAST + `zapgate` + `dastcanary` (#368) | Dynamic scan of the running app, gated on High/Medium with an ignore-list-with-justification. `dastcanary` is a deliberately-vulnerable sibling server whose planted findings prove the scanner and gate are actually working — a green scan cannot be a silently broken scan. | nightly |
| `backend/.semgrep/mycorrhizal-traps.yaml` (#370) | Five custom rules for this codebase's own recurring bug classes, each with a must-match fixture in `.semgrep/tests/`. `mycorrhizal-query-string-auth-material` is the standing enforcement behind ASVS 8.1.3 / 8.3.1 / 13.1.3. | PR (sast.yml) |
| `backend/errors/fail_secure_realdb_test.go`, `errors/middleware_test.go` (#366) | A forced DB failure yields a typed code and no raw driver error; a panic yields a generic 500 with stack and panic value logged server-side only. | PR |
| `backend/services/oidc_attack_matrix_test.go`, `controllers/oidc_attack_matrix_test.go` (#412) | `state`/nonce/PKCE binding, authorization-code replay, issuer/audience/`azp` validation, and the account-mix-up invariant (an OIDC identity may never authenticate as another local account because an attacker controls an email address). | PR |
| `backend/services/contact_sync_hostile_input_test.go` (#512) | A hostile remote CardDAV update cannot wipe or downgrade a secret-sensitivity custom field or relationship edge. | PR |
| `backend/controllers/contact_share_matrix_test.go` (#555) | Sensitivity filtering holds across an actual account boundary, asserted against the stored share payload rather than the API response; selecting a sensitive section cannot imply the opt-in. | PR |
| `backend/routes/session_lifecycle_test.go`, `middleware/auth_lifecycle_test.go` (#372) | `TokenVersion` revocation: password change / reset / admin reset each kill pre-existing sessions. | PR |
| `backend/httputil/fetch_test.go`, `services/webhook_ssrf_test.go`, `webhook_ssrf_integration_test.go`, `notification_service_test.go` (#373) | SSRF guard on the live webhook delivery path and the push path, not just the dialer in isolation. | PR |
| `backend/models/audit_chain.go` + `cmd/audit-verify` (#381) | Tamper-evidence: each `AuditEvent` commits `SHA-256(prev_hash ‖ content)`, a `BEFORE UPDATE` trigger rejects edits, and the operator can verify the chain out-of-band. | PR + operator |
| `database/concurrent_write_test.go` | `_txlock=immediate` — the DSN flag without which concurrent writes 500 with `database is locked` in under 5 ms. | PR |
| CodeQL, Trivy (misconfig + secret), zizmor, actionlint, shellcheck, golangci-lint (gosec + bodyclose), govulncheck, Dependency Review | SAST/SCA/workflow-security hard gates. | PR |
| Signed SBOM (`syft-sbom.yml`), Grype, TruffleHog git-history | Supply-chain second opinions. | main |
| Stryker mutation testing, full-length fuzz, CIS container hardening | Test-suite quality and container baseline. | nightly |
| **`backend/cmd/citecheck` (this pass)** | Every citation in the security checklists resolves, and no `satisfied` row cites nothing. | PR |

---

## 4. Manual verification records

The four manual checks issue #378 requires, plus what each found. "Manual" here means a human read
the code; where a script produced the candidate list, the script is named and its output was reviewed
item by item.

### A. Handler scoping — no IDOR

**Method.** Static sweep of all 50 non-test controllers: for every function containing a GORM query
verb (`First`/`Find`/`Take`/`Last`/`Where`/`Delete`/`Updates`/`Save`/`Create`/`Count`/`Scan`/`Pluck`/
`Model`/`Raw`/`Exec`), check whether the function body — comments stripped, so a comment mentioning
`user_id` cannot pass for scoping — carries an ownership token (`user_id`, `userID`, `VCardUID`,
`currentUserID`, or a scoped loader helper). Every function without one was then read.

**Result.** 222 controller functions contain a DB query. **4** carry no in-function ownership token,
and all four are correct:

| Site | Why it is correct |
|---|---|
| `admin_user_controller.go:186` `ListUsers` | Admin-only route, deliberately cross-user; gated by `middleware/admin.go` and covered by the authorization matrix's `admin` persona. |
| `user_controller.go:100` `LoginUser` | Looks up the account being authenticated. There is no caller identity to scope by yet — unscoped by definition. |
| `timeline_controller.go:304` `applyDateBounds` | Query-builder helper. It receives a `base` query that every caller has already scoped (`timeline_controller.go:346,368,372,391,411,434,482`). |
| `timeline_controller.go:313` `applyCursor` | Same. |

**Verdict: no IDOR found.** This is a static complement to the runtime evidence, which is the
stronger of the two: `authorization_matrix_test.go` probes every registered route with an `intruder`
persona and fails on a route with no declared authorization row, and `bolacheck` sweeps 13 entity
types cross-account over real HTTP.

### B. Session cookie flags

**Method.** Enumerated every `SetCookie` call site in the backend (script-extracted, including
multi-line calls, with the governing `SetSameSite` resolved per site), then read each one.

**Result.** 18 sites across `user_controller.go`, `two_factor_controller.go`, and
`oidc_controller.go`.

- **`HttpOnly`: true at all 18.** No site passes `false`; there is no JS-readable session cookie.
- **`Secure`: `cfg.CookieSecure` at every session-cookie site** (`auth_token`, `id_token`,
  `2fa_pending`, and their clears), with boot refusing the `FRONTEND_URL=https://` +
  `COOKIE_SECURE=false` combination (`config/config.go:527-532`).
- **`SameSite`: `Strict` on every session cookie** (`user_controller.go:203,234,262`,
  `two_factor_controller.go:415,466`, `oidc_controller.go:249`), tightened from `Lax` by issue #392.
- **`SameSite=Lax` on exactly four cookies, correctly**: the transient OIDC handshake cookies
  `oidc_state`/`oidc_nonce`/`oidc_pkce`/`oidc_client` (`oidc_controller.go:87-99`). These are read on
  a top-level cross-site navigation back from the identity provider, which a `Strict` cookie would
  never be attached to — `Strict` here would break every OIDC login. They are additionally
  path-scoped to `/api/v1/auth/oidc/callback` and expire in 600 s.

**One deviation found**, filed as issue #605: `oidc_client` and all four handshake-cookie clears
hardcode `Secure=true` instead of `cfg.CookieSecure`. That is *stricter* than configured, so it is
not a confidentiality gap — but on the supported plain-HTTP deployment a browser rejects the cookie
outright, so the Android client hint is never stored and the handshake cookies are never actively
cleared. Functional inconsistency, fails closed; recorded in row 3.4.1. **Fixed in #605.**

**Becomes a test (issue #610).** This audit is no longer a hand-read enumeration that has to be
redone every verification pass: `controllers/cookie_flags_test.go` drives every flow that mints or
clears a cookie (login, login+2FA, logout, password change, 2FA management, OIDC login start, OIDC
callback) against a real migrated schema and enumerates the `Set-Cookie` surface from the responses,
asserting `HttpOnly`, `Secure=cfg.CookieSecure`, and `SameSite` against a declared per-name policy
table. The table is exhaustive in both directions — an observed cookie with no declared row fails, a
declared row never observed fails — and the whole suite runs twice (once `CookieSecure=true`, once
`false`). Rows 3.4.1–3.4.3 cite it instead of a list of call sites.

### C. Cryptography, and the absence of unauthenticated modes

**Method.** Read every cryptographic call site and every route registration.

| Checked | Found |
|---|---|
| Password hashing | bcrypt at `bcrypt.DefaultCost` (10) through one shared `HashPassword` (`services/user_service.go:16-27`). Passwords over 72 bytes are **rejected** (`ErrPasswordTooLong`), not silently truncated — the bcrypt cap is explicit. See P1 for why not Argon2id yet. |
| JWT algorithm | The verifier pins the HMAC family before the key callback returns the secret: `token.Method.(*jwt.SigningMethodHMAC)` (`middleware/auth.go:66-71`). That is what defeats `alg: none` and RSA→HMAC confusion. The pin is family-wide rather than HS256-exact; HS384/HS512 would also verify, and all three take the same symmetric secret, so there is no downgrade to reach. Row 3.2.4 previously overstated this as HS256-exact and has been made precise. |
| Signing secret strength | Enforced at boot in four ordered checks — non-empty, ≥ 32 bytes, not a known published placeholder, minimum Shannon entropy (`config/config.go:375-403`). A long-but-published secret is as forgeable as a short one, which is why the placeholder check exists alongside the length floor. |
| Symmetric encryption | AES-256-GCM only — authenticated as it encrypts — for TOTP/integration credentials (`services/credential_crypto.go:33-58`, key HKDF-SHA256-derived from `JWT_SECRET_KEY`) and the at-rest field envelope (`backend/atrest`, single wrapped DEK, see P4). No unauthenticated mode is reachable by a caller. |
| Weak primitives | No MD5/DES/RC4/ECB/Blowfish anywhere. The tree's single `crypto/sha1` import is HIBP's own k-anonymity wire format, annotated at the import (`services/hibp_service.go:6`). SHA-256 is used only for non-reversible one-time token digests. |
| Random | `crypto/rand` for TOTP secrets and recovery codes (`services/twofactor.go:4,107`); bcrypt supplies its own per-hash salt. No `math/rand` in a security path. |
| Unauthenticated route surface | **15 registrations**, enumerated from `routes/routes.go`: `/health`, `/health/live`, `/health/ready` (the deep/liveness/readiness split, issue #421 — all secret-free, status + reason strings only); the two `.well-known` CardDAV/CalDAV redirects; `/auth/oidc/config` plus `login`/`callback` (registered only when OIDC is enabled); `register`; `login`; `login/2fa`; `logout`; `check-password-strength`; `password-reset/request`; `password-reset/confirm`. Every one that touches credentials carries `AuthRateLimitMiddleware`. Against 241 `protected.` + 9 `admin.` + 9 CardDAV/CalDAV Basic-auth routes. |
| Any way to turn auth off | **None.** No `DISABLE_AUTH` / `SKIP_AUTH` / `INSECURE_*` switch exists in any non-test Go file; `AuthMiddleware` is applied at the group level, so a route is either inside `protected`/`admin` or is one of the 15 above. The authorization matrix's `unauth` persona asserts this for every route rather than trusting the grouping. |

**Becomes a test (issue #612).** The "closed cryptographic surface" claim this audit read by hand is
now pinned by `cmd/citecheck`'s crypto-surface gate: it enumerates every non-test Go file importing
`crypto/*`, `golang.org/x/crypto/*` or a JWT/signing library and requires each to be cited by a V6
row in `asvs-l2.md` or to carry a justified entry in `docs/security/crypto-surface.ignore`,
failing in both directions (a new unaccounted call site, a declared one that stops importing
crypto). It runs in the same `Security-doc citations` job as the rest of citecheck, so a new call
site fails the build at the moment of introduction rather than at the next verification pass.

### D. Error paths do not leak internals

**Method.** Read the error envelope and the last-resort handler, then searched every non-test Go file
for a response body carrying a raw `err.Error()` or an equivalent unbounded error string.

**Result.** The envelope is `{error:{code,message,details}, request_id, timestamp}`
(`errors/middleware.go:13-24,67-86`); internal errors are generic text; the panic-recovery middleware
logs the panic value and stack server-side and returns a generic 500
(`errors/middleware.go:27-44`). Both are pinned by tests (#366).

**One exception found**, and it is the only site in the backend that bypasses the envelope: the
self-service "test my notification channel" endpoint reports a diagnostic string rather than an
envelope error (`notification_controller.go:167-172`). That is defensible — the endpoint exists so a
user can find out why *their own* ntfy/Gotify/push target rejected the message, and a generic message
would make the feature useless.

The outbound path was then traced to the end, because "unbounded error from an HTTP client" reads
like an SSRF oracle and it is worth being exact about whether it is one. **It is not, on the default
configuration.** `WEBHOOK_BLOCK_PRIVATE_URLS` defaults to `false` (`config/config.go:148`), so
`postNotificationJSON` skips its private-address pre-flight and `clientFor` returns the unguarded
client (`services/notification_service.go:836`, `services/webhook_service.go:64-70`); save-time
validation checks only the scheme (`middleware/validation.go:182-194`). That is the documented
opt-in-per-service position (row 5.2.6 / API7), and it is deliberate: pointing a self-hosted app at
an ntfy instance on your own LAN is the intended use, so defaulting the block on would break the
common case. The caller therefore already chose the target and already learns its reachability from
the returned status; the error text grants no capability they lack.

The genuine leak is the inverse, and only in the hardened configuration: with the flag **on**, the
guarded dialer returns two distinct sentinels — `ErrWebhookUnreachable` and
`ErrWebhookPrivateAddress` (`services/webhook_service.go:29-30`, `httputil/safedial.go:27-47`) — and
both were echoed verbatim, so an operator who turned the flag on to declare internal targets
off-limits got an endpoint that reports which rule a target tripped. Thin (it confirms "the address
you supplied is private", about an address the caller supplied) but the wrong direction. **Fixed by
issue #606**: the echoed string is now capped at 256 bytes and, with the flag on, all three guard
sentinels collapse to one neutral "not reachable under this instance's outbound policy" message,
while the full diagnostic still goes to the server log — so the endpoint stays useful on the default
configuration and stops distinguishing rejection rules under the hardening flag (pinned by
`notification_controller_test.go` `TestNotificationConfig_TestNtfy_ErrorTruncated`,
`TestNotificationConfig_TestNtfy_PrivateAddressCollapsedWhenGuarded`,
`TestNotificationConfig_TestNtfy_UnresolvableCollapsedWhenGuarded`).

**Re-checked for issue #421** (the `/health` → `/health/live` + `/health/ready` + deep-`/health`
split). All three are unauthenticated and build their own JSON rather than going through the error
envelope, so each was walked for the same "raw `err.Error()` / unbounded string in the body"
pattern. They pass by construction: every failing facet logs the underlying error / path / host /
`operational_check_results.detail` server-side (`logger`) and returns a fixed category string —
`"database read failed"`, `"unreachable"`, `"cannot read migration state"`, `"attachments directory
is missing"`, `"the last run reported failed"`. Migration version *numbers* and scheduled-job
*names* are returned deliberately (both are already public — `git` tags, open-source job registry —
and are the point of the endpoint); table names, row counts, absolute paths, the operator's SMTP
host and OIDC URL, and raw dial errors are not. Guarded by
`controllers/health_endpoints_test.go`'s `TestDeepHealth_ResponseBodyLeaksNoInternals` and the
path-free assertions in the readiness-filesystem tests.

---

## 5. Per-chapter verification notes

What this pass actually did per chapter, beyond the automated citation checks that cover all of them.

| Chapter | Verified this pass |
|---|---|
| **V1** Architecture | Confirmed the threat model (#377) is current except its §5, which asserted `SameSite=Lax` where the code has been `Strict` since #392 — corrected (F-3). CI inventory in 1.1.1 re-checked against the 23 workflows and their #578 tiers, and `citecheck` added. |
| **V2** Authentication | Manual audit C. The 11 `partial` rows are the largest cluster in the checklist and are all deliberate product positions (no view-password toggle, no notification on self-service change) — none is an unimplemented control. |
| **V3** Session Management | Manual audit B; every cookie flag read at its call site. 3.2.4 made precise about the algorithm pin. |
| **V4** Access Control | Manual audit A, plus confirming the authorization matrix still fails on an undeclared route (that property, not the current pass/fail, is what makes it evidence). |
| **V5** Validation | Confirmed the raw-SQL sites are parameterized: FTS search (`search_service.go:228-305`), the graph CTE (`graph_traversal.go:96-131`), and the export grouping query, whose only interpolated values are compile-time table/column constants (`export_controller.go:105-119`). |
| **V6** Stored Cryptography | Manual audit C. 6.2.5 and 6.2.7 were `satisfied` with no citation at all and now cite the actual call sites plus the gosec/CodeQL enforcement (F-2). |
| **V7** Error Handling | Manual audit D; found and documented the one envelope bypass. |
| **V8** Data Protection | 8.1.3 and 8.3.1 were uncited assertions about absence and now cite the Semgrep rule that continuously enforces them (F-2). |
| **V9** Communication | TLS boundary re-confirmed at `docs/deployment.md:32`; HSTS wiring re-located after `main.go` drift (`security_headers.go:43-45`, wired `main.go:279`). |
| **V10** Malicious Code | 10.3.2's SRI claim now cites why SRI is moot here (no external origin in the CSP) rather than asserting it (F-2). |
| **V11** Business Logic | The 3 `partial` rows are product decisions (per-user content quotas, unusual-activity monitoring, alerting) — unchanged, restated in §7. |
| **V12** Files and Resources | 12.3.6 now cites the lockfiles and the absence of `os/exec` in non-test packages rather than asserting it (F-2). |
| **V13** API | 13.1.3 now cites the Semgrep rule; 13.2.6 now states the deliberate absence of application-layer signing (F-2). API9 stays `partial` — no endpoint-inventory doc. |
| **V14** Configuration | The heaviest drift cluster: CORS, trusted proxies, and the boot validators had all moved within `main.go`/`config.go` (F-1). All re-located and re-read. |
| **API1–API10** | API1's evidence (the strongest claim in the file — "every handler AND-scopes") re-verified by manual audit A and the matrix/bolacheck pair. |
| **MASVS V2–V7** | Citations re-located after the Android tree moved (F-1); `deepLinkRoute`/`parseOidcReturn` re-read to confirm the strict scheme+host+path validation the PLATFORM-2/3 rows claim. STORAGE-5's login-screen gap is unchanged and remains the single MASVS exception. |

---

## 6. Findings

| # | Finding | Severity | Disposition |
|---|---|---|---|
| **F-1** | **48 distinct citations (74 occurrences) pointed at code that had moved.** Every one resolved to a real file with an in-bounds line range, so nothing flagged them: `main.go:191-209` cited for the CORS allowlist actually landed in the scheduler; `unit-tests.yml:175-177` cited for govulncheck landed in the fuzz step; `config.go:359-364` and `:375-380`, cited for the CORS and `COOKIE_SECURE` boot checks, landed in the at-rest-key comments the #380 work inserted above them. Also here: `webhook_service_test.go` (a file that no longer exists) and `errors/middleware.go:111-113` (a 112-line file). | High for the *checklist's* credibility; no code defect | **Fixed on this branch.** All 74 corrected and re-verified. `citecheck` now gates resolution on every PR, and `-drift` is the standing review queue for the content half. |
| **F-2** | **13 rows were `satisfied` with no citation whatsoever**, contradicting the checklist's own stated promise ("No row is left `satisfied` without a citation"). All 13 were negative controls — "no password expiry", "no KBA", "no CDN", "no plugin system" — where there is no `file:line` for a thing that does not exist, so they had been left as bare assertions. | Medium: unverifiable rows in a verification document | **Fixed on this branch.** Each now cites the artifact that proves the absence — the model/migration that has no such column, the Semgrep rule that fails a PR reintroducing it, the CSP that admits no external origin, the lockfiles. `citecheck` now fails a `satisfied` row that cites nothing. |
| **F-3** | **`threat-model.md` §5 stated the session cookie is `SameSite=Lax`.** It has been `Strict` since issue #392. A factual error, not a stale line number. | Medium: the threat model understated an implemented control | **Fixed on this branch**, including the reason `Lax` is retained for the OIDC handshake cookies only. |
| **F-4** | **OIDC handshake cookies hardcode `Secure=true`** on the `oidc_client` set and all four clears, while their siblings use `COOKIE_SECURE` (manual audit B). Stricter than configured, so not a confidentiality gap — but on the supported plain-HTTP deployment the browser rejects the cookie, so Android OIDC silently falls back to the web redirect and the handshake cookies are never actively cleared. | Low, fails closed | **Filed as issue #605** (v0.6.2). Recorded in row 3.4.1. |
| **F-5** | **The test-notification endpoint echoes an unbounded upstream error** to the client (manual audit D) — the only backend site bypassing the error envelope. Defensible as a self-service diagnostic (the user needs to know why *their own* ntfy/Gotify target refused the message), but unbounded. Traced to the end during review: **not** an SSRF oracle on the default configuration, because `WEBHOOK_BLOCK_PRIVATE_URLS` defaults off and pointing a self-hosted app at your own LAN is the intended use (row 5.2.6's opt-in-per-service position) — the caller already chose the target and already learns its reachability from the status code. The real, thin leak runs the other way: with the flag *on*, the guarded dialer's two distinct sentinels are echoed verbatim, so the hardening flag makes this endpoint marginally more informative rather than less. | Low | **Fixed by issue #606** (v0.6.2). Echoed string capped at 256 bytes; with the flag on, the guard sentinels collapse to one neutral outbound-policy message client-side while the full diagnostic stays in the server log. Remains a documented exception in row 7.4.1 (bounded diagnostic, not the envelope) by design. |
| **F-6** | **Row 3.2.4 overstated the JWT algorithm pin** as HS256-exact; the code pins the HMAC family. The security property (rejects `alg: none` and asymmetric confusion) is unchanged, but the row claimed more precision than the code has. | Low | **Fixed on this branch**: the row now states the family pin and why no downgrade is reachable. |

No finding in this pass required flipping a control from `satisfied` to `fail`. F-1 through F-3 and
F-6 were failures of the *documentation* to remain true; F-4 and F-5 are real code findings, both
low-severity and both failing closed. F-5 has since been fixed by issue #606 (§4 D, row 7.4.1).

Three further issues came out of asking what keeps this report true rather than out of the audit
itself, and are tracked in §9: **#608** (fold the gate and the re-verification obligation into the
release workflow), **#609** (semgrep rule for an outbound client bypassing the SSRF dialer), and
**#610** (pin audit B's cookie-flag enumeration as a test).

### What this pass could not verify

Stated so the claim is not read as stronger than it is:

- **Content correctness beyond the drift queue.** 360 line-range citations were checked for
  resolution; the ones the drift heuristic did not surface were not each re-read line by line. A
  citation whose target moved *and* whose new neighbourhood happens to share vocabulary with the row
  would survive.
- **Runtime behaviour of the E2E/DAST tiers.** Verified as "green hard gates on `main`", not
  re-executed here (§3).
- **Anything a self-assessment structurally cannot establish.** No adversary tried to break this
  system as part of this pass. That is issue #511's job, and until it happens the honest framing is
  "we verified our own evidence", not "we were tested".

---

## 7. Exception register

"L2 with 26 documented exceptions" is only meaningful if the exceptions are enumerable. They are.

### The five written-down positions

Full reasoning lives in `asvs-l2.md` § Documented positions; the revisit trigger is what matters for
re-verification.

| | Position | Revisit when |
|---|---|---|
| **P1** | bcrypt cost 10, not Argon2id (satisfies NIST 800-63B as written) | A password-hash version field + rehash-on-login path exists, making migration a code change rather than a forced reset for every user |
| **P2** | No crypto-agility abstraction; bcrypt/JWT called directly | A second algorithm is actually adopted (Argon2id, EdDSA sessions, external KMS). An abstraction with one implementation is dead weight |
| **P3** | HIBP breach check opt-in, off by default | A hosted multi-tenant deployment mode exists, where the operator already accepts outbound-call tradeoffs on users' behalf |
| **P4** | FTS-indexed columns stay plaintext; single wrapped DEK; lost key = lost data | Search moves off SQL-trigger-driven FTS5, or key escrow becomes a product requirement |
| **P5** | Backup confidentiality and retention are the operator's boundary | The project ships a managed backup destination |

### The 26 ASVS `partial` rows

Grouped by why each is short, because the groups have very different meanings.

**Deliberate product positions (11)** — the control is understood and not wanted as ASVS states it:
2.1.1 (length floor 8 + 50 bits entropy, per 800-63B, not 12 chars), 2.1.2, 2.1.12 (no
view-password toggle), 2.1.7 (P3), 3.4.4 (no `__Host-` prefix while plain-HTTP LAN deployments are supported), 6.2.4
(P2), 8.3.5 (read-auditing is a privacy/performance choice), 11.1.3, 11.1.7, 12.1.3 (per-user
quotas are product decisions), 14.4.2.

**Missing UI or surface, mechanism present (4)**: 2.2.3 and 2.5.5 (notification exists on the
recovery path, not on self-service change), 4.3.1 (2FA available to all, not *enforced* for admins),
API9 (no endpoint-inventory doc; `openapi.yaml` is maintained and drift-checked).

**Partial mechanism (8)**: 1.6.1 and 1.6.3 (rotation works; no standalone runbook, and TOTP/
integration re-encryption after JWT rotation is not automated), 2.3.2, 2.6.2, 2.8.2, 2.8.5
(TOTP reuse is detected, rejected and logged since issue #873 — no proactive owner notification, the
2.2.3 / 2.5.5 gap), 3.5.2, 13.2.5.

**Known small gaps with an obvious fix (3)**: 7.2.2 (no distinct access-denied event), 11.1.8 (no
alerting on lockout spikes), 13.1.5 (content-type rejection + correct status). *(8.1.1 and 8.2.1 —
`no-store` on `/api/` responses — closed by issue #872, pass 1.14 below.)*

### The 1 MASVS `partial` row

**STORAGE-5** — the login screen's password field masks and autofills correctly but keeps default
keyboard options, so it lacks the `KeyboardType.Password` IME-learning signal that register /
forgot-password / settings fields have. The one field where a password is typed most often.

MASVS also carries six positions of its own (`masvs-l1.md` § Documented positions), of which two —
P1 certificate pinning and P3 root/SafetyNet detection — were re-evaluated and kept declined under
issue #507, and two — P4 Room cache encryption and P6 screenshot/tapjacking protection — were
resolved in the affirmative and are now `satisfied`.

---

## 8. Re-verification procedure

A re-pass is a diff against this file, not a rewrite. In order:

1. `cd backend && go run ./cmd/citecheck` — must exit 0. Any failure is a citation to fix before
   anything else; the rest of the report means little on top of broken citations.
2. `cd backend && go run ./cmd/citecheck -drift` — read every candidate, **including the ones
   `citation-drift.ignore` already accepts**: a verification pass is exactly when a standing
   suppression should be re-justified or deleted. Correct the range, or accept it with a reason.
   This is the step that found F-1.

   **Run this step last, after every other edit in the pass, and then run it again.** Your own
   changes move lines too. In this pass, the `unit-tests.yml:175-177` → `:201-203` correction for
   govulncheck was made *before* the `docs-citations` job was inserted into that same file, and the
   40 lines of new job pushed govulncheck to `:241-243` — re-breaking a citation that had just been
   fixed, in the very commit that added the gate. The exact gate stayed green throughout (the range
   was still in bounds); only the drift pass saw it. Merging `main` into the branch has the same
   effect and needs the same re-run.
3. Run the suites in §3 and record the results, including scale, so a shrinking suite is visible.
4. Redo the four manual audits in §4. A/B/C/D are cheap: the scripts and one-liners behind them are
   described inline, and each produced a candidate list of single digits.
5. Re-read the `not-applicable` rows against the current architecture. They are written-down
   decisions about a single-process, self-hosted, no-cloud, RP-only-OIDC system; an architecture
   change is what flips them, and nothing else will.
6. **Re-examine §9's surface table for categories that did not exist last pass.** The enforced rows
   take care of themselves; this step exists for the row that is not in the table yet. Ask what
   kinds of security-relevant surface this release added — a new client, a new outbound integration,
   a new persistence target, a new authentication path — and for each, either name the mechanism
   that fails when the next one is added, or file the issue that will build it. A new category with
   neither is the gap this whole section exists to prevent.
7. Re-check every P1–P5 revisit trigger in §7.
8. Update the header (pass number, date, commit), the census (from `citecheck`), §6 findings, and add
   a changelog row (§10). If a mechanism in §9 has been superseded — most likely by #608 folding this
   into the release workflow — update §9 to describe what actually runs, and delete what it replaced.

## 9. Keeping this true between passes

A verification report is a photograph. What stops it becoming a *historical* photograph is three
mechanisms at three different cadences, deliberately unequal in cost.

**Every PR — automated, no human in the loop.** The `Security-doc citations` job
(`.github/workflows/unit-tests.yml`) runs `citecheck`, unfiltered by path. It fails on a citation
that stops resolving, a line that falls out of range, a vanished test identifier, a `satisfied` row
that cites nothing, an unaccepted drift candidate, or a stale entry in
`docs/security/citation-drift.ignore`. This is what keeps §2 true continuously rather than at
audit time, and it is the reason a re-pass is an hour instead of a rebuild.

**Every milestone — one citable checkbox.** All 16 gate issues (#531–#543 plus #500/#503/#525) carry
a standing criterion: the citation job is green on the merge commit and no unjustified suppression
was added. `.github/ISSUE_TEMPLATE/milestone_gate.md` carries it forward so a future gate inherits it
rather than depending on someone remembering this issue existed.

**Every release — a full re-pass.** The three release gates (#500 `0.8.0`, #503 `0.9.0`, #525
`1.0.0`) additionally require the ASVS/MASVS claim to be re-verified against the *shipped* code, with
a dated changelog row below as the citation. Re-running the whole pass every milestone would be
disproportionate and would get skipped; letting a published claim go unverified from v0.6.1 through
1.0.0 is the failure this tier exists to prevent. #525 had no security criterion at all before this
pass — it would have shipped the 1.0.0 stability contract on a claim last checked thousands of
commits earlier.

Two things about that model are worth being honest about:

- **The middle tier is scaffolding.** Sixteen hand-maintained checkboxes are exactly the kind of
  duplication this report criticises elsewhere. It is the right thing *now* — waiting for a release
  process to exist would let several milestones ship with no check — but it should be superseded by
  the automated release workflow (#499), which is issue **#608**. That issue's explicit instruction
  is to end with *fewer* homes for the obligation, not more.
- **None of the three sees genuinely new surface.** They prove existing claims still hold; they
  cannot notice that something was added that *deserves* a row. What covers what today:

| New surface | Enforced today? |
|---|---|
| A route | **Yes.** `routes/authorization_matrix_test.go` enumerates from the live router and fails when a new route has no declared authorization row. Self-maintaining. |
| A new authentication path (a route that mints a session) | **Yes.** `routes/session_minting_route_gate_test.go` recovers the ordered middleware chain of every route from gin's live routing tree (`router.Routes()` exposes only the final handler) and asserts, against a declared `sessionMintingRoutes` table that fails in both directions: every session-minting route carries both `AuthRateLimitMiddleware` and `EnforceMinClientVersion` ahead of its handler (rate limiter outermost), and **no other route carries `EnforceMinClientVersion`** — so a new auth path built to the #692 pattern is caught until it is declared, and the floor spreading to a non-minting route is caught too. A companion test drives the four routes through the real `RegisterRoutes` wiring with a floor configured and asserts a below-floor client is refused `403 CLIENT_NOT_SUPPORTED` before any handler runs (guarding against the middleware being neutered while still present). `AuthRateLimitMiddleware` was given a distinct closure so the chain walk can tell it from the API/CardDAV limiters. Built by issue **#840**, filed by the v0.6.10 gate. |
| An outbound HTTP client | **Yes, in two layers.** (1) The semgrep rule `mycorrhizal-unguarded-outbound-dialer` (`backend/.semgrep/mycorrhizal-traps.yaml`, issue #609, run by `sast.yml`) fails at introduction on any `http.Transport` literal whose `DialContext` is not `httputil.SafeDialContext` — the tree-wide mechanical gate. (2) INT-01 (#464) added `backend/integrations/matrix_test.go`: `TestEveryOutboundClientIsClassified` enumerates every file under `backend/services/` opening an outbound client and fails on one with no classification row; `TestSSRFClaimsMatchSource` fails when a row claims a guarded posture its source does not back up. The one gap neither layer covers is a client built **inside a library**, where there is no literal to match and (before INT-01) no row to demand a posture — exactly the OIDC case (`go-oidc`'s internal client). INT-01's manual classification caught it and INT-02 (#465) closed it (`newOIDCHTTPClient`, `OIDC_BLOCK_PRIVATE_URLS`); a future library-internal client is caught by `TestEveryOutboundClientIsClassified` demanding a row, whose `SSRF` field then forces the decision. |
| A cookie | **Yes.** `controllers/cookie_flags_test.go` drives every flow that mints or clears a cookie against the real migrated schema and enumerates the `Set-Cookie` surface from the responses, asserting `HttpOnly`/`Secure=cfg.CookieSecure`/`SameSite` per name against a declared table that fails in both directions (an undeclared cookie observed, a declared one never observed) — run twice, once `CookieSecure=true` and once `false`. Built by issue **#610**. |
| An entity or table | **Yes.** `controllers/delete_cascade_coverage_test.go` enumerates every table from the real migrated schema and requires a declared deletion bucket (`go-cascade-user`/`go-cascade-contact`/`fk-cascade-user`/`exempt`), failing on an unclassified table and on a stale declaration; it asserts `fk-cascade-user` tables really carry an `ON DELETE CASCADE` FK to `users`, rejects a contact-scoped table relying on a cascade from the soft-deleted `contacts` row (trap 6), and behaviorally verifies `DeleteUser`/`deleteContactAssociations` empty every declared table. Built by issue **#611**. |
| A crypto call site | **Yes.** `cmd/citecheck`'s crypto-surface gate enumerates every non-test Go file importing `crypto/*`, `golang.org/x/crypto/*` or a JWT/signing library and requires each to be cited by a V6 row in `asvs-l2.md` or to carry a justified entry in `docs/security/crypto-surface.ignore` — failing in both directions (a new unaccounted call site, and a declared one that stops importing crypto). Lands in the existing `Security-doc citations` job, so it fails the build at the moment of introduction. Built by issue **#612**. |
| A persistence target for instance data | **Partly.** `v0.6.2` added several: `system_events` (#424), `job_runs` (#391), `import_runs` (#651), `storage_samples` (#652) and `alert_states` (#428), plus the `webhook_deliveries` retention window (#622). All are system-generated operational/telemetry records, admin-only (or per-user non-sensitive), hard-delete, with a retention knob (`SYSTEM_EVENT_RETENTION_DAYS`, `JOB_RUN_RETENTION_DAYS`, `STORAGE_SAMPLE_RETENTION_DAYS`, `WEBHOOK_DELIVERY_RETENTION_DAYS`), and are recorded in `data-retention-lifecycle.md` (§3, §16–§20). Free-text fields are sanitized and length-capped (row 7.3.1; `import_runs` stores counts only, no messages). They are outside the cascade-coverage concern above (no user-data parent, nothing cascades into them — `import_runs` is the one exception, explicitly swept by `DeleteUser` in `admin_user_controller.go` and asserted by #611's behavioral sweep). No mechanical check yet asserts a *new* diagnostic/telemetry table gets a retention-lifecycle row — folded into #611's schema-driven coverage scope. |
| A privileged CI credential (repo-write outside PR review) | **Partly.** `release.yml` (interim REL-06, #499) mints a GitHub App token to push the release commit + tag directly to `main`, bypassing branch protection — the first non-human writer to `main`. Constrained by: the App's own installation scope; `permission-contents: write` on the minted token (nothing else); a single `workflow_dispatch`-only workflow with no PR-triggered or `push`-triggered path to it; and the App being the only non-human entry on `main`'s branch-protection / tag-ruleset bypass lists. No mechanical check asserts a *second* such credential or bypass entry does not appear — that judgement sits with the milestone gate until #499/#608 fold release into one audited workflow. |

The pattern worth generalising from the one row that *is* enforced: the authorization matrix is strong
evidence because it derives its subject list from the running system and **fails on an undeclared
member**, in both directions. Every future "is this still true?" check here should be built that
shape — #609, #610, #611, #612 and #840 are all written to that spec on purpose.

### Why not a recurring audit issue

The obvious alternative — schedule a periodic "look for new surface" issue — was considered and
rejected for the rows above, because it fires on a calendar rather than on the event that matters.
It finds a new unguarded client weeks after merge, assigned to someone without the context, with no
forcing function; a periodic issue can always be closed with "looked, seemed fine". Converting each
row to a check that fails *at the moment of introduction* is strictly better, and is what #609/#610/
#611/#612 do.

What a calendar genuinely cannot be replaced for is the row that does not exist yet: a surface
*class* nobody has thought of — a new client platform, a new persistence target, a new auth path.
No mechanical check can enumerate categories that have not been invented. That judgement is carried
by the milestone gate instead of a schedule, because the person closing a gate knows what that
milestone just added, and someone opening a quarterly reminder does not. It is the last standing
criterion in `.github/ISSUE_TEMPLATE/milestone_gate.md`.

That criterion has now been exercised once, which is the only evidence that it works. Closing the
v0.6.1 gate (#531) asked the question of this milestone and found two answers: the new outbound
integration (HIBP, #561) was already covered by the outbound-client row above, and the two new
cryptographic call sites (#380, #381) landed against the one row that named neither a mechanism
nor an issue. Issue **#612** is that gap, filed by the gate rather than by a scheduled audit —
which is the behaviour this design predicted, on the first attempt.

The one case where a schedule *is* right — risk that changes with time rather than with code
(dependency CVEs, expiring certificates) — is already covered by the nightly tier (Grype, Trivy,
govulncheck).

## 10. Changelog

| Pass | Date | Commit | Claim | Findings |
|---|---|---|---|---|
| 1 | 2026-08-26 | `6a7cb7a2` | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | 6 (F-1…F-6): 4 documentation defects fixed on the branch, 2 code findings filed as issues #605 and #606. No control flipped to `fail`. |
| 1.1 | 2026-08-27 | (see PR) | ASVS L2 with **29** documented exceptions; MASVS-L1 with 1 | Not a full re-pass — a single-row delta. Issue #509 added `docs/security/incident-response.md` (operator incident-response + credential/key-rotation runbook, rotation procedures exercised against a real build). The stated gap for **1.6.1** and **1.6.3** was "no standalone rotation doc"; both move `partial → satisfied` in `asvs-l2.md`. §7's "Partial mechanism" group and the headline exception count are Pass-1 figures and reconcile fully at the next release re-pass; the running count is 29. |
| 1.2 | 2026-08-27 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | Not a full re-pass — issue #606 (finding F-5) landed: the test-notification endpoint's echoed diagnostic is now capped at 256 bytes and, under `WEBHOOK_BLOCK_PRIVATE_URLS`, the SSRF guard's sentinels collapse to one neutral message client-side while the full error stays in the server log (§4 D, row 7.4.1). The row remains `satisfied` with the same documented exception (a bounded diagnostic instead of the envelope) — no count changes. |
| 1.3 | 2026-08-27 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | Not a full re-pass — issue #389 added an opt-in Prometheus `GET /metrics` endpoint. It is a **new authenticated operational route**, not an unauthenticated one: registered only when `METRICS_TOKEN` is set, gated by a constant-time bearer check, exposing bounded-cardinality counters with no log lines or per-user data. §4 audit A's "**15 registrations**" unauthenticated-route count is **unchanged**. Row 1.7.2 notes the surface; `crypto/subtle` in the new handler is a written-down exception in `crypto-surface.ignore` (comparison helper, not a primitive choice). No control flipped. |
| 1.4 | 2026-08-28 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | The v0.6.2 milestone-gate closure (issue #532). Not a full re-pass — records the milestone's surfaces for §9's "new class" question: the new authenticated admin observability routes (`/admin/system-status` #388, `/admin/diagnostics` #423, `/admin/job-runs` + `/job-runs/health` #391, `/admin/notification-health` #422, `/admin/subsystem-health` #427, `/admin/error-aggregation` #426) are covered by the §9 route row's authorization matrix; the update-availability check (#650) is a new outbound client covered by the §9 SSRF rule (#609); the new `import_runs`/`job_runs`/`storage_samples`/`alert_states` tables and the webhook-delivery retention job (#622) are covered by the §9 entity/table row (#611) plus `data-retention-lifecycle.md` §3/§16–§20. Two gate-closing fixes landed: migration failures now name the failing version and file and record a `migration_failed` system event (`backend/database/migrate.go`, pinned by `TestMigrationFailureIdentifiesMigrationAndRecordsEvent`), and export failures now carry `operation` + `category` in both the structured log and the response `error.details` (`backend/controllers/export_controller.go`, pinned by the export `_DBError_IdentifiesOperationAndCategory` tests). No control flipped, no count changes. |
| 1.5 | 2026-09-01 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | The v0.6.5 milestone-gate closure (issue #535). Not a full re-pass — records the milestone's surfaces for §9's "new class" question: the Monica import assistant (#549) adds a **new outbound HTTP client** (`backend/monica/client.go`), covered by the §9 SSRF-rule row (#609); it is not a regression — `monica.NewClient` routes through the shared `httputil.SafeDialContext` guard when `blockPrivate` is set, and the API token is held in the in-memory session only (`asvs-l2.md` 2.10.4 / 5.2.6, `data-retention-lifecycle.md` §12a). The Meerkat import assistant (#550) is an uploaded-file path reusing the shared import framework, not a new client (`data-retention-lifecycle.md` §12b). The new `import_source_links` idempotency ledger (migration `000045`, #724) is covered by the §9 entity/table row (#611): it is declared `fk-cascade-user` in `backend/controllers/delete_cascade_coverage_test.go` and behaviorally swept by that test's `DeleteUser` assertion, backed by a real `ON DELETE CASCADE` FK to `users`. No new authentication path. One gate-closing test gap closed on the branch: failure-path assertions for the two import assistants — `TestMonicaImportSession_AvatarFailurePathCounted` and `TestMeerkatImportSession_ConfirmWithMergeAction` (issue #725). No control flipped, no count changes. |
| 1.7 | 2026-09-02 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | INT-02 (issue #465), not a full re-pass. INT-01's manual integration classification (#464) found one outbound client the semgrep gate (#609) structurally cannot see — `go-oidc` builds its HTTP client inside the library, so there is no `http.Transport` literal to match. INT-02 closes it: `newOIDCHTTPClient` (`backend/services/oidc_service.go`) wires `httputil.SafeDialContext` when the new `OIDC_BLOCK_PRIVATE_URLS` flag is set (default off — LAN identity providers are common self-hosted), threaded through discovery/token/JWKS/UserInfo via `oidc.ClientContext`; pinned by `services/oidc_service_test.go`. Row **5.2.6** gains the OIDC citation; the §9 "outbound HTTP client" row is rewritten to name the library-internal gap and how the INT-01 matrix's per-row `SSRF` field forces the decision for the next one. Also bounded the previously-unbounded outbound email paths (`smtpDialTimeout`/`smtpDeadline`, `resendRequestTimeout`) — a resource-exhaustion hardening, not a control change; row **9.2.1**'s `mailer.go` citation moves with the STARTTLS refactor. No control flipped, no count changes. |
| 1.8 | 2026-09-03 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | INT-03/04 (issues #466/#467), not a full re-pass. **No new class of security-relevant surface.** No new outbound client (`integrations.RetryPolicy` / `DispositionForHTTPStatus` are pure classification helpers; the webhook and sync clients are unchanged). No new outbound integration. No new authentication path (`terminal_reason` / `failed_permanently` are diagnostic state, not credentials; the `Idempotency-Key` *header this now sends on outbound webhook deliveries* is echoed from the existing event-envelope UUID, sent so a receiver can de-duplicate — it is our data leaving, not a new secret). **New persistence:** columns only — `webhook_deliveries.failed_permanently`/`terminal_reason` and `{contact,calendar}_subscriptions.terminal_failure_at`/`terminal_reason` (migration `000049`), all derived diagnostic state on rows already inventoried in `data-retention-lifecycle.md` (webhook deliveries §16, subscriptions §3/§4); no new copy of user content, so no new retention window. `terminal_reason` is a fixed slug set; `last_error` strings were already sanitized/length-capped by `RecordSystemEvent` / `clampRunes`. The new CardDAV `ContactSyncSettings` panel is a frontend view over the existing authenticated `/api/v1/contact-subscriptions` routes (§9 route row's authorization matrix). See ADR 0013. No control flipped, no count changes. |
| 1.6 | 2026-09-01 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | The v0.6.7 milestone-gate closure (issue #537). Not a full re-pass — records the milestone's surfaces for §9's "new class" question. **No new client** (web and Android are unchanged; CON-01…04 add only request-header preconditions to the existing authenticated group). **No new outbound integration** — the concurrency work is entirely inbound REST/CardDAV. **No new authentication path** — `If-Match` (ADR 0008) and `Idempotency-Key` (ADR 0010) are preconditions evaluated *after* `AuthMiddleware`, not credentials. **New persistence targets:** `idempotency_keys` (migration `000047`, #459) and the `job_execution.last_outcome` column (migration `000048`, #526), both covered by the §9 entity/table row (#611) — `idempotency_keys` is declared in `backend/controllers/delete_cascade_coverage_test.go`, behaviorally swept by that test's `DeleteUser` assertion (user-scoped hard delete), and recorded in `data-retention-lifecycle.md` §22. One property distinguishes it from the v0.6.2/v0.6.5 tables in that row, which are telemetry: `idempotency_keys.response_body` transiently holds a copy of the created entity (user content), bounded by `IDEMPOTENCY_KEY_RETENTION_HOURS` (default 24, `<= 0` disables) via the job-locked `PurgeExpiredIdempotencyKeys` cron — §22 covers that copy and its window. CON-01/CON-04 refreshed drifted citation ranges in `asvs-l2.md`/`threat-model.md` in-commit and added `idempotency.go` to `crypto-surface.ignore` (`sha256` = request fingerprint, not a primitive choice). No control flipped, no count changes. |
| 1.9 | 2026-09-03 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | The v0.6.9 milestone-gate closure (issue #539). Not a full re-pass — records the milestone's surfaces for §9's "new class" question. **No new class of security-relevant surface.** **No new client** — the web `ContactSyncSettings` panel is a view over the existing authenticated `/api/v1/contact-subscriptions` routes; Android is unchanged. **No new outbound integration** — INT-01…04 (#464–#467) classify and harden the *existing* 15 external systems (`docs/int-01-integration-classification-matrix.md`, generated from `backend/integrations`); INT-02's OIDC guarded dialer + `OIDC_BLOCK_PRIVATE_URLS` closed a pre-existing library-internal gap and is already recorded (row 1.7). **No new authentication path** — `terminal_reason` / `failed_permanently` are diagnostic state; the outbound `Idempotency-Key` header is our own event-envelope UUID leaving, not a secret (row 1.8). **New persistence: columns + one index only.** Migration `000049` (INT-04) adds derived diagnostic columns to `webhook_deliveries` / `{contact,calendar}_subscriptions`, on rows already inventoried in `data-retention-lifecycle.md` §3/§4/§16 (row 1.8). Migration `000050` (the CAP-01 / DB-01 fix, #498) adds a partial unique index on `contacts(user_id, vcard_uid)` — no new table, no new data. `backend/internal/perfbench` (#469–#471), `backend/internal/largedata` (#468) and `backend/internal/diskspace` (#498) are test/dev infrastructure with no runtime network or privilege surface; `frontend/scripts/check-bundle-budget.mjs` (#556) is a build-time check. **No new privileged CI credential** — `.github/workflows/chaos-tests.yml` (#498) is `permissions: contents: read` throughout, no `secrets.*`, no `id-token`. The §9 mechanical checks (`authorization_matrix_test.go`, the semgrep unguarded-dialer rule, `delete_cascade_coverage_test.go`, `citecheck`'s crypto-surface gate) all pass on the merge commit. No control flipped, no count changes. |
| 1.10 | 2026-09-07 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | The v0.6.10 milestone-gate closure (issue #540). Not a full re-pass — records the milestone's surfaces for §9's "new class" question. **New authentication path:** the device-grant session exchange (`POST /auth/device/session`, `device_grants`, migration `000051`) from issue #722's biometric session-resume — a device holding an unrevoked grant exchanges it for a fresh session JWT, rate-limited and floor-enforced exactly like `/login` (`routes.go:60-67`). It is a new *instance* of an existing covered class, not a new class: the §9 route row's authorization matrix enumerates it from the live router (public persona) and the entity/table row (#611) sweeps `device_grants` (declared `goCascadeUser` in `backend/controllers/delete_cascade_coverage_test.go:98`, behaviorally asserted by that test's `DeleteUser` sweep; lifecycle in `data-retention-lifecycle.md` §8, `asvs-l2.md` rows updated in-commit by #722). **New auth hardening, not a new path:** `EnforceMinClientVersion` (`backend/middleware/client_version.go`) now guards all four session-minting routes ahead of any credential work, and `/health` advertises `min_client_version`/`api_contract_version` (`health_controller.go:53,61`) — the floor mechanism behind #528/#692, pinned by `client_version_route_test.go`. **No new client, no new outbound integration, no new privileged CI credential.** The one genuinely new *sub-class* the milestone's auth path exposed — a future session-minting route that could ship without rate limiting + the client-version floor — is recorded as the new §9 auth-path row, naming issue **#840** (the mechanical check it builds is what closes that row). No control flipped, no count changes. |
| 1.12 | 2026-09-09 | (see PR) | ASVS L2 with **29** documented exceptions; MASVS-L1 with 1 | Issue #866 (pen-test #860 finding F-6), not a full re-pass. **Two controls flipped `partial → satisfied`:** server-side session records (`sessions` table, migration `000053`; `sid` JWT claim; `middleware/auth.go:156-186`) now back per-device logout revocation and an idle timeout. **3.3.2** — `SESSION_IDLE_TIMEOUT_HOURS` (default 12 h, `0` disables) rejects an idle session before its 96 h absolute expiry, removing the "no idle timeout" deliberate-position exception. **3.3.4** — `GET/DELETE /api/v1/sessions` + `SessionsSettings.tsx` give a real per-device inventory with immediate revocation, removing the "no session-inventory UI" exception. **3.3.1** re-cited (logout now revokes server-side, not just clears the cookie); **3.3.3** re-cited (`RevokeAllSessions` runs beside every `TokenVersion++`); **1.11.2** reworded (session state is one SQLite table on the shared pool, not an out-of-sync cache); **6.3.1** gains the `session_service.go` CSPRNG citation. **New persistence:** `sessions` — operational bookkeeping, hard-delete, user-scoped (declared `goCascadeUser` in `delete_cascade_coverage_test.go`, swept by `DeleteUser`), TTL-purged by the job-locked `PurgeExpiredSessions` cron; `user_agent`/`ip` captured display-only, no new copy of user content. Lifecycle in `data-retention-lifecycle.md` §23. **New auth mechanism, not a new path** — the `sid` check runs *after* `AuthMiddleware`'s existing credential + `token_version` checks. Exception count 31 → 29. See ADR 0017. |
| 1.11 | 2026-09-08 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | Issue #566, not a full re-pass — closes the one gap the #377 threat-model draft recorded against the authorization matrix. The #371/#551 matrix (`authorization_matrix_test.go`) probes six personas that all authenticate the same way (cookie JWT). The new sibling `backend/routes/authorization_matrix_credentials_test.go` extends the same live-router enumeration to the two credential types that are not a cookie JWT: a **CardDAV/CalDAV Basic-auth credential** (asserted 401 on every `/api/v1/*` route — the REST layer only reads a cookie or `Bearer` token — and, on the `/carddav`+`/caldav` surface, reaches only its own collections, never another user's), and a **`carddav`-scoped API token** (asserted 403 on every REST route, `middleware/auth.go`), with a third column asserting a **`full`-scoped API token** matches the cookie-JWT `owner` verdict on every route (scope enforcement neither under- nor over-grants). Reuses `buildTable` + the completeness guard, so a new REST route with no row fails here too; a second guard covers the registered DAV routes. **No new class of security-relevant surface** — no new route, client, outbound integration, authentication path, persistence, or CI credential; this is test-only. Rows updated in-commit: `asvs-l2.md` **API5** gains the citation; `threat-model.md`'s "Malicious/misconfigured CardDAV client" and "Malicious API client" actor rows move off **gap**. No control flipped, no count changes. |
| 1.12 | 2026-09-09 | (see PR) | ASVS L2 with 31 documented exceptions; MASVS-L1 with 1 | Issue #861, not a full re-pass — a **claim correction** of the same class as Pass-1's F-6 (a row claiming more than the code does). The pen-test engagement (#860, Tester A finding F-1) reported the flat CSV export as leaking `private`/`secret` and `status: suggested` relationship edges. The **behaviour is deliberate and unchanged**: `controllers.ExportData` is the user's own full personal-data backup — it takes no `sections`/`include_sensitive` params (stated in `openapi.yaml`), carries every sensitivity and status labelled by its own column, and is the only full-fidelity export offered, so filtering it would be silent data loss in the one file a user relies on to hold everything. What was wrong was the **documentation**: `asvs-l2.md` rows **1.5.1**, **1.8.2** and **8.3.4**, `data-retention-lifecycle.md` §11, `pii-inventory.md`, `docs/privacy.md`, `README.md`, and CLAUDE.md all asserted the blanket rule "above `normal` is excluded from exports", which was never true of the CSV — a false claim that a black-box tester correctly read as a leak. All corrected in-commit to state the real rule (sensitivity governs copies that leave the instance or reach another party — external sync, contact shares, the neutral-`Card` exports — and is not an access-control tier against the owning user) plus the CSV exception and its rationale. The asymmetry is now **pinned**, not just asserted: `controllers/export_csv_full_fidelity_test.go` (`TestExportCSV_IsFullFidelityBackup_UnlikeVCard`) asserts over one seeded dataset that the CSV withholds nothing and that vCard default-denies exactly the private/secret rows, with a normal-sensitivity control paired to every absence assertion; hand-verified to fail on all four halves (CSV edges, CSV field values, CSV preferences, vCard filter). The web UI's export panel now states what the file contains before the user downloads it. **No control flipped, no count changes, no code behaviour changed.** |
| 1.13 | 2026-09-09 | (see PR) | ASVS L2 with 29 documented exceptions; MASVS-L1 with 1 | Pen-test #860 (Tester A) findings **F-2** (issue #862) and **F-7** (issue #867), not a full re-pass — an auth-hardening delta on `POST /api/v1/login` (+ `/login/2fa` + CardDAV Basic auth). **F-2 (login user-enumeration by timing):** the unknown-identifier branch returned without ever running bcrypt (~11 ms vs ~55 ms). Now the shared `services.SpendDummyPasswordHash` (`backend/services/user_service.go:137-151`) spends an equivalent cost-10 comparison on that branch in both `LoginUser` and `carddav.BasicAuthMiddleware` (the latter refactored off its own local dummy hash). The 401 body/status were already constant. **F-7 (account lockout weaponised for DoS):** the per-account lockout keyed only on the identifier, with no source component and no admin-unlock control, so anyone who knew a victim's identifier could keep any account — including the sole admin — continuously locked out. The lockout now keys on `(identifier, source-IP)` (`middleware/login_lockout.go`, `LoginKey`), so a failed run only denies the source that caused it; a per-identifier backstop (`GlobalAccountLoginAttempts` = 30 across all IPs → a **fixed** `GlobalAccountLockoutDuration` = 15 min, bypassed for IPs that authenticated within `KnownGoodIPTTL`) still bounds an IP-rotating attacker without denying the real user. Rows updated in-commit: `asvs-l2.md` **2.2.1**, **API2**, **8.1.4**, **API4**, **1.11.2** and the rate-limiting summary; drifted `user_controller.go` citations in rows 2.1.10 / 2.2.3 / 2.5.3 / 5.1.5 re-pointed and the #722 drift-ignore entry followed. **No new class of security-relevant surface** — no new route, client, outbound integration, authentication path, persistence, or CI credential; the lockout state stays the same in-memory, per-key, restart-visible map. Tests: `middleware/login_lockout_test.go`, `controllers.TestLoginUser_UnknownIdentifier_ResponseIsIndistinguishable` / `TestLoginUser_Griefing_DoesNotLockLegitimateIP` / `TestLoginUser_SameIP_StillLocksAfterMaxAttempts`, `carddav.TestBasicAuthMiddleware_Griefing_DoesNotLockLegitimateIP`, `services.TestSpendDummyPasswordHash` — each hand-verified to fail with the fix reverted. No control flipped, no count change (F-2/F-7 were open findings, not exception-register entries). |
| 1.14 | 2026-09-09 | (see PR) | ASVS L2 with **28** documented exceptions; MASVS-L1 with 1 | Issue #873 (credentialed pen test, Tester B), not a full re-pass. **One control flipped `partial → satisfied`: 2.8.4** (TOTP single-use within validity). A TOTP code that passed `valid2FAProof` was not burned, so it could be replayed to authenticate a second independent session inside its ±1 step (~90 s) window — recovery codes were already single-use, TOTP was not. Fix: `users.totp_last_used_step` (migration `000054`) records the RFC 6238 counter step of the last accepted code; `services.ValidateTOTPStep` reports a code's step and `services.BurnTOTPStep` does a single conditional `UPDATE … WHERE totp_last_used_step IS NULL OR totp_last_used_step < ?`, so a replay (or an older step) loses the compare and is rejected — the same atomic-`WHERE` single-use shape `ConsumeRecoveryCode` uses (V2.6.1, V11.1.6). `valid2FAProof` (login, 2FA disable, recovery-code regen) and `ConfirmTwoFactor` (enrollment) both burn the step; disable / admin 2FA reset clear it to NULL. **2.8.5** stays `partial` but is reworded: a replayed code is now rejected and recorded as a failed 2FA step (`AuditOpLoginFailed`) that counts toward the account lockout, so reuse is detected and logged — the residual gap is only the absence of a proactive owner notification (the 2.2.3 / 2.5.5 gap). **No new class of security-relevant surface** — no new route, client, outbound integration, or authentication path; `totp_last_used_step` is a derived monotonic marker on the already-inventoried `users` row (`data-retention-lifecycle.md` §4), not a new copy of user content. Tests: `backend/services/twofactor_replay_test.go`, `backend/controllers/two_factor_controller_test.go` (`TestTwoFactor_TOTPReplayRejectedWithinWindow`, `TestTwoFactor_ConfirmRecordsStepSoEnrollmentCodeCannotLogin`), each hand-verified to fail without the burn. Exception count 29 → 28. |
| 1.15 | 2026-09-09 | (see PR) | ASVS L2 with **26** documented exceptions; MASVS-L1 with 1 | Pen-test #860 (Tester B) findings **#869** and **#872**, not a full re-pass. **Two controls flipped `partial → satisfied`:** `SecurityHeadersMiddleware` (`backend/middleware/security_headers.go:49-57`) now sets `Cache-Control: no-store` on every `/api/` response. **8.1.1** and **8.2.1** — both were in §7's "known small gaps with an obvious fix" group with the identical gap text ("no `no-store` on API responses"); the header closes the shared/intermediary-cache and browser-bfcache retention of authenticated JSON. Scoped to the `/api/` prefix, which is the whole boundary — this Go process serves no static assets (the SPA's hashed, ETag'd files are nginx's and keep their long-cache). Pinned by `backend/middleware/security_headers_test.go` (`TestSecurityHeadersMiddleware_CacheControlNoStoreOnAPI`, `TestSecurityHeadersMiddleware_NoCacheControlOffAPI`), hand-verified to fail with the header removed. Issue **#869** is the paired change and does **not** move a row: `deliverWebhook` (`backend/services/webhook_service.go`) stopped storing the raw Go transport/URL-parse error on the webhook delivery record (an internal port-scan oracle echoed via `GET /api/v1/webhooks` and `POST /api/v1/webhooks/:id/test`), collapsing it to two generic constants with the detail logged server-side — row **7.4.1** gains a sentence alongside the existing notification-endpoint exception; the receiver's own `"unexpected status N"` is unchanged. **No new class of security-relevant surface** — no new route, client, outbound integration, authentication path, persistence, or CI credential; both changes are within existing middleware/service code. Drifted `security_headers.go` citations (the `strings` import shifted every line) re-pointed in-commit across `asvs-l2.md`, `citation-drift.ignore`, and `deployment-baseline.md`. Exception count 28 → 26 (this pass follows #873's pass 1.14). |
