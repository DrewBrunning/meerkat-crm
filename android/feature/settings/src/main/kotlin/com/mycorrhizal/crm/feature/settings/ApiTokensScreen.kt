package com.mycorrhizal.crm.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Autorenew
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.domain.compat.ServerCapabilities
import com.mycorrhizal.crm.domain.compat.ServerFeature
import com.mycorrhizal.crm.model.network.API_TOKEN_EXPIRY_OPTIONS
import com.mycorrhizal.crm.model.network.API_TOKEN_SCOPES
import com.mycorrhizal.crm.model.network.ApiToken
import com.mycorrhizal.crm.model.network.ApiTokenCreateResponse
import com.mycorrhizal.crm.model.network.DEFAULT_API_TOKEN_EXPIRY_DAYS
import com.mycorrhizal.crm.model.network.DEFAULT_API_TOKEN_SCOPE
import com.mycorrhizal.crm.ui.LocalServerVersion
import com.mycorrhizal.crm.ui.R
import com.mycorrhizal.crm.ui.components.AccessibleIconButton
import com.mycorrhizal.crm.ui.components.BrandFab
import com.mycorrhizal.crm.ui.components.EmptyState
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * API token list/create/revoke/revoke-all/rotate (issue #413's Android
 * follow-up, #573), mirroring web's SettingsPage.tsx API-token section:
 * revoke and rotate are each confirmed first, and both create and rotate
 * reveal the new plaintext exactly once via [RevealedTokenDialog].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ApiTokensScreen(
    onBack: () -> Unit,
    viewModel: ApiTokensViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var createOpen by remember { mutableStateOf(false) }
    var revokingToken by remember { mutableStateOf<ApiToken?>(null) }
    var rotatingToken by remember { mutableStateOf<ApiToken?>(null) }
    var revokeAllOpen by remember { mutableStateOf(false) }

    // Issue #692: create/list/revoke are v0.6.0; rotate + revoke-all need the
    // v0.6.1 endpoints, so those affordances are hidden on a v0.6.0 server.
    val advancedTokensSupported =
        ServerCapabilities.isSupported(LocalServerVersion.current, ServerFeature.API_TOKENS_ADVANCED)

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    AccessibleIconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.cd_back))
                    }
                },
                title = {
                    Text(stringResource(R.string.settings_api_tokens_title), style = MaterialTheme.typography.titleLarge)
                },
                actions = {
                    if (advancedTokensSupported) {
                        AccessibleIconButton(
                            onClick = { revokeAllOpen = true },
                            enabled = state.activeCount > 0 && !state.isRevokingAll,
                        ) {
                            Icon(
                                Icons.Outlined.Block,
                                contentDescription = stringResource(R.string.settings_api_tokens_revoke_all),
                            )
                        }
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                    navigationIconContentColor = MaterialTheme.colorScheme.onPrimary,
                    actionIconContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
        floatingActionButton = {
            BrandFab(onClick = { createOpen = true }) {
                Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.settings_api_tokens_create))
            }
        },
    ) { padding ->
        if (state.isLoading && state.tokens.isEmpty()) {
            Column(
                modifier = Modifier.fillMaxSize().padding(padding),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                CircularProgressIndicator()
            }
        } else if (state.tokens.isEmpty()) {
            EmptyState(
                message = stringResource(R.string.settings_api_tokens_empty),
                modifier = Modifier.padding(padding),
            )
        } else {
            LazyColumn(modifier = Modifier.fillMaxSize().padding(padding)) {
                if (state.error != null) {
                    item {
                        Text(
                            text = state.error.orEmpty(),
                            color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier
                                .padding(horizontal = 16.dp, vertical = 4.dp)
                                .semantics { liveRegion = LiveRegionMode.Assertive },
                        )
                    }
                }
                state.revokedAllCount?.let { count ->
                    item {
                        Text(
                            text = stringResource(R.string.settings_api_tokens_revoke_all_success, count),
                            color = MaterialTheme.colorScheme.tertiary,
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                        )
                    }
                }
                items(state.tokens, key = { it.id }) { token ->
                    ApiTokenRow(
                        token = token,
                        rotating = state.rotatingId == token.id,
                        revoking = state.revokingId == token.id,
                        rotateSupported = advancedTokensSupported,
                        onRotate = { rotatingToken = token },
                        onRevoke = { revokingToken = token },
                    )
                }
            }
        }
    }

    if (createOpen) {
        CreateApiTokenDialog(
            isSaving = state.isSaving,
            onConfirm = { name, expiresInDays, scope ->
                viewModel.create(name, expiresInDays, scope)
                createOpen = false
            },
            onDismiss = { createOpen = false },
        )
    }

    revokingToken?.let { token ->
        AlertDialog(
            onDismissRequest = { revokingToken = null },
            title = { Text(stringResource(R.string.settings_api_tokens_revoke_title)) },
            text = { Text(stringResource(R.string.settings_api_tokens_revoke_body, token.name)) },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.revoke(token)
                    revokingToken = null
                }) { Text(stringResource(R.string.settings_api_tokens_revoke_confirm)) }
            },
            dismissButton = {
                TextButton(onClick = { revokingToken = null }) {
                    Text(stringResource(R.string.settings_cancel))
                }
            },
        )
    }

    rotatingToken?.let { token ->
        AlertDialog(
            onDismissRequest = { rotatingToken = null },
            title = { Text(stringResource(R.string.settings_api_tokens_rotate_title)) },
            text = { Text(stringResource(R.string.settings_api_tokens_rotate_body, token.name)) },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.rotate(token)
                    rotatingToken = null
                }) { Text(stringResource(R.string.settings_api_tokens_rotate_confirm)) }
            },
            dismissButton = {
                TextButton(onClick = { rotatingToken = null }) {
                    Text(stringResource(R.string.settings_cancel))
                }
            },
        )
    }

    if (revokeAllOpen) {
        AlertDialog(
            onDismissRequest = { revokeAllOpen = false },
            title = { Text(stringResource(R.string.settings_api_tokens_revoke_all_title)) },
            text = { Text(stringResource(R.string.settings_api_tokens_revoke_all_body, state.activeCount)) },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.revokeAll()
                    revokeAllOpen = false
                }) { Text(stringResource(R.string.settings_api_tokens_revoke_all)) }
            },
            dismissButton = {
                TextButton(onClick = { revokeAllOpen = false }) {
                    Text(stringResource(R.string.settings_cancel))
                }
            },
        )
    }

    state.revealedToken?.let { revealed ->
        RevealedTokenDialog(
            revealed = revealed,
            isRotation = state.revealedIsRotation,
            onDismiss = { viewModel.dismissRevealedToken() },
        )
    }
}

