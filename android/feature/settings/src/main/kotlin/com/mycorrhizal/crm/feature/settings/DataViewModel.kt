package com.mycorrhizal.crm.feature.settings

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.ExportRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.model.network.ApplyContactAddressSuggestionInput
import com.mycorrhizal.crm.model.network.ContactAddressSuggestion
import com.mycorrhizal.crm.network.foldApiError
import com.mycorrhizal.crm.ui.R
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * T104 + address suggestions: the "propose data" screen. Two opt-in engines —
 * graph-inferred relationship suggestions and relationship/household-derived
 * address suggestions — plus the address-suggestion review list with explicit
 * Apply. Nothing is applied automatically; the user confirms each suggestion.
 *
 * Also owns the full-dataset export actions (issue #710, web parity): each
 * format is fetched as raw bytes and handed to the UI once ([DataUiState.
 * exported]) for the share sheet.
 */
data class DataUiState(
    val addressSuggestions: List<ContactAddressSuggestion> = emptyList(),
    val suggestionsLoaded: Boolean = false,
    val suggestionsLoading: Boolean = false,
    val isSuggestingRelationships: Boolean = false,
    /** The in-flight apply key: `contact_uid|address_key`. */
    val applyingKey: String? = null,
    /** Number of relationship edges the last suggest run newly created (null = not yet run). */
    val suggestedRelationshipCount: Int? = null,
    @StringRes val infoRes: Int? = null,
    val infoCount: Int? = null,
    /** True while a dataset export is being fetched. */
    val isExporting: Boolean = false,
    /** One-shot: the finished export, cleared by [DataViewModel.onExportHandled]. */
    val exported: DataExport? = null,
    val error: String? = null,
)

/** One completed dataset export: the bytes plus the format's filename/mime for the share sheet. */
data class DataExport(
    val kind: DataExportKind,
    val bytes: ByteArray,
) {
    val fileName: String = kind.fileName
    val mimeType: String = kind.mimeType
}

/**
 * The exportable formats on the Settings → Data screen. Each maps to one
 * backend endpoint (see ExportRepository) and to the share-sheet filename and
 * MIME type the Android client uses (the server streams the file directly).
 */
enum class DataExportKind(val fileName: String, val mimeType: String) {
    CSV("mycorrhizal-export.csv", "text/csv"),
    VCF3("mycorrhizal-contacts-v3.vcf", "text/vcard"),
    VCF4("mycorrhizal-contacts.vcf", "text/vcard"),
    JSCONTACT("mycorrhizal-contacts.jscontact.json", "application/json"),
    AUDIT_CSV("mycorrhizal-audit.csv", "text/csv"),
}

@HiltViewModel
class DataViewModel @Inject constructor(
    private val contactRepository: ContactRepository,
    private val relationshipEdgeRepository: RelationshipEdgeRepository,
    private val exportRepository: ExportRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(DataUiState())
    val uiState: StateFlow<DataUiState> = _uiState.asStateFlow()

    /** T104: run one round of graph inference over confirmed edges. */
    fun suggestRelationships() {
        if (_uiState.value.isSuggestingRelationships) return
        _uiState.update { it.copy(isSuggestingRelationships = true, error = null) }
        viewModelScope.launch {
            relationshipEdgeRepository.suggest().foldApiError(
                onSuccess = { edges ->
                    _uiState.update {
                        it.copy(
                            isSuggestingRelationships = false,
                            suggestedRelationshipCount = edges.size,
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(isSuggestingRelationships = false, error = error.displayMessage) }
                },
            )
        }
    }

    /**
     * 167: run the read-only address-suggestion scan. Nothing is persisted
     * until the user applies a specific suggestion.
     */
    fun scanAddressSuggestions() {
        if (_uiState.value.suggestionsLoading) return
        _uiState.update { it.copy(suggestionsLoading = true, error = null) }
        viewModelScope.launch {
            contactRepository.suggestContactAddresses().foldApiError(
                onSuccess = { suggestions ->
                    _uiState.update {
                        it.copy(
                            suggestionsLoading = false,
                            suggestionsLoaded = true,
                            addressSuggestions = suggestions,
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(suggestionsLoading = false, error = error.displayMessage) }
                },
            )
        }
    }

    /** Apply one address suggestion and remove it from the list. */
    fun applySuggestion(suggestion: ContactAddressSuggestion) {
        val key = suggestionKey(suggestion)
        if (_uiState.value.applyingKey != null) return
        _uiState.update { it.copy(applyingKey = key, error = null) }
        viewModelScope.launch {
            contactRepository.applyContactAddressSuggestion(
                ApplyContactAddressSuggestionInput(
                    contactVCardUid = suggestion.contactVCardUid,
                    sourceKind = suggestion.sourceKind,
                    sourceId = suggestion.sourceId,
                    addressKey = suggestion.addressKey,
                ),
            ).foldApiError(
                onSuccess = {
                    _uiState.update { state ->
                        state.copy(
                            applyingKey = null,
                            addressSuggestions = state.addressSuggestions.filterNot { suggestionKey(it) == key },
                            infoRes = R.string.data_address_applied,
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(applyingKey = null, error = error.displayMessage) }
                },
            )
        }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(error = null) }
    }

    /**
     * Fetches one full-dataset export as bytes and exposes it as a one-shot
     * [DataUiState.exported] for the screen to write out and share. Which
     * repository call runs is a pure function of [kind]; re-entrancy is
     * guarded by [DataUiState.isExporting].
     */
    fun export(kind: DataExportKind) {
        if (_uiState.value.isExporting) return
        _uiState.update { it.copy(isExporting = true, error = null) }
        val fetch: suspend () -> Result<ByteArray> = when (kind) {
            DataExportKind.CSV -> exportRepository::exportDataCsv
            DataExportKind.VCF3 -> { { exportRepository.exportContactsVcf(3) } }
            DataExportKind.VCF4 -> { { exportRepository.exportContactsVcf(null) } }
            DataExportKind.JSCONTACT -> exportRepository::exportContactsJsContact
            DataExportKind.AUDIT_CSV -> exportRepository::exportAuditLogCsv
        }
        viewModelScope.launch {
            fetch().foldApiError(
                onSuccess = { bytes ->
                    _uiState.update { it.copy(isExporting = false, exported = DataExport(kind, bytes)) }
                },
                onError = { error ->
                    _uiState.update { it.copy(isExporting = false, error = error.displayMessage) }
                },
            )
        }
    }

    fun onExportHandled() {
        _uiState.update { it.copy(exported = null) }
    }

    fun onInfoShown() {
        _uiState.update { it.copy(infoRes = null, infoCount = null) }
    }

    private fun suggestionKey(suggestion: ContactAddressSuggestion): String =
        "${suggestion.contactVCardUid}|${suggestion.addressKey}"
}

/** Human label for a relation token in the address-reason line (e.g. "parent_of" -> "parent of"). */
fun relationTokenLabel(token: String?): String = token?.replace('_', ' ')?.trim().orEmpty()
