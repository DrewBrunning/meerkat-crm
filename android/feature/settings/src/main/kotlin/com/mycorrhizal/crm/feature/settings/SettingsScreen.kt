package com.mycorrhizal.crm.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.clickable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import com.mycorrhizal.crm.ui.components.AccessibleIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.hilt.navigation.compose.hiltViewModel
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.ui.R

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    // Issue #150: null hides the hamburger — there is no drawer at Expanded.
    onMenuClick: (() -> Unit)? = {},
    onLoggedOut: () -> Unit,
    onCustomLinks: () -> Unit = {},
    onWebhooks: () -> Unit = {},
    // Issue #413's Android follow-up (#573): API token management.
    onApiTokens: () -> Unit = {},
    // Issue #814 Phase 2: TOTP two-factor enrollment/management.
    onTwoFactor: () -> Unit = {},
    onNotificationChannels: () -> Unit = {},
    // Issue #236: the Immich connection-config settings screen.
    onImmichSettings: () -> Unit = {},
    onCircleTagTriage: () -> Unit = {},
    // T104 + data suggestions: the Data review surface and its trigger.
    onData: () -> Unit = {},
    // Issue #348: admin user management.
    onManageUsers: () -> Unit = {},
    // Issue #424: the operational-event timeline.
    onSystemEvents: () -> Unit = {},
    onLocaleChanged: () -> Unit = {},
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val events by viewModel.events.collectAsStateWithLifecycle()

    LaunchedEffect(events) {
        when (events) {
            SettingsEvent.LoggedOut -> {
                viewModel.onEventShown()
                onLoggedOut()
            }
            // The activity recreates to re-resolve values-XX resources.
            SettingsEvent.LocaleChanged -> {
                viewModel.onEventShown()
                onLocaleChanged()
            }
            // A password change invalidated every JWT; the session is cleared
            // and the login screen takes over (see onLoggedOut).
            SettingsEvent.PasswordChanged -> {
                viewModel.onEventShown()
                onLoggedOut()
            }
            null -> Unit
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    onMenuClick?.let { onMenu ->
                        AccessibleIconButton(onClick = onMenu) {
                            Icon(Icons.Outlined.Menu, contentDescription = stringResource(R.string.cd_menu))
                        }
                    }
                },
                title = {
                    Text(stringResource(R.string.settings_title), style = MaterialTheme.typography.titleLarge)
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                    navigationIconContentColor = MaterialTheme.colorScheme.onPrimary,
                    actionIconContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
    ) { padding ->
        SettingsContent(
            state = state,
            onCustomLinks = onCustomLinks,
            onWebhooks = onWebhooks,
            onApiTokens = onApiTokens,
            onTwoFactor = onTwoFactor,
            onNotificationChannels = onNotificationChannels,
            onImmichSettings = onImmichSettings,
            onCircleTagTriage = onCircleTagTriage,
            onData = onData,
            onManageUsers = onManageUsers,
            onSystemEvents = onSystemEvents,
            onSuggestRelationships = viewModel::suggestRelationships,
            onLanguageChange = viewModel::updateLanguage,
            onDateFormatChange = viewModel::updateDateFormat,
            onThemeChange = viewModel::setThemePreference,
            onChangePassword = viewModel::changePassword,
            onCallTrackingChange = viewModel::setCallTrackingEnabled,
            onSmsTrackingChange = viewModel::setSmsTrackingEnabled,
            onNotificationsChange = viewModel::setNotificationsEnabled,
            onLogout = viewModel::logout,
            modifier = Modifier.padding(padding),
        )
    }
}

