package services

import (
	"fmt"
	"mycorrhizal/internal/faults"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// faultSearchRebuild is the failure-injection seam (issue #434 / #498) for the
// FTS rebuild transaction. Armed via the faults package, it fails the rebuild
// inside the single transaction (before the DELETEs), so the rollback leaves
// the previously-good index untouched — the injection test asserts search
// still works and the index is still consistent afterward.
// See docs/development/fault-injection.md.
const faultSearchRebuild = "services.search.rebuild"

// Full-text search over contacts, notes, and interactions (T11,
// T11; addresses added by T38,
// T38; phones added by T69,
// T69). Backed by FTS5
// virtual tables kept in sync by triggers (migrations 000007 + 000010 +
// 000020); the index is derived data and can be rebuilt at any time via
// RebuildSearchIndex.
//
// Contact addresses are searchable through the denormalized
// contacts.addresses_flat column (maintained by Contact.BeforeSave and
// backfilled by migration 000010), indexed into contacts_fts like the other
// flat fields — the ticket's lowest-friction option, consistent with how the
// rest of the index works.
//
// Contact phones are searchable through the denormalized
// contacts.phones_normalized column (maintained by Contact.BeforeSave from
// every Phones[] entry and backfilled by migration 000020), indexed into
// contacts_fts as each number's full digit string plus its PhoneKey. A
// phone-shaped query (PhoneQueryTokens) is matched against the same two
// normalized forms, so a query typed with different punctuation, grouping, or
// country code than the stored value still finds the contact.
//
// Scoping: each FTS row carries user_id (UNINDEXED), and every query filters
// on it — the highest-risk correctness rule of the ticket, pinned by
// TestSearch_CrossUserReturnsNothing and TestSearch_CrossUserAddressDoesNotLeak.
// The base-table JOIN also re-scopes and re-filters soft-deleted rows
// (defense-in-depth on top of the triggers).

// SearchResult is the grouped response: matched contacts, notes, and
// interactions (activities). Query and any resolved relation synonym are
// echoed so a client can show "matched relationship: sibling".
type SearchResult struct {
	Query            string              `json:"query"`
	ResolvedRelation string              `json:"resolved_relation,omitempty"`
	Contacts         []SearchContactHit  `json:"contacts"`
	Notes            []SearchNoteHit     `json:"notes"`
	Activities       []SearchActivityHit `json:"activities"`
}

// SearchContactHit is a matched contact (the slim ContactSummary shape) plus
// a snippet of the matched field.
type SearchContactHit struct {
	models.ContactSummary
	Snippet string `json:"snippet,omitempty"`
}

// SearchNoteHit is a matched note with its owning contact's name when one is
// attached, plus a snippet of the matched content.
type SearchNoteHit struct {
	models.Note
	ContactName string `json:"contact_name,omitempty"`
	Snippet     string `json:"snippet"`
}

// SearchActivityHit is a matched interaction (activity) plus a snippet.
type SearchActivityHit struct {
	models.Activity
	Snippet string `json:"snippet"`
}

const (
	searchDefaultLimit = 10
	searchMaxLimit     = 50
)

// MaxSearchTermLen bounds a free-text search term (issue #415). Both the
// /search handler and GET /contacts?search= enforce it: an unbounded term
// would push an arbitrarily long FTS5 MATCH / LIKE clause (with a %...% wrap
// for the LIKE paths) at the query planner per request, and a 1MB "term" is
// not a search, it is a request for CPU. Length in runes, not bytes, so a
// multibyte term is judged by the number of characters the user actually
// typed.
const MaxSearchTermLen = 256

// NormalizeSearchTerm canonicalizes a query term to the same Unicode NFC
// form every stored contact name is written in (contactmodel.NormalizeRecord,
// issue #485 / I18N-02) before it reaches any comparison arm. Stored data is
// NFC, so an NFC query term makes the LIKE-only arms (byte comparisons), the
// 1-rune searches below the FTS gate, and the FTS5 tokenizer all see
// comparable bytes no matter which encoding the client sent. The FTS tokenizer
// would itself fold NFD/NFC, but the LIKE arms would not — this is the query
// half of the "normalize at the same boundary as the write" rule. It is an
// identity on the common (already-NFC) term.
func NormalizeSearchTerm(term string) string {
	return norm.NFC.String(term)
}

// ResolveSearchSynonym reports whether the whole search term resolves to a
// relation token through the type registry ("mom"/"mother" → parent_of,
// "brother" → sibling_of) — T11's synonym consumer, also used by the graph
// traversal relation filter (services/graph_traversal.go).
func ResolveSearchSynonym(term string) (string, bool) {
	return models.MatchLegacyRelationType(term)
}

// ftsQuery turns a free-text term into a safe FTS5 MATCH expression: each
// whitespace-separated token is quoted (escaping embedded quotes) and given a
// prefix-match suffix, joined by implicit AND. This prevents FTS5 syntax
// errors from user input (quotes, operators) while still prefix-matching
// ("alice" finds "alicia").
func ftsQuery(term string) string {
	tokens := strings.Fields(term)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"*`)
	}
	return strings.Join(quoted, " ")
}

