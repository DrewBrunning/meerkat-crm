package com.mycorrhizal.crm.data.auth

import com.mycorrhizal.crm.data.session.DeviceGrantTokenStorage
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.BiometricEnrollmentStatus
import com.mycorrhizal.crm.domain.repository.DeviceGrantCreated
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.model.network.DeviceGrantCreateResponse
import com.mycorrhizal.crm.network.ApiClient
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DeviceGrantManagerTest {

    private class Harness(
        val api: ApiClient = mockk(),
        val storage: DeviceGrantTokenStorage = mockk(),
        val settings: LocalAuthSettingsRepository = mockk(),
        val sessionManager: SessionManager = mockk(),
    ) {
        val manager = DeviceGrantManager(api, storage, settings, sessionManager)
    }

    @Test
    fun `enroll stores the grant and marks the device enrolled`() = runTest {
        val h = Harness()
        coEvery { h.api.createDeviceGrant("Pixel") } returns
            Result.success(DeviceGrantCreateResponse(id = 7, label = "Pixel", token = "grant-token"))
        coEvery { h.storage.save("grant-token", 7L) } returns Unit
        coEvery { h.settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.ENROLLED) } returns Unit

        val result = h.manager.enroll("Pixel")

        assertTrue(result.isSuccess)
        coVerify { h.storage.save("grant-token", 7L) }
        coVerify { h.settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.ENROLLED) }
    }

    @Test
    fun `enroll propagates a server failure and stores nothing`() = runTest {
        val h = Harness()
        coEvery { h.api.createDeviceGrant(any()) } returns Result.failure(Exception("network"))

        val result = h.manager.enroll("Pixel")

        assertTrue(result.isFailure)
        coVerify(exactly = 0) { h.storage.save(any(), any()) }
        coVerify(exactly = 0) { h.settings.setBiometricEnrollmentStatus(any()) }
    }

    @Test
    fun `refresh succeeds when a grant is stored and the exchange succeeds`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadToken() } returns "grant-token"
        coEvery { h.api.exchangeDeviceSession("grant-token") } returns Result.success("fresh-jwt")
        coEvery { h.sessionManager.setToken("fresh-jwt") } returns Unit

        val refreshed = h.manager.refreshSessionFromStoredGrant()

        assertTrue(refreshed)
        coVerify { h.sessionManager.setToken("fresh-jwt") }
    }

    @Test
    fun `refresh is a no-op when no grant is stored`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadToken() } returns null

        val refreshed = h.manager.refreshSessionFromStoredGrant()

        assertFalse(refreshed)
        coVerify(exactly = 0) { h.api.exchangeDeviceSession(any()) }
    }

    @Test
    fun `refresh falls back to clearing when the exchange fails`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadToken() } returns "grant-token"
        coEvery { h.api.exchangeDeviceSession(any()) } returns Result.failure(Exception("revoked"))

        val refreshed = h.manager.refreshSessionFromStoredGrant()

        assertFalse(refreshed)
    }

    @Test
    fun `remove enrollment revokes by stored grant id and clears local state`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadGrantId() } returns 7L
        coEvery { h.api.revokeDeviceGrant(7L) } returns Result.success(Unit)
        coEvery { h.storage.clear() } returns Unit
        coEvery { h.settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.OPTED_OUT) } returns Unit

        val result = h.manager.removeEnrollment()

        assertTrue(result.isSuccess)
        coVerify { h.api.revokeDeviceGrant(7L) }
        coVerify { h.storage.clear() }
        coVerify { h.settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.OPTED_OUT) }
    }

    @Test
    fun `remove enrollment falls back to revoke-all when the grant id is missing`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadGrantId() } returns null
        coEvery { h.api.revokeAllDeviceGrants() } returns Result.success(com.mycorrhizal.crm.model.network.RevokeAllDeviceGrantsResponse(1))
        coEvery { h.storage.clear() } returns Unit
        coEvery { h.settings.setBiometricEnrollmentStatus(any()) } returns Unit

        val result = h.manager.removeEnrollment()

        assertTrue(result.isSuccess)
        coVerify { h.api.revokeAllDeviceGrants() }
    }

    @Test
    fun `createDeviceGrant maps the one-time token to the domain type`() = runTest {
        val h = Harness()
        coEvery { h.api.createDeviceGrant("Tablet") } returns
            Result.success(DeviceGrantCreateResponse(id = 3, label = "Tablet", token = "tok"))

        val created = h.manager.createDeviceGrant("Tablet").getOrThrow()

        assertEquals(DeviceGrantCreated(id = 3, label = "Tablet", token = "tok"), created)
    }

    @Test
    fun `isEnrolled reflects a stored grant`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadToken() } returns "grant-token"
        assertTrue(h.manager.isEnrolled())

        coEvery { h.storage.loadToken() } returns null
        assertFalse(h.manager.isEnrolled())
    }

    @Test
    fun `storedGrantToken and enrollmentStatus delegate to their stores`() = runTest {
        val h = Harness()
        coEvery { h.storage.loadToken() } returns "grant-token"
        assertEquals("grant-token", h.manager.storedGrantToken())

        val statusFlow = MutableStateFlow(BiometricEnrollmentStatus.ENROLLED)
        coEvery { h.settings.biometricEnrollmentStatus() } returns statusFlow
        assertEquals(BiometricEnrollmentStatus.ENROLLED, h.manager.enrollmentStatus().first())
    }

    @Test
    fun `createDeviceGrant fails when the server returns no token`() = runTest {
        val h = Harness()
        coEvery { h.api.createDeviceGrant("Pixel") } returns
            Result.success(DeviceGrantCreateResponse(id = 1, token = null))

        val result = h.manager.createDeviceGrant("Pixel")

        assertTrue(result.isFailure)
    }

}
