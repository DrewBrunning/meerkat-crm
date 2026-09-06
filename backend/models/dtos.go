package models

import (
	"encoding/json"

	"mycorrhizal/contactmodel"
	"time"
)

// ActivityInput represents the DTO for creating/updating activities
type ActivityInput struct {
	Title       string    `json:"title" validate:"required,min=1,max=200"`
	Description string    `json:"description" validate:"max=2000"`
	Location    string    `json:"location" validate:"max=300"`
	Date        time.Time `json:"date" validate:"required"`
	ContactIDs  []uint    `json:"contact_ids"` // Accept an array of contact IDs for many-to-many association
	// Type/ExternalRef are Interaction fields (activity.go) -- open/
	// conventional, no oneof validation, matching Activity.Type's own doc
	// comment on why it's deliberately unvalidated.
	Type        string `json:"type,omitempty"`
	ExternalRef string `json:"external_ref,omitempty"`
}

// CircleInput is the DTO for creating/updating a Circle (circle.go). Only
// Name is editable here -- membership lifecycle lives in its own
// AddCircleMember/RemoveCircleMember endpoints, not folded into update.
type CircleInput struct {
	Name string `json:"name" validate:"required,min=1,max=200"`
}

// CircleMemberInput is the DTO for POST /circles/:id/members.
type CircleMemberInput struct {
	MemberVCardUID string `json:"member_vcard_uid" validate:"required,uuid4"`
}

// HouseholdInput is the DTO for creating/updating a Household (household.go).
// Membership lifecycle lives in its own AddHouseholdMember/
// RemoveHouseholdMember endpoints, not folded into update — the same split as
// CircleInput. Type is a closed switch (drives the suggestion engine), so it
// carries the same `oneof` validation as the Household model field itself.
type HouseholdInput struct {
	Name    string                `json:"name" validate:"required,min=1,max=200"`
	Type    string                `json:"type" validate:"required,oneof=family_unit roommates other"`
	Address *contactmodel.Address `json:"address,omitempty"`
}

// HouseholdMemberInput is the DTO for POST /households/:id/members. Role is
// conventional, not validated (HouseholdRole* constants) — deliberately open,
// mirroring HouseholdMember.Role's own doc comment.
type HouseholdMemberInput struct {
	MemberVCardUID string `json:"member_vcard_uid" validate:"required,uuid4"`
	Role           string `json:"role,omitempty"`
	Since          string `json:"since,omitempty"`
	Until          string `json:"until,omitempty"`
}

// AcceptHouseholdSuggestionInput is the DTO for POST /households/suggestions/
// accept (T40 — T40). The suggested group is identified by its member VCardUIDs; the
// server recomputes the shared address and the dismissal hashes from the
// members' real contacts data, so a client can neither fabricate a group nor
// dismiss/accept an arbitrary hash. Name/Type are optional with server-side
// defaults.
type AcceptHouseholdSuggestionInput struct {
	MemberVCardUIDs []string `json:"member_vcard_uids" validate:"required,min=2,dive,required,uuid4"`
	Name            string   `json:"name,omitempty" validate:"omitempty,max=200"`
	Type            string   `json:"type,omitempty" validate:"omitempty,oneof=family_unit roommates other"`
}

// DismissHouseholdSuggestionInput is the DTO for POST /households/suggestions/
// dismiss (T40).
type DismissHouseholdSuggestionInput struct {
	MemberVCardUIDs []string `json:"member_vcard_uids" validate:"required,min=2,dive,required,uuid4"`
}

// ApplyContactAddressSuggestionInput is the DTO for POST
// /contacts/address-suggestions/apply: it names a single address suggestion by
// its identity (the recipient contact, the source that implies the shared
// address, and the normalized address key). The server re-derives the address
// from the current graph rather than trusting the client to supply it.
type ApplyContactAddressSuggestionInput struct {
	ContactVCardUID string `json:"contact_vcard_uid" validate:"required,uuid4"`
	SourceKind      string `json:"source_kind" validate:"required,oneof=relationship household"`
	SourceID        string `json:"source_id" validate:"required"`
	AddressKey      string `json:"address_key" validate:"required"`
}

// TagInput is the DTO for creating/updating a Tag (tag.go). Only Name is
// editable here -- tagging lifecycle lives in its own AddContactTag/
// RemoveContactTag endpoints, not folded into update.
type TagInput struct {
	Name string `json:"name" validate:"required,min=1,max=200"`
}