// PhoneQueryTokens normalizes a phone-shaped query term. It returns the term's
// full digit string (models.NormalizePhoneDigits) and its PhoneKey, and
// reports whether the term is phone-shaped at all: mostly digits, with every
// non-digit character drawn from the ones phone numbers are written with —
// + ( ) -. / and whitespace. A term like "alice" or "800 flowers" is not
// phone-shaped, so ordinary text search is untouched. The two-token shape
// mirrors the phones_normalized index column (T69): a query of the full digits
// ("18005551234") or of the canonical key ("8005551234") both resolve.
func PhoneQueryTokens(term string) (digits, key string, ok bool) {
	digits = models.NormalizePhoneDigits(term)
	if digits == "" {
		return "", "", false
	}
	for _, r := range term {
		if r >= '0' && r <= '9' || strings.ContainsRune("+()-./ \t", r) {
			continue
		}
		return "", "", false
	}
	return digits, models.PhoneKey(term), true
}

// phoneFTSMatch builds the FTS5 MATCH expression for a phone-shaped query: an
// OR of prefix-matches on the query's normalized digit string and its PhoneKey
// (deduped when they coincide). Because stored numbers are indexed under both
// tokens (see models.FlattenPhones), a query written in any format finds a
// contact stored in any other format.
func phoneFTSMatch(digits, key string) string {
	tokens := []string{`"` + digits + `"*`}
	if key != "" && key != digits {
		tokens = append(tokens, `"`+key+`"*`)
	}
	return strings.Join(tokens, " OR ")
}

// ContactFTSMatch builds the FTS5 MATCH expression to run against
// contacts_fts for a free-text query term: the phone-shaped expression
// (phoneFTSMatch) when the term is phone-shaped (PhoneQueryTokens), the plain
// prefix expression (ftsQuery) otherwise. Reports ok=false when the term
// tokenizes to nothing (e.g. all-whitespace) — callers must not run a MATCH
// with an empty expression (FTS5 syntax error).
//
// Both Search (this file, the cross-entity /search endpoint) and
// applyContactSearch (controllers/contact_controller.go, GET
// /contacts?search=, T85 — T85)
// call this, so the two paths' notion of "what a contacts_fts match looks
// like" cannot drift apart the way it would if each reimplemented the
// phone-vs-plain choice separately.
func ContactFTSMatch(term string) (expr string, ok bool) {
	if digits, key, phoneOK := PhoneQueryTokens(term); phoneOK {
		return phoneFTSMatch(digits, key), true
	}
	expr = ftsQuery(term)
	return expr, expr != ""
}

