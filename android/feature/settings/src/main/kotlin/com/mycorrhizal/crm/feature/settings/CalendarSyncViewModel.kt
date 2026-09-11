package com.mycorrhizal.crm.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.CalendarSubscriptionRepository
import com.mycorrhizal.crm.domain.repository.ContactSubscriptionRepository
import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.CalendarSubscriptionInput
import com.mycorrhizal.crm.model.network.CalendarSyncResult
import com.mycorrhizal.crm.model.network.ContactSubscription
import com.mycorrhizal.crm.network.ApiError
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class CalendarSyncUiState(
    val calendars: List<CalendarSubscription> = emptyList(),
    /** Read-only (issue #628 scope note 4) — CardDAV contact subscriptions' sync health. */
    val contactSubscriptions: List<ContactSubscription> = emptyList(),
    val isLoading: Boolean = false,
    val isSaving: Boolean = false,
    /** Ids of calendars whose sync-now call is in flight. */
    val syncingIds: Set<Int> = emptySet(),
    /** The most recent sync's tallies, shown once and then cleared. */
    val lastSyncResult: CalendarSyncResult? = null,
    /** A transient action error (load/save/delete/sync), shown and then cleared. */
    val error: String? = null,
) {
    val isEmpty: Boolean get() = calendars.isEmpty() && contactSubscriptions.isEmpty()
}

/**
 * Calendar (CalDAV/iCal) subscription list/create/edit/delete/sync-now, plus
 * a read-only contact (CardDAV) subscription health list — issue #390's
 * Android follow-up (#628). Mirrors web's `CalendarSyncSettings.tsx`:
 * create/update patch the returned subscription into state directly (it
 * already carries the full sync-health surface); delete filters locally;
 * sync-now re-fetches the calendar list afterward since the sync response
 * itself doesn't carry the updated health fields — the same choice web's
 * `await loadCalendars()` in `handleSync`'s `finally` makes.
 */
@HiltViewModel
class CalendarSyncViewModel @Inject constructor(
    private val calendarRepository: CalendarSubscriptionRepository,
    private val contactSubscriptionRepository: ContactSubscriptionRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(CalendarSyncUiState())
    val uiState: StateFlow<CalendarSyncUiState> = _uiState.asStateFlow()

    init {
        load()
    }

    fun load() {
        if (_uiState.value.isLoading) return
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, error = null) }
            calendarRepository.list()
                .onSuccess { list -> _uiState.update { it.copy(isLoading = false, calendars = list) } }
                .onFailure { e -> _uiState.update { it.copy(isLoading = false, error = e.displayMessage()) } }
            contactSubscriptionRepository.list()
                .onSuccess { list -> _uiState.update { it.copy(contactSubscriptions = list) } }
                .onFailure { e ->
                    // Secondary surface (read-only): don't clobber a calendar-load
                    // error already on screen with this one.
                    _uiState.update { if (it.error == null) it.copy(error = e.displayMessage()) else it }
                }
        }
    }

    fun save(input: CalendarSubscriptionInput, editingId: Int?) {
        if (_uiState.value.isSaving) return
        viewModelScope.launch {
            _uiState.update { it.copy(isSaving = true, error = null) }
            if (editingId == null) {
                calendarRepository.create(input)
                    .onSuccess { created ->
                        _uiState.update { it.copy(isSaving = false, calendars = it.calendars + created) }
                        // Web: trigger the first sync right away so the user gets feedback.
                        sync(created)
                    }
                    .onFailure { e -> _uiState.update { it.copy(isSaving = false, error = e.displayMessage()) } }
            } else {
                calendarRepository.update(editingId, input)
                    .onSuccess { updated ->
                        _uiState.update {
                            it.copy(
                                isSaving = false,
                                calendars = it.calendars.map { c -> if (c.id == editingId) updated else c },
                            )
                        }
                    }
                    .onFailure { e -> _uiState.update { it.copy(isSaving = false, error = e.displayMessage()) } }
            }
        }
    }

    fun delete(calendar: CalendarSubscription) {
        viewModelScope.launch {
            _uiState.update { it.copy(error = null) }
            calendarRepository.delete(calendar.id)
                .onSuccess {
                    _uiState.update { it.copy(calendars = it.calendars.filterNot { c -> c.id == calendar.id }) }
                }
                .onFailure { e -> _uiState.update { it.copy(error = e.displayMessage()) } }
        }
    }

    fun sync(calendar: CalendarSubscription) {
        viewModelScope.launch {
            _uiState.update { it.copy(syncingIds = it.syncingIds + calendar.id, error = null, lastSyncResult = null) }
            calendarRepository.sync(calendar.id)
                .onSuccess { result -> _uiState.update { it.copy(lastSyncResult = result) } }
                .onFailure { e -> _uiState.update { it.copy(error = e.displayMessage()) } }
            _uiState.update { it.copy(syncingIds = it.syncingIds - calendar.id) }
            // Health fields (consecutive_failures, last_run_stats, ...) live on the
            // subscription, not the sync response -- refresh to pick them up.
            calendarRepository.list().onSuccess { list -> _uiState.update { it.copy(calendars = list) } }
        }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(error = null) }
    }

    fun onSyncResultShown() {
        _uiState.update { it.copy(lastSyncResult = null) }
    }

    private fun Throwable.displayMessage(): String =
        (this as? ApiError)?.displayMessage ?: message ?: "error"
}
