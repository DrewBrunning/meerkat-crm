package controllers

import (
	"encoding/base64"
	"errors"
	"fmt"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/models"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// timelinePreviewLimit bounds each of the six timeline-eligible collection
// blocks of the M4 composite (buildContactDetail) at the default 5-item
// preview. 5 per type is provably sufficient to build a correct 5-item
// preview regardless of type distribution (T66 design decision 4): any item
// in the global top 5 must be within its own type's top 5, provided each
// type's block is ranked by the same key the preview merges on (event date).
const timelinePreviewLimit = 5

// ---------------------------------------------------------------------------
// Contact timeline (T66 — T66).
//
// GET /contacts/:id/timeline is the paginated, filterable view of a
// contact's merged timeline across all six event types, built on T17's
// cursor-pagination discipline but over a *derived* key. The cursor is a
// normalized (event_date, type, id) tuple: type is in the key because two
// rows in different tables can share a date, and id because two rows in the
// same table can too. PK types differ across the six tables (uint vs.
// UUID-string), so the cursor carries id as a string and each per-table
// predicate re-types it (parseUintID), the way T17's parseCursorID already
// does.
//
// Merge strategy (design decision 3, option b): fetch N+1 rows from each of
// the five SQL-orderable types (N being the requested page size), merge,
// sort by (event_date, type, id), take the top N. This is exactly correct,
// not approximate: if item X belongs in the true global top N but wasn't
// returned, X's own table must have had N items ranked above it — all of
// which are also globally above X, so X was never in the top N. The only
// cost is fetching up to 6N rows to return N, which at a page size of 25 is
// trivial. Do NOT "optimize" this into asking each table for N/6 — that
// undercounts when one type dominates.
//
// Life events are the one deliberate exception to the per-table bounded
// fetch: their date is a PartialDate stored as JSON, which SQLite cannot
// order or compare as a timestamp, so the cursor and bucket predicates for
// them are applied in Go on a resolved date instead. They are fetched in
// full per request — a bounded-small, user-authored set (a person's notable
// life events, not Immich's photo-appearance flood), so the exception is
// documented rather than engineered around with a schema change.
// ---------------------------------------------------------------------------

// timelineCursor is one position in the merged timeline's total order
// (event_date, type, id). The date is kept in its own location/format, never
// UTC-normalized, exactly like EncodeCursor: the per-table SQL predicate
// compares SQLite's stored DATETIME text lexicographically, so the encoded
// value must round-trip byte-for-byte against what the driver writes.
type timelineCursor struct {
	date time.Time
	typ  string
	id   string
}

func encodeTimelineCursor(date time.Time, typ, id string) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(date.Format(time.RFC3339Nano) + "|" + typ + "|" + id),
	)
}

// decodeTimelineCursor parses an opaque timeline cursor back into its
// (event_date, type, id) position. The id is the trailing component (ids are
// uints or UUIDs — neither can contain "|"), so the first two separators are
// authoritative.
func decodeTimelineCursor(raw string) (*timelineCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("cursor is not valid base64url")
	}
	parts := strings.SplitN(string(decoded), "|", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, errors.New("cursor is malformed")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, errors.New("cursor timestamp is malformed")
	}
	if _, ok := models.TimelineTypeRank(parts[1]); !ok {
		return nil, errors.New("cursor type is unknown")
	}
	return &timelineCursor{date: t, typ: parts[1], id: parts[2]}, nil
}

// parseTimelineParams extracts and validates the timeline endpoint's query
// controls: limit (shared default/bounds), order (asc|desc), the opaque
// cursor, the comma-separated type filter, and the recency bucket. A
// malformed cursor, unknown type token, or unknown bucket is an error (400)
// — never a silent fallback.
func parseTimelineParams(c *gin.Context) (limit int, order string, cur *timelineCursor, types []string, bucket string, appErr *apperrors.AppError) {
	limit = parsePositiveOrDefault(c.DefaultQuery("limit", "25"), defaultLimit)
	if limit > maxLimit {
		limit = maxLimit
	}
	order = c.DefaultQuery("order", "desc")
	if order != "asc" {
		order = "desc"
	}
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := decodeTimelineCursor(raw)
		if err != nil {
			return 0, "", nil, nil, "", apperrors.ErrInvalidInput("cursor", err.Error())
		}
		cur = parsed
	}
	types, appErr = parseTimelineTypeFilter(c.Query("type"))
	if appErr != nil {
		return
	}
	bucket, appErr = parseTimelineBucket(c.Query("bucket"))
	return
}