// ContactTagInput is the DTO for POST /tags/:id/contacts.
type ContactTagInput struct {
	ContactVCardUID string `json:"contact_vcard_uid" validate:"required,uuid4"`
}

// LifeEventInput is the DTO for creating/updating a LifeEvent (life_event.go).
// Category (T36) is independently submitted, not derived server-side from
// Type — the frontend's cascading category->type picker already knows both,
// and a custom (free-text) Type has no registry entry to derive from.
type LifeEventInput struct {
	EntityID         string                    `json:"entity_id" validate:"required,uuid4"`
	Type             string                    `json:"type,omitempty"`
	Category         string                    `json:"category,omitempty" validate:"omitempty,life_event_category"`
	Date             *contactmodel.PartialDate `json:"date,omitempty"`
	Description      string                    `json:"description,omitempty" validate:"max=2000"`
	Source           string                    `json:"source,omitempty" validate:"omitempty,oneof=user imported ai-suggested"`
	RelatedEntityIDs []string                  `json:"related_entity_ids,omitempty"`
	Remind           bool                      `json:"remind,omitempty"`
}

// ConversationAgendaInput is the DTO for creating/updating a ConversationAgenda
// (conversation_agenda.go). Only content fields; the resolved state
// (discussed_at / activity_id) is mutated by PATCH /:id/discuss, never by
// create/update — updating an item's content must not silently re-open or
// re-close it.
type ConversationAgendaInput struct {
	EntityID     string `json:"entity_id" validate:"required,uuid4"`
	Content      string `json:"content" validate:"required,min=1,max=2000"`
	ReferenceURL string `json:"reference_url,omitempty" validate:"omitempty,httpurl,max=2000"`
}

// ConversationAgendaDiscussInput is the DTO for PATCH /conversation-agenda/:id/
// discuss. ActivityID is optional: when present, the interaction that covered
// the item is linked to it (feeding the timeline); when absent, the item is
// marked discussed without a link.
type ConversationAgendaDiscussInput struct {
	ActivityID *uint `json:"activity_id,omitempty"`
}

// GiftInput is the DTO for creating/updating a Gift. Status
// defaults to "idea" server-side when omitted — a gift idea is captured
// opportunistically without choosing a state. ValueCents/Currency must be set
// together (explicit-currency rule, enforced in the controller since this
// codebase has no cross-field validator). LifeEventID/ActivityID are optional
// soft references verified to belong to the user. URL/Notes (T35) are the
// optional product link and free-text context.
type GiftInput struct {
	EntityID    string     `json:"entity_id" validate:"required,uuid4"`
	Status      string     `json:"status,omitempty" validate:"omitempty,oneof=idea purchased given received"`
	Occasion    string     `json:"occasion,omitempty" validate:"max=200"`
	Description string     `json:"description" validate:"required,min=1,max=2000"`
	URL         string     `json:"url,omitempty" validate:"omitempty,httpurl,max=2000"`
	Notes       string     `json:"notes,omitempty" validate:"omitempty,max=2000"`
	Date        *time.Time `json:"date,omitempty"`
	ValueCents  int64      `json:"value_cents,omitempty"`
	Currency    string     `json:"currency,omitempty" validate:"omitempty,len=3"`
	LifeEventID string     `json:"life_event_id,omitempty" validate:"omitempty,uuid4"`
	ActivityID  *uint      `json:"activity_id,omitempty"`
}

// CadencePolicyInput is the DTO for creating/updating a CadencePolicy
// (cadence_policy.go). QualifyingTypes is deliberately not
// `oneof`-validated: Activity.Type is an open classifier, so a policy may
// name types outside the current InteractionType* constants. An empty list
// means every default-qualifying type counts (CadencePolicy.Qualifies).
type CadencePolicyInput struct {
	EntityID           string   `json:"entity_id" validate:"required,uuid4"`
	TargetIntervalDays int      `json:"target_interval_days" validate:"required,gt=0,lte=3650"`
	QualifyingTypes    []string `json:"qualifying_types,omitempty" validate:"dive,max=100"`
}

