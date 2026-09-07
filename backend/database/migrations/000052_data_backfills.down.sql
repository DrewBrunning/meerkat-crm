-- Rolling back 000052 drops the completion ledger. It holds no user data —
-- only the (name, version) markers of one-shot maintenance backfills — so
-- dropping it is safe; the NFC backfill it gates would simply run again the
-- next time a caller executes it (NormalizeContactRecordsToNFC is idempotent,
-- so re-running after a rollback is a no-op over already-NFC rows).
DROP TABLE IF EXISTS data_backfills;
