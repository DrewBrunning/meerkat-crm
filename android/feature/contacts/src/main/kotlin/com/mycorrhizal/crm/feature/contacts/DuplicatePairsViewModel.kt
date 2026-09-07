package com.mycorrhizal.crm.feature.contacts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.DuplicateRepository
import com.mycorrhizal.crm.model.network.DuplicatePair
import com.mycorrhizal.crm.network.foldApiError
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * T93 duplicate review (web-parity for the "Review duplicates" surface).
 *
 * The candidate pairs are recomputed server-side on every fetch and dismissed
 * pairs are already filtered out, so this client stays stateless across
 * reloads — [load] always restarts from page one and pulls offset pages until
 * the scan is exhausted (bounded by [MAX_PAGES]), appending as each page
 * lands so a large scan fills incrementally instead of holding a blank
 * spinner. Safe because the backend sorts the whole result set before
 * offsetting, so appending pages preserves the global strongest-first order.
 */
@HiltViewModel
class DuplicatePairsViewModel @Inject constructor(
    private val duplicateRepository: DuplicateRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(DuplicatePairsUiState())
    val uiState: StateFlow<DuplicatePairsUiState> = _uiState.asStateFlow()

    private var loadJob: Job? = null

    init {
        load()
    }

    /** Full re-scan from page one. Re-entrant: a running scan is left alone. */
    fun load() {
        if (_uiState.value.isLoading) return
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            _uiState.update {
                it.copy(
                    isLoading = true,
                    error = null,
                    pairs = emptyList(),
                    total = 0,
                )
            }
            var exhausted = false
            for (page in 1..MAX_PAGES) {
                duplicateRepository.listPairs(page = page, limit = PAGE_SIZE).foldApiError(
                    onSuccess = { response ->
                        val appended = _uiState.value.pairs + response.pairs
                        _uiState.update { it.copy(pairs = appended, total = response.total) }
                        exhausted = appended.size >= response.total
                    },
                    onError = { error ->
                        _uiState.update { it.copy(isLoading = false, error = error.displayMessage) }
                    },
                )
                // A failed page aborts the scan (error already surfaced); only
                // keep pulling while more pairs remain.
                if (_uiState.value.error != null) break
                if (exhausted) break
            }
            _uiState.update { it.copy(isLoading = false) }
        }
    }

    /**
     * POST a permanent "not a duplicate" verdict, then drop the pair from the
     * in-memory list (the server filters dismissed pairs out of future scans,
     * so a local removal matches the next fetch's result). The UIDs are
     * ordered server-side, so either pair orientation dismisses the same row.
     */
    fun dismiss(pair: DuplicatePair) {
        val a = pair.a.uid.orEmpty()
        val b = pair.b.uid.orEmpty()
        if (a.isBlank() || b.isBlank()) return
        if (_uiState.value.dismissingKey != null) return
        _uiState.update { it.copy(dismissingKey = pair.reviewKey, error = null) }
        viewModelScope.launch {
            duplicateRepository.dismiss(a, b).foldApiError(
                onSuccess = {
                    _uiState.update { state ->
                        val remaining = state.pairs.filterNot { it.reviewKey == pair.reviewKey }
                        state.copy(
                            dismissingKey = null,
                            pairs = remaining,
                            total = (state.total - 1).coerceAtLeast(0),
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(dismissingKey = null, error = error.displayMessage) }
                },
            )
        }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(error = null) }
    }

    companion object {
        /** Page size for the scan's offset pagination (backend caps at 100). */
        const val PAGE_SIZE = 100

        /**
         * Hard cap on the number of pages the review surface will pull. The
         * pairs list is bounded by the real duplicate set (review is
         * inherently a full-scan surface); this guard exists only so a
         * pathological address book cannot drive an unbounded fan-out.
         */
        const val MAX_PAGES = 50
    }
}

data class DuplicatePairsUiState(
    val pairs: List<DuplicatePair> = emptyList(),
    val total: Int = 0,
    val isLoading: Boolean = false,
    val error: String? = null,
    /** The in-flight dismiss's [DuplicatePair.reviewKey]; null when none is running. */
    val dismissingKey: String? = null,
)
