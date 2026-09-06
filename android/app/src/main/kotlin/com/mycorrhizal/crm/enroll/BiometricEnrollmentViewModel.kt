package com.mycorrhizal.crm.enroll

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.data.auth.DeviceGrantManager
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.BiometricEnrollmentStatus
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.model.Generated
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/** UI state for the post-login biometric-enrollment prompt (issue #722). */
data class BiometricEnrollmentUiState(
    val visible: Boolean = false,
    val isBusy: Boolean = false,
    val error: Boolean = false,
)

sealed interface BiometricEnrollmentEvent {
    /** The prompt is resolved — the caller hides it. */
    data object Done : BiometricEnrollmentEvent
}

/**
 * The one-shot "sign in with biometrics?" prompt shown right after an
 * interactive login (issue #722). It only ever appears when the user has not
 * decided yet ([BiometricEnrollmentStatus.UNASKED]) and the device can
 * actually pass the local gate; "Enroll" mints + stores a device grant,
 * "Not now" leaves the state untouched (so the prompt returns next login),
 * and "Never ask again" persists the opt-out (Settings can still enroll).
 */
@HiltViewModel
class BiometricEnrollmentViewModel @Inject constructor(
    private val deviceGrantManager: DeviceGrantManager,
    private val localAuthSettings: LocalAuthSettingsRepository,
    private val localAuthCapabilities: LocalAuthCapabilities,
    sessionManager: SessionManager,
) : ViewModel() {

    private val _uiState = MutableStateFlow(BiometricEnrollmentUiState())
    val uiState: StateFlow<BiometricEnrollmentUiState> = _uiState.asStateFlow()

    private val _events = Channel<BiometricEnrollmentEvent>(Channel.BUFFERED)
    val events = _events.receiveAsFlow()

    init {
        viewModelScope.launch {
            // Wait for the hydrated session before evaluating the prompt: a
            // cold-start resume must never show it, and enrollment needs a
            // live session to mint the grant. Only ask when the user has not
            // decided yet and the device can satisfy the local gate.
            sessionManager.awaitHydrated()
            val status = localAuthSettings.biometricEnrollmentStatus().first()
            val canAuthenticate = localAuthCapabilities.canEnableLocalAuth()
            val loggedIn = sessionManager.observeSession().first().isLoggedIn
            if (status == BiometricEnrollmentStatus.UNASKED && canAuthenticate && loggedIn) {
                _uiState.value = _uiState.value.copy(visible = true)
            } else {
                _events.send(BiometricEnrollmentEvent.Done)
            }
        }
    }

    /** The user chose "Set up" — mint and store a device grant. */
    @Generated("UI-launch wrapper (viewModelScope); the deterministic state machine is covered via performEnroll")
    fun enroll() {
        if (_uiState.value.isBusy) return
        viewModelScope.launch {
            performEnroll((android.os.Build.MODEL ?: "").ifBlank { "Android" })
        }
    }

    /**
     * Enroll-and-finish, factored out of [enroll] so the deterministic state
     * machine is testable without relying on a launched coroutine.
     */
    internal suspend fun performEnroll(label: String) {
        if (_uiState.value.isBusy) return
        _uiState.value = _uiState.value.copy(isBusy = true, error = false)
        deviceGrantManager.enroll(label).fold(
            onSuccess = {
                _uiState.value = _uiState.value.copy(isBusy = false, visible = false)
                _events.send(BiometricEnrollmentEvent.Done)
            },
            onFailure = {
                _uiState.value = _uiState.value.copy(isBusy = false, error = true)
            },
        )
    }

    /** "Not now" — leave the state UNASKED so the prompt returns next login. */
    fun notNow() {
        _uiState.value = _uiState.value.copy(visible = false)
        viewModelScope.launch { _events.send(BiometricEnrollmentEvent.Done) }
    }

    /** "Never ask again" — persist the opt-out; Settings can still enroll. */
    fun neverAskAgain() {
        _uiState.value = _uiState.value.copy(visible = false)
        viewModelScope.launch {
            localAuthSettings.setBiometricEnrollmentStatus(BiometricEnrollmentStatus.OPTED_OUT)
            _events.send(BiometricEnrollmentEvent.Done)
        }
    }

    /** Dismissed (tap-outside / back) — treated like "not now". */
    fun dismiss() {
        if (_uiState.value.visible) notNow()
    }
}
