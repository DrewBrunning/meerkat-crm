package routes

// TestAuthorizationMatrix is the single exhaustive authorization surface for
// this API: every registered route × the six personas from the security-review
// plan (see issue #371), asserted against a real database.InitDB-migrated
// schema (CLAUDE.md backend trap #1 — never AutoMigrate for persistence).
//
// The route list is enumerated from the live router (router.Routes()), so a
// route added to routes.go is automatically included, and one that has no
// declared authorization row FAILS the test — a forgotten route is a failure,
// not a silent gap. The reverse direction is checked too: a declared row with
// no matching registered route is a stale-row failure.
//
// Personas (the six columns of the matrix):
//
//  1. unauth    — no token at all.
//  2. owner     — user A, the authenticated resource owner, touching their own
//     resource.
//  3. intruder  — user B (valid token) touching user A's resource: the
//     BOLA/IDOR probe.
//  4. admin     — an admin account. Admin routes admit it; non-admin routes
//     treat it like any other authenticated (non-owner) user.
//  5. disabled  — a soft-deleted user whose token is still cryptographically
//     valid. AuthMiddleware's user lookup misses the row, so it must 401.
//  6. expired   — a well-formed token with an exp in the past. Must 401.
//
// Expected verdicts per route class (the matrix shape 200/403/401, extended
// with the two legitimate scoped-outcome statuses 403/404 for the BOLA cell):
//
//	class        unauth   owner    intruder  admin    disabled  expired
//	public       admitted*        (no auth boundary; probed with unauth only)
//	protected    401      admitted admitted  admitted 401       401
//	item         401      admitted 404/403* 404/403*  401       401
//	admin        401      403      403       admitted 401       401
//
// "admitted" means the auth layer lets the caller through (status is not 401
// and not 403); it does not assert a specific 2xx because write routes probed
// with an empty body legitimately 400 on validation, and that is a validation
// concern, not an authorization one — the matrix owns the authz boundary, not
// request-shape fidelity. "404/403*" is the scoped BOLA outcome: a GET item
// probe against a seeded owner resource must 404 or 403 (never 2xx — a 2xx is
// an IDOR leak); a non-GET item probe (empty body) must simply not 2xx.
//
// The "owner" and "intruder" personas probe every item route against a real
// seeded owner-owned resource so a handler that forgot its user_id scope would
// actually return the row (2xx) and be caught. Destructive probes (DELETE etc.)
// run in a final "owner" pass so the seed resources stay intact for every BOLA
// probe that precedes them.

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type persona int

const (
	personaUnauth persona = iota
	personaOwner
	personaIntruder
	personaAdmin
	personaDisabled
	personaExpired
)

var personaNames = map[persona]string{
	personaUnauth:   "unauth",
	personaOwner:    "owner",
	personaIntruder: "intruder",
	personaAdmin:    "admin",
	personaDisabled: "disabled",
	personaExpired:  "expired",
}

type routeClass int

const (
	classPublic routeClass = iota
	classProtected
	classItem
	classAdmin
)

// authzRow is one declared authorization row. probe is the concrete path used
// for the item-route BOLA probe (owner resource ids already substituted);
// empty for every other class.
type authzRow struct {
	class routeClass
	probe string
}

// fabricated ids used for item routes whose resource is not seeded (loose
// "not 2xx" BOLA probe — the resource not existing is itself enough to prove
// no cross-account success).
const (
	fabricatedNum  = "99999999"
	fabricatedUUID = "00000000-0000-4000-8000-000000000000"
)

// seeded holds the concrete resource ids of the owner-owned fixtures the item
// routes' BOLA probes are run against.
type seeded struct {
	contact     string // numeric id of the contact probed by /contacts/:id
	contactUID  string
	note        string
	activity    string
	reminder    string
	circle      string
	household   string
	tag         string
	lifeEvent   string
	gift        string
	preference  string
	cadence     string
	agenda      string
	edge        string
	fieldDef    string
	linkType    string
	extIdentity string
	extActivity string
	webhook     string
}

