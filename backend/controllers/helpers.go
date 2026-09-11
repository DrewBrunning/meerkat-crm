package controllers

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mycorrhizal/config"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/models"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultPage  = 1
	defaultLimit = 25
	maxLimit     = 100
)

// PaginationParams represents sanitized pagination query values.
type PaginationParams struct {
	Page   int
	Limit  int
	Offset int
}

// requirePathUintID validates that a numeric-PK path parameter (:id,
// :repo_id, ...) is a well-formed non-negative integer before it reaches a
// GORM lookup. Handlers pass c.Param(...) straight into db.First(&x, id); a
// non-integer or out-of-range value (Schemathesis fuzzes things like
// "null,null", huge integers, and negative numbers) makes the SQLite driver
// return a non-ErrRecordNotFound error, which the existing
// ErrRecordNotFound-vs-else branch turns into a 500 instead of the correct
// 400 (issue #524). On a malformed value this aborts the request with 400
// and returns ok=false; callers must check ok and return immediately. On
// success it returns the raw path value unchanged, so existing
// `db.First(&x, id)` call sites keep working exactly as before.
func requirePathUintID(c *gin.Context, param string) (string, bool) {
	raw := c.Param(param)
	if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput(param, "must be a positive integer").WithDetails(param, raw))
		return "", false
	}
	return raw, true
}

func currentUserID(c *gin.Context) (uint, bool) {
	value, exists := c.Get("userID")
	if !exists {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Authentication required"))
		return 0, false
	}

	userID, ok := value.(uint)
	if !ok {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Authentication required"))
		return 0, false
	}

	return userID, true
}

func currentConfig(c *gin.Context) config.Config {
	if val, exists := c.Get("cfg"); exists {
		if cfg, ok := val.(config.Config); ok {
			return cfg
		}
	}
	return config.Config{}
}

// checkIfMatch enforces an optional RFC 9110 If-Match precondition for the
// REST conditional-write scheme (CON-01, issue #456, ADR 0008).
//
// Enforcement is opt-in: a request with no If-Match header writes
// unconditionally, exactly as before this ticket (last-writer-wins) — so
// existing clients and MAINT-02 (#491) are unaffected. When If-Match IS
// present it is matched against the row's current monotonic revision token
// (issue #591, ADR 0006), which the caller must have just loaded:
//
//   - `If-Match: *`            matches any existing row (the caller already
//     proved the row exists by loading it, so this always passes here)
//   - `If-Match: "42"`         matches only revision 42 — quotes optional,
//     an optional weak `W/` prefix is tolerated, and a comma-separated list
//     matches if ANY member matches
//
// On a mismatch (including a malformed or unparseable value) the request is
// aborted with 412 Precondition Failed and the caller MUST return without
// touching the row. Returns true when the caller may proceed.
//
// CardDAV keeps its own separate If-Match path (carddav/backend.go), which
// compares against the `etag` string column — this helper is REST-only.
func checkIfMatch(c *gin.Context, currentRevision int64) bool {
	if ifMatchSatisfied(c.GetHeader("If-Match"), currentRevision) {
		return true
	}
	apperrors.AbortWithError(c, apperrors.ErrPreconditionFailed("").
		WithDetails("expected_revision", currentRevision).
		WithDetails("if_match", c.GetHeader("If-Match")))
	return false
}

// ifMatchSatisfied is the pure predicate behind checkIfMatch: it reports
// whether the raw If-Match header value permits a write against a row
// currently at currentRevision. An empty header is "no precondition" and
// always permits the write; "*" permits it (the row exists); otherwise at
// least one list member must equal the decimal revision.
func ifMatchSatisfied(header string, currentRevision int64) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return true
	}
	want := strconv.FormatInt(currentRevision, 10)
	for _, tok := range strings.Split(header, ",") {
		tok = strings.TrimSpace(tok)
		tok = strings.TrimPrefix(tok, "W/")
		tok = strings.Trim(tok, `"`)
		if tok == "*" || tok == want {
			return true
		}
	}
	return false
}

// handleRevisionConflict classifies an error from a revision-bearing
// entity's db.Save()/db.Updates() call (CON-01 follow-up, issues #920,
// #924; ADR 0018). A *models.ErrRevisionConflict means the atomic
// compare-and-swap in the model's AfterSave hook (models/revision_cas.go)
// found the row gone/soft-deleted (404) or still live but moved past the
// revision this request loaded (412) — a genuine concurrent-write outcome,
// not a database failure, so it must not fall through to the generic
// ErrDatabase(500) handling at each call site. Returns true once it has
// aborted the request; a false return means err is some other failure and
// the caller should handle it as before.
func handleRevisionConflict(c *gin.Context, entityName string, err error) bool {
	var conflict *models.ErrRevisionConflict
	if !errors.As(err, &conflict) {
		return false
	}
	if conflict.Deleted {
		apperrors.AbortWithError(c, apperrors.ErrNotFound(entityName).WithDetails("id", fmt.Sprint(conflict.ID)))
		return true
	}
	apperrors.AbortWithError(c, apperrors.ErrPreconditionFailed("").
		WithDetails("expected_revision", conflict.ExpectedRevision).
		WithDetails("reason", "revision changed concurrently"))
	return true
}

