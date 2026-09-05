-- Rollback of 000051. Device grants are revocable credentials bound to a
-- user's install; dropping the table ends every outstanding grant (a device
-- must re-enroll after a downgrade).

DROP TABLE IF EXISTS device_grants;
