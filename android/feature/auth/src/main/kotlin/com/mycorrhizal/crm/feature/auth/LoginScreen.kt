package com.mycorrhizal.crm.feature.auth

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.autofill.ContentType
import androidx.compose.ui.platform.LocalAutofillManager
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.mycorrhizal.crm.ui.R
import com.mycorrhizal.crm.ui.components.AutofillOutlinedTextField

@Composable
fun LoginScreen(
    onLoggedIn: () -> Unit,
    onSignInWithSso: (String) -> Unit = {},
    // M26: links to the register and forgot-password flows.
    onRegisterClick: () -> Unit = {},
    onForgotPasswordClick: () -> Unit = {},
    // #203: the OIDC native-return failure (MainActivity) has no ViewModel of
    // its own to report through — it's a session-level event injected from
    // outside, shown through this screen's existing SnackbarHostState.
    oidcError: String? = null,
    onOidcErrorShown: () -> Unit = {},
    viewModel: LoginViewModel = viewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val autofillManager = LocalAutofillManager.current

    LaunchedEffect(Unit) {
        viewModel.events.collect { event ->
            when (event) {
                LoginEvent.LoggedIn -> {
                    // Nothing tagged ContentType.NewUsername/NewPassword here
                    // (this is sign-in, not account creation — see
                    // RegisterScreen for that), so there's no "new credential"
                    // capture step. commit() is still what tells the platform
                    // the just-submitted Username/Password fields were used
                    // successfully, which is what triggers the "save to
                    // password manager" prompt for a first-time login.
                    //
                    // N8 (#814): LoggedIn is only emitted once the WHOLE flow
                    // succeeds — for a 2FA account that is after the code step,
                    // never after the password step alone (the server would
                    // have rejected a stale save anyway).
                    autofillManager?.commit()
                    onLoggedIn()
                }
                LoginEvent.ServerUrlUpdated -> Unit
            }
        }
    }

    LoginScreenContent(
        uiState = state,
        onServerUrlChange = viewModel::onServerUrlChange,
        onModeChange = viewModel::onModeChange,
        onSubmit = viewModel::onSubmit,
        onTwoFactorSubmit = viewModel::onSubmitTwoFactorCode,
        onBackToCredentials = viewModel::onBackToCredentials,
        onSignInWithSso = onSignInWithSso,
        onRegisterClick = onRegisterClick,
        onForgotPasswordClick = onForgotPasswordClick,
        onErrorShown = viewModel::onErrorShown,
        oidcError = oidcError,
        onOidcErrorShown = onOidcErrorShown,
    )
}

/**
 * Stateless login form — the screen's canonical states are testable directly
 * (ticket §10.4). Field text lives in local `remember` state; credentials
 * (password/API token/2FA code) are never saveable and are passed up on submit.
 * When [LoginUiState.twoFactorStep] is set the credentials form is replaced by
 * the two-factor code step (mirrors web LoginPage.tsx's step machine).
 */
