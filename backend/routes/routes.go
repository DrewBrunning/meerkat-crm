package routes

import (
	"mycorrhizal/caldav"
	"mycorrhizal/carddav"
	"mycorrhizal/config"
	"mycorrhizal/controllers"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterRoutes(router *gin.Engine, cfg *config.Config, db *gorm.DB, oidcProvider *services.OIDCProvider) {

	// Health surface (no versioning, standard practice; all unauthenticated,
	// see health_controller.go for the live/ready/deep split — issue #421).
	router.GET("/health", controllers.HealthCheck)
	router.GET("/health/live", controllers.LivenessCheck)
	router.GET("/health/ready", controllers.ReadinessCheck)

	// Prometheus metrics (issue #389). Opt-in and off by default: the route
	// only exists when METRICS_TOKEN is configured, and then every scrape must
	// carry `Authorization: Bearer <METRICS_TOKEN>`. Not user-scoped and not a
	// REST/JSON operation, so it is deliberately outside the /api/v1 group,
	// the six-persona authorization matrix, and openapi.yaml — same rationale
	// as the CardDAV surface.
	if cfg.MetricsToken != "" {
		router.GET("/metrics", controllers.MetricsHandler(cfg, db))
	}

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// OIDC routes (config always public; login/callback only when OIDC is enabled)
		v1.GET("/auth/oidc/config", controllers.OIDCConfigHandler(cfg))
		if cfg.OIDC.Enabled && oidcProvider != nil {
			v1.GET("/auth/oidc/login", middleware.AuthRateLimitMiddleware(), controllers.OIDCLoginHandler(oidcProvider, cfg))
			v1.GET("/auth/oidc/callback", middleware.AuthRateLimitMiddleware(), controllers.OIDCCallbackHandler(oidcProvider, cfg))
		}

		// Public routes (no authentication required, strict rate limiting).
		// Issue #692: every route that mints a session (register, login, the
		// 2FA step, the device-grant exchange) enforces the configured
		// MIN_CLIENT_VERSION floor BEFORE any user authentication work — a
		// client below the floor is refused outright (client_version.go). The
		// floor is empty by default, which makes this middleware inert.
		v1.POST("/register", middleware.AuthRateLimitMiddleware(), middleware.EnforceMinClientVersion(cfg), middleware.ValidateJSONMiddleware(&models.UserRegistrationInput{}), controllers.RegisterUser(cfg))
		v1.POST("/login", middleware.AuthRateLimitMiddleware(), middleware.EnforceMinClientVersion(cfg), func(c *gin.Context) {
			controllers.LoginUser(c, cfg)
		})
		// N8: step 2 of interactive login — exchange a pending 2FA challenge
		// (2fa_pending cookie set by /login) + a TOTP/recovery code for the
		// real session cookie.
		v1.POST("/login/2fa", middleware.AuthRateLimitMiddleware(), middleware.EnforceMinClientVersion(cfg), func(c *gin.Context) {
			controllers.Complete2FALogin(c, cfg)
		})
		// Issue #722: fully biometric login — a device that holds an
		// unrevoked device grant (and whose owner just passed the local
		// biometric gate) exchanges it for a fresh session JWT. Rate-limited
		// exactly like /login: possession of a grant is as powerful as a
		// password and must be defended the same way.
		v1.POST("/auth/device/session", middleware.AuthRateLimitMiddleware(), middleware.EnforceMinClientVersion(cfg), middleware.ValidateJSONMiddleware(&models.DeviceGrantSessionInput{}), func(c *gin.Context) {
			controllers.ExchangeDeviceGrant(c, cfg)
		})
		v1.POST("/logout", func(c *gin.Context) {
			controllers.LogoutUser(c, cfg, oidcProvider)
		})
		v1.POST("/check-password-strength", middleware.AuthRateLimitMiddleware(), controllers.CheckPasswordStrength)
		v1.POST("/password-reset/request", middleware.AuthRateLimitMiddleware(), middleware.ValidateJSONMiddleware(&models.PasswordResetRequestInput{}), func(c *gin.Context) {
			controllers.RequestPasswordReset(c, cfg)
		})
		v1.POST("/password-reset/confirm", middleware.AuthRateLimitMiddleware(), middleware.ValidateJSONMiddleware(&models.PasswordResetConfirmInput{}), func(c *gin.Context) {
			controllers.ConfirmPasswordReset(c, cfg)
		})

		// Protected routes (authentication required, general rate limiting)
		protected := v1.Group("/")
		protected.Use(middleware.APIRateLimitMiddleware())
		protected.Use(middleware.AuthMiddleware(cfg))
		// CON-04 (issue #459, ADR 0010): one idempotency mechanism for the
		// whole authenticated surface. Inert unless a request is a POST
		// carrying an Idempotency-Key header, so every mutation is covered by
		// default and a new keyed POST needs no wiring.
		protected.Use(middleware.IdempotencyMiddleware())
		{
			protected.POST("/users/change-password", middleware.ValidateJSONMiddleware(&models.ChangePasswordInput{}), func(c *gin.Context) {
				controllers.ChangePassword(c, cfg)
			})
			protected.PATCH("/users/language", controllers.UpdateLanguage)
			protected.PATCH("/users/date-format", controllers.UpdateDateFormat)
			protected.GET("/users/enabled-contact-fields", controllers.GetEnabledContactFields)
			protected.PATCH("/users/enabled-contact-fields", middleware.ValidateJSONMiddleware(&models.EnabledContactFieldsInput{}), controllers.UpdateEnabledContactFields)
			protected.GET("/users/me", controllers.GetCurrentUser)
			// T90: mark a contact as "Me". Single-purpose, follows the shape of
			// the other /users mutation routes above.
			protected.PATCH("/users/me/self-contact", middleware.ValidateJSONMiddleware(&models.SelfContactInput{}), controllers.UpdateSelfContact)
			// N8 2FA management (issue #158). Status is read-only; setup
			// mints a pending secret, confirm flips it on + mints recovery
			// codes, disable/regenerate gate on a live TOTP code.
			protected.GET("/users/2fa/status", controllers.GetTwoFactorStatus)
			protected.POST("/users/2fa/setup", controllers.SetupTwoFactor)
			protected.POST("/users/2fa/confirm", controllers.ConfirmTwoFactor)
			protected.POST("/users/2fa/disable", controllers.DisableTwoFactor)
			protected.POST("/users/2fa/recovery-codes/regenerate", controllers.RegenerateRecoveryCodes)
			// P1 contact sharing recipient picker — the only non-admin way to discover
			// other users on the instance; deliberately thinner than
			// admin-only ListUsers (id+username only).
			protected.GET("/users/directory", controllers.ListUserDirectory)

			// M3 dashboard composite: birthdays + random
			// contacts + upcoming reminders + overdue cadences in one call.
			protected.GET("/dashboard", controllers.GetDashboard)

			// Contact routes
			protected.GET("/contacts", controllers.GetContacts)
			protected.GET("/contacts/circles", controllers.GetCircles)
			protected.GET("/contacts/random", controllers.GetContactsRandom)
			protected.GET("/contacts/birthdays", controllers.GetUpcomingBirthdays)
			protected.POST("/contacts/merge/preview", middleware.ValidateJSONMiddleware(&models.ContactMergeRequest{}), controllers.PreviewContactMerge)
			protected.POST("/contacts/merge", middleware.ValidateJSONMiddleware(&models.ContactMergeRequest{}), controllers.CommitContactMerge)
			protected.POST("/contacts", middleware.ValidateJSONMiddleware(&models.ContactRecordInput{}), controllers.CreateContact)
			// N5 bulk operations — registered before /contacts/:id so the
			// literal path is never captured as a contact ID.
			protected.POST("/contacts/bulk", middleware.ValidateJSONMiddleware(&models.BulkContactOperationInput{}), controllers.BulkContactOperation)
			// T93 duplicate scan — registered
			// before /contacts/:id like the other literal paths above, so
			// "duplicates" is never captured as a contact ID.
			protected.GET("/contacts/duplicates", controllers.GetDuplicatePairs)
			protected.POST("/contacts/duplicates/dismiss", middleware.ValidateJSONMiddleware(&models.DuplicateDismissalInput{}), controllers.DismissDuplicatePair)
			// Contact-address suggestions — registered before /contacts/:id so
			// the literal /address-suggestions path is never captured as a
			// contact ID.
			protected.POST("/contacts/address-suggestions", controllers.SuggestContactAddresses)
			protected.POST("/contacts/address-suggestions/apply", middleware.ValidateJSONMiddleware(&models.ApplyContactAddressSuggestionInput{}), controllers.ApplyContactAddressSuggestion)
			protected.GET("/contacts/:id", controllers.GetContact)
			// N2 prep view: read-only aggregation of everything the user
			// wants to remember before seeing this contact. Registered after
			// GET /contacts/:id so the literal /briefing path is never
			// captured as a contact ID.
			protected.GET("/contacts/:id/briefing", controllers.GetContactBriefing)
			// M4 contact-detail composite (M4): everything the contact
			// detail screen renders, in one call. Same ordering note as
			// /briefing above — registered after GET /contacts/:id so the
			// literal /detail path is never captured as a contact ID.
			protected.GET("/contacts/:id/detail", controllers.GetContactDetail)
			// T66 contact timeline: paginated, type- and recency-filtered view
			// of the merged timeline across all six event types. Registered
			// after /contacts/:id so the literal /timeline path is never
			// captured as a contact ID (same ordering note as /briefing and
			// /detail).
			protected.GET("/contacts/:id/timeline", controllers.GetContactTimeline)
			protected.PUT("/contacts/:id", middleware.ValidateJSONMiddleware(&models.ContactRecordInput{}), controllers.UpdateContact)
			protected.DELETE("/contacts/:id", controllers.DeleteContact)
			protected.POST("/contacts/:id/archive", controllers.ArchiveContact)
			protected.POST("/contacts/:id/unarchive", controllers.UnarchiveContact)
			// Issue #173: favorite toggle, same shape as archive/unarchive —
			// a CRM-local flag toggled by its own endpoint, not through the
			// nested Card/CRM payload.
			protected.POST("/contacts/:id/favorite", controllers.FavoriteContact)
			protected.POST("/contacts/:id/unfavorite", controllers.UnfavoriteContact)

			// Contact import routes (CSV). The upload route carries its own
			// BodySizeLimitMiddleware(services.MaxCSVSize) — DefaultBodySizeLimitMiddleware
			// (main.go, engine-wide) exempts this exact path (see
			// middleware.largeBodyRoutePaths) precisely so this one can apply
			// instead. Issue #416: without this, the 10MB global default
			// silently won regardless of MaxCSVSize, and no CSV import could
			// ever exceed 10MB despite the handler's own 20MB check.
			protected.POST("/contacts/import/upload", middleware.BodySizeLimitMiddleware(services.MaxCSVSize), controllers.UploadCSVForImport)
			protected.POST("/contacts/import/preview", middleware.ValidateJSONMiddleware(&models.ImportPreviewRequest{}), controllers.PreviewImport)
			protected.POST("/contacts/import/confirm", middleware.ValidateJSONMiddleware(&models.ImportConfirmRequest{}), controllers.ConfirmImport)

			// Contact import routes (VCF). Same body-size-limit override as
			// the CSV route above, sized to services.MaxVCFSize instead.
			protected.POST("/contacts/import/vcf/upload", middleware.BodySizeLimitMiddleware(services.MaxVCFSize), func(c *gin.Context) {
				controllers.UploadVCFForImport(c, cfg)
			})
			protected.POST("/contacts/import/vcf/confirm", middleware.ValidateJSONMiddleware(&models.ImportConfirmRequest{}), func(c *gin.Context) {
				controllers.ConfirmVCFImport(c, cfg)
			})

			// Contact import routes (JSContact JSON) —  extension.
			// Confirmation deliberately reuses /contacts/import/vcf/confirm
			// (see UploadJSContactForImport's doc comment): the session it
			// creates is format-agnostic once parsed into []VCFContactData.
			// Same body-size-limit override as VCF (UploadJSContactForImport
			// checks against the same services.MaxVCFSize constant).
			protected.POST("/contacts/import/jscontact/upload", middleware.BodySizeLimitMiddleware(services.MaxVCFSize), controllers.UploadJSContactForImport)

			// Contact import routes (records) — T96's Android device-contacts
			// path: a batch of neutral Card/CRM records run through the same
			// preview pipeline (duplicate detection + merge diff + within-batch),
			// confirmed via the shared /contacts/import/vcf/confirm endpoint.
			protected.POST("/contacts/import/records", middleware.ValidateJSONMiddleware(&models.ImportRecordsRequest{}), controllers.UploadImportRecords)

			// Import run history (issue #651) — one immutable row per confirmed
			// import (any format), newest first, user-scoped. Backs the history
			// table on the Data settings page.
			protected.GET("/contacts/import/history", controllers.GetImportHistory)

			// Monica import assistant (issue #549) — connect to a live Monica
			// instance, pull a snapshot in the background with progress, review
			// (with the loss report) then confirm through the shared
			// source-import engine. The API token lives only in the in-memory
			// session; SSRF egress is guarded per MONICA_BLOCK_PRIVATE_URLS.
			protected.POST("/contacts/import/monica/connect", middleware.ValidateJSONMiddleware(&models.MonicaConnectRequest{}), func(c *gin.Context) {
				controllers.ConnectMonicaImport(c, cfg)
			})
			protected.POST("/contacts/import/monica/fetch", middleware.ValidateJSONMiddleware(&models.MonicaFetchRequest{}), controllers.StartMonicaFetch)
			protected.GET("/contacts/import/monica/status", controllers.GetMonicaImportStatus)
			protected.GET("/contacts/import/monica/preview", controllers.GetMonicaImportPreview)
			protected.POST("/contacts/import/monica/confirm", middleware.ValidateJSONMiddleware(&models.MonicaConfirmRequest{}), func(c *gin.Context) {
				controllers.ConfirmMonicaImport(c, cfg)
			})
			protected.POST("/contacts/import/monica/cancel", controllers.CancelMonicaImport)

			// Meerkat import assistant (issue #550) — upload a Meerkat CRM
			// SQLite file, pick a source user, review (with the loss report)
			// then confirm through the shared source-import engine. The
			// uploaded file is held only as a 0600 temp file for the session's
			// lifetime. The upload route carries its own body-size limit
			// (MaxMeerkatDBSize), same override pattern as the CSV/VCF uploads.
			protected.POST("/contacts/import/meerkat/upload", middleware.BodySizeLimitMiddleware(services.MaxMeerkatDBSize), controllers.UploadMeerkatDatabase)
			protected.POST("/contacts/import/meerkat/fetch", middleware.ValidateJSONMiddleware(&models.MeerkatFetchRequest{}), controllers.StartMeerkatFetch)
			protected.GET("/contacts/import/meerkat/status", controllers.GetMeerkatImportStatus)
			protected.GET("/contacts/import/meerkat/preview", controllers.GetMeerkatImportPreview)
			protected.POST("/contacts/import/meerkat/confirm", middleware.ValidateJSONMiddleware(&models.SourceImportConfirmRequest{}), controllers.ConfirmMeerkatImport)
			protected.POST("/contacts/import/meerkat/cancel", controllers.CancelMeerkatImport)

			// P1 contact sharing
			// — one-time filtered copy between two users on the same
			// instance. Accept is preview-only (parses the stored payload
			// through the same import pipeline above); Confirm delegates to
			// ConfirmVCF (the exact method /contacts/import/vcf/confirm
			// uses) and only then flips the share to accepted.
			protected.POST("/contact-shares", middleware.ValidateJSONMiddleware(&models.ContactShareInput{}), controllers.CreateContactShare)
			protected.GET("/contact-shares/incoming", controllers.ListIncomingContactShares)
			protected.GET("/contact-shares/outgoing", controllers.ListOutgoingContactShares)
			protected.POST("/contact-shares/:id/accept", controllers.AcceptContactShare)
			protected.POST("/contact-shares/:id/confirm", middleware.ValidateJSONMiddleware(&models.ImportConfirmRequest{}), func(c *gin.Context) {
				controllers.ConfirmContactShare(c, cfg)
			})
			protected.POST("/contact-shares/:id/decline", controllers.DeclineContactShare)

			// RelationshipEdge routes (graph-model relationship API; replaces the
			// legacy /contacts/:id/relationships stack, removed)
			protected.POST("/relationship-edges", middleware.ValidateJSONMiddleware(&models.RelationshipEdgeInput{}), controllers.CreateRelationshipEdge)
			protected.GET("/relationship-edges", controllers.ListRelationshipEdges)
			// T104 graph-inference trigger — registered before /relationship-edges/:id
			// so the literal /suggest path is never captured as an edge ID.
			protected.POST("/relationship-edges/suggest", controllers.SuggestRelationshipEdges)
			protected.GET("/relationship-edges/:id", controllers.GetRelationshipEdge)
			protected.PUT("/relationship-edges/:id", middleware.ValidateJSONMiddleware(&models.RelationshipEdgeInput{}), controllers.UpdateRelationshipEdge)
			protected.DELETE("/relationship-edges/:id", controllers.DeleteRelationshipEdge)
			protected.PATCH("/relationship-edges/:id/accept", controllers.AcceptRelationshipEdge)

			// Attachment routes (N7 — N7). Registered before /contacts/:id reads
			// where needed; download/delete are addressed by attachment ID.
			protected.POST("/contacts/:id/attachments", func(c *gin.Context) {
				controllers.UploadAttachment(c, cfg)
			})
			protected.GET("/contacts/:id/attachments", controllers.ListContactAttachments)
			protected.GET("/attachments/:id/download", func(c *gin.Context) {
				controllers.DownloadAttachment(c, cfg)
			})
			protected.DELETE("/attachments/:id", func(c *gin.Context) {
				controllers.DeleteAttachment(c, cfg)
			})

			// Profile picture routes
			protected.POST("/contacts/:id/profile_picture", func(c *gin.Context) {
				controllers.AddPhotoToContact(c, cfg)
			})
			protected.GET("/contacts/:id/profile_picture", func(c *gin.Context) {
				controllers.GetProfilePicture(c, cfg)
			})

			// Image proxy route (for fetching images from external URLs)
			protected.GET("/proxy/image", controllers.ProxyImage)

			// Note routes
			protected.GET("/contacts/:id/notes", controllers.GetNotesForContact)
			protected.POST("/contacts/:id/notes", middleware.ValidateJSONMiddleware(&models.NoteInput{}), controllers.CreateNote)
			protected.GET("/notes/:id", controllers.GetNote)
			protected.GET("/notes", controllers.GetUnassignedNotes)
			protected.POST("/notes", middleware.ValidateJSONMiddleware(&models.NoteInput{}), controllers.CreateUnassignedNote)
			protected.PUT("/notes/:id", middleware.ValidateJSONMiddleware(&models.NoteInput{}), controllers.UpdateNote)
			protected.DELETE("/notes/:id", controllers.DeleteNote)

			// Activity routes
			protected.GET("/contacts/:id/activities", controllers.GetActivitiesForContact)
			protected.POST("/activities", middleware.ValidateJSONMiddleware(&models.ActivityInput{}), controllers.CreateActivity)
			protected.GET("/activities", controllers.GetActivities)
			protected.GET("/activities/:id", controllers.GetActivity)
			protected.PUT("/activities/:id", middleware.ValidateJSONMiddleware(&models.ActivityInput{}), controllers.UpdateActivity)
			protected.DELETE("/activities/:id", controllers.DeleteActivity)

			// Circle routes
			protected.POST("/circles", middleware.ValidateJSONMiddleware(&models.CircleInput{}), controllers.CreateCircle)
			protected.GET("/circles", controllers.ListCircles)
			protected.GET("/circles/:id", controllers.GetCircle)
			protected.PUT("/circles/:id", middleware.ValidateJSONMiddleware(&models.CircleInput{}), controllers.UpdateCircle)
			protected.DELETE("/circles/:id", controllers.DeleteCircle)
			protected.POST("/circles/:id/members", middleware.ValidateJSONMiddleware(&models.CircleMemberInput{}), controllers.AddCircleMember)
			protected.DELETE("/circles/:id/members/:vcard_uid", controllers.RemoveCircleMember)

			// LinkFieldType routes (T34 — T34)
			protected.POST("/link-field-types", middleware.ValidateJSONMiddleware(&models.LinkFieldTypeInput{}), controllers.CreateLinkFieldType)
			protected.GET("/link-field-types", controllers.ListLinkFieldTypes)
			protected.GET("/link-field-types/:id", controllers.GetLinkFieldType)
			protected.PUT("/link-field-types/reorder", middleware.ValidateJSONMiddleware(&models.LinkFieldTypeReorderInput{}), controllers.ReorderLinkFieldTypes)
			protected.PUT("/link-field-types/:id", middleware.ValidateJSONMiddleware(&models.LinkFieldTypeInput{}), controllers.UpdateLinkFieldType)
			protected.DELETE("/link-field-types/:id", controllers.DeleteLinkFieldType)

			// Household routes (T1 — T1)
			protected.POST("/households", middleware.ValidateJSONMiddleware(&models.HouseholdInput{}), controllers.CreateHousehold)
			protected.GET("/households", controllers.ListHouseholds)
			protected.GET("/households/:id", controllers.GetHousehold)
			protected.PUT("/households/:id", middleware.ValidateJSONMiddleware(&models.HouseholdInput{}), controllers.UpdateHousehold)
			protected.DELETE("/households/:id", controllers.DeleteHousehold)
			protected.POST("/households/:id/members", middleware.ValidateJSONMiddleware(&models.HouseholdMemberInput{}), controllers.AddHouseholdMember)
			protected.DELETE("/households/:id/members/:vcard_uid", controllers.RemoveHouseholdMember)
			protected.PATCH("/households/:id/members/:vcard_uid", controllers.UpdateHouseholdMember)
			protected.POST("/households/:id/suggest-relationships", controllers.SuggestHouseholdRelationships)

			// T40 address-based household suggestions (T40). The
			// literal /suggestions paths are registered before any /:id
			// capture would matter (there is no GET /households/:id/... below
			// that could collide), and accept/dismiss re-validate the group
			// server-side from its member VCardUIDs.
			protected.POST("/households/suggest-addresses", controllers.SuggestAddressHouseholds)
			protected.POST("/households/suggestions/accept", middleware.ValidateJSONMiddleware(&models.AcceptHouseholdSuggestionInput{}), controllers.AcceptAddressHouseholdSuggestion)
			protected.POST("/households/suggestions/dismiss", middleware.ValidateJSONMiddleware(&models.DismissHouseholdSuggestionInput{}), controllers.DismissAddressHouseholdSuggestion)

			// Tag routes
			protected.POST("/tags", middleware.ValidateJSONMiddleware(&models.TagInput{}), controllers.CreateTag)
			protected.GET("/tags", controllers.ListTags)
			protected.GET("/tags/:id", controllers.GetTag)
			protected.PUT("/tags/:id", middleware.ValidateJSONMiddleware(&models.TagInput{}), controllers.UpdateTag)
			protected.DELETE("/tags/:id", controllers.DeleteTag)
			protected.POST("/tags/:id/contacts", middleware.ValidateJSONMiddleware(&models.ContactTagInput{}), controllers.AddContactTag)
			protected.DELETE("/tags/:id/contacts/:vcard_uid", controllers.RemoveContactTag)

			// Custom field definition routes (T6 — T6). FieldValue routes are nested under
			// the contact (GET/PUT /contacts/:id/field-values) per the
			// endpoint-shape decision documented in
			// field_definition_controller.go.
			protected.POST("/field-definitions", middleware.ValidateJSONMiddleware(&models.FieldDefinitionInput{}), controllers.CreateFieldDefinition)
			protected.GET("/field-definitions", controllers.ListFieldDefinitions)
			protected.GET("/field-definitions/:id", controllers.GetFieldDefinition)
			protected.PUT("/field-definitions/:id", middleware.ValidateJSONMiddleware(&models.FieldDefinitionInput{}), controllers.UpdateFieldDefinition)
			protected.DELETE("/field-definitions/:id", controllers.DeleteFieldDefinition)
			protected.GET("/contacts/:id/field-values", controllers.ListContactFieldValues)
			protected.PUT("/contacts/:id/field-values", middleware.ValidateJSONMiddleware(&models.ContactFieldValuesInput{}), controllers.ReplaceContactFieldValues)

			// LifeEvent routes
			protected.POST("/life-events", middleware.ValidateJSONMiddleware(&models.LifeEventInput{}), controllers.CreateLifeEvent)
			protected.GET("/life-events", controllers.ListLifeEvents)
			protected.GET("/life-events/:id", controllers.GetLifeEvent)
			protected.PUT("/life-events/:id", middleware.ValidateJSONMiddleware(&models.LifeEventInput{}), controllers.UpdateLifeEvent)
			protected.DELETE("/life-events/:id", controllers.DeleteLifeEvent)

			// ConversationAgenda routes (T21 — T21). /:id/discuss is registered
			// after /:id reads but the PATCH method never collides with
			// them.
			protected.POST("/conversation-agenda", middleware.ValidateJSONMiddleware(&models.ConversationAgendaInput{}), controllers.CreateConversationAgenda)
			protected.GET("/conversation-agenda", controllers.ListConversationAgenda)
			protected.GET("/conversation-agenda/:id", controllers.GetConversationAgenda)
			protected.PUT("/conversation-agenda/:id", middleware.ValidateJSONMiddleware(&models.ConversationAgendaInput{}), controllers.UpdateConversationAgenda)
			protected.PATCH("/conversation-agenda/:id/discuss", middleware.ValidateJSONMiddleware(&models.ConversationAgendaDiscussInput{}), controllers.DiscussConversationAgenda)
			protected.DELETE("/conversation-agenda/:id", controllers.DeleteConversationAgenda)

			// Gift routes (T20b — T20b)
			protected.POST("/gifts", middleware.ValidateJSONMiddleware(&models.GiftInput{}), controllers.CreateGift)
			protected.GET("/gifts", controllers.ListGifts)
			protected.GET("/gifts/:id", controllers.GetGift)
			protected.PUT("/gifts/:id", middleware.ValidateJSONMiddleware(&models.GiftInput{}), controllers.UpdateGift)
			protected.DELETE("/gifts/:id", controllers.DeleteGift)

			// Preference routes (T20a — T20a)
			protected.POST("/preferences", middleware.ValidateJSONMiddleware(&models.PreferenceInput{}), controllers.CreatePreference)
			protected.GET("/preferences", controllers.ListPreferences)
			protected.GET("/preferences/:id", controllers.GetPreference)
			protected.PUT("/preferences/:id", middleware.ValidateJSONMiddleware(&models.PreferenceInput{}), controllers.UpdatePreference)
			protected.DELETE("/preferences/:id", controllers.DeletePreference)

			// CadencePolicy routes (T19 — T19).
			// /overdue is registered before /:id so the literal path is never
			// captured as a policy ID.
			protected.GET("/cadence-policies/overdue", controllers.GetOverdueCadences)
			protected.POST("/cadence-policies", middleware.ValidateJSONMiddleware(&models.CadencePolicyInput{}), controllers.CreateCadencePolicy)
			protected.GET("/cadence-policies", controllers.ListCadencePolicies)
			protected.GET("/cadence-policies/:id", controllers.GetCadencePolicy)
			protected.PUT("/cadence-policies/:id", middleware.ValidateJSONMiddleware(&models.CadencePolicyInput{}), controllers.UpdateCadencePolicy)
			protected.DELETE("/cadence-policies/:id", controllers.DeleteCadencePolicy)

			// ReachOutSuggestion routes (issue #177 — event-driven reach-out triggers).
			protected.GET("/reach-out-suggestions", controllers.ListReachOutSuggestions)
			protected.POST("/reach-out-suggestions/:id/dismiss", controllers.DismissReachOutSuggestion)

			// Reminder routes
			protected.GET("/reminders", controllers.GetAllReminders)
			protected.GET("/reminders/upcoming", controllers.GetUpcomingReminders)
			protected.GET("/contacts/:id/reminders", controllers.GetRemindersForContact)
			protected.POST("/contacts/:id/reminders", middleware.ValidateJSONMiddleware(&models.Reminder{}), controllers.CreateReminder)
			protected.GET("/reminders/:id", controllers.GetReminder)
			protected.PUT("/reminders/:id", middleware.ValidateJSONMiddleware(&models.Reminder{}), controllers.UpdateReminder)
			protected.POST("/reminders/:id/complete", controllers.CompleteReminder)
			protected.DELETE("/reminders/:id", controllers.DeleteReminder)

			// Reminder completion routes (for timeline)
			protected.GET("/contacts/:id/reminder-completions", controllers.GetCompletionsForContact)
			protected.DELETE("/reminder-completions/:id", controllers.DeleteCompletion)

			// Export routes
			protected.GET("/export", controllers.ExportData)
			protected.GET("/export/vcf", func(c *gin.Context) {
				controllers.ExportContactsAsVCF(c, cfg.ProfilePhotoDir)
			})
			protected.GET("/export/jscontact", controllers.ExportContactsAsJSContact)
			// Issue #442 (DATA-02): compute what an export would lose without
			// producing the file — same shared computation as the export
			// handlers, loss-report-only response.
			protected.GET("/export/preflight", controllers.ExportPreflight)

			// Graph/Network visualization route
			protected.GET("/graph", controllers.GetGraph)
			// Graph traversal / multi-hop chains (T10) — GET /graph/connections
			// with ?from=<VCardUID>&depth=<n>&relation=<token|synonym>
			protected.GET("/graph/connections", controllers.GetGraphConnections)

			// Full-text search (T11) — GET /search?q=<term>&limit=<n> across
			// contacts, notes, and interactions
			protected.GET("/search", controllers.SearchAll)

			// API token routes
			protected.GET("/api-tokens", controllers.ListApiTokens)
			protected.POST("/api-tokens", middleware.ValidateJSONMiddleware(&models.ApiTokenInput{}), controllers.CreateApiToken)
			protected.POST("/api-tokens/revoke-all", controllers.RevokeAllApiTokens)
			protected.DELETE("/api-tokens/:id", controllers.RevokeApiToken)
			protected.POST("/api-tokens/:id/rotate", controllers.RotateApiToken)

			// Issue #866: active-session inventory. List this account's live
			// sessions, revoke one by id (revoking the current one == logout
			// this device), or revoke every other session ("log out
			// everywhere else"). ASVS 3.3.4.
			protected.GET("/sessions", controllers.ListSessions)
			protected.DELETE("/sessions", controllers.RevokeOtherSessions)
			protected.DELETE("/sessions/:id", controllers.RevokeSession)

			// Issue #722: device grants (the server half of fully biometric
			// login) — enrolled by the authenticated caller, listed so a user
			// can see what's signed in, and revoked individually or all at
			// once (the lost-phone path).
			protected.GET("/auth/device/grants", controllers.ListDeviceGrants)
			protected.POST("/auth/device/grants", middleware.ValidateJSONMiddleware(&models.DeviceGrantInput{}), controllers.CreateDeviceGrant)
			protected.POST("/auth/device/grants/revoke-all", controllers.RevokeAllDeviceGrants)
			protected.DELETE("/auth/device/grants/:id", controllers.RevokeDeviceGrant)

			// Webhook routes
			protected.GET("/webhooks", controllers.ListWebhooks)
			protected.POST("/webhooks", middleware.ValidateJSONMiddleware(&models.WebhookInput{}), controllers.CreateWebhook)
			protected.GET("/webhooks/:id", controllers.GetWebhook)
			protected.PUT("/webhooks/:id", middleware.ValidateJSONMiddleware(&models.WebhookInput{}), controllers.UpdateWebhook)
			protected.DELETE("/webhooks/:id", controllers.DeleteWebhook)
			protected.POST("/webhooks/:id/test", controllers.TestWebhook)
			protected.GET("/webhooks/:id/deliveries", controllers.GetWebhookDeliveries)

			// Audit trail routes (T18 — T18). Read-only log surface + update-only undo.
			protected.GET("/audit", controllers.ListAuditEvents)
			protected.POST("/audit/:id/undo", controllers.UndoAuditEvent)
			// Issue #416: unbounded CSV export of the caller's own audit
			// trail, alongside the /export/* routes above.
			protected.GET("/audit/export", controllers.ExportAuditLog)

			// Notification routes (N9 — N9). Per-user channel config, the
			// per-user channel toggles, per-channel test notification, and Web
			// Push device registrations.
			protected.GET("/notifications/config", controllers.GetNotificationConfig)
			protected.PUT("/notifications/config", middleware.ValidateJSONMiddleware(&models.NotificationConfigInput{}), controllers.SaveNotificationConfig)
			protected.POST("/notifications/config/test", controllers.TestNotificationChannel)
			protected.GET("/notifications/push-subscriptions", controllers.ListPushSubscriptions)
			protected.POST("/notifications/push-subscriptions", middleware.ValidateJSONMiddleware(&models.PushSubscriptionInput{}), controllers.CreatePushSubscription)
			protected.DELETE("/notifications/push-subscriptions/:id", controllers.DeletePushSubscription)

			// Mobile push device registrations (M2 — M2). Platform-agnostic: a device registers
			// a token + client (fcm today, apns accepted), the backend
			// dispatches delivery by client. The web app never enrolls
			// devices; it lists and deletes them in Settings.
			protected.GET("/notifications/devices", controllers.ListDeviceRegistrations)
			protected.POST("/notifications/devices", middleware.ValidateJSONMiddleware(&models.DeviceRegistrationInput{}), controllers.CreateDeviceRegistration)
			protected.DELETE("/notifications/devices/:id", controllers.DeleteDeviceRegistration)

			// Calendar subscription routes (CalDAV/iCS activity import)
			protected.GET("/calendars", controllers.ListCalendarSubscriptions)
			protected.POST("/calendars", middleware.ValidateJSONMiddleware(&models.CalendarSubscriptionInput{}), controllers.CreateCalendarSubscription)
			protected.PUT("/calendars/:id", middleware.ValidateJSONMiddleware(&models.CalendarSubscriptionInput{}), controllers.UpdateCalendarSubscription)
			protected.DELETE("/calendars/:id", controllers.DeleteCalendarSubscription)
			protected.POST("/calendars/:id/sync", controllers.SyncCalendarSubscription)

			// Contact subscription routes (CardDAV client: sync contacts in
			// from an external address book)
			protected.GET("/contact-subscriptions", controllers.ListContactSubscriptions)
			protected.POST("/contact-subscriptions", middleware.ValidateJSONMiddleware(&models.ContactSubscriptionInput{}), controllers.CreateContactSubscription)
			protected.PUT("/contact-subscriptions/:id", middleware.ValidateJSONMiddleware(&models.ContactSubscriptionInput{}), controllers.UpdateContactSubscription)
			protected.DELETE("/contact-subscriptions/:id", controllers.DeleteContactSubscription)
			protected.POST("/contact-subscriptions/:id/sync", controllers.SyncContactSubscription)

			// CardDAV sync conflict routes (issue #395): a sync overwrote a
			// local edit; the user can review the conflict, restore the local
			// value, or dismiss it.
			protected.GET("/contact-sync-conflicts", controllers.ListContactSyncConflicts)
			protected.POST("/contact-sync-conflicts/:id/restore", controllers.RestoreContactSyncConflict)
			protected.POST("/contact-sync-conflicts/:id/dismiss", controllers.DismissContactSyncConflict)

			// ExternalIdentity routes (T14 — T14): the generic integration
			// substrate's link/enrichment CRUD. System-agnostic — no
			// integration-specific route lives here.
			protected.POST("/external-identities", middleware.ValidateJSONMiddleware(&models.ExternalIdentityInput{}), controllers.CreateExternalIdentity)
			protected.GET("/external-identities", controllers.ListExternalIdentities)
			protected.GET("/external-identities/:id", controllers.GetExternalIdentity)
			protected.PUT("/external-identities/:id", middleware.ValidateJSONMiddleware(&models.ExternalIdentityInput{}), controllers.UpdateExternalIdentity)
			protected.DELETE("/external-identities/:id", controllers.DeleteExternalIdentity)

			// ExternalActivity routes (T14): generic enrichment events,
			// linkable into a contact's timeline via ?contact_id=.
			protected.POST("/external-activities", middleware.ValidateJSONMiddleware(&models.ExternalActivityInput{}), controllers.CreateExternalActivity)
			protected.GET("/external-activities", controllers.ListExternalActivities)
			protected.GET("/external-activities/:id", controllers.GetExternalActivity)
			protected.PUT("/external-activities/:id", middleware.ValidateJSONMiddleware(&models.ExternalActivityInput{}), controllers.UpdateExternalActivity)
			protected.DELETE("/external-activities/:id", controllers.DeleteExternalActivity)

			// Immich routes (T15/T16 — T15/T16): the first concrete integration on the
			// generic substrate. Config is per-user-global; links are written
			// as ExternalIdentity (system: "immich"), enrichment as
			// ExternalActivity. /contacts/:vcard_uid/thumbnail is the hardened
			// person-thumbnail proxy (same SVG rejection + Content-Disposition
			// as /proxy/image).
			protected.GET("/immich/config", controllers.GetImmichConfig)
			protected.PUT("/immich/config", middleware.ValidateJSONMiddleware(&models.ImmichConfigInput{}), controllers.SaveImmichConfig)
			protected.DELETE("/immich/config", controllers.DeleteImmichConfig)
			protected.POST("/immich/test-connection", controllers.TestImmichConnection)
			protected.GET("/immich/people", controllers.ListImmichPeople)
			protected.POST("/immich/sync", controllers.SyncImmichNow)
			protected.POST("/immich/contacts/:vcard_uid/link", controllers.LinkImmichContact)
			protected.DELETE("/immich/contacts/:vcard_uid/link", controllers.UnlinkImmichContact)
			protected.GET("/immich/contacts/:vcard_uid/summary", controllers.GetImmichContactSummary)
			protected.GET("/immich/contacts/:vcard_uid/thumbnail", controllers.GetImmichThumbnail)
			protected.GET("/immich/contacts/:vcard_uid/assets", controllers.ListImmichContactAssets)
			protected.GET("/immich/contacts/:vcard_uid/assets/:asset_id/image", controllers.GetImmichAssetImage)

			// Paperless-ngx routes (issue #155): link contacts to documents in a
			// self-hosted Paperless-ngx instance. Config is per-user-global;
			// links are written as ExternalIdentity (system: "paperless"). L1 is
			// read-only linking — the document stays Paperless-owned.
			protected.GET("/paperless/config", controllers.GetPaperlessConfig)
			protected.PUT("/paperless/config", middleware.ValidateJSONMiddleware(&models.PaperlessConfigInput{}), controllers.SavePaperlessConfig)
			protected.DELETE("/paperless/config", controllers.DeletePaperlessConfig)
			protected.POST("/paperless/test-connection", controllers.TestPaperlessConnection)
			protected.GET("/paperless/documents", controllers.ListPaperlessDocuments)
			protected.POST("/paperless/contacts/:vcard_uid/link", controllers.LinkPaperlessContact)
			protected.DELETE("/paperless/contacts/:vcard_uid/links/:identity_id", controllers.UnlinkPaperlessContact)

			// Seafile routes (issue #156): link contacts to files/folders in a
			// self-hosted Seafile library. Config is per-user-global; links are
			// written as ExternalIdentity (system: "seafile"). L1 is read-only
			// linking — files/folders stay Seafile-owned.
			protected.GET("/seafile/config", controllers.GetSeafileConfig)
			protected.PUT("/seafile/config", middleware.ValidateJSONMiddleware(&models.SeafileConfigInput{}), controllers.SaveSeafileConfig)
			protected.DELETE("/seafile/config", controllers.DeleteSeafileConfig)
			protected.POST("/seafile/test-connection", controllers.TestSeafileConnection)
			protected.GET("/seafile/libraries", controllers.ListSeafileLibraries)
			protected.GET("/seafile/libraries/:repo_id/dir", controllers.ListSeafileDir)
			protected.POST("/seafile/contacts/:vcard_uid/link", controllers.LinkSeafileContact)
			protected.DELETE("/seafile/contacts/:vcard_uid/links/:identity_id", controllers.UnlinkSeafileContact)

			// Nextcloud / ownCloud (WebDAV) routes (issue #157): link contacts
			// to files/folders over standard WebDAV. Config is per-user-global;
			// links are written as ExternalIdentity (system: "nextcloud"). Only
			// an app password is accepted — never the account password. L1 is
			// read-only linking — files/folders stay on the instance.
			protected.GET("/nextcloud/config", controllers.GetWebDAVConfig)
			protected.PUT("/nextcloud/config", middleware.ValidateJSONMiddleware(&models.WebDAVConfigInput{}), controllers.SaveWebDAVConfig)
			protected.DELETE("/nextcloud/config", controllers.DeleteWebDAVConfig)
			protected.POST("/nextcloud/test-connection", controllers.TestWebDAVConnection)
			protected.GET("/nextcloud/dir", controllers.ListWebDAVDir)
			protected.POST("/nextcloud/contacts/:vcard_uid/link", controllers.LinkWebDAVContact)
			protected.DELETE("/nextcloud/contacts/:vcard_uid/links/:identity_id", controllers.UnlinkWebDAVContact)
		}

		// Admin routes (admin authentication required)
		admin := v1.Group("/admin")
		admin.Use(middleware.APIRateLimitMiddleware())
		admin.Use(middleware.AuthMiddleware(cfg))
		admin.Use(middleware.AdminMiddleware())
		{
			admin.GET("/users", controllers.ListUsers)
			admin.POST("/users", middleware.ValidateJSONMiddleware(&models.AdminUserCreateInput{}), controllers.CreateUser)
			admin.GET("/users/:id", controllers.GetUser)
			admin.PATCH("/users/:id", middleware.ValidateJSONMiddleware(&models.AdminUserUpdateInput{}), controllers.UpdateUser)
			admin.DELETE("/users/:id", controllers.DeleteUser)
			admin.POST("/users/:id/reset-2fa", controllers.ResetUserTwoFactor)
			admin.POST("/trigger-reminders", func(c *gin.Context) {
				controllers.TriggerReminders(c, *cfg)
			})
			admin.POST("/trigger-purge", func(c *gin.Context) {
				controllers.TriggerPurge(c, *cfg)
			})
			// Rebuild the FTS search index (T11) — needed after bulk data
			// changes that bypassed the FTS triggers
			admin.POST("/search/rebuild", controllers.RebuildSearchIndexHandler)

			// Rebuild the denormalized contact columns (issue #497) — the flat
			// contacts.* projection, sort_name, addresses_flat,
			// phones_normalized — from the nested Card. Same trigger-bypass
			// situations as the search rebuild; run both after a restore.
			admin.POST("/contacts/rebuild-derived", controllers.RebuildDerivedColumnsHandler)

			// Operational-event timeline (issue #424) — instance-wide
			// diagnostics, admin-only.
			admin.GET("/system-events", controllers.ListSystemEvents)

			// Per-subsystem last-known-good state (issue #427), derived on
			// read from system_events — instance-wide diagnostics, admin-only.
			admin.GET("/subsystem-health", controllers.GetSubsystemHealth)

			// Background-job run history + folded per-job health (issue #391),
			// derived on read from job_runs — instance-wide diagnostics,
			// admin-only.
			admin.GET("/job-runs", controllers.ListJobRuns)
			admin.GET("/job-runs/health", controllers.GetJobRunHealth)

			// Operational errors bucketed by cause over a rolling window
			// (issue #426), derived on read from system_events — instance-wide
			// diagnostics, admin-only.
			admin.GET("/error-aggregation", controllers.GetErrorAggregation)

			// Per-channel notification delivery health (issue #422) — derived
			// on read from notification_deliveries + the per-user channel
			// config, with the configured/failing/no-devices/unconfigured
			// distinction the issue calls out. Instance-wide, admin-only.
			admin.GET("/notification-health", controllers.GetNotificationChannelHealth)

			// One-pass instance diagnostics (issue #423) — the admin-gated
			// "is this install healthy?" sweep: config, database + migration
			// state, filesystem writability, backup validity, notification
			// channels, integration reachability, disk usage, background-job
			// liveness, and version, folded into an ok/warning/error checklist
			// with a summary. Read-only and secret-free.
			admin.GET("/diagnostics", controllers.RunDiagnostics)

			// On-demand data-integrity sweep (DB-01, issue #460) — the two
			// SQLite structural pragmas plus the per-invariant data checks
			// (ADR 0012: relationships pointing at deleted contacts, orphaned
			// join rows, dangling external references, malformed canonical
			// records, derived-index divergence), folded into one report.
			// Read-only, secret-free, detection-only (repair is the `doctor`
			// CLI). Instance-wide, admin-only.
			admin.GET("/integrity-check", controllers.RunIntegrityCheck)

			// Aggregated build/version, migration numbers, live
			// config-validation read-back, enabled feature flags, SQLite
			// operational facts and storage sizing (issue #388) — the
			// authenticated counterpart to the unauthenticated /health
			// surface. Instance-wide, admin-only, read-only.
			admin.GET("/system-status", controllers.GetSystemStatus)
		}
	}

	// CardDAV routes (optional, enabled via CARDDAV_ENABLED)
	if cfg.CardDAVEnabled {
		registerCardDAVRoutes(router, cfg, db)
	}

	// CalDAV routes (optional, enabled via CALDAV_ENABLED) — serve the CRM's
	// own Activities/LifeEvents out as an iCalendar collection (T12b).
	if cfg.CalDAVEnabled {
		registerCalDAVRoutes(router, db)
	}
}

