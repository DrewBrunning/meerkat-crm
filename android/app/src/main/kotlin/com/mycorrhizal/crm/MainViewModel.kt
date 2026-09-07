package com.mycorrhizal.crm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.AppLockState
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.compat.Compatibility
import com.mycorrhizal.crm.domain.compat.CompatibilityResolver
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.ServerCompatibilityRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * The client/server compatibility decision that gates the whole root (issue
 * #528). A [ForceUpdate] renders a blocking screen in front of every other
 * surface; the default is [NotRequired] (compatible, or the check has not yet
 * resolved).
 */
sealed interface CompatibilityGate {
    data object NotRequired : CompatibilityGate
    data class ForceUpdate(val requiredVersion: String) : CompatibilityGate
}

/**
 * Root application state: whether a session exists (drives login vs main tree)
 * and whether that session must pass the local app-lock gate first (issue
 * #722). `appLockState` is only meaningful once [AppLockState.Resolving] has
 * cleared — the root never renders the authenticated tree while it is pending.
 *
 * Issue #528: the session's server compatibility is checked once per login
 * edge (app start with a stored session, and every interactive login). The
 * check is intentionally NOT a gate on the main tree — it runs alongside it
 * and, if it resolves to a required update, swaps the root to the blocking
 * force-update screen. That keeps a cold start with an unreachable /health
 * fully functional (fail open), per docs/client-compatibility-policy.md.
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

    /** The current compatibility gate. Reset to [CompatibilityGate.NotRequired]
     *  whenever the session ends; set to [CompatibilityGate.ForceUpdate] when a
     *  session's /health check finds the client below the server's floor. */
    private val _compatibilityGate = MutableStateFlow<CompatibilityGate>(CompatibilityGate.NotRequired)
    val compatibilityGate: StateFlow<CompatibilityGate> = _compatibilityGate.asStateFlow()

    /** The server version while the current session's server is older than this
     *  app (the non-blocking "server could be upgraded" notice, issue #528).
     *  Null = no notice. */
    private val _serverOutdatedNoticeVersion = MutableStateFlow<String?>(null)
    val serverOutdatedNoticeVersion: StateFlow<String?> = _serverOutdatedNoticeVersion.asStateFlow()

    private var compatibilityCheck: Job? = null

    init {
        // Issue #528: once per session. observeSession() emits the hydrated
        // state on app start, so a cold start with a stored session triggers a
        // check, and every false→true transition (interactive login) does too.
        viewModelScope.launch {
            sessionManager.observeSession()
                .map { it.isLoggedIn }
                .distinctUntilChanged()
                .collect { loggedIn ->
                    if (!loggedIn) {
                        compatibilityCheck?.cancel()
                        _compatibilityGate.value = CompatibilityGate.NotRequired
                        _serverOutdatedNoticeVersion.value = null
                    } else {
                        compatibilityCheck?.cancel()
                        compatibilityCheck = viewModelScope.launch { runCompatibilityCheck() }
                    }
                }
        }
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
     * Fetches the server's compatibility contract once and resolves the three
     * states. Fail open is guaranteed by [CompatibilityResolver] — a network
     * error, an absent server, or a malformed body all resolve to compatible,
     * so this can never strand the user on a stale force-update screen.
     */
    private suspend fun runCompatibilityCheck() {
        val server = serverCompatibilityRepository.getServerHealth().getOrNull()
        when (val decision = CompatibilityResolver.resolve(BuildConfig.VERSION_NAME, server)) {
            Compatibility.Compatible -> {
                _serverOutdatedNoticeVersion.value = null
            }
            is Compatibility.ForceUpdate -> {
                _compatibilityGate.value = CompatibilityGate.ForceUpdate(decision.requiredVersion)
            }
            is Compatibility.ServerUpdateRecommended -> {
                _serverOutdatedNoticeVersion.value = decision.serverVersion
            }
        }
    }
}
