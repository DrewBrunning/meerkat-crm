# Security checklist: OWASP MASVS-L1 (Android client)

The Android companion to `asvs-l2.md` (which covers backend + frontend + deployment). This file is
the living answer to "is the mobile app secure?" — a per-control map of the OWASP MASVS Level 1
requirements to the Kotlin source and CI that satisfy them. If a control's status changes, the PR
that changes it updates this file. A security-sensitive Android PR should touch the relevant row
here; if it cannot point at a row, it does not know what it is changing.

| | |
|---|---|
| **Standards pinned** | OWASP MASVS 1.5.0, chapters V2–V7 (the `MSTG-*` control IDs). MASVS 2.0 renamed these one-to-one to `MASVS-*` (V2–V7); the L1 rows below are the same either way. |
| **Level** | **MASVS-L1** (rows marked `x` in the L1 column). L2- and R-only rows are listed per chapter as out of scope. |
| **Last full pass** | 2026-08-26 — the MASVS-L1 verification pass, issue #378, recorded alongside the ASVS pass in `docs/security/asvs-l2-verification-report.md`. Prior pass 2026-08-25 (issue #507: MASVS-L2 resilience re-evaluation — see Documented positions). |
| **Level claimed** | **MASVS-L1 with 1 documented exception** (STORAGE-5, the login screen's keyboard options). STORAGE-9 and PLATFORM-9 are L2 controls satisfied as a bonus, not a level claim — see P6. |
| **Scope** | The Android client (`android/`): Kotlin + Jetpack Compose, Hilt, OkHttp, Room, DataStore, `androidx.security:crypto`. The server it talks to is covered by `asvs-l2.md`; MSTG-AUTH rows therefore cite the *client-side* contract and point at the backend for enforcement. |

## Status legend

| Status | Meaning |
|---|---|
| **satisfied** | Control is met; the row cites `file:line` or a test name. |
| **partial** | Control is met in part, or met with a documented deviation (each `partial` row names the gap). |
| **not-applicable** | Control does not apply (no WebView, no native code, no third-party SDK, …) — one-line reason given. |
| **out-of-scope** | L2/R-only control (not in the L1 target); listed for grep-ability. |

"Answered in the doc" means: take any `MSTG-xxxx-y` from MASVS 1.5.0, grep for it below, and get
a status + citation. No row is left `satisfied` without a citation.

## Documented positions

The `android/.mobsf` config already records a set of deliberate scanner findings as ignore-list
entries with one-line rationales. These are positions, not code changes — they are promoted here
with their full rationale so the audit is self-contained.

### P1 — No certificate pinning (MSTG-NETWORK-4 is L2 anyway; re-evaluated, kept declined, issue #507)

Certificate pinning / transparency (`android_certificate_pinning`, `android_certificate_transparency`,
`android_ssl_pinning`) is deliberately not done. MSTG-NETWORK-4 is an L2 control, so it is out of
scope for L1 — but the ignore-list is explicit rather than silent.

**Defends against:** an on-path attacker (rogue Wi-Fi AP, compromised router, malicious CA) presenting
a valid-but-wrong certificate for the operator's domain. **Costs:** every user runs their own
**self-hosted** server (`docs/deployment.md:32`) with a certificate the app cannot know in advance —
frequently self-signed or issued by an internal/private CA the user set up themselves, rotated on
whatever schedule that user chooses. A pin the app ships with would be wrong for essentially every
install on day one, and a pin computed from the first-seen cert (trust-on-first-use) is a materially
different feature — a pin store, a UI to review/clear it, and a break-glass path for legitimate
rotation — not a checkbox toggle on the naive pinning MSTG-NETWORK-4 describes. **In scope per #377:**
yes, MITM is a real actor (`threat-model.md` "Unauthenticated network attacker" row) — but it's
neutralized today by standard TLS + system CA trust (`asvs-l2.md` V9, `masvs-l1.md` NETWORK-1/NETWORK-3),
which holds for a self-signed cert too once the user has explicitly trusted it via the **KeyChain
import flow** (`installCustomCertificate`) — never by disabling TLS verification (see the production
`network_security_config.xml` comment). **Decision: keep declined.** Naive pinning is actively wrong
for this app's threat model, and TOFU/user-managed pinning is a distinct, uncosted feature this issue
does not adopt — see `threat-model.md` gating decision 3.

### P2 — Debug-only cleartext to the emulator loopback

The debug source set (`app/src/debug/res/xml/network_security_config.xml:19-28`) permits cleartext
HTTP to `10.0.2.2` / `127.0.0.1` / `localhost` so `adb`-driven local-backend testing works. This
file **never ships in a release build** — the production config forbids cleartext unconditionally
(`app/src/main/res/xml/network_security_config.xml:18`). Ignored at INFO (`android_manifest_domain_config_cleartext`)
so it stays visible in the source comment rather than adding CI noise.

### P3 — No root/SafetyNet detection, anti-task-hijacking by design (re-evaluated, kept declined, issue #507)

`android_root_detection` and `android_safetynet`/`android_safetynet_api` are ignored.

**Defends against:** the app running on a rooted/compromised device where the OS sandbox itself can no
longer be trusted — a malicious app with root could read another app's memory or storage regardless of
what this app does. **Costs:** self-hosted users are disproportionately likely to root their devices
*deliberately* (custom ROMs, ad blocking, backup tooling, the same self-reliance that led them to
self-host a CRM in the first place); blocking or degrading the app on root would punish exactly the
audience this project serves, for a threat model where the attacker already has the device. SafetyNet
Attestation is additionally a **deprecated** platform service (superseded by Play Integrity, which
itself assumes a Play-Store-distributed app — this project also ships a direct-download APK, see
`android-apk-build.yml`). **In scope per #377:** the "lost/stolen Android device" actor
(`threat-model.md`) is real, but it's answered by data-at-rest controls (SQLCipher Room mirror,
Keystore-backed session token — P4/STORAGE-1) that hold regardless of root status, not by refusing to
run. A rooted-but-not-stolen device run by its own owner is not a threat this app has an asset to
protect against. **Decision: keep declined** — this is the control the issue named as the one to weigh
"honestly" against the audience, and the audience-cost argument holds without qualification. See
`threat-model.md` gating decision 3.

