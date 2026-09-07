package com.mycorrhizal.crm.domain.compat

import com.mycorrhizal.crm.model.AppVersion
import com.mycorrhizal.crm.model.network.ServerHealth

/**
 * Outcome of the client/server compatibility check (issue #528). Exactly the
 * three states the policy (docs/client-compatibility-policy.md) defines —
 * a client implementation must not invent a fourth.
 */
sealed interface Compatibility {
    /** Proceed silently. */
    data object Compatible : Compatibility

    /**
     * The client is below the server's declared floor
     * (`min_client_version`): the server will no longer serve this build, so
     * the app shows a blocking force-update screen naming [requiredVersion].
     */
    data class ForceUpdate(val requiredVersion: String) : Compatibility

    /**
     * The client is newer than the server's own build version: the phone
     * auto-updated while the server sat on an older release. Non-blocking —
     * the app keeps working, and a notice suggests upgrading the server.
     */
    data class ServerUpdateRecommended(val serverVersion: String) : Compatibility
}

/**
 * Decides the three-state compatibility outcome from the client's own
 * versionName and the server's advertised contract. Pure so the policy is
 * unit-testable without a network or composable host.
 *
 * The fail-open rule is load-bearing and lives here: any input the client
 * cannot confidently compare — a null/unreachable server, an unparseable
 * client version, an unparseable floor, an unparseable server version
 * (unstamped "dev" builds) — resolves to [Compatibility.Compatible]. An
 * unreachable /health must never brick the client into a permanent
 * force-update screen.
 */
object CompatibilityResolver {
    fun resolve(clientVersionName: String, server: ServerHealth?): Compatibility {
        if (server == null) return Compatibility.Compatible
        val client = AppVersion.parse(clientVersionName) ?: return Compatibility.Compatible

        server.minClientVersion?.let { rawFloor ->
            val floor = AppVersion.parse(rawFloor)
            if (floor != null && client < floor) {
                return Compatibility.ForceUpdate(rawFloor.trim())
            }
        }

        server.version?.let { rawServerVersion ->
            val serverVersion = AppVersion.parse(rawServerVersion)
            if (serverVersion != null && client > serverVersion) {
                return Compatibility.ServerUpdateRecommended(rawServerVersion.trim())
            }
        }

        return Compatibility.Compatible
    }
}