// FieldDefinitionInput is the DTO for creating/updating a FieldDefinition
// (field_definition.go, docs/adrs/0001-neutral-hub-and-spoke-contact-model.md). Label is
// display; Key is the stable machine name and — unlike every other field —
// is immutable after creation (see UpdateFieldDefinition's doc comment in
// field_definition_controller.go). Target/Type/Projection/Sensitivity each
// default server-side when omitted (contact / the model's own default
// values), mirroring PreferenceInput's defaults-not-required convention.
// Constraints carries the type-dependent validation rules and is validated
// indirectly at FieldValue-write time by services.ValidateFieldValue, not
// here.
type FieldDefinitionInput struct {
	Label       string           `json:"label" validate:"required,max=200"`
	Key         string           `json:"key" validate:"required,max=100"`
	Target      string           `json:"target,omitempty" validate:"omitempty,oneof=contact"`
	Type        string           `json:"type" validate:"required,oneof=string text number boolean date datetime uri email phone enum"`
	Constraints FieldConstraints `json:"constraints,omitempty"`
	Projection  string           `json:"projection,omitempty" validate:"omitempty,fielddefprojection"`
	Sensitivity string           `json:"sensitivity,omitempty" validate:"omitempty,oneof=normal private secret"`
}

// FieldValueInput is one element of ContactFieldValuesInput — a
// FieldValue to write for one FieldDefinition of one contact.
// Value is the raw JSON payload: a Multi field is a JSON array,
// a scalar is a bare JSON value (string/number/boolean). It is validated
// against the definition's Type+Constraints in the controller via
// services.ValidateFieldValue — a type mismatch is a 400, not a 500.
type FieldValueInput struct {
	FieldDefinitionID string          `json:"field_definition_id" validate:"required"`
	Value             json.RawMessage `json:"value" validate:"required"`
}

// ContactFieldValuesInput is the request body for PUT
// /contacts/:id/field-values — the full-replace value set for one contact
// (see ReplaceContactFieldValues' doc comment). An empty array clears every
// value on the contact.
type ContactFieldValuesInput struct {
	FieldValues []FieldValueInput `json:"field_values" validate:"dive"`
}

// PreferenceInput is the DTO for creating/updating a Preference
// (preference.go). Category is deliberately not `oneof`-validated —
// it is an open classifier (see Preference's doc comment). Sensitivity
// defaults to normal server-side when omitted (mirroring
// FieldValue/RelationshipEdge's own defaults), not validated as required.
type PreferenceInput struct {
	EntityID      string     `json:"entity_id" validate:"required,uuid4"`
	Category      string     `json:"category" validate:"required,max=100"`
	Key           string     `json:"key,omitempty" validate:"omitempty,max=100"`
	Value         string     `json:"value" validate:"required,max=1000"`
	Notes         string     `json:"notes,omitempty" validate:"omitempty,max=2000"`
	Source        string     `json:"source,omitempty" validate:"omitempty,oneof=conversation_note user ai-suggested external"`
	Confidence    *float64   `json:"confidence,omitempty"`
	LastConfirmed *time.Time `json:"last_confirmed,omitempty"`
	Sensitivity   string     `json:"sensitivity,omitempty" validate:"omitempty,oneof=normal private secret"`
}

// CalendarSubscriptionInput is the DTO for creating/updating a calendar subscription.
// Credentials are optional (public/unprotected calendars). On update, an empty
// password keeps the stored one; set ClearPassword to remove it.
type CalendarSubscriptionInput struct {
	Name          string `json:"name" validate:"required,min=1,max=100"`
	URL           string `json:"url" validate:"required,url,max=2048"`
	Username      string `json:"username" validate:"omitempty,max=200"`
	Password      string `json:"password" validate:"omitempty,max=500"`
	ClearPassword bool   `json:"clear_password"`
	SyncEnabled   *bool  `json:"sync_enabled"`
	PastDays      *int   `json:"past_days" validate:"omitempty,min=0,max=3650"`
	FutureDays    *int   `json:"future_days" validate:"omitempty,min=0,max=3650"`
}

// CalendarSubscriptionResponse is the DTO returned for a calendar subscription (no password)
type CalendarSubscriptionResponse struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	Username       string     `json:"username"`
	HasPassword    bool       `json:"has_password"`
	SyncEnabled    bool       `json:"sync_enabled"`
	PastDays       int        `json:"past_days"`
	FutureDays     int        `json:"future_days"`
	LastSyncedAt   *time.Time `json:"last_synced_at"`
	LastSyncStatus string     `json:"last_sync_status"`
	LastSyncError  string     `json:"last_sync_error"`
	CreatedAt      time.Time  `json:"created_at"`
	SyncHealthResponse
}

