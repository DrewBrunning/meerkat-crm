package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.domain.repository.SessionState
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map

/**
 * In-memory-backed [SessionManager] used by unit tests and as the base for
 * the production Android implementation. Holds the JWT and server URL in
 * memory; persistence is delegated to the injected [TokenStorage] and
 * [SessionPrefsStorage] so the class itself is a plain JVM object.
 */
class DefaultSessionManager(
    private val tokenStorage: TokenStorage,
    private val prefsStorage: SessionPrefsStorage,
    private val localDataCleaner: SessionDataCleaner = NoopSessionDataCleaner,
) : SessionManager {

    private var cachedToken: String? = null
    private var cachedServerUrl: String? = null
    private val sessionState = MutableStateFlow(SessionState())
    private val hydrated = kotlinx.coroutines.CompletableDeferred<Unit>()

    /** Load both cached values and surface the initial session. */
    suspend fun init() {
        cachedToken = tokenStorage.load()
        cachedServerUrl = prefsStorage.loadServerUrl()
        refreshState()
        hydrated.complete(Unit)
    }

    override suspend fun awaitHydrated() {
        hydrated.await()
    }

    override fun bearerToken(): String? = cachedToken

    override fun baseUrl(): String = cachedServerUrl.orEmpty()

    override fun observeSession(): Flow<SessionState> = sessionState

    /** Raw server URL flow for callers that need the current value. */
    fun serverUrlFlow(): Flow<String?> = sessionState.map { it.serverUrl }

    override suspend fun serverUrl(): String? = cachedServerUrl

    override suspend fun token(): String? = cachedToken

    override suspend fun setServerUrl(serverUrl: String) {
        cachedServerUrl = serverUrl
        prefsStorage.save(serverUrl)
        sessionState.value = sessionState.value.copy(serverUrl = serverUrl)
    }

    override suspend fun setSession(serverUrl: String, token: String, state: SessionState) {
        cachedServerUrl = serverUrl
        cachedToken = token
        prefsStorage.save(serverUrl)
        tokenStorage.save(token)
        sessionState.value = state.copy(serverUrl = serverUrl, isLoggedIn = true)
    }

    override suspend fun setProfile(profile: SessionState) {
        val current = sessionState.value
        sessionState.value = current.copy(
            userId = profile.userId ?: current.userId,
            username = profile.username ?: current.username,
            isAdmin = profile.isAdmin,
            language = profile.language ?: current.language,
            dateFormat = profile.dateFormat ?: current.dateFormat,
        )
    }

    override suspend fun setToken(token: String) {
        cachedToken = token
        tokenStorage.save(token)
    }

    override suspend fun clearSession() {
        cachedToken = null
        cachedServerUrl = null
        tokenStorage.clear()
        prefsStorage.clear()
        // Issue #385: purge the offline PII mirror + image cache on logout /
        // account removal so a dropped session leaves no contact data on disk.
        localDataCleaner.clear()
        sessionState.value = SessionState()
    }

    private fun refreshState() {
        sessionState.value = SessionState(
            serverUrl = cachedServerUrl,
            isLoggedIn = !cachedToken.isNullOrBlank(),
        )
    }
}