// Search runs a full-text query across the user's contacts, notes, and
// interactions. Empty/whitespace queries return an empty result. A query
// shorter than two characters also returns empty (avoiding a noisy index
// scan for one-character terms, matching the frontend's debounce gate).
//
// When householdID is non-nil, contact results are restricted to members of
// that household ("everyone in the Smith household", T11's household-scoped
// half) — the caller is responsible for verifying the household belongs to
// the user.
func Search(db *gorm.DB, userID uint, term string, limit int, householdID *string) (*SearchResult, error) {
	result := &SearchResult{
		Query:      strings.TrimSpace(term),
		Contacts:   []SearchContactHit{},
		Notes:      []SearchNoteHit{},
		Activities: []SearchActivityHit{},
	}
	term = strings.TrimSpace(term)
	// I18N-02: fold the query to NFC (the storage form) before the rune gate,
	// the synonym lookup, and every FTS/LIKE arm below, so a query typed in a
	// different encoding than the stored name still compares byte-equal in the
	// arms that need it. The echo keeps the folded form so clients never see
	// the term differ from what was matched.
	term = NormalizeSearchTerm(term)
	result.Query = term
	if term == "" || len([]rune(term)) < 2 {
		return result, nil
	}

	if limit < 1 {
		limit = searchDefaultLimit
	}
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}

	// Synonym resolution: if the whole term is a relation synonym, echo the
	// canonical token so clients can offer relationship-aware results (the
	// graph traversal relation filter is where it actually filters).
	if token, ok := ResolveSearchSynonym(term); ok {
		result.ResolvedRelation = token
	}

	match := ftsQuery(term)

	// T69: a phone-shaped term is matched against the normalized digit/key
	// tokens of phones_normalized rather than through the raw-tokenizer path
	// (which splits "(800) 555-1234" into three tokens that "8005551234"
	// never prefix-matches). Only the contacts index carries the normalized
	// column, so only the contacts query gets the phone-shaped expression;
	// notes and activities keep the plain text match (their raw fields are
	// not normalized). ContactFTSMatch encapsulates the phone-vs-plain choice
	// (T85) so this and applyContactSearch cannot drift.
	//
	// The ok result is deliberately discarded: it is false only for a term
	// that tokenizes to nothing, which the >= 2-rune gate above already
	// excludes, and in that case the returned expression and `match` are
	// both the empty string anyway — identical to pre-T85 behavior. There is
	// no meaningful fallback to make here; applyContactSearch, which has no
	// such upstream guarantee, is the caller that actually checks ok.
	contactMatch, _ := ContactFTSMatch(term)

	// Optional household scope: restrict contact hits to members of one
	// household (T11's "everyone in the Smith household").
	householdClause := ""
	householdArgs := []interface{}{}
	if householdID != nil {
		householdClause = " AND c.vcard_uid IN (SELECT member_vcard_uid FROM household_members WHERE household_id = ? AND user_id = ?)"
		householdArgs = append(householdArgs, *householdID, userID)
	}

	// Contacts: FTS hit → JOIN the live row (re-scoping + re-filtering
	// soft-deletes defensively), build the slim summary shape.
	var contactHits []struct {
		models.Contact
		Snippet string
	}
	contactArgs := append([]interface{}{contactMatch, userID, userID}, householdArgs...)
	contactArgs = append(contactArgs, limit)
	err := db.Raw(`
		SELECT c.*, snippet(contacts_fts, 0, '…', '…', '…', 20) AS snippet
		FROM contacts_fts
		JOIN contacts c ON c.id = contacts_fts.rowid
		WHERE contacts_fts MATCH ?
		  AND contacts_fts.user_id = ?
		  AND c.user_id = ?
		  AND c.deleted_at IS NULL`+householdClause+`
		ORDER BY contacts_fts.rank
		LIMIT ?`,
		contactArgs...,
	).Scan(&contactHits).Error
	if err != nil {
		return nil, fmt.Errorf("search contacts: %w", err)
	}
	for i := range contactHits {
		hit := SearchContactHit{
			ContactSummary: models.NewContactSummary(&contactHits[i].Contact),
			Snippet:        contactHits[i].Snippet,
		}
		result.Contacts = append(result.Contacts, hit)
	}

	// Notes: FTS hit → JOIN the live note + its contact name (direct FK).
	var noteHits []struct {
		models.Note
		ContactName string
		Snippet     string
	}
	err = db.Raw(`
		SELECT n.*, COALESCE(c.firstname || ' ' || c.lastname, '') AS contact_name,
		       snippet(notes_fts, 0, '…', '…', '…', 20) AS snippet
		FROM notes_fts
		JOIN notes n ON n.id = notes_fts.rowid
		LEFT JOIN contacts c ON c.id = n.contact_id
		WHERE notes_fts MATCH ?
		  AND notes_fts.user_id = ?
		  AND n.user_id = ?
		  AND n.deleted_at IS NULL
		ORDER BY notes_fts.rank
		LIMIT ?`,
		match, userID, userID, limit,
	).Scan(&noteHits).Error
	if err != nil {
		return nil, fmt.Errorf("search notes: %w", err)
	}
	for i := range noteHits {
		hit := SearchNoteHit{
			Note:        noteHits[i].Note,
			ContactName: strings.TrimSpace(noteHits[i].ContactName),
			Snippet:     noteHits[i].Snippet,
		}
		result.Notes = append(result.Notes, hit)
	}

	// Activities (interactions): FTS hit → JOIN the live activity.
	var activityHits []struct {
		models.Activity
		Snippet string
	}
	err = db.Raw(`
		SELECT a.*, snippet(activities_fts, 0, '…', '…', '…', 20) AS snippet
		FROM activities_fts
		JOIN activities a ON a.id = activities_fts.rowid
		WHERE activities_fts MATCH ?
		  AND activities_fts.user_id = ?
		  AND a.user_id = ?
		  AND a.deleted_at IS NULL
		ORDER BY activities_fts.rank
		LIMIT ?`,
		match, userID, userID, limit,
	).Scan(&activityHits).Error
	if err != nil {
		return nil, fmt.Errorf("search activities: %w", err)
	}
	for i := range activityHits {
		hit := SearchActivityHit{
			Activity: activityHits[i].Activity,
			Snippet:  activityHits[i].Snippet,
		}
		result.Activities = append(result.Activities, hit)
	}

	return result, nil
}

