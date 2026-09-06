package com.mycorrhizal.crm.data.auth

import com.mycorrhizal.crm.data.session.DeviceGrantTokenStorage
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.BiometricEnrollmentStatus
import com.mycorrhizal.crm.domain.repository.DeviceGrantCreated
import com.mycorrhizal.crm.domain.repository.DeviceGrantRepository
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.network.ApiClient
import kotlinx.coroutines.flow.Flow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Issue #722: the client half of fully biometric login — enroll this device
 * (mint a server grant and store it under the encrypted envelope), exchange
 * it for a fresh session when the stored JWT has expired, and remove it. The
 * grant is a long-lived credential; the biometric gate in front of the
 * authenticated tree is what keeps it usable only by its owner (see the ADR
 * for the "phone = have, biometric = are, server sees one possession factor"
 * framing).
 */
@Singleton
class DeviceGrantManager @Inject constructor(
    private val api: ApiClient,
    private val storage: DeviceGrantTokenStorage,
    private val settings: LocalAuthSettingsRepository,
    private val sessionManager: SessionManager,
) : DeviceGrantRepository {

    /** The plaintext grant stored for this install, or null when not enrolled. */
    suspend fun storedGrantToken(): String? = storage.loadToken()

    /** Whether this install has an enrolled (stored) grant. */
    suspend fun isEnrolled(): Boolean = storage.loadToken() != null

    fun enrollmentStatus(): Flow<BiometricEnrollmentStatus> = settings.biometricEnrollmentStatus()

    /**
     * Enroll this device for biometric sign-in: mint a grant while the caller
     * is authenticated, store it securely, and mark the install ENROLLED. The
     * plaintext grant is only ever handled here, between the server response
     * and the encrypted store.
     */
    suspend fun enroll(label: String): Result<Unit> =
        createDeviceGrant(label).fold(
            onSuccess = { created ->
                storage.save(created.token, created.id)
                settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.ENROLLED)
                Result.success(Unit)
            },
            onFailure = { Result.failure(it) },
        )

    /**
     * Remove biometric sign-in from this device: revoke its server grant (the
     * lost-phone path revokes every grant if the id is somehow missing),
     * clear the local copy, and drop the status back to OPTED_OUT so the user
     * is not nagged at the next login — they can re-enroll from Settings.
     */
    suspend fun removeEnrollment(): Result<Unit> {
        val id = storage.loadGrantId()
        val revoke = if (id != null) {
            api.revokeDeviceGrant(id)
        } else {
            api.revokeAllDeviceGrants().map { Unit }
        }
        return revoke.fold(
            onSuccess = {
                storage.clear()
                settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.OPTED_OUT)
                Result.success(Unit)
            },
            onFailure = { Result.failure(it) },
        )
    }

    override suspend fun createDeviceGrant(label: String): Result<DeviceGrantCreated> =
        api.createDeviceGrant(label).mapCatching { response ->
            val token = response.token
            if (token == null || token.isBlank()) {
                error("Server did not return a device grant token")
            } else {
                DeviceGrantCreated(id = response.id, label = response.label.orEmpty(), token = token)
            }
        }

    override suspend fun exchangeDeviceSession(deviceToken: String): Result<Unit> =
        api.exchangeDeviceSession(deviceToken).fold(
            onSuccess = { freshToken ->
                sessionManager.setToken(freshToken)
                Result.success(Unit)
            },
            onFailure = { Result.failure(it) },
        )

    /**
     * Bridge past an expired session: if this install holds a grant, exchange
     * it for a fresh JWT. Returns true only when a stored grant existed AND
     * the exchange succeeded (the session is now valid again). Callers that
     * get false fall back to the normal clear-to-login path.
     */
    suspend fun refreshSessionFromStoredGrant(): Boolean {
        val token = storage.loadToken() ?: return false
        return exchangeDeviceSession(token).isSuccess
    }

    override suspend fun revokeAllDeviceGrants(): Result<Unit> =
        api.revokeAllDeviceGrants().map { Unit }
}
