package com.mycorrhizal.crm.enroll

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.ui.R

/**
 * Stateful host for the one-shot post-login biometric-enrollment prompt (issue
 * #722). Renders nothing (and immediately signals [onDone]) when the user has
 * already decided or the device cannot pass the local gate; otherwise shows
 * the dialog until the user picks an option.
 */
@Composable
fun BiometricEnrollmentPromptHost(
    onDone: () -> Unit,
    viewModel: BiometricEnrollmentViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) {
        viewModel.events.collect { event ->
            if (event == BiometricEnrollmentEvent.Done) onDone()
        }
    }

    if (state.visible) {
        BiometricEnrollmentDialog(
            isBusy = state.isBusy,
            error = state.error,
            onEnroll = viewModel::enroll,
            onNotNow = viewModel::notNow,
            onNeverAskAgain = viewModel::neverAskAgain,
        )
    }
}

/**
 * Stateless "Sign in with biometrics?" dialog — three options per the
 * enrollment spec: "Set up" (enroll now), "Not now" (leave the state UNASKED
 * so the prompt returns at the next login), and "Never ask again" (persist the
 * opt-out). AlertDialog's two slots carry "Set up" / "Not now"; the third
 * option sits in the body so the dialog stays a single surface.
 */
@Composable
fun BiometricEnrollmentDialog(
    isBusy: Boolean,
    error: Boolean,
    onEnroll: () -> Unit,
    onNotNow: () -> Unit,
    onNeverAskAgain: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onNotNow,
        title = { Text(stringResource(R.string.biometric_enroll_dialog_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (error) {
                    Text(
                        text = stringResource(R.string.biometric_enroll_error),
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
                    )
                } else {
                    Text(stringResource(R.string.biometric_enroll_dialog_body))
                }
                TextButton(
                    onClick = onNeverAskAgain,
                    enabled = !isBusy,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.biometric_enroll_never))
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onEnroll, enabled = !isBusy) {
                Text(stringResource(R.string.biometric_enroll_action))
            }
        },
        dismissButton = {
            TextButton(onClick = onNotNow, enabled = !isBusy) {
                Text(stringResource(R.string.biometric_enroll_not_now))
            }
        },
    )
}
