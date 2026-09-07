package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.domain.repository.ExportRepository
import com.mycorrhizal.crm.network.ApiClient
import javax.inject.Inject

class ExportRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : ExportRepository {
    override suspend fun exportDataCsv(): Result<ByteArray> =
        apiClient.exportDataCsv()

    override suspend fun exportContactsVcf(version: Int?): Result<ByteArray> =
        apiClient.exportAllContactsVcf(version)

    override suspend fun exportContactsJsContact(): Result<ByteArray> =
        apiClient.exportAllContactsJsContact()

    override suspend fun exportAuditLogCsv(): Result<ByteArray> =
        apiClient.exportAuditLogCsv()
}
