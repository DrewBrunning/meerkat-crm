package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * What the app root should render for a persisted session (issue #722).
 *
 *  - [Resolving]: the controller has not yet read the local-auth preference
 *    and the hydrated session, so the root must not compose the authenticated
 *    tree yet (it may be about to require the local gate).
 *  - [Locked]: a persisted session exists and the opt-in local check is due —
 *    the biometric / device-credential gate must appear before any contact
 *    data.
 *  - [Unlocked]: no gate required right now.
 */
enum class AppLockState {
    Resolving,
    Locked,
    Unlocked,
}

/** Owns the app-lock gate decision; see [DefaultAppLockController]. */
interface AppLockController {
    val state: StateFlow<AppLockState>

    /** The process went to the background — start the grace clock. */
    fun onAppBackgrounded()

    /** The process returned to the foreground — re-gate if past the grace period. */
    fun onAppForegrounded()

    /** The local check succeeded (biometric / device credential). */
    fun onUserAuthenticated()
}

/**
 * Decides when a persisted session must pass the local (biometric /
 * device-credential) gate before the authenticated tree renders (issue #722).
 *
 * The gate is driven by [LocalAuthSettingsRepository.requireLocalAuth]:
 *
 *  - **Cold start with a persisted session** → [AppLockState.Locked] (a
 *    freshly-hydrated token means a returning user; the opt-in says "check me
 *    again before showing my data", and there is no way to tell a legitimate
 *    process restart from the phone being unlocked by someone else).
 *  - **Background longer than the configured [AutoLockDelay]** → locked again
 *    on the next foreground. Within the grace period a background/foreground
 *    round-trip does not re-prompt (not on every resume).
 *  - **Interactive login** (password / API token / 2FA / OIDC) never locks —
 *    the user just authenticated. Because the gate only ever applies to a
 *    logged-in session and a login necessarily starts from the logged-out
 *    (ungated) tree, no explicit "fresh login" signal is needed.
 *  - **Session cleared** (logout / 401 expiry / account removal) → unlocked, so
 *    the next login is not immediately re-gated.
 *  - **Disabling the setting** mid-session does not lock the current session
 *    (the user is already authenticated) — it disarms the *next* gate.
 *
 * The state is what the UI branches on; the controller never clears the
 * session. [onAppBackgrounded]/[onAppForegrounded] are wired to the process
 * lifecycle (not the Activity's), so launching another of the app's own
 * activities (the uCrop photo-crop screen) does not count as backgrounding.
 */
class DefaultAppLockController(
    private val settings: LocalAuthSettingsRepository,
    private val sessionManager: SessionManager,
    private val clock: () -> Long = System::currentTimeMillis,
) : AppLockController {
    private val _state = MutableStateFlow(AppLockState.Resolving)
    override val state: StateFlow<AppLockState> = _state.asStateFlow()

    @Volatile
    private var resolved = false
    @Volatile
    private var settingsKnown = false
    @Volatile
    private var sessionKnown = false
    @Volatile
    private var enabled = false
    @Volatile
    private var loggedIn = false
    @Volatile
    private var delay: AutoLockDelay = AutoLockDelay.DEFAULT
    @Volatile
    private var backgroundedAt: Long? = null

    /**
     * Start observing the preference + session. Called once from DI. Each
     * input is collected independently (not via `combine`) so a change to any
     * one of them re-runs [onInputsChanged] without waiting on the others.
     */
    fun start(scope: CoroutineScope) {
        scope.launch {
            settings.requireLocalAuth().collect { enabledNow ->
                enabled = enabledNow
                settingsKnown = true
                onInputsChanged()
            }
        }
        scope.launch {
            sessionManager.observeSession().collect { session ->
                loggedIn = session.isLoggedIn
                sessionKnown = true
                onInputsChanged()
            }
        }
        scope.launch {
            settings.autoLockDelay().collect { delayNow ->
                delay = delayNow
                onInputsChanged()
            }
        }
    }

    private fun onInputsChanged() {
        // Wait for the preference and the hydrated session before deciding —
        // until then the root must keep rendering nothing (Resolving).
        if (!settingsKnown || !sessionKnown) return
        if (!resolved) {
            // Initial resolution: gate a session that was persisted and
            // hydrated at cold start when the user opted in. This is the one
            // decision the later branches must not re-take.
            resolved = true
            _state.value = if (enabled && loggedIn) {
                AppLockState.Locked
            } else {
                AppLockState.Unlocked
            }
        } else if (!enabled || !loggedIn) {
            // Logout / 401 expiry / disabling the setting disarms the gate. A
            // Locked gate only clears via the user authenticating or the
            // session ending — disabling the setting while locked is
            // unreachable from the UI.
            if (_state.value == AppLockState.Locked) {
                _state.value = AppLockState.Unlocked
            }
        }
    }

    /** The process went to the background — start the grace clock. */
    override fun onAppBackgrounded() {
        if (enabled && loggedIn) backgroundedAt = clock()
    }

    /** The process returned to the foreground — re-gate if past the grace period. */
    override fun onAppForegrounded() {
        val backgrounded = backgroundedAt ?: return
        backgroundedAt = null
        if (!resolved || !enabled || !loggedIn) return
        val delayMillis = delay.minutes * 60_000L
        val overdue = delayMillis == 0L || clock() - backgrounded >= delayMillis
        if (overdue && _state.value == AppLockState.Unlocked) {
            _state.value = AppLockState.Locked
        }
    }

    /**
     * The local check succeeded (biometric / device credential). Clears the
     * gate and the background clock — the backgrounding that happened while
     * the OS prompt was up must not immediately re-lock the session.
     */
    override fun onUserAuthenticated() {
        backgroundedAt = null
        if (_state.value == AppLockState.Locked) {
            _state.value = AppLockState.Unlocked
        }
    }
}
