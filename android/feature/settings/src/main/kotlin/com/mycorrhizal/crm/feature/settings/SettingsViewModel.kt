package com.mycorrhizal.crm.feature.settings

import android.content.Context
import android.content.Intent
import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.network.foldApiError
import com.mycorrhizal.crm.ui.R
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SettingsUiState(
    val session: SessionState = SessionState(),
    val isLoggingOut: Boolean = false,
    val callTrackingEnabled: Boolean = false,
    val smsTrackingEnabled: Boolean = false,
    val notificationsEnabled: Boolean = true,
    val themePreference: String = AppSettingsRepository.THEME_SYSTEM,
    val isChangingPassword: Boolean = false,
    /** Static password validation error as a string resource id, resolved in the UI (mirrors LoginViewModel). */
    @StringRes val passwordErrorRes: Int? = null,
    /** Dynamic password-change error text (server message). */
    val passwordError: String? = null,
    /** T104: an in-flight suggest-relationships run. */
    val isSuggestingRelationships: Boolean = false,
    /** Number of relationship edges the last suggest run newly created (null = not yet run). */
    val suggestedRelationshipCount: Int? = null,
    @StringRes val relationshipSuggestErrorRes: Int? = null,
    // Issue #722: the opt-in local app lock.
    val requireLocalAuth: Boolean = false,
    val autoLockDelay: AutoLockDelay = AutoLockDelay.DEFAULT,
    /** Whether the device can currently satisfy the local gate (strong biometric or secure lock screen). */
    val localAuthSupported: Boolean = true,
)

sealed interface SettingsEvent {
    data object LoggedOut : SettingsEvent

    /** A language change was persisted — the Activity must recreate to re-resolve resources. */
    data object LocaleChanged : SettingsEvent

