package com.mycorrhizal.crm.applock

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.fragment.app.FragmentActivity
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.ui.R
import kotlinx.coroutines.launch

/**
 * Issue #722: the local-auth gate shown in front of a persisted session that
 * must not be resumed without the device's own check. The OS prompt runs on
 * first display and whenever the user taps the unlock button; cancelling the
 * prompt keeps this screen (the session's data is never composed underneath).
 * The only way off it — besides authenticating — is "Log out", which ends the
 * session normally (the opt-in preference itself is kept).
 */
@Composable
fun AppLockScreen(
    viewModel: AppLockViewModel = hiltViewModel(),
    prompter: AppUnlockPrompter? = null,
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val unlockPrompter = prompter ?: remember(context) {
        BiometricAppUnlockPrompter(context as FragmentActivity)
    }
    val title = stringResource(R.string.app_lock_heading)
    val subtitle = stringResource(R.string.app_lock_subtitle)
    // Pre-API-30 the prompt is strong-biometric-only and needs its own
    // negative (cancel) button; modern Android ignores it (the OS owns the
    // cancel once the device credential is in the allowed set).
    val cancelText = stringResource(R.string.settings_cancel)

    // Auto-prompt once per lock episode (config changes do not re-fire — the
    // saveable flag survives recreation); after a cancel the button retries.
    var autoPrompted by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        if (!autoPrompted) {
            autoPrompted = true
            if (state.canAuthenticate && !state.isUnlocking) {
                viewModel.onAuthStarted()
                unlockPrompter.requestUnlock(title, subtitle, cancelText).let(viewModel::onAuthResult)
            }
        }
    }

    val requestUnlock = {
        if (state.canAuthenticate && !state.isUnlocking) {
            scope.launch {
                viewModel.onAuthStarted()
                unlockPrompter.requestUnlock(title, subtitle, cancelText).let(viewModel::onAuthResult)
            }
        }
    }

    AppLockContent(
        state = state,
        onUnlockRequest = requestUnlock,
        onLogout = viewModel::onLogout,
        onErrorShown = viewModel::onErrorShown,
    )
}

/**
 * Stateless lock-gate layout — the canonical states are testable directly. It
 * renders no session data beyond the username (already shown on the device's
 * own lock screen), so this screen is safe to show before the gate passes.
 */
@Composable
fun AppLockContent(
    state: AppLockUiState,
    onUnlockRequest: () -> Unit,
    onLogout: () -> Unit,
    onErrorShown: () -> Unit = {},
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(24.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(16.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Image(
                painter = painterResource(id = R.drawable.ic_brand_logo),
                contentDescription = stringResource(R.string.app_name),
                modifier = Modifier.size(96.dp),
            )
            Text(
                text = stringResource(R.string.app_lock_heading),
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.semantics { heading() },
            )
            state.username?.let { username ->
                Text(
                    text = stringResource(R.string.app_lock_signed_in_as, username),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = stringResource(R.string.app_lock_subtitle),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            if (state.isUnlocking) {
                CircularProgressIndicator(
                    modifier = Modifier.semantics {
                        liveRegion = LiveRegionMode.Assertive
                    },
                )
            } else if (state.canAuthenticate) {
                Button(
                    onClick = onUnlockRequest,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.app_lock_unlock))
                }
            } else {
                // The device cannot satisfy the gate right now (no enrolled
                // Class 3 biometric and no secure lock screen). The user is not
                // locked out: logging out returns them to the login screen.
                Text(
                    text = stringResource(R.string.app_lock_unsupported),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
                )
            }

            TextButton(
                onClick = onLogout,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.settings_log_out))
            }

            val errorMessage = state.errorRes?.let { stringResource(it) }
            errorMessage?.let { message ->
                Text(
                    text = message,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
                )
                LaunchedEffect(message) { onErrorShown() }
            }
        }
    }
}