@Composable
fun SettingsContent(
    state: SettingsUiState,
    onCustomLinks: () -> Unit = {},
    onWebhooks: () -> Unit = {},
    onApiTokens: () -> Unit = {},
    onTwoFactor: () -> Unit = {},
    onNotificationChannels: () -> Unit = {},
    onImmichSettings: () -> Unit = {},
    onCircleTagTriage: () -> Unit = {},
    onData: () -> Unit = {},
    onManageUsers: () -> Unit = {},
    onSystemEvents: () -> Unit = {},
    onSuggestRelationships: () -> Unit = {},
    onLanguageChange: (String) -> Unit = {},
    onDateFormatChange: (String) -> Unit = {},
    onThemeChange: (String) -> Unit = {},
    onChangePassword: (String, String, String) -> Unit = { _, _, _ -> },
    onCallTrackingChange: (Boolean) -> Unit = {},
    onSmsTrackingChange: (Boolean) -> Unit = {},
    onNotificationsChange: (Boolean) -> Unit = {},
    onLogout: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var confirmLogout by remember { mutableStateOf(false) }
    var currentPassword by remember { mutableStateOf("") }
    var newPassword by remember { mutableStateOf("") }
    var confirmPassword by remember { mutableStateOf("") }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        // #208: section titles carried no heading semantics, so TalkBack's
        // heading navigation found nothing on this (scrollable) screen.
        Text(
            stringResource(R.string.settings_session),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.semantics { heading() },
        )
        val dash = stringResource(R.string.settings_value_placeholder)
        InfoRow(stringResource(R.string.settings_server), state.session.serverUrl ?: dash)
        InfoRow(stringResource(R.string.settings_username), state.session.username ?: dash)
        InfoRow(
            stringResource(R.string.settings_admin),
            if (state.session.isAdmin) stringResource(R.string.settings_yes) else stringResource(R.string.settings_no),
        )

        HorizontalDivider()

        // M25: appearance — language / date format / theme, editable.
        Text(
            stringResource(R.string.settings_appearance),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.semantics { heading() },
        )
        SettingDropdown(
            label = stringResource(R.string.settings_language),
            value = state.session.language.orEmpty(),
            options = listOf("en", "de", "it", "es", "fr"),
            optionLabel = { lang -> languageName(lang) },
            onSelect = onLanguageChange,
        )
        SettingDropdown(
            label = stringResource(R.string.settings_date_format),
            value = state.session.dateFormat.orEmpty(),
            options = DATE_FORMAT_OPTIONS,
            optionLabel = { fmt -> dateFormatName(fmt) },
            onSelect = onDateFormatChange,
        )
        SettingDropdown(
            label = stringResource(R.string.settings_theme),
            value = state.themePreference,
            options = listOf(
                AppSettingsRepository.THEME_SYSTEM,
                AppSettingsRepository.THEME_LIGHT,
                AppSettingsRepository.THEME_DARK,
            ),
            optionLabel = { pref ->
                when (pref) {
                    AppSettingsRepository.THEME_LIGHT -> stringResource(R.string.settings_theme_light)
                    AppSettingsRepository.THEME_DARK -> stringResource(R.string.settings_theme_dark)
                    else -> stringResource(R.string.settings_theme_system)
                }
            },
            onSelect = onThemeChange,
        )

        HorizontalDivider()

        // M25: password change.
        Text(
            stringResource(R.string.settings_password_title),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.semantics { heading() },
        )
        state.passwordErrorRes?.let { res ->
            Text(
                text = stringResource(res),
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
            )
        }
        state.passwordError?.let { error ->
            Text(
                text = error,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
            )
        }
        OutlinedTextField(
            value = currentPassword,
            onValueChange = { currentPassword = it },
            label = { Text(stringResource(R.string.settings_password_current)) },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = newPassword,
            onValueChange = { newPassword = it },
            label = { Text(stringResource(R.string.settings_password_new)) },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = confirmPassword,
            onValueChange = { confirmPassword = it },
            label = { Text(stringResource(R.string.settings_password_confirm)) },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            modifier = Modifier.fillMaxWidth(),
        )
        val changingPasswordLabel = stringResource(R.string.a11y_state_saving)
        OutlinedButton(
            onClick = {
                onChangePassword(currentPassword, newPassword, confirmPassword)
                currentPassword = ""
                newPassword = ""
                confirmPassword = ""
            },
            enabled = !state.isChangingPassword && newPassword.isNotBlank() && confirmPassword.isNotBlank(),
            modifier = Modifier
                .fillMaxWidth()
                .semantics { if (state.isChangingPassword) stateDescription = changingPasswordLabel },
        ) {
            if (state.isChangingPassword) {
                CircularProgressIndicator(modifier = Modifier.padding(end = 8.dp))
            }
            Text(stringResource(R.string.settings_password_change_button))
        }

        HorizontalDivider()

        // N8 (issue #814): TOTP two-factor enrollment/management (web parity).
        NavigationRow(stringResource(R.string.settings_two_factor_title), onClick = onTwoFactor)

        HorizontalDivider()

        // T104: propose data from what the graph already implies — the trigger
        // for graph-inferred relationship suggestions plus the Data screen that
        // reviews them (and the address-suggestion scan).
        Text(
            stringResource(R.string.settings_data),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.semantics { heading() },
        )
        state.relationshipSuggestErrorRes?.let { res ->
            Text(
                text = stringResource(res),
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.semantics { liveRegion = LiveRegionMode.Assertive },
            )
        }
        state.suggestedRelationshipCount?.let { count ->
            Text(
                text = if (count > 0) {
                    stringResource(R.string.settings_relationships_suggested, count)
                } else {
                    stringResource(R.string.settings_relationships_none)
                },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        val suggestingLabel = stringResource(R.string.a11y_state_saving)
        Button(
            onClick = onSuggestRelationships,
            enabled = !state.isSuggestingRelationships,
            modifier = Modifier
                .fillMaxWidth()
                .semantics { if (state.isSuggestingRelationships) stateDescription = suggestingLabel },
        ) {
            if (state.isSuggestingRelationships) {
                CircularProgressIndicator(modifier = Modifier.padding(end = 8.dp))
            }
            Text(stringResource(R.string.settings_suggest_relationships))
        }
        NavigationRow(stringResource(R.string.settings_data_review), onClick = onData)

        // Issue #348: admin-only user management, reachable only when the
        // session is an admin (the backend also 403s every admin route for
        // non-admins, so this gate is a navigation affordance, not a guard).
        if (state.session.isAdmin) {
            NavigationRow(stringResource(R.string.users_title), onClick = onManageUsers)
            NavigationRow(stringResource(R.string.sysevents_title), onClick = onSystemEvents)
        }

        HorizontalDivider()

        Text(
            stringResource(R.string.settings_tracking),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.semantics { heading() },
        )
        ToggleRow(
            label = stringResource(R.string.settings_call_tracking),
            checked = state.callTrackingEnabled,
            onCheckedChange = onCallTrackingChange,
        )
        ToggleRow(
            label = stringResource(R.string.settings_sms_tracking),
            checked = state.smsTrackingEnabled,
            onCheckedChange = onSmsTrackingChange,
        )
        ToggleRow(
            label = stringResource(R.string.settings_notifications),
            checked = state.notificationsEnabled,
            onCheckedChange = onNotificationsChange,
        )

        // M25: channels surfaces.
        NavigationRow(stringResource(R.string.settings_webhooks_title), onClick = onWebhooks)
        // Issue #413's Android follow-up (#573): API token management.
        NavigationRow(stringResource(R.string.settings_api_tokens_title), onClick = onApiTokens)
        NavigationRow(stringResource(R.string.settings_notifications_title), onClick = onNotificationChannels)
        // Issue #236: the Immich connection-config settings screen.
        NavigationRow(stringResource(R.string.settings_immich_title), onClick = onImmichSettings)

        // M26: one-time legacy circle/tag cleanup.
        NavigationRow(stringResource(R.string.settings_circle_tag_triage), onClick = onCircleTagTriage)

        Button(
            onClick = onCustomLinks,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(stringResource(R.string.custom_links_title))
        }

        val loggingOutLabel = stringResource(R.string.a11y_state_saving)
        Button(
            onClick = { confirmLogout = true },
            enabled = !state.isLoggingOut,
            modifier = Modifier
                .fillMaxWidth()
                .semantics { if (state.isLoggingOut) stateDescription = loggingOutLabel },
        ) {
            if (state.isLoggingOut) {
                CircularProgressIndicator(modifier = Modifier.padding(end = 8.dp))
            }
            Text(stringResource(R.string.settings_log_out))
        }
    }

    if (confirmLogout) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { confirmLogout = false },
            title = { Text(stringResource(R.string.settings_log_out_title)) },
            text = { Text(stringResource(R.string.settings_log_out_body)) },
            confirmButton = {
                Button(onClick = { confirmLogout = false; onLogout() }) {
                    Text(stringResource(R.string.settings_log_out))
                }
            },
            dismissButton = {
                androidx.compose.material3.TextButton(onClick = { confirmLogout = false }) {
                    Text(stringResource(R.string.settings_cancel))
                }
            },
        )
    }
}