The one task-hijacking-adjacent risk that *is* real is addressed by design, not by a detection
control: the exported `MainActivity` uses `launchMode="singleTask"` specifically so the OIDC deep
link is delivered via `onNewIntent` rather than stacking a second instance
(`app/src/main/AndroidManifest.xml:57`, comment at `:66-68`); `android_task_hijacking1/2` are ignored
for that reason, unrelated to the two resilience controls above.

### P4 — Room cache encryption carve-outs (resolved 2026-08-25, issue #385)

The Room mirror (`mycorrhizal-cache.db`) is now SQLCipher-encrypted whole-DB — AES-256-CBC pages
with per-page IVs + HMAC-SHA512 integrity, keyed by a random 32-byte passphrase stored in
`EncryptedSharedPreferences` behind a Keystore `MasterKey` (`RoomPassphraseStore.kt`). A one-time
bootstrap re-encrypts a pre-existing plaintext DB in place via `sqlcipher_export`
(`RoomCacheEncryption.kt`), preserving every object including the FTS4 mirror and the
non-rebuildable `pending_interactions` outbox; the plaintext file is overwritten before deletion.
The DB, its WAL/shm sidecars, and the Coil/photo cache are purged on logout/account-removal
(`LocalDataCleaner.kt`).

Deliberate test carve-out: the Robolectric JVM unit tests run the migration/DAO logic against the
plain framework SQLite factory because SQLCipher's `libsqlcipher.so` is an Android-native binary
that cannot load on the JVM. The encrypted factory — same migration chain plus the transition and
FTS search under encryption — is verified by instrumented tests
(`app/src/androidTest/.../storage/RoomCacheEncryptionTest.kt`) and implicitly by every E2E boot of
the real app, which opens the encrypted DB through `DataModule`.

### P5 — `.mobsf` false positives (not positions, recorded for the record)

`android_kotlin_hardcoded` fires only on the `const val SECRET = "secret"` sensitivity enum value
(the `normal|private|secret` taxonomy shared with the backend) — a domain value, not a credential.
`android_kotlin_sql_raw_query` fires only on the static Room-migration DDL strings
(`core/data/.../Migrations.kt`, `Migration13To14Test.kt`) — compile-time SQL with no injection
surface.

### P6 — Screenshot prevention + tapjacking protection (resolved 2026-08-25, issue #507)

The other two of the four MASVS-L2 resilience items issue #507 asked to re-evaluate — **reversed**,
not kept declined.