// ContactSubscriptionInput is the DTO for creating/updating a CardDAV
// contact subscription . Credentials are optional (some servers
// allow anonymous/public read). On update, an empty password keeps the
// stored one; set ClearPassword to remove it. Mirrors
// CalendarSubscriptionInput's shape, minus PastDays/FutureDays (no contacts
// analog).
type ContactSubscriptionInput struct {
	Name          string `json:"name" validate:"required,min=1,max=100"`
	URL           string `json:"url" validate:"required,url,max=2048"`
	Username      string `json:"username" validate:"omitempty,max=200"`
	Password      string `json:"password" validate:"omitempty,max=500"`
	ClearPassword bool   `json:"clear_password"`
	SyncEnabled   *bool  `json:"sync_enabled"`
}

// ContactSubscriptionResponse is the DTO returned for a contact subscription
// (no password, no sync token).
type ContactSubscriptionResponse struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	Username       string     `json:"username"`
	HasPassword    bool       `json:"has_password"`
	SyncEnabled    bool       `json:"sync_enabled"`
	LastSyncedAt   *time.Time `json:"last_synced_at"`
	LastSyncStatus string     `json:"last_sync_status"`
	LastSyncError  string     `json:"last_sync_error"`
	CreatedAt      time.Time  `json:"created_at"`
	SyncHealthResponse
	// PendingConflicts is the count of unreviewed local edits this
	// subscription's syncs have overwritten (issue #395's ContactSyncConflict
	// rows, status = pending). Calendars have no analog.
	PendingConflicts int64 `json:"pending_conflicts"`
}

// NoteInput represents the DTO for creating/updating notes
type NoteInput struct {
	Content   string    `json:"content" validate:"required,min=1,max=5000"`
	Date      time.Time `json:"date" validate:"required"`
	ContactID *uint     `json:"contact_id" validate:"omitempty,gt=0"`
}

// represents the DTO for updating which extended contact fields are visible in the UI. A nil/absent list means "use the default set"
type EnabledContactFieldsInput struct {
	Fields []string `json:"fields" validate:"dive,max=50"`
}

// UserRegistrationInput represents the DTO for user registration
// This DTO intentionally excludes IsAdmin to prevent mass assignment attacks
type UserRegistrationInput struct {
	Username string `json:"username" validate:"required,min=1,max=50,no_at_sign"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,strong_password"`
	Language string `json:"language" validate:"omitempty,oneof=en de it es fr"`
}

// PasswordResetRequestInput captures email for initiating password reset
type PasswordResetRequestInput struct {
	Email string `json:"email" validate:"required,email"`
}

// PasswordResetConfirmInput carries token and new password for reset flow
type PasswordResetConfirmInput struct {
	Token    string `json:"token" validate:"required,min=16"`
	Password string `json:"password" validate:"required,min=8,strong_password"`
}

// ChangePasswordInput is used by authenticated users to rotate credentials
type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required,min=8,strong_password"`
}

// ThinContactInput creates a not-yet-existing "thin" Contact inline as one
// endpoint of a RelationshipEdge — the graph-model equivalent of the legacy
// Relationship{Name, Gender, Birthday, RelatedContactID: nil} case (the
// legacy model and its one-time migration tool are both gone;
// this comment is kept only as historical context for the field
// mapping's origin).
type ThinContactInput struct {
	Name     string `json:"name" validate:"required,min=1,max=100"`
	Gender   string `json:"gender" validate:"omitempty,max=100"`
	Birthday string `json:"birthday" validate:"omitempty,birthday"`
}

