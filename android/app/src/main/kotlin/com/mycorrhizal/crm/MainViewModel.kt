package com.mycorrhizal.crm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.AppLockState
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.compat.Compatibility
import com.mycorrhizal.crm.domain.compat.CompatibilityResolver
import com.mycorrhizal.crm.domain.compat.ServerCapabilities
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.ServerCompatibilityRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.model.AppVersion
import com.mycorrhizal.crm.model.network.ServerHealth
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * The client/server compatibility decision that gates the whole root (issues
 * #528 + #692). A [ForceUpdate] or [ServerTooOld] renders a blocking screen in
 * front of every other surface; the default is [NotRequired] (compatible, or
 * the check has not yet resolved).
 */
sealed interface CompatibilityGate {
    data object NotRequired : CompatibilityGate

    /**
     * The server's declared `min_client_version` is above this client: the
     * server will no longer serve this build (issue #528). Rendered whether or
     * not a session exists — the server is known to refuse authentication, so
     * the auth screen is not worth showing until the user points at another
     * server or updates the app.
     */
    data class ForceUpdate(val requiredVersion: String) : CompatibilityGate

    /**
     * The server reports an older-than-baseline version (below 0.6.0, issue
     * #692): this app's whole authenticated surface expects the v0.6.0 API
     * contract, so the app refuses to show any UI and asks the operator to
     * upgrade the server instead of failing on every screen.
     */
    data object ServerTooOld : CompatibilityGate
}

/**
 * Root application state: whether a session exists (drives login vs main tree),
 * whether that session must pass the local app-lock gate first (issue #722),
 * and the client/server compatibility decision for the configured server
 * (issues #528 + #692).
 *
 * ## When the compatibility check runs
 *
 * The session's server version is resolved once per session from /health — the
 * single source of truth (issue #528) — and kept here as [serverVersion] for
 * the per-feature gates. A check runs:
 *
 *  - whenever the configured server URL changes (which includes app start with
 *    a stored URL, whether or not a session exists), and
 *  - again on every login edge (a fresh session against the same server).
 *
 * A ForceUpdate/ServerTooOld outcome is a blocking root surface even while
 * logged out: the server has declared it will refuse this build, so the auth
 * screen is not worth showing. Logging out clears the gate (the auth screen
 * returns so a different server can be entered); a cold start with the same
 * stored URL re-checks and re-raises it.
 *
 * The check is intentionally NOT a gate on the main tree — it runs alongside
 * it and, if it resolves to a blocking state, swaps the root. That keeps a cold
 * start with an unreachable /health fully functional (fail open), per
 * docs/client-compatibility-policy.md.
 */
@HiltViewModel
class MainViewModel @Inject constructor(
    sessionManager: SessionManager,
    appLockController: AppLockController,
    private val serverCompatibilityRepository: ServerCompatibilityRepository,
    private val authRepository: AuthRepository,
) : ViewModel() {
    val session: StateFlow<SessionState> = sessionManager.observeSession()
        .stateIn(viewModelScope, SharingStarted.Eagerly, SessionState())

    val appLockState: StateFlow<AppLockState> = appLockController.state
        .stateIn(viewModelScope, SharingStarted.Eagerly, AppLockState.Resolving)

    /** The current compatibility gate. See [CompatibilityGate]. */
    private val _compatibilityGate = MutableStateFlow<CompatibilityGate>(CompatibilityGate.NotRequired)
    val compatibilityGate: StateFlow<CompatibilityGate> = _compatibilityGate.asStateFlow()

    /** The server version while the current session's server is older than this
     *  app (the non-blocking "server could be upgraded" notice, issue #528).
     *  Null = no notice. */
    private val _serverOutdatedNoticeVersion = MutableStateFlow<String?>(null)
    val serverOutdatedNoticeVersion: StateFlow<String?> = _serverOutdatedNoticeVersion.asStateFlow()

    /**
     * The parsed server version the current check resolved (issue #692). Null
     * until the check has run, and when /health was unreachable/garbled — the
     * fail-open input for the per-feature capability gates.
     */
    private val _serverVersion = MutableStateFlow<AppVersion?>(null)
    val serverVersion: StateFlow<AppVersion?> = _serverVersion.asStateFlow()

    private var compatibilityCheck: Job? = null
    private var checkedServerUrl: String? = null
    private var wasLoggedIn = false

    init {
        // Issues #528 + #692: resolve the server contract against whatever
        // server URL the session knows, re-checking when the URL changes or a
        // fresh login happens (see the class doc).
        viewModelScope.launch {
            sessionManager.observeSession().collect { state ->
                val serverUrl = state.serverUrl?.trim().orEmpty()
                val loginEdge = state.isLoggedIn && !wasLoggedIn
                val logoutEdge = !state.isLoggedIn && wasLoggedIn
                wasLoggedIn = state.isLoggedIn

                when {
                    serverUrl.isEmpty() -> {
                        // No server configured: nothing to check against.
                        checkedServerUrl = null
                        resetCompatibilityState()
                    }
                    serverUrl != checkedServerUrl -> {
                        // A (possibly new) server to resolve — includes app start
                        // with a stored URL, logged in or not.
                        checkedServerUrl = serverUrl
                        runCheck()
                    }
                    loginEdge -> {
                        // Fresh session on the same server: re-resolve once.
                        runCheck()
                    }
                    logoutEdge -> {
                        // Ending a session returns to the auth screen without
                        // re-raising a blocking gate for the same URL — the next
                        // login edge re-checks.
                        compatibilityCheck?.cancel()
                        resetCompatibilityState()
                    }
                    // Idle emissions on the same URL/state: nothing to do.
                }
            }
        }
    }

    private fun runCheck() {
        compatibilityCheck?.cancel()
        resetCompatibilityState()
        compatibilityCheck = viewModelScope.launch { resolveCompatibility() }
    }

    /** The "server is older than this app" notice was dismissed. */
    fun onServerOutdatedNoticeDismissed() {
        _serverOutdatedNoticeVersion.value = null
    }

    /** Ends the session (used by the force-update screen's escape hatch). */
    fun logout() {
        viewModelScope.launch { authRepository.logout() }
    }

    /**
     * Dismisses a blocking pre-login gate (no session exists yet) so the auth
     * screen returns and a different server can be entered. Deliberately a
     * no-op once a session exists — the force-update escape there is logout.
     */
    fun dismissPreLoginGate() {
        if (!session.value.isLoggedIn) {
            resetCompatibilityState()
        }
    }

    private fun resetCompatibilityState() {
        _compatibilityGate.value = CompatibilityGate.NotRequired
        _serverOutdatedNoticeVersion.value = null
        _serverVersion.value = null
    }

    /**
     * Fetches the server's compatibility contract once and resolves the three
     * states plus the baseline gate. Fail open is guaranteed by
     * [CompatibilityResolver] and [ServerCapabilities] — a network error, an
     * absent server, or a malformed body all resolve to compatible, so this can
     * never strand the user on a stale blocking screen.
     */
    private suspend fun resolveCompatibility() {
        val server = serverCompatibilityRepository.getServerHealth().getOrNull()
        val outcome = resolveCompatibilityOutcome(BuildConfig.VERSION_NAME, server)
        _compatibilityGate.value = outcome.gate
        _serverOutdatedNoticeVersion.value = outcome.noticeVersion
        _serverVersion.value = outcome.serverVersion
    }
}

/**
 * The outcome of one compatibility resolution: the blocking gate (if any), the
 * non-blocking server-outdated notice version (if any), and the parsed server
 * version that feeds the per-feature capability gates. Pure so the whole
 * decision — including the fail-open and baseline branches — is unit-testable
 * without a build config or network.
 */
internal data class CompatibilityOutcome(
    val gate: CompatibilityGate,
    val noticeVersion: String?,
    val serverVersion: AppVersion?,
)

/**
 * Reduces a fetched /health contract against [clientVersionName] into the root
 * decision. Fail open is guaranteed by [CompatibilityResolver] and
 * [ServerCapabilities] — a null server (unreachable/malformed /health) resolves
 * to [CompatibilityGate.NotRequired] and a null server version, so this can
 * never strand the user on a stale blocking screen.
 *
 * The "server too old" rule (issue #692): a server whose version parses to
 * something below the 0.6.0 baseline blocks the whole tree
 * ([CompatibilityGate.ServerTooOld]) instead of degrading feature-by-feature —
 * every baseline capability expects that contract.
 */
internal fun resolveCompatibilityOutcome(
    clientVersionName: String,
    server: ServerHealth?,
): CompatibilityOutcome {
    val serverVersion = server?.version?.let { AppVersion.parse(it) }
    if (server == null) {
        return CompatibilityOutcome(CompatibilityGate.NotRequired, null, null)
    }
    val serverUnsupported = serverVersion != null && !ServerCapabilities.isServerSupported(serverVersion)
    return when (val decision = CompatibilityResolver.resolve(clientVersionName, server)) {
        Compatibility.Compatible -> CompatibilityOutcome(
            gate = if (serverUnsupported) CompatibilityGate.ServerTooOld else CompatibilityGate.NotRequired,
            noticeVersion = null,
            serverVersion = serverVersion,
        )
        is Compatibility.ForceUpdate -> CompatibilityOutcome(
            gate = CompatibilityGate.ForceUpdate(decision.requiredVersion),
            noticeVersion = null,
            serverVersion = serverVersion,
        )
        is Compatibility.ServerUpdateRecommended -> CompatibilityOutcome(
            gate = if (serverUnsupported) CompatibilityGate.ServerTooOld else CompatibilityGate.NotRequired,
            noticeVersion = if (serverUnsupported) null else decision.serverVersion,
            serverVersion = serverVersion,
        )
    }
}
