package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.domain.repository.ServerCompatibilityRepository
import com.mycorrhizal.crm.model.network.ServerHealth
import com.mycorrhizal.crm.network.ApiClient
import javax.inject.Inject

/**
 * Reads the compatibility contract off the public GET /health (issue #528).
 * Thin on purpose — the fail-open policy lives in the callers (they resolve a
 * [Result.failure] to "compatible"), not in a repository that would hide it.
 */
class ServerCompatibilityRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : ServerCompatibilityRepository {

    override suspend fun getServerHealth(): Result<ServerHealth> = apiClient.getHealth()
}
