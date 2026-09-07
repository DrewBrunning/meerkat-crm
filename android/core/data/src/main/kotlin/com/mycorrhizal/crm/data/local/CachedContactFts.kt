package com.mycorrhizal.crm.data.local

import androidx.room.Entity
import androidx.room.Fts4
import androidx.room.FtsOptions

/**
 * FTS4 mirror of [CachedContact] for fast local full-text search over the
 * cached contact list (Phase 2 item 13). Room generates the external-content
 * triggers that keep it in sync with `cached_contacts` on every insert/update/
 * delete, so a search over cached rows is a single FTS MATCH query — much
 * faster than LIKE scans, and it works offline.
 *
 * FTS4 (not FTS5) is used because Room's `@Fts4(contentEntity=...)` keeps the
 * virtual table in lockstep with the content table automatically; the FTS5
 * equivalent requires manual trigger management.
 *
 * **I18N-02 (issue #485):** the tokenizer is `unicode61` (added in schema v18,
 * [MIGRATION_17_18]) so offline search agrees with the server's search — the
 * backend indexes with FTS5 `unicode61` (default `remove_diacritics`), which
 * is accent- and ASCII-case-insensitive for Latin text and folds Turkish
 * dotted-İ to `i`. Room's previous default, FTS4 `simple`, folds ASCII case
 * only, so an offline query for "garcia" could not find a cached "García" the
 * same server query returns online. `unicode61` also folds NFD/NFC the same
 * way the server does, which keeps the offline mirror consistent with the NFC
 * text the server now emits. The server's deliberate non-folds (German ß is
 * not ss, Greek all-caps is not lowered) are inherited unchanged; see
 * docs/development/unicode-search.md.
 */
@Fts4(contentEntity = CachedContact::class, tokenizer = FtsOptions.TOKENIZER_UNICODE61)
@Entity(tableName = "cached_contacts_fts")
data class CachedContactFts(
    val fn: String? = null,
    val firstname: String? = null,
    val lastname: String? = null,
    val primaryEmail: String? = null,
    val primaryPhone: String? = null,
    /** See [CachedContact.phonesNormalized]; T76 indexes this instead of the raw
     *  [primaryPhone] for phone-shaped queries, since punctuation splits the raw
     *  column into unmatchable FTS4 tokens. */
    val phonesNormalized: String? = null,
    val org: String? = null,
)
