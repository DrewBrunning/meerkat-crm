-- Rollback of 000054 (issue #873). A downgrade also reverts the binary to code
-- that does not track TOTP step reuse, so the only thing lost is the single-use
-- burn -- codes go back to being replayable within their ±1 step window.
-- totp_last_used_step holds no durable user content (a wall-clock-derived
-- counter), so dropping it loses nothing recoverable.
ALTER TABLE users DROP COLUMN totp_last_used_step;
