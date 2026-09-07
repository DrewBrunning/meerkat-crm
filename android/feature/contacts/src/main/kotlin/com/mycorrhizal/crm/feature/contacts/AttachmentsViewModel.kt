package com.mycorrhizal.crm.feature.contacts

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.AttachmentRepository
import com.mycorrhizal.crm.model.network.ContactAttachment
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
 * N7 contact attachments (web-parity for the contact-detail Attachments
 * section). The list is metadata only; the bytes are pulled on demand for a
 * download/open ([downloaded]) and pushed on upload. A successful upload
 * refetches the list so the new row (with its server-assigned metadata)
 * appears rather than trusting a client-built echo.
 */
@HiltViewModel
class AttachmentsViewModel @Inject constructor(
    private val attachmentRepository: AttachmentRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(AttachmentUiState())
    val uiState: StateFlow<AttachmentUiState> = _uiState.asStateFlow()

    private var contactId: Int = 0

    fun setContact(id: Int) {
        if (id == 0) return
        if (contactId != id) {
            contactId = id
            load()
        }
    }

    fun load() {
        if (contactId == 0 || _uiState.value.isLoading) return
        _uiState.update { it.copy(isLoading = true, error = null) }
        viewModelScope.launch {
            attachmentRepository.list(contactId).foldApiError(
                onSuccess = { response ->
                    _uiState.update {
                        it.copy(
                            isLoading = false,
                            attachments = response.attachments,
                            total = response.total,
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(isLoading = false, error = error.displayMessage) }
                },
            )
        }
    }

    /**
     * Uploads one picked file. The server enforces the 25MB cap and rejects
     * SVG/HTML itself; the client checks the declared size first (before
     * reading the bytes into memory) so an oversized pick is refused without
     * an allocation. On success the list is refetched.
     */
    fun upload(fileName: String, mimeType: String, bytes: ByteArray) {
        if (_uiState.value.isUploading || bytes.isEmpty()) return
        _uiState.update { it.copy(isUploading = true, error = null) }
        viewModelScope.launch {
            attachmentRepository.upload(contactId, fileName, mimeType, bytes).foldApiError(
                onSuccess = {
                    _uiState.update { it.copy(isUploading = false) }
                    load()
                },
                onError = { error ->
                    _uiState.update { it.copy(isUploading = false, error = error.displayMessage) }
                },
            )
        }
    }

    /** Delete with the caller's confirmation handled in the UI (destructive, soft-deleted server-side). */
    fun delete(attachment: ContactAttachment) {
        if (_uiState.value.deletingId != null) return
        _uiState.update { it.copy(deletingId = attachment.id, error = null) }
        viewModelScope.launch {
            attachmentRepository.delete(attachment.id).foldApiError(
                onSuccess = {
                    _uiState.update { state ->
                        val remaining = state.attachments.filterNot { it.id == attachment.id }
                        state.copy(
                            deletingId = null,
                            attachments = remaining,
                            total = (state.total - 1).coerceAtLeast(0),
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(deletingId = null, error = error.displayMessage) }
                },
            )
        }
    }

    /**
     * Pulls one attachment's bytes for the screen to write out and open.
     * Exposed as a one-shot [downloaded] so the screen can side-effect on it
     * exactly once (the ContactDetail export pattern).
     */
    fun download(attachment: ContactAttachment) {
        if (_uiState.value.downloadingId != null) return
        _uiState.update { it.copy(downloadingId = attachment.id, error = null) }
        viewModelScope.launch {
            attachmentRepository.download(attachment.id).foldApiError(
                onSuccess = { bytes ->
                    _uiState.update {
                        it.copy(
                            downloadingId = null,
                            downloaded = attachment to bytes,
                        )
                    }
                },
                onError = { error ->
                    _uiState.update { it.copy(downloadingId = null, error = error.displayMessage) }
                },
            )
        }
    }

    fun onDownloadHandled() {
        _uiState.update { it.copy(downloaded = null) }
    }

    /**
     * The picker offered a file larger than the server's cap. Reported by the
     * screen after probing the declared size and before reading the bytes,
     * so an oversized pick is refused without an allocation.
     */
    fun rejectUpload() {
        _uiState.update { it.copy(uploadErrorRes = R.string.attachments_size_too_large) }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(error = null, uploadErrorRes = null) }
    }

    companion object {
        /** The backend's upload cap (attachment_controller.go) — probed before reading bytes. */
        const val MAX_ATTACHMENT_SIZE_BYTES = 25L * 1024 * 1024
    }
}

data class AttachmentUiState(
    val attachments: List<ContactAttachment> = emptyList(),
    val total: Int = 0,
    val isLoading: Boolean = false,
    val isUploading: Boolean = false,
    /** The in-flight delete's attachment id; null when none is running. */
    val deletingId: Int? = null,
    /** The in-flight download's attachment id; null when none is running. */
    val downloadingId: Int? = null,
    /** One-shot: the just-downloaded attachment + bytes, cleared by [AttachmentsViewModel.onDownloadHandled]. */
    val downloaded: Pair<ContactAttachment, ByteArray>? = null,
    /** A pre-read size rejection (e.g. over the server cap) — see [AttachmentsViewModel.rejectUpload]. */
    @StringRes val uploadErrorRes: Int? = null,
    val error: String? = null,
)