    /** Password change succeeded; the server invalidated every JWT, so the user must re-login. */
    data object PasswordChanged : SettingsEvent
}

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val authRepository: AuthRepository,
    private val trackingSettings: TrackingSettingsRepository,
    private val appSettings: AppSettingsRepository,
    private val relationshipEdgeRepository: RelationshipEdgeRepository,
    private val localAuthSettings: LocalAuthSettingsRepository,
    private val localAuthCapabilities: LocalAuthCapabilities,
    @ApplicationContext private val appContext: Context,
) : ViewModel() {

    private val _uiState = MutableStateFlow(SettingsUiState())
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    private val _events = MutableStateFlow<SettingsEvent?>(null)
    val events: StateFlow<SettingsEvent?> = _events

    init {
        viewModelScope.launch {
            authRepository.observeSession().collect { session ->
                _uiState.update { it.copy(session = session) }
            }
        }
        viewModelScope.launch {
            _uiState.update {
                it.copy(
                    callTrackingEnabled = trackingSettings.callTrackingEnabled(),
                    smsTrackingEnabled = trackingSettings.smsTrackingEnabled(),
                    notificationsEnabled = trackingSettings.notificationsEnabled(),
                )
            }
        }
        viewModelScope.launch {
            appSettings.themePreference().collect { pref ->
                _uiState.update { it.copy(themePreference = pref) }
            }
        }
        // Issue #722: the app-lock opt-in + its timeout, collected so the
        // toggle follows the persisted value live. The capability check is a
        // one-shot read (device posture doesn't change mid-session).
        viewModelScope.launch {
            localAuthSettings.requireLocalAuth().collect { enabled ->
                _uiState.update { it.copy(requireLocalAuth = enabled) }
            }
        }
        viewModelScope.launch {
            localAuthSettings.autoLockDelay().collect { delay ->
                _uiState.update { it.copy(autoLockDelay = delay) }
            }
        }
        _uiState.update { it.copy(localAuthSupported = localAuthCapabilities.canEnableLocalAuth()) }
    }

    fun setCallTrackingEnabled(enabled: Boolean) {
        _uiState.update { it.copy(callTrackingEnabled = enabled) }
        viewModelScope.launch {
            trackingSettings.setCallTrackingEnabled(enabled)
            if (enabled) {
                startCallDetectionService()
            } else {
                stopCallDetectionService()
            }
        }
    }

    fun setSmsTrackingEnabled(enabled: Boolean) {
        _uiState.update { it.copy(smsTrackingEnabled = enabled) }
        viewModelScope.launch { trackingSettings.setSmsTrackingEnabled(enabled) }
    }

    fun setNotificationsEnabled(enabled: Boolean) {
        _uiState.update { it.copy(notificationsEnabled = enabled) }
        viewModelScope.launch { trackingSettings.setNotificationsEnabled(enabled) }
    }

    // --- Issue #722: the opt-in local app lock ---

    /**
     * Toggle the "require biometric / device PIN to open the app" preference.
     * The toggle itself is disabled (never shown interactively) when the
     * device cannot satisfy the gate, so the guard below is defensive only.
     * Enabling never locks the current session — it applies to the next cold
     * start or the next time the app is backgrounded past the grace period.
     */
    fun setRequireLocalAuth(enabled: Boolean) {
        if (enabled && !_uiState.value.localAuthSupported) return
        _uiState.update { it.copy(requireLocalAuth = enabled) }
        viewModelScope.launch { localAuthSettings.setRequireLocalAuth(enabled) }
    }

    /** Change how long the app may sit in the background before re-locking. */
    fun setAutoLockDelay(delay: AutoLockDelay) {
        if (!_uiState.value.requireLocalAuth) return
        _uiState.update { it.copy(autoLockDelay = delay) }
        viewModelScope.launch { localAuthSettings.setAutoLockDelay(delay) }
    }

    // --- M25: profile & channels ---

    /**
     * Persist the language server-side and locally, then request a locale
     * change. The activity recreates on [SettingsEvent.LocaleChanged] so the
     * `values-XX` resources resolve in the new language without a restart.
     */
    fun updateLanguage(language: String) {
        val current = _uiState.value.session.language
        if (language == current) return
        viewModelScope.launch {
            authRepository.updateLanguage(language).onSuccess {
                appSettings.setLanguageOverride(language)
                _events.value = SettingsEvent.LocaleChanged
            }
        }
    }

    /** Persist the date-format preference server-side; SessionState re-emits so screens update live. */
    fun updateDateFormat(dateFormat: String) {
        val current = _uiState.value.session.dateFormat
        if (dateFormat == current) return
        viewModelScope.launch {
            authRepository.updateDateFormat(dateFormat)
        }
    }

    fun setThemePreference(preference: String) {
        viewModelScope.launch {
            appSettings.setThemePreference(preference)
        }
    }

    /**
     * Change the password. [newPassword] must equal [confirmPassword]; the
     * mismatch is a UI validation error, never sent to the server. On success
     * the server invalidates every JWT, so the session is cleared and
     * [SettingsEvent.PasswordChanged] asks the user to sign in again. Passwords
     * never live in [SettingsUiState].
     */
    fun changePassword(currentPassword: String, newPassword: String, confirmPassword: String) {
        if (_uiState.value.isChangingPassword) return
        if (newPassword != confirmPassword) {
            _uiState.update { it.copy(passwordErrorRes = R.string.settings_password_mismatch, passwordError = null) }
            return
        }
        _uiState.update { it.copy(isChangingPassword = true, passwordErrorRes = null, passwordError = null) }
        viewModelScope.launch {
            authRepository.changePassword(currentPassword, newPassword)
                .onSuccess {
                    _uiState.update { it.copy(isChangingPassword = false) }
                    // Every JWT was invalidated server-side; the web re-issues a
                    // cookie, a bearer-token client cannot. Force a clean re-login.
                    authRepository.logout()
                    _events.value = SettingsEvent.PasswordChanged
                }
                .onFailure { error ->
                    _uiState.update {
                        it.copy(isChangingPassword = false, passwordError = error.displayMessage())
                    }
                }
        }
    }

    private fun Throwable.displayMessage(): String =
        (this as? com.mycorrhizal.crm.network.ApiError)?.displayMessage ?: message ?: "error"

    private fun startCallDetectionService() {
        // minSdk is 26, so startForegroundService is always the correct path
        // (the pre-O branch could never be taken — M5 §7 cleanup).
        appContext.startForegroundService(
            Intent(appContext, com.mycorrhizal.crm.feature.tracking.CallDetectionService::class.java),
        )
    }

    private fun stopCallDetectionService() {
        appContext.stopService(
            Intent(appContext, com.mycorrhizal.crm.feature.tracking.CallDetectionService::class.java),
        )
    }

    fun logout() {
        if (_uiState.value.isLoggingOut) return
        _uiState.update { it.copy(isLoggingOut = true) }
        viewModelScope.launch {
            authRepository.logout()
            _events.value = SettingsEvent.LoggedOut
        }
    }

    /** Clear the one-shot event so the screen's LaunchedEffect doesn't re-fire it. */
    fun onEventShown() {
        _events.value = null
    }

    /**
     * T104: run one round of graph inference over the user's confirmed edges.
     * Opt-in (button press), one round per press, idempotent. The generated
     * suggestions appear in the existing review surface — a contact's
     * Relationships screen shows its suggested edges.
     */
    fun suggestRelationships() {
        if (_uiState.value.isSuggestingRelationships) return
        _uiState.update { it.copy(isSuggestingRelationships = true, relationshipSuggestErrorRes = null) }
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
                onError = { _ ->
                    _uiState.update {
                        it.copy(
                            isSuggestingRelationships = false,
                            relationshipSuggestErrorRes = R.string.settings_suggest_relationships_error,
                        )
                    }
                },
            )
        }
    }

    /** Clear the T104 result banner so it doesn't linger on the next visit. */
    fun onRelationshipSuggestBannerShown() {
        _uiState.update { it.copy(suggestedRelationshipCount = null, relationshipSuggestErrorRes = null) }
    }
}