// parseTimelineTypeFilter splits the ?type= query param (comma-separated
// tokens) into the set of timeline types to include. Empty means all six.
// Any unknown token is a 400 — an explicitly-set filter that is silently
// ignored is the worse failure.
func parseTimelineTypeFilter(raw string) ([]string, *apperrors.AppError) {
	if strings.TrimSpace(raw) == "" {
		return models.TimelineTypes, nil
	}
	var types []string
	seen := make(map[string]bool, len(models.TimelineTypes))
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if _, ok := models.TimelineTypeRank(tok); !ok {
			return nil, apperrors.ErrInvalidInput("type",
				fmt.Sprintf("unknown timeline type %q; expected one of: %s", tok, strings.Join(models.TimelineTypes, ", ")))
		}
		if !seen[tok] {
			seen[tok] = true
			types = append(types, tok)
		}
	}
	if len(types) == 0 {
		return models.TimelineTypes, nil
	}
	return types, nil
}

// parseTimelineBucket normalizes the ?bucket= recency filter, defaulting the
// empty value to "all". Anything outside the fixed set is a 400.
func parseTimelineBucket(raw string) (string, *apperrors.AppError) {
	switch raw {
	case "", models.TimelineBucketAll:
		return models.TimelineBucketAll, nil
	case models.TimelineBucketLast7Days, models.TimelineBucketLast30Days,
		models.TimelineBucketLast90Days, models.TimelineBucketThisYear:
		return raw, nil
	default:
		return "", apperrors.ErrInvalidInput("bucket",
			"must be one of: last_7_days, last_30_days, last_90_days, this_year, all")
	}
}

// timelineBucketCutoff returns the inclusive cutoff instant for a recency
// bucket computed against now, and ok=false when the bucket is "all". now
// carries the location the "this_year" boundary is computed in.
func timelineBucketCutoff(bucket string, now time.Time) (time.Time, bool) {
	switch bucket {
	case models.TimelineBucketLast7Days:
		return now.AddDate(0, 0, -7), true
	case models.TimelineBucketLast30Days:
		return now.AddDate(0, 0, -30), true
	case models.TimelineBucketLast90Days:
		return now.AddDate(0, 0, -90), true
	case models.TimelineBucketThisYear:
		return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location()), true
	default:
		return time.Time{}, false
	}
}

// timelineCursorPredicate builds the SQL predicate selecting rows of one
// table that belong strictly on the requested side of the cursor position in
// the normalized (event_date, type, id) order. desc=true pages forward
// newest-first ("strictly before the cursor"); desc=false oldest-first
// ("strictly after").
//
// The type component is what makes the per-table predicate more than the
// usual row-value comparison: a table whose type ranks below the cursor's
// type is entirely before the cursor at the shared date (its type tiebreak
// is smaller), while a table ranking above it must be strictly older. id is
// the tiebreak for the single table whose type equals the cursor's. id must
// be pre-typed to the table's PK column type so SQLite's type ordering
// cannot miscompare.
func timelineCursorPredicate(table, dateCol, typ string, cur *timelineCursor, id any, desc bool) (string, []any) {
	cursorRank, _ := models.TimelineTypeRank(cur.typ)
	tableRank, _ := models.TimelineTypeRank(typ)

	// desc pages newest-first: rows strictly BEFORE the cursor in the
	// (event_date, type, id) order.
	if desc {
		switch {
		case tableRank < cursorRank:
			// A type ranking below the cursor's is entirely before it at the
			// shared date (its type tiebreak is smaller): D <= cd.
			return fmt.Sprintf("(%s.%s <= ?)", table, dateCol), []any{cur.date}
		case tableRank == cursorRank:
			// Same type: the row-value (date, id) tiebreak decides.
			return fmt.Sprintf("(%s.%s < ? OR (%s.%s = ? AND %s.id < ?))", table, dateCol, table, dateCol, table), []any{cur.date, cur.date, id}
		default:
			// A type ranking above the cursor's needs a strictly older date
			// (at D = cd its type tiebreak puts it after the cursor): D < cd.
			return fmt.Sprintf("(%s.%s < ?)", table, dateCol), []any{cur.date}
		}
	}

	// asc pages oldest-first: rows strictly AFTER the cursor.
	switch {
	case tableRank < cursorRank:
		// A type ranking below the cursor's needs a strictly newer date
		// (at D = cd its type tiebreak puts it before the cursor): D > cd.
		return fmt.Sprintf("(%s.%s > ?)", table, dateCol), []any{cur.date}
	case tableRank == cursorRank:
		// Same type: the row-value (date, id) tiebreak decides.
		return fmt.Sprintf("(%s.%s > ? OR (%s.%s = ? AND %s.id > ?))", table, dateCol, table, dateCol, table), []any{cur.date, cur.date, id}
	default:
		// A type ranking above the cursor's is entirely after it at the
		// shared date (its type tiebreak is larger): D >= cd.
		return fmt.Sprintf("(%s.%s >= ?)", table, dateCol), []any{cur.date}
	}
}