// SearchIndexRebuildStats reports how many live rows were written to each FTS
// virtual table by a rebuild — one number per index (SEARCH-01, issue #461
// recommended actions 3 and 6). All three indexes are always rebuilt
// together, so a zero here means "no live rows of that kind", never "this
// index was skipped".
type SearchIndexRebuildStats struct {
	Contacts   int64 `json:"contacts"`
	Notes      int64 `json:"notes"`
	Activities int64 `json:"activities"`
}

// Total is the row count across all three indexes — the single number the
// job-run record uses for items_processed.
func (s SearchIndexRebuildStats) Total() int64 {
	return s.Contacts + s.Notes + s.Activities
}

// searchIndexRebuildMu serialises RebuildSearchIndexExclusive within a single
// process (issue #461 recommended action 4). SQLite already serialises the
// rebuild transaction against every other writer — a second instance's
// rebuild included — via _txlock=immediate, so concurrent rebuilds cannot
// corrupt the index; this mutex only spares the wasted work and lock
// contention of a second full rebuild racing the first in the same process
// (an operator double-submitting POST /admin/search/rebuild, or a
// post-restore rebuild overlapping a manual one). acquireJobLock (the
// scheduled-job primitive) is deliberately not used: this operation has no
// schedule and therefore no catch-up window, and its "ran too recently" /
// "caught_up" semantics would mislabel a deliberate back-to-back re-run.
var searchIndexRebuildMu sync.Mutex

// RebuildSearchIndex rebuilds the three FTS5 virtual tables from the live base
// tables. It is the low-level primitive kept for existing callers; prefer
// RebuildSearchIndexReport when you want the per-index row counts, or
// RebuildSearchIndexExclusive from an operator-facing entry point (it adds
// the in-process concurrency guard).
func RebuildSearchIndex(db *gorm.DB) error {
	_, err := RebuildSearchIndexReport(db)
	return err
}

