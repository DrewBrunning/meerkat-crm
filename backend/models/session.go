package models

import "time"

// Session is the server-side record of one interactive login (issue #866 —
// docs/adrs/0017-server-side-session-store.md). Its ID is carried in the JWT's
// `sid` claim and checked by AuthMiddleware on every request, so a session can
// be ended server-side — on logout (this device only), on an idle timeout, or
// in bulk on a password / 2FA change — instead of a stolen token staying valid
// for its whole absolute expiry.
//
// Hard-delete entity (CLAUDE.md backend trap #7): operational bookkeeping,
// re-derivable from a fresh login, no natural undo value — so no DeletedAt.
// Revocation is recorded with RevokedAt (auditable, mirrors DeviceGrant); the
// TTL purge job then hard-deletes expired and long-revoked rows.
//
// Explicit column tags because GORM's name derivation disagrees with the
// hand-written migration for the acronym-ish fields (trap #1): it would map
// UserAgent -> user_agent (matches) but IP -> "ip" only by luck of the
// singular; pin every column so a rename in one place can't silently drift.
type Session struct {
	ID         string     `gorm:"column:id;primaryKey" json:"id"`
	UserID     uint       `gorm:"column:user_id;not null;index" json:"-"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	LastSeenAt time.Time  `gorm:"column:last_seen_at;not null" json:"last_seen_at"`
	ExpiresAt  time.Time  `gorm:"column:expires_at;not null" json:"expires_at"`
	RevokedAt  *time.Time `gorm:"column:revoked_at" json:"-"`
	UserAgent  string     `gorm:"column:user_agent;not null;default:''" json:"user_agent"`
	IP         string     `gorm:"column:ip;not null;default:''" json:"ip"`
}

// SessionResponse is the active-session shape returned by GET /sessions. The
// opaque id is included so the client can target DELETE /sessions/:id; it is
// the same value that lives in a JWT's `sid` claim and is not otherwise
// sensitive (a stolen id is useless without the signed token).
type SessionResponse struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	// Current is true for the session making the request, so the UI can label
	// "This device" and steer the user away from revoking it by accident.
	Current bool `json:"current"`
}