// timelineDateOrder orders a query by (event_date, id) in the paging
// direction — the per-table order that makes the merged cursor pagination
// total and stable.
func timelineDateOrder(q *gorm.DB, table, dateCol string, desc bool) *gorm.DB {
	dir := "DESC"
	if !desc {
		dir = "ASC"
	}
	return q.Order(table + "." + dateCol + " " + dir).Order(table + ".id " + dir)
}

// resolveTimelineCursorIDs re-types the cursor's string id once per table
// shape — uint for the gorm.Model tables, string for the UUID-PK tables — so
// the per-table SQL predicates compare the way the columns store them
// (CLAUDE.md trap 1's exact silent-mismatch class, on the comparison side).
//
// Only the table whose type equals the cursor's type ever binds the id (the
// row-value tiebreak); every other table is selected purely on its date
// component. So the id is only validated for that one type, and only when it
// is a uint-PK type: a malformed numeric id there is a 400 (resuming a
// numeric-id page from a non-numeric id is a client error and fails loudly
// rather than silently matching nothing). A UUID-string cursor from a
// string-PK page boundary must NOT 400 a subsequent mixed-type page — the
// uint tables' predicates are date-only against it.
func resolveTimelineCursorIDs(types []string, cur *timelineCursor) (map[string]any, *apperrors.AppError) {
	ids := make(map[string]any, len(types))
	for _, t := range types {
		if t == cur.typ {
			switch t {
			case models.TimelineTypeNote, models.TimelineTypeActivity, models.TimelineTypeCompletion:
				id, ok := parseUintID(cur.id)
				if !ok {
					return nil, apperrors.ErrInvalidInput("cursor", "cursor id is malformed")
				}
				ids[t] = id
				continue
			}
		}
		// Non-matching types and string-PK types use the raw string; the
		// former never bind it, the latter store it as-is.
		ids[t] = cur.id
	}
	return ids, nil
}

// timelineComposer holds the per-request state shared by the six per-type
// fetches and the merge.
type timelineComposer struct {
	db        *gorm.DB
	userID    uint
	contact   *models.Contact
	limit     int
	desc      bool
	cur       *timelineCursor
	cursorIDs map[string]any
	cutoff    time.Time
	hasCutoff bool
	// now is the "current time" in the user's configured location, used for
	// recency cutoffs and for the yearless month/day life-event date case.
	now time.Time
}

// applyDateBounds narrows a per-type query to the recency bucket, when one
// was requested. The cutoff is bound as a time.Time so GORM writes it in the
// same TEXT format the driver stores DATETIME columns in — the same
// byte-for-byte assumption the T17 ?since= predicate relies on.
func (tc *timelineComposer) applyDateBounds(query *gorm.DB, table, dateCol string) *gorm.DB {
	if tc.hasCutoff {
		query = query.Where(table+"."+dateCol+" >= ?", tc.cutoff)
	}
	return query
}

// applyCursor narrows a per-type query to the cursor's page boundary. The id
// argument is the pre-typed value from cursorIDs.
func (tc *timelineComposer) applyCursor(query *gorm.DB, table, dateCol, typ string, id any) *gorm.DB {
	if tc.cur == nil {
		return query
	}
	pred, args := timelineCursorPredicate(table, dateCol, typ, tc.cur, id, tc.desc)
	return query.Where(pred, args...)
}

