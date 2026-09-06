package com.mycorrhizal.crm.enroll

import com.mycorrhizal.crm.data.auth.DeviceGrantManager
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.BiometricEnrollmentStatus
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class BiometricEnrollmentViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private class Harness(
        val viewModel: BiometricEnrollmentViewModel,
        val manager: DeviceGrantManager,
        val settings: LocalAuthSettingsRepository,
    )

    private fun harness(
        status: BiometricEnrollmentStatus = BiometricEnrollmentStatus.UNASKED,
        canAuthenticate: Boolean = true,
        loggedIn: Boolean = true,
    ): Harness {
        val manager = mockk<DeviceGrantManager>(relaxed = true)
        coEvery { manager.enroll(any()) } returns Result.success(Unit)
        val settings = mockk<LocalAuthSettingsRepository>()
        every { settings.biometricEnrollmentStatus() } returns MutableStateFlow(status)
        coEvery { settings.setBiometricEnrollmentStatus(any()) } returns Unit
        val capabilities = mockk<LocalAuthCapabilities> {
            every { canEnableLocalAuth() } returns canAuthenticate
        }
        val sessionManager = mockk<SessionManager>()
        coEvery { sessionManager.awaitHydrated() } returns Unit
        every { sessionManager.observeSession() } returns MutableStateFlow(SessionState(isLoggedIn = loggedIn))
        val viewModel = BiometricEnrollmentViewModel(manager, settings, capabilities, sessionManager)
        return Harness(viewModel, manager, settings)
    }

    // Issue #722: the prompt only shows for an undecided user on a capable,
    // logged-in device.
    @Test
    fun `the prompt is visible for an unasked user on a capable device`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        assertTrue(h.viewModel.uiState.value.visible)
    }

    @Test
    fun `the prompt is hidden once the user has decided or the device cannot authenticate`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val optedOut = harness(status = BiometricEnrollmentStatus.OPTED_OUT)
            advanceUntilIdle()
            assertFalse(optedOut.viewModel.uiState.value.visible)

            val alreadyEnrolled = harness(status = BiometricEnrollmentStatus.ENROLLED)
            advanceUntilIdle()
            assertFalse(alreadyEnrolled.viewModel.uiState.value.visible)

            val unsupported = harness(canAuthenticate = false)
            advanceUntilIdle()
            assertFalse(unsupported.viewModel.uiState.value.visible)
        }

    // A cold-start resume (logged-in without an interactive login in this
    // process) must never trigger the enrollment prompt — this VM only mounts
    // after an interactive login, but the guard stays for belt and braces.
    @Test
    fun `the prompt never shows while logged out`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness(loggedIn = false)
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.visible)
    }

    @Test
    fun `not now leaves the state unasked so the prompt returns next login`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.notNow()
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.visible)
        coVerify(exactly = 0) { h.settings.setBiometricEnrollmentStatus(any()) }
    }

    @Test
    fun `never ask again persists the opt-out`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.neverAskAgain()
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.visible)
        coVerify { h.settings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.OPTED_OUT) }
    }

    @Test
    fun `dismiss behaves like not now`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.dismiss()
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.visible)
        coVerify(exactly = 0) { h.settings.setBiometricEnrollmentStatus(any()) }
    }

    @Test
    fun `enroll mints a grant and dismisses`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.performEnroll("Pixel")

        coVerify { h.manager.enroll("Pixel") }
        assertFalse(h.viewModel.uiState.value.visible)
        assertFalse(h.viewModel.uiState.value.error)
        assertFalse(h.viewModel.uiState.value.isBusy)
    }

    @Test
    fun `a failed enrollment surfaces an error and stays visible`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.manager.enroll(any()) } returns Result.failure(Exception("network"))
        advanceUntilIdle()

        h.viewModel.performEnroll("Pixel")

        assertTrue(h.viewModel.uiState.value.error)
        assertTrue("an enrollment failure must not silently dismiss", h.viewModel.uiState.value.visible)
        assertFalse(h.viewModel.uiState.value.isBusy)
    }

}
