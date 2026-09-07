package com.mycorrhizal.crm

import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.AppLockState
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.ServerCompatibilityRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.model.AppVersion
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

    private fun sessionManager(flow: MutableStateFlow<SessionState>): SessionManager =
        mockk<SessionManager> {
            every { observeSession() } returns flow
        }

    private fun loggedInState(): SessionState = SessionState(
        serverUrl = "https://crm.example.com",
        isLoggedIn = true,
        userId = 7,
        username = "alice",
    )

    private fun vm(
        sessionManager: SessionManager,
        repo: ServerCompatibilityRepository,
        authRepository: AuthRepository = mockk(),
    ): MainViewModel = MainViewModel(
        sessionManager,
        appLockController(),
        repo,
        authRepository,
    )

    @Test
    fun `logged-out session is the initial state`() = runTest(mainDispatcherRule.testDispatcher) {
        val sessionManager = sessionManager(MutableStateFlow(SessionState()))

        val viewModel = vm(sessionManager, compatibilityRepository(Result.success(ServerHealth())))
        advanceUntilIdle()

        val state = viewModel.session.value
        assertFalse(state.isLoggedIn)
        assertTrue(state.userId == null)
        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
        assertNull(viewModel.serverVersion.value)
    }

    @Test
    fun `a logged-in session is surfaced to the session flow`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)

        val viewModel = vm(sessionManager, compatibilityRepository(Result.success(ServerHealth())))
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        val state = viewModel.session.value
        assertTrue(state.isLoggedIn)
        assertEquals(7, state.userId)
        assertEquals("alice", state.username)
    }

    // Issue #722: the app-lock gate state is surfaced for the root branch.
    @Test
    fun `the app-lock gate state is surfaced to the root`() = runTest(mainDispatcherRule.testDispatcher) {
        val lock = MutableStateFlow(AppLockState.Resolving)
        val sessionManager = sessionManager(MutableStateFlow(SessionState(isLoggedIn = true)))
        val controller = mockk<AppLockController> {
            every { this@mockk.state } returns lock
        }

        val viewModel = MainViewModel(sessionManager, controller, compatibilityRepository(Result.success(ServerHealth())), mockk())
        advanceUntilIdle()
        assertEquals(AppLockState.Resolving, viewModel.appLockState.value)

        lock.value = AppLockState.Locked
        advanceUntilIdle()
        assertEquals(AppLockState.Locked, viewModel.appLockState.value)
    }

    // --- Issues #528 + #692: client/server compatibility --------------------

    @Test
    fun `server floor above client version forces the update gate`() = runTest(mainDispatcherRule.testDispatcher) {
        // The debug/test client versionName is 0.1.0 (see the build-logic
        // defaults); a floor above it must force the update screen.
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(
            CompatibilityGate.ForceUpdate(requiredVersion = "0.6.0"),
            viewModel.compatibilityGate.value,
        )
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `no declared floor keeps a below-server client compatible`() = runTest(mainDispatcherRule.testDispatcher) {
        // The policy's default posture: an old client keeps working against a
        // new server until the server explicitly declares a floor.
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(version = "0.6.10", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
        assertEquals(AppVersion(0, 6, 10), viewModel.serverVersion.value)
    }

    @Test
    fun `a pre-baseline server blocks the whole tree as server too old`() = runTest(mainDispatcherRule.testDispatcher) {
        // Issue #692: a reachable server reporting 0.5.3 predates the app's
        // v0.6.0 baseline — refusing the whole surface beats failing on every
        // screen. (The debug client 0.1.0 is "older" than 0.5.3 numerically, so
        // the compatibility resolver calls it compatible; the baseline gate is
        // what refuses it.)
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(version = "0.5.3", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.ServerTooOld, viewModel.compatibilityGate.value)
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
        assertEquals(AppVersion(0, 5, 3), viewModel.serverVersion.value)
    }

    @Test
    fun `a pre-baseline server blocks even before a session exists`() = runTest(mainDispatcherRule.testDispatcher) {
        // App start with a stored server URL but no session: the resolved
        // server is still too old, and the root refuses the auth screen too.
        val flow = MutableStateFlow(SessionState(serverUrl = "https://crm.example.com"))
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(version = "0.4.0", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        assertFalse(viewModel.session.value.isLoggedIn)
        assertEquals(CompatibilityGate.ServerTooOld, viewModel.compatibilityGate.value)
    }

    @Test
    fun `a server floor above the client blocks login before a session exists`() = runTest(mainDispatcherRule.testDispatcher) {
        // Issue #692: the force-update gate now raises pre-login, because the
        // server would refuse authentication anyway.
        val flow = MutableStateFlow(SessionState(serverUrl = "https://crm.example.com"))
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        assertFalse(viewModel.session.value.isLoggedIn)
        assertEquals(CompatibilityGate.ForceUpdate("0.6.0"), viewModel.compatibilityGate.value)
    }

    @Test
    fun `a pre-login blocking gate can be dismissed to reach the auth screen`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState(serverUrl = "https://crm.example.com"))
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()
        assertEquals(CompatibilityGate.ForceUpdate("0.6.0"), viewModel.compatibilityGate.value)

        viewModel.dismissPreLoginGate()
        advanceUntilIdle()
        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
    }

    @Test
    fun `dismissing the pre-login gate is a no-op once a session exists`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()
        flow.value = loggedInState()
        advanceUntilIdle()
        assertEquals(CompatibilityGate.ForceUpdate("0.6.0"), viewModel.compatibilityGate.value)

        viewModel.dismissPreLoginGate()
        advanceUntilIdle()
        assertEquals(CompatibilityGate.ForceUpdate("0.6.0"), viewModel.compatibilityGate.value)
    }

    @Test
    fun `switching to a supported server clears a pre-login blocking gate`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState(serverUrl = "https://old.example.com"))
        val sessionManager = sessionManager(flow)
        // The mock answers per current URL: old.example.com declares a floor
        // above the client; the replacement server does not.
        var result: Result<ServerHealth> = Result.success(
            ServerHealth(minClientVersion = "0.9.0", apiContractVersion = "v1"),
        )
        val repo = mockk<ServerCompatibilityRepository> {
            coEvery { getServerHealth() } answers { result }
        }

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()
        assertEquals(CompatibilityGate.ForceUpdate("0.9.0"), viewModel.compatibilityGate.value)

        result = Result.success(ServerHealth(version = "0.6.10", apiContractVersion = "v1"))
        flow.value = SessionState(serverUrl = "https://new.example.com")
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
    }

    @Test
    fun `unreachable or malformed health fails open`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        // A network error must never brick the app into a force-update screen.
        val repo = compatibilityRepository(Result.failure(RuntimeException("connection refused")))

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
        assertNull(viewModel.serverVersion.value)
    }

    @Test
    fun `an unparseable server version fails open`() = runTest(mainDispatcherRule.testDispatcher) {
        // An unstamped "dev" server build cannot be compared — fail open.
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(version = "dev", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()
        flow.value = loggedInState()
        advanceUntilIdle()

        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
        assertNull(viewModel.serverVersion.value)
    }

    @Test
    fun `logging out clears a set force-update gate`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(
            Result.success(ServerHealth(minClientVersion = "0.6.0", apiContractVersion = "v1")),
        )

        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()
        flow.value = loggedInState()
        advanceUntilIdle()
        assertEquals(CompatibilityGate.ForceUpdate("0.6.0"), viewModel.compatibilityGate.value)

        // Logging out resets the gate so the auth screen returns (the URL stays
        // stored per #723, but the same-URL gate is not re-raised until the
        // next login edge).
        flow.value = SessionState(serverUrl = "https://crm.example.com")
        advanceUntilIdle()
        assertEquals(CompatibilityGate.NotRequired, viewModel.compatibilityGate.value)
        assertNull(viewModel.serverOutdatedNoticeVersion.value)
    }

    @Test
    fun `the check runs when the server url changes and on each login edge`() = runTest(mainDispatcherRule.testDispatcher) {
        val flow = MutableStateFlow(SessionState())
        val sessionManager = sessionManager(flow)
        val repo = compatibilityRepository(Result.success(ServerHealth(minClientVersion = "0.6.0")))
        val viewModel = vm(sessionManager, repo)
        advanceUntilIdle()

        flow.value = loggedInState()
        advanceUntilIdle()
        flow.value = flow.value.copy(serverUrl = "https://other.example.com")
        advanceUntilIdle()
        flow.value = SessionState(serverUrl = "https://other.example.com")
        advanceUntilIdle()
        flow.value = loggedInState().copy(serverUrl = "https://other.example.com")
        advanceUntilIdle()

        // URL change (2) + the second login edge (1) against the same URL.
        coVerify(exactly = 3) { repo.getServerHealth() }
    }

    @Test
    fun `logout on the force-update screen ends the session`() = runTest(mainDispatcherRule.testDispatcher) {
        val authRepository = mockk<AuthRepository>()
        coEvery { authRepository.logout() } returns Unit

        val sessionManager = sessionManager(MutableStateFlow(loggedInState()))
        val viewModel = vm(sessionManager, compatibilityRepository(Result.success(ServerHealth())), authRepository)
        advanceUntilIdle()

        viewModel.logout()
        advanceUntilIdle()
        coVerify(exactly = 1) { authRepository.logout() }
    }
}
