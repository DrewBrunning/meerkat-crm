package routes

// TestBodyOwnershipMatrix is the third authorization surface for this API,
// beside routes/authorization_matrix_test.go (path-carried identifiers, six
// personas) and routes/authorization_matrix_credentials_test.go (the two
// non-JWT credential types). Issue #877.
//
// Those two matrices are thorough about identifiers carried in the URL. Neither
// reaches an identifier carried in the *request body*, by construction — from
// authorization_matrix_test.go's own header, "a non-GET item probe (empty
// body) must simply not 2xx". So a foreign contact UID inside a populated body
// (POST /contacts/bulk with someone else's vcard_uids[], POST
// /relationship-edges with a foreign target_id, POST /gifts with a foreign
// entity_id, …) is never exercised there.
//
// The credentialed pen test's fourth pass (issue #860) swept that surface by
// hand and found no IDOR — the strongest runtime evidence behind
// docs/security/asvs-l2.md's API1 row. It was a one-time manual result; nothing
// kept it true. This test makes it permanent.
//
// ── What it asserts ────────────────────────────────────────────────────────
//
// For every endpoint that accepts an entity UID in its request body, probing
// with three inputs and comparing the answers:
//
//	caller's own UID   → 2xx   (control — the request shape is valid)
//	another user's UID → 404   (403 would be an existence oracle; 2xx an IDOR)
//	nonexistent v4 UID → 404   (must be indistinguishable from the row above)
//
// "Indistinguishable" is checked as: identical HTTP status, identical error
// `code`, identical error `message`. The full body is deliberately NOT
// compared byte-for-byte — errors/middleware.go stamps a per-response
// `timestamp` and echoes the *supplied* UID back in `details`, neither of
// which is an existence leak. A 403-vs-404, a different code, a different
// message, or a 2xx is.
//
// Shape-specific coverage the issue calls out:
//
//   - Collection fields (vcard_uids[], member_vcard_uids[]) are probed
//     owned-then-foreign AND foreign-then-owned, and the outcome is checked
//     against the victim's rows in the database, not the response body — a
//     handler that partially applied then errored looks like a rejection on
//     the wire.
//   - Two-endpoint operations (relationship-edges' source/target,
//     duplicates/dismiss' uid_a/uid_b, contacts/merge's keep_id/merge_id) are
//     probed with the foreign UID in each position — asymmetric checking is
//     the common defect.
//   - Container-vs-member routes (POST /circles/:id/members, …) carry a
//     container :id AND a member UID; both a foreign member in your own
//     container and your own member in a foreign container are probed.
//
// ── The completeness guard is the point ───────────────────────────────────
//
// ownScanUUID4Fields parses package models with go/ast and returns every
// struct field whose `validate` tag contains "uuid4". buildBodyOwnershipTable
// declares one row per such field. The guard fails in both directions: a
// scanned field with no row is `missing` (someone added a uuid4 field and did
// not declare its route + expected outcome), a declared row with no scanned
// field is `stale`. A hand-listed set of endpoints would go stale the first
// time someone adds an entity — which is exactly how the gap in issue #860
// opened.
//
// Rows fall in two kinds. `kindRequestDTO` rows carry a live probe. `kindModel`
// rows are the persisted-model mirrors of a request DTO (the same validate tag
// is repeated on the GORM model as belt-and-braces) or server-generated rows
// whose UID never arrives in a body — they carry a written reason instead of a
// probe, so a genuinely new request DTO cannot hide among them: it fails the
// guard until a human classifies it.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/internal/dbtest"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ownNonexistentUID is a well-formed v4 UUID that belongs to no row in any
// table. It must pass the `uuid4` struct-tag validator so the request reaches
// the handler's ownership check rather than 400-ing in middleware.
const ownNonexistentUID = "11111111-1111-4111-8111-111111111111"

// ── reflection surface ────────────────────────────────────────────────────

