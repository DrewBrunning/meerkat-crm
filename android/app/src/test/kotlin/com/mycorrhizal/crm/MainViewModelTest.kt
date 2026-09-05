package com.mycorrhizal.crm

import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.AppLockState
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class MainViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private fun appLockController(state: AppLockState = AppLockState.Resolving): AppLockController =
        mockk<AppLockController> {
            every { this@mockk.state } returns MutableStateFlow(state)
        }

    @Test
    fun `logged-out session is the initial state`() = runTest(mainDispatcherRule.testDispatcher) {
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns MutableStateFlow(SessionState())
        }

        val vm = MainViewModel(sessionManager, appLockController())
        advanceUntilIdle()

        val state = vm.session.value
        assertFalse(state.isLoggedIn)
        assertTrue(state.userId == null)
    }

    @Test
    fun `a logged-in session is surfaced to the session flow`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }

        val vm = MainViewModel(sessionManager, appLockController())
        advanceUntilIdle()

        flow.value = SessionState(
            serverUrl = "https://crm.example.com",
            isLoggedIn = true,
            userId = 7,
            username = "alice",
            isAdmin = true,
            language = "de",
        )
        advanceUntilIdle()

        val state = vm.session.value
        assertTrue(state.isLoggedIn)
        assertEquals(7, state.userId)
        assertEquals("alice", state.username)
        assertTrue(state.isAdmin)
        assertEquals("de", state.language)
    }

    // Issue #722: the app-lock gate state is surfaced for the root branch.
    @Test
    fun `the app-lock gate state is surfaced to the root`() = runTest(mainDispatcherRule.testDispatcher) {
        val lock = MutableStateFlow(AppLockState.Resolving)
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns MutableStateFlow(SessionState(isLoggedIn = true))
        }
        val controller = mockk<AppLockController> {
            every { this@mockk.state } returns lock
        }

        val vm = MainViewModel(sessionManager, controller)
        advanceUntilIdle()
        assertEquals(AppLockState.Resolving, vm.appLockState.value)

        lock.value = AppLockState.Locked
        advanceUntilIdle()
        assertEquals(AppLockState.Locked, vm.appLockState.value)
    }
}
