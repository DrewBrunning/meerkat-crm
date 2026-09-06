-- device_grants (issue #722, "fully biometric login"): a long-lived,
-- revocable device grant minted for a specific install so that a successful
-- local biometric unlock can be exchanged (POST /auth/device/session) for a
-- fresh session JWT past the stored token's expiry. Follows api_tokens:
-- only the SHA-256 hash is stored, the row is soft-deleted (gorm.Model),
-- and grants are revoked with revoked_at rather than removed, so revocation
-- is auditable. Revoked/last-used are written by the auth paths; the
-- plaintext token is shown exactly once at creation and never stored.
CREATE TABLE device_grants (
    id           INTEGER  PRIMARY KEY AUTOINCREMENT,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at   DATETIME,
    user_id      INTEGER  NOT NULL,
    token_hash   TEXT     NOT NULL UNIQUE,
    label        TEXT     NOT NULL DEFAULT '',
    last_used_at DATETIME,
    revoked_at   DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_device_grants_user_id ON device_grants(user_id);
CREATE INDEX idx_device_grants_token_hash ON device_grants(token_hash);