// ownScanUUID4Fields parses ../models/*.go (non-test) and returns
// "<Struct>.<Field>" → "file:line" for every struct field whose `validate`
// tag mentions uuid4.
func ownScanUUID4Fields(t *testing.T) map[string]string {
	t.Helper()
	const dir = "../models"
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		require.NoErrorf(t, perr, "parsing %s", e.Name())

		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Tag == nil || len(field.Names) == 0 {
					continue
				}
				tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
				if !strings.Contains(tag.Get("validate"), "uuid4") {
					continue
				}
				for _, name := range field.Names {
					key := ts.Name.Name + "." + name.Name
					out[key] = fmt.Sprintf("%s:%d", e.Name(), fset.Position(field.Pos()).Line)
				}
			}
			return true
		})
	}
	require.NotEmpty(t, out, "AST scan found zero uuid4 fields — the scanner is broken")
	return out
}

// ── declared rows ─────────────────────────────────────────────────────────

type ownRowKind int

const (
	kindRequestDTO ownRowKind = iota // carries a live probe
	kindModel                        // persisted-model mirror / server-generated; carries a reason
)

type ownRow struct {
	kind   ownRowKind
	reason string                            // required (and only used) for kindModel
	probe  func(t *testing.T, h *ownHarness) // required (and only used) for kindRequestDTO
}

// ── fixtures ──────────────────────────────────────────────────────────────

type ownFixtures struct {
	ownerID, victimID, recipientID uint

	ownerC1UID, ownerC2UID, victimC1UID string
	ownerC1Num, ownerC2Num, victimC1Num uint

	ownerCircleID, victimCircleID       string
	ownerHouseholdID, victimHouseholdID string
	ownerTagID, victimTagID             string
	ownerLifeEventID, victimLifeEventID string
	ownerLinkTypeID, victimLinkTypeID   string
}

type ownHarness struct {
	t      *testing.T
	router http.Handler
	db     *gorm.DB
	token  string // the owner's session
	fx     ownFixtures
}

func ownSeed(t *testing.T, db *gorm.DB) ownFixtures {
	t.Helper()

	owner := models.User{Username: "own-owner", Email: "own-owner@example.com", Password: "password123"}
	require.NoError(t, db.Create(&owner).Error)
	victim := models.User{Username: "own-victim", Email: "own-victim@example.com", Password: "password123"}
	require.NoError(t, db.Create(&victim).Error)
	recipient := models.User{Username: "own-recipient", Email: "own-recipient@example.com", Password: "password123"}
	require.NoError(t, db.Create(&recipient).Error)

	mkContact := func(uid uint, name string) models.Contact {
		c := models.Contact{UserID: uid, Firstname: name}
		require.NoError(t, db.Create(&c).Error)
		return c
	}
	oc1 := mkContact(owner.ID, "Owner One")
	oc2 := mkContact(owner.ID, "Owner Two")
	vc1 := mkContact(victim.ID, "Victim One")

	mkCircle := func(uid uint) string {
		c := models.Circle{UserID: uid, Name: "circle"}
		require.NoError(t, db.Create(&c).Error)
		return c.ID
	}
	mkHousehold := func(uid uint) string {
		h := models.Household{UserID: uid, Name: "household", Type: models.HouseholdTypeOther}
		require.NoError(t, db.Create(&h).Error)
		return h.ID
	}
	mkTag := func(uid uint) string {
		tg := models.Tag{UserID: uid, Name: "tag"}
		require.NoError(t, db.Create(&tg).Error)
		return tg.ID
	}
	mkLifeEvent := func(uid uint, entityUID string) string {
		le := models.LifeEvent{UserID: uid, EntityID: entityUID, Type: models.LifeEventTypeMoved}
		require.NoError(t, db.Create(&le).Error)
		return le.ID
	}
	mkLinkType := func(uid uint) string {
		lt := models.LinkFieldType{UserID: uid, Name: "signal", Protocol: "https://signal.me/#p/{value}", Category: models.LinkFieldTypeCategoryMessaging}
		require.NoError(t, db.Create(&lt).Error)
		return lt.ID
	}

	return ownFixtures{
		ownerID: owner.ID, victimID: victim.ID, recipientID: recipient.ID,
		ownerC1UID: oc1.VCardUID, ownerC2UID: oc2.VCardUID, victimC1UID: vc1.VCardUID,
		ownerC1Num: oc1.ID, ownerC2Num: oc2.ID, victimC1Num: vc1.ID,
		ownerCircleID: mkCircle(owner.ID), victimCircleID: mkCircle(victim.ID),
		ownerHouseholdID: mkHousehold(owner.ID), victimHouseholdID: mkHousehold(victim.ID),
		ownerTagID: mkTag(owner.ID), victimTagID: mkTag(victim.ID),
		ownerLifeEventID: mkLifeEvent(owner.ID, oc1.VCardUID), victimLifeEventID: mkLifeEvent(victim.ID, vc1.VCardUID),
		ownerLinkTypeID: mkLinkType(owner.ID), victimLinkTypeID: mkLinkType(victim.ID),
	}
}

