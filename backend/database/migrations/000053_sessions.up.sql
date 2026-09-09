-- Server-side session records (issue #866, pen-test #860 finding F-6 —
-- docs/adrs/0017-server-side-session-store.md).
--
-- Until this migration a session JWT was fully stateless: POST /logout only
-- cleared the auth_token cookie, so a copied/stolen token stayed valid for
-- its whole JWT_EXPIRY_HOURS (96 h default) window regardless of logout, and
-- the only real revocation was a users.token_version bump — which ends EVERY
-- session for that user at once (password change / reset / 2FA change),
-- far too broad for "log out this one device", and there was no idle timeout
-- at all.
--
-- Each interactive login (password, 2FA completion, OIDC callback, and the
-- device-grant -> session exchange) now also writes one row here, whose id is
-- carried in the JWT's `sid` claim. AuthMiddleware loads the row on every
-- request: a missing / revoked / absolutely-expired / idle-timed-out row is
-- rejected 401 the same way a stale token_version already was.
--
-- Lifecycle: operational bookkeeping, hard-delete only (CLAUDE.md backend
-- trap #7 — the row is re-derivable from a fresh login and a client just
-- re-authenticates rather than tracking its death). Revocation is recorded
-- with `revoked_at` (auditable, mirroring device_grants) rather than by
-- removing the row; a TTL purge job hard-deletes rows past `expires_at`
-- (plus a grace window) and long-revoked rows. Swept by DeleteUser's manual
-- cascade (CLAUDE.md backend trap #6) as well as the FK ON DELETE CASCADE.
--
-- `user_agent` / `ip` are captured only to render the "active sessions"
-- management list (GET /sessions); they are not used in any auth decision.

CREATE TABLE sessions (
    id           TEXT     PRIMARY KEY,       -- 32 random bytes, base64url; = JWT "sid" claim
    user_id      INTEGER  NOT NULL,
    created_at   DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,          -- advanced (throttled) on each authenticated request
    expires_at   DATETIME NOT NULL,          -- created_at + JWT_EXPIRY_HOURS (absolute ceiling)
    revoked_at   DATETIME,                   -- logout / password or 2FA change / manual revoke
    user_agent   TEXT     NOT NULL DEFAULT '',
    ip           TEXT     NOT NULL DEFAULT '',
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Scopes the GET /sessions list and the revoke-all path to one user.
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

-- Drives the TTL purge job.
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
