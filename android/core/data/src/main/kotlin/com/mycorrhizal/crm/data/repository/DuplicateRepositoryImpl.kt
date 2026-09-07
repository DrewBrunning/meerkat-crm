package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.domain.repository.DuplicateRepository
import com.mycorrhizal.crm.model.network.DuplicatePairsResponse
import com.mycorrhizal.crm.network.ApiClient
import javax.inject.Inject

class DuplicateRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : DuplicateRepository {
    override suspend fun listPairs(page: Int, limit: Int?): Result<DuplicatePairsResponse> =
        apiClient.listDuplicatePairs(page = page, limit = limit)

    override suspend fun dismiss(uidA: String, uidB: String): Result<Unit> =
        apiClient.dismissDuplicatePair(uidA = uidA, uidB = uidB)
}