// buildTableQuery composes the common timeline bounds — recency bucket,
// cursor predicate, then (event_date, id) ordering and the bounded limit —
// onto a per-type base query. Limit+1 is fetched so next_cursor presence is
// exact, matching every T17 list handler.
func (tc *timelineComposer) buildTableQuery(base *gorm.DB, table, dateCol, typ string) *gorm.DB {
	query := tc.applyDateBounds(base, table, dateCol)
	query = tc.applyCursor(query, table, dateCol, typ, tc.cursorIDs[typ])
	return timelineDateOrder(query, table, dateCol, tc.desc).Limit(tc.limit + 1)
}

// timelineEntry is one merged row: the raw entity plus the normalized sort
// key. numID is the numeric PK for uint-PK tables (nil for string-PK tables)
// — the id tiebreak must compare the way the per-table SQL predicate does,
// or a page boundary can disagree about which of two same-date rows of the
// same type comes first.
type timelineEntry struct {
	typ   string
	id    string
	date  time.Time
	numID *uint
	data  interface{}
}

func (tc *timelineComposer) fetchNotes() ([]timelineEntry, error) {
	query := tc.buildTableQuery(
		tc.db.Where("contact_id = ? AND user_id = ?", tc.contact.ID, tc.userID),
		"notes", "date", models.TimelineTypeNote,
	)
	var notes []models.Note
	if err := query.Find(&notes).Error; err != nil {
		return nil, err
	}
	entries := make([]timelineEntry, 0, len(notes))
	for i := range notes {
		n := &notes[i]
		entries = append(entries, timelineEntry{
			typ: models.TimelineTypeNote, id: fmt.Sprint(n.ID), date: n.Date,
			numID: &n.ID, data: *n,
		})
	}
	return entries, nil
}

func (tc *timelineComposer) fetchActivities() ([]timelineEntry, error) {
	query := tc.buildTableQuery(
		tc.db.Model(&models.Activity{}).
			Joins("JOIN activity_contacts ON activities.id = activity_contacts.activity_id").
			Where("activities.user_id = ? AND activity_contacts.contact_id = ?", tc.userID, tc.contact.ID),
		"activities", "date", models.TimelineTypeActivity,
	)
	query = query.Preload("Contacts", func(db *gorm.DB) *gorm.DB {
		return db.Select("ID", "Firstname", "Lastname", "PhotoThumbnail", "Circles").Where("user_id = ?", tc.userID)
	})
	var activities []models.Activity
	if err := query.Find(&activities).Error; err != nil {
		return nil, err
	}
	entries := make([]timelineEntry, 0, len(activities))
	for i := range activities {
		a := &activities[i]
		entries = append(entries, timelineEntry{
			typ: models.TimelineTypeActivity, id: fmt.Sprint(a.ID), date: a.Date,
			numID: &a.ID, data: *a,
		})
	}
	return entries, nil
}

func (tc *timelineComposer) fetchCompletions() ([]timelineEntry, error) {
	query := tc.buildTableQuery(
		tc.db.Where("contact_id = ? AND user_id = ?", tc.contact.ID, tc.userID),
		"reminder_completions", "completed_at", models.TimelineTypeCompletion,
	)
	var completions []models.ReminderCompletion
	if err := query.Find(&completions).Error; err != nil {
		return nil, err
	}
	entries := make([]timelineEntry, 0, len(completions))
	for i := range completions {
		comp := &completions[i]
		entries = append(entries, timelineEntry{
			typ: models.TimelineTypeCompletion, id: fmt.Sprint(comp.ID), date: comp.CompletedAt,
			numID: &comp.ID, data: *comp,
		})
	}
	return entries, nil
}

func (tc *timelineComposer) fetchExternalActivities() ([]timelineEntry, error) {
	query := tc.buildTableQuery(
		tc.db.Where("user_id = ? AND entity_id = ?", tc.userID, tc.contact.VCardUID),
		"external_activities", "occurred_at", models.TimelineTypeExternalActivity,
	)
	var activities []models.ExternalActivity
	if err := query.Find(&activities).Error; err != nil {
		return nil, err
	}
	entries := make([]timelineEntry, 0, len(activities))
	for i := range activities {
		a := &activities[i]
		entries = append(entries, timelineEntry{
			typ: models.TimelineTypeExternalActivity, id: a.ID, date: a.OccurredAt, data: *a,
		})
	}
	return entries, nil
}

