package com.mycorrhizal.crm.feature.contacts

import com.mycorrhizal.crm.domain.repository.DuplicateRepository
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.model.network.DuplicatePair
import com.mycorrhizal.crm.model.network.DuplicatePairsResponse
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class DuplicatePairsViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val duplicateRepository = mockk<DuplicateRepository>()

    private fun pair(aId: Int, aUid: String, bId: Int, bUid: String) = DuplicatePair(
        a = ContactSummary(id = aId, uid = aUid, firstname = "Dana", lastname = "White"),
        b = ContactSummary(id = bId, uid = bUid, firstname = "Dan", lastname = "White"),
        reasons = listOf("email", "name"),
        confidence = 0.85,
    )

    @Test
    fun `load pulls every page until the scan is exhausted`() =
        runTest(mainDispatcherRule.testDispatcher) {
            // 150 total candidates, page size 100 — the scan must walk two pages.
            val page1 = (1..100).map { pair(it, "u$it", it + 1000, "v$it") }
            val page2 = (101..150).map { pair(it, "u$it", it + 1000, "v$it") }
            coEvery { duplicateRepository.listPairs(page = 1, limit = 100) } returns Result.success(
                DuplicatePairsResponse(pairs = page1, total = 150, page = 1, limit = 100),
            )
            coEvery { duplicateRepository.listPairs(page = 2, limit = 100) } returns Result.success(
                DuplicatePairsResponse(pairs = page2, total = 150, page = 2, limit = 100),
            )

            val vm = DuplicatePairsViewModel(duplicateRepository)
            advanceUntilIdle()

            val state = vm.uiState.value
            assertEquals(150, state.pairs.size)
            assertEquals(150, state.total)
            assertNull(state.error)
            assertTrue(!state.isLoading)
            coVerify { duplicateRepository.listPairs(page = 1, limit = 100) }
            coVerify { duplicateRepository.listPairs(page = 2, limit = 100) }
        }

    @Test
    fun `first-page failure surfaces the error and stops the scan`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { duplicateRepository.listPairs(any(), any()) } returns Result.failure(
                ApiError.Client(500, "scan failed"),
            )

            val vm = DuplicatePairsViewModel(duplicateRepository)
            advanceUntilIdle()

            assertEquals("scan failed", vm.uiState.value.error)
            assertTrue(vm.uiState.value.pairs.isEmpty())
            coVerify(exactly = 1) { duplicateRepository.listPairs(any(), any()) }
        }

    @Test
    fun `dismiss posts the pair uids and drops the pair locally`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val candidate = pair(1, "u-a", 2, "u-b")
            coEvery { duplicateRepository.listPairs(any(), any()) } returns Result.success(
                DuplicatePairsResponse(pairs = listOf(candidate), total = 1, page = 1, limit = 100),
            )
            coEvery { duplicateRepository.dismiss("u-a", "u-b") } returns Result.success(Unit)

            val vm = DuplicatePairsViewModel(duplicateRepository)
            advanceUntilIdle()
            assertEquals(1, vm.uiState.value.pairs.size)

            vm.dismiss(candidate)
            advanceUntilIdle()

            coVerify { duplicateRepository.dismiss("u-a", "u-b") }
            assertTrue(vm.uiState.value.pairs.isEmpty())
            assertEquals(0, vm.uiState.value.total)
            assertNull(vm.uiState.value.dismissingKey)
        }

    @Test
    fun `dismiss failure keeps the pair and surfaces the error`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val candidate = pair(1, "u-a", 2, "u-b")
            coEvery { duplicateRepository.listPairs(any(), any()) } returns Result.success(
                DuplicatePairsResponse(pairs = listOf(candidate), total = 1, page = 1, limit = 100),
            )
            coEvery { duplicateRepository.dismiss("u-a", "u-b") } returns Result.failure(
                ApiError.Client(404, "Pair not found"),
            )

            val vm = DuplicatePairsViewModel(duplicateRepository)
            advanceUntilIdle()

            vm.dismiss(candidate)
            advanceUntilIdle()

            assertEquals(1, vm.uiState.value.pairs.size)
            assertEquals("Not found", vm.uiState.value.error)
        }

    @Test
    fun `onErrorShown clears the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { duplicateRepository.listPairs(any(), any()) } returns Result.failure(
            ApiError.Client(500, "boom"),
        )
        val vm = DuplicatePairsViewModel(duplicateRepository)
        advanceUntilIdle()
        assertEquals("boom", vm.uiState.value.error)

        vm.onErrorShown()
        assertNull(vm.uiState.value.error)
    }
}
