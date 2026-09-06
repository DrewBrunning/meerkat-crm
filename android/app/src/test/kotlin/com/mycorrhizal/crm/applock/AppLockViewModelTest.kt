package com.mycorrhizal.crm.applock

import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.testing.MainDispatcherRule
import com.mycorrhizal.crm.ui.R
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import io.mockk.verify
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class AppLockViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private class Harness(
        val viewModel: AppLockViewModel,
        val controller: AppLockController,
        val authRepository: AuthRepository,
    )

    private fun harness(
        session: SessionState = SessionState(isLoggedIn = true, username = "alice"),
        canAuthenticate: Boolean = true,
    ): Harness {
        val controller = mockk<AppLockController>(relaxed = true)
        val authRepository = mockk<AuthRepository>(relaxed = true)
        coEvery { authRepository.logout() } returns Unit
        val capabilities = mockk<LocalAuthCapabilities> {
            every { canEnableLocalAuth() } returns canAuthenticate
        }
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns MutableStateFlow(session)
        }
        val viewModel = AppLockViewModel(controller, authRepository, capabilities, sessionManager)
        return Harness(viewModel, controller, authRepository)
    }

    @Test
    fun `the session username and device capability are surfaced`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness(session = SessionState(isLoggedIn = true, username = "alice"))
        advanceUntilIdle()

        assertEquals("alice", h.viewModel.uiState.value.username)
        assertTrue(h.viewModel.uiState.value.canAuthenticate)
    }

    @Test
    fun `an unsupported device reports it cannot authenticate`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness(canAuthenticate = false)
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.canAuthenticate)
    }

    // Verify #1: a successful local check clears the gate.
    @Test
    fun `a successful local auth unlocks the session`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.onAuthStarted()
        h.viewModel.onAuthResult(AppLockAuthOutcome.Success)

        verify { h.controller.onUserAuthenticated() }
    }

    // Verify #1: cancelling the prompt leaves the app locked (no unlock, no error).
    @Test
    fun `a cancelled prompt leaves the app locked without an error`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.onAuthStarted()
        h.viewModel.onAuthResult(AppLockAuthOutcome.Cancelled)

        verify(exactly = 0) { h.controller.onUserAuthenticated() }
        assertNull(h.viewModel.uiState.value.errorRes)
        assertFalse("a cancel must re-enable the retry button", h.viewModel.uiState.value.isUnlocking)
    }

    @Test
    fun `an OS error is surfaced on the lock screen`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.onAuthStarted()
        h.viewModel.onAuthResult(AppLockAuthOutcome.Error)

        assertEquals(R.string.app_lock_auth_failed, h.viewModel.uiState.value.errorRes)
        assertFalse(h.viewModel.uiState.value.isUnlocking)
        verify(exactly = 0) { h.controller.onUserAuthenticated() }
    }

    @Test
    fun `an unavailable device shows the unsupported message`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness(canAuthenticate = false)
        advanceUntilIdle()

        h.viewModel.onAuthResult(AppLockAuthOutcome.NotAvailable)

        assertEquals(R.string.app_lock_unsupported, h.viewModel.uiState.value.errorRes)
    }

    @Test
    fun `onAuthStarted marks the unlock in progress and clears any prior error`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()
        h.viewModel.onAuthResult(AppLockAuthOutcome.Error)

        h.viewModel.onAuthStarted()

        assertTrue(h.viewModel.uiState.value.isUnlocking)
        assertNull(h.viewModel.uiState.value.errorRes)
    }

    @Test
    fun `onErrorShown clears the transient error`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()
        h.viewModel.onAuthResult(AppLockAuthOutcome.Error)

        h.viewModel.onErrorShown()

        assertNull(h.viewModel.uiState.value.errorRes)
    }

    @Test
    fun `logout ends the session`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.onLogout()
        advanceUntilIdle()

        coVerify { h.authRepository.logout() }
    }

    // A logout while an unlock attempt is in flight is ignored (single in-flight action).
    @Test
    fun `logout while unlocking is ignored`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        advanceUntilIdle()

        h.viewModel.onAuthStarted()
        h.viewModel.onLogout()

        coVerify(exactly = 0) { h.authRepository.logout() }
    }
}
