package com.mycorrhizal.crm.domain.repository

import com.mycorrhizal.crm.model.network.ServerHealth

/**
 * Reads the server's compatibility contract from the unauthenticated
 * GET /health (issue #528, docs/client-compatibility-policy.md). This is the
 * single source of truth for the server version on the client — nothing else
 * (an auth response, a settings screen) may echo a second copy, or the two
 * would drift.
 *
 * A failing fetch is NOT an error the caller surfaces: per the fail-open
 * rule, an unreachable or misbehaving /health must never brick the client, so
 * callers resolve any [Result.failure] to "compatible".
 */
interface ServerCompatibilityRepository {
    suspend fun getServerHealth(): Result<ServerHealth>
}
