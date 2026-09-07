package com.mycorrhizal.crm.feature.contacts

import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.ContactsPage
import com.mycorrhizal.crm.domain.repository.MergeRepository
import com.mycorrhizal.crm.model.network.ContactMergePreviewResponse
import com.mycorrhizal.crm.model.network.ContactMergeRequest
import com.mycorrhizal.crm.model.network.ContactMergeResolution
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class MergeAndBulkViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val mergeRepository = mockk<MergeRepository>()
    private val contactRepository = mockk<ContactRepository>()

    private fun viewModel(): MergeContactsViewModel =
        MergeContactsViewModel(mergeRepository, contactRepository)

    @Test
    fun `preview loads and exposes conflicts`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { mergeRepository.preview(any()) } returns Result.success(
            ContactMergePreviewResponse(
                keepId = 1, mergeId = 2,
                resolution = ContactMergeResolution(
                    conflicts = listOf(
                        com.mycorrhizal.crm.model.network.ContactMergeFieldConflict(
                            field = "firstname", label = "First name",
                            keeperValue = "Dana", loserValue = "Dana",
                        ),
                    ),
                ),
            ),
        )

        val vm = viewModel()
        vm.setPair(1, 2)
        vm.preview()
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals(1, state.preview?.resolution?.conflicts?.size)
        assertEquals(1, vm.allConflicts(state.preview!!).size)
    }

    @Test
    fun `commit sends resolutions and marks merged`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { mergeRepository.commit(any()) } returns Result.success(
            com.mycorrhizal.crm.model.network.ContactRecordResponse(id = 1),
        )

        val vm = viewModel()
        vm.setPair(1, 2)
        vm.resolve("firstname", "Dana")
        vm.commit()
        advanceUntilIdle()

        assertTrue(vm.uiState.value.merged)
        coVerify {
            mergeRepository.commit(
                match<ContactMergeRequest> { it.resolutions?.get("firstname") == "Dana" },
            )
        }
    }

    @Test
    fun `preview failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { mergeRepository.preview(any()) } returns Result.failure(ApiError.Client(400, "bad pair"))

        val vm = viewModel()
        vm.setPair(1, 1)
        vm.preview()
        advanceUntilIdle()

        assertEquals("bad pair", vm.uiState.value.error)
    }

    // --- M23: search-based target picker (test case 4) ---

    @Test
    fun `a merge can be initiated by search without ever entering a numeric id`() = runTest(mainDispatcherRule.testDispatcher) {
        // The reported gap: Android required typing the target's raw numeric ID. This test
        // drives the whole flow — type a name, pick the row, preview fires — with no ID input.
        coEvery { contactRepository.listContacts(cursor = null, limit = 100, search = "Dana") } returns
            Result.success(
                ContactsPage(
                    contacts = listOf(
                        ContactSummary(id = 5, uid = "uid-5", fn = "Dana White", firstname = "Dana"),
                        ContactSummary(id = 7, uid = "uid-7", fn = "Dana Brown", firstname = "Dana"),
                    ),
                    nextCursor = null, limit = 100, sync = null,
                ),
            )
        coEvery { mergeRepository.preview(match { it.mergeId == 5L }) } returns Result.success(
            ContactMergePreviewResponse(keepId = 1, mergeId = 5, resolution = ContactMergeResolution()),
        )

        val vm = viewModel()
        vm.setPair(1, 0)

        vm.onSearchQueryChange("Dana")
        advanceUntilIdle()

        assertEquals(2, vm.uiState.value.searchResults.size)

        vm.selectOther(vm.uiState.value.searchResults.first())
        advanceUntilIdle()

        assertEquals(5L, vm.uiState.value.mergeId)
        assertEquals(5L, vm.uiState.value.preview?.mergeId)
        coVerify { mergeRepository.preview(match { it.keepId == 1L && it.mergeId == 5L }) }
    }

    @Test
    fun `search results exclude the keeper`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { contactRepository.listContacts(cursor = null, limit = 100, search = "Dana") } returns
            Result.success(
                ContactsPage(
                    contacts = listOf(
                        // id 3 == keepId: a contact can't merge into itself and must not be offered.
                        ContactSummary(id = 3, uid = "uid-3", fn = "Dana Self"),
                        ContactSummary(id = 5, uid = "uid-5", fn = "Dana White"),
                    ),
                    nextCursor = null, limit = 100, sync = null,
                ),
            )

        val vm = viewModel()
        vm.setPair(3, 0)

        vm.onSearchQueryChange("Dana")
        advanceUntilIdle()

        assertEquals(1, vm.uiState.value.searchResults.size)
        assertEquals(5, vm.uiState.value.searchResults.first().id)
    }

    @Test
    fun `a preselected pair loads the preview without the search hop`() = runTest(mainDispatcherRule.testDispatcher) {
        // T93: duplicate review navigates to merge with BOTH contacts known;
        // setPair + loadPreview must fire the preview immediately.
        coEvery { mergeRepository.preview(any()) } returns Result.success(
            ContactMergePreviewResponse(keepId = 1, mergeId = 2, resolution = ContactMergeResolution()),
        )

        val vm = viewModel()
        vm.setPair(1, 2)
        vm.loadPreview()
        advanceUntilIdle()

        assertEquals(2L, vm.uiState.value.preview?.mergeId)
        coVerify { mergeRepository.preview(match { it.keepId == 1L && it.mergeId == 2L }) }
    }

    // T112: web T101 parity — the server search is deliberately broad (name +
    // email + phone + address + FTS), so a page can contain a contact matched
    // only on an unrelated field. The merge picker must not offer it, and must
    // disclose the drop via hiddenMatchCount instead of silently hiding it.
    @Test
    fun `search results are strict-name-filtered with a hidden count`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { contactRepository.listContacts(cursor = null, limit = 100, search = "Jordan") } returns
            Result.success(
                ContactsPage(
                    contacts = listOf(
                        ContactSummary(id = 5, uid = "uid-5", firstname = "Jordan", lastname = "Lee"),
                        // Server matched this on an address/email/FTS field; its
                        // display name does not contain "Jordan" and it must not
                        // be offered as a merge target.
                        ContactSummary(id = 7, uid = "uid-7", firstname = "Meike", lastname = "Schmidt"),
                    ),
                    nextCursor = null, limit = 100, sync = null,
                ),
            )

        val vm = viewModel()
        vm.setPair(1, 0)

        vm.onSearchQueryChange("Jordan")
        advanceUntilIdle()

        assertEquals(listOf(5), vm.uiState.value.searchResults.map { it.id })
        assertEquals(1, vm.uiState.value.hiddenMatchCount)
    }

    @Test
    fun `a blank search fires no request`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel()
        vm.setPair(1, 0)

        vm.onSearchQueryChange("")
        advanceUntilIdle()

        coVerify(exactly = 0) { contactRepository.listContacts(any(), any(), any()) }
        assertTrue(vm.uiState.value.searchResults.isEmpty())
    }

    @Test
    fun `search failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { contactRepository.listContacts(cursor = null, limit = 100, search = "zzz") } returns
            Result.failure(ApiError.Client(500, "server error"))

        val vm = viewModel()
        vm.setPair(1, 0)

        vm.onSearchQueryChange("zzz")
        advanceUntilIdle()

        assertEquals("server error", vm.uiState.value.error)
        assertTrue(vm.uiState.value.searchResults.isEmpty())
    }
}
