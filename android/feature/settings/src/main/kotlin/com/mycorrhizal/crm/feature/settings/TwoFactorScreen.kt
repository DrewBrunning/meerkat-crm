package com.mycorrhizal.crm.feature.settings

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.model.network.TwoFactorSetupResponse
import com.mycorrhizal.crm.ui.R
import com.mycorrhizal.crm.ui.components.AccessibleIconButton

/**
 * Two-factor (TOTP) enrollment and management (issue #158 web parity,
 * Android #814), reached from Settings — mirrors web TwoFactorSettings.tsx:
 * status → enable wizard (QR + manual key, confirm with a live code) or
 * regenerate/disable (each gated on a live code); recovery codes are shown
 * exactly once after confirm/regenerate.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TwoFactorScreen(
    onBack: () -> Unit,
    viewModel: TwoFactorViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    AccessibleIconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.cd_back))
                    }
                },
                title = {
                    Text(stringResource(R.string.settings_two_factor_title), style = MaterialTheme.typography.titleLarge)
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                    navigationIconContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
    ) { padding ->
        TwoFactorContent(
            state = state,
            onEnable = viewModel::startSetup,
            onRegenerate = viewModel::requestRegenerate,
            onDisable = viewModel::requestDisable,
            onConfirmSetup = viewModel::confirmSetup,
            onCloseSetup = viewModel::closeSetup,
            onSubmitPromptCode = viewModel::submitPromptCode,
            onDismissPrompt = viewModel::dismissPrompt,
            onDismissRecoveryCodes = viewModel::dismissRecoveryCodes,
            modifier = Modifier.padding(padding),
        )
    }
}

@Composable
fun TwoFactorContent(
    state: TwoFactorUiState,
    onEnable: () -> Unit,
    onRegenerate: () -> Unit,
    onDisable: () -> Unit,
    onConfirmSetup: (String) -> Unit,
    onCloseSetup: () -> Unit,
    onSubmitPromptCode: (String) -> Unit,
    onDismissPrompt: () -> Unit,
    onDismissRecoveryCodes: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        when {
            state.loading && state.enabled == null -> {
                Box(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            }
            state.enabled == true -> {
                Text(
                    text = stringResource(R.string.settings_two_factor_enabled_badge),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.tertiary,
                )
                Text(
                    text = stringResource(R.string.settings_two_factor_enabled_description),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    OutlinedButton(onClick = onRegenerate, enabled = !state.busy) {
                        Text(stringResource(R.string.settings_two_factor_regenerate_button))
                    }
                    OutlinedButton(onClick = onDisable, enabled = !state.busy) {
                        Text(stringResource(R.string.settings_two_factor_disable_button))
                    }
                }
            }
            else -> {
                Text(
                    text = stringResource(R.string.settings_two_factor_description),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                val enablingLabel = stringResource(R.string.a11y_state_saving)
                Button(
                    onClick = onEnable,
                    enabled = !state.busy,
                    modifier = Modifier
                        .fillMaxWidth()
                        .semantics { if (state.busy) stateDescription = enablingLabel },
                ) {
                    if (state.busy) {
                        CircularProgressIndicator(modifier = Modifier.size(18.dp))
                    }
                    Text(stringResource(R.string.settings_two_factor_enable_button))
                }
            }
        }

        // A dialog-level error (rejected code during setup/prompt) is shown
        // inside the dialog; only surface it here when no dialog is up, so a
        // live-region read doesn't announce it twice.
        val dialogOpen = state.setup != null || state.prompt != null || state.recoveryCodes != null
        val errorText = if (dialogOpen) null else state.errorRes?.let { stringResource(it) } ?: state.error
        errorText?.let { text ->
            Text(
                text = text,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
            )
        }
    }

    state.setup?.let { setup ->
        EnrollmentSetupDialog(
            setup = setup,
            busy = state.busy,
            error = state.error,
            errorRes = state.errorRes,
            onConfirm = onConfirmSetup,
            onDismiss = onCloseSetup,
        )
    }

    state.prompt?.let { prompt ->
        CodePromptDialog(
            prompt = prompt,
            busy = state.busy,
            error = state.error,
            errorRes = state.errorRes,
            onConfirm = onSubmitPromptCode,
            onDismiss = onDismissPrompt,
        )
    }

    state.recoveryCodes?.let { codes ->
        RecoveryCodesDialog(
            codes = codes,
            onDone = onDismissRecoveryCodes,
        )
    }
}

/** Issue #814 Phase 2: the enrollment wizard (QR + manual key + live-code confirm). */
@Composable
internal fun EnrollmentSetupDialog(
    setup: TwoFactorSetupResponse,
    busy: Boolean,
    error: String?,
    errorRes: Int?,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var code by remember { mutableStateOf("") }
    val qrBitmap = remember(setup.otpauthUrl) { QrCodeEncoder.encode(setup.otpauthUrl, 512) }

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(stringResource(R.string.settings_two_factor_setup_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = stringResource(R.string.settings_two_factor_setup_description),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                // The manual key below is the functional path (it always
                // renders); the QR is a convenience for scanner apps. The image
                // is 180dp, sized for the standard scan distance.
                qrBitmap?.let { bitmap ->
                    Box(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                        Image(
                            bitmap = bitmap.asImageBitmap(),
                            contentDescription = stringResource(R.string.settings_two_factor_qr_content_description),
                            modifier = Modifier.size(180.dp),
                        )
                    }
                }
                OutlinedTextField(
                    value = setup.secret,
                    onValueChange = {},
                    readOnly = true,
                    singleLine = true,
                    label = { Text(stringResource(R.string.settings_two_factor_setup_manual_key)) },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = code,
                    onValueChange = { code = it },
                    singleLine = true,
                    label = { Text(stringResource(R.string.settings_two_factor_setup_code_label)) },
                    supportingText = { Text(stringResource(R.string.settings_two_factor_setup_code_help)) },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.fillMaxWidth(),
                )
                val message = errorRes?.let { stringResource(it) } ?: error
                message?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(code) },
                enabled = !busy && code.isNotBlank(),
            ) {
                Text(stringResource(if (busy) R.string.settings_two_factor_setup_enabling else R.string.settings_two_factor_setup_confirm_button))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss, enabled = !busy) {
                Text(stringResource(R.string.settings_cancel))
            }
        },
    )
}

