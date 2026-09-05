package com.mycorrhizal.crm.domain.repository

import com.mycorrhizal.crm.model.network.AuthConfig
import com.mycorrhizal.crm.model.network.MessageResponse
import com.mycorrhizal.crm.model.network.PasswordStrength
import com.mycorrhizal.crm.model.network.TwoFactorConfirmResponse
import com.mycorrhizal.crm.model.network.TwoFactorSetupResponse
import com.mycorrhizal.crm.model.network.TwoFactorStatusResponse
import com.mycorrhizal.crm.model.network.UserProfile
import kotlinx.coroutines.flow.Flow

/** Describes the current session state exposed to the UI. */
data class SessionState(
    val serverUrl: String? = null,
    val isLoggedIn: Boolean = false,
    val userId: Int? = null,
    val username: String? = null,
    val isAdmin: Boolean = false,
    val language: String? = null,
    val dateFormat: String? = null,
)

/**
 * Outcome of the password step of interactive login (mirrors web
 * `auth.ts loginUser`): for a 2FA account the password alone never mints a
 * session.
 */
sealed interface LoginOutcome {
    /** A real session was issued (no 2FA on the account). */
    data object SessionEstablished : LoginOutcome

    /**
     * The account has 2FA enabled — complete the login with a TOTP/recovery
     * code via [AuthRepository.complete2faLogin] before any session exists.
     */
    data object TwoFactorRequired : LoginOutcome
}

/**
 * Authentication and session management.
 */
interface AuthRepository {
    /**
     * Login with username/email + password. Returns [LoginOutcome] so the
     * caller can tell a real session from a 2FA challenge that still needs
     * [complete2faLogin]; on success the returned JWT is stored.
     */
    suspend fun login(identifier: String, password: String): Result<LoginOutcome>

    /**
     * Step 2 of interactive login for a 2FA account: exchanges the transient
     * `2fa_pending` challenge captured by the preceding [login] plus a TOTP or
     * recovery code for a real session (the same persist + profile-fetch path
     * [login] uses). The challenge value lives in memory only, for the
     * in-flight login. Failures keep their HTTP status: 400 invalid code,
     * 401 missing/expired challenge (which clears the pending state, so the
     * caller must restart at step 1), 429 account lockout.
     */
    suspend fun complete2faLogin(code: String): Result<Unit>

    /** Authenticate with a `mycorrhizal_` API token (no password; bypasses 2FA). */
    suspend fun loginWithApiToken(token: String): Result<Unit>

    // M26: account creation + password reset.

    /**
     * GET /auth/oidc/config — public. RegisterScreen calls this to show a
     * "registration disabled" notice up front rather than only via the
     * eventual 403 on submit.
     */
    suspend fun getAuthConfig(): Result<AuthConfig>

    /** POST /register — creates the account. Does NOT authenticate; call [login] after. */
    suspend fun register(username: String, email: String, password: String): Result<Unit>

    /** POST /check-password-strength — server-side entropy/score for the register form. */
    suspend fun checkPasswordStrength(password: String): Result<PasswordStrength>

    /** POST /password-reset/request — anti-enumeration: the same message for known and unknown emails. */
    suspend fun requestPasswordReset(email: String): Result<String>

    /** POST /password-reset/confirm — resets the password with the emailed token. */
    suspend fun confirmPasswordReset(token: String, password: String): Result<Unit>

    /** Fetch the current user profile from the server (validates the token). */
    suspend fun fetchCurrentUser(): Result<UserProfile>

    /** PATCH the server language pref and update the in-session profile so `observeSession()` re-emits. */
    suspend fun updateLanguage(language: String): Result<Unit>

    /** PATCH the server date-format pref and update the in-session profile so `observeSession()` re-emits. */
    suspend fun updateDateFormat(dateFormat: String): Result<Unit>

    /**
     * POST /users/change-password. On success the server bumps TokenVersion,
     * invalidating every JWT (including this session's), so the caller must
     * re-login — the web re-issues a cookie, bearer-token Android cannot.
     * A wrong current password surfaces the server's 400 message.
     */
    suspend fun changePassword(currentPassword: String, newPassword: String): Result<Unit>

    // --- N8 two-factor management (issue #158, web parity #814). ---
    // Authenticated `/users/2fa/*`; mirrors web `frontend/src/api/users.ts`.
    // Nothing 2FA-related is persisted on the device beyond the session token.

    /** GET /users/2fa/status — whether 2FA is enabled. */
    suspend fun getTwoFactorStatus(): Result<TwoFactorStatusResponse>

    /** POST /users/2fa/setup — mints a pending TOTP secret (409 if already enabled; 403 for OIDC accounts). */
    suspend fun setupTwoFactor(): Result<TwoFactorSetupResponse>

    /**
     * POST /users/2fa/confirm — enables 2FA and returns the ten one-time
     * recovery codes (plaintext, shown exactly once). Bumps token_version:
     * the re-issued session token is stored so the current session survives.
     */
    suspend fun confirmTwoFactor(code: String): Result<TwoFactorConfirmResponse>

    /**
     * POST /users/2fa/disable — disables 2FA with a live code. Also bumps
     * token_version; the re-issued session token is stored.
     */
    suspend fun disableTwoFactor(code: String): Result<MessageResponse>

    /**
     * POST /users/2fa/recovery-codes/regenerate — replaces unused codes with a
     * fresh set, returned plaintext exactly once (gated on a live TOTP code).
     */
    suspend fun regenerateRecoveryCodes(code: String): Result<TwoFactorConfirmResponse>

    /** Clear the stored session (token + cached prefs). */
    suspend fun logout()

    /** Current session state. */
    fun observeSession(): Flow<SessionState>
}
