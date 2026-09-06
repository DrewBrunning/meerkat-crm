package com.mycorrhizal.crm.applock

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.ui.R
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

/** What the lock screen needs to render (issue #722). */
data class AppLockUiState(
    val username: String? = null,
    val isUnlocking: Boolean = false,
    /** The device can currently complete the local check (biometric or secure lock screen). */
    val canAuthenticate: Boolean = true,
    /** Failure of the last local-auth attempt, shown under the unlock button. */
    @StringRes val errorRes: Int? = null,
)

/** Outcome of a local-auth attempt (biometric prompt or device credential). */
sealed interface AppLockAuthOutcome {
    /** The user authenticated — the session may be resumed. */
    data object Success : AppLockAuthOutcome

    /** The user dismissed the prompt — stay locked. */
    data object Cancelled : AppLockAuthOutcome

    /** The device has no usable biometric or secure lock screen right now. */
    data object NotAvailable : AppLockAuthOutcome

    /** The OS reported a transient error (timeout, lockout, hardware hiccup). */
    data object Error : AppLockAuthOutcome
}

/**
 * Issue #722: the state + actions behind the app-lock gate screen. It holds no
 * credentials and never touches the session — a successful local check calls
 * [AppLockController.onUserAuthenticated] so the root can compose the
 * authenticated tree; "Log out" ends the session the normal way (which also
 * wipes the offline mirror).
 */
@HiltViewModel
class AppLockViewModel @Inject constructor(
    private val appLockController: AppLockController,
    private val authRepository: AuthRepository,
    private val localAuthCapabilities: LocalAuthCapabilities,
    sessionManager: SessionManager,
) : ViewModel() {

    private val _uiState = MutableStateFlow(AppLockUiState())
    val uiState: StateFlow<AppLockUiState> = _uiState.asStateFlow()

    init {
        viewModelScope.launch {
            sessionManager.observeSession().collect { session ->
                _uiState.update {
                    it.copy(
                        username = session.username ?: it.username,
                        canAuthenticate = localAuthCapabilities.canEnableLocalAuth(),
                    )
                }
            }
        }
    }

    /** A local-auth prompt was launched — show the in-progress state. */
    fun onAuthStarted() {
        _uiState.update { it.copy(isUnlocking = true, errorRes = null) }
    }

    /** The local-auth prompt finished. */
    fun onAuthResult(outcome: AppLockAuthOutcome) {
        when (outcome) {
            AppLockAuthOutcome.Success -> appLockController.onUserAuthenticated()
            AppLockAuthOutcome.Cancelled -> _uiState.update { it.copy(isUnlocking = false) }
            AppLockAuthOutcome.NotAvailable -> _uiState.update {
                it.copy(isUnlocking = false, errorRes = R.string.app_lock_unsupported)
            }
            AppLockAuthOutcome.Error -> _uiState.update {
                it.copy(isUnlocking = false, errorRes = R.string.app_lock_auth_failed)
            }
        }
    }

    /** The user chose "Log out" — end the session (the preference itself is kept). */
    fun onLogout() {
        if (_uiState.value.isUnlocking) return
        viewModelScope.launch { authRepository.logout() }
    }

    /** The error message was shown — drop it so it doesn't linger past the attempt. */
    fun onErrorShown() {
        _uiState.update { it.copy(errorRes = null) }
    }
}
