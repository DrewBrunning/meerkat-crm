package com.mycorrhizal.crm.data.session

import com.mycorrhizal.crm.network.SessionExpiryNotifier
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * Glue between the network layer's 401 detection and the session store (issue
 * #678): every session-expiry signal ends the session, which flips the app to
 * the auth flow. Kept as its own class rather than inline in DI so the
 * behavior is unit-testable — the session must never survive a 401.
 *
 * Issue #722: when this install holds a device grant, the 401 first tries one
 * grant exchange ([refresher]) — a session that expired because its JWT ran
 * out is seamlessly renewed on a device whose owner already passed the local
 * biometric gate. Only when that refresh fails (grant revoked / network
 * rejected) — or when no grant is enrolled, the default — does the session
 * clear. A grant that the server has revoked therefore still ends exactly as
 * a 401 always has.
 */
class SessionExpiryWiring(
    private val sessionExpiryNotifier: SessionExpiryNotifier,
    private val sessionManager: SessionManager,
    private val refresher: suspend () -> Boolean = { false },
) {
    fun start(scope: CoroutineScope) {
        sessionExpiryNotifier.register {
            scope.launch {
                val refreshed = refresher()
                if (!refreshed) sessionManager.clearSession()
            }
        }
    }
}
