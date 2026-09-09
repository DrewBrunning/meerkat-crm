# ADR 0017: Server-side session records — per-device revocation and idle timeout

- **Status:** accepted
- **Date:** 2026-09-09
- **Depends on:** ADR 0004 (soft vs hard delete — `sessions` is the operational-row / hard-delete
  class), ADR 0005 (operational-event model — the purge job reports through the same `job_runs`
  surface)
- **Implements:** issue #866 (pen-test #860, Tester A finding F-6: "logout does not invalidate the
  JWT server-side").
- **Feeds:** ASVS 3.3.1 / 3.3.2 / 3.3.3 / 3.3.4 (`docs/security/asvs-l2.md`); the session-inventory
  UI (`frontend/src/components/SessionsSettings.tsx`).

## Context

A session was a bare HS256 JWT in an httpOnly cookie: claims `authorized, username, user_id,
token_version, exp`, nothing server-side. Consequences:

- **Logout was cosmetic.** `POST /logout` cleared the cookie; a copy of the token made beforehand
  kept working for its whole `JWT_EXPIRY_HOURS` window (96 h default). Pen-test #860 confirmed a
  replayed pre-logout token still returned `200` on `/users/me`.
- **The only real revocation was a `users.token_version` bump** (password change / reset, 2FA
  enable/disable/regenerate, `JWT_SECRET_KEY` rotation). That ends *every* session for the user at
  once — correct for "my account is compromised", far too broad for "log this one device out".
- **No idle timeout at all.** ASVS 3.3.2 was a written-down `partial`: absolute expiry only.
- **No way to see or manage devices** (ASVS 3.3.4 `partial`).

The project's standing position (verification report P2) is against premature abstraction — "an
abstraction with one implementation is dead weight" — and the stateless-JWT trade-off had been
deliberately accepted. #866 is the point where the trade-off stopped paying: three ASVS gaps and a
pen-test finding all trace to the same missing server-side handle on a session.

## Decision

### One `sessions` row per interactive login, named by a `sid` JWT claim

`migration 000053` adds `sessions(id TEXT PK, user_id, created_at, last_seen_at, expires_at,
revoked_at NULL, user_agent, ip)`. `services.IssueSession` — the single entry point every login path
now uses (password, 2FA completion, OIDC callback, device-grant exchange, post-password-change
re-issue) — writes the row and signs a JWT carrying its id in a new `sid` claim (`iat` is added too,
for display only). `AuthMiddleware`, after its existing credential and `token_version` checks, loads
the row and rejects the request when it is **missing, revoked, past `expires_at`, or idle** — the
same `401 "Session expired, please sign in again"` the `token_version` mismatch already returned.

### Idle timeout is `last_seen_at` + a config window

Each authenticated request advances `last_seen_at` (throttled to ~once per minute, fire-and-forget,
like `TouchAPIToken`). `SESSION_IDLE_TIMEOUT_HOURS` (default **12** — ASVS L2's guidance for this app
class) rejects a session unused for longer than the window, short of its absolute
`JWT_EXPIRY_HOURS` ceiling. `0` disables idle enforcement (operator opt-out for a purely
personal deployment); it is validated to `0 || 1..JWT_EXPIRY_HOURS`.

### Revocation is per-device, with a bulk path for the compromise case

- **Logout** revokes only the current `sid` (`RevokeSession`), so logging out a phone does not end a
  desktop session.
- **Password / 2FA change** calls `RevokeAllSessions(userID)` beside the existing `TokenVersion++`
  and `RevokeAllDeviceGrants`, so those actions still end every session — now at the row level, not
  only by version mismatch — and the caller's own row is re-created by the re-issue path.
- **`GET /sessions` / `DELETE /sessions/:id` / `DELETE /sessions`** (revoke-all-but-current) are the
  user-facing inventory (ASVS 3.3.4), scoped by `user_id` (no IDOR).

### Lifecycle: hard-delete, TTL-purged

`sessions` is the operational-row class (ADR 0004): no `deleted_at`, revocation recorded with
`revoked_at` for auditability, and a job-locked 6-hourly `PurgeExpiredSessions` cron hard-deletes
rows past `expires_at` or revoked more than 24 h ago. Swept by `DeleteUser`'s explicit enumeration
(`go-cascade-user`) and an `ON DELETE CASCADE` FK. Not in the Android offline mirror; no
CardDAV/CalDAV projection. Full lifecycle in `docs/security/data-retention-lifecycle.md` §23.

## Alternatives considered

- **Per-user "sessions valid after T" timestamp + `iat`.** No new table, but logout still ends every
  session for the user (the exact breadth problem #866 calls out) and it cannot express a true idle
  timeout — only "time since last full login". Rejected.
- **Just shorten `Max-Age`.** Trades the leak window against re-login friction for every user on
  every device; does not give per-device logout or an inventory. Rejected as the primary fix (the
  knob still exists as `JWT_EXPIRY_HOURS`).
- **Opaque session cookie, drop the JWT entirely.** Larger blast radius — every consumer of the
  `user_id` / `token_version` claims (CardDAV Basic-auth fallback aside) would change, and the
  Android device-grant flow re-mints JWTs by design. The `sid` claim gets the server-side handle
  with none of that churn.

## Consequences

- **One extra indexed read per authenticated request** (a `sessions` PK lookup), on the same SQLite
  connection pool as every other query — not a separate cache to fall out of sync (ASVS 1.11.2
  reworded accordingly). The PERF-02 query-count baselines exercise handlers below the router, so
  they are unaffected; if that changes, batch the session read with the existing `token_version`
  user read.
- **Tokens minted before `000053` carry no `sid`** and are rejected — one forced re-login on
  upgrade, exactly the treatment an unversioned `token_version` already gets.
- **Test infra**: `services.GenerateToken` gained a `sid` parameter; integration tests that mint a
  token for a real router now call `services.IssueSession` (which also writes the row). Middleware
  unit tests seed a `sessions` row and set a `sid` claim.
- **Android**: app JWTs now carry `sid` and are subject to the idle timeout; a device-grant exchange
  re-mints on the next `401`, so biometric resume still recovers an idled-out session. Long-lived
  test suites (`docker-compose.test.yml`, Playwright `global-setup`) should set
  `SESSION_IDLE_TIMEOUT_HOURS=0`.
