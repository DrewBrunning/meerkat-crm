package com.mycorrhizal.crm.feature.contacts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.selectable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.model.network.DuplicatePair
import com.mycorrhizal.crm.model.network.DuplicateReasons
import com.mycorrhizal.crm.ui.R

/**
 * T93 duplicate review (web's "Review duplicates" surface): lists the scan's
 * candidate pairs strongest-first and offers Merge (into the existing merge
 * flow, with a keep-choice) and a permanent Not-a-duplicate dismissal.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DuplicatePairsScreen(
    onBack: () -> Unit,
    onMerge: (keepId: Long, mergeId: Long, mergeName: String) -> Unit,
    viewModel: DuplicatePairsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    DuplicatePairsScreenContent(
        uiState = state,
        onBack = onBack,
        onMerge = onMerge,
        onDismiss = viewModel::dismiss,
        onLoad = viewModel::load,
        onErrorShown = viewModel::onErrorShown,
    )
}

/**
 * Stateless duplicate-review content, split out so the four canonical states
 * (loading / error / empty / populated) and the two confirm dialogs are
 * directly testable (the house pattern — see [ContactListScreenContent]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DuplicatePairsScreenContent(
    uiState: DuplicatePairsUiState,
    onBack: () -> Unit = {},
    onMerge: (keepId: Long, mergeId: Long, mergeName: String) -> Unit = { _, _, _ -> },
    onDismiss: (DuplicatePair) -> Unit = {},
    onLoad: () -> Unit = {},
    onErrorShown: () -> Unit = {},
) {
    val snackbarHostState = remember { SnackbarHostState() }
    // Which pair a Merge is in progress for, and the chosen keeper UID (null
    // until the dialog's selection is pinned). Local screen chrome: the merge
    // itself happens on the separate Merge screen.
    var pendingMerge by remember { mutableStateOf<Pair<DuplicatePair, String?>?>(null) }
    // T93 dismissal is permanent (no undo surface exists) — a real
    // confirmation dialog first, matching web's window.confirm.
    var pendingDismiss by remember { mutableStateOf<DuplicatePair?>(null) }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.cd_back))
                    }
                },
                title = { Text(stringResource(R.string.duplicates_title), style = MaterialTheme.typography.titleLarge) },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                    navigationIconContentColor = MaterialTheme.colorScheme.onPrimary,
                    actionIconContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { padding ->
        Column(modifier = Modifier.fillMaxSize().padding(padding)) {
            when {
                uiState.isLoading && uiState.pairs.isEmpty() -> {
                    CircularProgressIndicator(
                        modifier = Modifier
                            .align(Alignment.CenterHorizontally)
                            .padding(24.dp)
                            .testTag("duplicates-loading"),
                    )
                }
                uiState.error != null && uiState.pairs.isEmpty() -> {
                    Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        modifier = Modifier.fillMaxWidth().padding(24.dp),
                    ) {
                        Text(
                            text = uiState.error.orEmpty(),
                            color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodyMedium,
                        )
                        Button(onClick = onLoad, modifier = Modifier.padding(top = 12.dp)) {
                            Text(stringResource(R.string.action_retry))
                        }
                    }
                }
                uiState.pairs.isEmpty() -> {
                    Text(
                        text = stringResource(R.string.duplicates_empty),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(24.dp)
                            .testTag("duplicates-empty"),
                    )
                }
                else -> {
                    LazyColumn(
                        modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(vertical = 8.dp),
                    ) {
                        item(key = "count") {
                            Text(
                                text = stringResource(R.string.duplicates_count, uiState.total),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                            )
                        }
                        items(uiState.pairs, key = { it.reviewKey }) { pair ->
                            DuplicatePairRow(
                                pair = pair,
                                dismissing = uiState.dismissingKey == pair.reviewKey,
                                onMerge = { pendingMerge = pair to pair.a.uid },
                                onDismiss = { pendingDismiss = pair },
                            )
                            HorizontalDivider(modifier = Modifier.padding(horizontal = 16.dp))
                        }
                    }
                }
            }
        }
    }

    uiState.error?.let { message ->
        LaunchedEffect(message) {
            snackbarHostState.showSnackbar(message)
            onErrorShown()
        }
    }

    pendingMerge?.let { (pair, keeperUid) ->
        MergeKeeperDialog(
            pair = pair,
            initialKeeperUid = keeperUid,
            onDismissRequest = { pendingMerge = null },
            onConfirm = { keeperUidChoice ->
                val keep = if (keeperUidChoice == pair.a.uid) pair.a else pair.b
                val merge = if (keeperUidChoice == pair.a.uid) pair.b else pair.a
                pendingMerge = null
                onMerge(keep.id.toLong(), merge.id.toLong(), merge.displayName)
            },
        )
    }

    pendingDismiss?.let { pair ->
        AlertDialog(
            onDismissRequest = { pendingDismiss = null },
            title = { Text(stringResource(R.string.duplicates_dismiss_title)) },
            text = { Text(stringResource(R.string.duplicates_dismiss_body)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        pendingDismiss = null
                        onDismiss(pair)
                    },
                    modifier = Modifier.testTag("dismiss-confirm"),
                ) {
                    Text(stringResource(R.string.duplicates_not_duplicate))
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingDismiss = null }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
}

/**
 * One candidate pair: both contacts (avatar + name + an identifier line), the
 * matched-tier reason chips + confidence, and the Merge / Not-a-duplicate
 * actions.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun DuplicatePairRow(
    pair: DuplicatePair,
    dismissing: Boolean,
    onMerge: () -> Unit,
    onDismiss: () -> Unit,
) {
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            DuplicateContactLine(contact = pair.a, modifier = Modifier.weight(1f))
            Text(
                text = stringResource(R.string.duplicates_vs),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 8.dp),
            )
            DuplicateContactLine(contact = pair.b, modifier = Modifier.weight(1f))
        }
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.padding(top = 8.dp),
        ) {
            pair.reasons.forEach { reason ->
                AssistChip(
                    onClick = {},
                    label = { Text(reasonLabel(reason)) },
                    enabled = false,
                )
            }
            Text(
                text = stringResource(R.string.duplicates_confidence, (pair.confidence * 100).toInt()),
                style = MaterialTheme.typography.labelSmall,
                color = if (pair.confidence >= 0.9) MaterialTheme.colorScheme.primary
                else MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.align(Alignment.CenterVertically),
            )
        }
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.padding(top = 4.dp),
        ) {
            OutlinedButton(
                onClick = onMerge,
                enabled = !dismissing,
                modifier = Modifier.testTag("merge-pair-${pair.reviewKey}"),
            ) {
                Text(stringResource(R.string.duplicates_merge))
            }
            TextButton(onClick = onDismiss, enabled = !dismissing) {
                Text(stringResource(R.string.duplicates_not_duplicate))
            }
        }
    }
}

@Composable
private fun DuplicateContactLine(contact: ContactSummary, modifier: Modifier = Modifier) {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = modifier) {
        ContactAvatar(
            photoUri = contact.photoThumbnail ?: contact.photo,
            contentDescription = contact.displayName,
            size = 40.dp,
        )
        Column(modifier = Modifier.padding(start = 8.dp)) {
            Text(
                text = contact.displayName,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = listOfNotNull(
                    contact.primaryEmail?.takeIf { it.isNotBlank() },
                    contact.primaryPhone?.takeIf { it.isNotBlank() },
                    if (contact.archived) stringResource(R.string.contact_archived) else null,
                ).joinToString(" · ").ifBlank { " " },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun reasonLabel(reason: String): String = when (reason) {
    DuplicateReasons.EMAIL -> stringResource(R.string.duplicates_reason_email)
    DuplicateReasons.NAME -> stringResource(R.string.duplicates_reason_name)
    DuplicateReasons.PHONE -> stringResource(R.string.duplicates_reason_phone)
    else -> reason
}

/** Which of the pair survives as the merge keeper. Defaults to pair.a (web parity). */
@Composable
private fun MergeKeeperDialog(
    pair: DuplicatePair,
    initialKeeperUid: String?,
    onDismissRequest: () -> Unit,
    onConfirm: (keeperUid: String) -> Unit,
) {
    var keeperUid by rememberSaveable(pair.reviewKey) { mutableStateOf(initialKeeperUid ?: pair.a.uid.orEmpty()) }
    AlertDialog(
        onDismissRequest = onDismissRequest,
        title = { Text(stringResource(R.string.duplicates_merge_title)) },
        text = {
            Column {
                Text(
                    text = stringResource(R.string.duplicates_kept_hint),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                listOf(pair.a, pair.b).forEach { contact ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier
                            .fillMaxWidth()
                            .selectable(
                                selected = keeperUid == contact.uid,
                                onClick = { keeperUid = contact.uid.orEmpty() },
                                role = Role.RadioButton,
                            )
                            .padding(vertical = 4.dp)
                            .testTag("keeper-${contact.id}"),
                    ) {
                        RadioButton(
                            selected = keeperUid == contact.uid,
                            onClick = null,
                        )
                        Text(contact.displayName, modifier = Modifier.padding(start = 8.dp))
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { if (keeperUid.isNotBlank()) onConfirm(keeperUid) },
                enabled = keeperUid.isNotBlank(),
                modifier = Modifier.testTag("merge-confirm"),
            ) {
                Text(stringResource(R.string.duplicates_merge))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismissRequest) {
                Text(stringResource(R.string.action_cancel))
            }
        },
    )
}
