package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.domain.repository.SessionState
import app.cash.turbine.test
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DefaultSessionManagerTest {

    private fun manager(): Pair<DefaultSessionManager, FakeTokenStorage> {
        val tokenStorage = FakeTokenStorage()
        val manager = DefaultSessionManager(tokenStorage, FakeSessionPrefsStorage())
        return manager to tokenStorage
    }

    @Test
    fun `bearerToken is empty before init`() {
        val (manager, _) = manager()
        assertNull(manager.bearerToken())
    }

    @Test
    fun `init hydrates the token from storage`() = runTest {
        val (manager, tokenStorage) = manager()
        tokenStorage.stored = "stored-jwt"
        manager.init()

        assertEquals("stored-jwt", manager.bearerToken())
    }

    @Test
    fun `setSession persists token and flips isLoggedIn`() = runTest {
        val (manager, tokenStorage) = manager()
        manager.init()
        manager.setServerUrl("https://crm.example.com")
        manager.setSession(
            serverUrl = "https://crm.example.com",
            token = "jwt-1",
            state = SessionState(userId = 7, username = "alice"),
        )

        assertEquals("jwt-1", manager.bearerToken())
        assertEquals("jwt-1", tokenStorage.stored)
        assertEquals("https://crm.example.com", manager.baseUrl())
        val state = manager.observeSession().first()
        assertTrue(state.isLoggedIn)
        assertEquals(7, state.userId)
    }

    @Test
    fun `clearSession removes token and resets state`() = runTest {
        val (manager, tokenStorage) = manager()
        manager.setSession(
            serverUrl = "https://crm.example.com",
            token = "jwt-1",
            state = SessionState(userId = 7, username = "alice"),
        )
        manager.clearSession()

        assertNull(manager.bearerToken())
        assertNull(tokenStorage.stored)
        assertFalse(manager.observeSession().first().isLoggedIn)
    }

    @Test
    fun `clearSession wipes cached user data through the session data cleaner`() = runTest {
        val tokenStorage = FakeTokenStorage()
        val cleaner = RecordingSessionDataCleaner()
        val manager = DefaultSessionManager(tokenStorage, FakeSessionPrefsStorage(), cleaner)
        manager.setSession("https://crm.example.com", "jwt-1", SessionState(userId = 7))

        manager.clearSession()

        assertEquals(1, cleaner.clearCount)
    }

    @Test
    fun `clearSession does not require a session data cleaner`() = runTest {
        val manager = DefaultSessionManager(FakeTokenStorage(), FakeSessionPrefsStorage())
        manager.setSession("https://crm.example.com", "jwt-1", SessionState(userId = 7))

        manager.clearSession()

        assertNull(manager.bearerToken())
    }

    private class RecordingSessionDataCleaner : SessionDataCleaner {
        var clearCount = 0
        override suspend fun clear() {
            clearCount++
        }
    }

    @Test
    fun `setServerUrl persists the origin`() = runTest {
        val (manager, _) = manager()
        manager.setServerUrl("https://beta.example.com")

        assertEquals("https://beta.example.com", manager.baseUrl())
        assertEquals("https://beta.example.com", manager.serverUrl())
    }

    @Test
    fun `setProfile merges profile without clearing login`() = runTest {
        val (manager, _) = manager()
        manager.setSession("https://crm.example.com", "jwt", SessionState(username = "alice"))
        manager.setProfile(SessionState(isAdmin = true, language = "de"))

        val state = manager.observeSession().first()
        assertTrue(state.isLoggedIn)
        assertTrue(state.isAdmin)
        assertEquals("de", state.language)
        assertEquals("alice", state.username)
    }

    // Issue #814: a token_version bump (2FA confirm/disable) re-issues the
    // session as a fresh bearer token. setToken swaps it in place — the
    // session stays logged in and the server URL/profile are untouched.
    @Test
    fun `setToken replaces the token in place without disturbing the session`() = runTest {
        val (manager, tokenStorage) = manager()
        manager.setSession(
            serverUrl = "https://crm.example.com",
            token = "old-jwt",
            state = SessionState(userId = 7, username = "alice"),
        )

        manager.setToken("reissued-jwt")

        assertEquals("reissued-jwt", manager.bearerToken())
        assertEquals("reissued-jwt", tokenStorage.stored)
        assertEquals("https://crm.example.com", manager.baseUrl())
        val state = manager.observeSession().first()
        assertTrue(state.isLoggedIn)
        assertEquals(7, state.userId)
        assertEquals("alice", state.username)
    }

    // M5 §5: a cold-start OIDC deep link must await hydration before reading
    // the server URL / writing the session — otherwise it races the async
    // startup init() (review-pass fix).
    @Test
    fun `awaitHydrated suspends until init completes`() = runTest {
        val (manager, tokenStorage) = manager()
        tokenStorage.stored = "stored-jwt"

        var hydrated = false
        // The async job's only observable effect is `hydrated` flipping to
        // true once awaitHydrated returns; the Deferred itself is never read.
        async { manager.awaitHydrated(); hydrated = true }

        // Not yet hydrated: init hasn't run, await is still suspended.
        advanceUntilIdle()
        assertFalse(hydrated)

        manager.init()
        advanceUntilIdle()

        assertTrue(hydrated)
        assertEquals("stored-jwt", manager.bearerToken())
    }

    // Issue #678: the session state machine must walk the full lifecycle —
    // logged-out → authenticated → (401) → logged-out → re-authenticated —
    // emitting the right SessionState at every step, so the auth-flow branch
    // in the app is driven by real state and no stale authed UI survives a
    // cleared session.
    @Test
    fun `session walks the full lifecycle state machine`() = runTest {
        val (manager, _) = manager()

        manager.observeSession().test {
            // logged-out (initial)
            assertEquals(SessionState(), awaitItem())

            // authenticated
            manager.setSession(
                serverUrl = "https://crm.example.com",
                token = "jwt-1",
                state = SessionState(userId = 7, username = "alice"),
            )
            val authenticated = awaitItem()
            assertTrue(authenticated.isLoggedIn)
            assertEquals(7, authenticated.userId)
            assertEquals("alice", authenticated.username)

            // (401) → logged-out
            manager.clearSession()
            val loggedOut = awaitItem()
            assertFalse(loggedOut.isLoggedIn)
            assertNull(loggedOut.userId)
            assertFalse("no stale username survives logout", loggedOut.username != null)

            // re-authenticated
            manager.setSession(
                serverUrl = "https://crm.example.com",
                token = "jwt-2",
                state = SessionState(userId = 7, username = "alice"),
            )
            val reAuthenticated = awaitItem()
            assertTrue(reAuthenticated.isLoggedIn)
            assertEquals("jwt-2", manager.bearerToken())
            assertEquals("alice", reAuthenticated.username)
        }
    }

    // Issue #678: process-death restore — a fresh manager instance (a new
    // process) must hydrate the persisted token and surface the same
    // logged-in state the old instance held. Only the token + server URL are
    // persisted; the profile fields (userId/username/admin) are refetched from
    // the server after restore (AuthRepositoryImpl re-derives them on login).
    @Test
    fun `a fresh instance restores the session after process death`() = runTest {
        val tokenStorage = FakeTokenStorage()
        val prefsStorage = FakeSessionPrefsStorage()
        val first = DefaultSessionManager(tokenStorage, prefsStorage)
        first.setSession(
            serverUrl = "https://crm.example.com",
            token = "jwt-1",
            state = SessionState(userId = 7, username = "alice"),
        )

        val restarted = DefaultSessionManager(tokenStorage, prefsStorage)
        restarted.init()

        assertEquals("jwt-1", restarted.bearerToken())
        assertEquals("https://crm.example.com", restarted.baseUrl())
        val state = restarted.observeSession().first()
        assertTrue(state.isLoggedIn)
    }
}