// GetPaginationParams extracts pagination query params using shared defaults and bounds.
func GetPaginationParams(c *gin.Context) PaginationParams {
	page := parsePositiveOrDefault(c.DefaultQuery("page", "1"), defaultPage)
	limit := parsePositiveOrDefault(c.DefaultQuery("limit", "25"), defaultLimit)
	if limit > maxLimit {
		limit = maxLimit
	}

	return PaginationParams{
		Page:   page,
		Limit:  limit,
		Offset: (page - 1) * limit,
	}
}

func parsePositiveOrDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

// parseContactSort normalizes GET /contacts' ?sort= query param (T73):
// "updated_at" (default — today's (updated_at, id) cursor order) or "name"
// (the denormalized sort_name key). Anything else is a 400, not a silent
// fallback — an explicitly-set parameter that is ignored is the worse
// failure (the same policy the handler applies to sort=name combined with
// ?since=).
func parseContactSort(c *gin.Context) (string, *apperrors.AppError) {
	sort := c.DefaultQuery("sort", "updated_at")
	switch sort {
	case "updated_at", "name":
		return sort, nil
	default:
		return "", apperrors.ErrInvalidInput("sort", "must be one of: updated_at, name")
	}
}

// ---------------------------------------------------------------------------
// Cursor pagination + change feeds (T17 — T17).
//
// Every list endpoint paginates with an opaque cursor instead of
// page/offset. The cursor is base64url("<RFC3339Nano UTC updated_at>|<id>")
// — a position in the total order (updated_at, id), which is stable under
// concurrent writes because `id` breaks ties that a bare `updated_at` would
// drop or duplicate at page boundaries. The shape is opaque to clients so it
// can change without another API-contract break.
// ---------------------------------------------------------------------------

// SyncMode describes how a collection is synced (T17's decided split).
const (
	// SyncModeIncremental collections are soft-deleted, support ?since= change
	// feeds, and normally drop `total` from their list response (exact counts are
	// expensive on the large, unboundedly-growing tables — contacts, notes,
	// activities, life_events). Preferences is a deliberate exception: it
	// soft-deletes and supports ?since= but is small enough that `total` stays
	// in browse mode.
	SyncModeIncremental = "incremental"
	// SyncModeFullResync collections are hard-deleted and bounded-small; a
	// client re-pulls them wholesale. `total` is cheap here and kept.
	SyncModeFullResync = "full_resync"
)

// IncrementalSyncCollections and FullResyncSyncCollections are the static
// sync-mode contract every list response carries, so a client can plan a
// sync without hardcoding which endpoints feed and which resync. Keep this
// in step with each entity's soft/hard-delete decision (see T17 and T26).
var (
	IncrementalSyncCollections = []string{
		"contacts", "notes", "activities", "life_events", "preferences",
		"cadence_policies", "conversation_agenda", "gifts",
	}
	FullResyncSyncCollections = []string{
		"circles", "households", "tags", "relationship_edges", "field_definitions",
		"external_identities", "external_activities",
	}
)

// buildSyncMeta returns the `sync` object every list response embeds: which
// sync mode THIS collection uses, plus the full static map of incremental vs
// full-resync collections so a client can plan a complete sync.
func buildSyncMeta(mode string) gin.H {
	return gin.H{
		"mode":        mode,
		"incremental": IncrementalSyncCollections,
		"full_resync": FullResyncSyncCollections,
	}
}

// Cursor is one position in the (updated_at, id) total order. ID is kept as
// its wire string and re-typed at each call site, since PK types differ
// (uint for gorm.Model entities, UUID strings for the others).
type Cursor struct {
	UpdatedAt time.Time
	ID        string
}

// NameCursor is one position in the (sort_name, id) total order used by
// GET /contacts?sort=name (T73) — the same opaque "resume after this row"
// idea as Cursor, but the leading component is the denormalized name-sort key
// (models.DeriveSortName) rather than a timestamp. Kept as a separate shape
// rather than a generalized Cursor because the two have different wire
// encodings and the name sort is deliberately local to the contacts list
// (T73 — never a feed, never any other list endpoint).
type NameCursor struct {
	SortName string
	ID       string
}

