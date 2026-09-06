package com.mycorrhizal.crm

import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.AppLockState
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.ServerCompatibilityRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.model.network.ServerHealth
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
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

    private fun compatibilityRepository(result: Result<ServerHealth>): ServerCompatibilityRepository =
        mockk<ServerCompatibilityRepository> {
            coEvery { getServerHealth() } returns result
        }

    private fun loggedInState(): SessionState = SessionState(
        serverUrl = "https://crm.example.com",
        isLoggedIn = true,
        userId = 7,
        username = "alice",
    )

    @Test
    fun `logged-out session is the initial state`() = runTest(mainDispatcherRule.testDispatcher) {
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns MutableStateFlow(SessionState())
        }

        val vm = MainViewModel(
            sessionManager,
            appLockController(),
            compatibilityRepository(Result.success(ServerHealth())),
            mockk(),
        )
        advanceUntilIdle()

        val state = vm.session.value
        assertFalse(state.isLoggedIn)
        assertTrue(state.userId == null)
        assertEquals(CompatibilityGate.NotRequired, vm.compatibilityGate.value)
        assertNull(vm.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `a logged-in session is surfaced to the session flow`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }

        val vm = MainViewModel(
            sessionManager,
            appLockController(),
            compatibilityRepository(Result.success(ServerHealth())),
            mockk(),
        )
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        val state = vm.session.value
        assertTrue(state.isLoggedIn)
        assertEquals(7, state.userId)
        assertEquals("alice", state.username)
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

        val vm = MainViewModel(
            sessionManager,
            controller,
            compatibilityRepository(Result.success(ServerHealth())),
            mockk(),
        )
        advanceUntilIdle()
        assertEquals(AppLockState.Resolving, vm.appLockState.value)

        lock.value = AppLockState.Locked
        advanceUntilIdle()
        assertEquals(AppLockState.Locked, vm.appLockState.value)
    }

    // --- Issue #528: client/server compatibility, checked once per login edge ---

    @Test
    fun `server floor above client version forces the update gate`() = runTest(mainDispatcherRule.testDispatcher) {
        // The debug/test client versionName is 0.1.0 (see the build-logic
        // defaults); a floor above it must force the update screen.
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val vm = MainViewModel(sessionManager, appLockController(), repo, mockk())
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(
            CompatibilityGate.ForceUpdate(requiredVersion = "0.6.0"),
            vm.compatibilityGate.value,
        )
        assertNull(vm.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `no declared floor keeps a below-server client compatible`() = runTest(mainDispatcherRule.testDispatcher) {
        // The policy's default posture: an old client keeps working against a
        // new server until the server explicitly declares a floor.
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }
        val repo = compatibilityRepository(
            Result.success(ServerHealth(version = "0.6.10", apiContractVersion = "v1")),
        )

        val vm = MainViewModel(sessionManager, appLockController(), repo, mockk())
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, vm.compatibilityGate.value)
        assertNull(vm.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `server older than client raises the non-blocking notice`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }
        val repo = compatibilityRepository(
            Result.success(ServerHealth(version = "0.0.5", apiContractVersion = "v1")),
        )

        val vm = MainViewModel(sessionManager, appLockController(), repo, mockk())
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, vm.compatibilityGate.value)
        assertEquals("0.0.5", vm.serverOutdatedNoticeVersion.value)

        vm.onServerOutdatedNoticeDismissed()
        assertNull(vm.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `unreachable or malformed health fails open`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }
        // A network error must never brick the app into a force-update screen.
        val repo = compatibilityRepository(Result.failure(RuntimeException("connection refused")))

        val vm = MainViewModel(sessionManager, appLockController(), repo, mockk())
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, vm.compatibilityGate.value)
        assertNull(vm.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `logging out clears a set force-update gate`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val vm = MainViewModel(sessionManager, appLockController(), repo, mockk())
        advanceUntilIdle()
        flow.value = loggedInState()
        advanceUntilIdle()
        assertEquals(CompatibilityGate.ForceUpdate("0.6.0"), vm.compatibilityGate.value)

        // Logging out resets the gate so the next login starts clean.
        flow.value = SessionState(serverUrl = "https://crm.example.com")
        advanceUntilIdle()
        assertEquals(CompatibilityGate.NotRequired, vm.compatibilityGate.value)
        assertNull(vm.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `the check runs once per login edge and not per idle recomposition`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns flow
        }
        val repo = compatibilityRepository(Result.success(ServerHealth(minClientVersion = "0.6.0")))
        val vm = MainViewModel(sessionManager, appLockController(), repo, mockk())
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()
        flow.value = flow.value.copy(serverUrl = "https://other.example.com")
        advanceUntilIdle()
        flow.value = SessionState(serverUrl = "https://other.example.com")
        advanceUntilIdle()
        flow.value = loggedInState().copy(serverUrl = "https://other.example.com")
        advanceUntilIdle()

        // Logged in twice across distinct edges => exactly two checks.
        coVerify(exactly = 2) { repo.getServerHealth() }
    }

    @Test
    fun `logout on the force-update screen ends the session`() = runTest(mainDispatcherRule.testDispatcher) {
        val authRepository = mockk<AuthRepository>()
        coEvery { authRepository.logout() } returns Unit

        val sessionManager = mockk<SessionManager> {
            every { observeSession() } returns MutableStateFlow(loggedInState())
        }
        val vm = MainViewModel(
            sessionManager,
            appLockController(),
            compatibilityRepository(Result.success(ServerHealth())),
            authRepository,
        )
        advanceUntilIdle()

        vm.logout()
        advanceUntilIdle()
        coVerify(exactly = 1) { authRepository.logout() }
    }
}
