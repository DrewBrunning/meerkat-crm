package routes

// TestConditionalWriteMatrix is CON-02 (issue #457): the exhaustive proof that
// the CON-01 optimistic-concurrency check (issue #456, ADR 0008) is wired into
// every write path that carries a revision token, and that a new mutation route
// cannot be added without a deliberate decision about whether it needs one.
//
// It mirrors TestAuthorizationMatrix's #371 shape: every mutating route
// (POST/PUT/PATCH/DELETE) enumerated from the live router must appear in the
// declared table below in exactly one of two buckets —
//
//   - cwEnforced  — a conditional single-row replace/delete on one of ADR
//     0006's five revision-bearing entities (Contact, Note, Activity,
//     LifeEvent, Reminder). A stale If-Match MUST be rejected with 412 and the
//     row MUST be left byte-identical.
//   - cwExempt    — everything else, each with a short recorded reason
//     (creates have no prior revision, state toggles are out of ADR 0008's
//     scope, non-revisioned entities have no token, DAV has its own If-Match
//     path, …).
//
// A registered route missing from the table fails the completeness check; a
// table row with no registered route fails as stale. So a newly added
// `PUT /api/v1/somethings/:id` on a revision-bearing entity that forgets the
// check lands with no row and turns this test red — the "produces confidence
// without protection" failure mode the ticket calls out.
//
// Real migrated schema via dbtest (CLAUDE.md backend trap #1), never
// AutoMigrate.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/contactmodel"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type cwClass int

const (
	cwEnforced cwClass = iota
	cwExempt
)

// cwRow is one declared conditional-write classification. For cwEnforced rows,
// probe is the concrete owner-resource path and body builds a minimally-valid
// request body (nil for DELETE). For cwExempt rows, reason records why the
// route needs no conditional-write check.
type cwRow struct {
	class  cwClass
	reason string
	probe  string
	body   func() any
}

// Short, grouped exemption reasons — a route is one of these or it is enforced.
const (
	reasonCreate       = "create: mints a new row, no prior revision to be stale against (retry-safety is CON-04/#459)"
	reasonToggle       = "state toggle on a revision-bearing entity — deliberately out of ADR 0008's scope (no body to conflict over, effectively idempotent)"
	reasonNonRevision  = "entity has no revision column (ADR 0006 excludes it): edge/join rows, per-user config, bounded lists a client re-pulls wholesale"
	reasonMembership   = "membership/join sub-resource add/remove — hard-delete rows, no per-row version token (ADR 0006)"
	reasonCollectionOp = "collection-level operation (bulk / merge / import / suggestion accept-dismiss / apply) — not a single-row conditional replace"
	reasonIntegration  = "per-user integration config or contact link/unlink — no revision column, last-writer-wins on the caller's own settings"
	reasonAction       = "per-row action endpoint on a non-revisioned surface (undo / rotate / test / sync / discuss / accept / dismiss / restore)"
	reasonAuth         = "authentication / account endpoint — no domain row with a revision"
	reasonAdmin        = "admin endpoint — operates on users/system, not a revision-bearing domain row"
	reasonDavProtocol  = "CardDAV/CalDAV protocol handler — keeps its own If-Match path (carddav/backend.go), covered by carddav tests"
)

