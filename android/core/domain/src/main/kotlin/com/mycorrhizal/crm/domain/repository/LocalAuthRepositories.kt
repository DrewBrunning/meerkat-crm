package com.mycorrhizal.crm.domain.repository

import kotlinx.coroutines.flow.Flow

/**
 * How long the app may sit in the background before a persisted session needs
 * the local (biometric / device-credential) check again (issue #722).
 * [IMMEDIATELY] locks on every background transition; the longer values allow
 * a grace period so a quick app-switch does not re-prompt on every resume.
 */
enum class AutoLockDelay(val minutes: Long) {
    IMMEDIATELY(0L),
    ONE_MINUTE(1L),
    FIVE_MINUTES(5L),
    FIFTEEN_MINUTES(15L),
    ONE_HOUR(60L);

    companion object {
        /** The delay applied the first time local auth is enabled. */
        val DEFAULT: AutoLockDelay = FIVE_MINUTES

        /** Unknown/absent persisted values fall back to [DEFAULT] rather than failing. */
        fun fromMinutes(minutes: Long): AutoLockDelay =
            entries.firstOrNull { it.minutes == minutes } ?: DEFAULT
    }
}

/**
 * Opt-in app-lock preference (issue #722, "Require biometric / device PIN to
 * open the app"). These are *preferences*, not credentials — the enabled flag
 * and the timeout width describe when the device's own secure gate is shown,
 * and are stored in a plain DataStore file. The secrets they gate (the stored
 * session JWT) stay in `EncryptedTokenStorage`; nothing sensitive lives here.
 *
 * Defaults match current behaviour exactly: [requireLocalAuth] is off, so an
 * existing install cold-starts straight into the authenticated tree with no
 * local check until the user opts in.
 */
interface LocalAuthSettingsRepository {
    /** Whether a biometric / device-credential check is required before the authenticated tree. Default `false`. */
    fun requireLocalAuth(): Flow<Boolean>

    suspend fun setRequireLocalAuth(enabled: Boolean)

    /** Background grace period before a persisted session is re-gated. Default [AutoLockDelay.DEFAULT]. */
    fun autoLockDelay(): Flow<AutoLockDelay>

    suspend fun setAutoLockDelay(delay: AutoLockDelay)

    /**
     * Whether the user has been asked to enroll this device for biometric
     * sign-in, and their answer ([BiometricEnrollmentStatus]). A preference,
     * not a credential — the grant itself lives in the device-grant store.
     */
    fun biometricEnrollmentStatus(): Flow<BiometricEnrollmentStatus>

    suspend fun setBiometricEnrollmentStatus(status: BiometricEnrollmentStatus)
}

/**
 * What the device can offer for the local gate (issue #722). Separated from
 * Android's `BiometricManager` so the Settings screen and the controller can
 * reason about capability without an Android dependency, and so tests can
 * fake the device posture.
 */
interface LocalAuthCapabilities {
    /**
     * Whether the user can actually complete the local check: a Class 3
     * (BIOMETRIC_STRONG) biometric is enrolled, or the device has a secure
     * lock screen whose credential can be used as the fallback. When false the
     * opt-in toggle must not be enableable (there is no way the gate could
     * ever open).
     */
    fun canEnableLocalAuth(): Boolean

    /** Whether a Class 3 biometric (not merely the device-credential fallback) is enrolled. */
    fun hasStrongBiometric(): Boolean
}

/**
 * Whether the user has been asked to enroll this device for biometric sign-in
 * (issue #722's fully-biometric-login layer), persisted on-device:
 *  - [UNASKED]: not asked yet — the login screen prompts again at the next
 *    username/password login.
 *  - [OPTED_OUT]: the user chose "never ask again" (or removed biometric
 *    sign-in) — no prompt until the app is reinstalled; enrollment is still
 *    available from Settings.
 *  - [ENROLLED]: a device grant is stored for this install.
 */
enum class BiometricEnrollmentStatus {
    UNASKED,
    OPTED_OUT,
    ENROLLED;

    companion object {
        fun fromName(name: String?): BiometricEnrollmentStatus =
            entries.firstOrNull { it.name == name } ?: UNASKED
    }
}

/**
 * Server half of fully biometric login (issue #722). A device grant is a
 * long-lived, revocable credential minted by the backend while the caller is
 * authenticated; [exchangeDeviceSession] proves possession of it and receives
 * a fresh session JWT. The grant itself never leaves the device after
 * enrollment (see the data-layer manager).
 */
interface DeviceGrantRepository {
    /**
     * POST /api/v1/auth/device/grants — enroll this device. Returns the
     * one-time plaintext grant, which the caller must store securely.
     */
    suspend fun createDeviceGrant(label: String): Result<DeviceGrantCreated>

    /**
     * POST /api/v1/auth/device/session — exchange possession of [deviceToken]
     * for a fresh session JWT. The returned token is persisted into the
     * session (same path a password login uses), so the caller only checks the
     * Result.
     */
    suspend fun exchangeDeviceSession(deviceToken: String): Result<Unit>

    /** POST /api/v1/auth/device/grants/revoke-all — the lost-phone path. */
    suspend fun revokeAllDeviceGrants(): Result<Unit>
}

/** A freshly-minted device grant (the plaintext appears exactly once). */
data class DeviceGrantCreated(
    val id: Long,
    val label: String,
    val token: String,
)