// ── request helper + shared assertions ────────────────────────────────────

func (h *ownHarness) req(method, path, body string) (int, string) {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func ownParseErr(body string) (code, message string) {
	var r struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &r)
	return r.Error.Code, r.Error.Message
}

func jstr(s string) string { b, _ := json.Marshal(s); return string(b) }

// assertMasked is the three-input probe for a single body UID field.
func (h *ownHarness) assertMasked(method, path string, body func(uid string) string, controlUID, foreignUID, missingUID string, wantForeign int, skipControl bool) {
	h.t.Helper()
	if !skipControl {
		st, rb := h.req(method, path, body(controlUID))
		require.Truef(h.t, st >= 200 && st < 300,
			"control probe %s %s with an owned UID -> %d (%s); want 2xx (the request shape must be valid)", method, path, st, rb)
	}
	fSt, fBody := h.req(method, path, body(foreignUID))
	mSt, mBody := h.req(method, path, body(missingUID))

	require.Equalf(h.t, wantForeign, fSt, "%s %s: foreign UID -> %d; want %d", method, path, fSt, wantForeign)
	require.Equalf(h.t, mSt, fSt, "%s %s: foreign UID -> %d but nonexistent UID -> %d (status is an existence oracle)", method, path, fSt, mSt)

	fc, fm := ownParseErr(fBody)
	mc, mm := ownParseErr(mBody)
	require.Equalf(h.t, mc, fc, "%s %s: error code differs (foreign=%q nonexistent=%q) — existence oracle", method, path, fc, mc)
	require.Equalf(h.t, mm, fm, "%s %s: error message differs (foreign=%q nonexistent=%q) — existence oracle", method, path, fm, mm)
}

// assertPairMasked probes a two-UID body field with the foreign / nonexistent
// UID in each position.
func (h *ownHarness) assertPairMasked(method, path string, body func(a, b string) string, ownedA, ownedB, foreignUID, missingUID string, wantStatus int) {
	h.t.Helper()
	st, rb := h.req(method, path, body(ownedA, ownedB))
	require.Truef(h.t, st >= 200 && st < 300, "control probe %s %s (both owned) -> %d (%s); want 2xx", method, path, st, rb)

	type pos struct{ name, a, b string }
	cases := []pos{
		{"foreign-in-first", foreignUID, ownedB},
		{"nonexistent-in-first", missingUID, ownedB},
		{"foreign-in-second", ownedA, foreignUID},
		{"nonexistent-in-second", ownedA, missingUID},
	}
	codes := map[string]string{}
	msgs := map[string]string{}
	for _, c := range cases {
		gs, gb := h.req(method, path, body(c.a, c.b))
		require.Equalf(h.t, wantStatus, gs, "%s %s [%s] -> %d; want %d", method, path, c.name, gs, wantStatus)
		codes[c.name], msgs[c.name] = ownParseErr(gb)
	}
	require.Equalf(h.t, codes["nonexistent-in-first"], codes["foreign-in-first"],
		"%s %s: first position — foreign vs nonexistent error code differ (existence oracle)", method, path)
	require.Equalf(h.t, codes["nonexistent-in-second"], codes["foreign-in-second"],
		"%s %s: second position — foreign vs nonexistent error code differ (existence oracle)", method, path)
	require.Equal(h.t, msgs["nonexistent-in-first"], msgs["foreign-in-first"])
	require.Equal(h.t, msgs["nonexistent-in-second"], msgs["foreign-in-second"])
}