**MSTG-STORAGE-9** ("the app removes sensitive data from views when moved to the background"):
every screen in this single-Activity app renders relationship PII (contact records, personal notes,
`secret`-sensitivity fields), so the concrete threat is the recent-apps thumbnail — anyone with brief
physical access to an unlocked-but-idle phone can open the app switcher and read a contact's data
without ever unlocking the app itself, no attacker sophistication required. **Cost:** the user can no
longer screenshot or screen-record their own contact data — a real but narrow loss (sharing a contact
card is already a first-class feature via the vCard share sheet, `AndroidManifest.xml:76-86`, so
screenshotting isn't the only way to get data out). `MainActivity.onCreate` sets
`WindowManager.LayoutParams.FLAG_SECURE` unconditionally (`MainActivity.kt`), which also blocks
screenshots and screen recording as a side effect of blanking the thumbnail — the same primitive
covers both. **MSTG-PLATFORM-9** ("the app protects itself against screen overlay attacks"): a
malicious overlay app could tapjack a user into confirming a destructive action (e.g. contact/account
deletion) they can't see underneath the overlay. **Cost:** effectively none — `filterTouchesWhenObscured`
only changes behavior when another window is actually drawn on top, which never happens during normal
single-window use. `MainActivity.onCreate` sets `window.decorView.filterTouchesWhenObscured = true`
(`MainActivity.kt`), which gates touch dispatch to the whole view tree from the decor view down — no
per-screen wiring needed. Both reversed the earlier positions recorded in `.mobsf`/P3 above (mobsf's
`android_prevent_screenshot` and `android_detect_tapjacking`/`android_tapjacking` ignore-rules were
removed in the same change, so mobsfscan now asserts their presence rather than staying silent on
them). Verified by the instrumented `ScreenResilienceTest`
(`app/src/androidTest/.../security/ScreenResilienceTest.kt`, issue #238's suite) — a Robolectric JVM
test's shadow `Window` doesn't model `FLAG_SECURE` or touch filtering, so this can only be verified on
a real device/emulator. See `threat-model.md` gating decision 3 for the assurance-level consequence:
this does **not** make MASVS-L2 claimable (root detection and certificate pinning above stay declined,
and several other L2 rows — STORAGE-10/11/13/14/15, the entire V8 Resiliency chapter — remain
out of scope) — the client's claimed target is still MASVS-L1, now with two L2 controls satisfied
as a documented bonus rather than a level claim.

---

## V2 — Data Storage and Privacy (MSTG-STORAGE)

L2-only, out of scope: STORAGE-8, STORAGE-10, STORAGE-11, STORAGE-13, STORAGE-14, STORAGE-15.
(STORAGE-8 is met anyway — backups are disabled: `allowBackup="false"` and `data_extraction_rules` exclude
every domain, `app/src/main/AndroidManifest.xml:38-39`, `res/xml/data_extraction_rules.xml`. STORAGE-9 is
L2 but satisfied — see the row below and P6.)

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| STORAGE-1 | Sensitive data in system credential storage | satisfied | The JWT is stored in `EncryptedSharedPreferences` with a Keystore `MasterKey` (AES256_GCM) — `core/data/.../EncryptedTokenStorage.kt:14-25`, DI binding `SessionStorageModule.kt:20-21`. The Room mirror is SQLCipher whole-DB encrypted — random 32-byte passphrase in `EncryptedSharedPreferences` + Keystore `MasterKey` (`RoomPassphraseStore.kt`), `SupportOpenHelperFactory` wired in `DataModule.kt:163`, plaintext→encrypted transition at `RoomCacheEncryption.kt` (P4). |
| STORAGE-2 | No sensitive data outside app container / credential storage | satisfied | All persistence is inside the sandbox (EncryptedSharedPreferences, DataStore, SQLCipher-encrypted Room). No external/shared storage for app data; the only `content://` surface is the FileProvider cache for user-initiated vCard sharing (`app/src/main/AndroidManifest.xml:78-86`). Offline PII is purged on logout/account-removal: the Room tables are cleared and the photo/attachment cache directory deleted (`LocalDataCleaner.kt`, invoked from `DefaultSessionManager.clearSession()`). |
| STORAGE-3 | No sensitive data written to logs | satisfied | The JWT is never logged (`EncryptedTokenStorage.kt:10` comment); OkHttp debug logging is `Level.BASIC` (request line only, no headers/body) and debug-only (`core/network/.../NetworkFactory.kt:43-49`). |
| STORAGE-4 | No sharing with third parties unless necessary | satisfied | No telemetry/analytics/third-party SDKs; Firebase is applied only when a real `google-services.json` exists and is otherwise inert (`app/build.gradle.kts:13-15`, `AndroidManifest.xml:98-105`). vCard export is a user-initiated system share sheet (`AndroidManifest.xml:76-86`). |
| STORAGE-5 | Keyboard cache disabled on sensitive inputs | partial | Register / forgot-password / settings / users secret fields use `KeyboardType.Password` (the Android signal that disables IME personalized learning) alongside `PasswordVisualTransformation` — `feature/auth/.../RegisterScreen.kt:151-152`, `ForgotPasswordScreen.kt:147-148,156-157`, `feature/settings/.../SettingsScreen.kt:268`. **Gap:** the login screen's password field (`feature/auth/.../LoginScreen.kt:221-227`) masks via `PasswordVisualTransformation` + autofill `ContentType.Password` but keeps default keyboard options — no `KeyboardType.Password` on the one field where a password is typed most often. |
| STORAGE-6 | No sensitive data exposed via IPC | satisfied | The only exported components are system-driven: `MainActivity` (launcher + OIDC deep link) and the `PHONE_STATE`/`SMS_RECEIVED` receivers (the latter guarded by `android.permission.BROADCAST_SMS`); everything else is `exported="false"` (`AndroidManifest.xml:78-129`). The FileProvider is `exported="false"` with per-URI grants (`:80-81`). |
| STORAGE-7 | No sensitive data exposed via UI | satisfied | Passwords are masked via `PasswordVisualTransformation` on all secret fields (see STORAGE-5 citations); no secret is rendered readably. |
| STORAGE-9 (L2) | App removes sensitive data from views when backgrounded | satisfied | `MainActivity.onCreate` sets `WindowManager.LayoutParams.FLAG_SECURE` unconditionally, blanking the recent-apps thumbnail and blocking screenshots/screen recording across every screen (`MainActivity.kt`) — issue #507, see P6. Verified by `ScreenResilienceTest.mainActivityWindowIsFlaggedSecure` (`app/src/androidTest/.../security/ScreenResilienceTest.kt`). |
| STORAGE-12 | App educates user on PII handling | satisfied | Self-hosted personal CRM: the privacy stance (data stays on the operator's server) is documented in `docs/deployment.md` and `CLAUDE.md`; no third-party collection exists to disclose. |

## V3 — Cryptography (MSTG-CRYPTO)

No L2-only rows in this chapter (CRYPTO-1…6 are all L1).

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| CRYPTO-1 | No hardcoded keys as sole encryption | satisfied | No symmetric keys in source; the Keystore `MasterKey` (AES256_GCM) is hardware-backed (`EncryptedTokenStorage.kt:15-17`). mobsfscan's `android_kotlin_hardcoded` fires only on the `SECRET` enum value (P5). |
| CRYPTO-2 | Proven cryptographic implementations | satisfied | `androidx.security:crypto` (`EncryptedSharedPreferences`, `MasterKey`) for the session token and Room passphrase, and SQLCipher (`net.zetetic:sqlcipher-android`) for the Room mirror — both platform-standard, no hand-rolled crypto (`EncryptedTokenStorage.kt:5-6`, `RoomPassphraseStore.kt`, `DataModule.kt`). |
| CRYPTO-3 | Appropriate primitives, best-practice parameters | satisfied | Master key `AES256_GCM`; pref keys `AES256_SIV`; pref values `AES256_GCM` (`EncryptedTokenStorage.kt:16,22-23`). SQLCipher uses AES-256-CBC pages with per-page random IVs + HMAC-SHA512 page integrity, keyed via PBKDF2-HMAC-SHA512 (256k iterations) from a random 32-byte passphrase — the library's standard parameters. |
| CRYPTO-4 | No deprecated algorithms | satisfied | No MD5/SHA1/DES/ECB/Blowfish anywhere in `android/`; only AES-256 (GCM/SIV/CBC) + HMAC-SHA512 via the platform library and SQLCipher. |
| CRYPTO-5 | No key reuse across purposes | satisfied | The single Keystore master key wraps *distinct* keys per purpose: the session-token key and the Room-mirror passphrase are separately generated (`EncryptedTokenStorage.kt`, `RoomPassphraseStore.kt`); the DB passphrase never encrypts anything but the SQLCipher database. No key is reused across different secrets. |
| CRYPTO-6 | Secure random number generator | satisfied | No custom RNG; the Keystore master key, `androidx.security.crypto`, and `SecureRandom` (Room passphrase, `RoomPassphraseStore.kt`) all draw from the platform CSPRNG; SQLCipher generates its per-page IVs and DB salt internally. |

## V4 — Authentication and Session Management (MSTG-AUTH)

The app is a thin client: authentication and authorization are enforced at the remote endpoint
(covered in `asvs-l2.md` V2/V3/V4). Client-side rows cite the client contract.

L2-only, out of scope: AUTH-8, AUTH-9, AUTH-10, AUTH-11.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| AUTH-1 | Authentication performed at the remote endpoint | satisfied | Username/password login (`feature/auth/.../LoginScreen.kt`, `LoginViewModel`) and OIDC (`feature/auth`) both submit to the backend; nothing is authenticated client-side only. |
| AUTH-2 | Stateful sessions use random server ids | not-applicable | Sessions are stateless JWTs, not stateful server ids — the `not-applicable` is the clean read; see AUTH-3. |
| AUTH-3 | Stateless tokens signed with a secure algorithm | satisfied | The backend issues HS256 JWTs (`asvs-l2.md` 3.2.4); the client treats the JWT as an opaque bearer credential and validates it against `/users/me` before flipping to logged-in (`MainActivity.kt:239-258`). |
| AUTH-4 | Remote endpoint terminates session on logout | satisfied | Backend logout + `token_version` revocation (`asvs-l2.md` 3.3.1); client `clearSession()` drops the token (`DefaultSessionManager`), leaving nothing to replay. |
| AUTH-5 | Password policy enforced at remote endpoint | satisfied | Entropy-based policy + strength meter (`asvs-l2.md` 2.1.1/2.1.8); client mirrors it in the register form. |
| AUTH-6 | Anti-automation against credential submission | satisfied | Per-account lockout + per-IP limiter at the endpoint (`asvs-l2.md` 2.2.1); client surfaces the 429 as a generic failure. |
| AUTH-7 | Session invalidation after inactivity, token expiry | satisfied | JWT 96 h absolute expiry + `token_version` revocation (`asvs-l2.md` 3.3.1/3.3.2); client handles 401 by clearing the session. |
| AUTH-12 | Authorization model enforced at remote endpoint | satisfied | Ownership scoping is entirely server-side (`asvs-l2.md` V4); the client only renders what the API returns — no client-side authz to bypass. |

## V5 — Network Communication (MSTG-NETWORK)

L2-only, out of scope: NETWORK-4, NETWORK-5, NETWORK-6.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| NETWORK-1 | TLS consistently, no cleartext | satisfied | Production network security config forbids cleartext (`cleartextTrafficPermitted="false"`) and trusts only system CAs — `app/src/main/res/xml/network_security_config.xml:17-23`, wired via `AndroidManifest.xml:43`. Pinned by test (see `NetworkSecurityConfigTest`). Debug-only loopback cleartext never ships (P2). |
| NETWORK-2 | TLS settings align with best practice | satisfied | The app lets the platform's TLS stack negotiate (no custom socket factory, no `sslSocketFactory` overrides anywhere); current OkHttp enforces TLS 1.2+. |
| NETWORK-3 | Verify X.509, only trusted CAs | satisfied | Trust anchors are `system` only (`network_security_config.xml:20`); user-installed CAs are trusted **only** in the debug variant (`debug/res/xml/network_security_config.xml:30-35`). No custom trust manager or `X509TrustManager` override. |

## V6 — Platform Interaction (MSTG-PLATFORM)

L2-only, out of scope: PLATFORM-10 (WebView cleanup — not-applicable, no WebView anywhere, see
PLATFORM-5/6/7), PLATFORM-11 (iOS-only third-party keyboard restriction — not-applicable, this is the
Android client). PLATFORM-9 is L2 but satisfied — see the row below and P6.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| PLATFORM-1 | Minimum necessary permissions | satisfied | Permissions are runtime, opt-in, and documented inline with rationale: `READ_CONTACTS` (link-action resolver, requested inline — `AndroidManifest.xml:7-12`), call-log/SMS/phone tracking (`:14-17`), notifications, overlay, foreground service, boot (`:18-26`). `uses-feature ... required="false"` so tablets/ChromeOS install without telephony hardware (`:30-31`). |
| PLATFORM-2 | Validate + sanitize all external input | satisfied | The OIDC deep link is strictly validated — scheme, host, **and** path — so an exported `MainActivity` cannot be driven by a foreign explicit-component VIEW intent (`MainActivity.kt:326-339`); pinned by `OidcReturnParsingTest` (`app/src/test/.../OidcReturnParsingTest.kt:53-58`). Notification deep links are parsed into known routes only (`MainActivity.kt:260-271`). |
| PLATFORM-3 | No sensitive functionality via custom URL schemes unless protected | satisfied | The only custom scheme is `mycorrhizal://`, and its one token-bearing route (`/oidc/callback`) carries the OIDC JWT — the scheme is unique to the app, the callback path is fixed, and an invalid/stale token is rejected before the session flips (`MainActivity.kt:239-258`). |
| PLATFORM-4 | No sensitive functionality via IPC unless protected | satisfied | No `ContentProvider` exposes app data (the FileProvider is `exported="false"` + per-URI grants); the exported receivers are system broadcasts only, and the SMS receiver requires `BROADCAST_SMS` (`AndroidManifest.xml:110-129`). |
| PLATFORM-5 | JavaScript disabled in WebViews | not-applicable | No WebView anywhere in the app (grep for `WebView`/`CustomTabs` returns nothing); OIDC login opens the system browser. |
| PLATFORM-6 | WebView protocol handlers minimized | not-applicable | No WebView (see PLATFORM-5). |
| PLATFORM-7 | Native bridges only render in-package JS | not-applicable | No WebView / `addJavascriptInterface` anywhere. |
| PLATFORM-8 | Safe deserialization | satisfied | Moshi (`NetworkFactory.kt:21,27-42`) with codegen (`@JsonClass`) — reflection-free, no `ObjectInputStream`/`Parcelable`-of-untrusted-data, no pickle/Java serialization. |
| PLATFORM-9 (L2) | App protects itself against screen overlay attacks | satisfied | `MainActivity.onCreate` sets `window.decorView.filterTouchesWhenObscured = true` unconditionally (`MainActivity.kt`), gating touch dispatch to the whole view tree so a tapjacking overlay can't tap through it — issue #507, see P6. Verified by `ScreenResilienceTest.mainActivityDecorViewFiltersTouchesWhenObscured` (`app/src/androidTest/.../security/ScreenResilienceTest.kt`). |

## V7 — Code Quality and Build Settings (MSTG-CODE)

No L2-only rows in this chapter.

| ID | Requirement (abbrev.) | Status | Evidence |
|---|---|---|---|
| CODE-1 | Signed with valid cert, private key protected | satisfied | Release signing via env/properties only, never committed; a partial set fails fast rather than producing a null-password APK (`app/build.gradle.kts:17-32`). |
| CODE-2 | Release-mode build, non-debuggable | satisfied | `isMinifyEnabled = true`, `isShrinkResources = true`, ProGuard rules for R8 (`app/build.gradle.kts:55-65`, `app/proguard-rules.pro`); release builds are non-debuggable by AGP default; `./gradlew assembleRelease` is gated in `.github/workflows/android-apk-build.yml` (signing secrets required, fail-fast) and `docker-publish.yml:225`. |
| CODE-3 | Debug symbols removed from native binaries | not-applicable | No native code (pure Kotlin/JVM); nothing to strip. |
| CODE-4 | No debug/backdoor code, no verbose logging | satisfied | HTTP logging is debug-build-only and `Level.BASIC` (`NetworkFactory.kt:35-41`); no hidden settings/backdoors; `detekt` in CI gates dead code and leftover debug paths (`android-tests.yml` → `./gradlew detekt`). |
| CODE-5 | Third-party components identified + vulnerability-checked | satisfied | CodeQL `java-kotlin` (`codeql.yml:123-175`), mobsfscan (`sast.yml`), Android Lint, `detekt`, and Dependabot's `gradle` ecosystem (`/android`, `.github/dependabot.yml`) + GitHub Dependency Review (`dependency-review.yml`). `filters.yaml` maps `android/**` to the Android + SAST + CodeQL jobs. |
| CODE-6 | Exceptions caught and handled | satisfied | Result/`fold` error handling across repositories and the OIDC flow (`MainActivity.kt:220-263`); `detekt`'s over-broad-exception rule in CI. |
| CODE-7 | Error handling denies access by default | satisfied | 401/404s clear the session rather than leaking data; failed profile fetch flips to logged-out (`MainActivity.kt:239-258`); no client-side fallback that reveals data on error. |
| CODE-8 | Unmanaged-code memory handled securely | not-applicable | No unmanaged/native code. |
| CODE-9 | Free toolchain security features enabled | satisfied | R8 byte-code minification + resource shrink (`app/build.gradle.kts:57-58`), ProGuard rules for reflection-using libs (`app/proguard-rules.pro`), and the platform's default hardening (NX, ASLR) apply to the managed app. |
