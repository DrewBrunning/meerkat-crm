package com.mycorrhizal.crm.compat

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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.mycorrhizal.crm.ui.R

/**
 * Issue #692: the blocking gate shown when the configured server reports a
 * version older than this app's baseline (0.6.0) — the whole authenticated
 * surface expects that API contract, so instead of letting every screen fail,
 * the app refuses to show UI and asks the operator to upgrade the server. It
 * names the server URL and the versions so the operator can act.
 *
 * Escape hatch mirrors [ForceUpdateScreen]: "Log out" when a session exists,
 * "Back to sign in" when the gate raised pre-login. Stateless.
 */
@Composable
fun ServerTooOldScreen(
    serverVersion: String,
    requiredVersion: String,
    serverUrl: String?,
    loggedIn: Boolean = true,
    onLogout: () -> Unit,
    onBackToLogin: (() -> Unit)? = null,
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(24.dp)
            .testTag("server-too-old-screen"),
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
                text = stringResource(R.string.server_too_old_heading),
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.semantics { heading() },
            )
            Text(
                text = stringResource(R.string.server_too_old_message, requiredVersion, serverVersion),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            serverUrl?.takeIf { it.isNotBlank() }?.let { url ->
                Text(
                    text = stringResource(R.string.server_too_old_server, url),
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            if (loggedIn || onBackToLogin == null) {
                Button(
                    onClick = onLogout,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.settings_log_out))
                }
            } else {
                Button(
                    onClick = onBackToLogin,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.force_update_back_to_login))
                }
            }
        }
    }
}