// EncodeCursor renders a cursor as an opaque base64url string. id may be any
// of the PK types (uint, uint64, string); it is rendered with fmt.Sprint.
//
// The timestamp is encoded in its OWN location, not normalized to UTC:
// SQLite stores DATETIME values as TEXT in the driver's format WITH the
// timezone offset (e.g. "2006-01-02 15:04:05.999999999-07:00"), and the
// cursor predicate compares that TEXT lexicographically. A cursor converted
// to UTC would bind as "+00:00" and sort on the wrong side of every stored
// local-time string — the exact bug this comment exists to prevent. Keeping
// the wall-clock + offset lets the round-tripped bound value match the
// stored text byte-for-byte.
func EncodeCursor(updatedAt time.Time, id any) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(updatedAt.Format(time.RFC3339Nano) + "|" + fmt.Sprint(id)),
	)
}

// DecodeCursor parses an opaque cursor back into its (updated_at, id)
// position. Returns an error for anything that is not a well-formed cursor.
func DecodeCursor(raw string) (*Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("cursor is not valid base64url")
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("cursor is malformed")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, errors.New("cursor timestamp is malformed")
	}
	return &Cursor{UpdatedAt: t, ID: parts[1]}, nil
}

// EncodeNameCursor renders a T73 name-sort cursor as an opaque base64url
// string: base64url("sort_name|id"). Same wire idiom as EncodeCursor; only
// the leading component differs (a sort name instead of a timestamp).
func EncodeNameCursor(sortName string, id any) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(sortName + "|" + fmt.Sprint(id)),
	)
}

// DecodeNameCursor parses a T73 name-sort cursor back into its
// (sort_name, id) position. Returns an error for anything that is not a
// well-formed name cursor. The timestamp-shape rejection exists so a cursor
// minted by an updated_at-sorted request fails loudly here instead of being
// silently treated as a sort name and matching nothing — the same explicit
// failure DecodeCursor gives a name-shaped cursor in the other direction.
//
// The separator is the LAST "|", not the first: the id is always the trailing
// numeric component, so a (pathological but legal) sort name that itself
// contains the delimiter still round-trips instead of being mis-split.
func DecodeNameCursor(raw string) (*NameCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("cursor is not valid base64url")
	}
	s := string(decoded)
	sep := strings.LastIndex(s, "|")
	if sep <= 0 || sep == len(s)-1 {
		return nil, errors.New("cursor is malformed")
	}
	sortName := s[:sep]
	if _, err := time.Parse(time.RFC3339Nano, sortName); err == nil {
		return nil, errors.New("cursor is a time-based cursor; expected a name-sorted cursor")
	}
	return &NameCursor{SortName: sortName, ID: s[sep+1:]}, nil
}

// CursorParams is the sanitized set of pagination controls shared by every
// list handler.
type CursorParams struct {
	// Limit bounds the page size (default 25, max 100).
	Limit int
	// Order is "asc" or "desc" (default "desc"). The change feed (?since=)
	// always resolves to "asc" so a replica replays history forward.
	Order string
	// Cursor is the resume-after position supplied via ?cursor= or ?since=,
	// in the (updated_at, id) shape. Only one of Cursor/NameCursor is ever
	// set.
	Cursor *Cursor
	// NameCursor is the T73 resume-after position supplied via ?cursor= when
	// the request is GET /contacts?sort=name — the (sort_name, id) shape.
	// Every other list endpoint leaves it nil.
	NameCursor *NameCursor
	// Since is true when the request was a ?since= change-feed request.
	Since bool
}

// cursorKind selects which ?cursor= shape GetCursorParams decodes: the
// time-based (updated_at, id) Cursor (every list endpoint, and the ?since=
// feed) or the T73 name-based (sort_name, id) NameCursor. The name shape is
// deliberately opt-in per caller — see GetCursorParamsForSort.
type cursorKind int

const (
	cursorUpdatedAt cursorKind = iota
	cursorName
)

// GetCursorParams extracts cursor pagination controls from the request,
// sharing defaults/bounds with the legacy offset parser above. A malformed
// ?cursor= or ?since= is an error (caller aborts with 400); both are
// exclusive-of-the-position "resume after this row" predicates. The ?cursor=
// value is decoded as a time-based cursor — the shape every endpoint except
// GET /contacts?sort=name uses. Since only GetContacts (via
// GetCursorParamsForSort) needs the name shape, the switch never leaks it
// anywhere else.
func GetCursorParams(c *gin.Context) (CursorParams, *apperrors.AppError) {
	return cursorParams(c, cursorUpdatedAt)
}

// GetCursorParamsForSort is the T73 variant of GetCursorParams for the
// contacts list: when sort is "name", a ?cursor= value is decoded as a
// (sort_name, id) NameCursor instead of a time-based Cursor. ?since= is
// ALWAYS decoded as time-based — the change feed is sync state ordered by
// (updated_at, id), never by name — and GetContacts rejects sort=name+since
// before this matters. Any other sort value falls through to the time-based
// shape.
func GetCursorParamsForSort(c *gin.Context, sort string) (CursorParams, *apperrors.AppError) {
	kind := cursorUpdatedAt
	if sort == "name" {
		kind = cursorName
	}
	return cursorParams(c, kind)
}