/** Issue #814 Phase 2: live-code gate for disable / regenerate (web parity). */
@Composable
internal fun CodePromptDialog(
    prompt: TwoFactorPrompt,
    busy: Boolean,
    error: String?,
    errorRes: Int?,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var code by remember { mutableStateOf("") }
    val (title, description) = when (prompt) {
        TwoFactorPrompt.DISABLE ->
            R.string.settings_two_factor_disable_title to R.string.settings_two_factor_disable_description
        TwoFactorPrompt.REGENERATE ->
            R.string.settings_two_factor_regenerate_title to R.string.settings_two_factor_regenerate_description
    }

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(stringResource(title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = stringResource(description),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = code,
                    onValueChange = { code = it },
                    singleLine = true,
                    label = { Text(stringResource(R.string.settings_two_factor_code_prompt_label)) },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.fillMaxWidth(),
                )
                val message = errorRes?.let { stringResource(it) } ?: error
                message?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(code) },
                enabled = !busy && code.isNotBlank(),
            ) {
                Text(stringResource(if (busy) R.string.settings_two_factor_submitting else R.string.settings_two_factor_confirm))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss, enabled = !busy) {
                Text(stringResource(R.string.settings_cancel))
            }
        },
    )
}

/** Issue #814 Phase 2: the ten one-time recovery codes — plaintext, shown exactly once. */
@Composable
internal fun RecoveryCodesDialog(
    codes: List<String>,
    onDone: () -> Unit,
) {
    val clipboard = LocalClipboardManager.current
    var copied by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDone,
        title = { Text(stringResource(R.string.settings_two_factor_recovery_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = stringResource(R.string.settings_two_factor_recovery_warning),
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodyMedium,
                )
                Text(
                    text = stringResource(R.string.settings_two_factor_recovery_description),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                codes.forEach { code ->
                    Text(
                        text = code,
                        style = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
                TextButton(onClick = {
                    clipboard.setText(AnnotatedString(codes.joinToString("\n")))
                    copied = true
                }) {
                    Icon(Icons.Outlined.ContentCopy, contentDescription = null)
                    Text(
                        stringResource(
                            if (copied) R.string.settings_two_factor_recovery_copied
                            else R.string.settings_two_factor_recovery_copy,
                        ),
                        modifier = Modifier.padding(start = 4.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDone) {
                Text(stringResource(R.string.settings_two_factor_recovery_done))
            }
        },
    )
}
