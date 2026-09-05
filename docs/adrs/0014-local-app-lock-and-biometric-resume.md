# ADR 0014: Local app lock and biometric resume (Android)

- **Status:** accepted
- **Date:** 2026-09-05
- **Depends on:** issue #385 (SQLCipher Room mirror — the data this gate protects); #814 (the 2FA
  login flow this PR branches off, both touching the login/session surface); issue #678 (the 401 →
  `clearSession` wiring the expired-token path relies on).
- **Implements:** issue #722 (Android: local auth — biometric/PIN before the offline session-resume
  flow). Platform: Android. The evaluation write-up the issue asked for is this document's Context +
  Decision; the short version lives in `docs/security/masvs-l1.md` P7.

## Context

An Android testing pass asked: *"how does Android login work offline?"* The honest answer was that
there is no offline *login*, and the offline *resume* path had no local gate at all:

- On cold start, `DefaultSessionManager.init()` loads the JWT from `EncryptedTokenStorage` and sets
  `isLoggedIn = !token.isNullOrBlank()`. A persisted token dropped the user straight into the
  authenticated tree, backed by the Room offline mirror, with zero network calls **and zero local
  authentication**.
- A fresh username/password login always hits the network and simply fails when offline — there is no
  local credential verifier, and realistically cannot be one (no password hash is stored on device).

**The gap:** anyone holding the unlocked device got the user's entire contact database on app open,
with no biometric / PIN / passcode check. For a personal relationship OS holding real PII, that was
worth closing — and "biometric login" deserved to be a general feature, not an offline-only trick.

Three candidate designs were evaluated:

- **Option A — BiometricPrompt** (`androidx.biometric`, `BIOMETRIC_STRONG`, with device-credential
  fallback). No secret for the user to remember; it gates the already-encrypted stored session.
- **Option B — a user-defined app PIN**, stored in the same encrypted envelope. Works on devices
  without enrolled biometrics; an app-level secret independent of the device lock.
- **Device credential only** (no biometric library) — a `KeyguardManager` confirm intent.

### Recommendation, adopted: A, with the device credential as the fallback

- **A is the primary authenticator.** A Class 3 (`BIOMETRIC_STRONG`) biometric is both more secure and
  lower-friction than a PIN. The comparison happens in the OS against Keystore-authenticated
  templates; **no biometric sample ever reaches the app**, and the app stores no biometric material.
- **The device credential is the fallback**, not a stored app PIN. A device PIN/pattern/password is
  itself a strong secret the user already maintains (it is what protects the Keystore keys that wrap
  the stored JWT), so the app does not need to mint, store, and hash a *second* secret. This is the
  issue's "A with B (or device credential) as the fallback" — the device credential plays B's role for
  devices with a secure lock screen but no enrolled strong biometric.
- **An app-defined PIN is rejected for v1.** It is strictly weaker than the device credential for the
  same unlock job (a 4-8 digit secret stored under a key the same device protects, bypassable by
  someone who knows only the app PIN while the phone itself is unlocked), it adds a set/change/forgot
  surface, and it would be a *second* app-level secret the security docs would have to inventory. The
  one case it would genuinely serve — a device with **no** secure lock screen — is exactly the case
  the device can't be trusted to protect the session anyway; the opt-in is therefore disabled with an
  explanation rather than silently downgraded.
- **Weak (Class 2) biometrics alone are not offered** (issue wording: `BIOMETRIC_STRONG`). Class 2
  face/unlock sensors are less reliable and were the source of the "weak biometric" guidance; a device
  with only Class 2 still has the device credential in the allowed set, so it is never locked out.

## Decision

1. **Opt-in, default off.** Settings → a new "App lock" section: "Require biometric or device PIN to
   open the app", persisted in the `local_auth_prefs` DataStore file. **Default off means existing
   installs cold-start exactly as today** until the user explicitly opts in (issue scope item 3). The
   toggle is disabled — with the reason — on a device that cannot satisfy the gate (no strong
   biometric and no secure lock screen), so it can never be enabled without a way to open it.

2. **Grace period.** Alongside the toggle, "Lock automatically after": immediately / 1 / 5 / 15 /
   60 minutes (default 5). The app locks only after it has been backgrounded **longer than the grace
   period**, so a quick app-switch never re-prompts ("not on every resume"); `IMMEDIATELY` is the
   zero-grace option for users who want every return to prompt.

