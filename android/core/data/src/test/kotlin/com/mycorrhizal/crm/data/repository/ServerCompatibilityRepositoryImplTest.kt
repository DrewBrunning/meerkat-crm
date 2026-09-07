package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.model.network.ServerHealth
import com.mycorrhizal.crm.network.ApiClient
import io.mockk.coEvery
import io.mockk.mockk
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerCompatibilityRepositoryImplTest {

    private val apiClient = mockk<ApiClient>()

    private fun repo(apiClient: ApiClient) = ServerCompatibilityRepositoryImpl(apiClient)

    @Test
    fun `delegates a successful health fetch through`() = runTest {
        val health = ServerHealth(version = "0.6.10", minClientVersion = "0.6.0")
        coEvery { apiClient.getHealth() } returns Result.success(health)

        val result = repo(apiClient).getServerHealth()

        assertTrue(result.isSuccess)
        assertEquals(health, result.getOrThrow())
    }

    @Test
    fun `delegates a failing health fetch through`() = runTest {
        coEvery { apiClient.getHealth() } returns Result.failure(RuntimeException("boom"))

        val result = repo(apiClient).getServerHealth()

        // The repository does NOT swallow failures: callers resolve them to
        // "compatible" (the fail-open rule), which keeps the decision visible.
        assertTrue(result.isFailure)
    }
}
