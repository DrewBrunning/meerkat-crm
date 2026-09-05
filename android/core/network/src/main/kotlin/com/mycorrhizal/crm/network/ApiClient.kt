package com.mycorrhizal.crm.network

import com.mycorrhizal.crm.model.network.AcceptHouseholdSuggestionInput
import com.mycorrhizal.crm.model.network.AcceptHouseholdSuggestionResponse
import com.mycorrhizal.crm.model.network.AddressSuggestionsResponse
import com.mycorrhizal.crm.model.network.ApplyContactAddressSuggestionInput
import com.mycorrhizal.crm.model.network.ContactAddressSuggestion
import com.mycorrhizal.crm.model.network.ContactAddressSuggestionsResponse
import com.mycorrhizal.crm.model.network.DismissHouseholdSuggestionInput
import com.mycorrhizal.crm.model.network.RelationshipSuggestionsResponse
import com.mycorrhizal.crm.model.network.SuggestRelationshipsResponse
import com.mycorrhizal.crm.model.network.ActivitiesPage
import com.mycorrhizal.crm.model.network.Activity
import com.mycorrhizal.crm.model.network.ActivityInput
import com.mycorrhizal.crm.model.network.AdminUser
import com.mycorrhizal.crm.model.network.AdminUserCreateInput
import com.mycorrhizal.crm.model.network.AdminUserUpdateInput
import com.mycorrhizal.crm.model.network.AdminUsersListResponse
import com.mycorrhizal.crm.model.network.AddCircleMemberResponse
import com.mycorrhizal.crm.model.network.AddContactTagResponse
import com.mycorrhizal.crm.model.network.AddHouseholdMemberResponse
import com.mycorrhizal.crm.model.network.AuditEventsResponse
import com.mycorrhizal.crm.model.network.AuthConfig
import com.mycorrhizal.crm.model.network.AuditUndoResponse
import com.mycorrhizal.crm.model.network.ErrorAggregationResponse
import com.mycorrhizal.crm.model.network.JobRunHealthResponse
import com.mycorrhizal.crm.model.network.JobRunsResponse
import com.mycorrhizal.crm.model.network.SubsystemHealthResponse
import com.mycorrhizal.crm.model.network.SystemEventsResponse
import com.mycorrhizal.crm.model.network.BackendError
import com.mycorrhizal.crm.model.network.BirthdaysResponse
import com.mycorrhizal.crm.model.network.CadencePoliciesResponse
import com.mycorrhizal.crm.model.network.CadencePolicy
import com.mycorrhizal.crm.model.network.CadencePolicyInput
import com.mycorrhizal.crm.model.network.ChangePasswordRequest
import com.mycorrhizal.crm.model.network.CheckPasswordStrengthRequest
import com.mycorrhizal.crm.model.network.ContactBriefing
import com.mycorrhizal.crm.model.network.CreateCadencePolicyResponse
import com.mycorrhizal.crm.model.network.OverdueCadencesResponse
import com.mycorrhizal.crm.model.network.Circle
import com.mycorrhizal.crm.model.network.CircleDetailResponse
import com.mycorrhizal.crm.model.network.CircleInput
import com.mycorrhizal.crm.model.network.CircleMember
import com.mycorrhizal.crm.model.network.CircleMemberInput
import com.mycorrhizal.crm.model.network.CirclesPage
import com.mycorrhizal.crm.model.network.ContactActivitiesResponse
import com.mycorrhizal.crm.model.network.ContactFieldValuesInput
import com.mycorrhizal.crm.model.network.ContactFieldValuesResponse
import com.mycorrhizal.crm.model.network.ContactNotesResponse
import com.mycorrhizal.crm.model.network.ContactRecordInput
import com.mycorrhizal.crm.model.network.ContactRecordResponse
import com.mycorrhizal.crm.model.network.ContactRemindersResponse
import com.mycorrhizal.crm.model.network.ContactTag
import com.mycorrhizal.crm.model.network.ContactTagInput
import com.mycorrhizal.crm.model.network.ContactsPage
import com.mycorrhizal.crm.model.network.CreateActivityResponse
import com.mycorrhizal.crm.model.network.CreateCircleResponse
import com.mycorrhizal.crm.model.network.CreateContactResponse
import com.mycorrhizal.crm.model.network.CreateConversationAgendaResponse
import com.mycorrhizal.crm.model.network.FieldDefinitionsResponse
import com.mycorrhizal.crm.model.network.CreateGiftResponse
import com.mycorrhizal.crm.model.network.CreateHouseholdResponse
import com.mycorrhizal.crm.model.network.CreateLifeEventResponse
import com.mycorrhizal.crm.model.network.CreateNoteResponse
import com.mycorrhizal.crm.model.network.CreatePreferenceResponse
import com.mycorrhizal.crm.model.network.CreateRelationshipEdgeResponse
import com.mycorrhizal.crm.model.network.CompletionsResponse
import com.mycorrhizal.crm.model.network.CreateReminderResponse
import com.mycorrhizal.crm.model.network.CreateTagResponse
import com.mycorrhizal.crm.model.network.ConversationAgenda
import com.mycorrhizal.crm.model.network.ConversationAgendaInput
import com.mycorrhizal.crm.model.network.ConversationAgendaPage
import com.mycorrhizal.crm.model.network.DiscussConversationAgendaInput
import com.mycorrhizal.crm.model.network.BulkContactOperationInput
import com.mycorrhizal.crm.model.network.BulkOperationResult
import com.mycorrhizal.crm.model.network.ContactMergeCommitResponse
import com.mycorrhizal.crm.model.network.ContactMergePreviewResponse
import com.mycorrhizal.crm.model.network.ContactMergeRequest
import com.mycorrhizal.crm.model.network.DashboardResponse
import com.mycorrhizal.crm.model.network.DeviceRegistration
import com.mycorrhizal.crm.model.network.DeviceRegistrationInput
import com.mycorrhizal.crm.model.network.DeviceRegistrationsResponse
import com.mycorrhizal.crm.model.network.ExternalIdentitiesPage
import com.mycorrhizal.crm.model.network.ImmichAssetsResponse
import com.mycorrhizal.crm.model.network.ImmichAssetSummary
import com.mycorrhizal.crm.model.network.ImmichConfigInput
import com.mycorrhizal.crm.model.network.ImmichConfigResponse
import com.mycorrhizal.crm.model.network.ImmichConnectionTestResult
import com.mycorrhizal.crm.model.network.ImmichLinkRequest
import com.mycorrhizal.crm.model.network.ImmichPeopleResponse
import com.mycorrhizal.crm.model.network.ImmichPerson
import com.mycorrhizal.crm.model.network.ImmichPersonSummary
import com.mycorrhizal.crm.model.network.ImmichSummaryResponse
import com.mycorrhizal.crm.model.network.NextcloudConfigInput
import com.mycorrhizal.crm.model.network.NextcloudConfigResponse
import com.mycorrhizal.crm.model.network.NextcloudConnectionTestResult
import com.mycorrhizal.crm.model.network.NextcloudItemsResponse
import com.mycorrhizal.crm.model.network.NextcloudLinkRequest
import com.mycorrhizal.crm.model.network.PaperlessConfigInput
import com.mycorrhizal.crm.model.network.PaperlessConfigResponse
import com.mycorrhizal.crm.model.network.PaperlessConnectionTestResult
import com.mycorrhizal.crm.model.network.PaperlessDocument
import com.mycorrhizal.crm.model.network.PaperlessDocumentsResponse
import com.mycorrhizal.crm.model.network.PaperlessLinkRequest
import com.mycorrhizal.crm.model.network.SeafileConfigInput
import com.mycorrhizal.crm.model.network.SeafileConfigResponse
import com.mycorrhizal.crm.model.network.SeafileConnectionTestResult
import com.mycorrhizal.crm.model.network.SeafileItem
import com.mycorrhizal.crm.model.network.SeafileItemsResponse
import com.mycorrhizal.crm.model.network.SeafileLibrariesResponse
import com.mycorrhizal.crm.model.network.SeafileLibrary
import com.mycorrhizal.crm.model.network.SeafileLinkRequest
import com.mycorrhizal.crm.model.network.WebDAVItem
import com.mycorrhizal.crm.model.network.Gift
import com.mycorrhizal.crm.model.network.GiftInput
import com.mycorrhizal.crm.model.network.GiftsPage
import com.mycorrhizal.crm.model.network.GraphConnectionsResponse
import com.mycorrhizal.crm.model.network.Household
import com.mycorrhizal.crm.model.network.HouseholdDetailResponse
import com.mycorrhizal.crm.model.network.HouseholdInput
import com.mycorrhizal.crm.model.network.HouseholdMember
import com.mycorrhizal.crm.model.network.HouseholdMemberInput
import com.mycorrhizal.crm.model.network.HouseholdsPage
import com.mycorrhizal.crm.model.network.ImportConfirmRequest
import com.mycorrhizal.crm.model.network.ImportPreviewResponse
import com.mycorrhizal.crm.model.network.ImportRecordsRequest
import com.mycorrhizal.crm.model.network.ImportResult
import com.mycorrhizal.crm.model.network.ImportUploadResponse
import com.mycorrhizal.crm.model.network.LifeEvent
import com.mycorrhizal.crm.model.network.LifeEventInput
import com.mycorrhizal.crm.model.network.LifeEventsPage
import com.mycorrhizal.crm.model.network.LoginRequest
import com.mycorrhizal.crm.model.network.LoginResponse
import com.mycorrhizal.crm.model.network.MessageResponse
import com.mycorrhizal.crm.model.network.Note
import com.mycorrhizal.crm.model.network.NoteInput
import com.mycorrhizal.crm.model.network.NotesPage
import com.mycorrhizal.crm.model.network.NotificationConfig
import com.mycorrhizal.crm.model.network.NotificationConfigInput
import com.mycorrhizal.crm.model.network.NotificationTestChannelRequest
import com.mycorrhizal.crm.model.network.NotificationTestResult
import com.mycorrhizal.crm.model.network.PasswordResetConfirmRequest
import com.mycorrhizal.crm.model.network.PasswordResetRequest
import com.mycorrhizal.crm.model.network.PasswordStrength
import com.mycorrhizal.crm.model.network.Preference
import com.mycorrhizal.crm.model.network.PreferenceInput
import com.mycorrhizal.crm.model.network.PreferencesPage
import com.mycorrhizal.crm.model.network.RegisterRequest
import com.mycorrhizal.crm.model.network.RelationshipEdge
import com.mycorrhizal.crm.model.network.RelationshipEdgeInput
import com.mycorrhizal.crm.model.network.RelationshipEdgesPage
import com.mycorrhizal.crm.model.network.Reminder
import com.mycorrhizal.crm.model.network.ReminderCompleteResponse
import com.mycorrhizal.crm.model.network.SearchResult
import com.mycorrhizal.crm.model.network.Tag
import com.mycorrhizal.crm.model.network.TagDetailResponse
import com.mycorrhizal.crm.model.network.TagInput
import com.mycorrhizal.crm.model.network.TagsPage
import com.mycorrhizal.crm.model.network.TwoFactorCodeInput
import com.mycorrhizal.crm.model.network.TwoFactorConfirmResponse
import com.mycorrhizal.crm.model.network.TwoFactorSetupResponse
import com.mycorrhizal.crm.model.network.TwoFactorStatusResponse
import com.mycorrhizal.crm.model.network.UserProfile
import com.mycorrhizal.crm.model.network.ContactShare
import com.mycorrhizal.crm.model.network.ContactShareInput
import com.mycorrhizal.crm.model.network.ContactSharesPage
import com.mycorrhizal.crm.model.network.CreateContactShareResponse
import com.mycorrhizal.crm.model.network.UserDirectoryEntry
import com.mycorrhizal.crm.model.network.UserDirectoryResponse
import com.mycorrhizal.crm.model.network.ApiToken
import com.mycorrhizal.crm.model.network.ApiTokenCreateResponse
import com.mycorrhizal.crm.model.network.ApiTokenInput
import com.mycorrhizal.crm.model.network.ApiTokensResponse
import com.mycorrhizal.crm.model.network.RevokeAllApiTokensResponse
import com.mycorrhizal.crm.model.network.UpdateDateFormatRequest
import com.mycorrhizal.crm.model.network.UpdateLanguageRequest
import com.mycorrhizal.crm.model.network.Webhook
import com.mycorrhizal.crm.model.network.WebhookCreateResponse
import com.mycorrhizal.crm.model.network.WebhookDelivery
import com.mycorrhizal.crm.model.network.WebhookDeliveriesResponse
import com.mycorrhizal.crm.model.network.WebhookInput
import com.mycorrhizal.crm.model.network.WebhooksResponse
import com.mycorrhizal.crm.model.network.WebhookTestResponse
import com.squareup.moshi.Moshi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException

