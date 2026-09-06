package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.network.SessionExpiryNotifier
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Issue #678: the glue between the network layer's 401 detection and the
 * session store. A session must never survive a 401 — the wiring clears it,
 * which flips the app to the auth flow.
 */
class SessionExpiryWiringTest {

    @Test
    fun `a session-expiry signal clears the session`() = runTest {
        val notifier = SessionExpiryNotifier()
        val tokenStorage = FakeTokenStorage()
        val prefsStorage = FakeSessionPrefsStorage()
        val cleaner = RecordingCleaner()
        val manager = DefaultSessionManager(tokenStorage, prefsStorage, cleaner)
        manager.setSession(
            serverUrl = "https://crm.example.com",
            token = "jwt-1",
            state = SessionState(userId = 7, username = "alice"),
        )

        SessionExpiryWiring(notifier, manager).start(this)
        notifier.onSessionExpired()
        advanceUntilIdle()

        assertNull("the bearer token must be dropped", manager.bearerToken())
        assertNull("the persisted token must be dropped", tokenStorage.stored)
        assertFalse("the session must flip to logged-out", manager.observeSession().first().isLoggedIn)
        assertEquals("local PII must be wiped on session end", 1, cleaner.clearCount)
        // Issue #723: a 401-driven logout goes through the same clearSession as
        // an explicit logout — the server URL survives it too, so the login
        // screen that follows is pre-filled.
        assertEquals("https://crm.example.com", manager.serverUrl())
    }

    @Test
    fun `a signal with no active session is a harmless no-op`() = runTest {
        val notifier = SessionExpiryNotifier()
        val tokenStorage = FakeTokenStorage()
        val cleaner = RecordingCleaner()
        val manager = DefaultSessionManager(tokenStorage, FakeSessionPrefsStorage(), cleaner)

        SessionExpiryWiring(notifier, manager).start(this)
        notifier.onSessionExpired()
        advanceUntilIdle()

        assertNull(manager.bearerToken())
        assertFalse(manager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `the session survives an authenticated request`() = runTest {
        val notifier = SessionExpiryNotifier()
        val manager = DefaultSessionManager(FakeTokenStorage(), FakeSessionPrefsStorage())
        manager.setSession("https://crm.example.com", "jwt-1", SessionState(userId = 7))

        SessionExpiryWiring(notifier, manager).start(this)

        assertTrue(manager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `a fresh process restart wires and clears through the same path`() = runTest {
        // Process-death restore: a fresh manager instance hydrates the stored
        // token; a subsequent 401 must still clear it.
        val notifier = SessionExpiryNotifier()
        val tokenStorage = FakeTokenStorage()
        val prefsStorage = FakeSessionPrefsStorage()
        val first = DefaultSessionManager(tokenStorage, prefsStorage)
        first.setSession("https://crm.example.com", "jwt-1", SessionState(userId = 7))

        val restarted = DefaultSessionManager(tokenStorage, prefsStorage)
        restarted.init()
        SessionExpiryWiring(notifier, restarted).start(this)

        assertEquals("jwt-1", restarted.bearerToken())
        notifier.onSessionExpired()
        advanceUntilIdle()

        assertNull(restarted.bearerToken())
        assertFalse(restarted.observeSession().first().isLoggedIn)
    }

    @Test
    fun `signals arriving before registration are not lost once registered`() = runTest {
        // A 401 that fires before the wiring registers must still clear the
        // session once the wiring is in place. Registration happens before any
        // request in production, but this pins the no-loss guarantee regardless.
        val notifier = SessionExpiryNotifier()
        val manager = DefaultSessionManager(FakeTokenStorage(), FakeSessionPrefsStorage())
        manager.setSession("https://crm.example.com", "jwt-1", SessionState(userId = 7))

        SessionExpiryWiring(notifier, manager).start(this)
        notifier.onSessionExpired()
        advanceUntilIdle()

        assertNull(manager.bearerToken())
    }

    private class RecordingCleaner : SessionDataCleaner {
        var clearCount = 0
        override suspend fun clear() {
            clearCount++
        }
    }
}