// RebuildSearchIndexReport truncates the three FTS virtual tables and
// re-inserts every live (deleted_at IS NULL) row from contacts, notes, and
// activities — including the denormalized contacts.addresses_flat address
// text and contacts.phones_normalized phone tokens — returning the number of
// rows written to each index.
//
// Idempotent and re-runnable: the index is derived data, so a rebuild always
// converges on exactly what the maintenance triggers (migrations
// 000007/000010/000020) would have produced. Run it after any write that
// bypassed those triggers — a bulk import, a hand-written raw-SQL migration
// that touched a base table, a restore from backup — or to make pre-existing
// contacts' addresses/phones searchable after migrations 000010/000020.
//
// Interruption safety (issue #461 recommended action 2): the whole rebuild —
// all three DELETEs and all three INSERTs — runs in ONE transaction. A crash,
// a cancelled request, or a SQL error rolls it back, leaving the
// previously-good index in place untouched; on COMMIT, WAL switches every
// reader to the new index atomically. There is no window in which search sees
// a half-populated index, and search is never blocked by a rebuild (readers
// keep using the pre-rebuild snapshot until COMMIT). The cost is that the
// rebuild holds SQLite's single write lock for its whole duration, so
// ordinary writes queue behind it — acceptable at the MVP scale this project
// targets (docs/operations/search-index.md).
func RebuildSearchIndexReport(db *gorm.DB) (SearchIndexRebuildStats, error) {
	var stats SearchIndexRebuildStats
	start := time.Now()

	err := db.Transaction(func(tx *gorm.DB) error {
		// Issue #434/#498 failure-injection seam: an armed fault fails the
		// rebuild inside the transaction, so the rollback leaves the
		// previously-good index in place. Unarmed this is a nil return.
		if err := faults.Hook(faultSearchRebuild); err != nil {
			return err
		}

		for _, stmt := range []string{
			"DELETE FROM contacts_fts",
			"DELETE FROM notes_fts",
			"DELETE FROM activities_fts",
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("rebuild search index (clear): %w", err)
			}
		}

		steps := []struct {
			index string
			count *int64
			stmt  string
		}{
			{"contacts_fts", &stats.Contacts, `INSERT INTO contacts_fts(rowid, user_id, firstname, lastname, nickname, email, phone, org, addresses_flat, phones_normalized)
			 SELECT id, user_id, firstname, lastname, nickname, email, phone, org, addresses_flat, phones_normalized
			 FROM contacts WHERE deleted_at IS NULL`},
			{"notes_fts", &stats.Notes, `INSERT INTO notes_fts(rowid, user_id, content)
			 SELECT id, user_id, content
			 FROM notes WHERE deleted_at IS NULL`},
			{"activities_fts", &stats.Activities, `INSERT INTO activities_fts(rowid, user_id, title, description, location)
			 SELECT id, user_id, title, description, location
			 FROM activities WHERE deleted_at IS NULL`},
		}
		for _, s := range steps {
			res := tx.Exec(s.stmt)
			if res.Error != nil {
				return fmt.Errorf("rebuild search index (%s): %w", s.index, res.Error)
			}
			*s.count = res.RowsAffected
			// Per-index progress (issue #461 recommended action 3): a rebuild
			// over a large corpus should not be a silent long write.
			logger.Info().
				Str("index", s.index).
				Int64("rows", res.RowsAffected).
				Msg("search index table rebuilt")
		}
		return nil
	})
	if err != nil {
		return SearchIndexRebuildStats{}, err
	}

	logger.Info().
		Int64("contacts", stats.Contacts).
		Int64("notes", stats.Notes).
		Int64("activities", stats.Activities).
		Int64("duration_ms", time.Since(start).Milliseconds()).
		Msg("search index rebuilt")
	return stats, nil
}

// RebuildSearchIndexExclusive is RebuildSearchIndexReport behind the
// in-process concurrency guard (searchIndexRebuildMu). It returns
// ErrJobSkipped without touching the index when another rebuild is already
// running in this process, so an operator-facing caller reports "already in
// progress" instead of queueing a redundant full rebuild.
func RebuildSearchIndexExclusive(db *gorm.DB) (SearchIndexRebuildStats, error) {
	if !searchIndexRebuildMu.TryLock() {
		return SearchIndexRebuildStats{}, ErrJobSkipped
	}
	defer searchIndexRebuildMu.Unlock()
	return RebuildSearchIndexReport(db)
}
