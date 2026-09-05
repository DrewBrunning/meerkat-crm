# Security checklist: OWASP ASVS L2 + API Security Top 10 (2023)

The living answer to "is this secure?" — a per-control map of two recognized standards to the
code and tests that satisfy them. If a control's status changes, the PR that changes it updates
this file. A security-sensitive PR should touch the relevant row here; if it cannot point at a
row, it does not know what it is changing.

| | |
|---|---|
| **Standards pinned** | OWASP ASVS 4.0.3 (V1–V14), OWASP API Security Top 10 (2023) |
| **Level** | ASVS **Level 2** (rows marked `✓` in the L2 column of the ASVS). L1 rows are included because L2 subsumes them; L3-only rows are listed per chapter as out of scope. |
| **Last full pass** | 2026-08-26 — the ASVS L2 verification pass, issue #378. Statuses, evidence, and the level claim are recorded in `docs/security/asvs-l2-verification-report.md`; the prior mapping audit was 2026-08-23. |
| **Level claimed** | **ASVS L2 with 31 documented exceptions** — every exception enumerated in the verification report's exception register. Not a silent downgrade: 186 rows `satisfied`, 38 `not-applicable` with reasons, 2 L3-only. |
| **Scope** | Single-process Go/Gin + SQLite app, React SPA behind an nginx container, TLS terminated by an operator-supplied reverse proxy. Many cloud/microservice controls are deliberately `not-applicable` — each such row says why. |

## Status legend

| Status | Meaning |
|---|---|
| **satisfied** | Control is met; the row cites `file:line` or a test name. |
| **partial** | Control is met in part, or met with a documented deviation (each `partial` row names the gap). |
| **not-applicable** | Control does not apply (single-process, self-hosted, no cloud, no XML, …) — one-line reason given. |
| **out-of-scope** | L3-only control (not in the L2 target); listed for grep-ability. |

"Answered in the doc" means: take any `Vx.y.z` from ASVS 4.0.3 or `API#` from the 2023 Top 10,
grep for it below, and get a status + citation. No row is left `satisfied` without a citation.

## Documented positions

Five deliberate, written-down decisions this project has made. They are positions, not code
changes; revisit each pre-1.0 or when the cited trigger happens.

### P1 — Password hashing: bcrypt, not Argon2id (NIST 800-63B §5.1.1.2)

Passwords are hashed with **bcrypt at cost 10** (`bcrypt.DefaultCost`) in the single shared
`services.HashPassword` (`backend/services/user_service.go:25`). Per NIST 800-63B the approved
memory-hard functions are Argon2id, scrypt, or bcrypt with a work factor of 10+ — bcrypt at 10
**satisfies** 800-63B as written (ASVS 2.4.4, min. work factor 10). Details:

- Salt: bcrypt's per-hash 128-bit random salt (ASVS 2.4.2).
- No silent truncation: passwords over 72 bytes are **rejected** with `ErrPasswordTooLong`
  (`backend/services/user_service.go:14,21-23`), so the bcrypt 72-byte cap is explicit, not a
  quiet truncation (ASVS 2.1.3).
- Deviation: ASVS 2.1.1 wants ≥ 12 characters; we enforce `min=8` + ≥ 50 bits of entropy
  (`backend/middleware/password_strength.go:21-45`), which is 800-63B's actual rule (length +
  guessability, not composition). Stronger-than-8 passwords are encouraged by the strength meter
  (`backend/controllers/user_controller.go:278-290`).

**Why not Argon2id yet:** bcrypt at 10 is adequate for this threat model (self-hosted, per-account
lockout, no federated login at rest), and migrating hashes is not a code change — it requires a
forced re-hash or password reset for every existing user, which is a data-adjacent decision for a
product with real production data (see the v0.2.0 note in `CLAUDE.md`). Decision: adopt Argon2id
in the same release that introduces a password-hash version field and a rehash-on-login path;
until then bcrypt cost is the knob (currently the library default 10; raising it is a one-line
change in `services/user_service.go`).

### P2 — Cryptographic agility (bcrypt/JWT are direct calls, not behind an abstraction)

`bcrypt` (password hashing) and HS256 `JWT` (sessions) are called directly at their call sites
(`backend/services/user_service.go:25,54`, `backend/middleware/auth.go:66-98`) rather than behind
a swap-able algorithm interface. This is a **conscious pre-1.0 decision**, not an oversight:

- Swapping either algorithm today is a code change plus a credential-rotation event, and the
  rotation machinery already exists: JWT keys rotate via `TokenVersion` + the JWT secret
  validation in `config/config.go:487-515` (all sessions die on bump), and bcrypt hashes can
  migrate via the P1 rehash-on-login path.
- ASVS 6.2.4 (algorithms swappable) is therefore marked **partial** below, with this position as
  the cited reason.
- Decision: introduce an abstraction **only** when a second algorithm is actually adopted
  (Argon2id, EdDSA sessions, or external KMS). An abstraction with one implementation is dead
  weight and a review surface.

Related at-rest note: the TOTP secret and integration credentials are AES-256-GCM encrypted with a
key HKDF-derived from `JWT_SECRET_KEY` (`backend/services/credential_crypto.go:22-29`). Rotating
`JWT_SECRET_KEY` therefore invalidates those stored secrets (documented at `credential_crypto.go:20-21`);
the ≥ 32-byte length plus placeholder/entropy rejection at boot (`config/config.go:487-515`) is what
keeps that key strong. TOTP/recovery secrets are generated at runtime from `crypto/rand` (no
env-configurable default to guard), and the credential key is derived from `JWT_SECRET_KEY`, so the
JWT checks cover every security-critical secret that can be configured (issue #393).

### P3 — Breached-password check: opt-in HIBP, off by default (ASVS 2.1.7)

Issue #376 asked for a k-anonymity breach check against Have I Been Pwned's range API at
registration/password change/password reset. Shipped as **opt-in** (`HIBP_CHECK_ENABLED`, default
`false`, `config/config.go`), not a default-on control — this is a deliberate position, not a gap:

- **The tradeoff is real, not hypothetical.** Enabling it means every new/changed password makes an
  outbound HTTP call (`services.CheckPasswordBreached`, `backend/services/hibp_service.go`) to a
  third-party service, from a self-hosted app whose whole premise is not depending on one. Only a
  5-character SHA-1 prefix of the password is ever sent (never the password or the full hash — HIBP's
  own k-anonymity protocol design), but "an outbound call happens on every password" is still a
  property an operator should choose, not one defaulted on them.
- **Fails open by design.** A network/API failure is logged and treated as "not breached, check
  skipped" (`CheckPasswordBreached`'s doc comment) — a third-party outage must never become an
  availability dependency for registering or changing a password.
- Local defense stays on regardless: the existing common-password blocklist
  (`middleware/password_strength.go:168-198`, cited at 2.1.7 below) runs unconditionally, with no
  network dependency and no opt-in required.
- **Decision:** stays opt-in through pre-1.0. Revisit defaulting it on if/when this project takes on
  a hosted-multi-tenant deployment mode, where the operator (not each self-hoster) already accepts
  outbound-call tradeoffs on users' behalf.

Since #380, user-authored PII is additionally encrypted at rest by `backend/atrest` with its own
master key — `DATA_ENCRYPTION_KEY`/`DATA_ENCRYPTION_KEY_FILE`, falling back to an HKDF-SHA256
derivation from `JWT_SECRET_KEY` for zero-config deployments (see **P4**). A dedicated key is
recommended so rotating `JWT_SECRET_KEY` never affects at-rest data.

### P4 — At-rest field encryption: FTS-plaintext exception, single wrapped DEK, lost-key posture (#380)

The at-rest layer (`backend/atrest`) is the issue #380 implementation. Three written-down positions:

1. **FTS-indexed columns stay plaintext, by design.** `notes.content`, `activities.*`, and the flat
   contact search fields are indexed by FTS5 via SQL triggers that read the base columns directly
   (migrations `000007`/`000010`/`000020`, `services/search_service.go` `RebuildSearchIndex`). SQL
   cannot decrypt, so those columns are deliberately NOT encrypted — encrypting them would silently
   break search. They are the documented searchable-plaintext set; everything sensitive that is not
   searched (contact free text, the neutral card, life-event/reminder/gift/preference/conversation
   text, audit snapshots, sync-conflict copies) IS encrypted.
2. **Single wrapped DEK, not per-row keys.** The issue recommends "envelope-encrypt per-row data
   keys so rotation doesn't require re-writing the whole DB". A single deployment DEK wrapped by the
   master key achieves the same rotation property strictly more cheaply: rotating the master key is
   a one-row `data_encryption_keys` UPDATE (unwrap, rewrap), never a payload rewrite — and per-row
   keys would add no extra security, since a master-key compromise unwraps every row's DEK anyway.
3. **"Lost key = lost data, by design."** The DEK is stored only wrapped by the master key; there is
   deliberately no escrow. Losing `DATA_ENCRYPTION_KEY` makes every encrypted column undecryptable.
   Rotation (`cmd/rotate-at-rest-key`) rewraps the DEK under a new master key.

### P5 — Backup confidentiality, retention & immutability: operator-owned boundary (#420, #505)

Backups are a **complete copy of the CRM's sensitive data** (the DB snapshot at full sensitivity plus
the photos/attachments directories), so the confidentiality bar for a backup is the same as for the
database itself. Issue #420's statement of where that bar sits:

- **Encryption is inherited, not added.** A `make backup` snapshot carries the DB's field-level
  at-rest encryption with it (`encv1:` ciphertext + the wrapped DEK in `data_encryption_keys`),
  which is exactly why `VACUUM INTO` remains a plain-file backup even though the live DB is
  encrypted. The snapshot is *not* wrapped in a further layer: the FTS-plaintext columns (the same
  set as P4) and the photos/attachments directories are unencrypted by the app, and protecting those
  at rest (`age`/`gpg`/encrypted volume) is the operator's job — identical to the pre-#380 posture.
- **The key is never in the backup.** The master key that unwraps the DEK (`DATA_ENCRYPTION_KEY`,
  else HKDF-derived from `JWT_SECRET_KEY`) lives in the operator's environment. A stolen backup
  alone yields ciphertext + hashes + the FTS-plaintext set; restoring under a different key fails
  closed at boot. A JWT-derived master key means rotating `JWT_SECRET_KEY` changes the derived key,
  failing the live boot and making old snapshots undecryptable — one more reason for a dedicated
  `DATA_ENCRYPTION_KEY` (P2/P4).
- **Retention and deletion are deliberately out of the app's reach.** No backup rotation/expiry in
  this app — an app-side expirer is the same capability an attacker running as the app would
  inherit, so expiry belongs to the destination's lifecycle policy or the pull-side host (issue
  #505, same position). Retention guidance is operator-facing: `docs/deployment.md`.
