package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class DefaultAppLockControllerTest {

    /** In-memory [LocalAuthSettingsRepository] driven explicitly by the test. */
    private class FakeLocalAuthSettingsRepository : LocalAuthSettingsRepository {
        private val require = MutableStateFlow(false)
        private val delay = MutableStateFlow(AutoLockDelay.DEFAULT)

        override fun requireLocalAuth(): Flow<Boolean> = require
        override suspend fun setRequireLocalAuth(enabled: Boolean) { require.value = enabled }
        override fun autoLockDelay(): Flow<AutoLockDelay> = delay
        override suspend fun setAutoLockDelay(delay: AutoLockDelay) { this.delay.value = delay }
    }

    private class Harness(var now: Long = 0L) {
        val settings = FakeLocalAuthSettingsRepository()
        val tokenStorage = FakeTokenStorage()
        val sessionManager = DefaultSessionManager(tokenStorage, FakeSessionPrefsStorage())
        val controller = DefaultAppLockController(settings, sessionManager) { now }
    }

    private suspend fun DefaultSessionManager.login() {
        setSession(
            serverUrl = "https://crm.example.com",
            token = "jwt-1",
            state = SessionState(userId = 7, username = "alice"),
        )
    }

    /**
     * Runs a controller test. The controller's collectors never complete, so
     * they are started on their own scope sharing the runTest scheduler (and
     * cancelled when the test body ends) rather than on the test body's scope
     * or `backgroundScope`, which runTest would wait on or never drive.
     */
    private fun controllerTest(block: suspend TestScope.(Harness) -> Unit) = runTest {
        val harness = Harness()
        val scope = CoroutineScope(StandardTestDispatcher(testScheduler) + SupervisorJob())
        harness.controller.start(scope)
        try {
            block(harness)
        } finally {
            scope.cancel()
        }
    }

    // Issue #722: the gate state must never leak into the UI before the
    // preference + hydrated session have been read (Resolving = render nothing).
    @Test
    fun `state is resolving before the controller has observed anything`() {
        val h = Harness()
        assertEquals(AppLockState.Resolving, h.controller.state.value)
    }

    @Test
    fun `cold start with the app lock off never gates a persisted session`() = controllerTest { h ->
        h.tokenStorage.stored = "stored-jwt"
        h.settings.setRequireLocalAuth(false)

        h.sessionManager.init()
        advanceUntilIdle()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // Scope item 2 + verify #2: with the opt-in off (the default), cold start
    // behaves exactly as today.
    @Test
    fun `cold start with the app lock on gates a persisted session`() = controllerTest { h ->
        h.tokenStorage.stored = "stored-jwt"
        h.settings.setRequireLocalAuth(true)

        h.sessionManager.init()
        advanceUntilIdle()

        assertEquals(AppLockState.Locked, h.controller.state.value)
    }

    @Test
    fun `a logged-out start resolves unlocked`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)

        h.sessionManager.init()
        advanceUntilIdle()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // Scope item 2: an interactive login never locks — the user just
    // authenticated, and the gate only ever applies to a resumed session.
    @Test
    fun `an interactive login during a running session does not lock`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)

        h.sessionManager.init()
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)

        h.sessionManager.login()
        advanceUntilIdle()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // Scope item 2: backgrounding past the configured grace period re-gates.
    @Test
    fun `returning after the grace period re-locks an armed session`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)
        h.settings.setAutoLockDelay(AutoLockDelay.FIVE_MINUTES)

        h.sessionManager.init()
        // The controller must first observe the logged-out start (production:
        // an interactive login happens many frames after process start).
        advanceUntilIdle()
        h.sessionManager.login()
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.now = 1_000L + AutoLockDelay.FIVE_MINUTES.minutes * 60_000L + 1L
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Locked, h.controller.state.value)
    }

    // Scope item 2: "not on every resume" — a quick app-switch is exempt.
    @Test
    fun `returning within the grace period does not re-lock`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)
        h.settings.setAutoLockDelay(AutoLockDelay.FIVE_MINUTES)

        h.sessionManager.init()
        // The controller must first observe the logged-out start (production:
        // an interactive login happens many frames after process start).
        advanceUntilIdle()
        h.sessionManager.login()
        advanceUntilIdle()

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.now = 1_000L + 60_000L // well under five minutes
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    @Test
    fun `the immediately delay re-locks on any background return`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)
        h.settings.setAutoLockDelay(AutoLockDelay.IMMEDIATELY)

        h.sessionManager.init()
        // The controller must first observe the logged-out start (production:
        // an interactive login happens many frames after process start).
        advanceUntilIdle()
        h.sessionManager.login()
        advanceUntilIdle()

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.now = 1_001L
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Locked, h.controller.state.value)
    }

    // Verify #1: cancelling leaves the app locked — a Locked gate only clears
    // via a successful local auth or the session ending.
    @Test
    fun `a cold-start gate clears only on successful local auth`() = controllerTest { h ->
        h.tokenStorage.stored = "stored-jwt"
        h.settings.setRequireLocalAuth(true)

        h.sessionManager.init()
        advanceUntilIdle()
        assertEquals(AppLockState.Locked, h.controller.state.value)

        // A foreground event with no background history (the OS prompt path)
        // must not accidentally unlock the gate.
        h.controller.onAppForegrounded()
        assertEquals(AppLockState.Locked, h.controller.state.value)

        h.controller.onUserAuthenticated()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // The OS biometric prompt itself backgrounds the app on some OEMs — a
    // successful auth must clear that clock or it would immediately re-lock.
    @Test
    fun `successful auth clears the background clock`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)
        h.settings.setAutoLockDelay(AutoLockDelay.ONE_MINUTE)

        h.sessionManager.init()
        // The controller must first observe the logged-out start (production:
        // an interactive login happens many frames after process start).
        advanceUntilIdle()
        h.sessionManager.login()
        advanceUntilIdle()

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.controller.onUserAuthenticated()
        h.now = 1_000L + AutoLockDelay.ONE_MINUTE.minutes * 60_000L + 1L
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // Scope item 6 / #678: session expiry or an explicit logout clears the
    // gate so the next interactive login lands on the main tree, never on a
    // stale lock.
    @Test
    fun `session expiry unlocks a locked gate`() = controllerTest { h ->
        h.tokenStorage.stored = "stored-jwt"
        h.settings.setRequireLocalAuth(true)

        h.sessionManager.init()
        advanceUntilIdle()
        assertEquals(AppLockState.Locked, h.controller.state.value)

        h.sessionManager.clearSession()
        advanceUntilIdle()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // Scope item 6: an explicit logout keeps the preference (it is not
    // cleared), and a later login on the same install is armed again.
    @Test
    fun `logout keeps the opt-in preference armed`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(true)

        h.sessionManager.init()
        // The controller must first observe the logged-out start (production:
        // an interactive login happens many frames after process start).
        advanceUntilIdle()
        h.sessionManager.login()
        advanceUntilIdle()
        h.sessionManager.clearSession()
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)
        // The preference itself survives logout — a later login is armed again.
        assertTrue(h.settings.requireLocalAuth().first())

        h.sessionManager.login()
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // Scope item 3: enabling the setting mid-session does not lock the current
    // session — the user is already authenticated — but the next background
    // past the grace period applies it.
    @Test
    fun `enabling mid-session does not lock immediately but arms the next gate`() = controllerTest { h ->
        h.tokenStorage.stored = "stored-jwt"

        h.sessionManager.init()
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)

        h.settings.setRequireLocalAuth(true)
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.now = 1_000L + AutoLockDelay.FIVE_MINUTES.minutes * 60_000L + 1L
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Locked, h.controller.state.value)
    }

    // Disabling the setting disarms the gate entirely (and never locks the
    // session it is flipped in).
    @Test
    fun `disabling mid-session never locks and disarms future gates`() = controllerTest { h ->
        h.tokenStorage.stored = "stored-jwt"
        h.settings.setRequireLocalAuth(true)
        h.sessionManager.init()
        advanceUntilIdle()
        assertEquals(AppLockState.Locked, h.controller.state.value)

        h.controller.onUserAuthenticated()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)

        h.settings.setRequireLocalAuth(false)
        advanceUntilIdle()
        assertEquals(AppLockState.Unlocked, h.controller.state.value)

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.now = 1_000L + AutoLockDelay.ONE_HOUR.minutes * 60_000L + 1L
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

    // A session with the opt-in off (default installs) never locks, no matter
    // how long it sat in the background.
    @Test
    fun `a disabled session never locks after a long background`() = controllerTest { h ->
        h.settings.setRequireLocalAuth(false)
        h.sessionManager.init()
        // The controller must first observe the logged-out start (production:
        // an interactive login happens many frames after process start).
        advanceUntilIdle()
        h.sessionManager.login()
        advanceUntilIdle()

        h.now = 1_000L
        h.controller.onAppBackgrounded()
        h.now = 1_000L + AutoLockDelay.ONE_HOUR.minutes * 60_000L + 1L
        h.controller.onAppForegrounded()

        assertEquals(AppLockState.Unlocked, h.controller.state.value)
    }

}