// RelationshipEdgeInput is the DTO for creating/updating a RelationshipEdge.
// Each endpoint is either an existing Contact (SourceID/TargetID, a
// Contact.VCardUID) or a not-yet-existing person described inline
// (SourceThin/TargetThin) — exactly one of the pair per endpoint, enforced
// in the controller (not via validate tags — this codebase has no existing
// precedent for required_without/excluded_with, and a controller-level
// check gives a much more precise per-field error).
//
// Directional/Source/Confidence/Status are deliberately absent: the server
// always derives Directional from Type (!IsSymmetricRelationType), and
// always sets Source/Confidence/Status itself — a client can never
// mass-assign a fabricated provenance or self-confirm a status. A
// user-created edge is always Source: user-confirmed, Confidence: 1.0,
// Status: confirmed. Status only ever changes via AcceptRelationshipEdge.
type RelationshipEdgeInput struct {
	SourceID   string            `json:"source_id,omitempty" validate:"omitempty,uuid4"`
	SourceThin *ThinContactInput `json:"source_thin,omitempty"`
	TargetID   string            `json:"target_id,omitempty" validate:"omitempty,uuid4"`
	TargetThin *ThinContactInput `json:"target_thin,omitempty"`

	Type        string                 `json:"type" validate:"required,relation_type"`
	Sensitivity string                 `json:"sensitivity,omitempty" validate:"omitempty,oneof=normal private secret"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ContactResponse represents the DTO returned from GET /contacts with photo thumbnail
type ContactResponse struct {
	Contact
	PhotoThumbnail string `json:"photo_thumbnail"`
}

// Birthday represents an upcoming contact birthday
type Birthday struct {
	Type           string `json:"type"`                      // always "contact"
	Name           string `json:"name"`                      // Unified display name
	Birthday       string `json:"birthday"`                  // Birthday in YYYY-MM-DD format
	PhotoThumbnail string `json:"photo_thumbnail,omitempty"` // Profile picture thumbnail (base64)
	ContactID      uint   `json:"contact_id"`                // Contact ID
}

// GraphNode represents a node in the network visualization (contact or activity)
//
// No Circles field: it used to carry the legacy flat Contact.Circles column,
// but NetworkGraph.tsx groups and colours nodes from the real Circle entities
// (the circleNamesByUid map useCircles builds), and the frontend's own
// GraphNode type never declared `circles` at all. It was dead payload shipping
// stale legacy strings on every graph request.
type GraphNode struct {
	ID             string `json:"id"`                        // "c-{contactID}" or "a-{activityID}"
	Type           string `json:"type"`                      // "contact" or "activity"
	Label          string `json:"label"`                     // Display name or activity title
	PhotoThumbnail string `json:"photo_thumbnail,omitempty"` // Profile picture for contacts (base64)
}

// GraphEdge represents an edge in the network visualization
type GraphEdge struct {
	ID     string `json:"id"`     // Unique edge ID
	Source string `json:"source"` // Source node ID
	Target string `json:"target"` // Target node ID
	Type   string `json:"type"`   // "relationship" or "activity"
	Label  string `json:"label"`  // Relationship type or activity title
}

// GraphResponse is the API response for the network graph
type GraphResponse struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphChainStep is one hop in a traversal chain (T10). Relation is
// the *display* token describing what the NEXT contact is to the PREVIOUS
// contact in the chain — the inverse is already applied when the hop walked
// against the edge's stored direction. For "John's sister's husband" the
// chain is [{Sister, sister_of}, {Husband, spouse_of}].
type GraphChainStep struct {
	ContactID       uint   `json:"contact_id"`
	ContactVCardUID string `json:"contact_vcard_uid"`
	ContactName     string `json:"contact_name"`
	Relation        string `json:"relation"`
}

// GraphChain is one traversal result: how a target contact is reachable from
// the starting contact, as a chain of relation steps. The anchor (start)
// contact is implicit — Steps[0] is the first hop away from it.
type GraphChain struct {
	TargetID       uint             `json:"target_id"`
	TargetVCardUID string           `json:"target_vcard_uid"`
	TargetName     string           `json:"target_name"`
	Depth          int              `json:"depth"`
	Steps          []GraphChainStep `json:"steps"`
}

// GraphConnectionsResponse is the API response for GET /graph/connections.
type GraphConnectionsResponse struct {
	FromVCardUID string       `json:"from_vcard_uid"`
	FromName     string       `json:"from_name"`
	Depth        int          `json:"depth"`
	Chains       []GraphChain `json:"chains"`
}

// SelfContactInput is the DTO for PATCH /users/me/self-contact (T90) — the
// caller's "Me" contact pointer. A null, absent, or empty vcard_uid clears
// the link (omitempty lets "" through); a non-empty value must resolve to a
// non-deleted contact owned by the caller (the handler returns 404
// otherwise). uuid4 matches Contact.VCardUID's generation
// (models/contact.go BeforeCreate), and the handler's ownership lookup is the
// real gate — this tag only rejects obviously malformed input.
type SelfContactInput struct {
	VCardUID string `json:"vcard_uid" validate:"omitempty,uuid4"`
}

// AdminUserResponse - user data returned to admin (no password)
type AdminUserResponse struct {
	ID         uint      `json:"id"`
	Username   string    `json:"username"`
	Email      string    `json:"email"`
	Language   string    `json:"language"`
	DateFormat string    `json:"date_format"`
	IsAdmin    bool      `json:"is_admin"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// /users/me payload: standard user fields plus caller's UI preferences (enabled contact fields)
type CurrentUserResponse struct {
	AdminUserResponse
	EnabledContactFields []string `json:"enabled_contact_fields"`
	SelfContactVCardUID  *string  `json:"self_contact_vcard_uid,omitempty"`
}

// AdminUserCreateInput - DTO for admin creating a new user. Unlike
// UserRegistrationInput (self-registration), this intentionally DOES accept
// IsAdmin: the caller already passed AdminMiddleware to reach this route, so
// setting it here isn't mass assignment by an untrusted party — the same
// trust boundary AdminUserUpdateInput's IsAdmin field relies on.
type AdminUserCreateInput struct {
	Username string `json:"username" validate:"required,min=1,max=50,no_at_sign"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,strong_password"`
	IsAdmin  bool   `json:"is_admin"`
}

// AdminUserUpdateInput - DTO for admin updating a user
type AdminUserUpdateInput struct {
	Username *string `json:"username" validate:"omitempty,min=1,max=50,no_at_sign"`
	Email    *string `json:"email" validate:"omitempty,email"`
	Password *string `json:"password" validate:"omitempty,min=8,strong_password"`
	IsAdmin  *bool   `json:"is_admin"`
}

// AdminUsersListResponse - paginated list of users
type AdminUsersListResponse struct {
	Users      []AdminUserResponse `json:"users"`
	Total      int64               `json:"total"`
	Page       int                 `json:"page"`
	Limit      int                 `json:"limit"`
	TotalPages int                 `json:"total_pages"`
}

// DefaultApiTokenExpiryDays is used when a token is created without an explicit
// lifetime. Tokens are long-lived bearer credentials, so they get a bounded
// life by default rather than living forever until someone remembers to revoke.
const DefaultApiTokenExpiryDays = 90

// MaxApiTokenExpiryDays caps how far out a token may be dated.
const MaxApiTokenExpiryDays = 365

// DefaultApiTokenScope is used when a token is created without an explicit scope.
const DefaultApiTokenScope = "full"

// ApiTokenInput represents the DTO for creating an API token
type ApiTokenInput struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
	// ExpiresInDays is optional; omitting it applies DefaultApiTokenExpiryDays.
	// There is deliberately no "never expires" option.
	ExpiresInDays *int `json:"expires_in_days" validate:"omitempty,min=1,max=365"`
	// Scope is optional; omitting it applies DefaultApiTokenScope ("full").
	// "carddav" restricts the token to the CardDAV Basic-Auth path only.
	Scope string `json:"scope" validate:"omitempty,oneof=full carddav"`
}

