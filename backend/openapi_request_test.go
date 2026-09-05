package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"mycorrhizal/controllers"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/routes"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// requestBodyBinding describes how one route's JSON body is actually bound
// in production, so its shape can be cross-checked against the OpenAPI
// requestBody schema for the same route (issue #256).
type requestBodyBinding struct {
	// structType is the Go struct type bound by ValidateJSONMiddleware or a
	// handler's own ShouldBindJSON, when that type is reflectable (named,
	// exported, or otherwise addressable from this package). nil when
	// boundFieldsOverride is used instead.
	structType reflect.Type
	// boundFieldsOverride hand-mirrors the JSON field names of a handler's
	// anonymous or function-local inline bind struct, for the handful of
	// routes where no importable type exists to reflect on. Keep in sync
	// with the cited call site — the same hand-maintained-mirror convention
	// TestOpenAPISpecValidates already uses for wantSchemas.
	boundFieldsOverride []string
	// validated is true when middleware.ValidateStruct (go-playground
	// validator, driven by `validate:"..."` tags) actually runs on the
	// bound struct in production. Several direct-ShouldBindJSON handlers
	// have `validate` tags on their DTO but never call ValidateStruct (they
	// do their own ad hoc checks instead) — validated must reflect what
	// really runs, not what the tags merely claim.
	validated bool
}

func bindingFor(v any) requestBodyBinding {
	return requestBodyBinding{structType: reflect.TypeOf(v), validated: true}
}

