package com.mycorrhizal.crm.feature.contacts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.MergeRepository
import com.mycorrhizal.crm.model.network.ContactMergeFieldConflict
import com.mycorrhizal.crm.model.network.ContactMergePreviewResponse
import com.mycorrhizal.crm.model.network.ContactMergeRequest
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.network.foldApiError
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class MergeUiState(
    val keepId: Long = 0,
    val mergeId: Long = 0,
    // M23: the target is picked by search, not typed raw ID (the reported gap).
    // searchResults never include the keeper — a contact can't merge into itself.
    val searchQuery: String = "",
    val searchResults: List<ContactSummary> = emptyList(),
    // T112: how many of the server's deliberately-broad results the client-side
    // strict-name filter dropped, so the picker can say so instead of silently
    // hiding rows (web T101 parity). Reset to 0 whenever the results clear.
    val hiddenMatchCount: Int = 0,
    val isSearching: Boolean = false,
    val pickedOther: ContactSummary? = null,
    val preview: ContactMergePreviewResponse? = null,
    val isLoading: Boolean = false,
    val isCommitting: Boolean = false,
    /** field -> chosen value; covers scalar + field-value conflicts. */
    val resolutions: Map<String, String> = emptyMap(),
    val merged: Boolean = false,
    val error: String? = null,
)

@HiltViewModel
class MergeContactsViewModel @Inject constructor(
    private val mergeRepository: MergeRepository,
    // M23: the search-based target picker needs the same contact search the list
    // uses; the repository is the established surface for it.
    private val contactRepository: ContactRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(MergeUiState())
    val uiState: StateFlow<MergeUiState> = _uiState.asStateFlow()

    fun setPair(keepId: Long, mergeId: Long) {
        _uiState.update { it.copy(keepId = keepId, mergeId = mergeId) }
    }

    /** M23: debounced server search for the merge target, excluding the keeper. */
    fun onSearchQueryChange(query: String) {
        _uiState.update { it.copy(searchQuery = query, searchResults = emptyList(), hiddenMatchCount = 0, isSearching = false) }
        searchJob?.cancel()
        searchJob = viewModelScope.launch {
            if (query.isBlank()) return@launch
            delay(SEARCH_DEBOUNCE_MS)
            _uiState.update { it.copy(isSearching = true) }
            contactRepository.listContacts(search = query.trim(), limit = SEARCH_LIMIT).foldApiError(
                onSuccess = { page ->
                    val keepId = _uiState.value.keepId
                    val trimmed = _uiState.value.searchQuery.trim()
                    // T112: web T101 parity — the server search is deliberately
                    // broad (name AND email AND phone AND address AND FTS tokens),
                    // which is right for the contacts list but surfaces unrelated
                    // contacts in a picker that feeds a destructive merge. Keep the
                    // wide server query (so a phone/email search still reaches the
                    // right contact) but only offer rows whose displayed name
                    // contains what was typed.
                    val withoutKeeper = page.contacts.filter { contact -> contact.id != keepId.toInt() }
                    val matched = withoutKeeper.filter { contact ->
                        contact.displayName.contains(trimmed, ignoreCase = true)
                    }
                    _uiState.update {
                        it.copy(
                            isSearching = false,
                            searchResults = matched,
                            hiddenMatchCount = withoutKeeper.size - matched.size,
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(isSearching = false, searchResults = emptyList(), error = error.displayMessage) }
                },
            )
        }
    }

    /** M23: picking a search result sets the merge target and loads the preview immediately. */
    fun selectOther(contact: ContactSummary) {
        _uiState.update {
            it.copy(
                mergeId = contact.id.toLong(),
                pickedOther = contact,
                searchQuery = "",
                searchResults = emptyList(),
                hiddenMatchCount = 0,
                isSearching = false,
                preview = null,
                resolutions = emptyMap(),
            )
        }
        preview()
    }

    fun preview() {
        val keep = _uiState.value.keepId
        val merge = _uiState.value.mergeId
        if (keep == 0L || merge == 0L) return
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, error = null) }
            mergeRepository.preview(ContactMergeRequest(keepId = keep, mergeId = merge)).foldApiError(
                onSuccess = { preview ->
                    _uiState.update { it.copy(isLoading = false, preview = preview) }
                },
                onError = { error ->
                    _uiState.update { it.copy(isLoading = false, error = error.displayMessage) }
                },
            )
        }
    }

    /**
     * T93: a duplicate-review merge arrives with both sides preselected. Loads
     * the preview immediately, without the search-picker hop. Public alias of
     * [preview] so the caller expresses intent (state is set first by
     * [setPair]).
     */
    fun loadPreview() {
        preview()
    }

    fun resolve(field: String, value: String) {
        _uiState.update { it.copy(resolutions = it.resolutions + (field to value)) }
    }

    fun commit() {
        val keep = _uiState.value.keepId
        val merge = _uiState.value.mergeId
        if (keep == 0L || merge == 0L || _uiState.value.isCommitting) return
        viewModelScope.launch {
            _uiState.update { it.copy(isCommitting = true, error = null) }
            mergeRepository.commit(
                ContactMergeRequest(keepId = keep, mergeId = merge, resolutions = _uiState.value.resolutions),
            ).foldApiError(
                onSuccess = {
                    _uiState.update { it.copy(isCommitting = false, merged = true) }
                },
                onError = { error ->
                    _uiState.update { it.copy(isCommitting = false, error = error.displayMessage) }
                },
            )
        }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(error = null) }
    }

    fun allConflicts(preview: ContactMergePreviewResponse): List<ContactMergeFieldConflict> =
        preview.resolution.conflicts + preview.resolution.fieldValueConflicts

    private var searchJob: Job? = null

    companion object {
        private const val SEARCH_DEBOUNCE_MS = 300L
        private const val SEARCH_LIMIT = 100
    }
}