// fetchGifts fetches only timeline-eligible gifts: given/received records
// with a handover date. Undated ideas are deliberately not timeline events
// (the web timeline filters them the same way), though they remain visible in
// the gifts block of the M4 composite.
func (tc *timelineComposer) fetchGifts() ([]timelineEntry, error) {
	query := tc.buildTableQuery(
		tc.db.Where("user_id = ? AND entity_id = ?", tc.userID, tc.contact.VCardUID).
			Where("status IN ?", []string{models.GiftStatusGiven, models.GiftStatusReceived}).
			Where("date IS NOT NULL"),
		"gifts", "date", models.TimelineTypeGift,
	)
	var gifts []models.Gift
	if err := query.Find(&gifts).Error; err != nil {
		return nil, err
	}
	entries := make([]timelineEntry, 0, len(gifts))
	for i := range gifts {
		g := &gifts[i]
		entries = append(entries, timelineEntry{
			typ: models.TimelineTypeGift, id: g.ID, date: *g.Date, data: *g,
		})
	}
	return entries, nil
}

// timelineLifeEventDate resolves a LifeEvent's timeline date from its
// PartialDate, mirroring the web's fullDateFromPartial exactly: the full date
// when all three components exist (UTC midnight — how the web's YYYY-MM-DD
// string parses); the current year when only month/day; Jan 1 of the year
// when only year; CreatedAt when nothing usable (the web falls back to
// created_at the same way). now is the "current time" for the yearless
// month/day case.
func timelineLifeEventDate(e *models.LifeEvent, now time.Time) time.Time {
	if d := e.Date; d != nil {
		if d.Year != nil && d.Month != nil && d.Day != nil {
			return time.Date(*d.Year, time.Month(*d.Month), *d.Day, 0, 0, 0, 0, time.UTC)
		}
		if d.Month != nil && d.Day != nil {
			return time.Date(now.Year(), time.Month(*d.Month), *d.Day, 0, 0, 0, 0, time.UTC)
		}
		if d.Year != nil {
			return time.Date(*d.Year, 1, 1, 0, 0, 0, 0, time.UTC)
		}
	}
	return e.CreatedAt
}

// fetchLifeEvents fetches the contact's life events in full and resolves each
// one's timeline date in Go, applying the cursor and bucket predicates here
// rather than in SQL — their PartialDate is JSON text with no orderable
// timestamp. This is the documented exception to the per-table bounded fetch
// (see the endpoint's doc comment).
func (tc *timelineComposer) fetchLifeEvents() ([]timelineEntry, error) {
	var events []models.LifeEvent
	if err := tc.db.Where("user_id = ? AND entity_id = ?", tc.userID, tc.contact.VCardUID).
		Order("created_at DESC").Find(&events).Error; err != nil {
		return nil, err
	}
	entries := make([]timelineEntry, 0, len(events))
	for i := range events {
		e := &events[i]
		date := timelineLifeEventDate(e, tc.now)
		if tc.hasCutoff && date.Before(tc.cutoff) {
			continue
		}
		if tc.cur != nil && !timelineEntrySideOfCursor(models.TimelineTypeLifeEvent, date, e.ID, tc.cur, tc.desc) {
			continue
		}
		entries = append(entries, timelineEntry{
			typ: models.TimelineTypeLifeEvent, id: e.ID, date: date, data: *e,
		})
	}
	return entries, nil
}

// timelineEntrySideOfCursor is the Go-side equivalent of
// timelineCursorPredicate, used only for life events: it reports whether an
// entry's normalized (event_date, type, id) position is strictly before the
// cursor for desc paging, or strictly after it for asc paging. Life-event ids
// are UUID strings, so the id tiebreak is textual — matching the string-PK
// SQL comparisons.
func timelineEntrySideOfCursor(typ string, date time.Time, id string, cur *timelineCursor, desc bool) bool {
	rank, _ := models.TimelineTypeRank(typ)
	cursorRank, _ := models.TimelineTypeRank(cur.typ)

	if desc {
		// entry strictly before cursor
		if date.Before(cur.date) {
			return true
		}
		if date.After(cur.date) {
			return false
		}
		if rank < cursorRank {
			return true
		}
		if rank > cursorRank {
			return false
		}
		return id < cur.id
	}
	// asc: entry strictly after cursor
	if date.After(cur.date) {
		return true
	}
	if date.Before(cur.date) {
		return false
	}
	if rank > cursorRank {
		return true
	}
	if rank < cursorRank {
		return false
	}
	return id > cur.id
}

