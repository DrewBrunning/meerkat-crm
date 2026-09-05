package com.mycorrhizal.crm.feature.settings

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.model.network.TwoFactorSetupResponse
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.network.toApiError
import com.mycorrhizal.crm.ui.R
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Which code-gated action is prompting for a verification code. */
enum class TwoFactorPrompt { DISABLE, REGENERATE }

/**
 * Two-factor (TOTP) enrollment and management state (issue #158 web parity,
 * Android #814). Mirrors web `TwoFactorSettings.tsx`:
 *  - status → enable (setup mints a secret/QR, confirm with a live code) or
 *    regenerate/disable (each gated on a live code);
 *  - recovery codes are shown plaintext exactly once ([recoveryCodes]) and
 *    the secret/URL in [setup] are transient — nothing 2FA-related is kept in
 *    the session or persisted beyond the normal bearer token.
 *
 * Error mapping mirrors web: a rejected code (400) maps to the localized
 * "Invalid code" text; setup's 403 (OIDC account) / 409 (already enabled) and
 * any 429 surface the server's own message.
 */
@HiltViewModel
class TwoFactorViewModel @Inject constructor(
    private val authRepository: AuthRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(TwoFactorUiState())
    val uiState: StateFlow<TwoFactorUiState> = _uiState.asStateFlow()

    init {
        load()
    }

    fun load() {
        if (_uiState.value.busy) return
        _uiState.update { it.copy(loading = true, error = null, errorRes = null) }
        viewModelScope.launch {
            authRepository.getTwoFactorStatus()
                .onSuccess { status ->
                    _uiState.update { it.copy(loading = false, enabled = status.enabled) }
                }
                .onFailure { e ->
                    _uiState.update { it.copy(loading = false, error = e.displayText()) }
                }
        }
    }

    /** Begin enrollment: mints a pending secret the wizard shows as QR + manual key. */
    fun startSetup() {
        if (_uiState.value.busy) return
        _uiState.update { it.copy(busy = true, error = null, errorRes = null) }
        viewModelScope.launch {
            authRepository.setupTwoFactor()
                .onSuccess { setup ->
                    _uiState.update { it.copy(busy = false, setup = setup) }
                }
                .onFailure { e ->
                    _uiState.update { it.copy(busy = false, error = e.displayText()) }
                }
        }
    }

    /** Confirm enrollment with a live TOTP code; on success 2FA is on and the recovery codes are shown once. */
    fun confirmSetup(code: String) {
        if (_uiState.value.busy || _uiState.value.setup == null || code.isBlank()) return
        _uiState.update { it.copy(busy = true, error = null, errorRes = null) }
        viewModelScope.launch {
            authRepository.confirmTwoFactor(code.trim())
                .onSuccess { result ->
                    _uiState.update {
                        it.copy(
                            busy = false,
                            setup = null,
                            enabled = true,
                            recoveryCodes = result.recoveryCodes,
                        )
                    }
                }
                .onFailure { e ->
                    _uiState.update { it.copy(busy = false).withCodeError(e) }
                }
        }
    }

    /** Close the enrollment wizard without confirming (the pending secret is dropped). */
    fun closeSetup() {
        if (_uiState.value.busy) return
        _uiState.update { it.copy(setup = null, error = null, errorRes = null) }
    }

    fun requestDisable() = requestPrompt(TwoFactorPrompt.DISABLE)

    fun requestRegenerate() = requestPrompt(TwoFactorPrompt.REGENERATE)

    private fun requestPrompt(prompt: TwoFactorPrompt) {
        if (_uiState.value.busy) return
        _uiState.update { it.copy(prompt = prompt, error = null, errorRes = null) }
    }

    fun dismissPrompt() {
        if (_uiState.value.busy) return
        _uiState.update { it.copy(prompt = null, error = null, errorRes = null) }
    }

    /**
     * Submit the live code the current [TwoFactorPrompt] asked for: disable
     * turns 2FA off; regenerate mints a fresh set of recovery codes (shown
     * once).
     */
    fun submitPromptCode(code: String) {
        val prompt = _uiState.value.prompt ?: return
        if (_uiState.value.busy || code.isBlank()) return
        _uiState.update { it.copy(busy = true, error = null, errorRes = null) }
        viewModelScope.launch {
            when (prompt) {
                TwoFactorPrompt.DISABLE ->
                    authRepository.disableTwoFactor(code.trim())
                        .onSuccess {
                            _uiState.update { it.copy(busy = false, prompt = null, enabled = false) }
                        }
                        .onFailure { e ->
                            _uiState.update { it.copy(busy = false).withCodeError(e) }
                        }
                TwoFactorPrompt.REGENERATE ->
                    authRepository.regenerateRecoveryCodes(code.trim())
                        .onSuccess { result ->
                            _uiState.update {
                                it.copy(
                                    busy = false,
                                    prompt = null,
                                    recoveryCodes = result.recoveryCodes.takeIf { codes -> codes.isNotEmpty() },
                                )
                            }
                        }
                        .onFailure { e ->
                            _uiState.update { it.copy(busy = false).withCodeError(e) }
                        }
            }
        }
    }

    /** Dismiss the exactly-once recovery-codes dialog. */
    fun dismissRecoveryCodes() {
        _uiState.update { it.copy(recoveryCodes = null) }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(error = null, errorRes = null) }
    }

    private fun TwoFactorUiState.withCodeError(error: Throwable): TwoFactorUiState {
        val apiError = error.toApiError()
        return if (apiError is ApiError.Client && apiError.code == 400) {
            copy(errorRes = R.string.settings_two_factor_invalid_code)
        } else {
            copy(error = error.displayText())
        }
    }
}

data class TwoFactorUiState(
    /** null while the initial status is still loading. */
    val enabled: Boolean? = null,
    val loading: Boolean = true,
    /** True while a mutation (setup/confirm/disable/regenerate) is in flight. */
    val busy: Boolean = false,
    /** The pending setup wizard (secret + otpauth URL), transient. */
    val setup: TwoFactorSetupResponse? = null,
    /** The just-minted recovery codes — shown exactly once, then dismissed. */
    val recoveryCodes: List<String>? = null,
    /** Which code-gated action is prompting (disable/regenerate). */
    val prompt: TwoFactorPrompt? = null,
    /** A transient action error (server text), shown and then cleared. */
    val error: String? = null,
    /** A localized action error (rejected code), shown and then cleared. */
    @StringRes val errorRes: Int? = null,
)

private fun Throwable.displayText(): String {
    val apiError = this as? ApiError ?: return message ?: "error"
    // 403 maps to a generic permission message in ApiError.displayMessage, but
    // the OIDC-account "2FA unavailable" reason is the informative part.
    return if (apiError is ApiError.Client && apiError.code == 403) {
        apiError.message ?: apiError.displayMessage
    } else {
        apiError.displayMessage
    }
}