// buildCWTable declares one row per mutating route. Keyed by "METHOD /path".
func buildCWTable(s seeded) map[string]cwRow {
	contact := s.contact
	uid := s.contactUID
	contactIDNum, _ := strconv.ParseUint(contact, 10, 64)
	contactID := uint(contactIDNum)

	enforced := func(probe string, body func() any) cwRow {
		return cwRow{class: cwEnforced, probe: probe, body: body}
	}
	exempt := func(reason string) cwRow { return cwRow{class: cwExempt, reason: reason} }

	contactBody := func() any {
		return models.ContactRecordInput{Card: contactmodel.Card{
			Name: &contactmodel.Name{Components: []contactmodel.NameComponent{{Kind: "given", Value: "Stale Probe"}}},
		}}
	}
	noteBody := func() any {
		return models.NoteInput{Content: "stale probe", Date: time.Now()}
	}
	activityBody := func() any {
		return models.ActivityInput{Title: "stale probe", Date: time.Now()}
	}
	lifeEventBody := func() any {
		return models.LifeEventInput{EntityID: uid, Type: models.LifeEventTypeMoved, Source: models.LifeEventSourceUser}
	}
	reminderBody := func() any {
		return models.Reminder{Message: "stale probe", RemindAt: time.Now().Add(24 * time.Hour), Recurrence: "once", ContactID: &contactID}
	}

	return map[string]cwRow{
		// === CON-01 enforced: PUT/DELETE on the five revision-bearing entities ===
		"PUT /api/v1/contacts/:id":       enforced("/api/v1/contacts/"+contact, contactBody),
		"DELETE /api/v1/contacts/:id":    enforced("/api/v1/contacts/"+contact, nil),
		"PUT /api/v1/notes/:id":          enforced("/api/v1/notes/"+s.note, noteBody),
		"DELETE /api/v1/notes/:id":       enforced("/api/v1/notes/"+s.note, nil),
		"PUT /api/v1/activities/:id":     enforced("/api/v1/activities/"+s.activity, activityBody),
		"DELETE /api/v1/activities/:id":  enforced("/api/v1/activities/"+s.activity, nil),
		"PUT /api/v1/life-events/:id":    enforced("/api/v1/life-events/"+s.lifeEvent, lifeEventBody),
		"DELETE /api/v1/life-events/:id": enforced("/api/v1/life-events/"+s.lifeEvent, nil),
		"PUT /api/v1/reminders/:id":      enforced("/api/v1/reminders/"+s.reminder, reminderBody),
		"DELETE /api/v1/reminders/:id":   enforced("/api/v1/reminders/"+s.reminder, nil),

		// === Toggles on revision-bearing entities (ADR 0008 exclusion) ===
		"POST /api/v1/contacts/:id/archive":    exempt(reasonToggle),
		"POST /api/v1/contacts/:id/unarchive":  exempt(reasonToggle),
		"POST /api/v1/contacts/:id/favorite":   exempt(reasonToggle),
		"POST /api/v1/contacts/:id/unfavorite": exempt(reasonToggle),
		"POST /api/v1/reminders/:id/complete":  exempt(reasonToggle),

		// === Creates (no prior revision) ===
		"POST /api/v1/contacts":                         exempt(reasonCreate),
		"POST /api/v1/contacts/:id/notes":               exempt(reasonCreate),
		"POST /api/v1/contacts/:id/reminders":           exempt(reasonCreate),
		"POST /api/v1/notes":                            exempt(reasonCreate),
		"POST /api/v1/activities":                       exempt(reasonCreate),
		"POST /api/v1/life-events":                      exempt(reasonCreate),
		"POST /api/v1/circles":                          exempt(reasonCreate),
		"POST /api/v1/households":                       exempt(reasonCreate),
		"POST /api/v1/tags":                             exempt(reasonCreate),
		"POST /api/v1/gifts":                            exempt(reasonCreate),
		"POST /api/v1/preferences":                      exempt(reasonCreate),
		"POST /api/v1/cadence-policies":                 exempt(reasonCreate),
		"POST /api/v1/conversation-agenda":              exempt(reasonCreate),
		"POST /api/v1/field-definitions":                exempt(reasonCreate),
		"POST /api/v1/link-field-types":                 exempt(reasonCreate),
		"POST /api/v1/relationship-edges":               exempt(reasonCreate),
		"POST /api/v1/external-identities":              exempt(reasonCreate),
		"POST /api/v1/external-activities":              exempt(reasonCreate),
		"POST /api/v1/webhooks":                         exempt(reasonCreate),
		"POST /api/v1/calendars":                        exempt(reasonCreate),
		"POST /api/v1/contact-subscriptions":            exempt(reasonCreate),
		"POST /api/v1/contact-shares":                   exempt(reasonCreate),
		"POST /api/v1/api-tokens":                       exempt(reasonCreate),
		"POST /api/v1/notifications/devices":            exempt(reasonCreate),
		"POST /api/v1/notifications/push-subscriptions": exempt(reasonCreate),
		// Issue #722: a device grant is a fresh, revision-free row like an API
		// token.
		"POST /api/v1/auth/device/grants": exempt(reasonCreate),

		// === PUT/DELETE on entities with no revision column (ADR 0006) ===
		"PUT /api/v1/circles/:id":                             exempt(reasonNonRevision),
		"DELETE /api/v1/circles/:id":                          exempt(reasonNonRevision),
		"PUT /api/v1/households/:id":                          exempt(reasonNonRevision),
		"DELETE /api/v1/households/:id":                       exempt(reasonNonRevision),
		"PUT /api/v1/tags/:id":                                exempt(reasonNonRevision),
		"DELETE /api/v1/tags/:id":                             exempt(reasonNonRevision),
		"PUT /api/v1/gifts/:id":                               exempt(reasonNonRevision),
		"DELETE /api/v1/gifts/:id":                            exempt(reasonNonRevision),
		"PUT /api/v1/preferences/:id":                         exempt(reasonNonRevision),
		"DELETE /api/v1/preferences/:id":                      exempt(reasonNonRevision),
		"PUT /api/v1/cadence-policies/:id":                    exempt(reasonNonRevision),
		"DELETE /api/v1/cadence-policies/:id":                 exempt(reasonNonRevision),
		"PUT /api/v1/conversation-agenda/:id":                 exempt(reasonNonRevision),
		"DELETE /api/v1/conversation-agenda/:id":              exempt(reasonNonRevision),
		"PUT /api/v1/field-definitions/:id":                   exempt(reasonNonRevision),
		"DELETE /api/v1/field-definitions/:id":                exempt(reasonNonRevision),
		"PUT /api/v1/link-field-types/:id":                    exempt(reasonNonRevision),
		"DELETE /api/v1/link-field-types/:id":                 exempt(reasonNonRevision),
		"PUT /api/v1/link-field-types/reorder":                exempt(reasonNonRevision),
		"PUT /api/v1/relationship-edges/:id":                  exempt(reasonNonRevision),
		"DELETE /api/v1/relationship-edges/:id":               exempt(reasonNonRevision),
		"PUT /api/v1/external-identities/:id":                 exempt(reasonNonRevision),
		"DELETE /api/v1/external-identities/:id":              exempt(reasonNonRevision),
		"PUT /api/v1/external-activities/:id":                 exempt(reasonNonRevision),
		"DELETE /api/v1/external-activities/:id":              exempt(reasonNonRevision),
		"PUT /api/v1/webhooks/:id":                            exempt(reasonNonRevision),
		"DELETE /api/v1/webhooks/:id":                         exempt(reasonNonRevision),
		"PUT /api/v1/calendars/:id":                           exempt(reasonNonRevision),
		"DELETE /api/v1/calendars/:id":                        exempt(reasonNonRevision),
		"PUT /api/v1/contact-subscriptions/:id":               exempt(reasonNonRevision),
		"DELETE /api/v1/contact-subscriptions/:id":            exempt(reasonNonRevision),
		"PUT /api/v1/contacts/:id/field-values":               exempt(reasonNonRevision),
		"PUT /api/v1/notifications/config":                    exempt(reasonNonRevision),
		"DELETE /api/v1/api-tokens/:id":                       exempt(reasonNonRevision),
		"POST /api/v1/api-tokens/revoke-all":                  exempt(reasonNonRevision),
		"DELETE /api/v1/sessions/:id":                         exempt(reasonNonRevision),
		"DELETE /api/v1/sessions":                             exempt(reasonNonRevision),
		"DELETE /api/v1/attachments/:id":                      exempt(reasonNonRevision),
		"DELETE /api/v1/reminder-completions/:id":             exempt(reasonNonRevision),
		"DELETE /api/v1/notifications/devices/:id":            exempt(reasonNonRevision),
		"DELETE /api/v1/notifications/push-subscriptions/:id": exempt(reasonNonRevision),
		"DELETE /api/v1/auth/device/grants/:id":               exempt(reasonNonRevision),
		"POST /api/v1/auth/device/grants/revoke-all":          exempt(reasonNonRevision),

		// === Membership / join sub-resources ===
		"POST /api/v1/circles/:id/members":                 exempt(reasonMembership),
		"DELETE /api/v1/circles/:id/members/:vcard_uid":    exempt(reasonMembership),
		"POST /api/v1/households/:id/members":              exempt(reasonMembership),
		"DELETE /api/v1/households/:id/members/:vcard_uid": exempt(reasonMembership),
		"PATCH /api/v1/households/:id/members/:vcard_uid":  exempt(reasonMembership),
		"POST /api/v1/tags/:id/contacts":                   exempt(reasonMembership),
		"DELETE /api/v1/tags/:id/contacts/:vcard_uid":      exempt(reasonMembership),

		// === Collection-level operations ===
		"POST /api/v1/contacts/bulk":                        exempt(reasonCollectionOp),
		"POST /api/v1/contacts/merge":                       exempt(reasonCollectionOp),
		"POST /api/v1/contacts/merge/preview":               exempt(reasonCollectionOp),
		"POST /api/v1/contacts/duplicates/dismiss":          exempt(reasonCollectionOp),
		"POST /api/v1/contacts/address-suggestions":         exempt(reasonCollectionOp),
		"POST /api/v1/contacts/address-suggestions/apply":   exempt(reasonCollectionOp),
		"POST /api/v1/households/suggest-addresses":         exempt(reasonCollectionOp),
		"POST /api/v1/households/suggestions/accept":        exempt(reasonCollectionOp),
		"POST /api/v1/households/suggestions/dismiss":       exempt(reasonCollectionOp),
		"POST /api/v1/households/:id/suggest-relationships": exempt(reasonCollectionOp),
		"POST /api/v1/relationship-edges/suggest":           exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/upload":               exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/preview":              exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/confirm":              exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/records":              exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/vcf/upload":           exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/vcf/confirm":          exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/jscontact/upload":     exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/monica/connect":       exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/monica/fetch":         exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/monica/confirm":       exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/monica/cancel":        exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/meerkat/upload":       exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/meerkat/fetch":        exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/meerkat/confirm":      exempt(reasonCollectionOp),
		"POST /api/v1/contacts/import/meerkat/cancel":       exempt(reasonCollectionOp),

		// === Per-row actions on non-revisioned surfaces ===
		"POST /api/v1/audit/:id/undo":                     exempt(reasonAction),
		"POST /api/v1/api-tokens/:id/rotate":              exempt(reasonAction),
		"POST /api/v1/webhooks/:id/test":                  exempt(reasonAction),
		"POST /api/v1/calendars/:id/sync":                 exempt(reasonAction),
		"POST /api/v1/contact-subscriptions/:id/sync":     exempt(reasonAction),
		"POST /api/v1/contact-sync-conflicts/:id/restore": exempt(reasonAction),
		"POST /api/v1/contact-sync-conflicts/:id/dismiss": exempt(reasonAction),
		"POST /api/v1/contact-shares/:id/accept":          exempt(reasonAction),
		"POST /api/v1/contact-shares/:id/confirm":         exempt(reasonAction),
		"POST /api/v1/contact-shares/:id/decline":         exempt(reasonAction),
		"POST /api/v1/reach-out-suggestions/:id/dismiss":  exempt(reasonAction),
		"PATCH /api/v1/relationship-edges/:id/accept":     exempt(reasonAction),
		"PATCH /api/v1/conversation-agenda/:id/discuss":   exempt(reasonAction),
		"POST /api/v1/notifications/config/test":          exempt(reasonAction),

		// === Integrations (per-user config + contact links) ===
		"PUT /api/v1/immich/config":                                       exempt(reasonIntegration),
		"DELETE /api/v1/immich/config":                                    exempt(reasonIntegration),
		"POST /api/v1/immich/test-connection":                             exempt(reasonIntegration),
		"POST /api/v1/immich/sync":                                        exempt(reasonIntegration),
		"POST /api/v1/immich/contacts/:vcard_uid/link":                    exempt(reasonIntegration),
		"DELETE /api/v1/immich/contacts/:vcard_uid/link":                  exempt(reasonIntegration),
		"PUT /api/v1/paperless/config":                                    exempt(reasonIntegration),
		"DELETE /api/v1/paperless/config":                                 exempt(reasonIntegration),
		"POST /api/v1/paperless/test-connection":                          exempt(reasonIntegration),
		"POST /api/v1/paperless/contacts/:vcard_uid/link":                 exempt(reasonIntegration),
		"DELETE /api/v1/paperless/contacts/:vcard_uid/links/:identity_id": exempt(reasonIntegration),
		"PUT /api/v1/seafile/config":                                      exempt(reasonIntegration),
		"DELETE /api/v1/seafile/config":                                   exempt(reasonIntegration),
		"POST /api/v1/seafile/test-connection":                            exempt(reasonIntegration),
		"POST /api/v1/seafile/contacts/:vcard_uid/link":                   exempt(reasonIntegration),
		"DELETE /api/v1/seafile/contacts/:vcard_uid/links/:identity_id":   exempt(reasonIntegration),
		"PUT /api/v1/nextcloud/config":                                    exempt(reasonIntegration),
		"DELETE /api/v1/nextcloud/config":                                 exempt(reasonIntegration),
		"POST /api/v1/nextcloud/test-connection":                          exempt(reasonIntegration),
		"POST /api/v1/nextcloud/contacts/:vcard_uid/link":                 exempt(reasonIntegration),
		"DELETE /api/v1/nextcloud/contacts/:vcard_uid/links/:identity_id": exempt(reasonIntegration),

		// === Auth / account ===
		"POST /api/v1/register":                            exempt(reasonAuth),
		"POST /api/v1/login":                               exempt(reasonAuth),
		"POST /api/v1/login/2fa":                           exempt(reasonAuth),
		"POST /api/v1/auth/device/session":                 exempt(reasonAuth),
		"POST /api/v1/logout":                              exempt(reasonAuth),
		"POST /api/v1/check-password-strength":             exempt(reasonAuth),
		"POST /api/v1/password-reset/request":              exempt(reasonAuth),
		"POST /api/v1/password-reset/confirm":              exempt(reasonAuth),
		"POST /api/v1/users/change-password":               exempt(reasonAuth),
		"PATCH /api/v1/users/language":                     exempt(reasonAuth),
		"PATCH /api/v1/users/date-format":                  exempt(reasonAuth),
		"PATCH /api/v1/users/enabled-contact-fields":       exempt(reasonAuth),
		"PATCH /api/v1/users/me/self-contact":              exempt(reasonAuth),
		"POST /api/v1/users/2fa/setup":                     exempt(reasonAuth),
		"POST /api/v1/users/2fa/confirm":                   exempt(reasonAuth),
		"POST /api/v1/users/2fa/disable":                   exempt(reasonAuth),
		"POST /api/v1/users/2fa/recovery-codes/regenerate": exempt(reasonAuth),

		// === Admin ===
		"POST /api/v1/admin/users":                    exempt(reasonAdmin),
		"PATCH /api/v1/admin/users/:id":               exempt(reasonAdmin),
		"DELETE /api/v1/admin/users/:id":              exempt(reasonAdmin),
		"POST /api/v1/admin/users/:id/reset-2fa":      exempt(reasonAdmin),
		"POST /api/v1/admin/trigger-reminders":        exempt(reasonAdmin),
		"POST /api/v1/admin/trigger-purge":            exempt(reasonAdmin),
		"POST /api/v1/admin/search/rebuild":           exempt(reasonAdmin),
		"POST /api/v1/admin/contacts/rebuild-derived": exempt(reasonAdmin),

		// === Uploads (multipart, not a JSON row replace) ===
		"POST /api/v1/contacts/:id/attachments":     exempt(reasonCollectionOp),
		"POST /api/v1/contacts/:id/profile_picture": exempt(reasonCollectionOp),

		// === DAV protocol ===
		"PUT /carddav/*path":    exempt(reasonDavProtocol),
		"POST /carddav/*path":   exempt(reasonDavProtocol),
		"DELETE /carddav/*path": exempt(reasonDavProtocol),
		"PATCH /carddav/*path":  exempt(reasonDavProtocol),
		"PUT /caldav/*path":     exempt(reasonDavProtocol),
		"POST /caldav/*path":    exempt(reasonDavProtocol),
		"DELETE /caldav/*path":  exempt(reasonDavProtocol),
		"PATCH /caldav/*path":   exempt(reasonDavProtocol),
	}
}

func TestConditionalWriteMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:     "con02-matrix-test-secret-key-that-is-long-enough",
		JWTExpiryHours:   96,
		ProfilePhotoDir:  t.TempDir(),
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
		CardDAVEnabled:   true,
		CalDAVEnabled:    true,
	}
	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	owner := models.User{Username: "con02-owner", Email: "con02-owner@example.com", Password: "password123"}
	require.NoError(t, db.Create(&owner).Error)
	token, err := services.IssueSession(db, owner, cfg, "", "")
	require.NoError(t, err)

	res := seedResources(t, db, owner.ID)
	table := buildCWTable(res)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	RegisterRoutes(router, cfg, db, nil)

	// --- completeness: every mutating route classified, every row still live ---
	registered := map[string]struct{}{}
	var missing []string
	for _, r := range router.Routes() {
		if !isMutating(r.Method) {
			continue
		}
		key := routeKey(r.Method, r.Path)
		registered[key] = struct{}{}
		if _, ok := table[key]; !ok {
			missing = append(missing, key)
		}
	}
	var stale []string
	for key := range table {
		if _, ok := registered[key]; !ok {
			stale = append(stale, key)
		}
	}
	require.Empty(t, missing,
		"mutating routes with no conditional-write classification — add a row to buildCWTable "+
			"(cwEnforced if it is a PUT/DELETE on a revision-bearing entity, cwExempt with a reason otherwise):\n  %s",
		strings.Join(missing, "\n  "))
	require.Empty(t, stale,
		"conditional-write rows with no matching registered route (stale):\n  %s", strings.Join(stale, "\n  "))

	// --- live probe: every cwEnforced route rejects a stale If-Match with 412
	// and leaves the row byte-identical ------------------------------------
	for key, row := range table {
		if row.class != cwEnforced {
			continue
		}
		t.Run(key, func(t *testing.T) {
			method, _, _ := strings.Cut(key, " ")
			rowID := row.probe[strings.LastIndex(row.probe, "/")+1:]

			before := snapshotRow(t, db, key, rowID)

			var bodyReader *bytes.Reader
			if row.body != nil {
				raw, err := json.Marshal(row.body())
				require.NoError(t, err)
				bodyReader = bytes.NewReader(raw)
			} else {
				bodyReader = bytes.NewReader(nil)
			}
			req, err := http.NewRequest(method, row.probe, bodyReader)
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			// A revision guaranteed never to be current.
			req.Header.Set("If-Match", `"999999999"`)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusPreconditionFailed, w.Code,
				"%s with a stale If-Match must be rejected with 412; body: %s", key, w.Body.String())

			var env struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
			require.Equal(t, "PRECONDITION_FAILED", env.Error.Code)

			after := snapshotRow(t, db, key, rowID)
			require.Equal(t, before, after,
				"%s: a rejected conditional write must leave the stored row byte-identical", key)
		})
	}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// snapshotRow returns a stable JSON snapshot of the row identified by id on the
// entity the enforced route targets, for the byte-identical before/after
// comparison around a rejected conditional write.
func snapshotRow(t *testing.T, db *gorm.DB, key, id string) string {
	t.Helper()
	marshal := func(v any) string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return string(b)
	}
	switch {
	case strings.Contains(key, "/contacts/:id"):
		var c models.Contact
		require.NoError(t, db.First(&c, id).Error)
		return marshal(c)
	case strings.Contains(key, "/notes/:id"):
		var n models.Note
		require.NoError(t, db.First(&n, id).Error)
		return marshal(n)
	case strings.Contains(key, "/activities/:id"):
		var a models.Activity
		require.NoError(t, db.First(&a, id).Error)
		return marshal(a)
	case strings.Contains(key, "/life-events/:id"):
		var l models.LifeEvent
		require.NoError(t, db.Where("id = ?", id).First(&l).Error)
		return marshal(l)
	case strings.Contains(key, "/reminders/:id"):
		var r models.Reminder
		require.NoError(t, db.First(&r, id).Error)
		return marshal(r)
	}
	t.Fatalf("snapshotRow: unhandled enforced key %q", key)
	return ""
}
