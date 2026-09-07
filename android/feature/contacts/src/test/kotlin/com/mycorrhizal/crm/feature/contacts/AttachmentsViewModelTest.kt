package com.mycorrhizal.crm.feature.contacts

import com.mycorrhizal.crm.domain.repository.AttachmentRepository
import com.mycorrhizal.crm.model.network.AttachmentListResponse
import com.mycorrhizal.crm.model.network.ContactAttachment
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

class AttachmentsViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val attachmentRepository = mockk<AttachmentRepository>()

    private val scan = ContactAttachment(id = 7, originalName = "scan.pdf", contentType = "application/pdf", sizeBytes = 2048)

    @Test
    fun `setContact loads the attachment list`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { attachmentRepository.list(5) } returns Result.success(
            AttachmentListResponse(attachments = listOf(scan), total = 1),
        )

        val vm = AttachmentsViewModel(attachmentRepository)
        vm.setContact(5)
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals(listOf(7), state.attachments.map { it.id })
        assertEquals(1, state.total)
        assertNull(state.error)
    }

    @Test
    fun `list failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { attachmentRepository.list(5) } returns Result.failure(ApiError.Client(500, "boom"))

        val vm = AttachmentsViewModel(attachmentRepository)
        vm.setContact(5)
        advanceUntilIdle()

        assertEquals("boom", vm.uiState.value.error)
    }

    @Test
    fun `upload posts the file and refetches the list`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { attachmentRepository.list(5) } returns Result.success(
            AttachmentListResponse(attachments = emptyList(), total = 0),
        )
        coEvery { attachmentRepository.upload(5, "notes.txt", "text/plain", any()) } returns Result.success(Unit)

        val vm = AttachmentsViewModel(attachmentRepository)
        vm.setContact(5)
        advanceUntilIdle()

        vm.upload("notes.txt", "text/plain", "hello".toByteArray())
        advanceUntilIdle()

        coVerify { attachmentRepository.upload(5, "notes.txt", "text/plain", any()) }
        coVerify(exactly = 2) { attachmentRepository.list(5) }
        assertTrue(!vm.uiState.value.isUploading)
    }

    @Test
    fun `upload failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { attachmentRepository.list(5) } returns Result.success(
            AttachmentListResponse(attachments = emptyList(), total = 0),
        )
        coEvery { attachmentRepository.upload(any(), any(), any(), any()) } returns Result.failure(
            ApiError.Client(400, "too large"),
        )

        val vm = AttachmentsViewModel(attachmentRepository)
        vm.setContact(5)
        advanceUntilIdle()

        vm.upload("big.txt", "text/plain", ByteArray(1))
        advanceUntilIdle()

        assertEquals("too large", vm.uiState.value.error)
    }

    @Test
    fun `delete removes the row locally`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { attachmentRepository.list(5) } returns Result.success(
            AttachmentListResponse(attachments = listOf(scan), total = 1),
        )
        coEvery { attachmentRepository.delete(7) } returns Result.success(Unit)

        val vm = AttachmentsViewModel(attachmentRepository)
        vm.setContact(5)
        advanceUntilIdle()

        vm.delete(scan)
        advanceUntilIdle()

        coVerify { attachmentRepository.delete(7) }
        assertTrue(vm.uiState.value.attachments.isEmpty())
        assertEquals(0, vm.uiState.value.total)
    }

    @Test
    fun `download exposes the bytes once and clears on handled`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { attachmentRepository.list(5) } returns Result.success(
                AttachmentListResponse(attachments = listOf(scan), total = 1),
            )
            coEvery { attachmentRepository.download(7) } returns Result.success("bytes".toByteArray())

            val vm = AttachmentsViewModel(attachmentRepository)
            vm.setContact(5)
            advanceUntilIdle()

            vm.download(scan)
            advanceUntilIdle()

            val downloaded = vm.uiState.value.downloaded
            assertEquals(7, downloaded?.first?.id)
            assertEquals("bytes", downloaded?.second?.decodeToString())

            vm.onDownloadHandled()
            assertNull(vm.uiState.value.downloaded)
        }

    @Test
    fun `download failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { attachmentRepository.list(5) } returns Result.success(
            AttachmentListResponse(attachments = listOf(scan), total = 1),
        )
        coEvery { attachmentRepository.download(7) } returns Result.failure(ApiError.Client(404, "gone"))

        val vm = AttachmentsViewModel(attachmentRepository)
        vm.setContact(5)
        advanceUntilIdle()

        vm.download(scan)
        advanceUntilIdle()

        assertEquals("Not found", vm.uiState.value.error)
        assertNull(vm.uiState.value.downloaded)
    }

    @Test
    fun `rejectUpload reports the size rejection resource`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = AttachmentsViewModel(attachmentRepository)
        vm.rejectUpload()

        val state = vm.uiState.value
        assertNull(state.error)
        assertEquals(com.mycorrhizal.crm.ui.R.string.attachments_size_too_large, state.uploadErrorRes)

        vm.onErrorShown()
        assertNull(vm.uiState.value.uploadErrorRes)
    }
}
