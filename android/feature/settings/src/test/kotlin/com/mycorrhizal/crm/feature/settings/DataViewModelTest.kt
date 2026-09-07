package com.mycorrhizal.crm.feature.settings

import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.ExportRepository
import com.mycorrhizal.crm.model.network.ApplyContactAddressSuggestionInput
import com.mycorrhizal.crm.model.network.ContactAddressSuggestion
import com.mycorrhizal.crm.model.network.RelationshipEdge
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import com.mycorrhizal.crm.ui.R
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

class DataViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val contactRepository = mockk<ContactRepository>()
    private val relationshipEdgeRepository = mockk<RelationshipEdgeRepository>()
    private val exportRepository = mockk<ExportRepository>()

    private val suggestion = ContactAddressSuggestion(
        contactVCardUid = "alice-uid",
        contactName = "Alice",
        sourceKind = "relationship",
        sourceId = "bob-uid",
        sourceName = "Bob",
        relationType = "spouse_of",
        addressKey = "key1",
    )

    @Test
    fun `suggestRelationships records the count of newly created edges`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { relationshipEdgeRepository.suggest() } returns Result.success(listOf(RelationshipEdge(id = "e1")))
            val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

            vm.suggestRelationships()
            advanceUntilIdle()

            coVerify { relationshipEdgeRepository.suggest() }
            assertEquals(1, vm.uiState.value.suggestedRelationshipCount)
            assertNull(vm.uiState.value.error)
        }

    @Test
    fun `suggestRelationships failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { relationshipEdgeRepository.suggest() } returns Result.failure(ApiError.Client(500, "boom"))
        val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

        vm.suggestRelationships()
        advanceUntilIdle()

        assertEquals("boom", vm.uiState.value.error)
        assertNull(vm.uiState.value.suggestedRelationshipCount)
    }

    @Test
    fun `scanAddressSuggestions loads the suggestions`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { contactRepository.suggestContactAddresses() } returns Result.success(listOf(suggestion))
        val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

        vm.scanAddressSuggestions()
        advanceUntilIdle()

        coVerify { contactRepository.suggestContactAddresses() }
        assertEquals(listOf(suggestion), vm.uiState.value.addressSuggestions)
        assertTrue(vm.uiState.value.suggestionsLoaded)
    }

    @Test
    fun `scanAddressSuggestions failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { contactRepository.suggestContactAddresses() } returns Result.failure(ApiError.Client(500, "boom"))
        val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

        vm.scanAddressSuggestions()
        advanceUntilIdle()

        assertEquals("boom", vm.uiState.value.error)
        assertTrue(vm.uiState.value.addressSuggestions.isEmpty())
    }

    @Test
    fun `applySuggestion removes the row and reports success`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { contactRepository.suggestContactAddresses() } returns Result.success(listOf(suggestion))
        coEvery { contactRepository.applyContactAddressSuggestion(any()) } returns Result.success(Unit)
        val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

        vm.scanAddressSuggestions()
        advanceUntilIdle()
        assertEquals(1, vm.uiState.value.addressSuggestions.size)

        vm.applySuggestion(suggestion)
        advanceUntilIdle()

        coVerify {
            contactRepository.applyContactAddressSuggestion(
                ApplyContactAddressSuggestionInput(
                    contactVCardUid = "alice-uid",
                    sourceKind = "relationship",
                    sourceId = "bob-uid",
                    addressKey = "key1",
                ),
            )
        }
        assertTrue(vm.uiState.value.addressSuggestions.isEmpty())
        assertEquals(R.string.data_address_applied, vm.uiState.value.infoRes)
    }

    @Test
    fun `applySuggestion failure keeps the row and surfaces the error`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { contactRepository.suggestContactAddresses() } returns Result.success(listOf(suggestion))
            coEvery { contactRepository.applyContactAddressSuggestion(any()) } returns
                Result.failure(ApiError.Client(409, "stale"))
            val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

            vm.scanAddressSuggestions()
            advanceUntilIdle()

            vm.applySuggestion(suggestion)
            advanceUntilIdle()

            assertEquals("stale", vm.uiState.value.error)
            assertEquals(1, vm.uiState.value.addressSuggestions.size)
            assertNull(vm.uiState.value.infoRes)
        }

    @Test
    fun `export CSV fetches the csv bytes and exposes them once`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { exportRepository.exportDataCsv() } returns Result.success("csv".toByteArray())
            val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

            vm.export(DataExportKind.CSV)
            advanceUntilIdle()

            val exported = vm.uiState.value.exported
            assertEquals(DataExportKind.CSV, exported?.kind)
            assertEquals("csv", exported?.bytes?.decodeToString())
            assertEquals("mycorrhizal-export.csv", exported?.fileName)
            assertTrue(!vm.uiState.value.isExporting)

            vm.onExportHandled()
            assertNull(vm.uiState.value.exported)
        }

    @Test
    fun `export routes each kind to the matching repository call`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { exportRepository.exportContactsVcf(any()) } returns Result.success("vcf".toByteArray())
            coEvery { exportRepository.exportContactsJsContact() } returns Result.success("[]".toByteArray())
            coEvery { exportRepository.exportAuditLogCsv() } returns Result.success("a,b".toByteArray())

            val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

            vm.export(DataExportKind.VCF3)
            advanceUntilIdle()
            assertEquals("mycorrhizal-contacts-v3.vcf", vm.uiState.value.exported?.fileName)
            coVerify { exportRepository.exportContactsVcf(3) }
            vm.onExportHandled()

            vm.export(DataExportKind.VCF4)
            advanceUntilIdle()
            coVerify { exportRepository.exportContactsVcf(null) }
            vm.onExportHandled()

            vm.export(DataExportKind.JSCONTACT)
            advanceUntilIdle()
            coVerify { exportRepository.exportContactsJsContact() }
            vm.onExportHandled()

            vm.export(DataExportKind.AUDIT_CSV)
            advanceUntilIdle()
            coVerify { exportRepository.exportAuditLogCsv() }
            vm.onExportHandled()
        }

    @Test
    fun `export failure surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { exportRepository.exportContactsVcf(any()) } returns
            Result.failure(ApiError.Client(500, "export failed"))
        val vm = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)

        vm.export(DataExportKind.VCF4)
        advanceUntilIdle()

        assertEquals("export failed", vm.uiState.value.error)
        assertNull(vm.uiState.value.exported)
        assertTrue(!vm.uiState.value.isExporting)
    }
}
