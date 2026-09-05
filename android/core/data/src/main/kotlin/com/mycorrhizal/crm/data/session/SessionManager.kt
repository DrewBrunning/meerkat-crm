package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.network.BaseUrlProvider
import com.mycorrhizal.crm.network.TokenProvider
import kotlinx.coroutines.flow.Flow

/**
 * Persists the bearer token. Implementations decide where the secret lives
 * (EncryptedSharedPreferences in production, in-memory in tests). The JWT is
 * a credential — it must never be logged.
 */
interface TokenStorage {
    suspend fun save(token: String)
    suspend fun load(): String?
    suspend fun clear()
}

/**
 * Persists non-secret session preferences (server URL, profile, language).
 * Plain DataStore is fine here — none of these are credentials.
 */
interface SessionPrefsStorage {
    suspend fun save(serverUrl: String?)
    suspend fun loadServerUrl(): String?
    suspend fun clear()
}

/**
 * Issue #385: wipes locally-cached user data (the Room mirror, cached images)
 * when a session ends. Runs inside [SessionManager.clearSession] so logout and
 * account-removal (invalid-token) paths both purge offline PII from the device.
 * Default no-op keeps `DefaultSessionManager` a plain JVM object for tests.
 */
interface SessionDataCleaner {
    suspend fun clear()
}

/** [SessionDataCleaner] that does nothing — the unit-test default. */
object NoopSessionDataCleaner : SessionDataCleaner {
    override suspend fun clear() = Unit
}

/**
 * Central session holder. Implements [TokenProvider] and [BaseUrlProvider]
 * from an in-memory cache so the synchronous OkHttp interceptors never touch
 * disk. The cache is hydrated at startup (see AppSessionManager).
 */
interface SessionManager : TokenProvider, BaseUrlProvider {
    fun observeSession(): Flow<SessionState>
    suspend fun serverUrl(): String?
    suspend fun token(): String?

    /** Persist a configured server origin before first authentication. */
    suspend fun setServerUrl(serverUrl: String)

    /** Persist a full session (server URL + bearer token + profile). */
    suspend fun setSession(serverUrl: String, token: String, state: SessionState)

    /**
     * Suspends until the persisted session has been hydrated into memory
     * (the async [DefaultSessionManager.init] at startup). Callers that must
     * read [serverUrl] or write a session before any user interaction — the
     * M5 OIDC cold-start deep link — await this first, or they'd read a null
     * URL and race the hydration write (review-pass fix).
     */
    suspend fun awaitHydrated()

    /** Merge profile details (userId, admin, language, …) into the session. */
    suspend fun setProfile(profile: SessionState)

    /**
     * Replace the stored bearer token in place, keeping the rest of the
     * session (server URL + profile) untouched. Used when the server re-issues
     * the session token after a token_version bump (2FA confirm/disable) so
     * the current session survives the mutation instead of 401ing on the next
     * request.
     */
    suspend fun setToken(token: String)

    /** Drop the session entirely (token + prefs). */
    suspend fun clearSession()
}