func cursorParams(c *gin.Context, kind cursorKind) (CursorParams, *apperrors.AppError) {
	limit := parsePositiveOrDefault(c.DefaultQuery("limit", "25"), defaultLimit)
	if limit > maxLimit {
		limit = maxLimit
	}

	params := CursorParams{
		Limit: limit,
		Order: c.DefaultQuery("order", "desc"),
	}
	if params.Order != "asc" {
		params.Order = "desc"
	}

	if raw := c.Query("cursor"); raw != "" {
		switch kind {
		case cursorName:
			cur, err := DecodeNameCursor(raw)
			if err != nil {
				return params, apperrors.ErrInvalidInput("cursor", err.Error())
			}
			params.NameCursor = cur
		default:
			cur, err := DecodeCursor(raw)
			if err != nil {
				return params, apperrors.ErrInvalidInput("cursor", err.Error())
			}
			params.Cursor = cur
		}
	}
	if raw := c.Query("since"); raw != "" {
		cur, err := DecodeCursor(raw)
		if err != nil {
			return params, apperrors.ErrInvalidInput("since", err.Error())
		}
		params.Cursor = cur
		params.Since = true
	}
	// The change feed must always move forward: newest-page-first browsing is
	// only meaningful for a live list, not for replaying history.
	if params.Since {
		params.Order = "asc"
	}
	return params, nil
}

// CheckFeedCursorAge returns a 410 Gone error when a ?since= cursor predates
// the purge retention window (T26) — the sync horizon. Soft-deleted rows
// older than the window have been hard-deleted by the purge job, so a client
// resuming from such a cursor has permanently missed tombstones and can only
// converge via a full resync. The retention number here is deliberately the
// SAME config the purge job reads (config.Config.DeleteRetentionDays) so the
// two can never drift apart. Only ?since= (feed) cursors are checked: a
// plain ?cursor= is transient UI navigation, not long-lived sync state.
func CheckFeedCursorAge(c *gin.Context, params CursorParams) *apperrors.AppError {
	if !params.Since || params.Cursor == nil {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -currentConfig(c).DeleteRetentionDays)
	if params.Cursor.UpdatedAt.Before(cutoff) {
		return apperrors.ErrGone(
			"cursor is older than the retention window (DELETED_RETENTION_DAYS); " +
				"deleted rows have been purged — full resync required",
		)
	}
	return nil
}

// cursorPredicate returns the SQL row-value predicate that selects rows
// strictly after (asc) or strictly before (desc) the cursor position, using
// the (updated_at, id) feed key. id is passed already re-typed to the
// table's PK column type so SQLite's type ordering cannot miscompare (a TEXT
// "5" compares less than INTEGER 5).
func cursorPredicate(table string, cursor *Cursor, id any, desc bool) (string, any, any) {
	op := ">"
	if desc {
		op = "<"
	}
	return fmt.Sprintf("(%s.updated_at, %s.id) %s (?, ?)", table, table, op), cursor.UpdatedAt, id
}

// nameCursorPredicate is the T73 name-sort analog of cursorPredicate: the
// row-value predicate over the (sort_name, id) key. Same "strictly after
// (asc) / strictly before (desc)" semantics and same explicit-id rule.
func nameCursorPredicate(table string, cursor *NameCursor, id any, desc bool) (string, any, any) {
	op := ">"
	if desc {
		op = "<"
	}
	return fmt.Sprintf("(%s.sort_name, %s.id) %s (?, ?)", table, table, op), cursor.SortName, id
}

// cursorOrderBy orders a query by the (updated_at, id) feed key in the given
// direction — the order that makes cursor pagination total and stable.
func cursorOrderBy(q *gorm.DB, table string, desc bool) *gorm.DB {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return q.Order(table + ".updated_at " + dir).Order(table + ".id " + dir)
}

// nameCursorOrderBy is the T73 name-sort analog of cursorOrderBy: order by
// the (sort_name, id) key in the given direction, the tiebreak-by-id making
// the order total for contacts sharing a sort_name.
func nameCursorOrderBy(q *gorm.DB, table string, desc bool) *gorm.DB {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return q.Order(table + ".sort_name " + dir).Order(table + ".id " + dir)
}

// parseCursorID re-types a cursor's string ID to a uint PK column value.
func parseCursorID(cursor *Cursor) (uint, bool) {
	return parseUintID(cursor.ID)
}

// parseUintID re-types a cursor's string ID to a uint PK column value,
// shared by the time-based and name-based cursor shapes.
func parseUintID(raw string) (uint, bool) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}
