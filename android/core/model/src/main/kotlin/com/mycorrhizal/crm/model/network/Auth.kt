package com.mycorrhizal.crm.model.network

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

/** POST /api/v1/login request body. Either [identifier] or legacy [email]. */
@JsonClass(generateAdapter = true)
data class LoginRequest(
    val identifier: String? = null,
    val email: String? = null,
    val password: String? = null,
)

/**
 * POST /api/v1/login success body (the auth_token JWT arrives as an httpOnly
 * cookie). Mirrors the OpenAPI `LoginResponse` + web `auth.ts`.
 */
@JsonClass(generateAdapter = true)
data class LoginResponse(
    val language: String? = null,
    @Json(name = "date_format") val dateFormat: String? = null,
    // N8 (#158/#179, Android #814): present and true when the account has 2FA
    // enabled. In that case NO session exists yet — the server set a short-lived
    // 2fa_pending cookie instead, and POST /login/2fa with a TOTP/recovery code
    // must complete the login.
    @Json(name = "two_factor_required") val twoFactorRequired: Boolean? = null,
)

// --- N8 two-factor auth (issue #158, web parity #814) ---

/** POST /api/v1/login/2fa request body — a 6-digit TOTP or XXXXX-XXXXX-XXXXX recovery code. */
@JsonClass(generateAdapter = true)
data class TwoFactorCodeInput(
    val code: String,
)

/** GET /api/v1/users/2fa/status — `{ enabled }`. */
@JsonClass(generateAdapter = true)
data class TwoFactorStatusResponse(
    val enabled: Boolean = false,
)

/**
 * POST /api/v1/users/2fa/setup — the pending TOTP secret and its otpauth://
 * URI (for the QR code). Plaintext, shown exactly once; 2FA is only enforced
 * once POST /users/2fa/confirm succeeds.
 */
@JsonClass(generateAdapter = true)
data class TwoFactorSetupResponse(
    val secret: String = "",
    @Json(name = "otpauth_url") val otpauthUrl: String = "",
)

/**
 * POST /api/v1/users/2fa/confirm and /recovery-codes/regenerate — the new
 * single-use recovery codes, returned plaintext exactly once.
 */
@JsonClass(generateAdapter = true)
data class TwoFactorConfirmResponse(
    val message: String? = null,
    @Json(name = "recovery_codes") val recoveryCodes: List<String> = emptyList(),
)

/**
 * GET /api/v1/users/me — the authenticated user. Mirrors the OpenAPI
 * AdminUserResponse + enabled_contact_fields (CurrentUserResponse allOf).
 */
@JsonClass(generateAdapter = true)
data class UserProfile(
    val id: Int = 0,
    val username: String? = null,
    val email: String? = null,
    val language: String? = null,
    @Json(name = "date_format") val dateFormat: String? = null,
    @Json(name = "is_admin") val isAdmin: Boolean = false,
    @Json(name = "created_at") val createdAt: String? = null,
    @Json(name = "updated_at") val updatedAt: String? = null,
    @Json(name = "enabled_contact_fields") val enabledContactFields: List<String>? = null,
    // T90: the "Me" contact pointer (users.self_contact_vcard_uid). Added for
    // M14's network screen, which defaults its "start from" picker to the
    // self contact when one is set; the backend backfills it lazily on every
    // /users/me, so this is reliably populated for pre-000018 accounts too.
    @Json(name = "self_contact_vcard_uid") val selfContactVCardUid: String? = null,
)

/**
 * GET /api/v1/auth/oidc/config — the one public, unauthenticated "what can a
 * client do here" surface: whether OIDC is enabled (+ a provider name hint,
 * unused by Android today — the SSO button is always shown), and whether
 * DISABLE_REGISTRATION is set server-side. RegisterScreen fetches this to
 * show a disabled notice up front instead of only finding out via the
 * eventual 403 on submit (Android testing feedback).
 */
@JsonClass(generateAdapter = true)
data class AuthConfig(
    val enabled: Boolean = false,
    @Json(name = "provider_name") val providerName: String? = null,
    @Json(name = "registration_disabled") val registrationDisabled: Boolean = false,
)

// --- M26: account creation + password reset (all public, rate-limited) ---

/** POST /register body — `language` optional (defaults server-side). */
@JsonClass(generateAdapter = true)
data class RegisterRequest(
    val username: String,
    val email: String,
    val password: String,
    val language: String? = null,
)

/** POST /check-password-strength body. */
@JsonClass(generateAdapter = true)
data class CheckPasswordStrengthRequest(
    val password: String,
)

/**
 * POST /check-password-strength response — a raw, unwrapped PasswordStrength
 * object. [isValid] is entropy >= 50 bits; [feedback] is a human-readable
 * reason; [score] is 0..4.
 */
@JsonClass(generateAdapter = true)
data class PasswordStrength(
    @Json(name = "is_valid") val isValid: Boolean = false,
    val entropy: Double? = null,
    val score: Int = 0,
    val feedback: String? = null,
    @Json(name = "min_entropy") val minEntropy: Double? = null,
    @Json(name = "char_set_size") val charSetSize: Int? = null,
    val length: Int? = null,
)

/** POST /password-reset/request body. */
@JsonClass(generateAdapter = true)
data class PasswordResetRequest(
    val email: String,
)

/** POST /password-reset/confirm body. */
@JsonClass(generateAdapter = true)
data class PasswordResetConfirmRequest(
    val token: String,
    val password: String,
)