@Composable
internal fun ApiTokenRow(
    token: ApiToken,
    rotating: Boolean,
    revoking: Boolean,
    onRotate: () -> Unit,
    onRevoke: () -> Unit,
    rotateSupported: Boolean = true,
) {
    val active = token.isActive()
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(text = token.name, style = MaterialTheme.typography.bodyLarge)
                Text(
                    text = stringResource(statusLabelRes(token)),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (active) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.error,
                )
            }
            Text(
                text = stringResource(
                    if (token.scope == "carddav") R.string.settings_api_tokens_scope_carddav else R.string.settings_api_tokens_scope_full,
                ),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = stringResource(R.string.settings_api_tokens_created, formatTokenTime(token.createdAt)),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            val lastUsed = token.lastUsedAt?.let { formatTokenTime(it) } ?: stringResource(R.string.settings_api_tokens_never_used)
            Text(
                text = stringResource(R.string.settings_api_tokens_last_used, lastUsed),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            token.expiresAt?.let { expiresAt ->
                Text(
                    text = stringResource(R.string.settings_api_tokens_expires, formatTokenTime(expiresAt)),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (active) {
            if (rotateSupported) {
                AccessibleIconButton(onClick = onRotate, enabled = !rotating) {
                    if (rotating) {
                        CircularProgressIndicator(modifier = Modifier.padding(4.dp), strokeWidth = 2.dp)
                    } else {
                        // #205-equivalent: the row-action label carries the token
                        // name so TalkBack doesn't read a bare "Rotate"/"Revoke"
                        // on every row.
                        Icon(
                            Icons.Outlined.Autorenew,
                            contentDescription = stringResource(R.string.settings_api_tokens_rotate_named, token.name),
                        )
                    }
                }
            }
            AccessibleIconButton(onClick = onRevoke, enabled = !revoking) {
                if (revoking) {
                    CircularProgressIndicator(modifier = Modifier.padding(4.dp), strokeWidth = 2.dp)
                } else {
                    Icon(
                        Icons.Outlined.Block,
                        contentDescription = stringResource(R.string.settings_api_tokens_revoke_named, token.name),
                    )
                }
            }
        }
    }
}

private fun statusLabelRes(token: ApiToken): Int = when {
    token.revokedAt != null -> R.string.settings_api_tokens_revoked
    token.isExpired() -> R.string.settings_api_tokens_expired
    else -> R.string.settings_api_tokens_active
}

@Composable
internal fun CreateApiTokenDialog(
    isSaving: Boolean,
    onConfirm: (name: String, expiresInDays: Int, scope: String) -> Unit,
    onDismiss: () -> Unit,
) {
    var name by remember { mutableStateOf("") }
    var expiresInDays by remember { mutableIntStateOf(DEFAULT_API_TOKEN_EXPIRY_DAYS) }
    var scope by remember { mutableStateOf(DEFAULT_API_TOKEN_SCOPE) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.settings_api_tokens_create_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text(stringResource(R.string.settings_api_tokens_name_label)) },
                    singleLine = true,
                )
                Text(
                    text = stringResource(R.string.settings_api_tokens_expiry_label),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    API_TOKEN_EXPIRY_OPTIONS.forEach { days ->
                        FilterChip(
                            selected = expiresInDays == days,
                            onClick = { expiresInDays = days },
                            label = {
                                Text(
                                    stringResource(R.string.settings_api_tokens_expiry_days, days),
                                    style = MaterialTheme.typography.labelMedium,
                                )
                            },
                        )
                    }
                }
                Text(
                    text = stringResource(R.string.settings_api_tokens_scope_label),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    API_TOKEN_SCOPES.forEach { option ->
                        FilterChip(
                            selected = scope == option,
                            onClick = { scope = option },
                            label = {
                                Text(
                                    stringResource(
                                        if (option == "carddav") R.string.settings_api_tokens_scope_carddav else R.string.settings_api_tokens_scope_full,
                                    ),
                                    style = MaterialTheme.typography.labelMedium,
                                )
                            },
                        )
                    }
                }
                Text(
                    text = stringResource(
                        if (scope == "carddav") R.string.settings_api_tokens_scope_carddav_help else R.string.settings_api_tokens_scope_full_help,
                    ),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(name.trim(), expiresInDays, scope) },
                enabled = !isSaving && name.isNotBlank(),
            ) {
                if (isSaving) CircularProgressIndicator(modifier = Modifier.padding(end = 4.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.settings_api_tokens_create))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.settings_cancel)) }
        },
    )
}