@Composable
fun LoginScreenContent(
    uiState: LoginUiState,
    onServerUrlChange: (String) -> Unit,
    onModeChange: (LoginMode) -> Unit,
    onSubmit: (serverUrl: String, identifier: String, password: String, apiToken: String) -> Unit,
    onTwoFactorSubmit: (String) -> Unit = {},
    onBackToCredentials: () -> Unit = {},
    onSignInWithSso: (String) -> Unit = {},
    onRegisterClick: () -> Unit = {},
    onForgotPasswordClick: () -> Unit = {},
    onErrorShown: () -> Unit = {},
    oidcError: String? = null,
    onOidcErrorShown: () -> Unit = {},
) {
    val snackbarHostState = remember { SnackbarHostState() }

    var serverUrl by rememberSaveable { mutableStateOf(uiState.serverUrl) }
    var identifier by rememberSaveable { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var apiToken by remember { mutableStateOf("") }
    var twoFactorCode by remember { mutableStateOf("") }
    // N8 (#814): a stale code must not survive leaving the step (a consumed
    // challenge is single-use, and the code is transient by design).
    LaunchedEffect(uiState.twoFactorStep) {
        if (!uiState.twoFactorStep) twoFactorCode = ""
    }

    Scaffold(
        // #203: Toast is announced inconsistently by TalkBack and can't be
        // re-read — an assertive live region on the SnackbarHost makes sure
        // both this screen's own errors and the injected oidcError below are
        // actually spoken when they appear.
        snackbarHost = {
            SnackbarHost(
                snackbarHostState,
                modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(24.dp)
                .verticalScroll(rememberScrollState()),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Image(
                painter = painterResource(id = R.drawable.ic_brand_logo),
                contentDescription = stringResource(R.string.app_name),
                modifier = Modifier.size(96.dp),
            )
            Text(
                text = stringResource(R.string.app_name),
                style = MaterialTheme.typography.titleLarge,
            )

            if (uiState.twoFactorStep) {
                TwoFactorLoginStep(
                    code = twoFactorCode,
                    onCodeChange = { twoFactorCode = it },
                    onTwoFactorSubmit = { onTwoFactorSubmit(twoFactorCode) },
                    onBackToCredentials = onBackToCredentials,
                    isLoading = uiState.isLoading,
                )
            } else {
                Text(
                    text = stringResource(R.string.login_connect_to_server),
                    style = MaterialTheme.typography.bodyLarge,
                )

                OutlinedTextField(
                    value = serverUrl,
                    onValueChange = { serverUrl = it; onServerUrlChange(it) },
                    label = { Text(stringResource(R.string.login_server_url)) },
                    placeholder = { Text(stringResource(R.string.login_server_url_hint)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                    modifier = Modifier.fillMaxWidth(),
                )

                SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                    SegmentedButton(
                        selected = uiState.mode == LoginMode.PASSWORD,
                        onClick = { onModeChange(LoginMode.PASSWORD) },
                        shape = SegmentedButtonDefaults.itemShape(index = 0, count = 2),
                    ) { Text(stringResource(R.string.login_mode_password)) }
                    SegmentedButton(
                        selected = uiState.mode == LoginMode.API_TOKEN,
                        onClick = { onModeChange(LoginMode.API_TOKEN) },
                        shape = SegmentedButtonDefaults.itemShape(index = 1, count = 2),
                    ) { Text(stringResource(R.string.login_api_token)) }
                }

                if (uiState.mode == LoginMode.PASSWORD) {
                    // The field accepts either a username or an email, so offer
                    // both content types — a password manager matches whichever
                    // it has saved for this site.
                    AutofillOutlinedTextField(
                        value = identifier,
                        onValueChange = { identifier = it },
                        label = stringResource(R.string.login_username_or_email),
                        contentType = ContentType.Username + ContentType.EmailAddress,
                    )
                    AutofillOutlinedTextField(
                        value = password,
                        onValueChange = { password = it },
                        label = stringResource(R.string.login_mode_password),
                        contentType = ContentType.Password,
                        visualTransformation = PasswordVisualTransformation(),
                    )
                } else {
                    OutlinedTextField(
                        value = apiToken,
                        onValueChange = { apiToken = it },
                        label = { Text(stringResource(R.string.login_api_token)) },
                        placeholder = { Text(stringResource(R.string.login_api_token_hint)) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }

                if (uiState.isLoading) {
                    // #203: no Button is present in this branch, so the
                    // stateDescription-on-a-Button pattern used elsewhere doesn't
                    // apply here — the spinner itself needs the name.
                    val savingLabel = stringResource(R.string.a11y_state_saving)
                    CircularProgressIndicator(
                        modifier = Modifier.semantics { contentDescription = savingLabel },
                    )
                } else {
                    Button(
                        onClick = {
                            onSubmit(serverUrl, identifier, password, apiToken)
                        },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(stringResource(R.string.login_sign_in))
                    }
                }

                TextButton(
                    onClick = { onSignInWithSso(serverUrl) },
                    enabled = serverUrl.isNotBlank(),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.login_sso))
                }

                // M26: account creation + password reset live on the same screen
                // as sign-in on web (LoginPage.tsx links both), so Android keeps
                // them together here too.
                TextButton(
                    onClick = onRegisterClick,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.login_no_account))
                }
                TextButton(
                    onClick = onForgotPasswordClick,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.login_forgot_password))
                }
            }

            val errorMessage = uiState.errorRes?.let { stringResource(it) } ?: uiState.error
            errorMessage?.let { message ->
                LaunchedEffect(message) {
                    snackbarHostState.showSnackbar(message)
                    onErrorShown()
                }
            }

            // #203: the OIDC native-return failure, injected from MainActivity
            // — was a Toast, now shares this screen's own SnackbarHostState.
            oidcError?.let { message ->
                LaunchedEffect(message) {
                    snackbarHostState.showSnackbar(message)
                    onOidcErrorShown()
                }
            }
        }
    }
}

/**
 * N8 (#814): step 2 of a two-factor login — a single code field (6-digit TOTP
 * or a XXXXX-XXXXX-XXXXX recovery code). Deliberately NOT a numeric-only
 * keyboard: recovery codes are alphanumeric and must be typeable, not just
 * pastable (the field stays paste-friendly for TOTP entry).
 */
@Composable
private fun TwoFactorLoginStep(
    code: String,
    onCodeChange: (String) -> Unit,
    onTwoFactorSubmit: () -> Unit,
    onBackToCredentials: () -> Unit,
    isLoading: Boolean,
) {
    Text(
        text = stringResource(R.string.login_two_factor_title),
        style = MaterialTheme.typography.titleMedium,
        modifier = Modifier.semantics { heading() },
    )
    Text(
        text = stringResource(R.string.login_two_factor_description),
        style = MaterialTheme.typography.bodyLarge,
    )
    OutlinedTextField(
        value = code,
        onValueChange = onCodeChange,
        label = { Text(stringResource(R.string.login_two_factor_code_label)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii),
        modifier = Modifier.fillMaxWidth(),
    )
    if (isLoading) {
        val savingLabel = stringResource(R.string.a11y_state_saving)
        CircularProgressIndicator(
            modifier = Modifier.semantics { contentDescription = savingLabel },
        )
    } else {
        Button(
            onClick = onTwoFactorSubmit,
            enabled = code.isNotBlank(),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(stringResource(R.string.login_sign_in))
        }
        TextButton(
            onClick = onBackToCredentials,
            enabled = !isLoading,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(stringResource(R.string.login_two_factor_back))
        }
    }
}