@Composable
private fun InfoRow(label: String, value: String) {
    Column {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(text = value, style = MaterialTheme.typography.bodyLarge)
    }
}

@Composable
private fun ToggleRow(
    label: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
) {
    Row(
        // #214: a bare Switch has no text/contentDescription of its own — the
        // adjacent label Text was a separate, unassociated node, so TalkBack
        // announced the switch with no name at all. Modifier.toggleable on the
        // row merges the label into the switch's accessible name (the standard
        // Material3 labeled-switch pattern).
        modifier = Modifier
            .fillMaxWidth()
            .toggleable(value = checked, onValueChange = onCheckedChange, role = Role.Switch)
            .padding(vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = null)
    }
}

@Composable
private fun SettingDropdown(
    label: String,
    value: String,
    options: List<String>,
    optionLabel: @Composable (String) -> String,
    onSelect: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    Column {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Box {
            OutlinedButton(
                onClick = { expanded = true },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    text = if (value.isEmpty()) stringResource(R.string.settings_value_placeholder) else optionLabel(value),
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                    contentDescription = null,
                )
            }
            DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                options.forEach { option ->
                    DropdownMenuItem(
                        text = { Text(optionLabel(option)) },
                        onClick = {
                            expanded = false
                            onSelect(option)
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun NavigationRow(label: String, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { onClick() }
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.bodyLarge)
        Icon(
            Icons.AutoMirrored.Outlined.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun languageName(tag: String): String = when (tag) {
    "de" -> "Deutsch"
    "it" -> "Italiano"
    "es" -> "Español"
    "fr" -> "Français"
    else -> "English"
}

@Composable
private fun dateFormatName(format: String): String = when (format) {
    "us" -> stringResource(R.string.settings_date_format_us)
    "iso" -> stringResource(R.string.settings_date_format_iso)
    "ca" -> stringResource(R.string.settings_date_format_ca)
    "eu-hyphen" -> stringResource(R.string.settings_date_format_eu_hyphen)
    "us-mmm" -> stringResource(R.string.settings_date_format_us_mmm)
    "us-mmmm" -> stringResource(R.string.settings_date_format_us_mmmm)
    "eu-mmm" -> stringResource(R.string.settings_date_format_eu_mmm)
    "eu-mmmm" -> stringResource(R.string.settings_date_format_eu_mmmm)
    else -> stringResource(R.string.settings_date_format_eu)
}

private val DATE_FORMAT_OPTIONS = listOf(
    "eu", "us", "iso", "ca", "eu-hyphen", "us-mmm", "us-mmmm", "eu-mmm", "eu-mmmm",
)
