package com.mycorrhizal.crm.model.network

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

// --- Issue #722 fully-biometric-login device grants (server half) ---

/** POST /api/v1/auth/device/grants body — optional display label. */
@JsonClass(generateAdapter = true)
data class DeviceGrantCreateRequest(
    val label: String? = null,
)

/**
 * POST /api/v1/auth/device/grants response. [token] is the one-time plaintext
 * grant, returned exactly once; [id] lets the client revoke this device later.
 */
@JsonClass(generateAdapter = true)
data class DeviceGrantCreateResponse(
    val id: Long = 0,
    val label: String? = null,
    @Json(name = "created_at") val createdAt: String? = null,
    @Json(name = "last_used_at") val lastUsedAt: String? = null,
    @Json(name = "revoked_at") val revokedAt: String? = null,
    val token: String? = null,
)

/** POST /api/v1/auth/device/session body — possession of the stored grant. */
@JsonClass(generateAdapter = true)
data class DeviceGrantExchangeRequest(
    @Json(name = "device_token") val deviceToken: String,
)

/** POST /api/v1/auth/device/grants/revoke-all — `{ revoked: n }`. */
@JsonClass(generateAdapter = true)
data class RevokeAllDeviceGrantsResponse(
    val revoked: Int? = null,
)
