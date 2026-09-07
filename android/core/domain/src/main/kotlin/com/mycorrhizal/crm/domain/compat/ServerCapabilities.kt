package com.mycorrhizal.crm.domain.compat

import com.mycorrhizal.crm.model.AppVersion

/**
 * The capability gate for issue #692: given the server version a session
 * resolved from /health (issue #528, the single source of truth), does the
 * connected server support a [ServerFeature]?
 *
 * ## Fail-open rule
 *
 * A null server version (no /health yet, an unreachable/misbehaving /health, an
 * unparseable version such as an unstamped "dev" build) resolves to
 * "supported": the client must never hide functionality because it could not
 * confirm the server's version. This mirrors the fail-open posture of
 * [CompatibilityResolver] — an unreachable /health must not degrade the app.
 *
 * ## The baseline
 *
 * [MIN_SUPPORTED_SERVER_VERSION] is the oldest server this app will talk to
 * (0.6.0, matching the backend's migration floor). A server that reports an
 * older-but-parseable version makes the whole authenticated surface unusable,
 * which is the "server too old" blocking gate in the root — see
 * [isServerSupported].
 */
object ServerCapabilities {

    /** The oldest server release this app supports (issue #692). */
    val MIN_SUPPORTED_SERVER_VERSION: AppVersion = AppVersion(0, 6, 0)

    /**
     * True when [server] is at or above [feature.minServerVersion]. A null
     * [server] (unknown) fails open to true.
     */
    fun isSupported(server: AppVersion?, feature: ServerFeature): Boolean =
        server == null || server >= feature.minServerVersion

    /**
     * True when the app may render its authenticated tree against [server].
     * A null [server] (unknown) fails open to true; a known server below
     * [MIN_SUPPORTED_SERVER_VERSION] is refused with the "server too old" gate.
     */
    fun isServerSupported(server: AppVersion?): Boolean =
        server == null || server >= MIN_SUPPORTED_SERVER_VERSION
}
