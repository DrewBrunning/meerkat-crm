package com.mycorrhizal.crm.model.network

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

/**
 * One file/document attached to a contact (N7). Mirrors the OpenAPI
 * Attachment schema: the row is metadata only — the bytes live server-side
 * under a generated UUID name, and [originalName] is display-only (never used
 * to build a filesystem path).
 */
@JsonClass(generateAdapter = true)
data class ContactAttachment(
    val id: Int = 0,
    @Json(name = "created_at") val createdAt: String? = null,
    @Json(name = "contact_vcard_uid") val contactVCardUid: String? = null,
    @Json(name = "original_name") val originalName: String = "",
    @Json(name = "content_type") val contentType: String = "",
    @Json(name = "size_bytes") val sizeBytes: Long = 0,
)

/** GET /contacts/{id}/attachments response envelope. */
@JsonClass(generateAdapter = true)
data class AttachmentListResponse(
    val attachments: List<ContactAttachment> = emptyList(),
    val total: Int = 0,
)

/** POST /contacts/{id}/attachments response envelope — `{ attachment }`. */
@JsonClass(generateAdapter = true)
data class AttachmentUploadResponse(
    val attachment: ContactAttachment? = null,
)
