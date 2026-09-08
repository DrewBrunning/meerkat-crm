# ADR 0016: Unicode normalization and search semantics

- **Status:** accepted
- **Date:** 2026-09-07
- **Implements:** issue #485 (I18N-02, the `v0.6.11` Unicode search milestone).
- **Feeds:** #461/#462 (FTS rebuild/consistency), #479 (Android offline mirror), #541 (the `v0.6.11`
  milestone gate), duplicate detection/merge (#433/#442/#515). Related: #484 (I18N fixtures),
  #438 (migration semantics for the backfill), #415 (search-term length bound).

## Context

There was no Unicode normalization anywhere in the backend, and no written statement of what search
does with non-ASCII text. "José" can be stored two ways — precomposed `é` (U+00E9, NFC) or `e` plus a
combining acute (U+0301, NFD). macOS and iOS emit NFD on some paths; almost everything else emits NFC.
The two render identically, and several byte-sensitive consumers cannot reconcile them:

- duplicate detection groups on `LOWER(firstname), LOWER(lastname)` — byte keys;
- the contacts-list LIKE arms, the sort key derivation, and CardDAV byte comparison are byte
  comparisons;
- the Android offline mirror tokenizes with FTS4 `simple` (ASCII-only case folding, no diacritic
  folding, byte-faithful).

Meanwhile the FTS5 search index (default `unicode61` tokenizer) was empirically *more* capable than
the issue assumed: it removes diacritics from precomposed Latin letters, folds ASCII case, folds
combining-mark sequences into separators, and folds Turkish dotted-İ to `i`. So the FTS arms mostly
"worked" for Latin by accident — while every consumer *around* them stayed encoding-sensitive and the
stored bytes stayed whatever the client sent.

## Decision

1. **NFC is the single stored form for contact text.** Every Record that crosses the write boundary
   (`models.ApplyRecordToContact`) is normalized to NFC first (`contactmodel.NormalizeRecord`,
   `backend/contactmodel/normalize.go`). All ingress paths — REST create/update, VCF/JSContact import,
   CardDAV put/reconcile, merge — funnel through that one function, so NFD can no longer enter.
2. **Queries are folded to NFC at the same boundary** (`services.NormalizeSearchTerm`), so every arm —
   FTS, the LIKE byte-comparison arms, the 1-rune searches below the FTS gate — sees storage-form
   bytes. Duplicate detection folds its incoming parameters too, so a caller that has not gone through
   the write boundary still matches an NFC-stored name.
3. **Pre-existing rows are backfilled exactly once** by an idempotent Go startup job
   (`services.NormalizeContactRecordsToNFC`), because SQL cannot express NFC and the `card`/`crm`
   columns are AES-GCM-encrypted when at-rest encryption is armed. Migration `000052` provides only the
   `data_backfills` completion ledger; the data transform is Go, in the same "migration provides
   schema, Go provides the transform" shape as `atrest.Backfill` and `models.RecomputeAuditChain`.
   Rows are rewritten through the model, so the FTS triggers keep the index consistent — no separate
   rebuild.
4. **The FTS5 `unicode61` default is the search tokenizer, deliberately unchanged.** Its empirically
   observed behavior — Latin accent- and case-insensitive across NFC/NFD, Turkish dotted-İ folded to
   `i`, no German ß→ss folding — is *documented and pinned by tests*
   (`docs/development/unicode-search.md`), not silently relied on. NFC storage is what makes the
   non-FTS consumers agree with the index.
5. **Normalization scope is display text only.** UIDs, URIs, opaque Passthrough/Localizations bytes,
   timestamps, language tags, and closed-vocabulary tokens are preserved verbatim. Normalizing a UID
   would orphan a contact's `vcard_uid` and break sync identity.

## Consequences

- Stored contact text is canonical, so duplicate detection, sort keys, the LIKE search arms, exports,
  and the Android online search results all agree regardless of which device produced the bytes.
- One-time backfill writes revision/etag bumps and audit rows only for rows that were actually NFD —
  the CardDAV consequence is that remote cards converge to NFC on the next sync, which is the point.
- German ß→ss and non-Latin all-caps folding are deliberately NOT provided; they are documented
  non-goals with pinned negative tests. Adding them later (e.g. a custom tokenizer or `ß` folding)
  is an ADR-level change because it alters search results.
- The Android offline Room mirror (`CachedContactFts`) is switched to the `unicode61` tokenizer
  (schema v18) so offline results match the server's, with the same documented non-folds.