func seedResources(t *testing.T, db *gorm.DB, ownerID uint) seeded {
	t.Helper()
	c := models.Contact{UserID: ownerID, Firstname: "Matrix Owner"}
	require.NoError(t, db.Create(&c).Error)
	ec := models.Contact{UserID: ownerID, Firstname: "Matrix Entity"}
	require.NoError(t, db.Create(&ec).Error)

	n := models.Note{UserID: ownerID, Content: "matrix note", Date: time.Now(), ContactID: &ec.ID}
	require.NoError(t, db.Create(&n).Error)
	a := models.Activity{UserID: ownerID, Title: "matrix activity", Date: time.Now()}
	require.NoError(t, db.Create(&a).Error)
	r := models.Reminder{UserID: ownerID, Message: "matrix reminder", RemindAt: time.Now().Add(24 * time.Hour), Recurrence: "once", ContactID: &ec.ID}
	require.NoError(t, db.Create(&r).Error)

	circle := models.Circle{UserID: ownerID, Name: "matrix circle"}
	require.NoError(t, db.Create(&circle).Error)
	household := models.Household{UserID: ownerID, Name: "matrix household", Type: models.HouseholdTypeOther}
	require.NoError(t, db.Create(&household).Error)
	tag := models.Tag{UserID: ownerID, Name: "matrix tag"}
	require.NoError(t, db.Create(&tag).Error)

	lifeEvent := models.LifeEvent{UserID: ownerID, EntityID: ec.VCardUID, Type: models.LifeEventTypeMoved}
	require.NoError(t, db.Create(&lifeEvent).Error)
	gift := models.Gift{UserID: ownerID, EntityID: ec.VCardUID}
	require.NoError(t, db.Create(&gift).Error)
	pref := models.Preference{UserID: ownerID, EntityID: ec.VCardUID, Category: "food", Value: "pizza"}
	require.NoError(t, db.Create(&pref).Error)
	cadence := models.CadencePolicy{UserID: ownerID, EntityID: ec.VCardUID, TargetIntervalDays: 30}
	require.NoError(t, db.Create(&cadence).Error)
	agenda := models.ConversationAgenda{UserID: ownerID, EntityID: ec.VCardUID, Content: "matrix agenda"}
	require.NoError(t, db.Create(&agenda).Error)

	edge := models.RelationshipEdge{
		UserID: ownerID, SourceID: ec.VCardUID, TargetID: c.VCardUID,
		Type: "friend_of", Directional: false,
		Source: models.RelationshipSourceUserConfirmed, Confidence: 1,
		Status: models.RelationshipStatusConfirmed,
	}
	require.NoError(t, db.Create(&edge).Error)

	fieldDef := models.FieldDefinition{UserID: ownerID, Label: "matrix custom", Key: "matrix_custom", Target: "contact", Type: "string"}
	require.NoError(t, db.Create(&fieldDef).Error)
	linkType := models.LinkFieldType{UserID: ownerID, Name: "whatsapp", Protocol: "https://wa.me/{value}", Category: models.LinkFieldTypeCategoryMessaging}
	require.NoError(t, db.Create(&linkType).Error)

	extIdentity := models.ExternalIdentity{UserID: ownerID, EntityID: ec.VCardUID, System: "immich", ExternalID: "matrix-person-1"}
	require.NoError(t, db.Create(&extIdentity).Error)
	extActivity := models.ExternalActivity{UserID: ownerID, EntityID: ec.VCardUID, SourceSystem: "immich", ExternalID: "matrix-asset-1", Type: "photo-appearance", OccurredAt: time.Now()}
	require.NoError(t, db.Create(&extActivity).Error)

	webhook := models.Webhook{UserID: ownerID, Name: "matrix webhook", URL: "https://example.com/hook", Secret: "secret", Events: []string{}}
	require.NoError(t, db.Create(&webhook).Error)

	return seeded{
		contact:     strconv.FormatUint(uint64(c.ID), 10),
		contactUID:  c.VCardUID,
		note:        strconv.FormatUint(uint64(n.ID), 10),
		activity:    strconv.FormatUint(uint64(a.ID), 10),
		reminder:    strconv.FormatUint(uint64(r.ID), 10),
		circle:      circle.ID,
		household:   household.ID,
		tag:         tag.ID,
		lifeEvent:   lifeEvent.ID,
		gift:        gift.ID,
		preference:  pref.ID,
		cadence:     cadence.ID,
		agenda:      agenda.ID,
		edge:        edge.ID,
		fieldDef:    fieldDef.ID,
		linkType:    linkType.ID,
		extIdentity: extIdentity.ID,
		extActivity: extActivity.ID,
		webhook:     strconv.FormatUint(uint64(webhook.ID), 10),
	}
}

