-- data_backfills (issue #485, I18N-02): completion ledger for one-shot
-- maintenance backfills that SQL cannot express.
--
-- The first (and, today, only) consumer is the Unicode NFC normalization of
-- stored contact text. This migration itself changes NO stored bytes: the
-- contacts.card/crm/passthrough columns are AES-GCM-encrypted at rest when
-- at-rest encryption is armed (a raw SQL UPDATE would corrupt them) and this
-- SQLite build has no Unicode normalization functions (see migration 000021's
-- KNOWN LIMITATION note), so the rewrite is a Go startup job
-- (services.NormalizeContactRecordsToNFC, wired into main.go beside
-- atrest.Backfill / models.RecomputeAuditChain) that consults this ledger to
-- run exactly once per database instead of re-scanning every contact on every
-- boot. MIG-03's semantic suite therefore sees a pure-DDL migration; the data
-- transform it enables is exercised by the Go backfill's own tests.
CREATE TABLE data_backfills (
    name         TEXT     PRIMARY KEY,
    version      INTEGER  NOT NULL,
    completed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