// requestBodyBindings mirrors every POST/PUT/PATCH route in
// routes/routes.go that binds a JSON request body, keyed the same way
// TestOpenAPIRouteCoverage keys the live router ("METHOD /spec/path"). This
// table must be kept in sync with routes.go by hand; requestFieldCoverage's
// completeness net (every registered mutating route must appear here or in
// noBodyMutatingRoutes) fails loudly if it drifts.
var requestBodyBindings = map[string]requestBodyBinding{
	// --- Auth / users (middleware-bound) ---
	"POST /register":                      bindingFor(models.UserRegistrationInput{}),
	"POST /password-reset/request":        bindingFor(models.PasswordResetRequestInput{}),
	"POST /password-reset/confirm":        bindingFor(models.PasswordResetConfirmInput{}),
	"POST /users/change-password":         bindingFor(models.ChangePasswordInput{}),
	"PATCH /users/enabled-contact-fields": bindingFor(models.EnabledContactFieldsInput{}),
	"PATCH /users/me/self-contact":        bindingFor(models.SelfContactInput{}),

	// --- Auth / users (direct ShouldBindJSON) ---
	"POST /login":              bindingFor(controllers.LoginInput{}),
	"PATCH /users/language":    bindingFor(controllers.UpdateLanguageInput{}),
	"PATCH /users/date-format": bindingFor(controllers.UpdateDateFormatInput{}),
	"POST /check-password-strength": {
		boundFieldsOverride: []string{"password"}, // user_controller.go CheckPasswordStrength
	},
	"POST /login/2fa": {
		boundFieldsOverride: []string{"code"}, // two_factor_controller.go Complete2FALogin
	},
	"POST /users/2fa/confirm": {
		boundFieldsOverride: []string{"code"}, // two_factor_controller.go ConfirmTwoFactor
	},
	"POST /users/2fa/disable": {
		boundFieldsOverride: []string{"code"}, // two_factor_controller.go DisableTwoFactor
	},
	"POST /users/2fa/recovery-codes/regenerate": {
		boundFieldsOverride: []string{"code"}, // two_factor_controller.go RegenerateRecoveryCodes
	},

	// --- Contacts ---
	"POST /contacts/merge/preview":             bindingFor(models.ContactMergeRequest{}),
	"POST /contacts/merge":                     bindingFor(models.ContactMergeRequest{}),
	"POST /contacts":                           bindingFor(models.ContactRecordInput{}),
	"PUT /contacts/{id}":                       bindingFor(models.ContactRecordInput{}),
	"POST /contacts/bulk":                      bindingFor(models.BulkContactOperationInput{}),
	"POST /contacts/duplicates/dismiss":        bindingFor(models.DuplicateDismissalInput{}),
	"POST /contacts/address-suggestions/apply": bindingFor(models.ApplyContactAddressSuggestionInput{}),
	"POST /contacts/import/preview":            bindingFor(models.ImportPreviewRequest{}),
	"POST /contacts/import/confirm":            bindingFor(models.ImportConfirmRequest{}),
	"POST /contacts/import/vcf/confirm":        bindingFor(models.ImportConfirmRequest{}),
	"POST /contacts/import/records":            bindingFor(models.ImportRecordsRequest{}),
	"POST /contacts/import/monica/connect":     bindingFor(models.MonicaConnectRequest{}),
	"POST /contacts/import/monica/fetch":       bindingFor(models.MonicaFetchRequest{}),
	"POST /contacts/import/monica/confirm":     bindingFor(models.MonicaConfirmRequest{}),
	"POST /contacts/import/meerkat/fetch":      bindingFor(models.MeerkatFetchRequest{}),
	"POST /contacts/import/meerkat/confirm":    bindingFor(models.SourceImportConfirmRequest{}),
	"POST /contact-shares":                     bindingFor(models.ContactShareInput{}),
	"POST /contact-shares/{id}/confirm":        bindingFor(models.ImportConfirmRequest{}),
	"PUT /contacts/{id}/field-values":          bindingFor(models.ContactFieldValuesInput{}),

	// --- Relationship edges / notes / activities ---
	"POST /relationship-edges":     bindingFor(models.RelationshipEdgeInput{}),
	"PUT /relationship-edges/{id}": bindingFor(models.RelationshipEdgeInput{}),
	"POST /contacts/{id}/notes":    bindingFor(models.NoteInput{}),
	"POST /notes":                  bindingFor(models.NoteInput{}),
	"PUT /notes/{id}":              bindingFor(models.NoteInput{}),
	"POST /activities":             bindingFor(models.ActivityInput{}),
	"PUT /activities/{id}":         bindingFor(models.ActivityInput{}),

	// --- Circles / link-field-types / households / tags ---
	"POST /circles":                 bindingFor(models.CircleInput{}),
	"PUT /circles/{id}":             bindingFor(models.CircleInput{}),
	"POST /circles/{id}/members":    bindingFor(models.CircleMemberInput{}),
	"POST /link-field-types":        bindingFor(models.LinkFieldTypeInput{}),
	"PUT /link-field-types/reorder": bindingFor(models.LinkFieldTypeReorderInput{}),
	"PUT /link-field-types/{id}":    bindingFor(models.LinkFieldTypeInput{}),
	"POST /households":              bindingFor(models.HouseholdInput{}),
	"PUT /households/{id}":          bindingFor(models.HouseholdInput{}),
	"POST /households/{id}/members": bindingFor(models.HouseholdMemberInput{}),
	"PATCH /households/{id}/members/{vcard_uid}": {
		boundFieldsOverride: []string{"role"}, // household_controller.go UpdateHouseholdMember's local memberUpdate type
	},
	"POST /households/suggestions/accept":  bindingFor(models.AcceptHouseholdSuggestionInput{}),
	"POST /households/suggestions/dismiss": bindingFor(models.DismissHouseholdSuggestionInput{}),
	"POST /tags":                           bindingFor(models.TagInput{}),
	"PUT /tags/{id}":                       bindingFor(models.TagInput{}),
	"POST /tags/{id}/contacts":             bindingFor(models.ContactTagInput{}),

	// --- Fields / life events / conversation agenda / gifts / preferences / cadence ---
	"POST /field-definitions":                 bindingFor(models.FieldDefinitionInput{}),
	"PUT /field-definitions/{id}":             bindingFor(models.FieldDefinitionInput{}),
	"POST /life-events":                       bindingFor(models.LifeEventInput{}),
	"PUT /life-events/{id}":                   bindingFor(models.LifeEventInput{}),
	"POST /conversation-agenda":               bindingFor(models.ConversationAgendaInput{}),
	"PUT /conversation-agenda/{id}":           bindingFor(models.ConversationAgendaInput{}),
	"PATCH /conversation-agenda/{id}/discuss": bindingFor(models.ConversationAgendaDiscussInput{}),
	"POST /gifts":                             bindingFor(models.GiftInput{}),
	"PUT /gifts/{id}":                         bindingFor(models.GiftInput{}),
	"POST /preferences":                       bindingFor(models.PreferenceInput{}),
	"PUT /preferences/{id}":                   bindingFor(models.PreferenceInput{}),
	"POST /cadence-policies":                  bindingFor(models.CadencePolicyInput{}),
	"PUT /cadence-policies/{id}":              bindingFor(models.CadencePolicyInput{}),

	// --- Reminders (bind the raw GORM model, not an Input DTO — deliberate
	// and already documented that way in openapi.yaml's Reminder schema). ---
	"POST /contacts/{id}/reminders": bindingFor(models.Reminder{}),
	"PUT /reminders/{id}":           bindingFor(models.Reminder{}),

	// --- API tokens / webhooks / notifications / calendars / subscriptions ---
	"POST /api-tokens": bindingFor(models.ApiTokenInput{}),
	// Issue #722: the device-grant surface.
	"POST /auth/device/grants":               bindingFor(models.DeviceGrantInput{}),
	"POST /auth/device/session":              bindingFor(models.DeviceGrantSessionInput{}),
	"POST /webhooks":                         bindingFor(models.WebhookInput{}),
	"PUT /webhooks/{id}":                     bindingFor(models.WebhookInput{}),
	"PUT /notifications/config":              bindingFor(models.NotificationConfigInput{}),
	"POST /notifications/push-subscriptions": bindingFor(models.PushSubscriptionInput{}),
	"POST /notifications/devices":            bindingFor(models.DeviceRegistrationInput{}),
	"POST /notifications/config/test": {
		structType: reflect.TypeOf(struct {
			Channel string `json:"channel"`
		}{}),
		validated: true, // notification_controller.go TestNotificationChannel calls middleware.ValidateStruct
	},
	"POST /calendars":                 bindingFor(models.CalendarSubscriptionInput{}),
	"PUT /calendars/{id}":             bindingFor(models.CalendarSubscriptionInput{}),
	"POST /contact-subscriptions":     bindingFor(models.ContactSubscriptionInput{}),
	"PUT /contact-subscriptions/{id}": bindingFor(models.ContactSubscriptionInput{}),

	// --- External identities / activities / integrations ---
	"POST /external-identities":     bindingFor(models.ExternalIdentityInput{}),
	"PUT /external-identities/{id}": bindingFor(models.ExternalIdentityInput{}),
	"POST /external-activities":     bindingFor(models.ExternalActivityInput{}),
	"PUT /external-activities/{id}": bindingFor(models.ExternalActivityInput{}),
	"PUT /immich/config":            bindingFor(models.ImmichConfigInput{}),
	"PUT /paperless/config":         bindingFor(models.PaperlessConfigInput{}),
	"PUT /seafile/config":           bindingFor(models.SeafileConfigInput{}),
	"PUT /nextcloud/config":         bindingFor(models.WebDAVConfigInput{}),
	"POST /immich/contacts/{vcard_uid}/link": {
		structType: reflect.TypeOf(struct {
			PersonID   string `json:"person_id"`
			PersonName string `json:"person_name,omitempty"`
		}{}),
		validated: true, // immich_controller.go LinkImmichContact
	},
	"POST /paperless/contacts/{vcard_uid}/link": {
		structType: reflect.TypeOf(struct {
			DocumentID string `json:"document_id"`
		}{}),
		validated: true, // paperless_controller.go LinkPaperlessContact
	},
	"POST /seafile/contacts/{vcard_uid}/link": {
		structType: reflect.TypeOf(struct {
			RepoID string `json:"repo_id"`
			Path   string `json:"path"`
			Name   string `json:"name"`
			Type   string `json:"type"`
			Size   int64  `json:"size,omitempty"`
			MTime  int64  `json:"mtime,omitempty"`
		}{}),
		validated: true, // seafile_controller.go LinkSeafileContact
	},
	"POST /nextcloud/contacts/{vcard_uid}/link": {
		structType: reflect.TypeOf(struct {
			Path       string `json:"path"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			Size       int64  `json:"size,omitempty"`
			ModifiedAt string `json:"modified_at,omitempty"`
			FileID     string `json:"file_id,omitempty"`
		}{}),
		validated: true, // webdav_controller.go LinkWebDAVContact
	},

	// --- Admin ---
	"POST /admin/users":       bindingFor(models.AdminUserCreateInput{}),
	"PATCH /admin/users/{id}": bindingFor(models.AdminUserUpdateInput{}),
}

// noBodyMutatingRoutes is every POST/PUT/PATCH route registered in
// routes/routes.go whose handler never binds a JSON body at all (path-param
// -only actions, multipart uploads, or DB/config-driven trigger endpoints).
// Together with requestBodyBindings this must account for every mutating
// route the live router registers — enforced by TestOpenAPIRequestFieldCoverage's
// completeness net.
var noBodyMutatingRoutes = map[string]bool{
	"POST /logout":                                true,
	"POST /users/2fa/setup":                       true,
	"POST /contacts/address-suggestions":          true,
	"POST /contacts/{id}/archive":                 true,
	"POST /contacts/{id}/unarchive":               true,
	"POST /contacts/{id}/favorite":                true,
	"POST /contacts/{id}/unfavorite":              true,
	"POST /contacts/import/upload":                true,
	"POST /contacts/import/vcf/upload":            true,
	"POST /contacts/import/jscontact/upload":      true,
	"POST /contacts/import/monica/cancel":         true,
	"POST /contacts/import/meerkat/upload":        true,
	"POST /contacts/import/meerkat/cancel":        true,
	"POST /contact-shares/{id}/accept":            true,
	"POST /contact-shares/{id}/decline":           true,
	"POST /relationship-edges/suggest":            true,
	"PATCH /relationship-edges/{id}/accept":       true,
	"POST /contacts/{id}/attachments":             true,
	"POST /contacts/{id}/profile_picture":         true,
	"POST /households/{id}/suggest-relationships": true,
	"POST /households/suggest-addresses":          true,
	"POST /reminders/{id}/complete":               true,
	"POST /webhooks/{id}/test":                    true,
	"POST /audit/{id}/undo":                       true,
	"POST /calendars/{id}/sync":                   true,
	"POST /contact-subscriptions/{id}/sync":       true,
	"POST /immich/test-connection":                true,
	"POST /immich/sync":                           true,
	"POST /paperless/test-connection":             true,
	"POST /seafile/test-connection":               true,
	"POST /nextcloud/test-connection":             true,
	"POST /admin/users/{id}/reset-2fa":            true,
	"POST /admin/trigger-reminders":               true,
	"POST /admin/trigger-purge":                   true,
	"POST /admin/search/rebuild":                  true,
	"POST /admin/contacts/rebuild-derived":        true,
	"POST /reach-out-suggestions/{id}/dismiss":    true,
	"POST /api-tokens/revoke-all":                 true,
	"POST /auth/device/grants/revoke-all":         true,
	"POST /api-tokens/{id}/rotate":                true,
	"POST /contact-sync-conflicts/{id}/restore":   true,
	"POST /contact-sync-conflicts/{id}/dismiss":   true,
}

// boundJSONFields returns the top-level JSON field names t would bind via
// encoding/json (which is what ShouldBindJSON uses under the hood):
// unexported fields are skipped, `json:"-"` fields are skipped, an
// anonymous/embedded struct field with no explicit tag name has its own
// fields promoted (so gorm.Model's ID/CreatedAt/UpdatedAt/DeletedAt surface
// correctly for models.Reminder), and everything else uses its tag name (or
// the Go field name when the tag has no name segment).
func boundJSONFields(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	fields := map[string]bool{}
	if t.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" && f.Anonymous {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k := range boundJSONFields(ft) {
					fields[k] = true
				}
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = true
	}
	return fields
}

// schemaPropertyNames returns the top-level property names a requestBody
// schema documents, merging any allOf branches' properties in (this spec
// doesn't use allOf for request bodies today, but the merge keeps this
// correct if that changes).
func schemaPropertyNames(ref *openapi3.SchemaRef) map[string]bool {
	names := map[string]bool{}
	if ref == nil || ref.Value == nil {
		return names
	}
	for name := range ref.Value.Properties {
		names[name] = true
	}
	for _, sub := range ref.Value.AllOf {
		for name := range schemaPropertyNames(sub) {
			names[name] = true
		}
	}
	return names
}

// requestBodySchema resolves the application/json requestBody schema for a
// documented operation, or nil if it has none.
func requestBodySchema(op *openapi3.Operation) *openapi3.SchemaRef {
	if op == nil || op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	mt := op.RequestBody.Value.Content.Get("application/json")
	if mt == nil {
		return nil
	}
	return mt.Schema
}

// TestOpenAPIRequestFieldCoverage (issue #256) proves every documented
// requestBody schema's fields match what the live handler actually binds
// from JSON, in both directions: a field the handler binds that the spec
// doesn't document is server-side mass-assignment creep; a field the spec
// documents that the handler never binds is dead documentation (the
// "silently ignores a documented field" bug this issue exists to catch).
func TestOpenAPIRequestFieldCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	routes.RegisterRoutes(router, openAPITestConfig(), db, nil)

	doc := loadOpenAPIDoc(t)

	// Sanity floor: guard against a vacuous pass if the tables are
	// accidentally gutted.
	require.GreaterOrEqual(t, len(requestBodyBindings), 85,
		"unexpectedly small requestBodyBindings table — this test may be broken")
	require.GreaterOrEqual(t, len(noBodyMutatingRoutes), 25,
		"unexpectedly small noBodyMutatingRoutes table — this test may be broken")

	// Completeness net: every registered mutating route must be accounted
	// for in one of the two tables, so a new route silently skips this
	// check the same way T8's drift test prevents an undocumented route.
	for _, r := range router.Routes() {
		if isCardDAVRoute(r.Path) {
			continue
		}
		if r.Method != "POST" && r.Method != "PUT" && r.Method != "PATCH" {
			continue
		}
		key := r.Method + " " + ginToOpenAPIPath(specPathFromRegistered(r.Path))
		if _, ok := requestBodyBindings[key]; ok {
			continue
		}
		if noBodyMutatingRoutes[key] {
			continue
		}
		t.Errorf("mutating route %s is registered in routes/routes.go but is missing from both "+
			"requestBodyBindings and noBodyMutatingRoutes in openapi_request_test.go", key)
	}

	for key, binding := range requestBodyBindings {
		method, path, _ := strings.Cut(key, " ")
		pi := doc.Paths.Find(path)
		if pi == nil {
			t.Errorf("%s: openapi.yaml has no path %s", key, path)
			continue
		}
		op := operationForMethod(pi, method)
		if op == nil {
			t.Errorf("%s: openapi.yaml's %s has no %s operation", key, path, method)
			continue
		}
		schema := requestBodySchema(op)
		if schema == nil {
			t.Errorf("%s: openapi.yaml documents no requestBody, but routes.go binds a JSON body here", key)
			continue
		}
		schemaFields := schemaPropertyNames(schema)

		var goFields map[string]bool
		if binding.structType != nil {
			goFields = boundJSONFields(binding.structType)
		} else {
			goFields = map[string]bool{}
			for _, f := range binding.boundFieldsOverride {
				goFields[f] = true
			}
		}

		for f := range goFields {
			if !schemaFields[f] {
				t.Errorf("%s: handler binds JSON field %q but openapi.yaml's requestBody schema has no matching property "+
					"(mass-assignment creep or stale docs)", key, f)
			}
		}
		for f := range schemaFields {
			if !goFields[f] {
				t.Errorf("%s: openapi.yaml documents field %q but the handler never binds it "+
					"(dead documentation — a client following the docs would have this field silently ignored)", key, f)
			}
		}
	}
}

// exampleFromSchema generates a JSON-serializable value satisfying schema:
// the spec's own example if it set one, otherwise a value walking every
// documented property (not just required ones, to exercise as much of the
// documented shape as possible). seen guards against $ref cycles — it is
// mutated and restored around each branch so sibling properties can still
// share a ref.
func exampleFromSchema(ref *openapi3.SchemaRef, seen map[string]bool) any {
	if ref == nil || ref.Value == nil {
		return nil
	}
	if ref.Ref != "" {
		if seen[ref.Ref] {
			// Cycle: stop recursing, but still return a type-appropriate
			// empty value rather than null — most schemas here don't set
			// `nullable: true`, so null would itself fail validation.
			switch {
			case ref.Value.Type.Is("array"):
				return []any{}
			case ref.Value.Type.Is("string"):
				return ""
			case ref.Value.Type.Is("integer") || ref.Value.Type.Is("number"):
				return 0
			case ref.Value.Type.Is("boolean"):
				return false
			default:
				return map[string]any{}
			}
		}
		seen[ref.Ref] = true
		defer delete(seen, ref.Ref)
	}
	schema := ref.Value

	if schema.Example != nil {
		return schema.Example
	}

	if len(schema.AllOf) > 0 {
		merged := map[string]any{}
		for _, sub := range schema.AllOf {
			if m, ok := exampleFromSchema(sub, seen).(map[string]any); ok {
				for k, v := range m {
					merged[k] = v
				}
			}
		}
		for name, propRef := range schema.Properties {
			merged[name] = exampleFromSchema(propRef, seen)
		}
		return merged
	}

	switch {
	case schema.Type.Is("array"):
		if schema.Items == nil {
			return []any{}
		}
		n := 1
		if schema.MinItems > uint64(n) {
			n = int(schema.MinItems)
		}
		items := make([]any, n)
		for i := range items {
			items[i] = exampleFromSchema(schema.Items, seen)
		}
		return items
	case schema.Type.Is("boolean"):
		return false
	case schema.Type.Is("integer") || schema.Type.Is("number"):
		if len(schema.Enum) > 0 {
			return schema.Enum[0]
		}
		if schema.Min != nil && *schema.Min > 1 {
			return *schema.Min
		}
		return 1
	case schema.Type.Is("string"):
		return exampleString(schema)
	case schema.Type.Is("object") || (schema.Type.IsEmpty() && len(schema.Properties) > 0):
		obj := map[string]any{}
		for name, propRef := range schema.Properties {
			obj[name] = exampleFromSchema(propRef, seen)
		}
		return obj
	default:
		// schema.Type is empty ({}, no properties): a deliberately
		// free-form "any JSON value" field (e.g. FieldValueInput.value,
		// whose real type varies per FieldDefinition at runtime). Generate
		// a concrete placeholder rather than null — an empty schema still
		// requires nullable:true for null to validate.
		return "test"
	}
}

func exampleString(schema *openapi3.Schema) string {
	if len(schema.Enum) > 0 {
		if s, ok := schema.Enum[0].(string); ok {
			return s
		}
	}
	switch schema.Format {
	case "uuid":
		return uuid.NewString()
	case "date":
		return "2024-01-01"
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "email":
		return "test@example.com"
	case "uri", "url":
		return "https://example.com"
	}
	s := "test"
	if schema.MinLength > 0 && len(s) < int(schema.MinLength) {
		s = strings.Repeat("x", int(schema.MinLength))
	}
	if schema.MaxLength != nil && uint64(len(s)) > *schema.MaxLength {
		n := int(*schema.MaxLength)
		if n < 1 {
			n = 1
		}
		s = strings.Repeat("x", n)
	}
	return s
}

// pathParamValues generates a plausible value for every path parameter an
// operation (or its PathItem) declares, so ValidateRequest's own parameter
// validation doesn't fail for reasons unrelated to the request body.
func pathParamValues(pi *openapi3.PathItem, op *openapi3.Operation) map[string]string {
	params := map[string]string{}
	add := func(prs openapi3.Parameters) {
		for _, p := range prs {
			if p.Value == nil || p.Value.In != openapi3.ParameterInPath {
				continue
			}
			v := exampleFromSchema(p.Value.Schema, map[string]bool{})
			params[p.Value.Name] = toParamString(v)
		}
	}
	add(pi.Parameters)
	add(op.Parameters)
	return params
}

func toParamString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return "1"
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return "1"
		}
		return strings.Trim(string(b), `"`)
	}
}

// TestOpenAPIRequestBodyExamplesValidate (issue #256) generates a body for
// every documented requestBody from its own schema (there are no
// requestBody `examples:` in openapi.yaml yet, so this always exercises the
// generator path — it prefers a spec-authored `example:` when one exists),
// validates it against the spec itself with openapi3filter.ValidateRequest,
// and — for every binding whose struct is actually go-playground-validated
// in production — also unmarshals the same generated body into the real Go
// DTO and runs middleware.ValidateStruct on it. That second check is what
// actually proves the live server accepts a body shaped exactly like the
// docs, not just that the generator's own output satisfies the schema it
// was generated from.
func TestOpenAPIRequestBodyExamplesValidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	routes.RegisterRoutes(router, openAPITestConfig(), db, nil)

	doc := loadOpenAPIDoc(t)

	// Confirm every registered route this table claims actually exists,
	// mirroring TestOpenAPIRouteCoverage's forward check, before spending
	// time validating bodies against routes that were never registered.
	registered := map[string]bool{}
	for _, r := range router.Routes() {
		registered[r.Method+" "+ginToOpenAPIPath(specPathFromRegistered(r.Path))] = true
	}

	for key, binding := range requestBodyBindings {
		method, path, _ := strings.Cut(key, " ")
		require.True(t, registered[key], "%s: not registered by routes.RegisterRoutes", key)

		pi := doc.Paths.Find(path)
		require.NotNil(t, pi, "%s: openapi.yaml has no path %s", key, path)
		op := operationForMethod(pi, method)
		require.NotNil(t, op, "%s: openapi.yaml's %s has no %s operation", key, path, method)

		schema := requestBodySchema(op)
		require.NotNil(t, schema, "%s: openapi.yaml documents no requestBody", key)

		example := exampleFromSchema(schema, map[string]bool{})
		body, err := json.Marshal(example)
		require.NoError(t, err, "%s: failed to marshal generated example", key)

		req := httptest.NewRequest(method, "http://spec.local"+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rvi := &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParamValues(pi, op),
			Route:      &routers.Route{Spec: doc, Path: path, PathItem: pi, Method: method, Operation: op},
			Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		}
		err = openapi3filter.ValidateRequest(context.Background(), rvi)
		require.NoError(t, err, "%s: generated example %s does not validate against its own documented schema", key, body)

		if binding.validated && binding.structType != nil {
			obj := reflect.New(binding.structType).Interface()
			require.NoError(t, json.Unmarshal(body, obj), "%s: failed to unmarshal generated example into %s", key, binding.structType)
			if verrs := middleware.ValidateStruct(obj); len(verrs) > 0 {
				t.Errorf("%s: a body shaped exactly like the documented schema fails the live go-playground validator: %+v", key, verrs)
			}
		}
	}
}