// ApiTokenResponse represents the DTO returned for an API token
type ApiTokenResponse struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Scope      string     `json:"scope"`
}

// ApiTokenCreateResponse is returned on token creation and includes the plaintext token
type ApiTokenCreateResponse struct {
	ApiTokenResponse
	Token string `json:"token"`
}

// WebhookInput is the DTO for creating/updating a webhook
type WebhookInput struct {
	Name string `json:"name" validate:"required,min=1,max=200"`
	URL  string `json:"url" validate:"required,http_url"`
	// max matches the number of oneof tokens below: the array-length bound
	// (issue #415) exists because dive/oneof alone lets a client send
	// thousands of *duplicate* valid tokens, turning one webhook into a
	// thousands-wide fan-out target on every matching event. The list must
	// stay in sync with frontend/src/components/WebhooksSettings.tsx's
	// SUPPORTED_EVENTS (frontend trap 4). Operator events: reach_out_suggested
	// (#177), db.integrity_check_failed / db.restore_drill_failed (#273/#275),
	// alert.raised / alert.cleared (#428, condition in the payload).
	Events   []string `json:"events" validate:"required,min=1,max=16,dive,oneof=contact.created contact.updated contact.deleted note.created note.updated note.deleted activity.created activity.updated activity.deleted reminder.triggered birthday.occurred reach_out_suggested db.integrity_check_failed db.restore_drill_failed alert.raised alert.cleared"`
	IsActive bool     `json:"is_active"`
}

