package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.LoginOutcome
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.model.network.AuthConfig
import com.mycorrhizal.crm.model.network.MessageResponse
import com.mycorrhizal.crm.model.network.PasswordStrength
import com.mycorrhizal.crm.model.network.TwoFactorConfirmResponse
import com.mycorrhizal.crm.model.network.TwoFactorSetupResponse
import com.mycorrhizal.crm.model.network.TwoFactorStatusResponse
import com.mycorrhizal.crm.model.network.UserProfile
import com.mycorrhizal.crm.network.ApiClient
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.network.toApiError
import kotlinx.coroutines.flow.Flow
import javax.inject.Inject

/**
 * Online-first auth: login exchanges credentials for a JWT (captured from the
 * httpOnly cookie), then fetches the profile to validate the token and learn
 * user metadata.
 */
class AuthRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
    private val sessionManager: SessionManager,
) : AuthRepository {

    /**
     * The transient `2fa_pending` challenge from the last 2FA step-1 login.
     * Held in memory ONLY, for the in-flight login between [login] and
     * [complete2faLogin] — never persisted (a credential by another name: it
     * proves a correct password was just typed).
     */
    private var pending2faCookie: String? = null

    override suspend fun login(identifier: String, password: String): Result<LoginOutcome> {
        val result = apiClient.login(identifier, password)
        val login = result.getOrElse { return Result.failure(it.toApiError()) }

        // N8 (#814): a 2FA account's password step returns no session — just a
        // `2fa_pending` challenge to exchange via complete2faLogin. Mirror web
        // auth.ts loginUser, which does not treat this as a completed login.
        if (login.twoFactorRequired) {
            val pending = login.pending2faCookie
            if (pending.isNullOrBlank()) {
                return Result.failure(ApiError.Parse("2FA required but no pending challenge was returned"))
            }
            pending2faCookie = pending
            return Result.success(LoginOutcome.TwoFactorRequired)
        }
        pending2faCookie = null

        val token = login.token
        if (token == null || token.isBlank()) {
            return Result.failure(ApiError.Parse("Login succeeded but no auth token was returned"))
        }

        return persistSessionWithProfileFetch(token)
            .map { LoginOutcome.SessionEstablished }
    }

    override suspend fun complete2faLogin(code: String): Result<Unit> {
        val pending = pending2faCookie
        if (pending.isNullOrBlank()) {
            // No in-flight challenge (login never ran, or a previous attempt
            // consumed/expired it) — the server would reject anyway.
            return Result.failure(ApiError.Client(401, "No pending two-factor login found. Please sign in again."))
        }

        val result = apiClient.complete2faLogin(code.trim(), pending)
        val login = result.getOrElse { error ->
            // A 401 means the challenge was consumed/expired/disabled — the
            // pending state is gone for good, so the caller must restart at
            // step 1 rather than retry the same code.
            val apiError = error as? ApiError
            if (apiError is ApiError.Client && apiError.code == 401) {
                pending2faCookie = null
            }
            return Result.failure(error.toApiError())
        }
        pending2faCookie = null

        val token = login.token
        if (token == null || token.isBlank()) {
            return Result.failure(ApiError.Parse("Login succeeded but no auth token was returned"))
        }

        return persistSessionWithProfileFetch(token)
    }

    override suspend fun loginWithApiToken(token: String): Result<Unit> {
        sessionManager.setSession(
            serverUrl = sessionManager.serverUrl().orEmpty(),
            token = token,
            state = SessionState(),
        )
        val profile = apiClient.currentUser().getOrElse { return Result.failure(it.toApiError()) }
        persistSession(token, profile)
        return Result.success(Unit)
    }

    override suspend fun getTwoFactorStatus(): Result<TwoFactorStatusResponse> =
        apiClient.getTwoFactorStatus().fold(
            onSuccess = { Result.success(it) },
            onFailure = { Result.failure(it.toApiError()) },
        )

    override suspend fun setupTwoFactor(): Result<TwoFactorSetupResponse> =
        apiClient.setupTwoFactor().fold(
            onSuccess = { Result.success(it) },
            onFailure = { Result.failure(it.toApiError()) },
        )

    override suspend fun confirmTwoFactor(code: String): Result<TwoFactorConfirmResponse> {
        val result = apiClient.confirmTwoFactor(code.trim())
        val body = result.getOrElse { return Result.failure(it.toApiError()) }
        // token_version was bumped: swap in the re-issued session token so this
        // session isn't invalidated by the very mutation that just succeeded.
        body.reissuedToken?.let { sessionManager.setToken(it) }
        return Result.success(body.value)
    }

    override suspend fun disableTwoFactor(code: String): Result<MessageResponse> {
        val result = apiClient.disableTwoFactor(code.trim())
        val body = result.getOrElse { return Result.failure(it.toApiError()) }
        body.reissuedToken?.let { sessionManager.setToken(it) }
        return Result.success(body.value)
    }

    override suspend fun regenerateRecoveryCodes(code: String): Result<TwoFactorConfirmResponse> {
        val result = apiClient.regenerateRecoveryCodes(code.trim())
        val body = result.getOrElse { return Result.failure(it.toApiError()) }
        body.reissuedToken?.let { sessionManager.setToken(it) }
        return Result.success(body.value)
    }

    override suspend fun getAuthConfig(): Result<AuthConfig> {
        val result = apiClient.getAuthConfig()
        return result.fold(
            onSuccess = { Result.success(it) },
            onFailure = { Result.failure(it.toApiError()) },
        )
    }

    override suspend fun register(username: String, email: String, password: String): Result<Unit> {
        val result = apiClient.register(username, email, password)
        return result.fold(
            onSuccess = { Result.success(Unit) },
            onFailure = { Result.failure(it.toApiError()) },
        )
    }

    override suspend fun checkPasswordStrength(password: String): Result<PasswordStrength> {
        val result = apiClient.checkPasswordStrength(password)
        return result.fold(
            onSuccess = { Result.success(it) },
            onFailure = { Result.failure(it.toApiError()) },
        )
    }

    override suspend fun requestPasswordReset(email: String): Result<String> {
        val result = apiClient.requestPasswordReset(email)
        return result.fold(
            onSuccess = { message -> Result.success(message?.message ?: "") },
            onFailure = { Result.failure(it.toApiError()) },
        )
    }

    override suspend fun confirmPasswordReset(token: String, password: String): Result<Unit> {
        val result = apiClient.confirmPasswordReset(token, password)
        return result.fold(
            onSuccess = { Result.success(Unit) },
            onFailure = { Result.failure(it.toApiError()) },
        )
    }

    override suspend fun fetchCurrentUser(): Result<UserProfile> = apiClient.currentUser()

    override suspend fun updateLanguage(language: String): Result<Unit> {
        val result = apiClient.updateLanguage(language)
        result.getOrElse { return Result.failure(it.toApiError()) }
        // Merge into the session so observeSession() re-emits and every screen
        // that reads SessionState.language picks up the change immediately.
        sessionManager.setProfile(SessionState(language = language))
        return Result.success(Unit)
    }

    override suspend fun updateDateFormat(dateFormat: String): Result<Unit> {
        val result = apiClient.updateDateFormat(dateFormat)
        result.getOrElse { return Result.failure(it.toApiError()) }
        sessionManager.setProfile(SessionState(dateFormat = dateFormat))
        return Result.success(Unit)
    }

    override suspend fun changePassword(currentPassword: String, newPassword: String): Result<Unit> {
        val result = apiClient.changePassword(currentPassword, newPassword)
        return result.fold(
            onSuccess = { Result.success(Unit) },
            onFailure = { Result.failure(it.toApiError()) },
        )
    }

    override suspend fun logout() {
        sessionManager.clearSession()
    }

    override fun observeSession(): Flow<SessionState> = sessionManager.observeSession()

    /**
     * The shared tail of both interactive-login steps ([login] and
     * [complete2faLogin]): persist the token, then fetch the profile. Returns
     * a failure Result when the profile fetch fails (the session is left set —
     * exactly the pre-existing [login] behaviour).
     */
    private suspend fun persistSessionWithProfileFetch(token: String): Result<Unit> {
        // Persist the token BEFORE the profile fetch: the OkHttp stack's
        // AuthInterceptor reads bearerToken() synchronously, so currentUser()
        // below would otherwise go out with no Authorization header (and come
        // back 401 "Authorization token required") — the password flow had this
        // exact ordering bug while loginWithApiToken, which sets the session
        // first, did not.
        sessionManager.setSession(
            serverUrl = sessionManager.serverUrl().orEmpty(),
            token = token,
            state = SessionState(),
        )

        val profile = apiClient.currentUser().getOrElse { return Result.failure(it.toApiError()) }
        persistSession(token, profile)
        return Result.success(Unit)
    }

    private suspend fun persistSession(token: String, profile: UserProfile) {
        sessionManager.setSession(
            serverUrl = sessionManager.serverUrl().orEmpty(),
            token = token,
            state = SessionState(
                userId = profile.id.takeIf { it != 0 },
                username = profile.username,
                isAdmin = profile.isAdmin,
                language = profile.language,
                dateFormat = profile.dateFormat,
            ),
        )
    }
}
