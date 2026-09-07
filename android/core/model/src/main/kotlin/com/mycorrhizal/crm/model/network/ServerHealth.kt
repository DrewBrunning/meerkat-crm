package com.mycorrhizal.crm.model.network

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

/**
 * GET /health — the unauthenticated deep-health body reduced to the fields the
 * client/server compatibility check needs (issue #528). The full response also
 * carries status/database/checks etc., which this client deliberately does not
 * model; Moshi ignores the unknown keys.
 *
 * Mirrors the OpenAPI `HealthResponse` flat fields:
 *  - [version] is the server's own build version (`buildinfo`), used for the
 *    "client newer than server" state.
 *  - [minClientVersion] is the oldest client versionName the server still
 *    supports. Absent = no floor declared = every client is compatible.
 *  - [apiContractVersion] is the API contract generation the server speaks
 *    ("v1" while the API is on /api/v1); announced before any client has to
 *    react to it, so it is parsed and retained but not yet acted on.
 *
 * Fail-open contract: a fetch that errors, or a body where [version] /
 * [minClientVersion] are absent or unparseable, resolves to "compatible"
 * (docs/client-compatibility-policy.md).
 */
@JsonClass(generateAdapter = true)
data class ServerHealth(
    val version: String? = null,
    @Json(name = "api_contract_version") val apiContractVersion: String? = null,
    @Json(name = "min_client_version") val minClientVersion: String? = null,
)
