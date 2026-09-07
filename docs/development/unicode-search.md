# Unicode normalization and search behavior

Written-down expectations for how stored text and search treat Unicode (I18N-02, issue #485; the
load-bearing decision record is [ADR 0016](../adrs/0016-unicode-normalization-and-search-semantics.md)).
The rows below are **pinned by tests**, not discovered: `services/search_unicode_test.go`,
`controllers/contact_search_unicode_test.go`, `contactmodel/normalize_test.go`,
`models/apply_record_normalize_test.go`, `services/unicode_nfc_backfill_test.go`. Changing a row
means changing a test and the ADR, deliberately.

## Storage: everything is NFC

All contact text that crosses the write boundary (`models.ApplyRecordToContact` — REST create/update,
VCF/JSContact import, CardDAV put/reconcile, merge) is normalized to Unicode **NFC**
(`contactmodel.NormalizeRecord`, `backend/contactmodel/normalize.go`). NFD can no longer enter stored
bytes through any ingress path.

Scope of the normalization:

| Normalized to NFC | Preserved verbatim |
|---|---|
| name components, full names, sort-as | `card.uid` (sync identity) |
| nicknames | URIs (links, calendars, IMPP, media, members, relatedTo targets, geo) |
| organization / title / unit names | opaque Passthrough / Localizations bytes |
| email/phone/online-service labels | timestamps, language tags, closed vocabularies (kinds, contexts, media types) |
| address components, address full text, time zones | |
| card notes + authors, keywords, pronouns | |
| CRM envelope free text (`how_we_met`, work/contact info, gender) | |

Pre-existing NFD rows are rewritten once by the startup job `services.NormalizeContactRecordsToNFC`
(CLI: `go run ./cmd/backfill-unicode-nfc`), gated by migration `000052`'s `data_backfills` ledger.
Rows are rewritten through the Contact model, so the derived flat search columns and the `contacts_fts`
index follow via the normal save path — no separate FTS rebuild, asserted by `CheckSearchIndexConsistency`
in the backfill tests. Note content and activity text are **not** normalized on write; their NFD content
is still found because the tokenizer folds encodings (below).

## Search: what matches what

The search tokenizer is FTS5's default `unicode61` (no `tokenize=` clause anywhere in the migrations) —
deliberately unchanged. The behavior below is what this repo's pure-Go SQLite build actually does and
what the tests pin. Query terms are NFC-folded (`services.NormalizeSearchTerm`) before any arm runs, so
the byte-comparison arms (LIKE, 1-rune searches below the FTS gate, note/activity lists) see
storage-form bytes.

| Stored (NFC) | Typed | Found? | Why |
|---|---|---|---|
| `García` | `garcia`, `GARCÍA`, `García`, NFD `Garci` + combining acute + `a` | ✅ | unicode61 removes Latin diacritics; ASCII case folds; NFC/NFD tokenize identically |
| `Müller` | `muller`, `MÜLLER` | ✅ | same (Latin) |
| `İstanbul` (Turkish dotted capital I) | `istanbul`, `ISTANBUL` | ✅ | unicode61 folds U+0130 to `i` |
| `Istanbul` | `ıstanbul` (dotless ı) | ❌ | dotless `ı` (U+0131) stays a distinct token |
| `Straße` | `straße` | ✅ | ß is itself the token |
| `Straße` | `strasse`, `STRASSE` | ❌ | ß is **not** folded to `ss` — documented non-goal |
| `Κωνσταντίνος` | `κωνσταντίνος` | ✅ | accent removed on lowercase/titlecase |
| `Κωνσταντίνος` | `ΚΩΝΣΤΑΝΤΙΝΟΣ` | ❌ | Greek all-caps is not folded to lowercase — documented non-goal |
| `Екатерина` | `екатерина`, `ЕКАТЕРИНА` | ✅ | observed tokenizer behavior (pinned as-is, not a promise) |
| NFD note content | NFC query (`/search`) | ✅ | tokenizer folds encodings even though notes aren't stored-normalized |
| pre-backfill NFD row | NFC/NFD/ASCII query | ✅ | search never regressed during the backfill window |

Deliberate non-goals (documented decisions, not bugs):

- **German ß→ss folding is not provided.** There is no universal answer, and adding it changes search
  results — an ADR-level change.
- **Non-Latin all-caps folding is not promised.** What the tokenizer happens to do (Cyrillic folds;
  Greek all-caps does not) is pinned as observed behavior so it cannot silently drift, but it is not a
  product guarantee. Latin-script text is the guaranteed-insensitive set.
- **CJK segmentation** is unicode61's per-codepoint behavior; prefix matching starts at the head of a
  token run. No word segmentation is promised.

## Duplicates and merge

Duplicate detection and merge compare bytes (SQL `LOWER` keys; `kv == lv` scalar equality). They get
the same guarantee for free because storage is NFC: two spellings of one name in different encodings
are stored identically, so they pair and merge without a spurious conflict. `DetectDuplicate` also
folds its incoming parameters to NFC as a belt-and-braces measure for callers that skip the write
boundary. Case-folding in these paths remains SQLite's ASCII-only `lower()` (the documented `sort_name`
limitation) — an NFD/NFC pair is unified; an all-caps-`JOSÉ`-vs-`josé` pair is not, which is unchanged
behavior.

## Android

Online Android search is server-side (`GET /contacts?search=`), so it inherits this document. The
**offline** Room mirror (`CachedContactFts`, external-content FTS4) now uses the `unicode61`
tokenizer (Room schema v18, [Migration 17→18]) so offline search agrees with the server too: the
`unicode61` behavior pinned in the table above (Latin accent/case fold, Turkish dotted-İ→i, ß and
dotless-ı non-folds, Greek all-caps non-fold) is exactly what the server's FTS5 produces, and it is
pinned on-device by `CachedContactDaoTest` and `Migration17To18Test`. Server text is NFC, so cached
rows are NFC; `unicode61` folds NFD/NFC identically to the server, so no additional on-device
normalization is needed. Issue #479 (the offline mirror's schema) is closed by that version bump.