// assertContainerMasked probes POST /<container>/:id/<members> style routes:
// the member UID and the container id are two independent ownership questions.
func (h *ownHarness) assertContainerMasked(method, ownContainerPath, foreignContainerPath string, body func(uid string) string) {
	h.t.Helper()
	// control: the caller's own member into their own container. A sibling
	// probe (e.g. the bulk add_circle/add_tag control) may already have added
	// this exact (container, member) pair, so 409 ALREADY_EXISTS is an
	// acceptable control outcome too — it still proves the request shape is
	// valid and that both the container id and the member UID resolved.
	st, rb := h.req(method, ownContainerPath, body(h.fx.ownerC1UID))
	require.Truef(h.t, (st >= 200 && st < 300) || st == http.StatusConflict,
		"control probe %s %s with an owned member -> %d (%s); want 2xx or 409", method, ownContainerPath, st, rb)
	// foreign / nonexistent member into the caller's own container
	h.assertMasked(method, ownContainerPath, body, "", h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, true)
	// the caller's own member into a container owned by someone else: the
	// container id must be masked too (404, never a 2xx that would let a
	// caller write into a foreign collection).
	cSt, cBody := h.req(method, foreignContainerPath, body(h.fx.ownerC1UID))
	require.Equalf(h.t, http.StatusNotFound, cSt,
		"%s %s: own member into a foreign container -> %d (%s); want 404", method, foreignContainerPath, cSt, cBody)
}

