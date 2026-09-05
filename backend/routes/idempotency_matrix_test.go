package routes

// TestIdempotencyMatrix is CON-04 (issue #459): every mutating route
// (POST/PUT/PATCH/DELETE) enumerated from the live router must carry an
// idempotency classification, so a new mutation cannot be added without a
// deliberate decision about retry-safety — the #371 route-enumeration pattern,
// the same shape as TestAuthorizationMatrix and TestConditionalWriteMatrix.
//
// Buckets (ADR 0010):
//
//   - idemNatural  — PUT / DELETE / PATCH. Re-applying is a no-op (PUT/DELETE
//     to a known id) or a checked 409 (membership adds). No mechanism needed;
//     the assertion is just "it is one of these methods".
//   - idemKeyed    — a POST that mints a new id, appends, or has a side effect
//     beyond the database. Covered by middleware.IdempotencyMiddleware: a
//     retry carrying the same Idempotency-Key replays the stored response
//     without re-running the handler.
//   - idemExempt   — a POST that is safe to repeat regardless (a read-only
//     computation, an idempotent state toggle, an already-de-duplicated
//     action, or an auth/admin/DAV surface), each with a recorded reason.
//
// A registered mutating route missing from the table fails the completeness
// check; a table row with no registered route fails as stale.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"gorm.io/gorm/logger"
)

type idemClass int

const (
	idemNatural idemClass = iota
	idemKeyed
	idemExempt
)

type idemRow struct {
	class  idemClass
	reason string
}

const (
	idemReasonCreate     = "POST mints a new row / appends — retry-protected by Idempotency-Key (ADR 0010)"
	idemReasonSideFx     = "POST with a side effect beyond the DB (webhook/push/email) — retry-protected by Idempotency-Key so the side effect fires once"
	idemReasonQuery      = "POST is a read-only computation (preview / suggestion / search-shaped) — repeating it produces the same answer, no row minted"
	idemReasonToggle     = "POST is an idempotent state toggle — setting the flag again is a no-op"
	idemReasonDedup      = "POST is already de-duplicated by its own key (job lock / natural key / delivery key)"
	idemReasonAuth       = "authentication / account endpoint — no domain row minted; its own anti-replay applies"
	idemReasonAdmin      = "admin trigger/management endpoint — operator-driven, re-runnable by design"
	idemReasonDav        = "CardDAV/CalDAV protocol handler — its own If-Match / UID semantics, outside the REST idempotency middleware"
	idemReasonMembership = "membership add guarded by a natural-key unique index — a duplicate is a checked 409 ErrAlreadyExists, never a second row"
)