/**
 * Shared by create and rotate, since rotate also mints a fresh plaintext
 * that's only shown this once -- [isRotation] swaps the title/warning copy.
 */
@Composable
internal fun RevealedTokenDialog(
    revealed: ApiTokenCreateResponse,
    isRotation: Boolean,
    onDismiss: () -> Unit,
) {
    val clipboard = LocalClipboardManager.current
    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(
                stringResource(
                    if (isRotation) R.string.settings_api_tokens_rotated_title else R.string.settings_api_tokens_created_title,
                ),
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    stringResource(
                        if (isRotation) R.string.settings_api_tokens_rotated_warning else R.string.settings_api_tokens_created_warning,
                    ),
                )
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    OutlinedTextField(
                        value = revealed.token.orEmpty(),
                        onValueChange = {},
                        readOnly = true,
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                    )
                    AccessibleIconButton(onClick = { clipboard.setText(AnnotatedString(revealed.token.orEmpty())) }) {
                        Icon(Icons.Outlined.ContentCopy, contentDescription = stringResource(R.string.settings_api_tokens_copy))
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.settings_api_tokens_done)) }
        },
    )
}

/**
 * Locale-aware date + time (mirroring web's formatDate → toLocaleString and
 * AuditScreen's formatAuditTime).
 */
private fun formatTokenTime(iso: String?): String {
    if (iso.isNullOrBlank()) return ""
    return runCatching {
        val instant = Instant.parse(iso)
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
            .withZone(ZoneId.systemDefault())
            .format(instant)
    }.getOrDefault(iso)
}
