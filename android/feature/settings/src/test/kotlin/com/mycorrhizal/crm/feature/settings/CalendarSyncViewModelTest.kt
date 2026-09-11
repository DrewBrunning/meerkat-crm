package com.mycorrhizal.crm.feature.settings

import com.mycorrhizal.crm.domain.repository.CalendarSubscriptionRepository
import com.mycorrhizal.crm.domain.repository.ContactSubscriptionRepository
import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.CalendarSubscriptionInput
import com.mycorrhizal.crm.model.network.CalendarSyncResult
import com.mycorrhizal.crm.model.network.ContactSubscription
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class CalendarSyncViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val calendarRepository = mockk<CalendarSubscriptionRepository>()
    private val contactSubscriptionRepository = mockk<ContactSubscriptionRepository>()

    private fun calendar(
        id: Int,
        name: String = "Cal $id",
        consecutiveFailures: Int = 0,
        lastSyncStatus: String = "",
    ) = CalendarSubscription(id = id, name = name, url = "https://example.com/$id.ics", consecutiveFailures = consecutiveFailures, lastSyncStatus = lastSyncStatus)

    private fun viewModel(): CalendarSyncViewModel {
        coEvery { contactSubscriptionRepository.list() } returns Result.success(emptyList())
        return CalendarSyncViewModel(calendarRepository, contactSubscriptionRepository)
    }

    @Test
    fun `load populates calendars and contact subscriptions`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { calendarRepository.list() } returns Result.success(listOf(calendar(1), calendar(2)))
        coEvery { contactSubscriptionRepository.list() } returns Result.success(
            listOf(ContactSubscription(id = 1, name = "Address Book")),
        )
        val vm = CalendarSyncViewModel(calendarRepository, contactSubscriptionRepository)
        advanceUntilIdle()

        val state = vm.uiState.value
        assertFalse(state.isLoading)
        assertEquals(2, state.calendars.size)
        assertEquals(1, state.contactSubscriptions.size)
        assertEquals("Address Book", state.contactSubscriptions[0].name)
    }

    @Test
    fun `load failure on calendars surfaces the error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { calendarRepository.list() } returns Result.failure(ApiError.Server(500, "boom"))
        val vm = viewModel()
        advanceUntilIdle()

        assertEquals("Server error (500)", vm.uiState.value.error)
    }

    @Test
    fun `load failure on contact subscriptions does not clobber a calendar error`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { calendarRepository.list() } returns Result.failure(ApiError.Server(500, "calendars down"))
            coEvery { contactSubscriptionRepository.list() } returns Result.failure(ApiError.Server(500, "contacts down"))
            val vm = CalendarSyncViewModel(calendarRepository, contactSubscriptionRepository)
            advanceUntilIdle()

            assertEquals("Server error (500)", vm.uiState.value.error)
        }

    @Test
    fun `create appends the new calendar and triggers its first sync`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { calendarRepository.list() } returns Result.success(emptyList())
        val created = calendar(9, name = "New")
        coEvery { calendarRepository.create(any()) } returns Result.success(created)
        coEvery { calendarRepository.sync(9) } returns Result.success(CalendarSyncResult(created = 3, updated = 1, skipped = 0))
        val vm = viewModel()
        advanceUntilIdle()

        vm.save(CalendarSubscriptionInput(name = "New", url = "https://example.com/9.ics"), editingId = null)
        advanceUntilIdle()

        coVerify { calendarRepository.sync(9) }
        assertEquals(3, vm.uiState.value.lastSyncResult?.created)
    }

    @Test
    fun `update replaces the edited calendar in place`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { calendarRepository.list() } returns Result.success(listOf(calendar(1), calendar(2)))
        coEvery { calendarRepository.update(2, any()) } returns Result.success(calendar(2, name = "Renamed"))
        val vm = viewModel()
        advanceUntilIdle()

        vm.save(CalendarSubscriptionInput(name = "Renamed", url = "https://example.com/2.ics"), editingId = 2)
        advanceUntilIdle()

        assertEquals("Renamed", vm.uiState.value.calendars[1].name)
        assertFalse(vm.uiState.value.isSaving)
    }

    @Test
    fun `delete removes the calendar after a confirmed call`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { calendarRepository.list() } returns Result.success(listOf(calendar(1), calendar(2)))
        coEvery { calendarRepository.delete(1) } returns Result.success(Unit)
        val vm = viewModel()
        advanceUntilIdle()

        vm.delete(vm.uiState.value.calendars[0])
        advanceUntilIdle()

        coVerify { calendarRepository.delete(1) }
        assertEquals(listOf(2), vm.uiState.value.calendars.map { it.id })
    }

    @Test
    fun `sync marks the id in flight then reloads the list on success`() = runTest(mainDispatcherRule.testDispatcher) {
        val healthy = calendar(1)
        val healed = calendar(1, lastSyncStatus = "success")
        coEvery { calendarRepository.list() } returns Result.success(listOf(healthy)) andThen Result.success(listOf(healed))
        coEvery { calendarRepository.sync(1) } returns Result.success(CalendarSyncResult(created = 2, updated = 0, skipped = 1))
        val vm = viewModel()
        advanceUntilIdle()

        vm.sync(healthy)
        advanceUntilIdle()

        val state = vm.uiState.value
        assertTrue(state.syncingIds.isEmpty())
        assertEquals(2, state.lastSyncResult?.created)
        assertEquals("success", state.calendars[0].lastSyncStatus)
    }

    @Test
    fun `sync failure surfaces the error and still reloads the list`() = runTest(mainDispatcherRule.testDispatcher) {
        val cal = calendar(1)
        coEvery { calendarRepository.list() } returns Result.success(listOf(cal))
        coEvery { calendarRepository.sync(1) } returns Result.failure(ApiError.Client(400, "unreachable"))
        val vm = viewModel()
        advanceUntilIdle()

        vm.sync(cal)
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals("unreachable", state.error)
        assertNull(state.lastSyncResult)
        assertTrue(state.syncingIds.isEmpty())
        coVerify(exactly = 2) { calendarRepository.list() }
    }
}