// buildTable declares one authorization row per registered route. The class
// captures the authz boundary; the probe path (item routes) names the seeded
// owner resource the BOLA probe targets. Integration routes (immich/
// paperless/seafile/nextcloud) and join/action routes without a seeded
// resource are item-class with a fabricated id — they still get the "not 2xx"
// BOLA probe, just not the stricter "must 404/403 on a real row" check.
func buildTable(s seeded) map[string]authzRow {
	uid := s.contactUID
	contact := s.contact
	return map[string]authzRow{
		// --- public (no auth boundary) -------------------------------------
		"GET /health":                          {class: classPublic},
		"GET /health/live":                     {class: classPublic},
		"GET /health/ready":                    {class: classPublic},
		"GET /api/v1/auth/oidc/config":         {class: classPublic},
		"POST /api/v1/register":                {class: classPublic},
		"POST /api/v1/login":                   {class: classPublic},
		"POST /api/v1/login/2fa":               {class: classPublic},
		"POST /api/v1/logout":                  {class: classPublic},
		"POST /api/v1/check-password-strength": {class: classPublic},
		"POST /api/v1/password-reset/request":  {class: classPublic},
		"POST /api/v1/password-reset/confirm":  {class: classPublic},
		// Issue #722: possession of an unrevoked device grant exchanges for a
		// session without a password — public (rate-limited like /login), never
		// a passwordless back door for a stale API session.
		"POST /api/v1/auth/device/session": {class: classPublic},

		// --- admin routes --------------------------------------------------
		"GET /api/v1/admin/users":                     {class: classAdmin},
		"POST /api/v1/admin/users":                    {class: classAdmin},
		"GET /api/v1/admin/users/:id":                 {class: classAdmin},
		"PATCH /api/v1/admin/users/:id":               {class: classAdmin},
		"DELETE /api/v1/admin/users/:id":              {class: classAdmin},
		"POST /api/v1/admin/users/:id/reset-2fa":      {class: classAdmin},
		"POST /api/v1/admin/trigger-reminders":        {class: classAdmin},
		"POST /api/v1/admin/trigger-purge":            {class: classAdmin},
		"POST /api/v1/admin/search/rebuild":           {class: classAdmin},
		"POST /api/v1/admin/contacts/rebuild-derived": {class: classAdmin},
		"GET /api/v1/admin/system-events":             {class: classAdmin},
		"GET /api/v1/admin/subsystem-health":          {class: classAdmin},
		"GET /api/v1/admin/job-runs":                  {class: classAdmin},
		"GET /api/v1/admin/job-runs/health":           {class: classAdmin},
		"GET /api/v1/admin/error-aggregation":         {class: classAdmin},
		"GET /api/v1/admin/notification-health":       {class: classAdmin},
		"GET /api/v1/admin/diagnostics":               {class: classAdmin},
		"GET /api/v1/admin/integrity-check":           {class: classAdmin},
		"GET /api/v1/admin/system-status":             {class: classAdmin},

		// --- user / dashboard ----------------------------------------------
		"POST /api/v1/users/change-password":               {class: classProtected},
		"PATCH /api/v1/users/language":                     {class: classProtected},
		"PATCH /api/v1/users/date-format":                  {class: classProtected},
		"GET /api/v1/users/enabled-contact-fields":         {class: classProtected},
		"PATCH /api/v1/users/enabled-contact-fields":       {class: classProtected},
		"GET /api/v1/users/me":                             {class: classProtected},
		"PATCH /api/v1/users/me/self-contact":              {class: classProtected},
		"GET /api/v1/users/2fa/status":                     {class: classProtected},
		"POST /api/v1/users/2fa/setup":                     {class: classProtected},
		"POST /api/v1/users/2fa/confirm":                   {class: classProtected},
		"POST /api/v1/users/2fa/disable":                   {class: classProtected},
		"POST /api/v1/users/2fa/recovery-codes/regenerate": {class: classProtected},
		"GET /api/v1/users/directory":                      {class: classProtected},
		"GET /api/v1/dashboard":                            {class: classProtected},

		// --- contacts (collections + actions) ------------------------------
		"GET /api/v1/contacts":                            {class: classProtected},
		"GET /api/v1/contacts/circles":                    {class: classProtected},
		"GET /api/v1/contacts/random":                     {class: classProtected},
		"GET /api/v1/contacts/birthdays":                  {class: classProtected},
		"POST /api/v1/contacts/merge/preview":             {class: classProtected},
		"POST /api/v1/contacts/merge":                     {class: classProtected},
		"POST /api/v1/contacts":                           {class: classProtected},
		"POST /api/v1/contacts/bulk":                      {class: classProtected},
		"GET /api/v1/contacts/duplicates":                 {class: classProtected},
		"POST /api/v1/contacts/duplicates/dismiss":        {class: classProtected},
		"POST /api/v1/contacts/address-suggestions":       {class: classProtected},
		"POST /api/v1/contacts/address-suggestions/apply": {class: classProtected},

		// --- contacts (item routes) -----------------------------------------
		"GET /api/v1/contacts/:id":                      {class: classItem, probe: "/api/v1/contacts/" + contact},
		"PUT /api/v1/contacts/:id":                      {class: classItem, probe: "/api/v1/contacts/" + contact},
		"DELETE /api/v1/contacts/:id":                   {class: classItem, probe: "/api/v1/contacts/" + contact},
		"GET /api/v1/contacts/:id/briefing":             {class: classItem, probe: "/api/v1/contacts/" + contact + "/briefing"},
		"GET /api/v1/contacts/:id/detail":               {class: classItem, probe: "/api/v1/contacts/" + contact + "/detail"},
		"GET /api/v1/contacts/:id/timeline":             {class: classItem, probe: "/api/v1/contacts/" + contact + "/timeline"},
		"POST /api/v1/contacts/:id/archive":             {class: classItem, probe: "/api/v1/contacts/" + contact + "/archive"},
		"POST /api/v1/contacts/:id/unarchive":           {class: classItem, probe: "/api/v1/contacts/" + contact + "/unarchive"},
		"POST /api/v1/contacts/:id/favorite":            {class: classItem, probe: "/api/v1/contacts/" + contact + "/favorite"},
		"POST /api/v1/contacts/:id/unfavorite":          {class: classItem, probe: "/api/v1/contacts/" + contact + "/unfavorite"},
		"GET /api/v1/contacts/:id/attachments":          {class: classItem, probe: "/api/v1/contacts/" + contact + "/attachments"},
		"POST /api/v1/contacts/:id/attachments":         {class: classItem, probe: "/api/v1/contacts/" + contact + "/attachments"},
		"POST /api/v1/contacts/:id/profile_picture":     {class: classItem, probe: "/api/v1/contacts/" + contact + "/profile_picture"},
		"GET /api/v1/contacts/:id/profile_picture":      {class: classItem, probe: "/api/v1/contacts/" + contact + "/profile_picture"},
		"GET /api/v1/contacts/:id/notes":                {class: classItem, probe: "/api/v1/contacts/" + contact + "/notes"},
		"POST /api/v1/contacts/:id/notes":               {class: classItem, probe: "/api/v1/contacts/" + contact + "/notes"},
		"GET /api/v1/contacts/:id/activities":           {class: classItem, probe: "/api/v1/contacts/" + contact + "/activities"},
		"GET /api/v1/contacts/:id/field-values":         {class: classItem, probe: "/api/v1/contacts/" + contact + "/field-values"},
		"PUT /api/v1/contacts/:id/field-values":         {class: classItem, probe: "/api/v1/contacts/" + contact + "/field-values"},
		"GET /api/v1/contacts/:id/reminders":            {class: classItem, probe: "/api/v1/contacts/" + contact + "/reminders"},
		"POST /api/v1/contacts/:id/reminders":           {class: classItem, probe: "/api/v1/contacts/" + contact + "/reminders"},
		"GET /api/v1/contacts/:id/reminder-completions": {class: classItem, probe: "/api/v1/contacts/" + contact + "/reminder-completions"},

		// --- contact import -------------------------------------------------
		"POST /api/v1/contacts/import/upload":           {class: classProtected},
		"POST /api/v1/contacts/import/preview":          {class: classProtected},
		"POST /api/v1/contacts/import/confirm":          {class: classProtected},
		"POST /api/v1/contacts/import/vcf/upload":       {class: classProtected},
		"POST /api/v1/contacts/import/vcf/confirm":      {class: classProtected},
		"POST /api/v1/contacts/import/jscontact/upload": {class: classProtected},
		"POST /api/v1/contacts/import/records":          {class: classProtected},
		"GET /api/v1/contacts/import/history":           {class: classProtected},
		"POST /api/v1/contacts/import/monica/connect":   {class: classProtected},
		"POST /api/v1/contacts/import/monica/fetch":     {class: classProtected},
		"GET /api/v1/contacts/import/monica/status":     {class: classProtected},
		"GET /api/v1/contacts/import/monica/preview":    {class: classProtected},
		"POST /api/v1/contacts/import/monica/confirm":   {class: classProtected},
		"POST /api/v1/contacts/import/monica/cancel":    {class: classProtected},
		"POST /api/v1/contacts/import/meerkat/upload":   {class: classProtected},
		"POST /api/v1/contacts/import/meerkat/fetch":    {class: classProtected},
		"GET /api/v1/contacts/import/meerkat/status":    {class: classProtected},
		"GET /api/v1/contacts/import/meerkat/preview":   {class: classProtected},
		"POST /api/v1/contacts/import/meerkat/confirm":  {class: classProtected},
		"POST /api/v1/contacts/import/meerkat/cancel":   {class: classProtected},

		// --- contact shares -------------------------------------------------
		"POST /api/v1/contact-shares":             {class: classProtected},
		"GET /api/v1/contact-shares/incoming":     {class: classProtected},
		"GET /api/v1/contact-shares/outgoing":     {class: classProtected},
		"POST /api/v1/contact-shares/:id/accept":  {class: classItem, probe: "/api/v1/contact-shares/" + fabricatedUUID + "/accept"},
		"POST /api/v1/contact-shares/:id/confirm": {class: classItem, probe: "/api/v1/contact-shares/" + fabricatedUUID + "/confirm"},
		"POST /api/v1/contact-shares/:id/decline": {class: classItem, probe: "/api/v1/contact-shares/" + fabricatedUUID + "/decline"},

		// --- relationship edges ---------------------------------------------
		"POST /api/v1/relationship-edges":             {class: classProtected},
		"GET /api/v1/relationship-edges":              {class: classProtected},
		"POST /api/v1/relationship-edges/suggest":     {class: classProtected},
		"GET /api/v1/relationship-edges/:id":          {class: classItem, probe: "/api/v1/relationship-edges/" + s.edge},
		"PUT /api/v1/relationship-edges/:id":          {class: classItem, probe: "/api/v1/relationship-edges/" + s.edge},
		"DELETE /api/v1/relationship-edges/:id":       {class: classItem, probe: "/api/v1/relationship-edges/" + s.edge},
		"PATCH /api/v1/relationship-edges/:id/accept": {class: classItem, probe: "/api/v1/relationship-edges/" + s.edge + "/accept"},

		// --- attachments ----------------------------------------------------
		"GET /api/v1/attachments/:id/download": {class: classItem, probe: "/api/v1/attachments/" + fabricatedNum + "/download"},
		"DELETE /api/v1/attachments/:id":       {class: classItem, probe: "/api/v1/attachments/" + fabricatedNum},

		// --- image proxy ----------------------------------------------------
		"GET /api/v1/proxy/image": {class: classProtected},

		// --- notes ----------------------------------------------------------
		"GET /api/v1/notes":        {class: classProtected},
		"POST /api/v1/notes":       {class: classProtected},
		"GET /api/v1/notes/:id":    {class: classItem, probe: "/api/v1/notes/" + s.note},
		"PUT /api/v1/notes/:id":    {class: classItem, probe: "/api/v1/notes/" + s.note},
		"DELETE /api/v1/notes/:id": {class: classItem, probe: "/api/v1/notes/" + s.note},

		// --- activities -----------------------------------------------------
		"POST /api/v1/activities":       {class: classProtected},
		"GET /api/v1/activities":        {class: classProtected},
		"GET /api/v1/activities/:id":    {class: classItem, probe: "/api/v1/activities/" + s.activity},
		"PUT /api/v1/activities/:id":    {class: classItem, probe: "/api/v1/activities/" + s.activity},
		"DELETE /api/v1/activities/:id": {class: classItem, probe: "/api/v1/activities/" + s.activity},

		// --- circles --------------------------------------------------------
		"POST /api/v1/circles":                          {class: classProtected},
		"GET /api/v1/circles":                           {class: classProtected},
		"GET /api/v1/circles/:id":                       {class: classItem, probe: "/api/v1/circles/" + s.circle},
		"PUT /api/v1/circles/:id":                       {class: classItem, probe: "/api/v1/circles/" + s.circle},
		"DELETE /api/v1/circles/:id":                    {class: classItem, probe: "/api/v1/circles/" + s.circle},
		"POST /api/v1/circles/:id/members":              {class: classItem, probe: "/api/v1/circles/" + s.circle + "/members"},
		"DELETE /api/v1/circles/:id/members/:vcard_uid": {class: classItem, probe: "/api/v1/circles/" + s.circle + "/members/" + fabricatedUUID},

		// --- link field types -----------------------------------------------
		"POST /api/v1/link-field-types":        {class: classProtected},
		"GET /api/v1/link-field-types":         {class: classProtected},
		"PUT /api/v1/link-field-types/reorder": {class: classProtected},
		"GET /api/v1/link-field-types/:id":     {class: classItem, probe: "/api/v1/link-field-types/" + s.linkType},
		"PUT /api/v1/link-field-types/:id":     {class: classItem, probe: "/api/v1/link-field-types/" + s.linkType},
		"DELETE /api/v1/link-field-types/:id":  {class: classItem, probe: "/api/v1/link-field-types/" + s.linkType},

		// --- households -----------------------------------------------------
		"POST /api/v1/households":                           {class: classProtected},
		"GET /api/v1/households":                            {class: classProtected},
		"GET /api/v1/households/:id":                        {class: classItem, probe: "/api/v1/households/" + s.household},
		"PUT /api/v1/households/:id":                        {class: classItem, probe: "/api/v1/households/" + s.household},
		"DELETE /api/v1/households/:id":                     {class: classItem, probe: "/api/v1/households/" + s.household},
		"POST /api/v1/households/:id/members":               {class: classItem, probe: "/api/v1/households/" + s.household + "/members"},
		"DELETE /api/v1/households/:id/members/:vcard_uid":  {class: classItem, probe: "/api/v1/households/" + s.household + "/members/" + fabricatedUUID},
		"PATCH /api/v1/households/:id/members/:vcard_uid":   {class: classItem, probe: "/api/v1/households/" + s.household + "/members/" + fabricatedUUID},
		"POST /api/v1/households/:id/suggest-relationships": {class: classItem, probe: "/api/v1/households/" + s.household + "/suggest-relationships"},
		"POST /api/v1/households/suggest-addresses":         {class: classProtected},
		"POST /api/v1/households/suggestions/accept":        {class: classProtected},
		"POST /api/v1/households/suggestions/dismiss":       {class: classProtected},

		// --- tags -----------------------------------------------------------
		"POST /api/v1/tags":                           {class: classProtected},
		"GET /api/v1/tags":                            {class: classProtected},
		"GET /api/v1/tags/:id":                        {class: classItem, probe: "/api/v1/tags/" + s.tag},
		"PUT /api/v1/tags/:id":                        {class: classItem, probe: "/api/v1/tags/" + s.tag},
		"DELETE /api/v1/tags/:id":                     {class: classItem, probe: "/api/v1/tags/" + s.tag},
		"POST /api/v1/tags/:id/contacts":              {class: classItem, probe: "/api/v1/tags/" + s.tag + "/contacts"},
		"DELETE /api/v1/tags/:id/contacts/:vcard_uid": {class: classItem, probe: "/api/v1/tags/" + s.tag + "/contacts/" + fabricatedUUID},

		// --- field definitions ----------------------------------------------
		"POST /api/v1/field-definitions":       {class: classProtected},
		"GET /api/v1/field-definitions":        {class: classProtected},
		"GET /api/v1/field-definitions/:id":    {class: classItem, probe: "/api/v1/field-definitions/" + s.fieldDef},
		"PUT /api/v1/field-definitions/:id":    {class: classItem, probe: "/api/v1/field-definitions/" + s.fieldDef},
		"DELETE /api/v1/field-definitions/:id": {class: classItem, probe: "/api/v1/field-definitions/" + s.fieldDef},

		// --- life events ----------------------------------------------------
		"POST /api/v1/life-events":       {class: classProtected},
		"GET /api/v1/life-events":        {class: classProtected},
		"GET /api/v1/life-events/:id":    {class: classItem, probe: "/api/v1/life-events/" + s.lifeEvent},
		"PUT /api/v1/life-events/:id":    {class: classItem, probe: "/api/v1/life-events/" + s.lifeEvent},
		"DELETE /api/v1/life-events/:id": {class: classItem, probe: "/api/v1/life-events/" + s.lifeEvent},

		// --- conversation agenda --------------------------------------------
		"POST /api/v1/conversation-agenda":              {class: classProtected},
		"GET /api/v1/conversation-agenda":               {class: classProtected},
		"GET /api/v1/conversation-agenda/:id":           {class: classItem, probe: "/api/v1/conversation-agenda/" + s.agenda},
		"PUT /api/v1/conversation-agenda/:id":           {class: classItem, probe: "/api/v1/conversation-agenda/" + s.agenda},
		"PATCH /api/v1/conversation-agenda/:id/discuss": {class: classItem, probe: "/api/v1/conversation-agenda/" + s.agenda + "/discuss"},
		"DELETE /api/v1/conversation-agenda/:id":        {class: classItem, probe: "/api/v1/conversation-agenda/" + s.agenda},

		// --- gifts ----------------------------------------------------------
		"POST /api/v1/gifts":       {class: classProtected},
		"GET /api/v1/gifts":        {class: classProtected},
		"GET /api/v1/gifts/:id":    {class: classItem, probe: "/api/v1/gifts/" + s.gift},
		"PUT /api/v1/gifts/:id":    {class: classItem, probe: "/api/v1/gifts/" + s.gift},
		"DELETE /api/v1/gifts/:id": {class: classItem, probe: "/api/v1/gifts/" + s.gift},

		// --- preferences ----------------------------------------------------
		"POST /api/v1/preferences":       {class: classProtected},
		"GET /api/v1/preferences":        {class: classProtected},
		"GET /api/v1/preferences/:id":    {class: classItem, probe: "/api/v1/preferences/" + s.preference},
		"PUT /api/v1/preferences/:id":    {class: classItem, probe: "/api/v1/preferences/" + s.preference},
		"DELETE /api/v1/preferences/:id": {class: classItem, probe: "/api/v1/preferences/" + s.preference},

		// --- cadence policies -----------------------------------------------
		"GET /api/v1/cadence-policies/overdue": {class: classProtected},
		"POST /api/v1/cadence-policies":        {class: classProtected},
		"GET /api/v1/cadence-policies":         {class: classProtected},
		"GET /api/v1/cadence-policies/:id":     {class: classItem, probe: "/api/v1/cadence-policies/" + s.cadence},
		"PUT /api/v1/cadence-policies/:id":     {class: classItem, probe: "/api/v1/cadence-policies/" + s.cadence},
		"DELETE /api/v1/cadence-policies/:id":  {class: classItem, probe: "/api/v1/cadence-policies/" + s.cadence},

		// --- reach-out suggestions ------------------------------------------
		"GET /api/v1/reach-out-suggestions":              {class: classProtected},
		"POST /api/v1/reach-out-suggestions/:id/dismiss": {class: classItem, probe: "/api/v1/reach-out-suggestions/" + fabricatedUUID + "/dismiss"},

		// --- reminders ------------------------------------------------------
		"GET /api/v1/reminders":               {class: classProtected},
		"GET /api/v1/reminders/upcoming":      {class: classProtected},
		"GET /api/v1/reminders/:id":           {class: classItem, probe: "/api/v1/reminders/" + s.reminder},
		"PUT /api/v1/reminders/:id":           {class: classItem, probe: "/api/v1/reminders/" + s.reminder},
		"POST /api/v1/reminders/:id/complete": {class: classItem, probe: "/api/v1/reminders/" + s.reminder + "/complete"},
		"DELETE /api/v1/reminders/:id":        {class: classItem, probe: "/api/v1/reminders/" + s.reminder},

		// --- reminder completions -------------------------------------------
		"DELETE /api/v1/reminder-completions/:id": {class: classItem, probe: "/api/v1/reminder-completions/" + fabricatedNum},

		// --- export ---------------------------------------------------------
		"GET /api/v1/export":           {class: classProtected},
		"GET /api/v1/export/vcf":       {class: classProtected},
		"GET /api/v1/export/jscontact": {class: classProtected},
		// Issue #442 (DATA-02): loss-report preflight, user-scoped exactly
		// like the exports it mirrors.
		"GET /api/v1/export/preflight": {class: classProtected},

		// --- graph / search -------------------------------------------------
		"GET /api/v1/graph":             {class: classProtected},
		"GET /api/v1/graph/connections": {class: classProtected},
		"GET /api/v1/search":            {class: classProtected},

		// --- api tokens -----------------------------------------------------
		"GET /api/v1/api-tokens":             {class: classProtected},
		"POST /api/v1/api-tokens":            {class: classProtected},
		"POST /api/v1/api-tokens/revoke-all": {class: classProtected},
		"DELETE /api/v1/api-tokens/:id":      {class: classItem, probe: "/api/v1/api-tokens/" + fabricatedNum},
		"POST /api/v1/api-tokens/:id/rotate": {class: classItem, probe: "/api/v1/api-tokens/" + fabricatedNum + "/rotate"},
		"GET /api/v1/sessions":               {class: classProtected},
		"DELETE /api/v1/sessions":            {class: classProtected},
		"DELETE /api/v1/sessions/:id":        {class: classItem, probe: "/api/v1/sessions/" + fabricatedNum},

		// --- device grants (issue #722, the server half of biometric login) --
		"GET /api/v1/auth/device/grants":             {class: classProtected},
		"POST /api/v1/auth/device/grants":            {class: classProtected},
		"POST /api/v1/auth/device/grants/revoke-all": {class: classProtected},
		"DELETE /api/v1/auth/device/grants/:id":      {class: classItem, probe: "/api/v1/auth/device/grants/" + fabricatedNum},

		// --- webhooks -------------------------------------------------------
		"GET /api/v1/webhooks":                {class: classProtected},
		"POST /api/v1/webhooks":               {class: classProtected},
		"GET /api/v1/webhooks/:id":            {class: classItem, probe: "/api/v1/webhooks/" + s.webhook},
		"PUT /api/v1/webhooks/:id":            {class: classItem, probe: "/api/v1/webhooks/" + s.webhook},
		"DELETE /api/v1/webhooks/:id":         {class: classItem, probe: "/api/v1/webhooks/" + s.webhook},
		"POST /api/v1/webhooks/:id/test":      {class: classItem, probe: "/api/v1/webhooks/" + s.webhook + "/test"},
		"GET /api/v1/webhooks/:id/deliveries": {class: classItem, probe: "/api/v1/webhooks/" + s.webhook + "/deliveries"},

		// --- audit ----------------------------------------------------------
		"GET /api/v1/audit":           {class: classProtected},
		"POST /api/v1/audit/:id/undo": {class: classItem, probe: "/api/v1/audit/" + fabricatedNum + "/undo"},
		// Issue #416: CSV export of the caller's own audit trail, scoped by
		// user_id exactly like GET /audit above -- classProtected, not
		// classItem, since there is no :id in the path for persona 3 to
		// probe against another user's resource.
		"GET /api/v1/audit/export": {class: classProtected},

		// --- notifications --------------------------------------------------
		"GET /api/v1/notifications/config":                    {class: classProtected},
		"PUT /api/v1/notifications/config":                    {class: classProtected},
		"POST /api/v1/notifications/config/test":              {class: classProtected},
		"GET /api/v1/notifications/push-subscriptions":        {class: classProtected},
		"POST /api/v1/notifications/push-subscriptions":       {class: classProtected},
		"DELETE /api/v1/notifications/push-subscriptions/:id": {class: classItem, probe: "/api/v1/notifications/push-subscriptions/" + fabricatedNum},
		"GET /api/v1/notifications/devices":                   {class: classProtected},
		"POST /api/v1/notifications/devices":                  {class: classProtected},
		"DELETE /api/v1/notifications/devices/:id":            {class: classItem, probe: "/api/v1/notifications/devices/" + fabricatedNum},

		// --- calendars ------------------------------------------------------
		"GET /api/v1/calendars":           {class: classProtected},
		"POST /api/v1/calendars":          {class: classProtected},
		"PUT /api/v1/calendars/:id":       {class: classItem, probe: "/api/v1/calendars/" + fabricatedNum},
		"DELETE /api/v1/calendars/:id":    {class: classItem, probe: "/api/v1/calendars/" + fabricatedNum},
		"POST /api/v1/calendars/:id/sync": {class: classItem, probe: "/api/v1/calendars/" + fabricatedNum + "/sync"},

		// --- contact subscriptions ------------------------------------------
		"GET /api/v1/contact-subscriptions":           {class: classProtected},
		"POST /api/v1/contact-subscriptions":          {class: classProtected},
		"PUT /api/v1/contact-subscriptions/:id":       {class: classItem, probe: "/api/v1/contact-subscriptions/" + fabricatedNum},
		"DELETE /api/v1/contact-subscriptions/:id":    {class: classItem, probe: "/api/v1/contact-subscriptions/" + fabricatedNum},
		"POST /api/v1/contact-subscriptions/:id/sync": {class: classItem, probe: "/api/v1/contact-subscriptions/" + fabricatedNum + "/sync"},

		// --- contact sync conflicts -----------------------------------------
		"GET /api/v1/contact-sync-conflicts":              {class: classProtected},
		"POST /api/v1/contact-sync-conflicts/:id/restore": {class: classItem, probe: "/api/v1/contact-sync-conflicts/" + fabricatedNum + "/restore"},
		"POST /api/v1/contact-sync-conflicts/:id/dismiss": {class: classItem, probe: "/api/v1/contact-sync-conflicts/" + fabricatedNum + "/dismiss"},

		// --- external identities --------------------------------------------
		"POST /api/v1/external-identities":       {class: classProtected},
		"GET /api/v1/external-identities":        {class: classProtected},
		"GET /api/v1/external-identities/:id":    {class: classItem, probe: "/api/v1/external-identities/" + s.extIdentity},
		"PUT /api/v1/external-identities/:id":    {class: classItem, probe: "/api/v1/external-identities/" + s.extIdentity},
		"DELETE /api/v1/external-identities/:id": {class: classItem, probe: "/api/v1/external-identities/" + s.extIdentity},

		// --- external activities --------------------------------------------
		"POST /api/v1/external-activities":       {class: classProtected},
		"GET /api/v1/external-activities":        {class: classProtected},
		"GET /api/v1/external-activities/:id":    {class: classItem, probe: "/api/v1/external-activities/" + s.extActivity},
		"PUT /api/v1/external-activities/:id":    {class: classItem, probe: "/api/v1/external-activities/" + s.extActivity},
		"DELETE /api/v1/external-activities/:id": {class: classItem, probe: "/api/v1/external-activities/" + s.extActivity},

		// --- immich (integration; contact-scoped) ---------------------------
		"GET /api/v1/immich/config":                      {class: classProtected},
		"PUT /api/v1/immich/config":                      {class: classProtected},
		"DELETE /api/v1/immich/config":                   {class: classProtected},
		"POST /api/v1/immich/test-connection":            {class: classProtected},
		"GET /api/v1/immich/people":                      {class: classProtected},
		"POST /api/v1/immich/sync":                       {class: classProtected},
		"POST /api/v1/immich/contacts/:vcard_uid/link":   {class: classItem, probe: "/api/v1/immich/contacts/" + uid + "/link"},
		"DELETE /api/v1/immich/contacts/:vcard_uid/link": {class: classItem, probe: "/api/v1/immich/contacts/" + uid + "/link"},
		// summary returns 200 {"summary":nil} for any contact with no
		// (user-scoped) identity link — a legitimate, correctly-scoped
		// "not linked" answer, not a 404/403 — so a status-only BOLA probe
		// cannot assert it. Its data is scoped by user_id in the handler and
		// covered by immich_controller_test.go.
		"GET /api/v1/immich/contacts/:vcard_uid/summary":                {class: classProtected},
		"GET /api/v1/immich/contacts/:vcard_uid/thumbnail":              {class: classItem, probe: "/api/v1/immich/contacts/" + uid + "/thumbnail"},
		"GET /api/v1/immich/contacts/:vcard_uid/assets":                 {class: classItem, probe: "/api/v1/immich/contacts/" + uid + "/assets"},
		"GET /api/v1/immich/contacts/:vcard_uid/assets/:asset_id/image": {class: classItem, probe: "/api/v1/immich/contacts/" + uid + "/assets/" + fabricatedUUID + "/image"},

		// --- paperless (integration; contact-scoped) ------------------------
		"GET /api/v1/paperless/config":                                    {class: classProtected},
		"PUT /api/v1/paperless/config":                                    {class: classProtected},
		"DELETE /api/v1/paperless/config":                                 {class: classProtected},
		"POST /api/v1/paperless/test-connection":                          {class: classProtected},
		"GET /api/v1/paperless/documents":                                 {class: classProtected},
		"POST /api/v1/paperless/contacts/:vcard_uid/link":                 {class: classItem, probe: "/api/v1/paperless/contacts/" + uid + "/link"},
		"DELETE /api/v1/paperless/contacts/:vcard_uid/links/:identity_id": {class: classItem, probe: "/api/v1/paperless/contacts/" + uid + "/links/" + fabricatedUUID},

		// --- seafile (integration; contact-scoped) --------------------------
		"GET /api/v1/seafile/config":           {class: classProtected},
		"PUT /api/v1/seafile/config":           {class: classProtected},
		"DELETE /api/v1/seafile/config":        {class: classProtected},
		"POST /api/v1/seafile/test-connection": {class: classProtected},
		"GET /api/v1/seafile/libraries":        {class: classProtected},
		// Lists a directory in the *caller's own* Seafile config (503 when
		// unconfigured) — not a shared, ownership-scoped resource, so there
		// is no cross-account object to probe.
		"GET /api/v1/seafile/libraries/:repo_id/dir":                    {class: classProtected},
		"POST /api/v1/seafile/contacts/:vcard_uid/link":                 {class: classItem, probe: "/api/v1/seafile/contacts/" + uid + "/link"},
		"DELETE /api/v1/seafile/contacts/:vcard_uid/links/:identity_id": {class: classItem, probe: "/api/v1/seafile/contacts/" + uid + "/links/" + fabricatedUUID},

		// --- nextcloud/webdav (integration; contact-scoped) -----------------
		"GET /api/v1/nextcloud/config":                                    {class: classProtected},
		"PUT /api/v1/nextcloud/config":                                    {class: classProtected},
		"DELETE /api/v1/nextcloud/config":                                 {class: classProtected},
		"POST /api/v1/nextcloud/test-connection":                          {class: classProtected},
		"GET /api/v1/nextcloud/dir":                                       {class: classProtected},
		"POST /api/v1/nextcloud/contacts/:vcard_uid/link":                 {class: classItem, probe: "/api/v1/nextcloud/contacts/" + uid + "/link"},
		"DELETE /api/v1/nextcloud/contacts/:vcard_uid/links/:identity_id": {class: classItem, probe: "/api/v1/nextcloud/contacts/" + uid + "/links/" + fabricatedUUID},
	}
}