// registerCalDAVRoutes sets up CalDAV endpoints for Interaction/LifeEvent
// sync (T12b, T12b). Authentication
// reuses the CardDAV BasicAuth path (password or a DAV-scoped API token), so
// both DAV surfaces share one credential story. Read-only: clients subscribe,
// they never write through this endpoint.
func registerCalDAVRoutes(router *gin.Engine, db *gorm.DB) {
	router.GET("/.well-known/caldav", caldav.WellKnownRedirect)

	handler := caldav.NewHandler(db)

	calDAVGroup := router.Group("/caldav")
	calDAVGroup.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})
	calDAVGroup.Use(middleware.CardDAVRateLimitMiddleware())
	calDAVGroup.Use(carddav.BasicAuthMiddleware())
	{
		ginHandler := handler.GinHandler()
		calDAVGroup.Any("/*path", ginHandler)
		calDAVGroup.Handle("PROPFIND", "/*path", ginHandler)
		calDAVGroup.Handle("REPORT", "/*path", ginHandler)
	}
}

// registerCardDAVRoutes sets up CardDAV endpoints for contact synchronization
func registerCardDAVRoutes(router *gin.Engine, cfg *config.Config, db *gorm.DB) {
	// Well-known discovery endpoint (no auth required for discovery)
	router.GET("/.well-known/carddav", carddav.WellKnownRedirect)

	handler := carddav.NewHandler(db, cfg.ProfilePhotoDir)

	cardDAVGroup := router.Group("/carddav")
	cardDAVGroup.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})
	cardDAVGroup.Use(middleware.CardDAVRateLimitMiddleware())
	cardDAVGroup.Use(carddav.BasicAuthMiddleware())
	{
		ginHandler := handler.GinHandler()
		cardDAVGroup.Any("/*path", ginHandler)
		// WebDAV methods required for CardDAV
		cardDAVGroup.Handle("PROPFIND", "/*path", ginHandler)
		cardDAVGroup.Handle("REPORT", "/*path", ginHandler)
		cardDAVGroup.Handle("MKCOL", "/*path", ginHandler)
		cardDAVGroup.Handle("COPY", "/*path", ginHandler)
		cardDAVGroup.Handle("MOVE", "/*path", ginHandler)
	}
}
