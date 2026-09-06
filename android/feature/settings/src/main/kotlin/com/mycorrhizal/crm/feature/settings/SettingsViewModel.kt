package com.mycorrhizal.crm.feature.settings

import android.content.Context
import android.content.Intent
import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.feature.tracking.PermissionChecker
import com.mycorrhizal.crm.feature.tracking.TrackingCatchUpScheduler
import com.mycorrhizal.crm.feature.tracking.TrackingPermissions
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

/** Which opt-in a runtime-permission request belongs to. */
enum class TrackingPermissionRequest {
    CALL_TRACKING,
    SMS_TRACKING,
}

/**
 * Why a tracking-permission dialog is showing (issue #721).
 * [Rationale] follows a plain denial — explain and offer a retry.
 * [AppSettings] follows a permanent denial ("don't ask again") — the only
 * remaining path is the system app-settings page.
 */
sealed interface TrackingPermissionDialog {
    data class Rationale(val request: TrackingPermissionRequest) : TrackingPermissionDialog
    data class AppSettings(val request: TrackingPermissionRequest) : TrackingPermissionDialog
}

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
    /**
     * Issue #721: a tracking toggle was switched on whose OS permissions are
     * not (all) granted — the UI must show the runtime permission dialog for
     * this request and report the outcome back via
     * [SettingsViewModel.onPermissionRequestResult]. Null when no system
     * dialog is pending.
     */
    val pendingPermissionRequest: TrackingPermissionRequest? = null,
    /** Issue #721: a tracking-permission denial dialog to render (or null). */
    val permissionDialog: TrackingPermissionDialog? = null,
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
    private val permissionChecker: PermissionChecker,
    private val catchUpScheduler: TrackingCatchUpScheduler,
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
        refreshPermissionState()
        viewModelScope.launch {
            appSettings.themePreference().collect { pref ->
                _uiState.update { it.copy(themePreference = pref) }
            }
        }
    }

    /**
     * Issue #721: reconcile the tracking toggles with the real OS grant state.
     *
     * The DataStore flag is only ever true while the matching permissions are
     * granted. If the flag is true but a permission was revoked in system
     * settings since, tracking is *off*: the flag is persisted false, the
     * foreground detection service stops, and the toggle renders off — a
     * toggle that claims "on" while capturing nothing would be lying. Called
     * on ViewModel creation and again whenever the screen resumes (a revoke
     * while the Settings screen is open, followed by a return from system
     * settings, must flip the toggle immediately).
     */
    fun refreshPermissionState() {
        viewModelScope.launch {
            val callStored = trackingSettings.callTrackingEnabled()
            val smsStored = trackingSettings.smsTrackingEnabled()
            val callGranted = callPermissionsGranted()
            val smsGranted = smsPermissionsGranted()

            if (callStored && !callGranted) {
                trackingSettings.setCallTrackingEnabled(false)
                stopCallDetectionService()
            }
            if (smsStored && !smsGranted) {
                trackingSettings.setSmsTrackingEnabled(false)
            }

            _uiState.update {
                it.copy(
                    callTrackingEnabled = callStored && callGranted,
                    smsTrackingEnabled = smsStored && smsGranted,
                    notificationsEnabled = trackingSettings.notificationsEnabled(),
                )
            }
        }
    }

    // --- tracking toggles (issue #721) ---

    /**
     * Turning call tracking on only takes effect once READ_CALL_LOG +
     * READ_PHONE_STATE are granted. Already granted → enable immediately;
     * otherwise ask the UI to show the runtime permission dialog (the toggle
     * itself stays off until [onPermissionRequestResult] reports the grant).
     */
    fun setCallTrackingEnabled(enabled: Boolean) {
        if (_uiState.value.pendingPermissionRequest != null && enabled) return
        if (!enabled) {
            // A disable wins over an in-flight request *for the same feature*:
            // the system dialog is modal, so a toggle-off while one is up is a
            // deliberate cancel — clearing the pending request also makes a
            // late dialog outcome (which the OS still delivers) a no-op in
            // onPermissionRequestResult.
            _uiState.update {
                it.copy(
                    callTrackingEnabled = false,
                    pendingPermissionRequest = if (_uiState.value.pendingPermissionRequest == TrackingPermissionRequest.CALL_TRACKING) {
                        null
                    } else {
                        _uiState.value.pendingPermissionRequest
                    },
                )
            }
            viewModelScope.launch {
                trackingSettings.setCallTrackingEnabled(false)
                stopCallDetectionService()
            }
            return
        }
        if (callPermissionsGranted()) {
            enableCallTracking()
        } else {
            _uiState.update { it.copy(pendingPermissionRequest = TrackingPermissionRequest.CALL_TRACKING) }
        }
    }

    /**
     * SMS tracking mirrors [setCallTrackingEnabled] with the READ_SMS +
     * RECEIVE_SMS grant set.
     */
    fun setSmsTrackingEnabled(enabled: Boolean) {
        if (_uiState.value.pendingPermissionRequest != null && enabled) return
        if (!enabled) {
            _uiState.update {
                it.copy(
                    smsTrackingEnabled = false,
                    pendingPermissionRequest = if (_uiState.value.pendingPermissionRequest == TrackingPermissionRequest.SMS_TRACKING) {
                        null
                    } else {
                        _uiState.value.pendingPermissionRequest
                    },
                )
            }
            viewModelScope.launch { trackingSettings.setSmsTrackingEnabled(false) }
            return
        }
        if (smsPermissionsGranted()) {
            enableSmsTracking()
        } else {
            _uiState.update { it.copy(pendingPermissionRequest = TrackingPermissionRequest.SMS_TRACKING) }
        }
    }

    fun setNotificationsEnabled(enabled: Boolean) {
        _uiState.update { it.copy(notificationsEnabled = enabled) }
        viewModelScope.launch { trackingSettings.setNotificationsEnabled(enabled) }
    }

    /**
     * The OS permission dialog for [request] closed. The grant is re-read
     * from the real permission state (the most reliable source — the result
     * map and the checker can diverge on partial grants). Granted → enable
     * (persist flag, start capture, kick the immediate catch-up). Denied →
     * leave the flag false and raise the rationale dialog, or the
     * permanent-denial "open system settings" dialog when [permanentlyDenied]
     * (the user checked "don't ask again", so the OS will not show the dialog
     * again).
     */
    fun onPermissionRequestResult(request: TrackingPermissionRequest, permanentlyDenied: Boolean) {
        // A disable issued while the (modal) system dialog was up cleared the
        // pending request; the OS still delivers the outcome, but the cancel
        // wins — only a request we are still waiting on is acted on.
        if (_uiState.value.pendingPermissionRequest != request) return
        _uiState.update { it.copy(pendingPermissionRequest = null) }
        val granted = when (request) {
            TrackingPermissionRequest.CALL_TRACKING -> callPermissionsGranted()
            TrackingPermissionRequest.SMS_TRACKING -> smsPermissionsGranted()
        }
        if (granted) {
            when (request) {
                TrackingPermissionRequest.CALL_TRACKING -> enableCallTracking()
                TrackingPermissionRequest.SMS_TRACKING -> enableSmsTracking()
            }
        } else {
            _uiState.update {
                it.copy(
                    permissionDialog = if (permanentlyDenied) {
                        TrackingPermissionDialog.AppSettings(request)
                    } else {
                        TrackingPermissionDialog.Rationale(request)
                    },
                )
            }
        }
    }

    /** "Not now" (or the system back button) on a tracking-permission dialog. */
    fun onPermissionDialogDismiss() {
        _uiState.update { it.copy(permissionDialog = null) }
    }

    /** "Try again" on a rationale dialog — re-request the same permission set. */
    fun onPermissionDialogRetry() {
        val request = (_uiState.value.permissionDialog as? TrackingPermissionDialog.Rationale)?.request ?: return
        _uiState.update {
            it.copy(permissionDialog = null, pendingPermissionRequest = request)
        }
    }

    private fun callPermissionsGranted(): Boolean =
        TrackingPermissions.CALL_TRACKING.all { permissionChecker.isGranted(it) }

    private fun smsPermissionsGranted(): Boolean =
        TrackingPermissions.SMS_TRACKING.all { permissionChecker.isGranted(it) }

    private fun enableCallTracking() {
        _uiState.update { it.copy(callTrackingEnabled = true) }
        viewModelScope.launch {
            trackingSettings.setCallTrackingEnabled(true)
            startCallDetectionService()
            // Issue #721 scope #5: don't wait for the periodic cadence or the
            // next call — stage recent call-log history right after the grant.
            catchUpScheduler.enqueueCallLogCatchUp()
        }
    }

    private fun enableSmsTracking() {
        _uiState.update { it.copy(smsTrackingEnabled = true) }
        viewModelScope.launch {
            trackingSettings.setSmsTrackingEnabled(true)
            // Issue #721 scope #5: stage recent outgoing texts right after the grant.
            catchUpScheduler.enqueueSmsBackfill()
        }
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