/**
 * Hand-written typed client for the Phase-1 backend surface (login, current
 * user, contacts list/detail). Request URLs use a placeholder origin; the
 * [BaseUrlInterceptor] rewrites them onto the user's configured server.
 *
 * Generated openapi-generator output is the long-term replacement once the
 * spec's allOf/oneOf schemas are flattened (ticket §1.3); this client is
 * deliberately small and endpoint-focused so the swap stays local to
 * :core:network.
 */
class ApiClient(
    private val okHttpClient: OkHttpClient,
    private val moshi: Moshi,
) {
    private val jsonMediaType = "application/json".toMediaType()

    /**
     * POST /api/v1/login. For a 2FA account the response carries
     * `two_factor_required: true` and sets a short-lived `2fa_pending` cookie
     * but NO session — [LoginResult.twoFactorRequired] is set and
     * [LoginResult.pending2faCookie] holds the captured challenge value so a
     * later [complete2faLogin] can exchange it. For a non-2FA account the
     * `auth_token` JWT is captured from the Set-Cookie header as before.
     */
    suspend fun login(identifier: String, password: String): Result<LoginResult> =
        executePost(LOGIN_PATH, LoginRequest(identifier = identifier, password = password)) { response, body ->
            val loginResponse = moshi.adapter(LoginResponse::class.java).fromJson(body)
            val twoFactorRequired = loginResponse?.twoFactorRequired == true
            val setCookies = response.headers("Set-Cookie")
            LoginResult(
                token = if (twoFactorRequired) null else extractCookie(setCookies, AUTH_COOKIE),
                language = loginResponse?.language,
                dateFormat = loginResponse?.dateFormat,
                twoFactorRequired = twoFactorRequired,
                pending2faCookie = if (twoFactorRequired) extractCookie(setCookies, TWO_FACTOR_COOKIE) else null,
            )
        }

    /**
     * POST /api/v1/login/2fa — step 2 of interactive login for a 2FA account.
     * The server requires the short-lived `2fa_pending` challenge cookie from
     * the preceding [login]; the OkHttp stack has no cookie jar, so the value
     * captured by [login] is forwarded as a Cookie header. On success the real
     * `auth_token` cookie is captured exactly like [login].
     *
     * Errors stay distinguishable by status: 400 invalid code, 401 missing/
     * expired/consumed challenge, 429 account lockout — each surfaces as
     * [ApiError.Client] with that code (a 429 body's flat `message` is parsed
     * so it can be shown verbatim, mirroring the web's 429 branch).
     */
    suspend fun complete2faLogin(code: String, pending2faCookie: String): Result<LoginResult> {
        val request = Request.Builder()
            .url("$PLACEHOLDER_ORIGIN$LOGIN_2FA_PATH".toHttpUrl())
            .addHeader("Cookie", "$TWO_FACTOR_COOKIE=$pending2faCookie")
            .post(TwoFactorCodeInput(code).toJsonBody())
            .build()
        return execute(request) { response, body ->
            val loginResponse = moshi.adapter(LoginResponse::class.java).fromJson(body)
            val token = extractCookie(response.headers("Set-Cookie"), AUTH_COOKIE)
            LoginResult(
                token = token,
                language = loginResponse?.language,
                dateFormat = loginResponse?.dateFormat,
            )
        }
    }

    // --- N8 2FA management (issue #158, web parity #814). Authenticated —
    // the bearer session rides the Authorization header like every other
    // `/users/*` call. confirm/disable bump token_version server-side, which
    // re-issues the session as a fresh `auth_token` cookie: [ReissuedTokenResult]
    // carries that cookie so the data layer can refresh the stored JWT and the
    // current session survives the mutation.

    /** GET /api/v1/users/2fa/status — `{ enabled }`. */
    suspend fun getTwoFactorStatus(): Result<TwoFactorStatusResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$TWO_FACTOR_PATH/status") { _, body ->
            moshi.adapter(TwoFactorStatusResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/users/2fa/setup — mints a pending TOTP secret. 409 if already enabled; 403 for OIDC accounts. */
    suspend fun setupTwoFactor(): Result<TwoFactorSetupResponse> =
        executePostEmpty("$TWO_FACTOR_PATH/setup") { _, body ->
            moshi.adapter(TwoFactorSetupResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/users/2fa/confirm — enables 2FA and mints the one-time recovery codes; re-issues the session cookie. */
    suspend fun confirmTwoFactor(code: String): Result<ReissuedTokenResult<TwoFactorConfirmResponse>> =
        executePost("$TWO_FACTOR_PATH/confirm", TwoFactorCodeInput(code)) { response, body ->
            val parsed = moshi.adapter(TwoFactorConfirmResponse::class.java).fromJson(body)
            if (parsed == null) {
                null
            } else {
                ReissuedTokenResult(parsed, extractCookie(response.headers("Set-Cookie"), AUTH_COOKIE))
            }
        }

    /** POST /api/v1/users/2fa/disable — disables 2FA with a live code; re-issues the session cookie. */
    suspend fun disableTwoFactor(code: String): Result<ReissuedTokenResult<MessageResponse>> =
        executePost("$TWO_FACTOR_PATH/disable", TwoFactorCodeInput(code)) { response, body ->
            val parsed = moshi.adapter(MessageResponse::class.java).fromJson(body)
            if (parsed == null) {
                null
            } else {
                ReissuedTokenResult(parsed, extractCookie(response.headers("Set-Cookie"), AUTH_COOKIE))
            }
        }

    /** POST /api/v1/users/2fa/recovery-codes/regenerate — new codes shown plaintext exactly once. */
    suspend fun regenerateRecoveryCodes(code: String): Result<ReissuedTokenResult<TwoFactorConfirmResponse>> =
        executePost("$TWO_FACTOR_PATH/recovery-codes/regenerate", TwoFactorCodeInput(code)) { response, body ->
            val parsed = moshi.adapter(TwoFactorConfirmResponse::class.java).fromJson(body)
            if (parsed == null) {
                null
            } else {
                ReissuedTokenResult(parsed, extractCookie(response.headers("Set-Cookie"), AUTH_COOKIE))
            }
        }

    /** GET /api/v1/users/me. */
    suspend fun currentUser(): Result<UserProfile> =
        executeGet("$PLACEHOLDER_ORIGIN$ME_PATH") { _, body ->
            moshi.adapter(UserProfile::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/auth/oidc/config — public, unauthenticated. Fetched by
     * RegisterScreen to show a "registration disabled" notice up front
     * instead of only via the eventual 403 on submit.
     */
    suspend fun getAuthConfig(): Result<AuthConfig> =
        executeGet("$PLACEHOLDER_ORIGIN$AUTH_CONFIG_PATH") { _, body ->
            moshi.adapter(AuthConfig::class.java).fromJson(body)
        }

    // M26: account creation + password reset. All public and rate-limited
    // server-side (AuthRateLimitMiddleware); the register flow auto-logs-in on
    // success (the ticket's test case 3), so this surface pairs with [login].

    /** POST /api/v1/register — 201 `{ message }`; 409 on duplicate email/username. */
    suspend fun register(username: String, email: String, password: String): Result<MessageResponse> =
        executePost(REGISTER_PATH, RegisterRequest(username = username, email = email, password = password)) { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/check-password-strength — raw, unwrapped [PasswordStrength]. */
    suspend fun checkPasswordStrength(password: String): Result<PasswordStrength> =
        executePost(CHECK_PASSWORD_STRENGTH_PATH, CheckPasswordStrengthRequest(password)) { _, body ->
            moshi.adapter(PasswordStrength::class.java).fromJson(body)
        }

    /** POST /api/v1/password-reset/request — anti-enumeration: always the same message. */
    suspend fun requestPasswordReset(email: String): Result<MessageResponse> =
        executePost(PASSWORD_RESET_REQUEST_PATH, PasswordResetRequest(email)) { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/password-reset/confirm — resets the password and bumps TokenVersion. */
    suspend fun confirmPasswordReset(token: String, password: String): Result<MessageResponse> =
        executePost(
            PASSWORD_RESET_CONFIRM_PATH,
            PasswordResetConfirmRequest(token = token, password = password),
        ) { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    // --- M25: settings surfaces (profile prefs, webhooks, notification channels) ---
    // All endpoints pre-date the Android client (route table in
    // backend/routes/routes.go); the gap this ticket closes is the missing
    // client surface, not the backend.

    /** PATCH /api/v1/users/language — the same route web's SettingsPage uses. */
    suspend fun updateLanguage(language: String): Result<MessageResponse> =
        executePatch("$PLACEHOLDER_ORIGIN$USERS_PATH/language", UpdateLanguageRequest(language)) { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    /** PATCH /api/v1/users/date-format — the same route web's SettingsPage uses. */
    suspend fun updateDateFormat(dateFormat: String): Result<MessageResponse> =
        executePatch("$PLACEHOLDER_ORIGIN$USERS_PATH/date-format", UpdateDateFormatRequest(dateFormat)) { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    /**
     * POST /api/v1/users/change-password — the server bumps TokenVersion on
     * success, invalidating every JWT (including the caller's bearer token),
     * so the Android session must re-login afterwards. A wrong current
     * password is a 400 with the server's message, surfaced via [ApiError].
     */
    suspend fun changePassword(currentPassword: String, newPassword: String): Result<MessageResponse> =
        executePost("$USERS_PATH/change-password", ChangePasswordRequest(currentPassword, newPassword)) { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    /** GET /api/v1/notifications/config — flat per-user notification config. */
    suspend fun getNotificationConfig(): Result<NotificationConfig> =
        executeGet("$PLACEHOLDER_ORIGIN$NOTIFICATIONS_CONFIG_PATH") { _, body ->
            moshi.adapter(NotificationConfig::class.java).fromJson(body)
        }

    /** PUT /api/v1/notifications/config — full config echo; the token is never returned. */
    suspend fun saveNotificationConfig(input: NotificationConfigInput): Result<NotificationConfig> =
        executePut("$PLACEHOLDER_ORIGIN$NOTIFICATIONS_CONFIG_PATH", input) { _, body ->
            moshi.adapter(NotificationConfig::class.java).fromJson(body)
        }

    /** POST /api/v1/notifications/config/test — diagnosed failures are HTTP 200 `{ok:false}`. */
    suspend fun testNotificationChannel(channel: String): Result<NotificationTestResult> =
        executePost("$NOTIFICATIONS_CONFIG_PATH/test", NotificationTestChannelRequest(channel)) { _, body ->
            moshi.adapter(NotificationTestResult::class.java).fromJson(body)
        }

    // M5 §5a (issue #152): mobile push device registrations — the Android FCM
    // client registers on login and deletes on logout. The backend endpoints
    // (M2) pre-date this client; the gap was the missing Android surface.

    /** POST /api/v1/notifications/devices — 201, the raw [DeviceRegistration] row. */
    suspend fun registerDevice(input: DeviceRegistrationInput): Result<DeviceRegistration> =
        executePost(NOTIFICATIONS_DEVICES_PATH, input) { _, body ->
            moshi.adapter(DeviceRegistration::class.java).fromJson(body)
        }

    /** DELETE /api/v1/notifications/devices/:id — `{ message }`. */
    suspend fun deleteDevice(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$NOTIFICATIONS_DEVICES_PATH/$id")

    /** GET /api/v1/notifications/devices — `{ devices: [...] }`, unwrapped here. */
    suspend fun listDeviceRegistrations(): Result<List<DeviceRegistration>> =
        executeGet("$PLACEHOLDER_ORIGIN$NOTIFICATIONS_DEVICES_PATH") { _, body ->
            moshi.adapter(DeviceRegistrationsResponse::class.java).fromJson(body)?.devices
        }

    /** GET /api/v1/webhooks — `{ webhooks: [...] }`, unwrapped here. */
    suspend fun listWebhooks(): Result<List<Webhook>> =
        executeGet("$PLACEHOLDER_ORIGIN$WEBHOOKS_PATH") { _, body ->
            moshi.adapter(WebhooksResponse::class.java).fromJson(body)?.webhooks
        }

    /** POST /api/v1/webhooks — 201; the only response that carries the plaintext secret. */
    suspend fun createWebhook(input: WebhookInput): Result<WebhookCreateResponse> =
        executePost(WEBHOOKS_PATH, input) { _, body ->
            moshi.adapter(WebhookCreateResponse::class.java).fromJson(body)
        }

    /** PUT /api/v1/webhooks/{id} — raw Webhook response (no secret). */
    suspend fun updateWebhook(id: Int, input: WebhookInput): Result<Webhook> =
        executePut("$PLACEHOLDER_ORIGIN$WEBHOOKS_PATH/$id", input) { _, body ->
            moshi.adapter(Webhook::class.java).fromJson(body)
        }

    /** DELETE /api/v1/webhooks/{id} — `{ message }`. */
    suspend fun deleteWebhook(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$WEBHOOKS_PATH/$id")

    /** POST /api/v1/webhooks/{id}/test — `{ delivery: {...} }`, unwrapped here. */
    suspend fun testWebhook(id: Int): Result<WebhookDelivery> =
        executePostEmpty("$WEBHOOKS_PATH/$id/test") { _, body ->
            moshi.adapter(WebhookTestResponse::class.java).fromJson(body)?.delivery
        }

    /** GET /api/v1/webhooks/{id}/deliveries — `{ deliveries: [...] }`, most recent 50, unwrapped here. */
    suspend fun getWebhookDeliveries(id: Int): Result<List<WebhookDelivery>> =
        executeGet("$PLACEHOLDER_ORIGIN$WEBHOOKS_PATH/$id/deliveries") { _, body ->
            moshi.adapter(WebhookDeliveriesResponse::class.java).fromJson(body)?.deliveries
        }

    // --- Issue #413 / #573: API token lifecycle (list/create/revoke/rotate). ---
    // The endpoints pre-date the Android client (backend/routes/routes.go); the
    // gap this closes is the missing Android surface, same as webhooks (M25).

    /** GET /api/v1/api-tokens — `{ tokens: [...] }`, unwrapped here. */
    suspend fun listApiTokens(): Result<List<ApiToken>> =
        executeGet("$PLACEHOLDER_ORIGIN$API_TOKENS_PATH") { _, body ->
            moshi.adapter(ApiTokensResponse::class.java).fromJson(body)?.tokens
        }

    /** POST /api/v1/api-tokens — 201; the only response that carries the plaintext token. */
    suspend fun createApiToken(input: ApiTokenInput): Result<ApiTokenCreateResponse> =
        executePost(API_TOKENS_PATH, input) { _, body ->
            moshi.adapter(ApiTokenCreateResponse::class.java).fromJson(body)
        }

    /** DELETE /api/v1/api-tokens/{id} — `{ message }`. */
    suspend fun revokeApiToken(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$API_TOKENS_PATH/$id")

    /**
     * POST /api/v1/api-tokens/revoke-all — ends every one of the caller's
     * standing tokens at once (e.g. a lost device); `{ revoked: N }`.
     */
    suspend fun revokeAllApiTokens(): Result<RevokeAllApiTokensResponse> =
        executePostEmpty("$API_TOKENS_PATH/revoke-all") { _, body ->
            moshi.adapter(RevokeAllApiTokensResponse::class.java).fromJson(body)
        }

    /**
     * POST /api/v1/api-tokens/{id}/rotate — 201; revokes the token and
     * reissues a new one with the same name/scope. Like create, the new
     * plaintext is shown exactly once.
     */
    suspend fun rotateApiToken(id: Int): Result<ApiTokenCreateResponse> =
        executePostEmpty("$API_TOKENS_PATH/$id/rotate") { _, body ->
            moshi.adapter(ApiTokenCreateResponse::class.java).fromJson(body)
        }

    /** GET /api/v1/contacts (cursor-paginated list). */
    suspend fun listContacts(
        cursor: String? = null,
        limit: Int? = null,
        search: String? = null,
        includeArchived: Boolean? = null,
        // M23: filters by a circle NAME — the backend's `?circle=` matches
        // `circles.name` (contact_controller.go), matching web's filter value.
        circle: String? = null,
        // M26: the circle/tag-triage lookup — `?circle_legacy=` filters by a
        // legacy free-text circle string from the old flat `contacts.circles`
        // JSON column (CircleTagTriagePage's contact collection).
        circleLegacy: String? = null,
        // Issue #212: `?favorites=true` narrows to the caller's favorite
        // contacts only — the wire contract web #173 shipped.
        favorites: Boolean? = null,
        vcardUids: List<String>? = null,
    ): Result<ContactsPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CONTACTS_PATH".toHttpUrl().newBuilder()
        if (!vcardUids.isNullOrEmpty()) {
            // ?vcard_uid= (repeatable) short-circuits the backend's whole
            // search/sort/pagination path (contact_controller.go), so don't
            // send cursor/limit/search alongside it -- they'd be silently
            // ignored server-side, and sending them here would be misleading.
            vcardUids.forEach { urlBuilder.addQueryParameter("vcard_uid", it) }
            includeArchived?.let { urlBuilder.addQueryParameter("include_archived", it.toString()) }
        } else {
            cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
            limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
            search?.let { urlBuilder.addQueryParameter("search", it) }
            includeArchived?.let { urlBuilder.addQueryParameter("include_archived", it.toString()) }
            circle?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("circle", it) }
            circleLegacy?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("circle_legacy", it) }
            favorites?.let { urlBuilder.addQueryParameter("favorites", it.toString()) }
        }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ContactsPage::class.java).fromJson(body)
        }
    }

    /**
     * M26: GET /api/v1/contacts/circles?legacy=true — the distinct legacy
     * free-text circle strings still sitting in the old flat `contacts.circles`
     * JSON column, as a bare JSON array of strings. The circle/tag-triage tool
     * classifies each one.
     */
    suspend fun listLegacyCircles(): Result<List<String>> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/circles?legacy=true") { _, body ->
            moshi.adapter<List<String>>(
                com.squareup.moshi.Types.newParameterizedType(List::class.java, String::class.java),
            ).fromJson(body)
        }

    /** GET /api/v1/contacts/{id} (full neutral Record/Card). */
    suspend fun getContact(id: Int): Result<ContactRecordResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$id") { _, body ->
            moshi.adapter(ContactRecordResponse::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/contacts/{id}/briefing — the N2 prep-view composite (M11):
     * everything the user wants to remember before seeing a person in one
     * response. N2's backend does all the assembly; this is a read. The six
     * collection blocks are normalized in [ContactBriefing] (absent/null/[]
     * all decode to an empty list), so the screen can dereference `.size`
     * unconditionally — the exact contract regression that crashed web's prep
     * view into its ErrorBoundary (`/CLAUDE.md` frontend trap #8).
     */
    suspend fun getBriefing(contactId: Int): Result<ContactBriefing> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/briefing") { _, body ->
            moshi.adapter(ContactBriefing::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/dashboard — the M3 "today/overview" composite (M10): one
     * call replacing the four-request fan-out the dashboard used to fire
     * (`listUpcomingBirthdays`, `listUpcomingReminders`,
     * `listOverdueCadences`, plus a per-reminder contact lookup). The
     * backend embeds each reminder's contact display name (M3 design
     * decision 2), so no second fetch is needed. All four blocks are
     * normalized to `[]` server-side; see [DashboardResponse]'s doc comment.
     */
    suspend fun getDashboard(): Result<DashboardResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$DASHBOARD_PATH") { _, body ->
            moshi.adapter(DashboardResponse::class.java).fromJson(body)
        }

    /**
     * POST /api/v1/reach-out-suggestions/{id}/dismiss — dismisses a pending
     * event-driven reach-out suggestion (issue #177) so it stops appearing on
     * the dashboard. Idempotent server-side; no body.
     */
    suspend fun dismissReachOutSuggestion(id: String): Result<MessageResponse> =
        executePostEmpty("$REACH_OUT_SUGGESTIONS_PATH/$id/dismiss") { _, body ->
            moshi.adapter(MessageResponse::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/search — cross-entity FTS across notes and activities (T87: folded into the
     * contact list rather than a dedicated search screen). `q`'s two-character gate is the
     * backend's own; callers should apply it too rather than firing a request destined to
     * return empty. The response's `contacts` group is deliberately unmodeled — see
     * [SearchResult]'s doc comment.
     */
    suspend fun search(q: String, limit: Int? = null, householdId: String? = null): Result<SearchResult> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$SEARCH_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("q", q)
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        householdId?.let { urlBuilder.addQueryParameter("household_id", it) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(SearchResult::class.java).fromJson(body)
        }
    }

    /**
     * POST /api/v1/contacts. The create endpoint wraps its response as
     * `{ message, contact }` (ticket §2.6 asymmetry) — this method unwraps it
     * so callers receive the bare ContactRecordResponse.
     */
    suspend fun createContact(input: ContactRecordInput): Result<ContactRecordResponse> =
        executePost(CONTACTS_PATH, input) { _, body ->
            moshi.adapter(CreateContactResponse::class.java).fromJson(body)?.contact
        }

    /** PUT /api/v1/contacts/{id} — returns the raw ContactRecordResponse (200). */
    suspend fun updateContact(id: Int, input: ContactRecordInput): Result<ContactRecordResponse> =
        executePut("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$id", input) { _, body ->
            moshi.adapter(ContactRecordResponse::class.java).fromJson(body)
        }

    // M24: top-level contact actions (delete/archive/unarchive/export). The
    // endpoints all pre-date the Android client (route table in
    // backend/routes/routes.go) — the gap this ticket closes is the missing
    // client surface, not the backend.

    /** DELETE /api/v1/contacts/{id} — soft-delete; the row stays for undo (T60/M16). */
    suspend fun deleteContact(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$id")

    /** POST /api/v1/contacts/{id}/archive — archives the contact and retires its reminders. */
    suspend fun archiveContact(id: Int): Result<Unit> =
        executePostEmpty("$CONTACTS_PATH/$id/archive") { _, _ -> Unit }

    /** POST /api/v1/contacts/{id}/unarchive — restores an archived contact. */
    suspend fun unarchiveContact(id: Int): Result<Unit> =
        executePostEmpty("$CONTACTS_PATH/$id/unarchive") { _, _ -> Unit }

    // Issue #212 (web #173): the CRM-local favorite toggle. The endpoints
    // return the updated flat models.Contact; the Android client only needs
    // success/failure (the optimistic star flip is reconciled on error), so
    // the body is discarded — the same shape as archive/unarchive above.

    /** POST /api/v1/contacts/{id}/favorite — marks the contact as a favorite. */
    suspend fun favoriteContact(id: Int): Result<Unit> =
        executePostEmpty("$CONTACTS_PATH/$id/favorite") { _, _ -> Unit }

    /** POST /api/v1/contacts/{id}/unfavorite — clears the favorite flag. */
    suspend fun unfavoriteContact(id: Int): Result<Unit> =
        executePostEmpty("$CONTACTS_PATH/$id/unfavorite") { _, _ -> Unit }

    /**
     * GET /api/v1/export/vcf?vcard_uid=… — exports a single contact as vCard
     * (4.0 default, 3.0 when [version] == 3), honoring the backend's default
     * field selection (all sections, private/secret sensitivity excluded).
     * Returns the raw file bytes; see `executeGetBytes`.
     */
    suspend fun exportContactVcf(vcardUid: String, version: Int? = null): Result<ByteArray> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$EXPORT_VCF_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("vcard_uid", vcardUid)
        if (version == 3) urlBuilder.addQueryParameter("version", "3")
        return executeGetBytes(urlBuilder.build().toString())
    }

    /**
     * POST /api/v1/contacts/{id}/profile_picture — a multipart upload with
     * form field `photo`, matching web's `uploadProfilePicture` and the
     * backend's `AddPhotoToContact`. The backend re-sniffs the image format
     * from the bytes (JPEG/PNG/HEIC), so [mimeType] is informational; the
     * server enforces the 10MB cap itself (400 on excess). [fileName] is
     * arbitrary (the server names the stored file itself) — "profile.jpg" is
     * only a fallback for providers that need one. The 200 body is the updated
     * flat Contact, which this caller ignores: the detail (including the fresh
     * `card.photoUri`/`photoThumbnail`) is refetched by the ViewModel.
     */
    suspend fun uploadContactPhoto(
        id: Int,
        bytes: ByteArray,
        mimeType: String = "image/jpeg",
    ): Result<Unit> =
        executeMultipartUpload(
            path = "$CONTACTS_PATH/$id/profile_picture",
            fieldName = "photo",
            fileName = "profile.jpg",
            mediaType = mimeType,
            fileBytes = bytes,
        ) { _, _ -> Unit }

    /** GET /api/v1/field-definitions (T84). */
    suspend fun listFieldDefinitions(limit: Int? = null): Result<FieldDefinitionsResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$FIELD_DEFINITIONS_PATH".toHttpUrl().newBuilder()
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(FieldDefinitionsResponse::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/contacts/{id}/field-values (T84). */
    suspend fun listContactFieldValues(contactId: Int): Result<ContactFieldValuesResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/field-values") { _, body ->
            moshi.adapter(ContactFieldValuesResponse::class.java).fromJson(body)
        }

    /**
     * PUT /api/v1/contacts/{id}/field-values (T84) — full-replace; see
     * [ContactFieldValuesInput]'s doc comment. No UI calls this yet (T84 ships the read-only
     * slice); it exists for the round-trip test and so the write path isn't a second ticket.
     */
    suspend fun replaceContactFieldValues(
        contactId: Int,
        input: ContactFieldValuesInput,
    ): Result<ContactFieldValuesResponse> =
        executePut("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/field-values", input) { _, body ->
            moshi.adapter(ContactFieldValuesResponse::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/contacts/{id}/activities — a contact's activities (M19:
     * T17 cursor-paginated; [search]/[fromDate]/[toDate] filter server-side).
     */
    suspend fun listContactActivities(
        contactId: Int,
        cursor: String? = null,
        limit: Int? = null,
        search: String? = null,
        fromDate: String? = null,
        toDate: String? = null,
    ): Result<ContactActivitiesResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/activities".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        search?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("search", it) }
        fromDate?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("fromDate", it) }
        toDate?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("toDate", it) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ContactActivitiesResponse::class.java).fromJson(body)
        }
    }

    /** DELETE /api/v1/activities/{id} — soft-deletes an activity (M19). */
    suspend fun deleteActivity(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$ACTIVITIES_PATH/$id")

    /**
     * POST /api/v1/activities — wrapped `{ message, activity }`, unwrapped here.
     *
     * ANDROID-02 (issue #479): the outbox sync passes the row's [idempotencyKey] so a retry
     * after an ambiguous failure (server committed, response lost) replays the stored response
     * instead of creating a second Activity — see CON-04/ADR-0010. Other callers omit it and
     * get a plain, non-idempotent create.
     */
    suspend fun createActivity(input: ActivityInput, idempotencyKey: String? = null): Result<Activity> =
        executePost(ACTIVITIES_PATH, input, idempotencyKey = idempotencyKey) { _, body ->
            moshi.adapter(CreateActivityResponse::class.java).fromJson(body)?.activity
        }

    /** PUT /api/v1/activities/{id} — raw Activity response. */
    suspend fun updateActivity(id: Int, input: ActivityInput): Result<Activity> =
        executePut("$PLACEHOLDER_ORIGIN$ACTIVITIES_PATH/$id", input) { _, body ->
            moshi.adapter(Activity::class.java).fromJson(body)
        }

    /** GET /api/v1/activities/{id} — a single activity (with participants). */
    suspend fun getActivity(id: Int): Result<Activity> =
        executeGet("$PLACEHOLDER_ORIGIN$ACTIVITIES_PATH/$id") { _, body ->
            moshi.adapter(Activity::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/activities (cursor-paginated, all activities). [includeContacts] appends
     * `?include=contacts` — matches `GetActivities`' `c.DefaultQuery("include", "")` check —
     * so the M9 Activities inbox can show each activity's participants.
     */
    suspend fun listActivities(
        cursor: String? = null,
        limit: Int? = null,
        includeContacts: Boolean = false,
    ): Result<ActivitiesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$ACTIVITIES_PATH".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        if (includeContacts) urlBuilder.addQueryParameter("include", "contacts")
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ActivitiesPage::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/notes — the N4 unfiled-notes inbox (M9 Notes drawer entry), cursor-paginated. */
    suspend fun listNotes(cursor: String? = null, limit: Int? = null): Result<NotesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$NOTES_PATH".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(NotesPage::class.java).fromJson(body)
        }
    }

    /**
     * GET /api/v1/contacts/{id}/notes — a contact's notes (M19: T17
     * cursor-paginated; [search]/[fromDate]/[toDate] filter server-side).
     */
    suspend fun listContactNotes(
        contactId: Int,
        cursor: String? = null,
        limit: Int? = null,
        search: String? = null,
        fromDate: String? = null,
        toDate: String? = null,
    ): Result<ContactNotesResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/notes".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        search?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("search", it) }
        fromDate?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("fromDate", it) }
        toDate?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("toDate", it) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ContactNotesResponse::class.java).fromJson(body)
        }
    }

    /** DELETE /api/v1/notes/{id} — soft-deletes a note (M19). */
    suspend fun deleteNote(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$NOTES_PATH/$id")

    /** POST /api/v1/contacts/{id}/notes — wrapped `{ message, note }`, unwrapped here. */
    suspend fun createNote(contactId: Int, input: NoteInput): Result<Note> =
        executePost("$CONTACTS_PATH/$contactId/notes", input) { _, body ->
            moshi.adapter(CreateNoteResponse::class.java).fromJson(body)?.note
        }

    /** POST /api/v1/notes — create an unassigned note (contact_id honored, unlike the nested route). */
    suspend fun createUnassignedNote(input: NoteInput): Result<Note> =
        executePost(NOTES_PATH, input) { _, body ->
            moshi.adapter(CreateNoteResponse::class.java).fromJson(body)?.note
        }

    /** PUT /api/v1/notes/{id} — wrapped `{ message, note }`, unwrapped here. */
    suspend fun updateNote(id: Int, input: NoteInput): Result<Note> =
        executePut("$PLACEHOLDER_ORIGIN$NOTES_PATH/$id", input) { _, body ->
            moshi.adapter(CreateNoteResponse::class.java).fromJson(body)?.note
        }

    /** GET /api/v1/notes/{id} — a single note. */
    suspend fun getNote(id: Int): Result<Note> =
        executeGet("$PLACEHOLDER_ORIGIN$NOTES_PATH/$id") { _, body ->
            moshi.adapter(Note::class.java).fromJson(body)
        }

    /** GET /api/v1/contacts/{id}/reminders — a contact's reminders. */
    suspend fun listContactReminders(contactId: Int): Result<ContactRemindersResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/reminders") { _, body ->
            moshi.adapter(ContactRemindersResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/contacts/{id}/reminders — wrapped `{ message, reminder }`, unwrapped here. */
    suspend fun createReminder(contactId: Int, reminder: Reminder): Result<Reminder> =
        executePost("$CONTACTS_PATH/$contactId/reminders", reminder) { _, body ->
            moshi.adapter(CreateReminderResponse::class.java).fromJson(body)?.reminder
        }

    /** PUT /api/v1/reminders/{id} — wrapped `{ message, reminder }`, unwrapped here. */
    suspend fun updateReminder(id: Int, reminder: Reminder): Result<Reminder> =
        executePut("$PLACEHOLDER_ORIGIN$REMINDERS_PATH/$id", reminder) { _, body ->
            moshi.adapter(CreateReminderResponse::class.java).fromJson(body)?.reminder
        }

    /**
     * POST /api/v1/reminders/{id}/complete — completes a reminder (no body).
     * [skip] is the M10 skip path: `?skip=true` reschedules recurring
     * reminders without recording completion in the timeline (the web
     * confirms before calling it — match that at the call site).
     */
    suspend fun completeReminder(id: Int, skip: Boolean = false): Result<ReminderCompleteResponse> {
        val path = if (skip) "$REMINDERS_PATH/$id/complete?skip=true" else "$REMINDERS_PATH/$id/complete"
        return executePostEmpty(path) { _, body ->
            moshi.adapter(ReminderCompleteResponse::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/reminders — all reminders for the user. */
    suspend fun listReminders(): Result<ContactRemindersResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$REMINDERS_PATH") { _, body ->
            moshi.adapter(ContactRemindersResponse::class.java).fromJson(body)
        }

    /** GET /api/v1/reminders/upcoming — reminders due within ~7 days. */
    suspend fun listUpcomingReminders(): Result<ContactRemindersResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$REMINDERS_PATH/upcoming") { _, body ->
            moshi.adapter(ContactRemindersResponse::class.java).fromJson(body)
        }

    /** GET /api/v1/reminders/{id} — a single reminder. */
    suspend fun getReminder(id: Int): Result<Reminder> =
        executeGet("$PLACEHOLDER_ORIGIN$REMINDERS_PATH/$id") { _, body ->
            moshi.adapter(Reminder::class.java).fromJson(body)
        }

    /** DELETE /api/v1/reminders/{id} — delete a reminder. */
    suspend fun deleteReminder(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$REMINDERS_PATH/$id")

    /** GET /api/v1/contacts/{id}/reminder-completions — a contact's completion timeline. */
    suspend fun listContactReminderCompletions(contactId: Int): Result<CompletionsResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/$contactId/reminder-completions") { _, body ->
            moshi.adapter(CompletionsResponse::class.java).fromJson(body)
        }

    /** DELETE /api/v1/reminder-completions/{id} — remove a completion (undo). */
    suspend fun deleteReminderCompletion(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$REMINDER_COMPLETIONS_PATH/$id")

    /** GET /api/v1/contacts/birthdays — upcoming birthdays. */
    suspend fun listUpcomingBirthdays(): Result<BirthdaysResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CONTACTS_PATH/birthdays") { _, body ->
            moshi.adapter(BirthdaysResponse::class.java).fromJson(body)
        }

    /** GET /api/v1/cadence-policies/overdue — overdue cadences. */
    suspend fun listOverdueCadences(): Result<OverdueCadencesResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CADENCE_POLICIES_PATH/overdue") { _, body ->
            moshi.adapter(OverdueCadencesResponse::class.java).fromJson(body)
        }

    /**
     * GET /api/v1/cadence-policies?entity_id=… — a contact's policies
     * (0 or 1, server-enforced). `entity_id` is the Contact.VCardUID.
     */
    suspend fun listCadencePolicies(entityId: String): Result<CadencePoliciesResponse> {
        val url = "$PLACEHOLDER_ORIGIN$CADENCE_POLICIES_PATH".toHttpUrl().newBuilder()
            .addQueryParameter("entity_id", entityId)
            .build()
        return executeGet(url.toString()) { _, body ->
            moshi.adapter(CadencePoliciesResponse::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/cadence-policies/{id} — raw (unwrapped) policy with health. */
    suspend fun getCadencePolicy(id: String): Result<CadencePolicy> =
        executeGet("$PLACEHOLDER_ORIGIN$CADENCE_POLICIES_PATH/$id") { _, body ->
            moshi.adapter(CadencePolicy::class.java).fromJson(body)
        }

    /** POST /api/v1/cadence-policies — wrapped `{ cadence_policy }`, unwrapped here. */
    suspend fun createCadencePolicy(input: CadencePolicyInput): Result<CadencePolicy> =
        executePost(CADENCE_POLICIES_PATH, input) { _, body ->
            moshi.adapter(CreateCadencePolicyResponse::class.java).fromJson(body)?.cadencePolicy
        }

    /**
     * PUT /api/v1/cadence-policies/{id} — raw (unwrapped) updated policy,
     * unlike create's wrapped response (deliberate backend asymmetry).
     */
    suspend fun updateCadencePolicy(id: String, input: CadencePolicyInput): Result<CadencePolicy> =
        executePut("$PLACEHOLDER_ORIGIN$CADENCE_POLICIES_PATH/$id", input) { _, body ->
            moshi.adapter(CadencePolicy::class.java).fromJson(body)
        }

    /** DELETE /api/v1/cadence-policies/{id} — `{ message }`. */
    suspend fun deleteCadencePolicy(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$CADENCE_POLICIES_PATH/$id")

    /** GET /api/v1/circles — cursor-paginated; members when include_members=true. */
    suspend fun listCircles(
        cursor: String? = null,
        limit: Int? = null,
        includeMembers: Boolean = false,
    ): Result<CirclesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CIRCLES_PATH".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        if (includeMembers) urlBuilder.addQueryParameter("include_members", "true")
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(CirclesPage::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/circles/{id} — `{ circle, members }`. */
    suspend fun getCircle(id: String): Result<CircleDetailResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$CIRCLES_PATH/$id") { _, body ->
            moshi.adapter(CircleDetailResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/circles — wrapped `{ message, circle }`, unwrapped here. */
    suspend fun createCircle(input: CircleInput): Result<Circle> =
        executePost(CIRCLES_PATH, input) { _, body ->
            moshi.adapter(CreateCircleResponse::class.java).fromJson(body)?.circle
        }

    /** PUT /api/v1/circles/{id} — raw Circle response. */
    suspend fun updateCircle(id: String, input: CircleInput): Result<Circle> =
        executePut("$PLACEHOLDER_ORIGIN$CIRCLES_PATH/$id", input) { _, body ->
            moshi.adapter(Circle::class.java).fromJson(body)
        }

    /** DELETE /api/v1/circles/{id}. */
    suspend fun deleteCircle(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$CIRCLES_PATH/$id")

    /** POST /api/v1/circles/{id}/members — wrapped `{ message, member }`. */
    suspend fun addCircleMember(circleId: String, input: CircleMemberInput): Result<CircleMember> =
        executePost("$CIRCLES_PATH/$circleId/members", input) { _, body ->
            moshi.adapter(AddCircleMemberResponse::class.java).fromJson(body)?.member
        }

    /** DELETE /api/v1/circles/{id}/members/{vcard_uid}. */
    suspend fun removeCircleMember(circleId: String, vcardUid: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$CIRCLES_PATH/$circleId/members/$vcardUid")

    /** GET /api/v1/tags — cursor-paginated; contacts when include_contacts=true. */
    suspend fun listTags(
        cursor: String? = null,
        limit: Int? = null,
        includeContacts: Boolean = false,
    ): Result<TagsPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$TAGS_PATH".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        if (includeContacts) urlBuilder.addQueryParameter("include_contacts", "true")
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(TagsPage::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/tags/{id} — `{ tag, contacts }`. */
    suspend fun getTag(id: String): Result<TagDetailResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$TAGS_PATH/$id") { _, body ->
            moshi.adapter(TagDetailResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/tags — wrapped `{ message, tag }`, unwrapped here. */
    suspend fun createTag(input: TagInput): Result<Tag> =
        executePost(TAGS_PATH, input) { _, body ->
            moshi.adapter(CreateTagResponse::class.java).fromJson(body)?.tag
        }

    /** PUT /api/v1/tags/{id} — raw Tag response. */
    suspend fun updateTag(id: String, input: TagInput): Result<Tag> =
        executePut("$PLACEHOLDER_ORIGIN$TAGS_PATH/$id", input) { _, body ->
            moshi.adapter(Tag::class.java).fromJson(body)
        }

    /** DELETE /api/v1/tags/{id}. */
    suspend fun deleteTag(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$TAGS_PATH/$id")

    /** POST /api/v1/tags/{id}/contacts — wrapped `{ message, tagging }`. */
    suspend fun addContactTag(tagId: String, input: ContactTagInput): Result<ContactTag> =
        executePost("$TAGS_PATH/$tagId/contacts", input) { _, body ->
            moshi.adapter(AddContactTagResponse::class.java).fromJson(body)?.tagging
        }

    /** DELETE /api/v1/tags/{id}/contacts/{vcard_uid}. */
    suspend fun removeContactTag(tagId: String, vcardUid: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$TAGS_PATH/$tagId/contacts/$vcardUid")

    /** GET /api/v1/households — cursor-paginated; members when include_members=true. */
    suspend fun listHouseholds(
        cursor: String? = null,
        limit: Int? = null,
        includeMembers: Boolean = false,
    ): Result<HouseholdsPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$HOUSEHOLDS_PATH".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        if (includeMembers) urlBuilder.addQueryParameter("include_members", "true")
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(HouseholdsPage::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/households/{id} — `{ household, members }`. */
    suspend fun getHousehold(id: String): Result<HouseholdDetailResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$HOUSEHOLDS_PATH/$id") { _, body ->
            moshi.adapter(HouseholdDetailResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/households — wrapped `{ message, household }`, unwrapped here. */
    suspend fun createHousehold(input: HouseholdInput): Result<Household> =
        executePost(HOUSEHOLDS_PATH, input) { _, body ->
            moshi.adapter(CreateHouseholdResponse::class.java).fromJson(body)?.household
        }

    /** PUT /api/v1/households/{id} — raw Household response. */
    suspend fun updateHousehold(id: String, input: HouseholdInput): Result<Household> =
        executePut("$PLACEHOLDER_ORIGIN$HOUSEHOLDS_PATH/$id", input) { _, body ->
            moshi.adapter(Household::class.java).fromJson(body)
        }

    /** DELETE /api/v1/households/{id}. */
    suspend fun deleteHousehold(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$HOUSEHOLDS_PATH/$id")

    /** POST /api/v1/households/{id}/members — wrapped `{ message, member }`. */
    suspend fun addHouseholdMember(id: String, input: HouseholdMemberInput): Result<HouseholdMember> =
        executePost("$HOUSEHOLDS_PATH/$id/members", input) { _, body ->
            moshi.adapter(AddHouseholdMemberResponse::class.java).fromJson(body)?.member
        }

    /** DELETE /api/v1/households/{id}/members/{vcard_uid}. */
    suspend fun removeHouseholdMember(id: String, vcardUid: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$HOUSEHOLDS_PATH/$id/members/$vcardUid")

    /** PATCH /api/v1/households/{id}/members/{vcard_uid} — update role/since/until. */
    suspend fun updateHouseholdMember(id: String, vcardUid: String, input: HouseholdMemberInput): Result<Unit> =
        executePatch("$PLACEHOLDER_ORIGIN$HOUSEHOLDS_PATH/$id/members/$vcardUid", input)

    /** POST /api/v1/households/{id}/suggest-relationships — trigger the relationship-suggestion engine. */
    suspend fun suggestHouseholdRelationships(id: String): Result<SuggestRelationshipsResponse> =
        executePostEmpty("$HOUSEHOLDS_PATH/$id/suggest-relationships") { _, body ->
            moshi.adapter(SuggestRelationshipsResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/households/suggest-addresses — T40 shared-address scan (read-only, idempotent). */
    suspend fun suggestAddressHouseholds(): Result<AddressSuggestionsResponse> =
        executePostEmpty("$HOUSEHOLDS_PATH/suggest-addresses") { _, body ->
            moshi.adapter(AddressSuggestionsResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/households/suggestions/accept — materialize a household from a suggested group; unwrapped `{ household }`. */
    suspend fun acceptHouseholdSuggestion(input: AcceptHouseholdSuggestionInput): Result<Household> =
        executePost("$HOUSEHOLDS_PATH/suggestions/accept", input) { _, body ->
            moshi.adapter(AcceptHouseholdSuggestionResponse::class.java).fromJson(body)?.household
        }

    /** POST /api/v1/households/suggestions/dismiss — permanently dismiss a suggested group. */
    suspend fun dismissHouseholdSuggestion(input: DismissHouseholdSuggestionInput): Result<Unit> =
        executePost("$HOUSEHOLDS_PATH/suggestions/dismiss", input) { _, _ -> Unit }

    /** GET /api/v1/relationship-edges — cursor-paginated, filtered by contact. */
    suspend fun listRelationshipEdges(
        contactId: String,
        status: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<RelationshipEdgesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$RELATIONSHIP_EDGES_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("contact_id", contactId)
        status?.let { urlBuilder.addQueryParameter("status", it) }
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(RelationshipEdgesPage::class.java).fromJson(body)
        }
    }

    /** POST /api/v1/relationship-edges — wrapped `{ relationship_edge }`, unwrapped here. */
    suspend fun createRelationshipEdge(input: RelationshipEdgeInput): Result<RelationshipEdge> =
        executePost(RELATIONSHIP_EDGES_PATH, input) { _, body ->
            moshi.adapter(CreateRelationshipEdgeResponse::class.java).fromJson(body)?.relationshipEdge
        }

    /** PUT /api/v1/relationship-edges/{id} — raw (unwrapped) updated edge, unlike create's wrapped response. */
    suspend fun updateRelationshipEdge(id: String, input: RelationshipEdgeInput): Result<RelationshipEdge> =
        executePut("$PLACEHOLDER_ORIGIN$RELATIONSHIP_EDGES_PATH/$id", input) { _, body ->
            moshi.adapter(RelationshipEdge::class.java).fromJson(body)
        }

    /** PATCH /api/v1/relationship-edges/{id}/accept — promotes a suggestion; raw edge. */
    suspend fun acceptRelationshipEdge(id: String): Result<RelationshipEdge> =
        executePatchEmpty("$PLACEHOLDER_ORIGIN$RELATIONSHIP_EDGES_PATH/$id/accept") { _, body ->
            moshi.adapter(RelationshipEdge::class.java).fromJson(body)
        }

    /** DELETE /api/v1/relationship-edges/{id} — doubles as reject for suggestions. */
    suspend fun deleteRelationshipEdge(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$RELATIONSHIP_EDGES_PATH/$id")

    /** POST /api/v1/relationship-edges/suggest — T104 graph-inference trigger (one round, idempotent). */
    suspend fun suggestRelationshipEdges(): Result<RelationshipSuggestionsResponse> =
        executePostEmpty("$RELATIONSHIP_EDGES_PATH/suggest") { _, body ->
            moshi.adapter(RelationshipSuggestionsResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/contacts/address-suggestions — read-only, idempotent address-suggestion scan. */
    suspend fun suggestContactAddresses(): Result<ContactAddressSuggestionsResponse> =
        executePostEmpty("$CONTACTS_PATH/address-suggestions") { _, body ->
            moshi.adapter(ContactAddressSuggestionsResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/contacts/address-suggestions/apply — apply one address suggestion. */
    suspend fun applyContactAddressSuggestion(input: ApplyContactAddressSuggestionInput): Result<Unit> =
        executePost("$CONTACTS_PATH/address-suggestions/apply", input) { _, _ -> Unit }

    // --- Issue #220: ExternalIdentity substrate (T14) + the Immich integration (T15/T16) ---
    // All endpoints pre-date the Android client; the backend is platform-agnostic
    // (external_identity_controller.go, immich_controller.go). The Android surface
    // is a read-only External Links list + delete, plus the Immich "choose from
    // Immich" profile-photo flow. Thumbnails/photos render via Coil against the
    // proxied URLs (auth attached by the shared stack); picks are fetched as bytes.

    /** GET /api/v1/external-identities?contact_id=… — cursor-paginated, full_resync. */
    suspend fun listExternalIdentities(contactId: String, limit: Int = 100): Result<ExternalIdentitiesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$EXTERNAL_IDENTITIES_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("contact_id", contactId)
        urlBuilder.addQueryParameter("limit", limit.toString())
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ExternalIdentitiesPage::class.java).fromJson(body)
        }
    }

    /** DELETE /api/v1/external-identities/:id — hard delete (edge-shaped row). */
    suspend fun deleteExternalIdentity(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$EXTERNAL_IDENTITIES_PATH/$id")

    /** GET /api/v1/immich/config — `has_api_key` gates the Immich UI entry points. */
    suspend fun getImmichConfig(): Result<ImmichConfigResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$IMMICH_PATH/config") { _, body ->
            moshi.adapter(ImmichConfigResponse::class.java).fromJson(body)
        }

    /** GET /api/v1/immich/people — every person in the user's instance, unwrapped. */
    suspend fun listImmichPeople(): Result<List<ImmichPerson>> =
        executeGet("$PLACEHOLDER_ORIGIN$IMMICH_PATH/people") { _, body ->
            moshi.adapter(ImmichPeopleResponse::class.java).fromJson(body)?.people
        }

    /** POST /api/v1/immich/contacts/:vcard_uid/link — links the person; 201. */
    suspend fun linkImmichContact(vcardUid: String, personId: String, personName: String): Result<Unit> =
        executePost("$IMMICH_PATH/contacts/$vcardUid/link", ImmichLinkRequest(personId, personName)) { _, _ -> Unit }

    /** DELETE /api/v1/immich/contacts/:vcard_uid/link — unlinks; keeps enrichment history. */
    suspend fun unlinkImmichContact(vcardUid: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$IMMICH_PATH/contacts/$vcardUid/link")

    /** GET /api/v1/immich/contacts/:vcard_uid/summary — null when unlinked. */
    suspend fun getImmichContactSummary(vcardUid: String): Result<ImmichPersonSummary?> {
        // The mapper returns the non-null wrapper so `summary: null` is a
        // success-with-null, not a parse failure (execute treats a null mapper
        // result as "empty body").
        val response = executeGet("$PLACEHOLDER_ORIGIN$IMMICH_PATH/contacts/$vcardUid/summary") { _, body ->
            moshi.adapter(ImmichSummaryResponse::class.java).fromJson(body)
        }
        return response.map { it.summary }
    }

    /** GET /api/v1/immich/contacts/:vcard_uid/assets — recent photos (id + occurred_at). */
    suspend fun listImmichContactAssets(vcardUid: String): Result<List<ImmichAssetSummary>> =
        executeGet("$PLACEHOLDER_ORIGIN$IMMICH_PATH/contacts/$vcardUid/assets") { _, body ->
            moshi.adapter(ImmichAssetsResponse::class.java).fromJson(body)?.assets
        }

    /** GET /api/v1/immich/contacts/:vcard_uid/thumbnail — the linked person's photo bytes. */
    suspend fun getImmichThumbnailBytes(vcardUid: String): Result<ByteArray> =
        executeGetBytes("$PLACEHOLDER_ORIGIN$IMMICH_PATH/contacts/$vcardUid/thumbnail")

    /** GET /api/v1/immich/contacts/:vcard_uid/assets/:asset_id/image — one photo's bytes. */
    suspend fun getImmichAssetImageBytes(vcardUid: String, assetId: String): Result<ByteArray> =
        executeGetBytes("$PLACEHOLDER_ORIGIN$IMMICH_PATH/contacts/$vcardUid/assets/$assetId/image")

    // --- Issue #236: Immich config CRUD + test-connection, and the Paperless/Seafile/
    // Nextcloud create/link surfaces. See FileLinkIntegrations.kt's doc comment for why
    // these three write the ExternalIdentity row server-side via their own /link endpoint
    // rather than the generic POST /external-identities.

    /** PUT /api/v1/immich/config — full config echo; the API key is never returned. */
    suspend fun saveImmichConfig(input: ImmichConfigInput): Result<ImmichConfigResponse> =
        executePut("$PLACEHOLDER_ORIGIN$IMMICH_PATH/config", input) { _, body ->
            moshi.adapter(ImmichConfigResponse::class.java).fromJson(body)
        }

    /** DELETE /api/v1/immich/config. */
    suspend fun deleteImmichConfig(): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$IMMICH_PATH/config")

    /** POST /api/v1/immich/test-connection — diagnosed failures are HTTP 200 `{ok:false}`. */
    suspend fun testImmichConnection(): Result<ImmichConnectionTestResult> =
        executePostEmpty("$IMMICH_PATH/test-connection") { _, body ->
            moshi.adapter(ImmichConnectionTestResult::class.java).fromJson(body)
        }

    /** GET /api/v1/paperless/config — `has_api_token` gates the Paperless UI entry points. */
    suspend fun getPaperlessConfig(): Result<PaperlessConfigResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$PAPERLESS_PATH/config") { _, body ->
            moshi.adapter(PaperlessConfigResponse::class.java).fromJson(body)
        }

    /** PUT /api/v1/paperless/config — full config echo; the token is never returned. */
    suspend fun savePaperlessConfig(input: PaperlessConfigInput): Result<PaperlessConfigResponse> =
        executePut("$PLACEHOLDER_ORIGIN$PAPERLESS_PATH/config", input) { _, body ->
            moshi.adapter(PaperlessConfigResponse::class.java).fromJson(body)
        }

    /** DELETE /api/v1/paperless/config. */
    suspend fun deletePaperlessConfig(): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$PAPERLESS_PATH/config")

    /** POST /api/v1/paperless/test-connection — diagnosed failures are HTTP 200 `{ok:false}`. */
    suspend fun testPaperlessConnection(): Result<PaperlessConnectionTestResult> =
        executePostEmpty("$PAPERLESS_PATH/test-connection") { _, body ->
            moshi.adapter(PaperlessConnectionTestResult::class.java).fromJson(body)
        }

    /** GET /api/v1/paperless/documents?query=… — full list when [query] is null; unwrapped. */
    suspend fun searchPaperlessDocuments(query: String? = null): Result<List<PaperlessDocument>> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$PAPERLESS_PATH/documents".toHttpUrl().newBuilder()
        if (!query.isNullOrBlank()) urlBuilder.addQueryParameter("query", query)
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(PaperlessDocumentsResponse::class.java).fromJson(body)?.documents
        }
    }

    /** POST /api/v1/paperless/contacts/:vcard_uid/link — 201; writes the ExternalIdentity server-side. */
    suspend fun linkPaperlessContact(vcardUid: String, documentId: String): Result<Unit> =
        executePost("$PAPERLESS_PATH/contacts/$vcardUid/link", PaperlessLinkRequest(documentId)) { _, _ -> Unit }

    /** GET /api/v1/seafile/config — `has_api_token` gates the Seafile UI entry points. */
    suspend fun getSeafileConfig(): Result<SeafileConfigResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$SEAFILE_PATH/config") { _, body ->
            moshi.adapter(SeafileConfigResponse::class.java).fromJson(body)
        }

    /** PUT /api/v1/seafile/config — full config echo; the token is never returned. */
    suspend fun saveSeafileConfig(input: SeafileConfigInput): Result<SeafileConfigResponse> =
        executePut("$PLACEHOLDER_ORIGIN$SEAFILE_PATH/config", input) { _, body ->
            moshi.adapter(SeafileConfigResponse::class.java).fromJson(body)
        }

    /** DELETE /api/v1/seafile/config. */
    suspend fun deleteSeafileConfig(): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$SEAFILE_PATH/config")

    /** POST /api/v1/seafile/test-connection — diagnosed failures are HTTP 200 `{ok:false}`. */
    suspend fun testSeafileConnection(): Result<SeafileConnectionTestResult> =
        executePostEmpty("$SEAFILE_PATH/test-connection") { _, body ->
            moshi.adapter(SeafileConnectionTestResult::class.java).fromJson(body)
        }

    /** GET /api/v1/seafile/libraries — unwrapped. */
    suspend fun listSeafileLibraries(): Result<List<SeafileLibrary>> =
        executeGet("$PLACEHOLDER_ORIGIN$SEAFILE_PATH/libraries") { _, body ->
            moshi.adapter(SeafileLibrariesResponse::class.java).fromJson(body)?.libraries
        }

    /** GET /api/v1/seafile/libraries/:repo_id/dir?path=… — unwrapped. */
    suspend fun listSeafileDir(repoId: String, path: String): Result<List<SeafileItem>> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$SEAFILE_PATH/libraries/$repoId/dir".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("path", path)
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(SeafileItemsResponse::class.java).fromJson(body)?.items
        }
    }

    /** POST /api/v1/seafile/contacts/:vcard_uid/link — 201; writes the ExternalIdentity server-side. */
    suspend fun linkSeafileContact(vcardUid: String, request: SeafileLinkRequest): Result<Unit> =
        executePost("$SEAFILE_PATH/contacts/$vcardUid/link", request) { _, _ -> Unit }

    /** GET /api/v1/nextcloud/config — `has_app_password` gates the Nextcloud UI entry points. */
    suspend fun getNextcloudConfig(): Result<NextcloudConfigResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$NEXTCLOUD_PATH/config") { _, body ->
            moshi.adapter(NextcloudConfigResponse::class.java).fromJson(body)
        }

    /** PUT /api/v1/nextcloud/config — full config echo; the app password is never returned. */
    suspend fun saveNextcloudConfig(input: NextcloudConfigInput): Result<NextcloudConfigResponse> =
        executePut("$PLACEHOLDER_ORIGIN$NEXTCLOUD_PATH/config", input) { _, body ->
            moshi.adapter(NextcloudConfigResponse::class.java).fromJson(body)
        }

    /** DELETE /api/v1/nextcloud/config. */
    suspend fun deleteNextcloudConfig(): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$NEXTCLOUD_PATH/config")

    /** POST /api/v1/nextcloud/test-connection — diagnosed failures are HTTP 200 `{ok:false}`. */
    suspend fun testNextcloudConnection(): Result<NextcloudConnectionTestResult> =
        executePostEmpty("$NEXTCLOUD_PATH/test-connection") { _, body ->
            moshi.adapter(NextcloudConnectionTestResult::class.java).fromJson(body)
        }

    /** GET /api/v1/nextcloud/dir?path=… — defaults to the dav root; unwrapped. */
    suspend fun listNextcloudDir(path: String = "/"): Result<List<WebDAVItem>> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$NEXTCLOUD_PATH/dir".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("path", path)
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(NextcloudItemsResponse::class.java).fromJson(body)?.items
        }
    }

    /** POST /api/v1/nextcloud/contacts/:vcard_uid/link — 201; writes the ExternalIdentity server-side. */
    suspend fun linkNextcloudContact(vcardUid: String, request: NextcloudLinkRequest): Result<Unit> =
        executePost("$NEXTCLOUD_PATH/contacts/$vcardUid/link", request) { _, _ -> Unit }

    // --- Life events ---

    suspend fun listLifeEvents(
        entityId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<LifeEventsPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$LIFE_EVENTS_PATH".toHttpUrl().newBuilder()
        entityId?.let { urlBuilder.addQueryParameter("entity_id", it) }
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(LifeEventsPage::class.java).fromJson(body)
        }
    }

    suspend fun createLifeEvent(input: LifeEventInput): Result<LifeEvent> =
        executePost(LIFE_EVENTS_PATH, input) { _, body ->
            moshi.adapter(CreateLifeEventResponse::class.java).fromJson(body)?.lifeEvent
        }

    suspend fun updateLifeEvent(id: String, input: LifeEventInput): Result<LifeEvent> =
        executePut("$PLACEHOLDER_ORIGIN$LIFE_EVENTS_PATH/$id", input) { _, body ->
            moshi.adapter(LifeEvent::class.java).fromJson(body)
        }

    suspend fun deleteLifeEvent(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$LIFE_EVENTS_PATH/$id")

    // --- Gifts ---

    suspend fun listGifts(
        entityId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<GiftsPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$GIFTS_PATH".toHttpUrl().newBuilder()
        entityId?.let { urlBuilder.addQueryParameter("entity_id", it) }
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(GiftsPage::class.java).fromJson(body)
        }
    }

    suspend fun createGift(input: GiftInput): Result<Gift> =
        executePost(GIFTS_PATH, input) { _, body ->
            moshi.adapter(CreateGiftResponse::class.java).fromJson(body)?.gift
        }

    suspend fun updateGift(id: String, input: GiftInput): Result<Gift> =
        executePut("$PLACEHOLDER_ORIGIN$GIFTS_PATH/$id", input) { _, body ->
            moshi.adapter(Gift::class.java).fromJson(body)
        }

    suspend fun deleteGift(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$GIFTS_PATH/$id")

    // --- Preferences ---

    suspend fun listPreferences(
        entityId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<PreferencesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$PREFERENCES_PATH".toHttpUrl().newBuilder()
        entityId?.let { urlBuilder.addQueryParameter("entity_id", it) }
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(PreferencesPage::class.java).fromJson(body)
        }
    }

    suspend fun createPreference(input: PreferenceInput): Result<Preference> =
        executePost(PREFERENCES_PATH, input) { _, body ->
            moshi.adapter(CreatePreferenceResponse::class.java).fromJson(body)?.preference
        }

    suspend fun updatePreference(id: String, input: PreferenceInput): Result<Preference> =
        executePut("$PLACEHOLDER_ORIGIN$PREFERENCES_PATH/$id", input) { _, body ->
            moshi.adapter(Preference::class.java).fromJson(body)
        }

    suspend fun deletePreference(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$PREFERENCES_PATH/$id")

    // --- Conversation agenda ---

    suspend fun listConversationAgenda(
        entityId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<ConversationAgendaPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CONVERSATION_AGENDA_PATH".toHttpUrl().newBuilder()
        entityId?.let { urlBuilder.addQueryParameter("entity_id", it) }
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ConversationAgendaPage::class.java).fromJson(body)
        }
    }

    suspend fun createConversationAgenda(input: ConversationAgendaInput): Result<ConversationAgenda> =
        executePost(CONVERSATION_AGENDA_PATH, input) { _, body ->
            moshi.adapter(CreateConversationAgendaResponse::class.java).fromJson(body)?.conversationAgenda
        }

    suspend fun updateConversationAgenda(id: String, input: ConversationAgendaInput): Result<ConversationAgenda> =
        executePut("$PLACEHOLDER_ORIGIN$CONVERSATION_AGENDA_PATH/$id", input) { _, body ->
            moshi.adapter(ConversationAgenda::class.java).fromJson(body)
        }

    suspend fun deleteConversationAgenda(id: String): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$CONVERSATION_AGENDA_PATH/$id")

    /**
     * PATCH /api/v1/conversation-agenda/{id}/discuss — marks an item
     * discussed (M18: optionally linked to an existing activity). Sends
     * `{ activity_id }` when [activityId] is set and an empty object
     * otherwise — matching web's MarkDiscussedDialog.
     */
    suspend fun discussConversationAgenda(id: String, activityId: Int? = null): Result<ConversationAgenda> =
        executePatch(
            "$PLACEHOLDER_ORIGIN$CONVERSATION_AGENDA_PATH/$id/discuss",
            DiscussConversationAgendaInput(activityId = activityId),
        ) { _, body ->
            moshi.adapter(ConversationAgenda::class.java).fromJson(body)
        }

    // --- Contact merge ---

    suspend fun previewMerge(request: ContactMergeRequest): Result<ContactMergePreviewResponse> =
        executePost("$CONTACTS_PATH/merge/preview", request) { _, body ->
            moshi.adapter(ContactMergePreviewResponse::class.java).fromJson(body)
        }

    suspend fun commitMerge(request: ContactMergeRequest): Result<ContactRecordResponse> =
        executePost("$CONTACTS_PATH/merge", request) { _, body ->
            moshi.adapter(ContactMergeCommitResponse::class.java).fromJson(body)?.contact
        }

    // --- Bulk operations ---

    suspend fun bulkOperation(input: BulkContactOperationInput): Result<BulkOperationResult> =
        executePost("$CONTACTS_PATH/bulk", input) { _, body ->
            moshi.adapter(BulkOperationResult::class.java).fromJson(body)
        }

    // --- Import (CSV / VCF / JSContact) ---

    suspend fun uploadCsvImport(fileBytes: ByteArray, fileName: String): Result<ImportUploadResponse> =
        executeMultipartUpload(
            "$CONTACTS_PATH/import/upload",
            fieldName = "file",
            fileName = fileName,
            mediaType = "application/octet-stream",
            fileBytes = fileBytes,
        ) { _, body ->
            moshi.adapter(ImportUploadResponse::class.java).fromJson(body)
        }

    suspend fun uploadVcfImport(fileBytes: ByteArray, fileName: String): Result<ImportPreviewResponse> =
        executeMultipartUpload(
            "$CONTACTS_PATH/import/vcf/upload",
            fieldName = "file",
            fileName = fileName,
            mediaType = "application/octet-stream",
            fileBytes = fileBytes,
        ) { _, body ->
            moshi.adapter(ImportPreviewResponse::class.java).fromJson(body)
        }

    suspend fun previewCsvImport(request: ImportConfirmRequest): Result<ImportPreviewResponse> =
        executePost("$CONTACTS_PATH/import/preview", request) { _, body ->
            moshi.adapter(ImportPreviewResponse::class.java).fromJson(body)
        }

    suspend fun confirmImport(request: ImportConfirmRequest): Result<ImportResult> =
        executePost("$CONTACTS_PATH/import/confirm", request) { _, body ->
            moshi.adapter(ImportResult::class.java).fromJson(body)
        }

    /** M9 item 4: confirms a VCF import session — same request/response shape as [confirmImport]. */
    suspend fun confirmVcfImport(request: ImportConfirmRequest): Result<ImportResult> =
        executePost("$CONTACTS_PATH/import/vcf/confirm", request) { _, body ->
            moshi.adapter(ImportResult::class.java).fromJson(body)
        }

    /**
     * T96: starts a records-based import session from a batch of neutral
     * Card/CRM records (the device-contacts import path produces these via
     * DeviceContactMapper.toInput). Runs the same preview pipeline as a VCF
     * upload — validation, server-side duplicate detection with a merge diff,
     * within-batch duplicate detection — and is confirmed via
     * [confirmVcfImport].
     */
    suspend fun uploadImportRecords(records: List<ContactRecordInput>): Result<ImportPreviewResponse> =
        executePost("$CONTACTS_PATH/import/records", ImportRecordsRequest(records)) { _, body ->
            moshi.adapter(ImportPreviewResponse::class.java).fromJson(body)
        }

    // M15: contact sharing (P1) — the backend endpoints have served web since
    // P1 shipped; this closes the missing Android client surface (the ticket's
    // 7-endpoint diff against ApiClient).

    /** GET /api/v1/contact-shares/incoming — shares offered TO the current user, cursor-paginated. */
    suspend fun listIncomingContactShares(
        cursor: String? = null,
        limit: Int? = null,
    ): Result<ContactSharesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CONTACT_SHARES_PATH/incoming".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ContactSharesPage::class.java).fromJson(body)
        }
    }

    /** GET /api/v1/contact-shares/outgoing — shares the current user has sent, cursor-paginated. */
    suspend fun listOutgoingContactShares(
        cursor: String? = null,
        limit: Int? = null,
    ): Result<ContactSharesPage> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$CONTACT_SHARES_PATH/outgoing".toHttpUrl().newBuilder()
        cursor?.let { urlBuilder.addQueryParameter("cursor", it) }
        limit?.let { urlBuilder.addQueryParameter("limit", it.toString()) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(ContactSharesPage::class.java).fromJson(body)
        }
    }

    /** POST /api/v1/contact-shares — offers a filtered one-time copy; wrapped `{ message, contact_share }`. */
    suspend fun createContactShare(input: ContactShareInput): Result<ContactShare> =
        executePost(CONTACT_SHARES_PATH, input) { _, body ->
            moshi.adapter(CreateContactShareResponse::class.java).fromJson(body)?.contactShare
        }

    /**
     * POST /api/v1/contact-shares/{id}/accept — PREVIEW-ONLY: runs the share's
     * payload through the import pipeline and returns an ImportPreviewResponse
     * with duplicate matches. Does NOT change the share's status (that is
     * [confirmContactShare]'s job). The recipient picks add/update/skip per
     * row from this preview, then confirms.
     */
    suspend fun acceptContactShare(id: String): Result<ImportPreviewResponse> =
        executePostEmpty("$CONTACT_SHARES_PATH/$id/accept") { _, body ->
            moshi.adapter(ImportPreviewResponse::class.java).fromJson(body)
        }

    /** POST /api/v1/contact-shares/{id}/confirm — finalizes an accepted share with the chosen per-row actions. */
    suspend fun confirmContactShare(id: String, request: ImportConfirmRequest): Result<ImportResult> =
        executePost("$CONTACT_SHARES_PATH/$id/confirm", request) { _, body ->
            moshi.adapter(ImportResult::class.java).fromJson(body)
        }

    /** POST /api/v1/contact-shares/{id}/decline — flips a pending share to declined. */
    suspend fun declineContactShare(id: String): Result<Unit> =
        executePostEmpty("$CONTACT_SHARES_PATH/$id/decline") { _, _ -> Unit }

    /** GET /api/v1/users/directory — every other user (id + username), for the recipient picker. */
    suspend fun getUserDirectory(): Result<List<UserDirectoryEntry>> =
        executeGet("$PLACEHOLDER_ORIGIN$USERS_PATH/directory") { _, body ->
            moshi.adapter(UserDirectoryResponse::class.java).fromJson(body)?.users
        }

    // --- Admin user management (issue #348) — the five admin-group routes in
    // backend/routes/routes.go; the caller must be an admin or every one of
    // these 403s server-side.

    /** GET /api/v1/admin/users?page=&limit= — paginated, id-ASC user list. */
    suspend fun listUsers(page: Int = 1, limit: Int = 100): Result<AdminUsersListResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$ADMIN_USERS_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("page", page.toString())
        urlBuilder.addQueryParameter("limit", limit.toString())
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(AdminUsersListResponse::class.java).fromJson(body)
        }
    }

    /** POST /api/v1/admin/users — 201 with the bare AdminUser. */
    suspend fun createUser(input: AdminUserCreateInput): Result<AdminUser> =
        executePost(ADMIN_USERS_PATH, input) { _, body ->
            moshi.adapter(AdminUser::class.java).fromJson(body)
        }

    /** GET /api/v1/admin/users/{id} — a single user. */
    suspend fun getUser(id: Int): Result<AdminUser> =
        executeGet("$PLACEHOLDER_ORIGIN$ADMIN_USERS_PATH/$id") { _, body ->
            moshi.adapter(AdminUser::class.java).fromJson(body)
        }

    /** PATCH /api/v1/admin/users/{id} — the bare updated AdminUser. */
    suspend fun updateUser(id: Int, input: AdminUserUpdateInput): Result<AdminUser> =
        executePatch("$PLACEHOLDER_ORIGIN$ADMIN_USERS_PATH/$id", input) { _, body ->
            moshi.adapter(AdminUser::class.java).fromJson(body)
        }

    /** DELETE /api/v1/admin/users/{id} — removes the account and all its data (hard, T26). */
    suspend fun deleteUser(id: Int): Result<Unit> =
        executeDelete("$PLACEHOLDER_ORIGIN$ADMIN_USERS_PATH/$id")

    // --- Audit trail (M16, mirroring web's AuditPage over T18/T60's backend) ---

    /**
     * GET /audit — the caller's immutable event log, newest first, with
     * server-side entity_type/entity_id filters (the backend does all the
     * IDOR gating). [limit] is the window (default 100, max 500); the API has
     * no cursor, so "load more" re-fetches with a larger limit.
     */
    suspend fun getAuditEvents(
        entityType: String? = null,
        entityId: String? = null,
        limit: Int = 100,
    ): Result<AuditEventsResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$AUDIT_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("limit", limit.toString())
        entityType?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("entity_type", it) }
        entityId?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("entity_id", it) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(AuditEventsResponse::class.java).fromJson(body)
        }
    }

    /**
     * POST /audit/:id/undo — reverts a contact-update event from its before
     * snapshot. Backend rejects every other entity / a delete event with 400,
     * and 410 once the event has aged past AUDIT_RETENTION_DAYS.
     */
    suspend fun undoAuditEvent(id: Long): Result<AuditUndoResponse> =
        executePostEmpty("$AUDIT_PATH/$id/undo") { _, body ->
            moshi.adapter(AuditUndoResponse::class.java).fromJson(body)
        }

    // --- System events (issue #424 — the operational-event timeline) ---

    /**
     * GET /admin/system-events — the operational-event timeline, newest first,
     * with server-side component / severity / event_type / correlation_id
     * filters. Admin-only and instance-wide (not user-scoped). [limit] is the
     * window (default 100, max 500); the API has no cursor. [ids] is the
     * exact-row drill-down from an error-aggregation bucket (issue #426), sent
     * comma-separated; the backend caps it at 500.
     */
    suspend fun getSystemEvents(
        component: String? = null,
        severity: String? = null,
        eventType: String? = null,
        correlationId: String? = null,
        ids: List<Long>? = null,
        limit: Int = 100,
    ): Result<SystemEventsResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$ADMIN_SYSTEM_EVENTS_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("limit", limit.toString())
        component?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("component", it) }
        severity?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("severity", it) }
        eventType?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("event_type", it) }
        correlationId?.takeIf { it.isNotBlank() }
            ?.let { urlBuilder.addQueryParameter("correlation_id", it) }
        ids?.takeIf { it.isNotEmpty() }
            ?.let { urlBuilder.addQueryParameter("ids", it.joinToString(",")) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(SystemEventsResponse::class.java).fromJson(body)
        }
    }

    /**
     * GET /admin/error-aggregation — operational failures over a rolling window
     * bucketed by cause (issue #426): one bucket per (component, normalized
     * error) with its count, whether it recurs, first/last seen, a sample raw
     * error, and the exact system_events ids behind it. Admin-only,
     * instance-wide; the server derives it from the operational-event stream.
     */
    suspend fun getErrorAggregation(windowHours: Int = 24): Result<ErrorAggregationResponse> {
        val url = "$PLACEHOLDER_ORIGIN$ADMIN_ERROR_AGGREGATION_PATH".toHttpUrl().newBuilder()
            .addQueryParameter("window_hours", windowHours.toString())
            .build().toString()
        return executeGet(url) { _, body ->
            moshi.adapter(ErrorAggregationResponse::class.java).fromJson(body)
        }
    }

    /**
     * GET /admin/subsystem-health — the per-subsystem last-known-good state
     * (issue #427): for each subsystem its current status, last attempt /
     * success / failure, the first failure of the current incident, and the
     * consecutive-failure count. Admin-only, instance-wide, no parameters; the
     * server derives it from the operational-event stream.
     */
    suspend fun getSubsystemHealth(): Result<SubsystemHealthResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$ADMIN_SUBSYSTEM_HEALTH_PATH") { _, body ->
            moshi.adapter(SubsystemHealthResponse::class.java).fromJson(body)
        }

    /**
     * GET /admin/job-runs — background-job run history (issue #391), newest
     * first, with optional server-side job_name / result / since / until
     * filters. Admin-only, instance-wide. [limit] defaults to 100, max 500.
     */
    suspend fun getJobRuns(
        jobName: String? = null,
        result: String? = null,
        since: String? = null,
        until: String? = null,
        limit: Int = 100,
    ): Result<JobRunsResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$ADMIN_JOB_RUNS_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("limit", limit.toString())
        jobName?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("job_name", it) }
        result?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("result", it) }
        since?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("since", it) }
        until?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("until", it) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(JobRunsResponse::class.java).fromJson(body)
        }
    }

    /**
     * GET /admin/job-runs/health — the folded per-job run health (issue #391):
     * for each known background job its status, last run / success / failure,
     * the consecutive-failure run and its first-failure time, and an avg/max
     * duration trend. Admin-only, instance-wide, no parameters; the server
     * derives it from the job_runs history.
     */
    suspend fun getJobRunHealth(): Result<JobRunHealthResponse> =
        executeGet("$PLACEHOLDER_ORIGIN$ADMIN_JOB_RUNS_HEALTH_PATH") { _, body ->
            moshi.adapter(JobRunHealthResponse::class.java).fromJson(body)
        }

    // M14: the ego-centric network graph. `GET /graph/connections` (T10's
    // traversal) returns names already resolved and inverses already applied,
    // so this client needs no name resolution of its own — the design decision
    // that makes the mobile view a list rather than a force-graph.

    /**
     * GET /api/v1/graph/connections — every contact reachable from [from] (a
     * Contact.VCardUID, NOT a numeric id) within [depth] hops, each carrying
     * its resolved relation chain. [relation] accepts a canonical token or a
     * registry synonym (e.g. `"brother"` → `sibling_of`) and is passed through
     * verbatim — the server resolves it, an unresolvable value yields an empty
     * chain set rather than an error.
     */
    suspend fun getConnections(
        from: String,
        depth: Int? = null,
        relation: String? = null,
    ): Result<GraphConnectionsResponse> {
        val urlBuilder = "$PLACEHOLDER_ORIGIN$GRAPH_CONNECTIONS_PATH".toHttpUrl().newBuilder()
        urlBuilder.addQueryParameter("from", from)
        depth?.let { urlBuilder.addQueryParameter("depth", it.toString()) }
        relation?.takeIf { it.isNotBlank() }?.let { urlBuilder.addQueryParameter("relation", it) }
        return executeGet(urlBuilder.build().toString()) { _, body ->
            moshi.adapter(GraphConnectionsResponse::class.java).fromJson(body)
        }
    }

    private suspend fun <T> executeGet(
        url: String,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> {
        val request = Request.Builder().url(url).get().build()
        return execute(request, mapper)
    }

    private suspend fun <T> executePost(
        path: String,
        body: Any,
        idempotencyKey: String? = null,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> {
        val requestBuilder = Request.Builder()
            .url("$PLACEHOLDER_ORIGIN$path".toHttpUrl())
            .post(body.toJsonBody())
        // CON-04/ADR-0010 (issue #479): an outbox-sync retry must be recognized as one by the
        // server. The key is per logical operation, generated once when the row is recorded.
        if (idempotencyKey != null) {
            requestBuilder.addHeader(IDEMPOTENCY_KEY_HEADER, idempotencyKey)
        }
        return execute(requestBuilder.build(), mapper)
    }

    private suspend fun <T> executePostEmpty(
        path: String,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> {
        val request = Request.Builder()
            .url("$PLACEHOLDER_ORIGIN$path".toHttpUrl())
            .post(okhttp3.RequestBody.create(null, ByteArray(0)))
            .build()
        return execute(request, mapper)
    }

    private suspend fun <T> executePut(
        url: String,
        body: Any,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> {
        val request = Request.Builder()
            .url(url.toHttpUrl())
            .put(body.toJsonBody())
            .build()
        return execute(request, mapper)
    }

    private suspend fun <T> executePatch(
        url: String,
        body: Any,
        mapper: (okhttp3.Response, String) -> T? = { _, _ -> Unit as T },
    ): Result<T> {
        val request = Request.Builder()
            .url(url.toHttpUrl())
            .patch(body.toJsonBody())
            .build()
        return execute(request, mapper)
    }

    private suspend fun <T> executePatchEmpty(
        url: String,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> {
        val request = Request.Builder()
            .url(url.toHttpUrl())
            .patch(okhttp3.RequestBody.create(null, ByteArray(0)))
            .build()
        return execute(request, mapper)
    }

    private suspend fun <T> executeMultipartUpload(
        path: String,
        fieldName: String,
        fileName: String,
        mediaType: String,
        fileBytes: ByteArray,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> {
        val body = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart(fieldName, fileName, fileBytes.toRequestBody(mediaType.toMediaType()))
            .build()
        val request = Request.Builder()
            .url("$PLACEHOLDER_ORIGIN$path".toHttpUrl())
            .post(body)
            .build()
        return execute(request, mapper)
    }

    private fun Any.toJsonBody(): okhttp3.RequestBody =
        moshi.adapter<Any>(javaClass).toJson(this).toRequestBody(jsonMediaType)

    private suspend fun <T> executeDelete(
        url: String,
        mapper: (okhttp3.Response, String) -> T? = { _, _ -> Unit as T },
    ): Result<T> {
        val request = Request.Builder()
            .url(url.toHttpUrl())
            .delete()
            .build()
        return execute(request, mapper)
    }

    /**
     * GET for a binary/download response: reads the body as raw bytes rather
     * than a decoded string (a VCF export is a file, not JSON). Non-2xx still
     * parses the JSON error body via the normal path.
     */
    // detekt(TooGenericExceptionCaught): a request boundary must map every
    // failure to Result; IOException (network), Moshi (serialization) and any
    // other runtime error all funnel through toApiError(). Narrowing the catch
    // would drop failure modes.
    @Suppress("TooGenericExceptionCaught")
    private suspend fun executeGetBytes(url: String): Result<ByteArray> =
        withContext(Dispatchers.IO) {
            try {
                val request = Request.Builder().url(url.toHttpUrl()).get().build()
                val response = okHttpClient.newCall(request).execute()
                response.use {
                    val body = it.body
                    if (!it.isSuccessful) {
                        val errorBody = body?.string().orEmpty()
                        return@withContext Result.failure(parseError(it.code, errorBody))
                    }
                    val bytes = body?.bytes()
                    if (bytes == null || bytes.isEmpty()) {
                        Result.failure(ApiError.Parse("Empty response body"))
                    } else {
                        Result.success(bytes)
                    }
                }
            } catch (e: Exception) {
                Result.failure(e.toApiError())
            }
        }

    // detekt(TooGenericExceptionCaught): same request-boundary rationale as
    // executeGetBytes — every failure funnels through toApiError().
    @Suppress("TooGenericExceptionCaught")
    private suspend fun <T> execute(
        request: Request,
        mapper: (okhttp3.Response, String) -> T?,
    ): Result<T> = withContext(Dispatchers.IO) {
        try {
            val response = okHttpClient.newCall(request).execute()
            response.use {
                val body = it.body?.string().orEmpty()
                if (!it.isSuccessful) {
                    return@withContext Result.failure(parseError(it.code, body))
                }
                val mapped = mapper(it, body)
                if (mapped == null) {
                    Result.failure(ApiError.Parse("Empty response body"))
                } else {
                    Result.success(mapped)
                }
            }
        } catch (e: Exception) {
            Result.failure(e.toApiError())
        }
    }

    private fun parseError(code: Int, body: String): ApiError {
        val parsed = parseErrorDisplayMessage(body)
        val message = parsed?.takeIf { it.isNotBlank() } ?: body.ifBlank { "HTTP $code" }
        return if (code in 400..499) ApiError.Client(code, message) else ApiError.Server(code, message)
    }

    /**
     * Best-effort human-readable message from an error body. Prefers the
     * standard `{ error: { code, message, ... } }` envelope, then falls back to
     * the flat `{ message }` shape the account-lockout responses use
     * (`{ error, message, retry_after, retry_after_at }` — no envelope), so a
     * 429's lockout text is shown verbatim instead of the raw JSON (the web's
     * auth.ts 429 branch does the same). Each parse is independent — a flat
     * body's `error` is a String, which the envelope model can't decode, and
     * that must not mask the `message` fallback.
     */
    private fun parseErrorDisplayMessage(body: String): String? {
        val envelopeMessage = runCatching {
            moshi.adapter(BackendError::class.java).fromJson(body)?.error?.displayMessage
        }.getOrNull()
        envelopeMessage?.takeIf { it.isNotBlank() }?.let { return it }
        return runCatching {
            moshi.adapter(MessageResponse::class.java).fromJson(body)?.message
        }.getOrNull()?.takeIf { it.isNotBlank() }
    }

    private fun extractCookie(setCookieHeaders: List<String>, name: String): String? {
        for (header in setCookieHeaders) {
            val cookie = header.substringBefore(';').trim()
            val eq = cookie.indexOf('=')
            if (eq > 0 && cookie.substring(0, eq) == name) {
                return cookie.substring(eq + 1)
            }
        }
        return null
    }

    companion object {
        /**
         * CON-04/ADR-0010 (issue #459/#479): the client-supplied idempotency header. The outbox
         * sync worker sends each pending row's key on every attempt; the server replays the
         * stored outcome for a repeated (user, key) instead of running the handler twice.
         */
        const val IDEMPOTENCY_KEY_HEADER = "Idempotency-Key"

        /**
         * Every request is built against this placeholder origin and rewritten
         * onto the configured server by [BaseUrlInterceptor]. Public so Coil
         * (M5 §3.1) can build absolute URLs for the relative profile-photo
         * paths the backend returns — the same interceptors then rewrite the
         * host AND attach the auth header, since BaseUrl runs before Auth.
         */
        const val PLACEHOLDER_ORIGIN = "http://mycorrhizal.invalid"
        private const val API_V1 = "/api/v1"
        private const val LOGIN_PATH = "$API_V1/login"
        private const val LOGIN_2FA_PATH = "$API_V1/login/2fa"
        private const val TWO_FACTOR_PATH = "$API_V1/users/2fa"
        private const val AUTH_CONFIG_PATH = "$API_V1/auth/oidc/config"
        private const val ME_PATH = "$API_V1/users/me"
        private const val REGISTER_PATH = "$API_V1/register"
        private const val CHECK_PASSWORD_STRENGTH_PATH = "$API_V1/check-password-strength"
        private const val PASSWORD_RESET_REQUEST_PATH = "$API_V1/password-reset/request"
        private const val PASSWORD_RESET_CONFIRM_PATH = "$API_V1/password-reset/confirm"
        private const val USERS_PATH = "$API_V1/users"
        private const val ADMIN_USERS_PATH = "$API_V1/admin/users"
        private const val WEBHOOKS_PATH = "$API_V1/webhooks"
        private const val API_TOKENS_PATH = "$API_V1/api-tokens"
        private const val NOTIFICATIONS_CONFIG_PATH = "$API_V1/notifications/config"
        private const val NOTIFICATIONS_DEVICES_PATH = "$API_V1/notifications/devices"
        private const val CONTACTS_PATH = "$API_V1/contacts"
        private const val SEARCH_PATH = "$API_V1/search"
        private const val FIELD_DEFINITIONS_PATH = "$API_V1/field-definitions"
        private const val ACTIVITIES_PATH = "$API_V1/activities"
        private const val NOTES_PATH = "$API_V1/notes"
        private const val REMINDERS_PATH = "$API_V1/reminders"
        private const val REMINDER_COMPLETIONS_PATH = "$API_V1/reminder-completions"
        private const val CIRCLES_PATH = "$API_V1/circles"
        private const val TAGS_PATH = "$API_V1/tags"
        private const val HOUSEHOLDS_PATH = "$API_V1/households"
        private const val RELATIONSHIP_EDGES_PATH = "$API_V1/relationship-edges"
        private const val LIFE_EVENTS_PATH = "$API_V1/life-events"
        private const val GIFTS_PATH = "$API_V1/gifts"
        private const val PREFERENCES_PATH = "$API_V1/preferences"
        private const val CONVERSATION_AGENDA_PATH = "$API_V1/conversation-agenda"
        private const val CADENCE_POLICIES_PATH = "$API_V1/cadence-policies"
        private const val DASHBOARD_PATH = "$API_V1/dashboard"
        private const val REACH_OUT_SUGGESTIONS_PATH = "$API_V1/reach-out-suggestions"
        private const val EXPORT_VCF_PATH = "$API_V1/export/vcf"
        private const val CONTACT_SHARES_PATH = "$API_V1/contact-shares"
        private const val AUDIT_PATH = "$API_V1/audit"
        private const val ADMIN_SYSTEM_EVENTS_PATH = "$API_V1/admin/system-events"
        private const val ADMIN_SUBSYSTEM_HEALTH_PATH = "$API_V1/admin/subsystem-health"
        private const val ADMIN_ERROR_AGGREGATION_PATH = "$API_V1/admin/error-aggregation"
        private const val ADMIN_JOB_RUNS_PATH = "$API_V1/admin/job-runs"
        private const val ADMIN_JOB_RUNS_HEALTH_PATH = "$API_V1/admin/job-runs/health"
        private const val GRAPH_CONNECTIONS_PATH = "$API_V1/graph/connections"
        private const val EXTERNAL_IDENTITIES_PATH = "$API_V1/external-identities"
        private const val IMMICH_PATH = "$API_V1/immich"
        private const val PAPERLESS_PATH = "$API_V1/paperless"
        private const val SEAFILE_PATH = "$API_V1/seafile"
        private const val NEXTCLOUD_PATH = "$API_V1/nextcloud"
        private const val AUTH_COOKIE = "auth_token"
        /** The short-lived 2FA login challenge cookie (600s, httpOnly) — captured, never stored. */
        private const val TWO_FACTOR_COOKIE = "2fa_pending"
    }
}

/**
 * Successful login: the bearer JWT (captured from the httpOnly cookie) plus
 * profile prefs. For a 2FA account there is NO token yet:
 * [twoFactorRequired] is true and [pending2faCookie] holds the transient
 * `2fa_pending` challenge value that [ApiClient.complete2faLogin] must send
 * back. [pending2faCookie] must never be persisted.
 */
data class LoginResult(
    val token: String?,
    val language: String?,
    val dateFormat: String?,
    val twoFactorRequired: Boolean = false,
    val pending2faCookie: String? = null,
)

/**
 * A 2FA-management mutation whose success re-issued the caller's session
 * cookie: confirm/disable bump token_version server-side, which invalidates
 * the old JWT and mints a fresh `auth_token`. [value] is the parsed body and
 * [reissuedToken] the new bearer token (null when the server did not re-issue
 * one) — the data layer refreshes the stored token so the session survives.
 */
data class ReissuedTokenResult<T>(
    val value: T,
    val reissuedToken: String?,
)