func jsonArray(uids []string) string {
	parts := make([]string, len(uids))
	for i, u := range uids {
		parts[i] = jstr(u)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// ── the table ─────────────────────────────────────────────────────────────

func buildBodyOwnershipTable(fx ownFixtures) map[string]ownRow {
	model := func(reason string) ownRow { return ownRow{kind: kindModel, reason: reason} }
	dto := func(p func(t *testing.T, h *ownHarness)) ownRow { return ownRow{kind: kindRequestDTO, probe: p} }

	// relationship-edges and duplicates/dismiss each expose their body UID
	// through two struct fields that share one endpoint; the pair probe
	// already covers both positions, so it runs once, guarded by a flag both
	// rows close over.
	relEdgeRan := false
	relEdgeProbe := func(t *testing.T, h *ownHarness) {
		if relEdgeRan {
			return
		}
		relEdgeRan = true
		h.assertPairMasked(http.MethodPost, "/api/v1/relationship-edges",
			func(a, b string) string {
				return fmt.Sprintf(`{"source_id":%s,"target_id":%s,"type":"friend_of"}`, jstr(a), jstr(b))
			},
			h.fx.ownerC1UID, h.fx.ownerC2UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound)
	}
	dupRan := false
	dupProbe := func(t *testing.T, h *ownHarness) {
		if dupRan {
			return
		}
		dupRan = true
		h.assertPairMasked(http.MethodPost, "/api/v1/contacts/duplicates/dismiss",
			func(a, b string) string {
				return fmt.Sprintf(`{"uid_a":%s,"uid_b":%s}`, jstr(a), jstr(b))
			},
			h.fx.ownerC1UID, h.fx.ownerC2UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound)
	}

	return map[string]ownRow{
		// ── single contact-UID body fields (foreign → 404, masked) ──────────
		"ContactShareInput.VCardUID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/contact-shares",
				func(uid string) string {
					return fmt.Sprintf(`{"to_user_id":%d,"vcard_uid":%s,"sections":["emails"]}`, h.fx.recipientID, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"GiftInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/gifts",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"description":"x"}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"GiftInput.LifeEventID": dto(func(t *testing.T, h *ownHarness) {
			// entity_id is a valid owned contact; life_event_id is the probed
			// UID (a LifeEvent id, not a contact — same 404-mask rule).
			h.assertMasked(http.MethodPost, "/api/v1/gifts",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"description":"x","life_event_id":%s}`, jstr(h.fx.ownerC1UID), jstr(uid))
				},
				h.fx.ownerLifeEventID, h.fx.victimLifeEventID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"PreferenceInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/preferences",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"category":"food","value":"pizza"}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"CadencePolicyInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/cadence-policies",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"target_interval_days":30}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"LifeEventInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/life-events",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"type":"moved"}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"ConversationAgendaInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/conversation-agenda",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"content":"x"}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"ExternalIdentityInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/external-identities",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"system":"immich","external_id":"x1"}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"ExternalActivityInput.EntityID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/external-activities",
				func(uid string) string {
					return fmt.Sprintf(`{"entity_id":%s,"source_system":"immich","external_id":"a1","type":"photo","occurred_at":"2024-01-01T00:00:00Z"}`, jstr(uid))
				},
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"SelfContactInput.VCardUID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPatch, "/api/v1/users/me/self-contact",
				func(uid string) string { return fmt.Sprintf(`{"vcard_uid":%s}`, jstr(uid)) },
				h.fx.ownerC1UID, h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"ApplyContactAddressSuggestionInput.ContactVCardUID": dto(func(t *testing.T, h *ownHarness) {
			// No real address suggestion exists to accept, so an owned UID
			// cannot 2xx here — but the contact-ownership check runs first, so
			// a foreign vs a nonexistent UID must still be indistinguishable.
			h.assertMasked(http.MethodPost, "/api/v1/contacts/address-suggestions/apply",
				func(uid string) string {
					return fmt.Sprintf(`{"contact_vcard_uid":%s,"source_kind":"household","source_id":%s,"address_key":"x"}`, jstr(uid), jstr(h.fx.ownerHouseholdID))
				},
				"", h.fx.victimC1UID, ownNonexistentUID, http.StatusNotFound, true)
		}),

		// ── two-UID body operations (foreign in each position → 404) ────────
		"RelationshipEdgeInput.SourceID": dto(relEdgeProbe),
		"RelationshipEdgeInput.TargetID": dto(relEdgeProbe),
		"DuplicateDismissalInput.UIDA":   dto(dupProbe),
		"DuplicateDismissalInput.UIDB":   dto(dupProbe),

		// ── container + member routes ──────────────────────────────────────
		"CircleMemberInput.MemberVCardUID": dto(func(t *testing.T, h *ownHarness) {
			h.assertContainerMasked(http.MethodPost,
				"/api/v1/circles/"+h.fx.ownerCircleID+"/members",
				"/api/v1/circles/"+h.fx.victimCircleID+"/members",
				func(uid string) string { return fmt.Sprintf(`{"member_vcard_uid":%s}`, jstr(uid)) })
		}),
		"HouseholdMemberInput.MemberVCardUID": dto(func(t *testing.T, h *ownHarness) {
			h.assertContainerMasked(http.MethodPost,
				"/api/v1/households/"+h.fx.ownerHouseholdID+"/members",
				"/api/v1/households/"+h.fx.victimHouseholdID+"/members",
				func(uid string) string { return fmt.Sprintf(`{"member_vcard_uid":%s}`, jstr(uid)) })
		}),
		"ContactTagInput.ContactVCardUID": dto(func(t *testing.T, h *ownHarness) {
			h.assertContainerMasked(http.MethodPost,
				"/api/v1/tags/"+h.fx.ownerTagID+"/contacts",
				"/api/v1/tags/"+h.fx.victimTagID+"/contacts",
				func(uid string) string { return fmt.Sprintf(`{"contact_vcard_uid":%s}`, jstr(uid)) })
		}),

		// ── collection fields — assert the victim's DB rows, not the wire ───
		"BulkContactOperationInput.VCardUIDs": dto(func(t *testing.T, h *ownHarness) {
			mk := func(uids []string) string {
				return fmt.Sprintf(`{"action":"add_tag","tag_id":%s,"vcard_uids":%s}`, jstr(h.fx.ownerTagID), jsonArray(uids))
			}
			st, rb := h.req(http.MethodPost, "/api/v1/contacts/bulk", mk([]string{h.fx.ownerC1UID}))
			require.Truef(t, st >= 200 && st < 300, "bulk control -> %d (%s); want 2xx", st, rb)

			// POST /contacts/bulk is partial-success by design (a foreign uid
			// is a per-row failure, not a request-level 404), so probe both
			// list orders and then check the victim was untouched.
			for _, uids := range [][]string{
				{h.fx.ownerC1UID, h.fx.victimC1UID},
				{h.fx.victimC1UID, h.fx.ownerC1UID},
			} {
				gs, gb := h.req(http.MethodPost, "/api/v1/contacts/bulk", mk(uids))
				require.Equalf(t, http.StatusOK, gs, "bulk mixed list -> %d; want 200 partial-success", gs)
				require.Containsf(t, gb, h.fx.victimC1UID, "bulk response should report the foreign uid as a failure")
				require.Containsf(t, gb, "not found or not owned", "bulk response should report the foreign uid as a failure")
			}
			var n int64
			require.NoError(t, h.db.Model(&models.ContactTag{}).Where("contact_vcard_uid = ?", h.fx.victimC1UID).Count(&n).Error)
			require.Zerof(t, n, "bulk op created a tag membership for the victim's contact (%d rows)", n)
		}),
		"BulkContactOperationInput.CircleID": dto(func(t *testing.T, h *ownHarness) {
			// circle_id is a Circle id, not a contact UID: a foreign one is a
			// request-level 404 (a malformed action, not a per-row outcome).
			h.assertMasked(http.MethodPost, "/api/v1/contacts/bulk",
				func(cid string) string {
					return fmt.Sprintf(`{"action":"add_circle","circle_id":%s,"vcard_uids":%s}`, jstr(cid), jsonArray([]string{h.fx.ownerC1UID}))
				},
				h.fx.ownerCircleID, h.fx.victimCircleID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"BulkContactOperationInput.TagID": dto(func(t *testing.T, h *ownHarness) {
			h.assertMasked(http.MethodPost, "/api/v1/contacts/bulk",
				func(tid string) string {
					return fmt.Sprintf(`{"action":"add_tag","tag_id":%s,"vcard_uids":%s}`, jstr(tid), jsonArray([]string{h.fx.ownerC1UID}))
				},
				h.fx.ownerTagID, h.fx.victimTagID, ownNonexistentUID, http.StatusNotFound, false)
		}),
		"AcceptHouseholdSuggestionInput.MemberVCardUIDs":  dto(ownHouseholdSuggestionProbe("/api/v1/households/suggestions/accept")),
		"DismissHouseholdSuggestionInput.MemberVCardUIDs": dto(ownHouseholdSuggestionProbe("/api/v1/households/suggestions/dismiss")),

		"LinkFieldTypeReorderInput.Order": dto(func(t *testing.T, h *ownHarness) {
			// order[] is a set of the caller's own LinkFieldType ids; a
			// foreign or nonexistent id fails validation identically (400,
			// same message), never reordering a foreign row.
			h.assertMasked(http.MethodPut, "/api/v1/link-field-types/reorder",
				func(id string) string { return fmt.Sprintf(`{"order":%s}`, jsonArray([]string{id})) },
				h.fx.ownerLinkTypeID, h.fx.victimLinkTypeID, ownNonexistentUID, http.StatusBadRequest, false)
		}),

		// ── persisted-model mirrors / server-generated (no body probe) ──────
		"CadencePolicy.EntityID":             model("persisted-model mirror of CadencePolicyInput.EntityID; the write path validates the DTO — covered above."),
		"CircleMember.MemberVCardUID":        model("persisted join-row mirror of CircleMemberInput.MemberVCardUID — covered above."),
		"ExternalIdentity.EntityID":          model("persisted-model mirror of ExternalIdentityInput.EntityID — covered above."),
		"HouseholdMember.MemberVCardUID":     model("persisted join-row mirror of HouseholdMemberInput.MemberVCardUID — covered above."),
		"FieldValue.EntityID":                model("set from the :id contact on PUT /contacts/:id/field-values (path-carried, covered by authorization_matrix_test.go); the body carries FieldDefinition ids, not a contact UID."),
		"ContactTag.ContactVCardUID":         model("persisted join-row mirror of ContactTagInput.ContactVCardUID — covered above."),
		"Gift.EntityID":                      model("persisted-model mirror of GiftInput.EntityID — covered above."),
		"Gift.LifeEventID":                   model("persisted-model mirror of GiftInput.LifeEventID — covered above."),
		"LifeEvent.EntityID":                 model("persisted-model mirror of LifeEventInput.EntityID — covered above."),
		"Preference.EntityID":                model("persisted-model mirror of PreferenceInput.EntityID — covered above."),
		"ExternalActivity.EntityID":          model("persisted-model mirror of ExternalActivityInput.EntityID — covered above."),
		"ConversationAgenda.EntityID":        model("persisted-model mirror of ConversationAgendaInput.EntityID — covered above."),
		"RelationshipEdge.SourceID":          model("persisted-model mirror of RelationshipEdgeInput.SourceID — covered above."),
		"RelationshipEdge.TargetID":          model("persisted-model mirror of RelationshipEdgeInput.TargetID — covered above."),
		"ReachOutSuggestion.ContactVCardUID": model("server-generated row; the only endpoint (POST /reach-out-suggestions/:id/dismiss) carries the id in the path — covered by authorization_matrix_test.go."),
	}
}

// ownHouseholdSuggestionProbe builds the probe for the accept/dismiss
// address-household-suggestion endpoints: both take member_vcard_uids[] (min
// 2) and re-derive the group server-side, so a list that mixes an owned and a
// foreign UID resolves to fewer contacts than asked and 404s — in either
// order — and creates nothing for the victim.
func ownHouseholdSuggestionProbe(path string) func(t *testing.T, h *ownHarness) {
	return func(t *testing.T, h *ownHarness) {
		for _, uids := range [][]string{
			{h.fx.ownerC1UID, h.fx.victimC1UID},
			{h.fx.victimC1UID, h.fx.ownerC1UID},
			{h.fx.ownerC1UID, ownNonexistentUID},
		} {
			st, body := h.req(http.MethodPost, path, fmt.Sprintf(`{"member_vcard_uids":%s}`, jsonArray(uids)))
			require.Equalf(t, http.StatusNotFound, st, "%s %v -> %d; want 404", path, uids, st)
			code, _ := ownParseErr(body)
			require.Equalf(t, "NOT_FOUND", code, "%s: want a NOT_FOUND (existence-masked) error, got %q", path, code)
		}
		// Nothing was created for the victim.
		var households int64
		require.NoError(t, h.db.Model(&models.Household{}).Where("user_id = ?", h.fx.ownerID).
			Where("id NOT IN (?)", []string{h.fx.ownerHouseholdID}).Count(&households).Error)
		require.Zerof(t, households, "%s created a household off a mixed owned/foreign member list", path)
		var members int64
		require.NoError(t, h.db.Model(&models.HouseholdMember{}).Where("member_vcard_uid = ?", h.fx.victimC1UID).Count(&members).Error)
		require.Zerof(t, members, "%s created a household membership referencing the victim's contact", path)
	}
}

// ── the test ──────────────────────────────────────────────────────────────

func TestBodyOwnershipMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := dbtest.New(t)
	db.Logger = logger.Default.LogMode(logger.Silent)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := &config.Config{
		JWTSecretKey:     "ownership-matrix-test-secret-key-that-is-long-enough",
		JWTExpiryHours:   96,
		ProfilePhotoDir:  t.TempDir(),
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
	}

	// Shared IP-keyed bucket, same concern as the sibling matrices: raise the
	// burst so a 429 never masquerades as an authorization verdict.
	middleware.ConfigureAPIRateLimiter(time.Microsecond, 1_000_000)

	fx := ownSeed(t, db)

	var owner models.User
	require.NoError(t, db.First(&owner, fx.ownerID).Error)
	ownerJWT, err := services.IssueSession(db, owner, cfg, "", "")
	require.NoError(t, err)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	RegisterRoutes(router, cfg, db, nil)

	table := buildBodyOwnershipTable(fx)

	// ── completeness guard (bidirectional) ────────────────────────────────
	scanned := ownScanUUID4Fields(t)

	var missing, stale []string
	for key, where := range scanned {
		if _, ok := table[key]; !ok {
			missing = append(missing, key+"  ("+where+")")
		}
	}
	for key := range table {
		if _, ok := scanned[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	require.Emptyf(t, missing,
		"models struct fields carry a uuid4 validate tag but have no ownership row — declare each in buildBodyOwnershipTable "+
			"(a kindRequestDTO row with a probe, or a kindModel row with a reason):\n  %s", strings.Join(missing, "\n  "))
	require.Emptyf(t, stale,
		"ownership rows with no matching uuid4 field in package models (renamed or removed?):\n  %s", strings.Join(stale, "\n  "))

	// ── per-row shape ────────────────────────────────────────────────────
	for key, row := range table {
		switch row.kind {
		case kindRequestDTO:
			require.NotNilf(t, row.probe, "request-DTO row %q has no probe", key)
		case kindModel:
			require.Nilf(t, row.probe, "model-mirror row %q must not carry a probe", key)
			require.NotEmptyf(t, row.reason, "model-mirror row %q needs a reason explaining why no body probe applies", key)
		}
	}

	// ── run the probes ──────────────────────────────────────────────────
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		row := table[k]
		if row.kind != kindRequestDTO {
			continue
		}
		t.Run(k, func(t *testing.T) {
			row.probe(t, &ownHarness{t: t, router: router, db: db, token: ownerJWT, fx: fx})
		})
	}

	// ── contacts/merge — numeric keep_id/merge_id, so not anchored by a
	// uuid4 field, but the same two-contact argument-order concern applies. ─
	t.Run("contacts/merge argument order", func(t *testing.T) {
		hh := &ownHarness{t: t, router: router, db: db, token: ownerJWT, fx: fx}
		body := func(keep, merge uint) string {
			return fmt.Sprintf(`{"keep_id":%d,"merge_id":%d}`, keep, merge)
		}
		// The preview endpoint is the shape control: two owned contacts -> 2xx
		// (it merely reports any scalar conflicts). The commit endpoint needs a
		// `resolutions` map for conflicting contacts, but its ownership check
		// (loadMergeContacts) runs before conflict resolution, so a foreign
		// keep_id/merge_id 404s regardless — no commit control is needed.
		st, rb := hh.req(http.MethodPost, "/api/v1/contacts/merge/preview", body(fx.ownerC1Num, fx.ownerC2Num))
		require.Truef(t, st >= 200 && st < 300, "merge/preview control (both owned) -> %d (%s); want 2xx", st, rb)

		for _, path := range []string{"/api/v1/contacts/merge/preview", "/api/v1/contacts/merge"} {
			for _, c := range []struct {
				name        string
				keep, merge uint
			}{
				{"foreign-keep", fx.victimC1Num, fx.ownerC2Num},
				{"foreign-merge", fx.ownerC1Num, fx.victimC1Num},
			} {
				gs, gb := hh.req(http.MethodPost, path, body(c.keep, c.merge))
				require.Equalf(t, http.StatusNotFound, gs, "%s [%s] -> %d; want 404", path, c.name, gs)
				code, _ := ownParseErr(gb)
				require.Equalf(t, "NOT_FOUND", code, "%s [%s]: want NOT_FOUND, got %q", path, c.name, code)
			}
		}
	})
}