- **Immutability is write-new-only in the app, off-host by architecture against a compromised host
  (issue #505).** `database.BackupSnapshot` — the whole backup-write surface, behind `make backup`
  and the automatic pre-migration snapshot — only ever creates a new file: it refuses to overwrite,
  removes only the temp it reserved, and no code path anywhere deletes/truncates/re-encrypts/rotates
  an existing backup or pre-migration snapshot. Pinned *by trying it*:
  `backend/database/backup_immutability_test.go` attempts overwrite/in-place modification and
  neighbour deletion through the app's own primitive and asserts every existing backup is
  byte-identical after, `TestBackupImmutability_NoBackupExpiryOrRotationCodeInTheApp` walks the
  backend source and fails on any `func …(prune|rotate|expire|…)…(backup|snapshot)…`, and
  `TestBackupImmutability_PreMigrationRollbackPointSurvivesRoutineRotation` proves a non-recursive
  routine sweep cannot reach the issue #530 rollback point in its `pre-migration/` subdirectory.
  On-host that is only a bug barrier — the app's uid can still `rm` its own files — so genuine
  immutability against a compromised host is the operator putting backups where the app holds no
  credential to reach: a pull-based off-host copy (documented default) or object-locked remote
  storage. Operator runbook + the verify-by-trying procedure: `docs/deployment.md` → "Backup
  immutability & ransomware resistance".
- **Restore security is verified, not assumed.** The restore drill (issue #275) restores a fresh
  snapshot into a scratch DB and compares every table's row count against live — and since #420 also
  verifies the snapshot's wrapped DEK unwraps under the current master key
  (`services/restore_drill_service.go` → `atrest.VerifyBackupDecryptable`), so a rotated/lost key is
  caught weekly, not at the moment of need. A real restore is a point-in-time rollback that
  resurrects soft-deleted rows (`docs/security/data-retention-lifecycle.md` §10).

The operator runbook — where backups live, retention schedule, soft-deleted data and age-out — is
`docs/deployment.md`'s "Backup confidentiality & retention" section, and "Backup immutability &
ransomware resistance" immediately after it for the off-host/immutability choice and its
verify-by-trying procedure; `data-retention-lifecycle.md` §10 is the per-data-type view.

### P6 — Update-availability check: opt-in GitHub call, off by default (#650)

Issue #650 asked for an "is a newer release available" signal on the admin System status page and
`BuildVersionCard`. Shipped as **opt-in** (`UPDATE_CHECK_ENABLED`, default `false`,
`config/config.go`), not a default-on control — the same position as P3's HIBP check:

- **A self-hosted instance making an outbound call is the operator's decision.** When enabled, the
  admin system-status endpoint issues one `GET` per 6h (memoized, `services/update_check.go`
  `updateCheckCacheTTL`) to `https://api.github.com/repos/DrewBrunning/mycorrhizal-crm/releases/latest`
  through the shared SSRF-guarded dialer (`httputil.SafeDialContext`, `services/update_check.go`
  `newUpdateCheckClient`), and compares `tag_name` against `buildinfo.Get().Version` with a real
  semver comparison (`golang.org/x/mod/semver`).
- **Fails soft by design.** Any lookup error (network, non-200, unparseable body) leaves `latest`
  "unknown" and `update_available` false — it renders a dash, never errors the surface it feeds. A
  non-release running version (`dev`, empty, unparseable) is never reported as "behind", even when a
  newer tag exists.
- **No data leaves the instance.** Only the version comparison happens client-side; the request body
  sent is empty. The outbound target is noted in `data-retention-lifecycle.md` §19.
- **Decision:** stays opt-in through pre-1.0, non-goal to default it on or add any auto-update
  behaviour.

### P7 — Android local app lock: biometric resume is client-side, V2/V3 unchanged (issue #722)

Android now offers an opt-in local gate — a Class 3 biometric or device-credential (PIN/pattern/
password) check before a *persisted session* is resumed into the authenticated tree, after a cold
start or a background period past a configurable grace timeout. This is a **client-side, device-local
decision** and deliberately does not touch the server's authentication surface: the resumed credential
is the same stateless JWT the server already issued (`backend/services/user_service.go`, V2/V3 rows
above), so there is no new token type, no refresh-token grant, no server-side device registration, and
nothing in this design changes the V2/V3 evidence. In particular there is **no** "remember this device
so a biometric can mint a fresh token past `exp`" grant — resuming past JWT expiry requires a normal
password (and, for a 2FA account, a fresh TOTP) login, exactly as before; an expired token lands the
user on the login screen via the existing 401 → `clearSession` path after a successful unlock, never a
dead end. The client-side rationale, threat model and decision are recorded in `masvs-l1.md` P7 and
`docs/adrs/0014-local-app-lock-and-biometric-resume.md`.

---

## V1 — Architecture, Design and Threat Modeling

L3-only, out of scope: 1.11.3.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 1.1.1 | Secure SDLC | satisfied | Issues-first backlog, one branch per concern, CI gates — `CLAUDE.md` (Workflow), `.github/workflows/`. Static (SAST/SCA) and dynamic (DAST) testing tiered (issue #578): PR gate — unit tests, `tsc --noEmit`, biome lint/format (`biome.yml`), actionlint+shellcheck (`actionlint.yml`), zizmor, semgrep (`backend/.semgrep/mycorrhizal-traps.yaml`, `sast.yml`, issue #370), CodeQL, trivy misconfig+secret hard gates (`container-hardening.yml`), security-doc citation gate (`backend/cmd/citecheck`, the `docs-citations` job in `unit-tests.yml`, issue #378 — unfiltered by path, since moving code is what orphans a citation); main merge — signed SBOM (`syft-sbom.yml`), Grype second-opinion CVE scan (`grype.yml`), TruffleHog git-history secret scan (`trufflehog.yml`); nightly — OWASP ZAP DAST (`zap-dast.yaml`, `zap-dast.yml`, gate `backend/cmd/zapgate` + canary `backend/cmd/dastcanary`), Stryker mutation testing (`stryker.yml`, report-only), full-length fuzz, CIS hardening; release — one `workflow_dispatch` (`release.yml`) registers the release + schema fixture and tags `main`, the tag triggers `docker-publish.yml` (schema-fixture-gate, three cosign-signed images + signed APK, SLSA provenance, SBOMs), pushed by a `contents:write`-scoped GitHub App token on `main`'s branch-protection bypass list (see `docs/security/asvs-l2-verification-report.md` §9). All SHA-pinned actions; SAST/SCA SARIF to the Security tab; DAST is gated by `backend/cmd/zapgate` on High/Medium with the ZAP JSON report kept as a 30-day run artifact for detail — not code scanning, since a DAST finding has no source line to anchor a SARIF result to (issue #615); ignore-lists-with-justification (`.trivyignore`, `.grype.yml`, `.trufflehogignore`, `zap/dast.ignore`) |
| 1.1.2 | Threat modeling per design change | satisfied | `docs/security/threat-model.md` (issue #377) is the standing threat model: assets, trust boundaries, actor/threat matrix, and five explicit gating-decision reviews. "Living document" convention — a design change that adds a trust boundary (new integration, sync direction, client) updates it in the same PR, per its own "How to keep this honest" section, mirroring this checklist's own convention. Prior-art threat analysis in ADRs (`docs/adrs/`) and the original 14-finding security review (`CLAUDE.md` Security posture) still stand as history. |
| 1.1.3 | User stories carry security constraints | satisfied | Issues carry explicit acceptance/security criteria, e.g. the "verified by" gates in this repo's security tickets (see e.g. T75/T26 records in `CLAUDE.md`). |
| 1.1.4 | Trust boundaries documented | satisfied | `docs/security/threat-model.md` (Trust boundaries + Actors × trust boundaries sections, issue #377) is the primary map; underlying citations: `docs/deployment.md:32` (TLS at external reverse proxy, nginx→app loopback), Cookie/`COOKIE_SECURE` boundary in `.env.example:131-145`, CORS boundary `backend/main.go:430-446` |
| 1.1.5 | High-level architecture + remote services analyzed | satisfied | `docs/deployment.md`, `docs/adrs/`, `docs/data-model.md`; remote-service clients (CardDAV/WebDAV, Immich, Seafile, Resend/SMTP) each have their own guarded client (`backend/services/immich_client.go`, `seafile_client.go`, `contact_sync_service.go`, `mailer.go`) |
| 1.1.6 | Centralized, reusable security controls | satisfied | Single auth middleware `backend/middleware/auth.go`; single validation middleware `backend/middleware/validation.go`; single rate-limiter `backend/middleware/rate_limiter.go`; single SSRF dialer `backend/httputil/safedial.go`; single error envelope `backend/errors/` |
| 1.1.7 | Secure coding checklist available | satisfied | `CLAUDE.md` (Backend/Frontend traps + Security posture) is the standing checklist all work is expected to read |
| 1.2.1 | Low-privilege OS accounts | satisfied | Non-root `appuser` in `backend/Dockerfile` + `docker/entrypoint.sh:4-5` (PUID/PGID configurable, default 1001); CIS-enforced in CI — `docker/cis-hardening.sh` (4.1 non-root, 4.8 setuid/setgid stripped) via `.github/workflows/container-hardening.yml`; operator-facing baseline (capabilities, filesystem perms) in `docs/security/deployment-baseline.md` (issue #417) |
| 1.2.2 | Component-to-component comms authenticated | not-applicable | Single process; nginx→app is loopback (`docker/nginx.conf:7,25`); no microservices, so no mTLS surface |
| 1.2.3 | Single vetted auth mechanism | satisfied | One JWT HS256 verifier for all API routes (`backend/middleware/auth.go:66-98`, wired `routes/routes.go:52-55`); CardDAV Basic auth reuses the same password/API-token validation (`backend/carddav/auth.go:24-114`) |
| 1.2.4 | Consistent auth strength across pathways | satisfied | Every protected route shares `middleware.AuthMiddleware`; CardDAV/CalDAV Basic path enforces the same password policy + account lockout (`backend/carddav/auth.go:37-48`); OIDC is the only other pathway and lands on the same JWT cookie (`backend/controllers/oidc_controller.go:249-253`). Full OIDC attack matrix — `state`/nonce/PKCE binding, authorization-code replay, issuer/audience/`azp` validation, and the `OIDC_TRUST_EMAIL`/`OIDC_AUTO_PROVISION` account-mix-up invariant (an OIDC identity may never authenticate as another local account merely because an attacker controls/knows an email address) — is pinned by `backend/services/oidc_attack_matrix_test.go` + `backend/controllers/oidc_attack_matrix_test.go`, alongside the pre-existing `oidc_service_test.go`/`oidc_controller_test.go`/`oidc_claims_test.go`/`oidc_userinfo_test.go` (issue #412) |
| 1.4.1 | Access control at trusted enforcement point | satisfied | All authorization is server-side (`routes/routes.go:52-55`, per-controller `Where("user_id = ?")`); frontend route guards are UX only |
| 1.4.4 | Single access control mechanism | satisfied | Every handler resolves the user from context (`backend/controllers/helpers.go:30-44`) and AND-scopes every query by `user_id` — canonical example `backend/controllers/circle_controller.go:49` |
| 1.4.5 | Feature/attribute-based (not just role-based) | satisfied | Ownership is per-entity (`Contact.VCardUID`, `user_id`) not role-based; admin is a separate gate (`backend/middleware/admin.go`) |
| 1.5.1 | Input/output handling requirements defined | satisfied | Sensitivity model `normal\|private\|secret` (`backend/models/dtos.go:218,352`) defines handling: >normal is excluded from exports and sync in the query (`backend/models/contact_record.go:116-136`). Issue #416 pinning: the same query-level exclusion for a custom `FieldDefinition`/`FieldValue` (not just `RelationshipEdge`) is now covered by `controllers/export_controller_test.go` (`TestExportContactsAsVCF_SecretCustomField_ExcludedByDefault_IncludedWithOptIn` and its JSContact equivalent); API-token hashes and TOTP secrets never appearing in any export is pinned by `controllers/export_secret_exclusion_test.go`. Issue #512 pinning: `reconcileContactSync` (`services/contact_sync_service.go`) never touches the `field_values`/`relationship_edges` tables at all — a hostile remote CardDAV update cannot wipe or downgrade a secret-sensitivity custom field or relationship edge, proven end-to-end by `services/contact_sync_hostile_input_test.go` (`TestReconcileContactSync_SensitiveFieldValueAndRelationshipEdgeSurviveHostileRemoteUpdate`). Issue #555 extends the same guarantee across an actual account boundary, not just an export: `controllers/contact_share_matrix_test.go`'s `TestCreateContactShare_SensitivityMatrix` asserts, against the raw stored `ContactShare.Payload` (not the API response), that private/secret data in all three sensitivity-bearing surfaces (`RelationshipEdge`, hobby `Preference`, custom-field `FieldValue`) stays out of a share without the sender's explicit `IncludeSensitive` opt-in, that normal-sensitivity data always crosses, and that selecting a sensitive section cannot itself imply the opt-in (`TestCreateContactShare_SectionSelectionAloneCannotImplyOptIn`) — the same foot-gun guard #444 built for export, now proven for sharing. Issue #442 (DATA-02) pins that these policy exclusions are *not* reported as fidelity loss: the export loss-report header (`controllers/export_loss.go` `setExportLossReportHeader`) and the preflight endpoint (`GET /export/preflight`) carry only DATA-01-matrix-classified losses, and `controllers/export_loss_test.go`'s `TestExportLossHeader_SensitivityPolicyExclusionNotReported` asserts a policy-excluded secret edge surfaces in neither the file nor the report — conflating the two would teach users to ignore the report. Issue #444 (DATA-04) runs the whole design as a matrix over the TEST-02 canonical fixture (`controllers/selective_export_matrix_test.go`): format (vCard 3/4, JSContact) × scope (single contact, full export) × opt-in, asserting default-deny through the *projected* surfaces (`RelatedTo`, `PersonalInfo`, `Passthrough.VCard`) with a normal control paired to every "sensitive absent" check so a vacuously-passing cell fails; the foot-gun guard pinned at the HTTP layer (`TestSelectiveExport_AllSectionTokensDoNotImplyOptIn` — every section token in `?sections=` still leaves `IncludeSensitive` false) and structurally (`TestSelectiveExport_FieldSelectionAllIsStructurallyOptInFree`); the "opt-in covers only the selected sections" clause (`TestSelectiveExport_OptInCoversOnlySelectedSections`); and CardDAV/CalDAV as export surfaces with no opt-in at all (`carddav/selective_export_sensitivity_test.go` `TestCardDAV_NeverServesSensitiveContactData`, `caldav/selective_export_sensitivity_test.go`) — the "filtered in the projection, not per-caller" proof, since CardDAV has no selection plumbing to re-add a per-caller filter. The DATA-02 boundary is pinned three ways in `controllers/selective_export_loss_boundary_test.go`: user-selected omissions and sensitivity-default omissions are absent from the loss report, format-driven omissions are present, and the opt-in's inclusion is not a loss. Auditability decision (issue #444 req. 8): a sensitive export is *not* individually audit-logged — a documented decision in `docs/security/data-retention-lifecycle.md` §11, not a silent gap. |
| 1.5.2 | No serialization with untrusted clients | satisfied | Wire format is JSON DTOs only (`encoding/json`); no gob/pickle/object serialization anywhere |
| 1.5.3 | Input validation on trusted layer | satisfied | `ValidateJSONMiddleware` runs server-side on every input route (`backend/middleware/validation.go:332-368`); client-side validation is UX only |
| 1.5.4 | Output encoding near the interpreter | satisfied | React auto-escapes all rendering (no `dangerouslySetInnerHTML` in `frontend/src`); CSV formula injection neutralized at the export boundary (`backend/controllers/export_controller.go:41-57`), proven end-to-end from a hostile import through the real upload/confirm/export handlers by `controllers/import_export_csv_injection_test.go` (issue #432) |
| 1.6.1 | Cryptographic key management policy | satisfied | Key lifecycle documented end to end in `docs/security/incident-response.md` (issue #509): per-secret rotation procedures for `JWT_SECRET_KEY`, the at-rest master key, API tokens, account passwords, TOTP, and the OIDC client secret, each with its blast radius and verification, exercised against a real build. Underlying mechanisms: JWT secret validated at boot — ≥ 32 bytes, no known placeholder, minimum entropy (`backend/config/config.go:487-515`); TOTP/integration secrets AES-256-GCM at rest (`backend/services/credential_crypto.go`); at-rest master key + wrapped DEK rotation (`cmd/rotate-at-rest-key`, `atrest.RotateMasterKey`, **P4**); backup key implications — a snapshot carries the wrapped DEK but never the master key, so restore requires the same key and fails closed otherwise (**P5**, issue #420). |
| 1.6.2 | Key vault / API-based key access | not-applicable | Self-hosted single process; secrets come from environment (`backend/.env.example`); see P2 (crypto agility position) |
| 1.6.3 | Keys replaceable, re-encrypt path defined | satisfied | Per-user session invalidation via the per-request `token_version` check (`backend/middleware/auth.go:134-154`); at-rest master-key rotation rewraps the single wrapped DEK without touching payloads (`cmd/rotate-at-rest-key`, `atrest.RotateMasterKey`). Re-keying `JWT_SECRET_KEY` renders TOTP/integration-credential blobs undecryptable by design (`credential_crypto.go:20-21`); the replacement path — users re-enter integration credentials, admins run per-user 2FA reset, and the at-rest key must be decoupled first or the server won't boot — is written down and verified in `docs/security/incident-response.md` (issue #509). Operator-guided re-entry, not automated re-encryption; that is the deliberate position (P2). |
| 1.6.4 | Client-side secrets treated as insecure | satisfied | Session token is an httpOnly cookie, never JS-readable (`frontend/src/auth.ts:127-134`); no secrets in `localStorage` |
| 1.7.1 | Common logging format | satisfied | zerolog JSON everywhere (`backend/logger/logger.go:26-68`); operational producers share a standard field vocabulary — `event`/`component`/`operation`/`duration_ms`/`result`/`error`/`correlation_id` (`backend/logger/fields.go`, issue #425) |
| 1.7.2 | Logs shipped to remote system | not-applicable | Self-hosted; logs go to stdout → operator's docker log driver. An opt-in Prometheus scrape surface exists for operational signals — `GET /metrics` (issue #389), registered only when `METRICS_TOKEN` is set and gated by a constant-time `Authorization: Bearer` check (`backend/controllers/metrics_controller.go`, `backend/routes/routes.go`); it exposes bounded-cardinality counters/gauges (HTTP rate/latency/status, job outcomes, `system_events` outcomes, DB pool, storage) and no log lines or per-user data. Still operator-pull, not app-push. Its authenticated JSON counterpart is `GET /api/v1/admin/system-status` (issue #388, `backend/controllers/system_status_controller.go`, `backend/routes/routes.go:639`) — admin-gated (`AuthMiddleware` + `AdminMiddleware`), read-only, aggregating build/version, migration numbers, a live `cfg.Validate()` read-back, the enabled feature-flag booleans, SQLite operational facts and storage sizing; it carries no secret value (the feature block is booleans only, each validation entry is a field name plus a remediation message). The data-integrity checker `GET /api/v1/admin/integrity-check` (DB-01, issue #460, `backend/controllers/integrity_controller.go`) is the same shape — admin-gated, read-only, secret-free (findings carry a table/column name and a row count, never a value, id, or path; `controllers/integrity_controller_test.go` pins the no-leak property). Tests `controllers/metrics_controller_test.go`, `routes/metrics_route_test.go`, `controllers/system_status_controller_test.go`, `metrics/`. |
| 1.8.1 | Sensitive data classified | satisfied | `sensitivity` field on contacts/edges + explicit class (PII in contacts; credentials in encrypted columns) |
| 1.8.2 | Protection levels have requirements | satisfied | >normal: filtered in projection query (`backend/models/contact_record.go:116-136`), graph traversal (`backend/services/graph_traversal.go:113-115`), suggestions (`backend/services/graph_suggestion_service.go:85`), briefings (`backend/controllers/briefing_controller.go:236-244`). DATA-04 (issue #444) pins the export half of this as a matrix over the TEST-02 fixture (`controllers/selective_export_matrix_test.go`) and proves the filter lives in the projection, not the callers, via CardDAV (`carddav/selective_export_sensitivity_test.go` — a surface with no opt-in plumbing at all) and the loss-report boundary (`controllers/selective_export_loss_boundary_test.go`). |
| 1.9.1 | Communications encrypted | satisfied | TLS at external reverse proxy (`docs/deployment.md:32`); HSTS emitted when HTTPS is configured (`backend/middleware/security_headers.go:43-45`, `backend/main.go:461`); SMTP STARTTLS/implicit TLS (`backend/services/mailer.go`, `SMTP_USE_TLS`) |
| 1.9.2 | Peer authenticity verified | not-applicable | Single process (loopback between nginx and app); outbound calls use Go's standard TLS certificate verification (`httputil/fetch.go:94-119`, `mailer.go:230`) |
| 1.10.1 | Source control + traceability | satisfied | Git + issues-driven commits; one branch per concern (`CLAUDE.md` Workflow) |
| 1.11.1 | Components documented by function | satisfied | `docs/adrs/`, `docs/data-model.md`, `docs/development.md` |
| 1.11.2 | High-value flows don't share unsynchronized state | satisfied | Stateless JWT + per-request `token_version` DB check (`backend/middleware/auth.go:141-154`); no shared mutable session store; the only shared state (in-memory rate limiter) is per-key and restart-visible by design (`rate_limiter.go:32-36`) |
| 1.12.2 | Uploaded files served safely + CSP | satisfied | Attachments download-only with `Content-Disposition: attachment` + `nosniff` (`backend/controllers/attachment_controller.go:212-238`); photos re-encoded to JPEG, SVG rejected (`backend/controllers/photo_controller.go:330-338,361-369`); CSP `frame-ancestors 'none'` (`backend/middleware/security_headers.go:23`, `docker/nginx.conf:21`) |
| 1.14.1 | Segregation of trust levels | satisfied | nginx reverse proxy in front of the app (`docker/nginx.conf`); non-root container; reference topology (proxy → nginx → loopback backend → volumes → backups) diagrammed in `docs/security/deployment-baseline.md` (issue #417) |
| 1.14.2 | Binary signatures / verified endpoints | satisfied | cosign-signed images + SLSA provenance (`docker-publish.yml:551-557`, `docker-publish.yml:577-585`); released images additionally carry cosign-signed portable Syft SBOMs, SPDX + CycloneDX (`docker-publish.yml:594-601`, `syft-sbom.yml`); the release APK is cosign co-signed keyless (additive to the SIGNING_* keystore) alongside `attest-build-provenance` (`docker-publish.yml:255-258,291-293`); operator-facing verification steps (issue #418) — `docs/security/release-verification.md` |
| 1.14.3 | Build pipeline warns on outdated components | satisfied | govulncheck (`unit-tests.yml:240-242`), Trivy (`docker-publish.yml:692-699` + PR-time misconfig/secret gates `container-hardening.yml:144-179`), Grype second-opinion CVE DB (`grype.yml`, gates critical/high on main+nightly), TruffleHog git-history verified-secret scan (`trufflehog.yml`), CodeQL (`codeql.yml`), zizmor (`zizmor.yml`), Dependabot + Dependency Review (`dependency-review.yml`) |
| 1.14.4 | Automated build + verify | satisfied | CI builds, unit + Playwright e2e + Android instrumented e2e (`.github/workflows/unit-tests.yml`, `e2e-tests.yml`, `android-tests.yml`) |
| 1.14.5 | Sandboxing/containerization | satisfied | Single non-root container (`backend/Dockerfile`), `docker/nginx.conf` limits exposure; CIS hardening baseline gated in CI (`docker/cis-hardening.sh`, `.github/workflows/container-hardening.yml`); operator hardening baseline (capabilities, resource limits, no Docker-socket mount) and explicit non-goals (host OS, Docker daemon, reverse proxy, DNS, firewall, host admins) in `docs/security/deployment-baseline.md` (issue #417) |
| 1.14.6 | No deprecated client tech | satisfied | React 18 + TypeScript only — there is no plugin surface to deprecate: the SPA entry point loads its own bundle and nothing else (`frontend/index.html`), fonts are vendored locally (`frontend/public/fonts`), and no `<object>`/`<embed>`/`<applet>` element exists anywhere under `frontend/src`. Dependency surface is the lockfile (`frontend/yarn.lock`) |

## V2 — Authentication

L3-only, out of scope: 2.2.4, 2.2.5, 2.2.6, 2.2.7.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 2.1.1 | Passwords ≥ 12 chars | partial | We enforce `min=8` + ≥ 50 bits entropy (`backend/middleware/password_strength.go:21-45`); deviation documented in **P1** |
| 2.1.2 | ≥ 64 chars permitted, > 128 denied | partial | 64–72 bytes permitted; > 72 bytes rejected by the bcrypt cap (`backend/services/user_service.go:14,21-23`) — denied earlier than 128, deliberately |
| 2.1.3 | No password truncation | satisfied | Explicit 72-byte rejection instead of silent truncation (`backend/services/user_service.go:21-23`); test `services/user_service_test.go:13-32` |
| 2.1.4 | Any printable Unicode permitted | satisfied | No charset restrictions; policy is entropy-based (`password_strength.go:33-45`) |
| 2.1.5 | Users can change password | satisfied | `backend/controllers/user_controller.go:626-737` |
| 2.1.6 | Change requires current + new | satisfied | Current verified at `user_controller.go:674`, new≠old at `:679` |
| 2.1.7 | Breached-password check | partial | Local common-password blocklist incl. symbol-stripped variants (`backend/middleware/password_strength.go:168-198`), always on. Plus an opt-in HIBP k-anonymity check (`HIBP_CHECK_ENABLED`, `backend/services/hibp_service.go`) at registration/change/reset — see P3 for why it's opt-in, not default-on. Gap: blocklist is ~30 entries, not top-10k; HIBP coverage only applies when an operator turns it on. |
| 2.1.8 | Password strength meter | satisfied | `/check-password-strength` endpoint (`backend/controllers/user_controller.go:278-290`, route `routes/routes.go:58`); meter on `frontend/src/RegisterPage.tsx` |
| 2.1.9 | No composition rules | satisfied | Entropy-based; no upper/lower/digit/symbol requirements (`password_strength.go:33-45`) |
| 2.1.10 | No rotation/history requirements | satisfied | Neither is implemented, which per NIST 800-63B §5.1.1.2 *is* the requirement: the `users` table carries no expiry/last-rotated/history column (`backend/models/user.go:9-32`, migrations under `backend/database/migrations/`), and the change-password path re-hashes in place with no history write (`backend/controllers/user_controller.go:759`) |
| 2.1.11 | Paste + password managers allowed | satisfied | Standard `<input type="password">`; no paste blocking |
| 2.1.12 | View-password toggle | partial | Not implemented — password fields are plain masked inputs (`frontend/src/LoginPage.tsx:205`, `RegisterPage.tsx:134`); browsers' built-in reveal handles it. Gap: no in-app toggle. |
| 2.2.1 | Anti-automation (≤ 100 failed/h/account) | satisfied | Per-account lockout: 5 failures → 1 min, exponential to 30 min cap (`backend/middleware/rate_limiter.go:12-22,68-110`) — caps well under 100/h; per-IP auth limiter 2 req/s burst 50 (`:236`); tests `rate_limiter_test.go:292-531`, `two_factor_controller_test.go:270-305` |
| 2.2.2 | Weak authenticators only as secondary | satisfied | No SMS/email OTP at all; the only second factor is TOTP (`backend/services/twofactor.go`) |
| 2.2.3 | Notify on credential changes | partial | Audit trail records all changes (`backend/models/audit.go` hooks). Issue #411 added a proactive "your password was changed" email on the recovery path (`ConfirmPasswordReset`, `backend/controllers/user_controller.go:499`, `services.SendPasswordChangedEmail`, `backend/services/password_reset_service.go`) -- the path where the account owner is least likely to already know it happened. Gap: self-service `ChangePassword` and 2FA toggle still send no notification. |
| 2.3.1 | System-generated initial secrets random + expiring | satisfied | Password-reset tokens: 32 bytes `crypto/rand`, 1 h TTL, hash-only storage (`backend/services/password_reset_service.go:15-41`) |
| 2.3.2 | User-provided authenticator devices | partial | TOTP authenticator apps fully supported (setup/confirm/disable/recovery, `backend/services/twofactor.go`, `two_factor_controller.go`); FIDO/U2F hardware keys not supported |
| 2.3.3 | Renewal instructions for time-bound authenticators | not-applicable | TOTP secrets do not expire; no time-bound authenticators |
| 2.4.1 | Passwords hashed, salted, KDF | satisfied | bcrypt (KDF) with per-hash 128-bit salt, cost 10 (`backend/services/user_service.go:25`); tests `services/user_service_test.go` |
| 2.4.2 | Unique salt ≥ 32 bits | satisfied | bcrypt generates and embeds a fresh 128-bit random salt per hash; the single hashing call site takes the library default and supplies no salt of its own (`backend/services/user_service.go:16-27`, `golang.org/x/crypto/bcrypt`). See **P1** |
| 2.4.3 | PBKDF2 iterations | not-applicable | bcrypt used, not PBKDF2 |
| 2.4.4 | bcrypt work factor ≥ 10 | satisfied | `bcrypt.DefaultCost` = 10 (`services/user_service.go:25`); see P1 |
| 2.4.5 | Secret pepper / additional KDF round | not-applicable | Requires a separately-stored secret device (HSM); see P1/P2 for the single-process position |
| 2.5.1 | Recovery secret never sent in clear | satisfied | Reset sends a one-time random link token, never a password; password is only ever user-chosen (`password_reset_service.go`, `user_controller.go:362-437`) |
| 2.5.2 | No hints / KBA | satisfied | No password-hint or knowledge-based-answer field exists on the user model (`backend/models/user.go:9-32`) or in any registration/recovery DTO (`backend/models/dtos.go`); recovery is token-only (`backend/services/password_reset_service.go`) |
| 2.5.3 | Recovery never reveals current password | satisfied | Reset re-hashes a new password; the old hash is never recoverable/returned (`user_controller.go:454`) |
| 2.5.4 | No shared/default accounts | satisfied | No seeded accounts; first registered user becomes admin (`user_controller.go:55-63`) |
| 2.5.5 | Notify on factor change | partial | Audited (`backend/models/audit.go`) but not proactively notified; same gap as 2.2.3 -- 2FA enable/disable is unaffected by #411 |
| 2.5.6 | Secure recovery mechanism | satisfied | crypto/rand token, hash-only storage, 1 h TTL, endpoints rate-limited (`routes/routes.go:46-49`); `/password-reset/request` returns an identical status+body for known and unknown emails, and the confirm path never reveals *why* a token was rejected (invalid, already used, and expired all collapse to the same generic error) -- both pinned by `controllers/user_controller_test.go` (`TestRequestPasswordReset_UnknownEmail_SameResponseAsKnown`, `TestConfirmPasswordReset_RejectsSecondUse`, `TestConfirmPasswordReset_RejectsExpiredToken`, issue #411). The reset flow also never builds a link from request `Host`/`X-Forwarded-Host` -- the email carries only the raw token (`services/templates/password_reset.html`), and the frontend dialog has the user paste it in (`frontend/src/components/ForgotPasswordDialog.tsx`) -- so host-header poisoning of the reset flow doesn't apply by construction, not by a guard that could regress. Tests `services/password_reset_service_test.go`. Issue #592 added the analogous operator-assisted mechanism for the 2FA factor itself (not just the password): `POST /admin/users/:id/reset-2fa` (`admin_user_controller.go`), admin-only, idempotent, audited under a dedicated `two_factor_admin_reset` operation distinct from the self-service `totp_disable` (see 7.1.3). Tests `controllers/admin_user_controller_test.go` (`TestResetUserTwoFactor_*`). |
| 2.5.7 | Identity proofing on factor loss | not-applicable | Enrollment has no identity proofing (self-hosted); recovery codes fill the loss path. Issue #592 closed the gap behind that: until it landed, a user who lost their TOTP device *and* their recovery codes (or never saved them) had no way back into their account, with or without email configured — `DisableTwoFactor` is self-service-only, gated on a live code (`two_factor_controller.go:196-241`), and email delivery is optional in this self-hosted app (`cfg.EmailEnabled()`, `services/password_reset_service.go:47-51`). `POST /admin/users/:id/reset-2fa` (`admin_user_controller.go`) is the operator-assisted fallback, modeled on the existing admin password reset: disables TOTP, hard-deletes recovery codes, bumps `TokenVersion`. Still not-applicable as *identity proofing* proper — the trust boundary is "you already hold an authenticated admin session," not a KYC-style check — consistent with this row's existing self-hosted framing. |
| 2.6.1 | Lookup secrets single use | satisfied | Recovery codes deleted in the same `WHERE` that consumes them (`backend/services/twofactor.go:171-175`); test `two_factor_controller_test.go:250`; regeneration invalidates the superseded set (`routes/session_lifecycle_test.go`) |
| 2.6.2 | Lookup-secret entropy ≥ 112 bits or salted+hashed | partial | Recovery codes are 15 chars from a 32-symbol alphabet = 75 bits, stored SHA-256 hashed **without** a per-code salt (`twofactor.go:38,88-148`). Gap vs the letter; see 2.6.3 for why it's still safe. |
| 2.6.3 | Resistant to offline attack | satisfied | 75-bit random codes (≈3.8×10²²) + SHA-256; API tokens are 256-bit (`controllers/api_token_controller.go:65-70`) |
| 2.7.1–2.7.6 | Out-of-band verifier | not-applicable | No SMS/email/push OOB authenticator exists; the 2FA step is in-band TOTP (see V2.8) |
| 2.8.1 | TOTP has defined lifetime | satisfied | RFC 6238, 30 s period, skew 1 (`backend/services/twofactor.go:72-83`) |
| 2.8.2 | TOTP keys highly protected | partial | Secret encrypted at rest AES-256-GCM, HKDF-derived key (`backend/services/credential_crypto.go:22-54`) — no HSM/OS keyring; see P2 |
| 2.8.3 | Approved algorithms for OTP | satisfied | RFC 6238 TOTP (HMAC-SHA1), `pquerna/otp` (`twofactor.go:55-83`) |
| 2.8.4 | TOTP single use within validity | partial | No one-time-use rejection inside the 30 s window (standard TOTP behavior); brute force is rate-limited (`two_factor_controller.go:346-356,377-387`) |
| 2.8.5 | Reused TOTP logged + notified | partial | Reuse is not tracked/notified; failed-code attempts are rate-limited and logged as 429s. Gap: reuse detection. |
| 2.8.6 | TOTP revocable, immediate effect | satisfied | Disable 2FA requires a live code, then clears secret + bumps `token_version` (kills all sessions) (`two_factor_controller.go:196-256,231`); E2E `routes/session_lifecycle_test.go` |
| 2.8.7 | Biometrics only as secondary factor | not-applicable | No biometric authenticators |
| 2.9.1–2.9.3 | Cryptographic verifiers (smart cards/FIDO) | not-applicable | No FIDO/smart-card authenticators; see 2.3.2 |
| 2.10.1–2.10.3 | Service authentication | not-applicable | No intra-service accounts; single process |
| 2.10.4 | Integration secrets not in source, protected | satisfied | WebDAV/Immich/Seafile credentials are user-supplied and stored AES-256-GCM encrypted (`backend/services/credential_crypto.go`); nothing in source or git. The Monica import assistant's API token (#549) is a *session-only* third-party credential: held on the in-memory `*monica.Client` in `MonicaImportManager` for the import wizard's lifetime and never written to config, DB, or disk (`backend/services/monica_import_session.go`); it is also never logged — `monica_import_session_test.go` `TestMonicaImportSession_FullFlow_AddAndMerge` asserts the token string never appears in the structured log. Lifecycle in `docs/security/data-retention-lifecycle.md` §12a |

## V3 — Session Management

L3-only, out of scope: 3.6.1, 3.6.2.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 3.1.1 | No session tokens in URLs | satisfied | Tokens live in httpOnly cookie / `Authorization` header only (`backend/middleware/auth.go:23-43`); OIDC state/nonce in cookies (`oidc_controller.go:80-83`) |
| 3.2.1 | New token on authentication | satisfied | Fresh HS256 JWT minted at login and at 2FA completion (`backend/services/user_service.go:32-61`, `two_factor_controller.go:393-410`) |
| 3.2.2 | ≥ 64 bits entropy | satisfied | Token is a signed assertion; the signing secret is ≥ 32 bytes (256 bits) enforced at boot, and placeholder/low-entropy secrets are rejected (`config/config.go:487-515`) |
| 3.2.3 | Tokens in secure browser storage | satisfied | httpOnly cookie (`user_controller.go:234-243`); never `localStorage`/`sessionStorage` (`frontend/src/auth.ts:127-134`) |
| 3.2.4 | Approved crypto for tokens | satisfied | HS256 (HMAC-SHA-256) issued (`services/user_service.go:54`); the verifier pins the *HMAC family* — `token.Method.(*jwt.SigningMethodHMAC)` — before the key callback returns the secret (`middleware/auth.go:66-71`), which is what defeats both `alg: none` and the RSA/HMAC confusion attack. Stated precisely because the pin is family-wide, not HS256-exact: HS384/HS512 would also verify, and all three take the same symmetric secret, so there is no downgrade to reach (verified 2026-08-26, issue #378) |
| 3.3.1 | Logout/expiry invalidate session | satisfied | Logout clears cookies (`user_controller.go:262-266`); JWT expiry 96 h default (`config/config.go:183-188`); `token_version` revocation (`middleware/auth.go:141-154`); test `middleware/auth_lifecycle_test.go:59-84`, E2E `routes/session_lifecycle_test.go` |
| 3.3.2 | Re-authentication after idle | partial | Absolute 96 h expiry but **no idle timeout** (`config/config.go:183-188`). Gap vs L2's 12 h/30 min idle guidance; documented as the chosen trade-off for a personal CRM |
| 3.3.3 | Terminate other sessions after password change | satisfied | `TokenVersion++` on change/reset kills all other sessions; caller's own cookie is re-issued (`user_controller.go:701,422-428,709-734`); E2E: change/reset/admin-reset each kill a pre-change JWT (`routes/session_lifecycle_test.go`). Issue #411: the recovery-path reset also revokes every standing API token, since those carry no `TokenVersion` of their own and a reset is presumed compromise; test `TestConfirmPasswordReset_RevokesExistingAPITokens`. Issue #413 extracted that bulk-revoke into `services.RevokeAllAPITokens` (`services/api_token_service.go:20-24`) and wired it into `UpdateUser`'s admin password reset too (`admin_user_controller.go:436-446`) — previously only `TokenVersion` was bumped there, leaving a leaked API token live after an admin's account-takeover response; test `TestSessionLifecycle_AdminPasswordResetRevokesAPITokens`. Self-service `ChangePassword` deliberately leaves API tokens alone (lower-risk, current-password-gated action; see that function's own comment). |
| 3.3.4 | View and log out all sessions | partial | Stateless JWTs — no per-device session list; "log out everything" is achievable via password change (3.3.3) or admin reset (`admin_user_controller.go:394-409`). Gap: no session inventory UI. |
| 3.4.1 | Cookie `Secure` attribute | satisfied | `Secure = cfg.CookieSecure` on every session cookie, and boot refuses `FRONTEND_URL=https` + `COOKIE_SECURE=false` (`user_controller.go:234-243`, `config/config.go:677-683`). The whole cookie surface — every flow that mints or clears a cookie, driven against a real migrated schema, `HttpOnly`/`Secure`/`SameSite` per name — is pinned by `controllers/cookie_flags_test.go` (issue #610), which runs once with `CookieSecure=true` and once with `false`; the OIDC handshake cookies' one-time hardcode (`Secure=true` on the `oidc_client` set and the four clears, `oidc_controller.go:99,132-135`) was fixed in #605 to follow `cfg.CookieSecure` like their siblings (`oidc_controller.go:88-90,104,137-140`), since a `Secure` cookie is rejected outright by a browser on the supported plain-HTTP deployment |
| 3.4.2 | Cookie `HttpOnly` | satisfied | `user_controller.go:234-243`; every observed cookie asserted `HttpOnly` by `controllers/cookie_flags_test.go` (issue #610) |
| 3.4.3 | Cookie `SameSite` | satisfied | `SameSite=Strict` on every session cookie (`auth_token`/`2fa_pending`/`id_token` — `user_controller.go:234-243`, `two_factor_controller.go:415-425`, `oidc_controller.go:249`), tightened from `Lax` (issue #392). `SameSite=Lax` is retained, deliberately, only on the transient OIDC `oidc_state`/`oidc_nonce`/`oidc_pkce` cookies (`oidc_controller.go:80-98`) — they're read at `/auth/oidc/callback` across a cross-site top-level redirect *from the IdP*, where a `Strict` cookie would never arrive. Pinned by `controllers/csrf_posture_test.go` and `controllers/cookie_flags_test.go` (issue #610, which fails on a cookie whose observed `SameSite` differs from its declared policy). |
| 3.4.4 | `__Host-` prefix | partial | No `__Host-` prefix; the cookie is host-only with `Path=/` and no Domain, and `COOKIE_SECURE` may be off in plain-HTTP LAN setups (`.env.example:131-145`). Gap: prefix impossible while dev/HTTP setups exist. |
| 3.4.5 | Precise cookie path | satisfied | Single app at `/`; no sibling apps on the domain (`Path=/`) |
| 3.5.1 | Revocable OAuth tokens | not-applicable | No OAuth token issuance (OIDC is RP-only); scoped API tokens are revocable individually (`api_token_controller.go:125-155`), in bulk via self-service revoke-all (`RevokeAllApiTokens`, `api_token_controller.go:162-195`) or an admin password reset (issue #413), E2E `routes/session_lifecycle_test.go` |
| 3.5.2 | Session tokens over static API keys | partial | Sessions are JWTs; long-lived API tokens exist **by design** for CardDAV/scripting — scoped (`full`/`carddav`), hashed at rest, default 90-day expiry, individually revocable and rotatable (revoke + reissue same name/scope, `RotateApiToken`, `api_token_controller.go:202-271`, issue #413), or all at once via self-service revoke-all (`api_token_controller.go:162-195`) |
| 3.5.3 | Stateless tokens signed + tamper-proof | satisfied | HS256 with explicit algorithm pinning (`middleware/auth.go:66-98`); `purpose:"2fa"` challenge tokens structurally can't be sessions (`:109-118`); test `auth_lifecycle_test.go:88` |
| 3.7.1 | Full login required for sensitive ops | satisfied | 2FA challenges can't authenticate (`auth.go:109-118`); password change requires the current password (`user_controller.go:674`); 2FA disable requires a live code (`two_factor_controller.go:196-256`) |

## V4 — Access Control

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 4.1.1 | Enforce access control on trusted layer | satisfied | Server-side only; canonical `backend/controllers/circle_controller.go:49` (`Where("id = ? AND user_id = ?")`) |
| 4.1.2 | Policy data not user-manipulable | satisfied | `IsAdmin` excluded from registration DTO and JWT (`models/dtos.go:294-300`, `services/user_service.go:43`); relationship status/provenance server-derived (`relationship_edge_controller.go:155-160`) |
| 4.1.3 | Least privilege | satisfied | `user_id`/`VCardUID` scoping on every handler; audit of unscoped queries (2026-08-23) found only deliberate cross-user surfaces: contact sharing (`contact_share_controller.go:50`), thin user directory (`admin_user_controller.go:162-181`), auth lookups, internal jobs |
| 4.1.5 | Fail securely | satisfied | Ownership misses are AND-scoped 404s (never 403 revealing existence) (`contact_share_controller.go:212-233`); unknown errors → generic 500 (`errors/errors.go:342-353`) |
| 4.2.1 | IDOR protection (create/read/update/delete) | satisfied | Every controller query AND-scoped; pinned by per-entity cross-user tests (e.g. `circle_controller_test.go:182`, `household_controller_test.go:254,277`, `relationship_edge_controller_test.go:84,107`, `two_factor_controller_test.go:413`, `sync_conflict_controller_test.go:109` — the contact-sync-conflict list/restore/dismiss endpoints, issue #395); exhaustive every-route × six-persona matrix (`backend/routes/authorization_matrix_test.go`, issue #371). `ContactShare` is this codebase's one *deliberate* cross-user surface, so it gets its own matrix (issue #555, `controllers/contact_share_matrix_test.go`): the sender/recipient asymmetry (a sender gets the same 404 a non-party gets when trying to accept/decline/confirm their own outgoing share, `TestAcceptDeclineConfirmContactShare_SenderCannotActOnOwnOutgoingShare`), the frozen-snapshot guarantee (`TestCreateContactShare_PayloadFrozenAtCreation`, `TestContactShare_PayloadUnchangedAcrossLifecycleTransitions`), and the recipient-capability rule once a share is accepted (`TestConfirmContactShare_AcceptedContactBecomesOrdinaryAndCanBeReShared`: the landed contact is the recipient's own, governed by their own sensitivity classifications, with no residual restriction from the original share). |
| 4.2.2 | Anti-CSRF | satisfied | No CSRF tokens — documented mitigation: `SameSite=Strict` session cookies (see 3.4.3) + strict CORS origin allowlist (`backend/main.go:430-446`) with wildcard refused in release (`config/config.go:649-660`); API clients use `Authorization` headers, not cookies. Audited (issue #392): zero state-changing `GET` routes exist (`routes/routes.go` — every mutating handler is `POST`/`PUT`/`PATCH`/`DELETE`), so there is no cross-site-reachable-by-navigation surface left even under the retained `Lax` OIDC state cookies. Pinned by `controllers/csrf_posture_test.go` (cookie flags + a cross-site-shaped request with no session cookie rejected). |
| 4.3.1 | Admin interfaces use MFA | partial | 2FA is available to all users but not **enforced** for admins (`models/user.go:16`). Gap: no `require 2FA for admins` policy. |
| 4.3.2 | Directory browsing disabled | satisfied | No static serving of user dirs; uploads served only through controllers; nginx `autoindex` off (default), only built SPA assets served (`docker/nginx.conf`) |
| 4.3.3 | Step-up/adaptive auth | not-applicable | Single risk tier (personal CRM); TOTP is the step-up where it matters (sensitive ops, V3.7.1) |

## V5 — Validation, Sanitization and Encoding

L3-only, out of scope: none in this chapter (5.4 is L2).

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 5.1.1 | HTTP parameter pollution | satisfied | Gin binds JSON from the request body only; query/cookie/header sources are never merged into input structs (`backend/middleware/validation.go:332-368`) |
| 5.1.2 | Mass assignment protection | satisfied | Explicit DTO allowlists; `IsAdmin` excluded (`models/dtos.go:294-300`); relationship provenance/status/confidence absent from input by design (`models/dtos.go:339-354`); field-by-field update copies (`circle_controller.go:164`, `life_event_controller.go:346-353`) |
| 5.1.3 | Positive (allow-list) validation | satisfied | Struct tags (`required`, `uuid4`, `oneof`, `httpurl`, …) + custom validators (`backend/middleware/validation.go:19-33`) |
| 5.1.4 | Strong typing + schema | satisfied | Typed DTOs with per-field validation incl. cross-field rule (gift value ⇒ currency, `gift_controller.go:19-24`); `phone`/`birthday`/`safeurl` custom validators (`validation.go:120-211`). Issue #416 pinning: `contacts(user_id, vcard_uid)` has a real partial unique index (`database/migrations/000001_initial_schema.up.sql:90-92`) so an imported vCard whose UID collides with an existing contact — same file or a different one — cannot silently create a second contact under that identity; the create fails the constraint and is surfaced as a per-row `ImportResult.Errors` entry while the rest of the batch continues, per `controllers/import_duplicate_uid_test.go`. Issue #512 pinning: the same unique index guards the CardDAV reconcile path too — a remote address object whose UID collides with an existing contact fails the sync transaction (rolled back whole, no partial write) rather than creating a duplicate, without disturbing the pre-existing contact; `services/contact_sync_hostile_input_test.go` (`TestReconcileContactSync_DuplicateUIDCollidesWithExistingContact_RejectedWithoutCorruption`). TEST-04 (issue #432): the within-file duplicate-UID shape is now a declared corpus fixture (`docs/adversarial-fixtures/sem-duplicate-uid.vcf`, tier preserve-at-parse) driven through `ParseVCF` by `services/adversarial_import_test.go` (`TestParseVCF_DuplicateUIDWithinFile_BothParse`), so the parse-then-constraint-boundary split is pinned by the corpus rather than only by the controller test. |
| 5.1.5 | URL redirects allow-listed | satisfied | No server-side open redirects; frontend navigates only hardcoded routes (`frontend/src/App.tsx:723,773-775` — `/search` folds into `/contacts` via `SearchRedirect`, and every `/login`/`/register`/`*` match redirects to the fixed `/`). OIDC login/callback redirects target only the fixed `/` (web) or the app's own `mycorrhizal://` deep link (Android) — never a client-supplied URL — and `LogoutUser`'s RP-Initiated Logout `post_logout_redirect_uri` comes from server config (`cfg.OIDC.PostLogoutRedirectURL`), not request input (`user_controller.go:301`); attacker-supplied redirect-shaped query params on the callback are pinned to have no effect (`controllers/oidc_attack_matrix_test.go`, issue #412) |
| 5.2.1 | HTML sanitizer for WYSIWYG | not-applicable | No HTML input anywhere; notes/fields are plain text; frontend renders text only (no `dangerouslySetInnerHTML` in `frontend/src`). Issue #416 pinning: a vCard/CSV import carrying `<script>` in a free-text field round-trips as inert literal text, proven end-to-end by `services/import_sanitize_test.go` (`TestBuildContactFromRow_HTMLScriptInFreeTextField_StoredLiterallyNotStripped`) rather than resting on "no sink exists" alone. Issue #512 pinning: the same is now proven on the CardDAV reconcile path (`services/contact_sync_hostile_input_test.go` `TestReconcileContactSync_HTMLScriptInFreeTextField_StoredLiterallyNotStripped`) and the CalDAV reconcile path (`services/calendar_sync_hostile_input_test.go` `TestCalendarSync_HostileVEVENT_OversizedAndHTMLClampedNotCrashed`) — a compromised/malicious CardDAV or CalDAV server is a second untrusted ingestion point for the identical payload class. |
| 5.2.2 | Unstructured data sanitized | satisfied | `SanitizeString` strips null/control chars (`validation.go:95-116`); length caps on every free-text field (`models/dtos.go`) — that struct-tag validation path is for direct REST create/update calls only. Issue #416 finding: the **import** paths (VCF/CSV/JSContact/Android records) never ran it — `services.ValidateImportedContact` checks only firstname/email/phone/birthday format, not length or content — so a hostile import could carry invalid UTF-8 or control characters straight into storage. Closed by `services.SanitizeImportedContact` (`services/import_service.go`), called from the shared `BuildImportRowPreview` choke point before validation: replaces invalid UTF-8 (`strings.ToValidUTF8`) and strips C0/C1 control characters (keeping tab/LF/CR), with a diagnostic per changed field. Deliberately does NOT truncate long values or strip HTML (see the function's doc comment: no legitimate-data cost to fixing bytes, but truncation/HTML-stripping would destroy real user content ADR-0002 says to preserve). Pinned by `services/import_sanitize_test.go`. Issue #512 findings (two, both closed in the same PR): (1) `reconcileContactSync` (`services/contact_sync_service.go`) — the CardDAV *client* sync path, a second untrusted ingestion point for the exact same hostile vCard bytes — never called `SanitizeImportedContact` at all; now does, on both the create and update branches. (2) A deeper bug affecting *both* that path and the original VCF import path this row already covered: `SanitizeImportedContact` only cleaned the flat `Firstname`/`Lastname` scalars, not `contact.Card.Name` — and `Contact.BeforeSave`'s `cardSetDirectly` branch (`models/contact.go`) re-derives `Firstname`/`Lastname`/`FN` from `contact.Card` on every save that follows an `ApplyRecordToContact` call (VCF/JSContact import confirm, CardDAV/CalDAV reconcile, REST create/update), silently reverting the sanitization the instant the contact was actually saved. Hand-verified real bug, not hypothetical: caught by `services/contact_sync_hostile_input_test.go`'s control-character tests initially failing against the sync-path-only fix, and independently reproduced against the pre-existing VCF path by `services/import_vcf_hostile_input_test.go` (`TestConfirmVCF_RealDB_ControlCharactersAndInvalidUTF8_StaySanitizedOnSave`). Fixed by also sanitizing `contact.Card.Name` inside `SanitizeImportedContact` (`sanitizeCardName`, `services/import_service.go`) so the flat fields and the neutral Card copy stay consistent through the re-derivation. TEST-04 (issue #432): the adversarial corpus drives the whole import side — `docs/adversarial-fixtures/` (`enc-invalid-utf8.vcf`, `enc-overlong-utf8.vcf`, `inj-control-chars.vcf`, `inj-null-byte.vcf`) with per-fixture declared tiers, asserted at adapter level by `internal/adversarial/adversarial_test.go` (`TestDeclaredTiersHold`) and through the full pipeline by `services/adversarial_import_test.go` (`TestParseVCF_ControlCharsAndInvalidUTF8_AdversarialFixtures`), so a parser regression re-introducing the raw bytes fails the corpus before any user data is at risk. |
| 5.2.3 | SMTP injection protection | satisfied | `To` is `required,email`-validated (`models/dtos.go:303-311`); `Subject` MIME-Q-encoded (`services/mailer.go:284`); body is fixed-template |
| 5.2.4 | No eval/dynamic execution | satisfied | No `eval` in Go or frontend; `eslint-plugin-security` in CI |
| 5.2.5 | Template injection | satisfied | Email templates are fixed strings from an embedded FS; no user input in template logic (`services/email_renderer.go`) |
| 5.2.6 | SSRF protection | satisfied | Public-IP-only dialer with DNS-rebinding pinning (`backend/httputil/safedial.go:27-47`, `ipguard.go:30-59`); pre-flight URL checks (`httputil/fetch.go:17-58`); per-service opt-in flags (webhooks/Immich/CardDAV/Seafile, `config/config.go:65-83,136-147`); tests `httputil/fetch_test.go`, `services/webhook_ssrf_test.go`, `services/webhook_ssrf_integration_test.go` (live webhook job path), `services/notification_service_test.go` (push path); semgrep gate that no new outbound client bypasses the dialer (`.semgrep/mycorrhizal-traps.yaml` rule `mycorrhizal-unguarded-outbound-dialer`, fixture `.semgrep/tests/unguarded_outbound_dialer.go`, run by `sast.yml`). The admin diagnostics sweep's integration probes (#423) honor each integration's block-private-URLs flag through the same dialer, and the endpoint is admin-gated so the probe fan-out cannot be driven unauthenticated (`backend/services/diagnostics.go:447-478` `probeIntegration`). The opt-in update-availability check (#650) routes its outbound GitHub call through the same dialer (`backend/services/update_check.go` `newUpdateCheckClient`), pinned by `services/update_check_test.go` `TestUpdateCheckClient_TransportIsSSRFGuarded`. The Monica import assistant's live-API client (#549) does the same — `monica.NewClient` builds its transport `DialContext` from `httputil.SafeDialContext` when `MONICA_BLOCK_PRIVATE_URLS` is on (`backend/monica/client.go` `NewClient`), so a user-entered instance URL cannot be used to reach an internal address; pinned by `monica/client_test.go` `TestBlockPrivateRefusesLoopback`. The OIDC provider client (INT-02, #465) is the case the semgrep gate structurally cannot see — `go-oidc` builds the HTTP client inside the library, so there is no `http.Transport` literal for the rule to match — and it was found by INT-01's manual classification (`docs/int-01-integration-classification-matrix.md`), not a mechanical check: `newOIDCHTTPClient` now wires `httputil.SafeDialContext` when `OIDC_BLOCK_PRIVATE_URLS` is on and threads that client through discovery/token/JWKS/UserInfo via `oidc.ClientContext` (`backend/services/oidc_service.go`), pinned by `services/oidc_service_test.go` `TestOIDCClient_BlocksPrivateAddressWhenEnabled` / `_AllowsLoopbackByDefault`. Default off because a LAN identity provider is a common self-hosted deployment |
| 5.2.7 | SVG scriptable content | satisfied | SVG rejected at photo upload/proxy (`photo_controller.go:361-369`) and by attachment markup-signature check (`attachment_controller.go:42-63`), sniffed from real bytes rather than trusting filename/declared Content-Type — E2E'd against mislabeled/polyglot uploads (issue #375, `controllers/attachment_real_db_test.go` `TestAttachmentPolyglotMislabeledContentSniffed`) |
| 5.2.8 | Markdown/CSS/template content | not-applicable | No markdown/CSS/XSL/BBCode rendering of user content |
| 5.3.1–5.3.3 | Output encoding / XSS | satisfied | React auto-escaping (no raw-HTML sinks); CSV cells neutralized (`export_controller.go:41-57`); JSON via `encoding/json`; CSV neutralization E2E'd through the real `/export` handler against real DB rows (issue #375, `controllers/hostile_input_e2e_test.go` `TestExportData_CSVFormulaInjectionNeutralized`), complementing the pure-function `TestCsvSafe*` coverage in `export_csv_injection_test.go`. TEST-04 (issue #432): the neutralization is proven to hold end-to-end from the import side — `controllers/import_export_csv_injection_test.go` (`TestImportToExport_CSVFormulaPrefixesNeutralized`) imports `docs/adversarial-fixtures/inj-csv-formula-note.vcf` through the real VCF upload/confirm handlers, asserts the raw `=`/`+`/`-`/`@` prefixes survive import into the DB untouched, then asserts the CSV export re-emits them `'`-neutralized; the import-side preservation alone is pinned at the service level by `services/adversarial_import_test.go` (`TestParseVCF_CSVFormulaPrefixesSurviveToExportableFields`). |
| 5.3.4 | Parameterized queries | satisfied | GORM everywhere; raw SQL is parameterized (search FTS `search_service.go:231-311`, graph CTE `graph_traversal.go:96-131`, export `export_controller.go:105-119`) |
| 5.3.5 | Contextual encoding where no parameterization | satisfied | No non-parameterized SQL paths; identifier interpolation is compile-time constants only (`export_controller.go:105-128`) |
| 5.3.6 | JSON injection / JSON eval | satisfied | Responses via `encoding/json`; frontend uses `fetch().json()` (JSON.parse), never eval |
| 5.3.7 | LDAP injection | not-applicable | No LDAP |
| 5.3.8 | OS command injection | satisfied | No `os/exec` anywhere (security review ban); parameterized everything |
| 5.3.9 | LFI/RFI | satisfied | Server-generated UUID filenames (`photo_controller.go:264-266`, `attachments/attachments.go:39-52`); traversal guards reject `..`/absolute (`photo_controller.go:95-99`, `attachments.go:27-35`) |
| 5.3.10 | XPath/XML injection | not-applicable | No XML parsing of untrusted input (CardDAV is text/vCard) |
| 5.4.1–5.4.3 | Memory safety / format strings / integer overflow | not-applicable | Go is memory-safe (runtime bounds checks); `unsafe` unused in app code; `go vet` in CI |
| 5.5.1 | Serialized objects integrity | satisfied | Wire format is JSON DTOs only — no gob/binary/object-graph deserialization of client input anywhere (V1.5.2). The one signed serialized object is the session JWT, whose algorithm and signature are verified before any claim is read (`backend/middleware/auth.go:66-98`) |
| 5.5.2 | XXE / restrictive XML parsing | satisfied | Issue #416 correction: XML parsing of externally-controlled input DOES exist — `services/webdav_client.go:283` `xml.Unmarshal`s a Nextcloud/ownCloud PROPFIND response (size-capped at 8MiB, `maxWebDAVBodyBytes`), and the CalDAV/CardDAV server accepts client PROPFIND/REPORT XML via the third-party `go-webdav` dependency (not first-party code). Go's `encoding/xml` has no external-entity/DTD-expansion support at all by default (no `Decoder.Entity` map is ever populated in this codebase), so classic XXE is not applicable regardless — proven against the real `webdav_client.go` code path, not asserted from documentation, by `services/webdav_client_xxe_test.go` (an internal-subset entity reference is rejected as a parse error, not expanded; an external DTD reference is never fetched) |
| 5.5.3 | No untrusted deserialization | satisfied | `encoding/json` into typed structs only; no gob/pickle/yaml of untrusted input |
| 5.5.4 | JSON.parse, not eval | satisfied | `fetch().json()` everywhere in `frontend/src/api/*` |

## V6 — Stored Cryptography

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 6.1.1 | Regulated private data encrypted at rest | satisfied | Secrets (TOTP, integration credentials) AES-256-GCM encrypted (`backend/services/credential_crypto.go:33-54`); user-authored PII field-level AES-256-GCM encrypted at rest — `backend/atrest` (issue #380): contact free-text/neutral card/crm/passthrough, `life_events.description`, `reminders.message`, `reminder_completions.message`, `gifts`, `preferences`, `conversation_agenda`, `audit_events.before_snapshot`, sync-conflict copies. Wrapped-DEK envelope: master key from `DATA_ENCRYPTION_KEY`/`_FILE` (HKDF-JWT fallback), DEK AES-GCM-wrapped into `data_encryption_keys`, ciphertext `encv1:`-prefixed (migration `000033`, backfill `atrest/backfill.go`). **Deliberate exception:** FTS5-indexed columns (`notes.content`, `activities.*`, flat contact search fields) stay plaintext so search works — documented in `docs/security/threat-model.md` (Gating decision 1). Backups inherit this same encryption: a `VACUUM INTO` snapshot carries the ciphertext + wrapped DEK, and the master key is never in the backup (issue #420, **P5**). |
| 6.1.2 | Regulated health data at rest | not-applicable | Neutral contact model has no medical fields; no regulated health data by design |
| 6.1.3 | Regulated financial data at rest | not-applicable | Gift records are non-sensitive user-authored notes (no account/credit/tax data) |
| 6.2.1 | Crypto fails securely, no padding oracle | satisfied | AEAD (GCM) — no padding to oracle; failures are generic 500s (`errors/errors.go:342-353`) |
| 6.2.2 | Approved algorithms/libraries | satisfied | bcrypt, AES-256-GCM, HMAC-SHA-256, SHA-256, HKDF-SHA256, RFC 6238 TOTP — all stdlib/`golang.org/x/crypto`. JWT signing/verification via `golang-jwt` (`backend/middleware/auth.go`, `backend/services/notification_service.go`); HMAC-SHA-256 webhook signatures (`backend/services/webhook_service.go`). The whole import surface is pinned by `cmd/citecheck`'s crypto-surface gate (issue #612) |
| 6.2.3 | IV/cipher/mode config | satisfied | GCM 12-byte nonces from `crypto/rand` (`credential_crypto.go:47-50`); no ECB; no custom modes |
| 6.2.4 | Algorithms swappable | partial | Direct calls, not behind an abstraction — deliberate pre-1.0 decision, documented in **P2** |
| 6.2.5 | No weak modes/hashes | satisfied | The whole cryptographic surface is AES-256-GCM (`backend/services/credential_crypto.go:33-58`, `backend/atrest/atrest.go`), bcrypt (`backend/services/user_service.go:16-27`, `backend/controllers/user_controller.go`, `backend/carddav/auth.go`) and SHA-256 for one-way token digests (`backend/services/password_reset_service.go:34`) and the audit hash chain (`backend/models/audit_chain.go`). No MD5/DES/RC4/ECB/Blowfish use exists; the tree's single `crypto/sha1` import is HIBP's own k-anonymity wire format, annotated at the import (`backend/services/hibp_service.go:6`, **P3**). Enforced continuously by gosec via golangci-lint (`unit-tests.yml:253-255`), CodeQL (`codeql.yml`) and the `cmd/citecheck` crypto-surface gate (issue #612) |
| 6.2.6 | Nonce never reused per key | satisfied | Fresh `crypto/rand` nonce per encryption (`credential_crypto.go:47-50`); test `services/credential_crypto_test.go` |
| 6.2.7 | Ciphertext authenticated | satisfied | Every ciphertext this app writes is AES-256-GCM, which authenticates as it encrypts — credentials/TOTP (`backend/services/credential_crypto.go:33-58`) and the at-rest field envelope (`backend/atrest/atrest.go`). No unauthenticated mode is reachable by a caller. L3 row, met anyway |
| 6.2.8 | Constant-time comparisons | out-of-scope | L3; note bcrypt/HMAC comparisons are constant-time by library design |
| 6.3.1 | CSPRNG for secrets | satisfied | `crypto/rand` for API tokens (`api_token_controller.go:65-70`), reset tokens (`password_reset_service.go:22-25`), recovery codes (`twofactor.go:103-122`), GCM nonces, webhook secrets (`backend/controllers/webhook_controller.go`), import sessions (`backend/services/import_session.go`) and OIDC state/PKCE (`backend/services/oidc_service.go`) — surface pinned by `cmd/citecheck`'s crypto gate (issue #612) |
| 6.3.2 | UUID v4 via CSPRNG | satisfied | `google/uuid` v4 in `BeforeCreate` (e.g. `models/circle.go:31`, `models/contact.go:451`) |
| 6.3.3 | Entropy under load | out-of-scope | L3 |
| 6.4.1 | Secrets-management solution | satisfied | Env-var/file secrets with boot-time validation — `JWT_SECRET_KEY` length/placeholder/entropy gates (`config/config.go:487-515`), `DATA_ENCRYPTION_KEY`/`_FILE` base64-32-byte validation (`config/config.go:527-551`); at-rest master key + wrapped-DEK envelope with a rotation tool (`cmd/rotate-at-rest-key`, `atrest.RotateMasterKey`) and a documented "lost key = lost data, by design" posture (`atrest/atrest.go`); no vault (self-hosted; see P2). |
| 6.4.2 | Key material isolated from app | not-applicable | Single process; the secret must live in the process env by design (self-hosted; see P2) |

## V7 — Error Handling and Logging

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 7.1.1 | No credentials/payment in logs | satisfied | Logger context has `request_id`, `user_id`, `method`, `path`, `ip` — no body, no Authorization header, no cookies (`backend/logger/logger.go:91-121`); passwords never logged. The app runs `gin.New()` + `gin.Recovery()` (not `gin.Default()`) so gin's own request `Logger()` — which would write a second, unredacted `path?query` line — is not attached (`backend/main.go:415-423`). GORM's own log path (issue #621) is closed too: every connection opened through `database.InitDB`/`OpenMigratedFile` uses `newGormLogger` (`backend/database/migrate.go`), which logs `?` placeholders instead of interpolated literal values (`ParameterizedQueries`) and suppresses the benign not-found SELECTs (`IgnoreRecordNotFoundError`) — pinned by `database/migrate_test.go::TestGormLoggerDoesNotInterpolatePII`. |
| 7.1.2 | No other sensitive data in logs | satisfied | The request-log `query` field is filtered by `RedactQueryValues` (`backend/logger/redact.go`), applied in `backend/middleware/logging.go:20`. Since issue #510 this is an **allow-list**: only pagination/sort/low-cardinality enum keys are logged verbatim; every other value — free-text `search`/`q` terms, record ids, OIDC `state`, and the credential-shaped keys — becomes `[REDACTED]`. Recipient email addresses in the mail/password-reset paths are masked to `a***@domain` via `logger.MaskEmail` (`backend/logger/mask.go`) rather than logged in full. Verified against real captured logs, not by reading the code — see `docs/security/pii-inventory.md` §3.3 and "How this was verified". Tests: `logger/redact_test.go`, `logger/mask_test.go`, `middleware/logging_test.go`, `services/mailer_test.go`. The GORM residual path from #510 is **fixed** (issue #621): every connection through `database/migrate.go` uses `newGormLogger`, which logs `?` placeholders rather than interpolated values and drops not-found SELECTs entirely (`ParameterizedQueries`/`IgnoreRecordNotFoundError`), pinned by `database/migrate_test.go::TestGormLoggerDoesNotInterpolatePII`. |
| 7.1.3 | Security events logged | satisfied | Request log records 401/403/429s; audit hooks record all data mutations (`backend/models/audit.go`); since #381 the audit trail also carries distinct auth/admin lifecycle event types — login success/failure, registration, password change/reset (plus #411's password-reset-*requested*, distinct from the completed reset), TOTP enable/disable + recovery-code regeneration, API-token create/revoke (individually, or one event per token for revoke-all/rotate, issue #413), admin user create/edit/delete/role change, admin-initiated 2FA reset (`two_factor_admin_reset`, distinct from the self-service `totp_disable`, issue #592) — recorded from the controllers themselves (`RecordAuditEvent`, `backend/models/audit.go:246`, wired in `user_controller.go`, `two_factor_controller.go`, `api_token_controller.go`, `admin_user_controller.go`, `oidc_controller.go`; coverage pinned by `controllers/auth_audit_events_test.go`). No deserialization events exist (no deserialization, V5.5). |
| 7.1.4 | Log event timeline detail | satisfied | `request_id`, `user_id`, IP, UA, method, path, status, duration (`backend/logger/logger.go:91-121`); a `correlation_id` threads one HTTP request or scheduled-job run through every background step and outbound call it spawns (`backend/logger/context.go`, `backend/middleware/request_id.go`, issue #425), and significant operational state transitions are additionally persisted with duration/result to the `system_events` timeline (`backend/models/system_event.go`, issue #424); every scheduled-job invocation's outcome — name, trigger, duration, result (success/failure/skipped), items processed — is persisted per-run to `job_runs` (`backend/models/job_run.go`, issue #391) |
| 7.2.1 | Authentication decisions logged | satisfied | Login/2FA/register attempts (success and failure) are request-logged with IP, method, path, status (`backend/middleware/logging.go`); lockout events surface as 429s (`rate_limiter.go`); 2FA challenge issuance/consumption is audit-visible via the 2FA controller flow; since #381 successful and failed logins (password step and 2FA step, plus OIDC) are additionally recorded as dedicated `auth` audit events (`RecordAuditEvent`, `controllers/auth_audit_events_test.go`) |
| 7.2.2 | Access-control failures logged | partial | 401/403/404s are logged, but 404-masked IDOR misses are indistinguishable from genuine 404s *by design* (V4.1.5). Gap: no separate access-denied event. |
| 7.3.1 | Log injection prevented | satisfied | User-controlled values are control-character-escaped and length-capped before logging (`backend/logger/sanitize.go`), applied to request path/query/UA/error (`backend/middleware/logging.go`), request-path context (`backend/logger/logger.go:112-118`), the persisted `system_events.error`/`detail` free-text fields (`backend/models/system_event.go`, `RecordSystemEvent` → `SanitizeLogField` + rune cap), the persisted `job_runs.error`/`detail` fields (`backend/models/job_run.go`, `RecordJobRun` → same treatment, issue #391), and user-content diagnostics in the message position (`carddav/backend.go:352,445`, `export_controller.go:657,726`, `contact_share_controller.go:87`, `photo_controller.go:353,368`) — the console writer prints the message verbatim, so raw newlines there were a real line-injection vector; tests: `logger/sanitize_test.go`, `middleware/logging_test.go`, `models/system_event_test.go`, `models/job_run_test.go` |
| 7.3.3 | Logs protected | satisfied | The audit trail is tamper-EVIDENT by construction (issue #381): every `AuditEvent` commits `SHA-256(prev_hash \|\| content)` and the 000016/000034 `BEFORE UPDATE` trigger hard-rejects any edit; `VerifyAuditChain` (`backend/models/audit_chain.go`) recomputes the chain and flags insertion/deletion/reorder/edits, exposed to the operator as `make audit-verify` (`cmd/audit-verify`). Append-only by construction (no model update/delete path), the only sanctioned writer besides the recorder is the retention purge, which re-links the chain (`services/audit_purge_service.go:42`). Plain request/error log stream remains stdout → the operator's docker log driver (single process, self-hosted). |
| 7.3.4 | Time synchronization, UTC | not-applicable | Operator/OS concern; timestamps are UTC (`zerolog Timestamp()`) |
| 7.4.1 | Generic error + referenceable ID | satisfied | Envelope `{error:{code,message,details}, request_id, timestamp}` (`backend/errors/middleware.go:13-24,67-86`); internal errors are generic text, with no panic value / stack / type names in the body (pinned by `errors/middleware_test.go`, `errors/fail_secure_realdb_test.go`). **One documented exception**, found by the #378 pass and the only site in the backend that bypasses the envelope: the self-service "test my notification channel" endpoint reports a diagnostic string rather than an envelope error (`notification_controller.go:167-172`), because the whole point of the endpoint is to tell the user why *their own* ntfy/Gotify/push target rejected the message. Bounded since #606: the echoed string is capped at 256 bytes (`services/notification_service.go` `NotificationTestErrorMessage`) and, with `WEBHOOK_BLOCK_PRIVATE_URLS` on, the SSRF guard's distinct sentinels (`ErrNotificationPrivateAddress`, `ErrWebhookUnreachable`, `ErrWebhookPrivateAddress`) collapse to one neutral "not reachable under this instance's outbound policy" message so the hardening flag cannot be used to probe which rule a target tripped; the full diagnostic still goes to the server log (pinned by `notification_controller_test.go` `TestNotificationConfig_TestNtfy_ErrorTruncated` / `_PrivateAddressCollapsedWhenGuarded` / `_UnresolvableCollapsedWhenGuarded`). Not an SSRF oracle on the default configuration — `WEBHOOK_BLOCK_PRIVATE_URLS` defaults off (5.2.6's opt-in-per-service position), so the caller already chose the target and already learns its reachability from the status code. The admin diagnostics sweep (#423) reports a *read-only* checklist and never echoes config values or integration base URLs — the failing variable's name or a generic "unreachable"/"blocked by private-address policy" is all that leaves the process, with the detail going to the log (pinned by `services/diagnostics_test.go` `TestRunDiagnosticsNoSecretLeak`, `TestRunDiagnosticsIntegrationReachability`) |
| 7.4.2 | Exception handling across codebase | satisfied | Typed `AppError` + `AbortWithError` (`errors/errors.go`, `errors/middleware.go:109-112`); `.Error` checked on every write (trap 4, `CLAUDE.md`) |
| 7.4.3 | Last-resort handler | satisfied | Panic-recovery middleware → generic 500, stack + panic value logged server-side only, never in the response body (`errors/middleware.go:27-44`); `runJob` / `safeGo` panic recovery for scheduled jobs and goroutines (`main.go:31-94`); forced DB failure → typed code, no raw driver error (pinned by `errors/fail_secure_realdb_test.go`) |

## V8 — Data Protection

L3-only, out of scope: 8.1.5, 8.1.6.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 8.1.1 | No sensitive data in server caches | partial | No in-process/HTTP cache of API responses exists, but API responses carry no `Cache-Control: no-store`, so an operator proxy could cache them. Gap: add no-store to API responses. |
| 8.1.2 | Cached copies purged | not-applicable | No server-side caching layer |
| 8.1.3 | Minimal request parameters | satisfied | Purpose-built per-endpoint DTOs (`backend/models/dtos.go`); auth material travels in the body or the `Authorization`/`Cookie` header, never a query string — enforced continuously rather than by convention, by the custom Semgrep rule `mycorrhizal-query-string-auth-material` (`backend/.semgrep/mycorrhizal-traps.yaml:148-177`, fixture `backend/.semgrep/tests/query_string_auth_material.go`, `sast.yml`) |
| 8.1.4 | Abnormal request detection/alert | satisfied | Per-IP API limiter (1/600 ms, burst 1000) + per-account lockout + configurable intervals (`backend/middleware/rate_limiter.go:249,255-258,275-280`) |
| 8.2.1 | Anti-caching headers for browsers | partial | SPA HTML gets `Cache-Control: no-cache` (`docker/nginx.conf:178`); API responses get no anti-cache headers. Gap: same as 8.1.1. |
| 8.2.2 | No sensitive data in browser storage | satisfied | Token in httpOnly cookie; `localStorage` holds only prefs + `user_info` (id/username/admin) (`frontend/src/auth.ts:15,120`); service worker caches app-shell and `.png` only — never API responses (`src/service-worker.ts`) |
| 8.2.3 | Client data cleared on logout | satisfied | Logout clears the cookie and `USER_INFO_KEY` (`frontend/src/auth.ts:172`); SPA state dies with the session |
| 8.3.1 | Sensitive data not in query strings | satisfied | Mutations carry their payload in the body; GET parameters are pagination/filter values only (`backend/controllers/helpers.go`). The absence of auth material in query strings is gated by the same Semgrep rule as 8.1.3 (`backend/.semgrep/mycorrhizal-traps.yaml:148-177`), whose one suppressed exception is the spec-mandated OAuth2 `code` callback |
| 8.3.2 | Export/remove data on demand | satisfied | Exports: CSV/vCard3/vCard4/jSContact (`routes/routes.go:405-411`), plus the audit-trail CSV export added by issue #416 (`GET /audit/export`, `controllers.ExportAuditLog`, `routes/routes.go`); deletion: per-entity delete + soft-delete undo (`contact_controller.go` `DeleteContact`, audit undo `audit_controller.go`); confirmed propagating to CardDAV/CalDAV (next listing excludes the soft-deleted row, no `.Unscoped()`) and the Android Room mirror (T17 `sync.incremental` tombstone ids) — full per-data-type lifecycle in `docs/security/data-retention-lifecycle.md` (issue #414). Issue #442 (DATA-02) makes export loss explicit rather than silent: the vCard/JSContact handlers carry a `X-Mycorrhizal-Export-Loss-Report` header and `GET /export/preflight` (same shared computation, `controllers/export_loss.go`) reports which contact lost which field to which format and why (the DATA-01 matrix classification) before any file is produced; the report⇄matrix correspondence is asserted by `controllers/export_loss_test.go` (`TestExportPreflight_CanonicalFixtureReportsCorrespondToMatrix`) and `correspondence/matrix_test.go` (`TestClassificationForCorrespondence`). **Every table in the schema has a declared deletion policy**, pinned by `controllers/delete_cascade_coverage_test.go` (issue #611): the schema is enumerated from the real migrated database, every table must fall into a declared bucket (`go-cascade-user`/`go-cascade-contact`/`fk-cascade-user`/`exempt`), `fk-cascade-user` tables are asserted to really carry an `ON DELETE CASCADE` FK to `users`, a contact-scoped table may not rely on a cascade from the soft-deleted `contacts` row (CLAUDE.md trap 6), and `DeleteUser`/`deleteContactAssociations` are behaviorally verified to empty every declared table. Note: account deletion is admin-only — no user self-service account wipe (accepted for single-user self-host). |
| 8.3.3 | Clear consent language | not-applicable | Self-hosted; no third-party data collection (privacy is the operator's, by design) |
| 8.3.4 | Sensitive-data policy in place | satisfied | `sensitivity` classification enforced in exports/sync/graph (V1.8.2); ADRs document the model (`docs/adrs/`) |
| 8.3.5 | Sensitive-data access audited | partial | Mutations of all major entities audited with redacted before-snapshots (`backend/models/audit.go:105-175`); **reads** are not audited. Gap: read-auditing would be a deliberate privacy/performance choice. |
| 8.3.6 | Memory zeroization | not-applicable | Go runtime; no explicit zeroization (accepted; see P2's scope note) |
| 8.3.7 | Encryption with confidentiality+integrity | satisfied | AES-256-GCM for at-rest secrets (`credential_crypto.go:33-54`) and for field-level PII (`atrest`, issue #380); TLS for transport |
| 8.3.8 | Retention classification, auto-delete | satisfied | T26 purge jobs (feed/audit/ContactShare/webhook-delivery retention, `backend/services/purge_service.go`, `audit_purge_service.go`, `contact_share_purge_service.go`, `webhook_delivery_purge_service.go`); 410 Gone past purge window (`controllers/helpers.go:421-431`); soft-delete is the retention buffer for user content. Every data type's retention/deletion statement (including attachments, FTS index, backups, exports, CardDAV/CalDAV, Android mirror) is now documented in `docs/security/data-retention-lifecycle.md` (issue #414); the `ContactShare`-has-no-purge-window gap surfaced there was closed by issue #574 (`CONTACT_SHARE_RETENTION_DAYS`, default 30, pinned by `services/contact_share_purge_service_test.go`); the `WebhookDelivery`-has-no-window gap from the #510 review was closed by issue #622 (`WEBHOOK_DELIVERY_RETENTION_DAYS`, default 30, plus a payload trim on successful deliveries — pinned by `services/webhook_delivery_purge_service_test.go` and the trim tests in `services/webhook_delivery_test.go`). Backup confidentiality/retention/restore-security (operator-owned boundary: inherited field-level encryption, master key never in the backup, operator-owned retention, restore-drill decryptability check) documented in `docs/deployment.md`'s "Backup confidentiality & retention" + **P5** (issue #420). Backup *immutability* is the same operator-owned boundary (issue #505): the app's backup-write surface is write-new-only with no delete/rotate/expire path anywhere (`backend/database/backup_immutability_test.go`), and immutability against a compromised host is achieved off-host (pull-based copy or object-locked storage), documented in `docs/deployment.md`'s "Backup immutability & ransomware resistance" + **P5**. |

## V9 — Communication

L3-only, out of scope: 9.2.5.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 9.1.1 | TLS for all client connectivity, no fallback | satisfied | TLS at the external reverse proxy (`docs/deployment.md:32`); HSTS emitted when HTTPS is configured (`backend/middleware/security_headers.go:43-45`); no plaintext listener exposed |
| 9.1.2 | Only strong ciphers | not-applicable | TLS config belongs to the operator's external proxy (Mozilla-level guidance in `docs/deployment.md`) |
| 9.1.3 | TLS 1.2/1.3 only | not-applicable | Same — external proxy's responsibility (documented) |
| 9.2.1 | Trusted TLS certs for outbound | satisfied | Go standard library verifies certificates on all outbound calls; SMTP pins `ServerName` on both transports — implicit TLS via `tls.DialWithDialer` (`backend/services/mailer.go:230`) and opportunistic `STARTTLS` via `client.StartTLS` (`backend/services/mailer.go:214`); both dials are timeout-bounded (`smtpDialTimeout`/`smtpDeadline`, INT-02 issue #465) |
| 9.2.2 | TLS for all outbound connections | satisfied | Outbound fetches are http(s)-only (`httputil/fetch.go:17-58`); SMTP STARTTLS/TLS (`mailer.go`, `SMTP_USE_TLS`); CardDAV/WebDAV clients use https |
| 9.2.3 | Outbound connections authenticated | satisfied | SMTP auth, CardDAV/WebDAV credentials, Immich/Seafile API keys (`services/` client packages) |
| 9.2.4 | OCSP stapling | not-applicable | External proxy's TLS configuration |

## V10 — Malicious Code

L3-only, out of scope: 10.1.1, 10.2.3, 10.2.4, 10.2.5, 10.2.6.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 10.2.1 | No phone-home / data collection | satisfied | No telemetry/analytics anywhere in app code; dependency scanners + CodeQL in CI (`.github/workflows/codeql.yml`); review culture per `CLAUDE.md` |
| 10.2.2 | No excessive permissions | satisfied | Android manifest minimal; mobsfscan manifest/secret scan in CI (`.github/workflows/sast.yml`) |
| 10.3.1 | Signed updates | satisfied | No auto-update feature; images are cosign-signed with SLSA provenance (`docker-publish.yml:551-557`, `docker-publish.yml:577-585`); verify commands for operators in `docs/security/release-verification.md` |
| 10.3.2 | Integrity of loaded code (SRI) | satisfied | There is no third-party script or style tag to protect with SRI: every asset is self-hosted and same-origin (`frontend/index.html`, fonts vendored at `frontend/public/fonts`), and the CSP admits no external origin (`backend/middleware/security_headers.go:23`, `docker/nginx.conf:21`). Dependency integrity comes from the lockfiles (`frontend/yarn.lock`, `backend/go.sum`) plus Dependency Review and Grype (`dependency-review.yml`, `grype.yml`) |
| 10.3.3 | Subdomain-takeover protection | not-applicable | Self-hosted; DNS is operator-managed (documented in `docs/deployment.md`) |

## V11 — Business Logic

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 11.1.1 | Sequential, unbypassable flows | satisfied | No multi-step business flows to bypass; the one two-step flow (2FA enrollment setup→confirm) is server-state-enforced (`two_factor_controller.go:60-191`) |
| 11.1.2 | Realistic human-time steps | not-applicable | No multi-step flows; anti-automation covers the rest (11.1.4) |
| 11.1.3 | Per-user limits on business actions | partial | Per-user quotas exist on the fan-out/operational surfaces (issue #415): 20 webhooks (`webhook_controller.go:74`), 5 concurrent in-memory import sessions (`services/import_session.go:39` `MaxImportSessionsPerUser`, enforced by `import_session.go:228` `CountActive` + the upload/accept handlers), 20 push subscriptions + 20 device registrations (`notification_controller.go:24-26`). No quotas on content creation (contacts/notes/activities). Gap: content quotas would be a product decision. |
| 11.1.4 | Anti-automation / anti-DoS | satisfied | Body-size limits 10 MB/1 MB (`middleware/body_limit.go:11-40`), `MaxMultipartMemory` (`main.go:437`), API rate limiter, account lockout, plus the issue #415 inventory below (search-term 256-rune cap, import row/size + session caps, bulk-500, audit-export 100k, webhook fan-out bounds, graph-depth-5) |
| 11.1.5 | Business-logic limits per threat model | satisfied | Duplicate-add 409s (`circle_controller.go:242`, `household_controller.go:245`, `tag_controller.go:242`), one cadence policy per contact (`cadence_controller.go:66-74`), gift value⇒currency rule (`gift_controller.go:19-24`), self-edge rejection (`relationship_edge_controller.go:104-106`), accept-only-suggested (`:365-368`) |
| 11.1.6 | No TOCTOU/race conditions | satisfied | `_txlock=immediate` + WAL so write transactions take the write lock up front (`database/migrate.go:49-68`); pinned by `database/concurrent_write_test.go`; single-use recovery code consumed in one `WHERE` (`twofactor.go:171-175`) |
| 11.1.7 | Monitor unusual business activity | partial | Anomaly signals exist (429s, lockouts) and are logged; no standing business-anomaly monitoring. Gap: product decision. |
| 11.1.8 | Configurable alerting | partial | Webhooks exist (user-configured, `services/webhook_broadcast.go`) but are not wired to security-anomaly events. Gap: no alert on lockout spikes. |

## V12 — Files and Resources

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 12.1.1 | No oversized uploads / DoS | satisfied | Photo 10 MB cap (`photo_controller.go:147-151`), attachment 25 MB (`attachment_controller.go:96-115`), global body cap 10MB + `MaxBytesReader` (`body_limit.go:21-32`) applied engine-wide by `main.go`; wire-size caps alone don't bound decoded memory, so declared image dimensions are checked via `image.DecodeConfig` (header-only read) before any full decode allocates a raster (`photostore/photostore.go:39-72` `CheckImageDimensions`, wired into `photo_controller.go:241-244` and `photostore.go:87-89`) — E2E'd end-to-end (issue #375) by `controllers/hostile_input_e2e_test.go` (`TestAddPhotoToContact_DecompressionBombRefused`, `TestProcessAndSavePhoto_DecompressionBombRefused`) and `photostore/decompression_bomb_test.go`. Issue #416 fix: the three VCF/CSV/JSContact import-upload routes are an explicit, hardcoded exemption from that 10MB default (`middleware.largeBodyRoutePaths`) with their own larger `BodySizeLimitMiddleware` registered directly on the route in `routes.go` (`services.MaxCSVSize`=20MB, `services.MaxVCFSize`=50MB) — before this fix those two constants were dead code, since the engine-wide 10MB default rejected anything larger before the handler's own check could ever run; pinned by `middleware/body_limit_test.go` (`TestDefaultBodySizeLimitMiddleware_ExemptPathBypassesDefaultLimit`) and the real-route E2E in `controllers/import_body_size_limit_e2e_test.go`. Issue #550's Meerkat import assistant adds `/contacts/import/meerkat/upload` to that same hardcoded exemption with its own `BodySizeLimitMiddleware(services.MaxMeerkatDBSize)` (100 MB), and the session layer re-checks `header.Size` and copies at most `MaxMeerkatDBSize+1` bytes (`services/meerkat_import_session.go` `Upload`); pinned by `services/meerkat_import_session_test.go` (`TestMeerkatImportSession_RejectsOversized`). Issue #512 pinning: the outbound CardDAV/CalDAV *client* sync response caps (`maxContactResponseBytes`/`maxCalendarResponseBytes`, 20MiB each, `services/contact_sync_service.go`, `services/calendar_sync_service.go`) are proven actually wired into the live `SyncSubscription` HTTP path, not just present as constants, by `services/contact_sync_hostile_input_test.go` (`TestSyncSubscription_OversizedResponse_RejectedNotSilentlyAccepted`) and `services/calendar_sync_hostile_input_test.go` (`TestSyncSubscription_OversizedCalendarResponse_RejectedNotSilentlyAccepted`) — an oversized remote response is refused, not silently buffered/parsed. |
| 12.1.2 | Zip-bomb protection | not-applicable | The app never decompresses archives (no zip/gz processing of uploads). The adjacent image-decompression-bomb vector (a highly compressible image declaring huge pixel dimensions) is covered by 12.1.1's `CheckImageDimensions` guard, not this control. |
| 12.1.3 | Per-user storage quota | partial | No quota (self-hosted; disk is the operator's). Gap: product decision. |
| 12.2.1 | File type validated by content | satisfied | Magic-byte sniffing + decode-verify (JPEG/PNG/HEIC) (`photo_controller.go:202-258`, `photostore/photostore.go:77-96`); attachments sniffed the same way, ignoring the client-declared filename/Content-Type (`attachment_controller.go:117-124`) — E2E'd against mislabeled/polyglot content (issue #375, `controllers/attachment_real_db_test.go` `TestAttachmentPolyglotMislabeledContentSniffed`). Issue #550: an uploaded Meerkat import file is validated by its SQLite magic header before it is opened, and opened `mode=ro` — never written, migrated, or executed (`backend/meerkat/reader.go` `Open`, `ErrNotSQLite`); a non-SQLite upload is rejected cleanly, pinned by `services/meerkat_import_session_test.go` (`TestMeerkatImportSession_RejectsNonSQLite`). |
| 12.3.1 | Filename metadata not used by FS | satisfied | Server-generated UUID names (`photo_controller.go:264-266`, `attachments.go:39-52`) |
| 12.3.2 | LFI via filenames | satisfied | Traversal guards (`photo_controller.go:95-99`, `attachments.go:27-35`) — E2E'd end-to-end through the real upload handler (issue #375, `controllers/attachment_real_db_test.go` `TestAttachmentTraversalFilenameNeutralized`) |
| 12.3.3 | RFI/SSRF via filenames/URLs | satisfied | Public-IP-only dialer (`httputil/safedial.go:27-47`); URL import path shares it (`photostore.go:207-209`) |
| 12.3.4 | Reflective File Download | satisfied | `Content-Disposition` with fixed/server names + `nosniff` on downloads (`attachment_controller.go:212-238`, `photo_controller.go:385`) |
| 12.3.5 | No OS-command injection via files | satisfied | No `os/exec`; no filename in system calls |
| 12.3.6 | No execution of untrusted code | satisfied | No plugin system, no runtime code loading, and no `os/exec` in any non-test backend package (`CLAUDE.md` Security posture; the sole `os/exec` import in the tree is `backend/database/backup_test.go`). Every dependency is version-pinned in a lockfile (`backend/go.sum`, `frontend/yarn.lock`) and the Go toolchain is pinned (`backend/go.mod`) |
| 12.4.1 | Uploads outside web root, limited perms | satisfied | Photo/attachment dirs 0700/0750 (`photostore.go:137`, `attachments.go:40,48`); served via controllers, not static. The Meerkat import upload (issue #550) is staged in a per-session `os.MkdirTemp` directory, written `0600`, never under a served path, and `os.RemoveAll`'d on cancel/expiry (`services/meerkat_import_session.go`); pinned by `services/meerkat_import_session_test.go` (`TestMeerkatImportSession_CancelRemovesTempDir`, `TestMeerkatImportSession_CleanupExpiredRemovesTempDir`) |
| 12.4.2 | Antivirus scanning | not-applicable | Self-hosted; replaced by magic-byte + decode + markup-signature validation (V12.2.1, `attachment_controller.go:42-63`) |
| 12.5.1 | Extension allow-list on web tier | satisfied | No static serving of uploads; only built SPA assets are static (`docker/nginx.conf`) |
| 12.5.2 | Uploads never executed as HTML/JS | satisfied | `nosniff`, download-only default, SVG/HTML rejected, photos re-encoded to JPEG (`photo_controller.go:330-338`, `attachment_controller.go:212-238`) |
| 12.6.1 | SSRF resource allow-list | satisfied | Public-IP-only dialer rejects loopback/link-local/private/CGNAT and pins resolved IPs (`httputil/ipguard.go:30-59`, `safedial.go:27-47`); per-service opt-in (V5.2.6) |

## V13 — API and Web Service

L3-only, out of scope: none in this chapter.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 13.1.1 | Single parser/encoding stack | satisfied | One Go `net/http`+`encoding/json` stack for all endpoints; CardDAV/CalDAV are separate RFC text protocols with their own strict parsers |
| 13.1.3 | No sensitive info in API URLs | satisfied | Session material is an httpOnly cookie and API tokens an `Authorization` header (`backend/middleware/auth.go:20-58`); path parameters are opaque resource ids that are re-scoped to the caller on every read (V4.2.1). Gated by the `mycorrhizal-query-string-auth-material` Semgrep rule (`backend/.semgrep/mycorrhizal-traps.yaml:148-177`) |
| 13.1.4 | Authz at URI and resource level | satisfied | Route-group middleware (`routes/routes.go:52-55`) + per-resource `user_id` scoping in every handler |
| 13.1.5 | Reject unexpected content types | partial | Non-JSON bodies fail `ShouldBindJSON` → 400 `VALIDATION_ERROR` (`validation.go:332-368`); not the 406/415 the standard asks for. Gap: content-type check + correct status. |
| 13.2.1 | HTTP methods valid for action | satisfied | Route table defines only implemented methods; anything else → 404 (`routes/routes.go`) |
| 13.2.2 | JSON schema validation before accept | satisfied | Struct-tag schema via `ValidateJSONMiddleware` on every input route (`validation.go:332-368`) |
| 13.2.3 | CSRF protection for cookie-based REST | satisfied | `SameSite=Strict` session cookies + strict CORS allowlist (V4.2.2, `main.go:441-457`) |
| 13.2.5 | Explicit Content-Type check | partial | Same as 13.1.5 (bound by `ShouldBindJSON`, status is 400 not 415) |
| 13.2.6 | Header/payload integrity in transit | satisfied | TLS at the operator's reverse proxy supplies confidentiality and integrity for the whole request/response (`docs/deployment.md:32`), with HSTS emitted once HTTPS is configured (`backend/middleware/security_headers.go:43-45`, wired at `backend/main.go:461`). No application-layer message signing is layered on top, deliberately: single origin, no intermediaries inside the trust boundary (V1.9.2) |
| 13.3.1–13.3.2 | SOAP/WS-Security | not-applicable | No SOAP |
| 13.4.1–13.4.2 | GraphQL | not-applicable | No GraphQL; REST only |

## V14 — Configuration

L3-only, out of scope: 14.1.5.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| 14.1.1 | Secure, repeatable build/deploy | satisfied | CI/CD via GitHub Actions + `docker-compose` (`docker-compose.yml`, `.github/workflows/`) |
| 14.1.2 | Compiler hardening flags | not-applicable | Go is memory-safe; `go vet` + gosec in CI (`unit-tests.yml`) |
| 14.1.3 | Server hardened per framework guidance | satisfied | Non-root container (`Dockerfile`, `entrypoint.sh`), security headers (`security_headers.go`), `GIN_MODE=release` documented for production (`.env.example:21,102`) |
| 14.1.4 | Redeployable from documented runbook | satisfied | `docker-compose` + `docs/deployment.md`; migrations run on boot (`database.InitDB`) |
| 14.2.1 | Dependencies current (checker in build) | satisfied | govulncheck (`unit-tests.yml:240-242`), Trivy (`docker-publish.yml:692-699`), Trivy license scan (`license-compliance.yml`, issue #361), Dependabot, Dependency Review |
| 14.2.2 | Unneeded features removed | satisfied | No debug endpoints; release-mode config guards (`config.go:649-660`); no sample data |
| 14.2.3 | SRI for CDN assets | not-applicable | All assets self-hosted; no CDN |
| 14.2.4 | Third-party from trusted repos | satisfied | Pinned actions by SHA (zizmor), lockfiles (`go.sum`, `yarn.lock`), pinned Go toolchain |
| 14.2.5 | SBOM maintained | satisfied | SBOM generated + attached as image referrer (`docker-publish.yml:510-515`); standalone signed SPDX/CycloneDX SBOMs per release and per main-branch merge (`syft-sbom.yml`); how to fetch and verify one — `docs/security/release-verification.md` |
| 14.2.6 | Third-party sandboxed | not-applicable | Single-process app; dependency vetting via scanners (14.2.1) |
| 14.3.2 | Debug modes disabled in production | satisfied | `GIN_MODE=release` required for prod-safe config (`.env.example:21,102`); enforced by config tests (`config_test.go:45-91`) |
| 14.3.3 | No version disclosure in headers | satisfied | No version headers/`X-Powered-By`; server errors are generic (V7.4.1) |
| 14.4.1 | Content-Type with safe charset | satisfied | Gin sets `application/json; charset=utf-8` |
| 14.4.2 | `Content-Disposition: attachment` on API responses | partial | Set on file downloads (`attachment_controller.go:212-238`); JSON API responses don't carry it. Gap: mostly cosmetic for a JSON SPA but requested by ASVS. |
| 14.4.3 | CSP header | satisfied | API: `default-src 'none'; frame-ancestors 'none'` (`security_headers.go:23`); SPA: strict CSP in nginx (`docker/nginx.conf:21`). Enforcement (not just presence) proven against a real browser by injecting an inline script and asserting the `securitypolicyviolation` event (`frontend/e2e/securityHeaders.spec.ts`, issue #374). That same E2E caught `/api/`, `/health`, `/carddav/`, `/caldav/` each carrying *both* the API's CSP and the SPA's — nginx's server-level `add_header` set was being inherited into those proxy locations on top of the backend's own headers (fixed in the same PR: each proxy location now cancels that inheritance, `docker/nginx.conf:32-53` region). |
| 14.4.4 | `X-Content-Type-Options: nosniff` | satisfied | `security_headers.go:39`, `docker/nginx.conf:19`; served-response assertion on `/`, `/index.html`, a hashed static asset, and `/api/` in `frontend/e2e/securityHeaders.spec.ts` (issue #374) |
| 14.4.5 | HSTS on all responses | satisfied | `max-age=31536000; includeSubDomains` when HTTPS configured — API (`security_headers.go:43-45`) and nginx edge (`docker/nginx.conf` + `docker/entrypoint.sh`, gated on `COOKIE_SECURE`); boot refuses the insecure combo (`config.go:677-683`). Absence on this stack's plain-HTTP config (`COOKIE_SECURE` unset) is asserted live in `frontend/e2e/securityHeaders.spec.ts` (issue #374); the on/off branch itself is pinned by `security_headers_test.go`. |
| 14.4.6 | Referrer-Policy | satisfied | `strict-origin-when-cross-origin` (`security_headers.go:40`); served-response assertion in `frontend/e2e/securityHeaders.spec.ts` (issue #374) |
| 14.4.7 | Not frameable | satisfied | `X-Frame-Options: DENY` + `frame-ancestors 'none'` (`security_headers.go:38`, `security_headers_test.go:12-48`); served-response assertion in `frontend/e2e/securityHeaders.spec.ts` (issue #374) |
| 14.5.1 | Only methods in use accepted | satisfied | Route table defines the set; others 404 (`routes/routes.go`) |
| 14.5.2 | Origin not used for authz | satisfied | Authorization derives only from the verified JWT/API token plus the `user_id` scope on every query (`backend/middleware/auth.go:66-98`, V4.1.1). `Origin` is read exclusively by the CORS policy (`backend/main.go:430-446`), which decides whether a browser may *read* a response — never whether the caller may perform the action |
| 14.5.3 | CORS strict allow-list, no "null" | satisfied | `AllowOrigins = [FrontendURL]`; `"*"` refused in release (`main.go:441-457`, `config.go:649-660`) |
| 14.5.4 | Proxy-supplied headers authenticated | satisfied | No proxy headers are trusted for auth; `X-Forwarded-*` used only for logging/ClientIP via validated trusted proxies (`config.go:711-725`, `main.go:484`) |

`Permissions-Policy` is not an ASVS 4.0.3 L2 control, but it is set on every
response (`camera=(), microphone=(), geolocation=(), interest-cohort=()` — API
`security_headers.go`, SPA `docker/nginx.conf` + `frontend/nginx.conf`). It
closes the last remaining gap in the "Network, Headers" hardening checklist
(issue #364).

---

## OWASP API Security Top 10 (2023)

| # | Risk | Status | Evidence |
|---|---|---|---|
| **API1** | Broken Object Level Authorization (BOLA/IDOR) | satisfied | Every handler AND-scopes by `user_id`/`VCardUID` (`circle_controller.go:49`); 404-masking hides existence (`contact_share_controller.go:212-233`); cross-user tests per entity (V4.2.1, incl. `sync_conflict_controller_test.go:109` and the contact-share matrix, issue #555, `controllers/contact_share_matrix_test.go`); exhaustive every-route × six-persona authorization matrix (`backend/routes/authorization_matrix_test.go`, issue #371); automated cross-account sweep (`backend/cmd/bolacheck`, `schemathesis.yml`) |
| **API2** | Broken Authentication | satisfied | bcrypt cost 10 + explicit 72-byte cap (P1); dummy bcrypt compare on unknown users (`carddav/auth.go:16-19,56-57`); per-account exponential lockout (`rate_limiter.go:12-22,68-110`); TOTP 2FA (V2.8); `TokenVersion` revocation (`auth.go:141-154`) |
| **API3** | Broken Object Property Level Authorization (BOPLA) | satisfied | DTO allowlists; `IsAdmin`/status/provenance/confidence client-unsets (`models/dtos.go:294-300,339-354`); field-by-field update copies (`life_event_controller.go:346-353`); spec-derived request fuzzing (`schemathesis.yml`, `backend/cmd/schemagate`) |
| **API4** | Unrestricted Resource Consumption | satisfied | Body limits 10 MB/1 MB (`body_limit.go:11-40`), `MaxMultipartMemory` (`main.go:437`), per-IP API limiter (`rate_limiter.go:249`), timeouts (`HTTP_READ/WRITE_TIMEOUT`, `.env.example`); the per-operation bounds are inventoried in the issue #415 table below |
| **API5** | Broken Function Level Authorization | satisfied | Admin routes gated by `AdminMiddleware` (`middleware/admin.go`); `is_admin` never in JWT (`services/user_service.go:43`); CardDAV-scope tokens blocked from API (`auth.go:54-58`). Every admin route is enumerated in the every-route × six-persona matrix (`routes/authorization_matrix_test.go`); the newest admin-only diagnostic surfaces, `GET /api/v1/admin/system-status` (issue #388, `routes/routes.go:639`) and `GET /api/v1/admin/integrity-check` (DB-01, issue #460, `routes/routes.go:632`), are gated by that same middleware pair and are in the matrix's `classAdmin` set (unauthenticated→401 / non-admin→403) alongside their own endpoint tests (`controllers/system_status_controller_test.go`, `controllers/integrity_controller_test.go`) |
| **API6** | Unrestricted Access to Sensitive Business Flows | satisfied | Rate limits on auth/business endpoints (`routes/routes.go:27-49,53`); sensitivity filtering in exports/sync/graph (V1.8.2); account lockout on login and 2FA (`two_factor_controller.go:346-356`) |
| **API7** | Server Side Request Forgery | satisfied | Public-IP-only dialer with DNS-rebinding pinning (`httputil/safedial.go:27-47`); pre-flight checks (`fetch.go:17-58`); enforced always on photo proxy/import, opt-in per service (V5.2.6); tests `httputil/fetch_test.go`, `webhook_ssrf_test.go`, `webhook_ssrf_integration_test.go` (live webhook job path), `notification_service_test.go` (push path); semgrep gate on new outbound dialers (`.semgrep/mycorrhizal-traps.yaml` rule `mycorrhizal-unguarded-outbound-dialer`, fixture `.semgrep/tests/unguarded_outbound_dialer.go`) |
| **API8** | Security Misconfiguration | satisfied | Boot-time config validation (`config.go:372-403,511-516,527-532`); security headers (V14.4); release-mode guards; `/.well-known/security.txt` (RFC 9116); config tests `config_test.go` |
| **API9** | Improper Inventory Management | partial | `openapi.yaml` maintained + drift-checked (`backend/openapi_spotcheck_test.go`, `openapi_request_test.go`); no formal versioning/deprecation policy beyond API v1 path. Gap: endpoint inventory doc. |
| **API10** | Unsafe Consumption of APIs | satisfied | SSRF-guarded dialers on every outbound fetch (V5.2.6); response size caps (`fetch.go:61,152-160`); content-type enforcement on fetched media (`fetch.go:126-163`); SVG/HTML rejection (`photo_controller.go:361-369`, `attachment_controller.go:42-63`) |

---

## Resource-exhaustion / application-level DoS limits (issue #415)

The invariant: **a single authenticated user must not consume unbounded CPU, memory, disk, DB
connections, or outbound bandwidth.** IP rate limiting does not stop an authenticated caller from
repeatedly invoking expensive operations, so every expensive operation below carries an explicit,
tested bound. "Own data" means the operation's cost is proportional to the caller's own dataset
(paginated/own-scoped), which is the documented bound for the read-mostly surfaces.

| Operation | Bound | Where | Test |
|---|---|---|---|
| Request body size (engine-wide) | 10 MB; import uploads 20 MB CSV / 50 MB VCF+JSContact | `middleware/body_limit.go:12,31-35`, `routes/routes.go:141-160` | `middleware/body_limit_test.go`, `controllers/import_body_size_limit_e2e_test.go` |
| Attachment upload | 25 MB (effectively ≤10 MB under the engine-wide cap) | `attachment_controller.go:96-115` | `attachment_real_db_test.go` |
| Profile photo | 10 MB + decompression-bomb/dimension guards | `photo_controller.go:147-151`, `photostore/photostore.go:39-72` | `photostore/decompression_bomb_test.go`, `controllers/hostile_input_e2e_test.go` |
| Search result count | 50 per section, default 10 | `services/search_service.go:75-76` | `search_service_test.go` |
| Search term length | 256 runes → 400 | `services/search_service.go:86` `MaxSearchTermLen`; enforced `search_controller.go:36`, `contact_controller.go:385` | `search_controller_test.go`, `contact_controller_test.go` |
| Import size / row count | CSV 20 MB / 20 000 rows; VCF+JSContact 50 MB / 20 000 contacts; records 500 | `services/import_service.go:33-37`, `models/import.go:126` | `import_service_vcf_test.go`, `import_controller_test.go` |
| Adversarial corpus (TEST-04) | parser-level size/structural bounds; the #415 limits above are this corpus's upload boundary, cross-referenced not duplicated | `docs/adversarial-fixtures/` (+ embedded `internal/adversarial/fixtures/`) | `internal/adversarial/adversarial_test.go` (`TestDeclaredTiersHold`, `TestSizeHostility_Amplified` — a 2 MB single property, 20 000 properties on one card, no panic/hang), `services/adversarial_import_test.go` (bounded failure: a malformed record inside a batch is a named skip row, valid neighbors import) |
| In-memory import sessions / user | 5 concurrent → 429 | `services/import_session.go:29` `MaxImportSessionsPerUser`, `CountActive:216`; enforced in all upload + share-accept handlers | `import_session_test.go` `TestCountActive`, `import_controller_test.go` `TestUploadCSVForImport_SessionOverLimit` |
| Bulk contact ops | 500 targets | `models/bulk_operation.go:20` | `bulk_operation_controller_test.go` |
| Graph traversal depth | 5 | `services/graph_traversal.go:35` | `graph_controller_test.go` |
| Graph view (GET /graph) | own data (full graph of the caller's contacts/edges/activities) | `graph_controller.go:18-134` | `graph_controller_test.go` |
| Export (CSV/VCF/JSContact) | own data (bounded by the caller's contact dataset) | `export_controller.go:173-750` | `export_controller_test.go` |
| Audit-log export | 100 000 rows → 400 | `export_controller.go:762` `MaxAuditExportRows` (read via the `auditExportLimit:771` seam), `Limit` at `:816` | `audit_export_controller_test.go` `TestExportAuditLog_RejectsOverCap` |
| Audit log list | 500 rows | `audit_controller.go:33-38` | `audit_controller_test.go` |
| Webhooks / user | 20 | `webhook_controller.go:34` | `webhook_controller_test.go` |
| Webhook Events per webhook | 12 (the oneof token count) | `models/dtos.go:533-538` | `webhook_controller_test.go` `TestCreateWebhook_TooManyEventTokens` |
| Webhook delivery fan-out | semaphore 10 concurrent outbound; 15 s timeout; ≤3 redirects; 3 attempts | `webhook_service.go:24,37-60,71` | `webhook_broadcast_test.go`, `webhook_delivery_test.go` |
| Push subscriptions / user | 20 → 409 | `notification_controller.go:24,209` | `notification_controller_test.go` `TestPushSubscription_OverCapRejected` |
| Device registrations / user | 20 → 409 | `notification_controller.go:26,301` | `notification_controller_test.go` `TestDeviceRegistration_OverCapRejected` |
| Pagination | max 100/page | `helpers.go:20` | `helpers_test.go` |
| Rate limiting | per-IP 100 req/min sustained (API), burst 1000; per-account lockout on auth | `rate_limiter.go:249,258` | `rate_limiter_test.go` |
| HTTP timeouts | read/write/idle (`HTTP_READ/WRITE/IDLE_TIMEOUT`, 1–300 s) | `main.go:327-333`, `config/config.go:56-57` | `config_test.go` |

Parser recursion: the vCard2.1/3.0/4.0 adapters are line-based/iterative (`vcard4/`, `vcard3/`,
`vcard21_normalize.go`), and JSContact parsing goes through `encoding/json` (depth-limited —
the `js-deeply-nested.json` corpus fixture proves the depth guard fires as an error, issue #432);
vCard parsing is additionally bounded by the 50 MB file cap and the 20 000-contact row cap above.
Concurrent operations beyond the webhook semaphore are bounded by the HTTP timeouts + the per-IP
rate limiter; there is no global in-process semaphore (deliberate — this is single-process
self-hosted, and bounding per-operation work is cheaper than bounding concurrency).

---

## How to keep this honest (the "living" part)

- **PR/review anchor:** a security-sensitive PR must update the row(s) it affects. If the PR
  changes a control's implementation, flip the row's status and citation in the same commit.
- **New endpoints/entities:** the V4.2.1 and API1 rows are where a new `user_id` scope or its
  absence gets recorded; the V7.2.2 row is where a new access-denied path gets noted.
- **New dependencies:** update V14.2.1/14.2.4 if the CI scanner set changes; the SBOM row if
  image attestation changes.
- **New ZAP findings:** accepted DAST findings are recorded in `zap/dast.ignore` (ignore-list with
  justification, same shape as `android/.mobsf`); the canary self-test (`backend/cmd/dastcanary`)
  must stay vulnerable-by-design or the DAST gate goes blind.
- **New Schemathesis findings:** accepted 5xx/auth findings are recorded in
  `schemathesis/schemathesis.ignore` (ignore-list with justification); a finding that can't be
  justified there is a real bug, not a config gap.
- **N/A rows are decisions:** if a control starts to apply (say, a second process or a managed
  database appears), its row must flip from `not-applicable` to a real status.
- **grep-ability check:** no row is `satisfied` without a `file:line` or test-name citation; a
  review can `grep -n "4.2.1" docs/security/asvs-l2.md` and land on the answer.
- **Citations are gated, not trusted:** `cd backend && go run ./cmd/citecheck` proves every
  citation in this file (and `masvs-l1.md`, `threat-model.md`) resolves to a real file with an
  in-range line, that every cited test identifier still exists, and that no row is `satisfied`
  with an empty or citation-free evidence cell. It runs on every PR as the `Security-doc
  citations` job in `.github/workflows/unit-tests.yml`, unfiltered by path — a citation is
  orphaned by *moving code*, not by editing this file.
- **Drift gates against a written-down baseline:** a citation whose line range is still in bounds
  but whose target has moved is the class the resolution gate structurally cannot see, and the one
  that actually accumulates — the #378 pass found 74 of them. `citecheck` therefore fails on any
  drift candidate not accepted in `docs/security/citation-drift.ignore`, and equally on an entry
  there that no longer matches anything, so the file cannot fill up with dead suppressions. Adding
  a line to it is a decision with a reason attached, the same as `.trivyignore` or
  `zap/dast.ignore` — not a mute button. `go run ./cmd/citecheck -drift` prints the unfiltered
  listing (accepted entries included), which is what you want during a verification pass.
- **A verification pass, not just a mapping edit:** when the status of many rows could have moved
  at once (a release, a security review, a re-verification), do a full pass and add a dated row to
  `asvs-l2-verification-report.md`'s changelog rather than editing statuses in place. That report
  is the answer to "when was this last actually checked, and by what method?".