func buildIdemTable() map[string]idemRow {
	natural := idemRow{class: idemNatural}
	keyed := func(r string) idemRow { return idemRow{class: idemKeyed, reason: r} }
	exempt := func(r string) idemRow { return idemRow{class: idemExempt, reason: r} }

	t := map[string]idemRow{}

	// Every PUT / PATCH / DELETE is naturally idempotent — enumerate from the
	// same dump the conditional-write matrix uses, but here the method alone
	// decides the bucket, so collapse it to a method check below and only
	// declare the POSTs explicitly. (A PUT/PATCH/DELETE with no row is still
	// naturally idempotent.)
	_ = natural

	// --- idemKeyed: creates / appends / side-effecting POSTs --------------
	for _, k := range []string{
		"POST /api/v1/contacts",
		"POST /api/v1/contacts/:id/notes",
		"POST /api/v1/contacts/:id/reminders",
		"POST /api/v1/notes",
		"POST /api/v1/activities",
		"POST /api/v1/life-events",
		"POST /api/v1/circles",
		"POST /api/v1/households",
		"POST /api/v1/tags",
		"POST /api/v1/gifts",
		"POST /api/v1/preferences",
		"POST /api/v1/cadence-policies",
		"POST /api/v1/conversation-agenda",
		"POST /api/v1/field-definitions",
		"POST /api/v1/link-field-types",
		"POST /api/v1/relationship-edges",
		"POST /api/v1/external-identities",
		"POST /api/v1/external-activities",
		"POST /api/v1/calendars",
		"POST /api/v1/contact-subscriptions",
		"POST /api/v1/contact-shares",
		"POST /api/v1/api-tokens",
		"POST /api/v1/auth/device/grants",
		"POST /api/v1/notifications/devices",
		"POST /api/v1/notifications/push-subscriptions",
	} {
		t[k] = keyed(idemReasonCreate)
	}
	for _, k := range []string{
		"POST /api/v1/webhooks",
		"POST /api/v1/immich/contacts/:vcard_uid/link",
		"POST /api/v1/paperless/contacts/:vcard_uid/link",
		"POST /api/v1/seafile/contacts/:vcard_uid/link",
		"POST /api/v1/nextcloud/contacts/:vcard_uid/link",
	} {
		t[k] = keyed(idemReasonSideFx)
	}

	// --- idemExempt: safe-to-repeat POSTs -------------------------------
	for _, k := range []string{
		"POST /api/v1/contacts/merge/preview",
		"POST /api/v1/contacts/address-suggestions",
		"POST /api/v1/contacts/address-suggestions/apply",
		"POST /api/v1/contacts/duplicates/dismiss",
		"POST /api/v1/households/suggest-addresses",
		"POST /api/v1/households/suggestions/accept",
		"POST /api/v1/households/suggestions/dismiss",
		"POST /api/v1/households/:id/suggest-relationships",
		"POST /api/v1/relationship-edges/suggest",
		"POST /api/v1/contacts/bulk",
		"POST /api/v1/contacts/merge",
	} {
		t[k] = exempt(idemReasonQuery)
	}
	for _, k := range []string{
		"POST /api/v1/contacts/:id/archive",
		"POST /api/v1/contacts/:id/unarchive",
		"POST /api/v1/contacts/:id/favorite",
		"POST /api/v1/contacts/:id/unfavorite",
		"POST /api/v1/contact-shares/:id/accept",
		"POST /api/v1/contact-shares/:id/confirm",
		"POST /api/v1/contact-shares/:id/decline",
		"POST /api/v1/reach-out-suggestions/:id/dismiss",
		"POST /api/v1/contact-sync-conflicts/:id/restore",
		"POST /api/v1/contact-sync-conflicts/:id/dismiss",
	} {
		t[k] = exempt(idemReasonToggle)
	}
	for _, k := range []string{
		"POST /api/v1/reminders/:id/complete",
		"POST /api/v1/webhooks/:id/test",
		"POST /api/v1/calendars/:id/sync",
		"POST /api/v1/contact-subscriptions/:id/sync",
		"POST /api/v1/immich/sync",
		"POST /api/v1/api-tokens/:id/rotate",
		"POST /api/v1/api-tokens/revoke-all",
		"POST /api/v1/auth/device/grants/revoke-all",
		"POST /api/v1/notifications/config/test",
		"POST /api/v1/audit/:id/undo",
		"POST /api/v1/immich/test-connection",
		"POST /api/v1/paperless/test-connection",
		"POST /api/v1/seafile/test-connection",
		"POST /api/v1/nextcloud/test-connection",
	} {
		t[k] = exempt(idemReasonDedup)
	}
	for _, k := range []string{
		"POST /api/v1/circles/:id/members",
		"POST /api/v1/households/:id/members",
		"POST /api/v1/tags/:id/contacts",
	} {
		t[k] = exempt(idemReasonMembership)
	}
	// Imports: multi-step wizards (connect/fetch/preview/confirm/cancel). Each
	// step is operator-driven and re-runnable; the confirm step is guarded by
	// its own session/idempotency handling inside the import service.
	for _, k := range []string{
		"POST /api/v1/contacts/import/upload",
		"POST /api/v1/contacts/import/preview",
		"POST /api/v1/contacts/import/confirm",
		"POST /api/v1/contacts/import/records",
		"POST /api/v1/contacts/import/vcf/upload",
		"POST /api/v1/contacts/import/vcf/confirm",
		"POST /api/v1/contacts/import/jscontact/upload",
		"POST /api/v1/contacts/import/monica/connect",
		"POST /api/v1/contacts/import/monica/fetch",
		"POST /api/v1/contacts/import/monica/confirm",
		"POST /api/v1/contacts/import/monica/cancel",
		"POST /api/v1/contacts/import/meerkat/upload",
		"POST /api/v1/contacts/import/meerkat/fetch",
		"POST /api/v1/contacts/import/meerkat/confirm",
		"POST /api/v1/contacts/import/meerkat/cancel",
	} {
		t[k] = exempt(idemReasonDedup)
	}
	for _, k := range []string{
		"POST /api/v1/register",
		"POST /api/v1/login",
		"POST /api/v1/login/2fa",
		"POST /api/v1/auth/device/session",
		"POST /api/v1/logout",
		"POST /api/v1/check-password-strength",
		"POST /api/v1/password-reset/request",
		"POST /api/v1/password-reset/confirm",
		"POST /api/v1/users/change-password",
		"POST /api/v1/users/2fa/setup",
		"POST /api/v1/users/2fa/confirm",
		"POST /api/v1/users/2fa/disable",
		"POST /api/v1/users/2fa/recovery-codes/regenerate",
	} {
		t[k] = exempt(idemReasonAuth)
	}
	for _, k := range []string{
		"POST /api/v1/admin/users",
		"POST /api/v1/admin/users/:id/reset-2fa",
		"POST /api/v1/admin/trigger-reminders",
		"POST /api/v1/admin/trigger-purge",
		"POST /api/v1/admin/search/rebuild",
		"POST /api/v1/admin/contacts/rebuild-derived",
	} {
		t[k] = exempt(idemReasonAdmin)
	}
	for _, k := range []string{
		"POST /api/v1/contacts/:id/attachments",
		"POST /api/v1/contacts/:id/profile_picture",
	} {
		t[k] = keyed(idemReasonCreate)
	}
	for _, k := range []string{"POST /carddav/*path", "POST /caldav/*path"} {
		t[k] = exempt(idemReasonDav)
	}
	return t
}

func TestIdempotencyMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey: "con04-matrix-secret-key-that-is-long-enough",
		FrontendURL:  "http://localhost:5173", Port: "7300",
		ReminderTime: "12:00", ReminderTimezone: "UTC",
		CardDAVEnabled: true, CalDAVEnabled: true,
	}
	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("db", db); c.Set("cfg", *cfg); c.Next() })
	RegisterRoutes(router, cfg, db, nil)

	table := buildIdemTable()

	registered := map[string]struct{}{}
	var missing []string
	for _, r := range router.Routes() {
		if !isMutating(r.Method) {
			continue
		}
		key := routeKey(r.Method, r.Path)
		registered[key] = struct{}{}
		if r.Method != http.MethodPost {
			continue // PUT/PATCH/DELETE are idemNatural by method; no row needed
		}
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
		"POST routes with no idempotency classification — add a row to buildIdemTable "+
			"(idemKeyed if it mints a row / has a side effect, idemExempt with a reason otherwise):\n  %s",
		strings.Join(missing, "\n  "))
	require.Empty(t, stale,
		"idempotency rows with no matching registered route (stale):\n  %s", strings.Join(stale, "\n  "))
}

// TestIdempotencyMatrix_KeyedCreateReplaysThroughRealRouter drives a real
// keyed POST twice through the fully-wired router (RegisterRoutes, so the
// production middleware chain including IdempotencyMiddleware) and asserts the
// second call is a replay: one contact, and the byte-identical stored body.
func TestIdempotencyMatrix_KeyedCreateReplaysThroughRealRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:   "con04-replay-secret-key-that-is-long-enough",
		JWTExpiryHours: 96, ProfilePhotoDir: t.TempDir(),
		FrontendURL: "http://localhost:5173", Port: "7300",
		ReminderTime: "12:00", ReminderTimezone: "UTC",
	}
	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	user := models.User{Username: "con04-replay", Email: "con04-replay@example.com", Password: "password123"}
	require.NoError(t, db.Create(&user).Error)
	token, err := services.GenerateToken(user, cfg)
	require.NoError(t, err)

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("db", db); c.Set("cfg", *cfg); c.Next() })
	RegisterRoutes(router, cfg, db, nil)

	body, err := json.Marshal(models.ContactRecordInput{Card: contactmodel.Card{
		Name: &contactmodel.Name{Components: []contactmodel.NameComponent{{Kind: "given", Value: "Retry Me"}}},
	}})
	require.NoError(t, err)

	post := func() *httptest.ResponseRecorder {
		req, _ := http.NewRequest("POST", "/api/v1/contacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", "contact-create-key-1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	first := post()
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	require.Empty(t, first.Header().Get("Idempotency-Replayed"))

	second := post()
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	require.Equal(t, "true", second.Header().Get("Idempotency-Replayed"))
	require.JSONEq(t, first.Body.String(), second.Body.String())

	var contacts int64
	require.NoError(t, db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&contacts).Error)
	require.EqualValues(t, 1, contacts, "the retried keyed create produced exactly one contact")
}
