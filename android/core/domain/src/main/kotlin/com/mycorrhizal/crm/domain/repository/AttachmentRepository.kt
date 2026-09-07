package com.mycorrhizal.crm.domain.repository

import com.mycorrhizal.crm.model.network.AttachmentListResponse

/**
 * N7 contact attachments (web-parity for the contact-detail Attachments
 * section). Metadata rows are small; the bytes are transferred explicitly on
 * download and never mirrored locally beyond the request buffer.
 */
interface AttachmentRepository {
    /** GET /contacts/{id}/attachments — the contact's non-deleted attachments, newest first. */
    suspend fun list(contactId: Int): Result<AttachmentListResponse>

    /** POST /contacts/{id}/attachments — multipart upload (server caps size/type). */
    suspend fun upload(contactId: Int, fileName: String, mimeType: String, bytes: ByteArray): Result<Unit>

    /** GET /attachments/{id}/download — the raw bytes of one attachment. */
    suspend fun download(attachmentId: Int): Result<ByteArray>

    /** DELETE /attachments/{id} — soft-delete (T26 tombstone). */
    suspend fun delete(attachmentId: Int): Result<Unit>
}
