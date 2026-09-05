package com.mycorrhizal.crm.feature.auth

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.usecase.LoginUseCase
import com.mycorrhizal.crm.domain.usecase.LoginWithApiTokenUseCase
import com.mycorrhizal.crm.model.util.Validators
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.network.toApiError
import com.mycorrhizal.crm.ui.R
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

data class LoginUiState(
    val serverUrl: String = "",
    val mode: LoginMode = LoginMode.PASSWORD,
    val isLoading: Boolean = false,
    /** Static validation error as a string resource id, resolved in the UI. */
    @StringRes val errorRes: Int? = null,
    /** Dynamic server error text (e.g. "Invalid credentials"). */
    val error: String? = null,
    // N8 (#814): the password step succeeded but the account has 2FA — the UI
    // swaps to a code-entry step until the TOTP/recovery code clears.
    val twoFactorStep: Boolean = false,
)

enum class LoginMode { PASSWORD, API_TOKEN }

sealed interface LoginEvent {
    data object LoggedIn : LoginEvent
    data object ServerUrlUpdated : LoginEvent
}

@HiltViewModel
class LoginViewModel @Inject constructor(
    private val loginUseCase: LoginUseCase,
    private val loginWithApiTokenUseCase: LoginWithApiTokenUseCase,
    private val sessionManager: SessionManager,
    // N8 (#814): the 2FA code step. It talks to the repository directly (not a
    // use case) because the HTTP status it must distinguish — 400 invalid
    // code vs 401 expired challenge vs 429 lockout — is an ApiError concept
    // that core:domain deliberately does not depend on (see RegisterViewModel
    // for the same direct-repository pattern).
    private val authRepository: AuthRepository,
) : ViewModel() {

    private val _uiState = MutableStateFlow(LoginUiState())
    val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    private val _events = Channel<LoginEvent>(Channel.BUFFERED)
    val events = _events.receiveAsFlow()

    fun onServerUrlChange(value: String) {
        _uiState.update { it.copy(serverUrl = value) }
        // Persist immediately (not just on submit): the register and
        // forgot-password flows are reached from this screen and read the
        // server URL from the session manager (M26).
        viewModelScope.launch { sessionManager.setServerUrl(value.trim().trimEnd('/')) }
    }

    fun onModeChange(mode: LoginMode) {
        _uiState.update { it.copy(mode = mode, errorRes = null, error = null) }
    }

    /**
     * Authenticate with the given credentials. The values are deliberately
     * passed in rather than stored in [LoginUiState] — a password or API token
     * must not linger in ViewModel state after the attempt (security).
     */
    fun onSubmit(
        serverUrl: String,
        identifier: String,
        password: String,
        apiToken: String,
    ) {
        if (_uiState.value.isLoading) return

        val trimmedUrl = serverUrl.trim().trimEnd('/')
        if (!Validators.isValidServerUrl(trimmedUrl)) {
            _uiState.update {
                it.copy(errorRes = R.string.login_error_valid_server_url, error = null)
            }
            return
        }

        // Field-level validation (blank checks, token prefix) is a UI concern —
        // these are localized here, not in the domain use cases.
        val validationError = when (_uiState.value.mode) {
            LoginMode.PASSWORD -> when {
                identifier.isBlank() -> R.string.login_error_identifier_required
                password.isBlank() -> R.string.login_error_password_required
                else -> null
            }
            LoginMode.API_TOKEN -> when {
                apiToken.isBlank() -> R.string.login_error_token_required
                !apiToken.startsWith("mycorrhizal_") -> R.string.login_error_token_prefix
                else -> null
            }
        }
        if (validationError != null) {
            _uiState.update { it.copy(errorRes = validationError, error = null) }
            return
        }

        _uiState.update { it.copy(isLoading = true, errorRes = null, error = null) }
        viewModelScope.launch {
            sessionManager.setServerUrl(trimmedUrl)
            _events.send(LoginEvent.ServerUrlUpdated)

            val result = when (_uiState.value.mode) {
                LoginMode.PASSWORD -> loginUseCase(identifier, password)
                LoginMode.API_TOKEN -> loginWithApiTokenUseCase(apiToken)
            }

            when (result) {
                is LoginUseCase.Result.Success -> {
                    _uiState.update { it.copy(isLoading = false) }
                    _events.send(LoginEvent.LoggedIn)
                }
                // N8 (#814): 2FA account — the password was correct but no
                // session exists yet. Stay on this screen's code step.
                is LoginUseCase.Result.TwoFactorRequired -> {
                    _uiState.update { it.copy(isLoading = false, twoFactorStep = true, errorRes = null, error = null) }
                }
                is LoginUseCase.Result.Failure -> {
                    _uiState.update { it.copy(isLoading = false, error = result.message) }
                }
                is LoginWithApiTokenUseCase.Result.Success -> {
                    _uiState.update { it.copy(isLoading = false) }
                    _events.send(LoginEvent.LoggedIn)
                }
                is LoginWithApiTokenUseCase.Result.Failure -> {
                    _uiState.update { it.copy(isLoading = false, error = result.message) }
                }
            }
        }
    }

    /** The user left the 2FA code step (back button) — return to the credentials form. */
    fun onBackToCredentials() {
        if (_uiState.value.isLoading) return
        _uiState.update { it.copy(twoFactorStep = false, errorRes = null, error = null) }
    }

    /**
     * Step 2 of a 2FA login: exchange the TOTP/recovery code for a session.
     * The transient 2fa_pending challenge is held in the repository (memory
     * only) — the code itself is passed in and never stored in [LoginUiState].
     *
     * Error mapping mirrors web `auth.ts login2FA` + `LoginPage.tsx`:
     *  - 400 wrong code → localized "Invalid code", stay on the step.
     *  - 401 challenge consumed/expired/disabled → back to the credentials
     *    step with a "sign in again" message (a retry here would always 401).
     *  - 429 lockout → the server's lockout text verbatim; stop grinding.
     */
    fun onSubmitTwoFactorCode(code: String) {
        if (_uiState.value.isLoading || code.isBlank()) return
        _uiState.update { it.copy(isLoading = true, errorRes = null, error = null) }
        viewModelScope.launch {
            val outcome = authRepository.complete2faLogin(code.trim())
            outcome.fold(
                onSuccess = {
                    _uiState.update { it.copy(isLoading = false) }
                    _events.send(LoginEvent.LoggedIn)
                },
                onFailure = { error ->
                    handleTwoFactorFailure(error)
                },
            )
        }
    }

    private fun handleTwoFactorFailure(error: Throwable) {
        val apiError = error.toApiError()
        val base = LoginUiState(
            serverUrl = _uiState.value.serverUrl,
            mode = LoginMode.PASSWORD,
            isLoading = false,
            twoFactorStep = true,
        )
        _uiState.value = when {
            apiError is ApiError.Client && apiError.code == 401 ->
                // The 10-minute challenge expired (or was already consumed /
                // 2FA was disabled) — a retry here can never succeed.
                base.copy(twoFactorStep = false, errorRes = R.string.login_error_two_factor_expired)
            apiError is ApiError.Client && apiError.code == 429 ->
                // Lockout: surface the server's message and stop (web parity).
                base.copy(error = apiError.displayMessage)
            else ->
                // 400 wrong code and any transient/network failure keep the
                // user on the step with the localized "invalid code" copy.
                base.copy(errorRes = R.string.login_error_two_factor_invalid)
        }
    }

    fun onErrorShown() {
        _uiState.update { it.copy(errorRes = null, error = null) }
    }
}
