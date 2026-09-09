-- Rollback of 000053. A downgrade also reverts the binary to code that does
-- not read the JWT `sid` claim, so outstanding session tokens keep working
-- under the stateless token_version + absolute-expiry checks alone; only the
-- server-side revocation added by #866 (per-device logout, idle timeout,
-- the /sessions management list) is lost.

DROP TABLE IF EXISTS sessions;