3. **When the gate shows.** A session that must pass the local check is gated in two situations:
   - **Cold start with a persisted session** — a freshly-hydrated token is a *resume*, and the opt-in
     says "check me again before showing my data". There is no way to distinguish a legitimate process
     restart from the phone being picked up by someone else, so cold starts always gate when the
     setting is on.
   - **Foreground after a background past the grace period.**
   A fresh interactive login (password / API token / 2FA / OIDC) is **never** gated: the user just
   authenticated, and a login necessarily starts from the logged-out (ungated) tree.

4. **The gate is a separate surface, not the login screen.** The root now branches on
   `session.isLoggedIn` *and* the app-lock state, and the authenticated tree is only composed once the
   gate has cleared. Cancelling the OS prompt keeps the gate up — the session's data is never composed
   underneath it. The only ways off are authenticating or "Log out". This is deliberately the same
   surface whether the resume happens online or offline (the check itself makes no network call), which
   is what makes "biometric login in place of username/password" a general feature rather than an
   offline special case.

5. **Logout and expiry semantics** (issue scope item 6). An explicit logout keeps the opt-in
   preference (it is not cleared) and clears the gate; session expiry (the existing 401 →
   `clearSession` wiring, issue #678) also clears the gate so the next login lands on the main tree,
   never on a stale lock. A session that is `Locked` is only ever cleared by a successful local auth
   or by the session ending.

6. **Expired-token UX** (issue scope item 5). Biometrics gate access to the *stored* JWT; they cannot
   mint a new one offline. If that JWT has expired, an unlock lands on the authenticated tree and the
   first API call 401s → `clearSession` → the login screen, cleanly and with no blank/error state —
   the same path issue #678 already hardened. No dead end: the user types their password (and, for a
   2FA account, a fresh TOTP code).

7. **No backend change.** The backend is pure stateless JWT with no refresh tokens, no blocklist and
   no server-side device registry; the only revocation is `users.token_version`, bumped on password
   change/reset and 2FA toggles. A stored JWT stays valid until `exp` (~96 h default) or a bump, so a
   biometric resume of the existing stored session needs no server machinery. There is deliberately
   **no long-lived "remember this device" grant** that would let a biometric unlock mint a fresh token
   past `exp`; that would mean server-side refresh tokens / per-device credentials — a real design
   change with a real server-side secret, explicitly out of scope for this issue (see Consequences).

## Scope → implementation map

| Issue scope item | Where it landed |
|---|---|
| 1. Evaluation + decision | This ADR + `docs/security/masvs-l1.md` P7 |
| 2. Local gate on the authenticated tree | `DefaultAppLockController` (state machine), `MainViewModel` + `MycorrhizalApp` root branch |
| 3. Opt-in setting, encrypted-preferences-note, default off | `LocalAuthSettingsRepository(Impl)` (plain DataStore — a preference, not a credential; the secret it gates stays in `EncryptedTokenStorage`) + Settings UI |
| 4. Biometric login as a general resume | The app-lock surface (works online and offline); reachable on every cold start / grace-timeout resume |
| 5. Expired-token UX | Existing 401 → `clearSession` (issue #678); documented in P7 |
| 6. `clearSession` / `SessionExpiryInterceptor` interaction | Controller transitions pinned by `DefaultAppLockControllerTest` |
| 7. Security docs | `masvs-l1.md` P7, `asvs-l2.md` P7, `data-retention-lifecycle.md` §8 note |
| 8. Tests | Controller state machine, settings VM + screen, root branch, lock VM + screen, prompt-posture guard |

## Consequences

- **Positive:** a lost/stolen-unlocked-device attacker (the threat-model actor this closes) must now
  defeat the device's own biometric or PIN before the contact database renders; offline resume keeps
  working; default-off preserves today's behavior for every existing install.
- **Neutral:** the biometric prompt is the OS dialog, not an in-app view; the app cannot style it, and
  its exact look varies by OEM/Android version. Robolectric cannot drive a real `BiometricPrompt`, so
  the OS call is a thin, guard-tested seam and the gate logic around it is fully unit-tested.
- **Negative / out of scope:** no app-level PIN (device credential only — see Context); no refresh
  tokens / remembered-device grant (a biometric unlock cannot mint a token past `exp`; the user
  re-enters the password). Weak (Class 2) biometrics are not offered on their own. A biometric-prompt
  that is cancelled on a device whose credential was just removed leaves only "Log out".