// routeKey renders a gin route as its "METHOD /path-template" key, matching
// the declared authorization rows above.
func routeKey(method, path string) string {
	return method + " " + path
}

func TestAuthorizationMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	// The loose BOLA probes deliberately hit non-existent fabricated ids, so
	// handlers log an expected "record not found" on every one. Silence GORM's
	// logger to keep the test output to real failures.
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:     "authz-matrix-test-secret-key-that-is-long-enough",
		JWTExpiryHours:   96,
		ProfilePhotoDir:  t.TempDir(),
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
	}

	// The general-API rate limiter is a shared process-global bucket keyed by
	// client IP; every httptest request shares one IP, and this test issues
	// ~1500 requests. Raise the burst so rate limiting never turns an authz
	// verdict into a spurious 429.
	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	// --- seed actors --------------------------------------------------------
	owner := models.User{Username: "matrix-owner", Email: "matrix-owner@example.com", Password: "password123"}
	require.NoError(t, db.Create(&owner).Error)
	intruder := models.User{Username: "matrix-intruder", Email: "matrix-intruder@example.com", Password: "password123"}
	require.NoError(t, db.Create(&intruder).Error)
	admin := models.User{Username: "matrix-admin", Email: "matrix-admin@example.com", Password: "password123", IsAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	disabled := models.User{Username: "matrix-disabled", Email: "matrix-disabled@example.com", Password: "password123"}
	require.NoError(t, db.Create(&disabled).Error)
	require.NoError(t, db.Delete(&disabled).Error) // soft-delete: valid token, missing row

	// --- mint tokens --------------------------------------------------------
	tokens := map[persona]string{}
	mint := func(u models.User) string {
		tok, err := services.IssueSession(db, u, cfg, "", "")
		require.NoError(t, err)
		return tok
	}
	tokens[personaOwner] = mint(owner)
	tokens[personaIntruder] = mint(intruder)
	tokens[personaAdmin] = mint(admin)
	tokens[personaDisabled] = mint(disabled)

	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"authorized":    true,
		"username":      owner.Username,
		"user_id":       owner.ID,
		"token_version": owner.TokenVersion,
		"exp":           time.Now().Add(-time.Hour).Unix(),
	})
	expiredStr, err := expired.SignedString([]byte(cfg.JWTSecretKey))
	require.NoError(t, err)
	tokens[personaExpired] = expiredStr

	// --- seed owner resources + build the route table -----------------------
	res := seedResources(t, db, owner.ID)
	table := buildTable(res)

	// --- wire the router exactly as main.go does (minus CORS/logging) -------
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	RegisterRoutes(router, cfg, db, nil)

	// --- completeness checks: every registered route has a row, and every
	// declared row matches a registered route -------------------------------
	registered := map[string]gin.RouteInfo{}
	for _, r := range router.Routes() {
		registered[routeKey(r.Method, r.Path)] = r
	}

	var missing, stale []string
	for key := range registered {
		if _, ok := table[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range table {
		if _, ok := registered[key]; !ok {
			stale = append(stale, key)
		}
	}
	require.Empty(t, missing,
		"registered routes with no authorization row — add a row to buildTable:\n  %v", missing)
	require.Empty(t, stale,
		"declared authorization rows with no matching registered route:\n  %v", stale)

	// --- run the matrix -----------------------------------------------------
	// Personas are ordered so every BOLA probe (intruder/admin) sees intact
	// seed resources; the owner's destructive probes run last, where their
	// mutations can only turn later "admitted" assertions into 404s (harmless).
	order := []persona{
		personaUnauth, personaDisabled, personaExpired,
		personaAdmin, personaIntruder, personaOwner,
	}

	failures := 0
	for _, p := range order {
		for _, r := range router.Routes() {
			key := routeKey(r.Method, r.Path)
			row := table[key]
			if row.class == classPublic && p != personaUnauth {
				continue // public routes have no auth boundary; one probe suffices
			}
			path := r.Path
			if row.probe != "" {
				path = row.probe
			}
			status := dispatch(router, r.Method, path, tokens[p])
			if check := expect(row, r.Method, p); !check(status) {
				failures++
				t.Errorf("%-9s %-6s %s -> %d", personaNames[p], r.Method, path, status)
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d authorization-matrix violation(s); see the mismatches above", failures)
	}
}

// dispatch issues a single request against the router with the persona's
// bearer token (empty for unauth) and an empty body, returning the status.
func dispatch(router http.Handler, method, path, token string) int {
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		return 0
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code
}

// expect returns the predicate the status must satisfy for (row, method,
// persona). It encodes the verdict table in the file's doc comment.
func expect(row authzRow, method string, p persona) func(int) bool {
	switch row.class {
	case classPublic:
		return admitted()
	case classAdmin:
		switch p {
		case personaUnauth, personaDisabled, personaExpired:
			return isStatus(http.StatusUnauthorized)
		case personaOwner, personaIntruder:
			return isStatus(http.StatusForbidden)
		default: // admin
			return admitted()
		}
	case classItem:
		switch p {
		case personaUnauth, personaDisabled, personaExpired:
			return isStatus(http.StatusUnauthorized)
		case personaOwner:
			return admitted()
		default: // intruder + admin are both cross-account on item routes
			if method == http.MethodGet {
				return scoped()
			}
			return not2xx()
		}
	default: // classProtected
		switch p {
		case personaUnauth, personaDisabled, personaExpired:
			return isStatus(http.StatusUnauthorized)
		default:
			return admitted()
		}
	}
}

func isStatus(want int) func(int) bool {
	return func(got int) bool { return got == want }
}

func admitted() func(int) bool {
	return func(got int) bool { return got != http.StatusUnauthorized && got != http.StatusForbidden }
}

// not2xx is the loose BOLA verdict for non-GET item probes (empty body): the
// intruder must not get a successful (2xx) cross-account outcome.
func not2xx() func(int) bool {
	return func(got int) bool { return got < 200 || got >= 300 }
}

// scoped is the strict BOLA verdict for GET item probes against a seeded
// owner resource: 404 (existence mask) or 403 are the only legitimate
// cross-account outcomes.
func scoped() func(int) bool {
	return func(got int) bool { return got == http.StatusNotFound || got == http.StatusForbidden }
}