// WebhookResponse is the DTO returned for a webhook (no secret)
type WebhookResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	// DeliveryHealth is the always-visible INT-04 (#467) surface for a
	// webhook: a rollup of its most recent delivery so a receiver that has
	// permanently failed (revoked token, deleted endpoint) is visible next to
	// the webhook itself, not only inside the collapsed delivery log.
	DeliveryHealth WebhookDeliveryHealth `json:"delivery_health"`
}

// WebhookDeliveryHealth summarizes a webhook's latest delivery attempt.
// Zero-valued (all nulls/false) when the webhook has never been delivered to.
type WebhookDeliveryHealth struct {
	LastDeliveryAt    *time.Time `json:"last_delivery_at"`
	LastStatusCode    *int       `json:"last_status_code"`
	LastError         *string    `json:"last_error"`
	FailedPermanently bool       `json:"failed_permanently"`
	TerminalReason    string     `json:"terminal_reason"`
	// Retrying is true when the last delivery failed but still has a retry
	// scheduled (a transient failure working itself out).
	Retrying bool `json:"retrying"`
}

// WebhookCreateResponse is the DTO returned once after creation — includes the plaintext secret
type WebhookCreateResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	Secret    string    `json:"secret"`
}

// WebhookDeliveryResponse is the DTO returned for a webhook delivery record
type WebhookDeliveryResponse struct {
	ID                uint       `json:"id"`
	WebhookID         uint       `json:"webhook_id"`
	EventType         string     `json:"event_type"`
	StatusCode        *int       `json:"status_code"`
	Error             *string    `json:"error"`
	Attempts          int        `json:"attempts"`
	NextRetryAt       *time.Time `json:"next_retry_at"`
	CreatedAt         time.Time  `json:"created_at"`
	FailedPermanently bool       `json:"failed_permanently"`
	TerminalReason    string     `json:"terminal_reason"`
}

// LinkFieldTypeInput is the DTO for creating/updating a LinkFieldType
// (link_field_type.go, T34). Protocol is validated with `safeurl` so a
// `javascript:`/`data:`/etc. scheme can't be stored as the template — see
// LinkFieldType's own doc comment for why that alone isn't sufficient once
// {value} is substituted at render time. IsDefault is deliberately absent
// here: it is never client-settable (CreateLinkFieldType always forces
// false; edits never touch it).
type LinkFieldTypeInput struct {
	Name     string `json:"name" validate:"required,min=1,max=200"`
	Protocol string `json:"protocol" validate:"max=2000,safeurl"`
	Category string `json:"category" validate:"required,oneof=messaging social other"`
	Icon     string `json:"icon,omitempty" validate:"max=100"`
	// Position is optional on create (defaults to end-of-list server-side)
	// but always sent on update (full-replace, matching CadencePolicyInput's
	// precedent of treating every DTO field as replaceable).
	Position *int `json:"position,omitempty"`
}

// LinkFieldTypeReorderInput is the DTO for PUT /link-field-types/reorder —
// the full set of the user's LinkFieldType IDs in the desired display
// order. Every ID must belong to the caller; the handler rejects the
// request otherwise rather than silently reordering a subset.
type LinkFieldTypeReorderInput struct {
	Order []string `json:"order" validate:"required,min=1,dive,uuid4"`
}

// DeviceGrantInput is the DTO for creating a device grant (issue #722's fully
// biometric login). The label is a free-form device name shown in the
// management list so the user can recognise ("Pixel 8a", "work tablet").
type DeviceGrantInput struct {
	Label string `json:"label" validate:"omitempty,max=100"`
}

// DeviceGrantResponse is what the management list returns — never the token.
type DeviceGrantResponse struct {
	ID         uint       `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// DeviceGrantCreateResponse adds the one-time plaintext token, exactly like
// ApiTokenCreateResponse — shown once at enrollment, never returned again.
type DeviceGrantCreateResponse struct {
	DeviceGrantResponse
	Token string `json:"token"`
}

// DeviceGrantSessionInput is the public exchange request: possession of the
// plaintext grant (which only exists on the device that enrolled it, behind
// its biometric gate) is what mint a fresh session.
type DeviceGrantSessionInput struct {
	DeviceToken string `json:"device_token" validate:"required,min=32"`
}