// timelineEntryBefore reports whether entry a sorts before entry b in the
// requested direction of the (event_date, type, id) order. id compares
// numerically for uint-PK tables and textually for string-PK ones, matching
// the per-table SQL predicate so a page boundary never disagrees with the
// merge.
func timelineEntryBefore(a, b timelineEntry, desc bool) bool {
	if a.date.Equal(b.date) {
		if a.typ == b.typ {
			if a.numID != nil && b.numID != nil {
				if desc {
					return *a.numID > *b.numID
				}
				return *a.numID < *b.numID
			}
			if desc {
				return a.id > b.id
			}
			return a.id < b.id
		}
		aRank, _ := models.TimelineTypeRank(a.typ)
		bRank, _ := models.TimelineTypeRank(b.typ)
		if desc {
			return aRank > bRank
		}
		return aRank < bRank
	}
	if desc {
		return a.date.After(b.date)
	}
	return a.date.Before(b.date)
}

// GetContactTimeline returns a page of a contact's merged timeline across
// the six event types, filtered by ?type= (comma-separated) and ?bucket=
// (recency), paginated by an opaque (event_date, type, id) cursor. Read-only;
// every sub-query is scoped by user_id (CLAUDE.md trap 5).
func GetContactTimeline(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	contact, ok := resolveOwnedContactByID(c, db, userID, c.Param("id"))
	if !ok {
		return
	}

	limit, order, cur, types, bucket, appErr := parseTimelineParams(c)
	if appErr != nil {
		apperrors.AbortWithError(c, appErr)
		return
	}

	cursorIDs := map[string]any{}
	if cur != nil {
		cursorIDs, appErr = resolveTimelineCursorIDs(types, cur)
		if appErr != nil {
			apperrors.AbortWithError(c, appErr)
			return
		}
	}

	now := reminderNow(c)
	cutoff, hasCutoff := timelineBucketCutoff(bucket, now)

	composer := timelineComposer{
		db: db, userID: userID, contact: contact,
		limit: limit, desc: order == "desc", cur: cur, cursorIDs: cursorIDs,
		cutoff: cutoff, hasCutoff: hasCutoff, now: now,
	}

	include := make(map[string]bool, len(types))
	for _, t := range types {
		include[t] = true
	}

	entries := make([]timelineEntry, 0, 6*(limit+1))
	if include[models.TimelineTypeLifeEvent] {
		life, lifeErr := composer.fetchLifeEvents()
		if lifeErr != nil {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact timeline").WithError(lifeErr))
			return
		}
		entries = append(entries, life...)
	}
	var err error
	for _, t := range types {
		if t == models.TimelineTypeLifeEvent {
			continue
		}
		var fetched []timelineEntry
		switch t {
		case models.TimelineTypeNote:
			fetched, err = composer.fetchNotes()
		case models.TimelineTypeActivity:
			fetched, err = composer.fetchActivities()
		case models.TimelineTypeCompletion:
			fetched, err = composer.fetchCompletions()
		case models.TimelineTypeExternalActivity:
			fetched, err = composer.fetchExternalActivities()
		case models.TimelineTypeGift:
			fetched, err = composer.fetchGifts()
		}
		if err != nil {
			break
		}
		entries = append(entries, fetched...)
	}
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact timeline").WithError(err))
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		return timelineEntryBefore(entries[i], entries[j], composer.desc)
	})

	nextCursor := ""
	if len(entries) > limit {
		last := entries[limit-1]
		nextCursor = encodeTimelineCursor(last.date, last.typ, last.id)
		entries = entries[:limit]
	}

	// Items is deliberately never nil so the response serializes `[]`, not
	// null/absent (CLAUDE.md frontend trap 8).
	items := make([]models.TimelineItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, models.TimelineItem{Type: e.typ, ID: e.id, Date: e.date, Data: e.data})
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"next_cursor": nextCursor,
		"limit":       limit,
	})
}
