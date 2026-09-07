package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.domain.repository.AttachmentRepository
import com.mycorrhizal.crm.model.network.AttachmentListResponse
import com.mycorrhizal.crm.network.ApiClient
import javax.inject.Inject

class AttachmentRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : AttachmentRepository {
    override suspend fun list(contactId: Int): Result<AttachmentListResponse> =
        apiClient.listContactAttachments(contactId)

    override suspend fun upload(contactId: Int, fileName: String, mimeType: String, bytes: ByteArray): Result<Unit> =
        apiClient.uploadContactAttachment(contactId, fileName, mimeType, bytes)

    override suspend fun download(attachmentId: Int): Result<ByteArray> =
        apiClient.downloadAttachment(attachmentId)

    override suspend fun delete(attachmentId: Int): Result<Unit> =
        apiClient.deleteAttachment(attachmentId)
}
