package com.mycorrhizal.crm.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.CalendarSubscriptionInput
import com.mycorrhizal.crm.model.network.ContactSubscription
import com.mycorrhizal.crm.ui.R
import com.mycorrhizal.crm.ui.components.AccessibleIconButton
import com.mycorrhizal.crm.ui.components.BrandFab
import com.mycorrhizal.crm.ui.components.EmptyState
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * Calendar (CalDAV/iCal) subscription list/create/edit/delete/sync-now, plus
 * a read-only contact (CardDAV) subscription health list -- issue #390's
 * Android follow-up (#628). Mirrors web's `CalendarSyncSettings.tsx`: delete
 * is confirmed first, and creating a calendar triggers its first sync
 * immediately for feedback (handled in [CalendarSyncViewModel.save]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CalendarSyncScreen(
    onBack: () -> Unit,
    viewModel: CalendarSyncViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var editorOpen by remember { mutableStateOf(false) }
    var editingCalendar by remember { mutableStateOf<CalendarSubscription?>(null) }
    var deletingCalendar by remember { mutableStateOf<CalendarSubscription?>(null) }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    AccessibleIconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.cd_back))
                    }
                },
                title = {
                    Text(stringResource(R.string.settings_calendar_sync_title), style = MaterialTheme.typography.titleLarge)
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
            BrandFab(onClick = {
                editingCalendar = null
                editorOpen = true
            }) {
                Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.settings_calendar_sync_add))
            }
        },
    ) { padding ->
        if (state.isLoading && state.isEmpty) {
            Column(
                modifier = Modifier.fillMaxSize().padding(padding),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                CircularProgressIndicator()
            }
        } else if (state.isEmpty) {
            EmptyState(
                message = stringResource(R.string.settings_calendar_sync_empty),
                modifier = Modifier.padding(padding),
            )
        } else {
            LazyColumn(modifier = Modifier.fillMaxSize().padding(padding)) {
                item {
                    Text(
                        text = stringResource(R.string.settings_calendar_sync_description),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
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
                state.lastSyncResult?.let { result ->
                    item {
                        Text(
                            text = stringResource(
                                R.string.settings_calendar_sync_sync_success,
                                result.created,
                                result.updated,
                                result.skipped,
                            ),
                            color = MaterialTheme.colorScheme.tertiary,
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                        )
                    }
                }
                if (state.calendars.isEmpty()) {
                    item {
                        Text(
                            text = stringResource(R.string.settings_calendar_sync_empty),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                        )
                    }
                }
                items(state.calendars, key = { "cal-${it.id}" }) { calendar ->
                    CalendarSubscriptionRow(
                        calendar = calendar,
                        syncing = state.syncingIds.contains(calendar.id),
                        onSync = { viewModel.sync(calendar) },
                        onEdit = {
                            editingCalendar = calendar
                            editorOpen = true
                        },
                        onDelete = { deletingCalendar = calendar },
                    )
                }
                if (state.contactSubscriptions.isNotEmpty()) {
                    item { HorizontalDivider(modifier = Modifier.padding(vertical = 8.dp)) }
                    item {
                        Column(modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
                            Text(
                                text = stringResource(R.string.settings_contact_sync_title),
                                style = MaterialTheme.typography.titleSmall,
                            )
                            Text(
                                text = stringResource(R.string.settings_contact_sync_description),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    items(state.contactSubscriptions, key = { "contact-${it.id}" }) { subscription ->
                        ContactSubscriptionRow(subscription = subscription)
                    }
                }
            }
        }
    }

    if (editorOpen) {
        CalendarEditorDialog(
            initial = editingCalendar,
            isSaving = state.isSaving,
            onConfirm = { input ->
                viewModel.save(input, editingCalendar?.id)
                editorOpen = false
            },
            onDismiss = { editorOpen = false },
        )
    }

    deletingCalendar?.let { calendar ->
        AlertDialog(
            onDismissRequest = { deletingCalendar = null },
            title = { Text(stringResource(R.string.settings_calendar_sync_delete_title)) },
            text = { Text(stringResource(R.string.settings_calendar_sync_delete_body, calendar.name)) },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.delete(calendar)
                    deletingCalendar = null
                }) { Text(stringResource(R.string.action_delete)) }
            },
            dismissButton = {
                TextButton(onClick = { deletingCalendar = null }) {
                    Text(stringResource(R.string.settings_cancel))
                }
            },
        )
    }
}

@Composable
internal fun CalendarSubscriptionRow(
    calendar: CalendarSubscription,
    syncing: Boolean,
    onSync: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(text = calendar.name, style = MaterialTheme.typography.bodyLarge)
                if (!calendar.syncEnabled) {
                    Text(
                        text = stringResource(R.string.settings_calendar_sync_disabled),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (calendar.lastSyncStatus == "error") {
                    Text(
                        text = if (calendar.consecutiveFailures > 1) {
                            stringResource(R.string.settings_calendar_sync_sync_failed_count, calendar.consecutiveFailures)
                        } else {
                            stringResource(R.string.settings_calendar_sync_sync_failed)
                        },
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
            Text(
                text = "${calendar.url} — ${lastSyncedLine(calendar.lastSyncedAt)}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            SyncHealthSection(
                consecutiveFailures = calendar.consecutiveFailures,
                incidentFirstFailureAt = calendar.incidentFirstFailureAt,
                lastSuccessAt = calendar.lastSuccessAt,
                lastSyncStatus = calendar.lastSyncStatus,
                lastRunStats = calendar.lastRunStats,
                terminalFailureAt = calendar.terminalFailureAt,
                terminalReason = calendar.terminalReason,
                includeArchived = false,
            )
        }
        AccessibleIconButton(onClick = onSync, enabled = !syncing) {
            if (syncing) {
                CircularProgressIndicator(modifier = Modifier.padding(4.dp), strokeWidth = 2.dp)
            } else {
                // #205-equivalent: the row-action label carries the calendar
                // name so TalkBack doesn't read a bare "Sync now" on every row.
                Icon(Icons.Outlined.Sync, contentDescription = stringResource(R.string.settings_calendar_sync_sync_now_named, calendar.name))
            }
        }
        AccessibleIconButton(onClick = onEdit) {
            Icon(Icons.Outlined.Edit, contentDescription = stringResource(R.string.settings_calendar_sync_edit_named, calendar.name))
        }
        AccessibleIconButton(onClick = onDelete) {
            Icon(Icons.Outlined.Delete, contentDescription = stringResource(R.string.settings_calendar_sync_delete_named, calendar.name))
        }
    }
}

/**
 * Read-only (issue #628 scope note 4: web has no create/edit/delete UI for
 * CardDAV contact subscriptions either, so wiring only the health list here
 * is deliberate, not a placeholder).
 */
@Composable
internal fun ContactSubscriptionRow(subscription: ContactSubscription) {
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(text = subscription.name, style = MaterialTheme.typography.bodyLarge)
            if (!subscription.syncEnabled) {
                Text(
                    text = stringResource(R.string.settings_calendar_sync_disabled),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        Text(
            text = "${subscription.url} — ${lastSyncedLine(subscription.lastSyncedAt)}",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        SyncHealthSection(
            consecutiveFailures = subscription.consecutiveFailures,
            incidentFirstFailureAt = subscription.incidentFirstFailureAt,
            lastSuccessAt = subscription.lastSuccessAt,
            lastSyncStatus = subscription.lastSyncStatus,
            lastRunStats = subscription.lastRunStats,
            terminalFailureAt = subscription.terminalFailureAt,
            terminalReason = subscription.terminalReason,
            includeArchived = true,
        )
        if (subscription.pendingConflicts > 0) {
            Text(
                text = stringResource(R.string.settings_contact_sync_pending_conflicts, subscription.pendingConflicts),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
    }
}

/**
 * The sync-health line shared by calendar and contact subscription rows
 * (issue #390 / INT-04 #467): a terminal failure gets its own dedicated
 * notice; otherwise a standing failure shows how long it's been failing and
 * when it last worked, and a healthy sync shows the last run's tallies.
 * Mirrors web's `renderHealthLine` / `renderTerminalNotice`.
 */
@Composable
internal fun SyncHealthSection(
    consecutiveFailures: Int,
    incidentFirstFailureAt: String?,
    lastSuccessAt: String?,
    lastSyncStatus: String,
    lastRunStats: Map<String, Int>,
    terminalFailureAt: String?,
    terminalReason: String,
    includeArchived: Boolean,
) {
    if (terminalFailureAt != null) {
        Text(
            text = stringResource(R.string.settings_sync_health_terminal_title),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.error,
            fontWeight = FontWeight.Bold,
        )
        Text(
            text = terminalReasonText(terminalReason),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.error,
        )
        Text(
            text = staleLine(lastSuccessAt),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.error,
        )
        return
    }
    if (consecutiveFailures > 0) {
        val since = incidentFirstFailureAt?.let { formatSyncTime(it) }
            ?: stringResource(R.string.settings_calendar_sync_never_synced)
        val lastGood = lastSuccessAt?.let { stringResource(R.string.settings_sync_health_last_success, formatSyncTime(it)) }
            ?: stringResource(R.string.settings_sync_health_never_succeeded)
        Text(
            text = stringResource(R.string.settings_sync_health_failing, since, consecutiveFailures) + " · " + lastGood,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.error,
        )
        return
    }
    if (lastSyncStatus == "success" && lastRunStats.isNotEmpty()) {
        val text = if (includeArchived) {
            stringResource(
                R.string.settings_sync_health_last_run_contacts,
                lastRunStats["created"] ?: 0,
                lastRunStats["updated"] ?: 0,
                lastRunStats["archived"] ?: 0,
                lastRunStats["skipped"] ?: 0,
            )
        } else {
            stringResource(
                R.string.settings_sync_health_last_run,
                lastRunStats["created"] ?: 0,
                lastRunStats["updated"] ?: 0,
                lastRunStats["skipped"] ?: 0,
            )
        }
        Text(text = text, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun lastSyncedLine(lastSyncedAt: String?): String =
    lastSyncedAt?.let { stringResource(R.string.settings_calendar_sync_last_synced, formatSyncTime(it)) }
        ?: stringResource(R.string.settings_calendar_sync_never_synced)

/** i18n key text for the actionable message behind a terminal failure reason (INT-04, #467). */
@Composable
private fun terminalReasonText(reason: String): String = when (reason) {
    "auth-expiry" -> stringResource(R.string.settings_sync_health_terminal_auth_expiry)
    "authz-revoked" -> stringResource(R.string.settings_sync_health_terminal_authz_revoked)
    "remote-resource-deleted" -> stringResource(R.string.settings_sync_health_terminal_remote_deleted)
    else -> stringResource(R.string.settings_sync_health_terminal_generic)
}

/**
 * Staleness line (INT-04 #467 action 6): stays useful even when the failure
 * classification is wrong -- "last successful sync N days ago".
 */
@Composable
private fun staleLine(lastSuccessAt: String?): String {
    val days = daysSince(lastSuccessAt) ?: return stringResource(R.string.settings_sync_health_stale_never)
    return if (days >= 2) {
        stringResource(R.string.settings_sync_health_stale_days, days)
    } else {
        stringResource(R.string.settings_sync_health_stale_recent, formatSyncDate(lastSuccessAt!!))
    }
}

/** Whole days between [iso] and now, or null when [iso] is null/unparseable. */
private fun daysSince(iso: String?): Int? {
    if (iso.isNullOrBlank()) return null
    return runCatching {
        maxOf(0L, Duration.between(Instant.parse(iso), Instant.now()).toDays()).toInt()
    }.getOrNull()
}

/** Locale-aware date + time, mirroring [ApiTokensScreen]'s `formatTokenTime`. */
private fun formatSyncTime(iso: String?): String {
    if (iso.isNullOrBlank()) return ""
    return runCatching {
        val instant = Instant.parse(iso)
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
            .withZone(ZoneId.systemDefault())
            .format(instant)
    }.getOrDefault(iso)
}

/** Locale-aware date only, used by the staleness line (web's toLocaleDateString). */
private fun formatSyncDate(iso: String): String =
    runCatching {
        val instant = Instant.parse(iso)
        DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
            .withZone(ZoneId.systemDefault())
            .format(instant)
    }.getOrDefault(iso)

@Composable
internal fun CalendarEditorDialog(
    initial: CalendarSubscription?,
    isSaving: Boolean,
    onConfirm: (CalendarSubscriptionInput) -> Unit,
    onDismiss: () -> Unit,
) {
    var name by remember { mutableStateOf(initial?.name ?: "") }
    var url by remember { mutableStateOf(initial?.url ?: "") }
    var username by remember { mutableStateOf(initial?.username ?: "") }
    var password by remember { mutableStateOf("") }
    var syncEnabled by remember { mutableStateOf(initial?.syncEnabled ?: true) }
    var pastDays by remember { mutableStateOf((initial?.pastDays ?: 5).toString()) }
    var futureDays by remember { mutableStateOf((initial?.futureDays ?: 10).toString()) }

    val hasPassword = initial?.hasPassword ?: false
    val originalUsername = initial?.username ?: ""

    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(
                stringResource(
                    if (initial == null) R.string.settings_calendar_sync_add_title else R.string.settings_calendar_sync_edit_title,
                ),
            )
        },
        text = {
            Column(
                modifier = Modifier.heightIn(max = 480.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(
                    text = stringResource(R.string.settings_calendar_sync_dialog_info),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text(stringResource(R.string.settings_calendar_sync_name)) },
                    singleLine = true,
                )
                OutlinedTextField(
                    value = url,
                    onValueChange = { url = it },
                    label = { Text(stringResource(R.string.settings_calendar_sync_url)) },
                    singleLine = true,
                    supportingText = { Text(stringResource(R.string.settings_calendar_sync_url_help)) },
                )
                if (url.trim().lowercase().startsWith("http://") &&
                    (username.trim().isNotEmpty() || password.isNotEmpty() || hasPassword)
                ) {
                    Text(
                        text = stringResource(R.string.settings_calendar_sync_insecure_url_warning),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    label = { Text(stringResource(R.string.settings_calendar_sync_username)) },
                    singleLine = true,
                    supportingText = { Text(stringResource(R.string.settings_calendar_sync_credentials_optional)) },
                )
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text(stringResource(R.string.settings_calendar_sync_password)) },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    supportingText = if (hasPassword) {
                        { Text(stringResource(R.string.settings_calendar_sync_password_keep)) }
                    } else {
                        null
                    },
                )
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    OutlinedTextField(
                        value = pastDays,
                        onValueChange = { pastDays = it },
                        label = { Text(stringResource(R.string.settings_calendar_sync_past_days)) },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.weight(1f),
                    )
                    OutlinedTextField(
                        value = futureDays,
                        onValueChange = { futureDays = it },
                        label = { Text(stringResource(R.string.settings_calendar_sync_future_days)) },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.weight(1f),
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Switch(checked = syncEnabled, onCheckedChange = { syncEnabled = it })
                    Text(stringResource(R.string.settings_calendar_sync_auto_sync))
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    onConfirm(
                        CalendarSubscriptionInput(
                            name = name.trim(),
                            url = url.trim(),
                            username = username.trim(),
                            password = password,
                            // Clear a stored password when the user removed the username of a
                            // previously protected calendar and left the password empty.
                            clearPassword = initial != null &&
                                hasPassword &&
                                originalUsername.isNotEmpty() &&
                                username.trim().isEmpty() &&
                                password.isEmpty(),
                            syncEnabled = syncEnabled,
                            pastDays = parseDays(pastDays, 5),
                            futureDays = parseDays(futureDays, 10),
                        ),
                    )
                },
                enabled = !isSaving && name.isNotBlank() && url.isNotBlank(),
            ) {
                if (isSaving) CircularProgressIndicator(modifier = Modifier.padding(end = 4.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.settings_save))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.settings_cancel)) }
        },
    )
}

private fun parseDays(value: String, fallback: Int): Int {
    val parsed = value.toIntOrNull()
    return if (parsed != null && parsed >= 0) parsed else fallback
}
