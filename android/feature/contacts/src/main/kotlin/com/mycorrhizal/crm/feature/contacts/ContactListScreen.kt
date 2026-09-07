package com.mycorrhizal.crm.feature.contacts

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import com.mycorrhizal.crm.ui.components.AccessibleIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.toggleableState
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.hilt.navigation.compose.hiltViewModel
import com.mycorrhizal.crm.model.network.BulkActions
import com.mycorrhizal.crm.model.network.Circle
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.model.network.SearchActivityHit
import com.mycorrhizal.crm.model.network.SearchNoteHit
import com.mycorrhizal.crm.model.network.SearchResult
import com.mycorrhizal.crm.ui.components.BrandFab
import com.mycorrhizal.crm.ui.components.EmptyState
import com.mycorrhizal.crm.ui.components.LoadingSkeleton
import com.mycorrhizal.crm.ui.R

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ContactListScreen(
    onContactClick: (Int) -> Unit,
    onCreateContact: () -> Unit = {},
    // Issue #150: nullable — null when there is no app-level drawer to open
    // (the tablet two-pane's persistent list pane, where the NavigationRail
    // replaces the drawer), which hides the hamburger.
    onMenuClick: (() -> Unit)? = {},
    onImportContacts: () -> Unit = {},
    // T93 (issue #710): opens the duplicate-review surface (web's "Review
    // duplicates" button on the contacts page).
    onReviewDuplicates: () -> Unit = {},
    viewModel: ContactListViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }

    // Reload when returning from the create/edit form so a newly added or
    // renamed contact shows up without a manual refresh.
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) viewModel.loadContacts()
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    LaunchedEffect(Unit) {
        viewModel.events.collect { event ->
            when (event) {
                is ContactListEvent.NavigateToContact -> onContactClick(event.contactId)
                ContactListEvent.ForceLogout -> Unit // handled by the app-level nav
            }
        }
    }

    ContactListScreenContent(
        uiState = state,
        onSearchQueryChange = viewModel::onSearchQueryChange,
        // A click only routes through the ViewModel event; the LaunchedEffect
        // above performs the actual navigation so each tap navigates once.
        onContactClick = viewModel::onContactClick,
        onCircleFilterChange = viewModel::onCircleFilterChange,
        onIncludeArchivedChange = viewModel::onIncludeArchivedChange,
        onIncludeFavoritesChange = viewModel::onIncludeFavoritesChange,
        onToggleFavorite = viewModel::toggleFavorite,
        onToggleSelection = viewModel::toggleSelection,
        onToggleSelectAll = viewModel::toggleSelectAll,
        onRunBulkAction = viewModel::runBulkAction,
        onBulkResultShown = viewModel::onBulkResultShown,
        onCreateContact = onCreateContact,
        onMenuClick = onMenuClick,
        onImportContacts = onImportContacts,
        onReviewDuplicates = onReviewDuplicates,
        onErrorShown = viewModel::onErrorShown,
        onLoadMore = viewModel::loadNextPage,
    )
}

/**
 * Stateless screen content — the four canonical states (loading / empty /
 * error / populated) are testable directly via this composable (ticket §10.4).
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun ContactListScreenContent(
    uiState: ContactListUiState,
    onSearchQueryChange: (String) -> Unit,
    onContactClick: (Int) -> Unit,
    onCircleFilterChange: (String?) -> Unit = {},
    onIncludeArchivedChange: (Boolean) -> Unit = {},
    // Issue #212: the favorites-only lens + per-row star toggle (web #173).
    onIncludeFavoritesChange: (Boolean) -> Unit = {},
    onToggleFavorite: (ContactSummary) -> Unit = {},
    onToggleSelection: (Int) -> Unit = {},
    onToggleSelectAll: () -> Unit = {},
    onRunBulkAction: (String, String?, String?) -> Unit = { _, _, _ -> },
    onBulkResultShown: () -> Unit = {},
    onCreateContact: () -> Unit = {},
    // Issue #150: see ContactListScreen — null hides the hamburger (no drawer).
    onMenuClick: (() -> Unit)? = {},
    onImportContacts: () -> Unit = {},
    onReviewDuplicates: () -> Unit = {},
    onErrorShown: () -> Unit = {},
    onLoadMore: () -> Unit = {},
) {
    val snackbarHostState = remember { SnackbarHostState() }
    var search by rememberSaveable { mutableStateOf(uiState.searchQuery) }
    // M23: inline bulk-selection mode (web parity). The selection *set* lives in the
    // ViewModel so filter changes can clear it; this flag is pure screen chrome.
    var selectMode by rememberSaveable { mutableStateOf(false) }
    // Set while a circle/tag picker dialog is open; the action it's picking
    // for (add vs remove) travels alongside it so confirming the picker can
    // set pendingAction directly (ported from the retired BulkOperationsScreen).
    var picker by remember { mutableStateOf<Pair<BulkPickerTarget, String>?>(null) }
    var pendingAction by remember { mutableStateOf<String?>(null) }
    var pendingCircleId by remember { mutableStateOf<String?>(null) }
    var pendingTagId by remember { mutableStateOf<String?>(null) }
    val listState = rememberLazyListState()

    // M9: infinite scroll — fire loadNextPage() once the user scrolls within 5 rows of the end
    // of the loaded contacts, matching ContactListViewModel.loadNextPage()'s own re-entrancy
    // guards (isLoading / isLoadingMore / hasMore), so this can fire repeatedly as the list
    // grows without risking duplicate in-flight requests.
    //
    // `remember` MUST be keyed on lastContactIndex. uiState is a plain parameter, not a State,
    // so an unkeyed `remember { derivedStateOf { ... uiState ... } }` captures the *first*
    // composition's uiState permanently — and the first composition is always the ViewModel's
    // empty initial state, pinning lastIndex at -1 and silently disabling pagination forever.
    // Only listState.layoutInfo is a real State and re-triggers the block on its own.
    val lastContactIndex = uiState.contacts.lastIndex
    val shouldLoadMore by remember(lastContactIndex) {
        derivedStateOf {
            val lastVisible = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: return@derivedStateOf false
            lastContactIndex >= 0 && lastVisible >= lastContactIndex - 5
        }
    }
    LaunchedEffect(shouldLoadMore, uiState.pagination.hasMore, uiState.pagination.isLoadingMore) {
        if (shouldLoadMore && uiState.pagination.hasMore && !uiState.pagination.isLoadingMore) {
            onLoadMore()
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
                    Text(
                        text = if (selectMode) {
                            stringResource(R.string.contacts_selected_count, uiState.selected.size)
                        } else {
                            stringResource(R.string.nav_contacts)
                        },
                        style = MaterialTheme.typography.titleLarge,
                    )
                },
                actions = {
                    if (selectMode) {
                        TextButton(
                            onClick = onToggleSelectAll,
                            modifier = Modifier.testTag("select-all"),
                        ) {
                            Text(stringResource(R.string.contacts_select_all), color = MaterialTheme.colorScheme.onPrimary)
                        }
                        AccessibleIconButton(
                            onClick = { selectMode = false },
                            modifier = Modifier.testTag("exit-select-mode"),
                        ) {
                            Icon(
                                Icons.Outlined.Close,
                                contentDescription = stringResource(R.string.contacts_exit_select_mode),
                                tint = MaterialTheme.colorScheme.onPrimary,
                            )
                        }
                    } else {
                        // T93: duplicate review (web's "Review duplicates"
                        // button) — only meaningful outside selection mode.
                        AccessibleIconButton(
                            onClick = onReviewDuplicates,
                            modifier = Modifier.testTag("review-duplicates"),
                        ) {
                            Icon(
                                Icons.Outlined.ContentCopy,
                                contentDescription = stringResource(R.string.duplicates_title),
                                tint = MaterialTheme.colorScheme.onPrimary,
                            )
                        }
                        AccessibleIconButton(
                            onClick = { selectMode = true },
                            modifier = Modifier.testTag("enter-select-mode"),
                        ) {
                            Icon(
                                Icons.Outlined.Checklist,
                                contentDescription = stringResource(R.string.contacts_select_mode),
                                tint = MaterialTheme.colorScheme.onPrimary,
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
            if (!selectMode) {
                BrandFab(onClick = onCreateContact) {
                    Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.contacts_new))
                }
            }
        },
        bottomBar = {
            if (selectMode && uiState.selected.isNotEmpty()) {
                BulkActionBar(
                    isRunning = uiState.isBulkRunning,
                    onArchive = { pendingAction = BulkActions.ARCHIVE },
                    onUnarchive = { pendingAction = BulkActions.UNARCHIVE },
                    onDelete = { pendingAction = BulkActions.DELETE },
                    onAddCircle = { picker = BulkPickerTarget.CIRCLE to BulkActions.ADD_CIRCLE },
                    onRemoveCircle = { picker = BulkPickerTarget.CIRCLE to BulkActions.REMOVE_CIRCLE },
                    onAddTag = { picker = BulkPickerTarget.TAG to BulkActions.ADD_TAG },
                    onRemoveTag = { picker = BulkPickerTarget.TAG to BulkActions.REMOVE_TAG },
                )
            }
        },
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            OutlinedTextField(
                value = search,
                onValueChange = {
                    search = it
                    onSearchQueryChange(it)
                },
                label = { Text(stringResource(R.string.contacts_search_hint)) },
                singleLine = true,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 8.dp),
            )

            ContactListFilterRow(
                circles = uiState.circles,
                circleFilter = uiState.circleFilter,
                includeArchived = uiState.includeArchived,
                includeFavorites = uiState.includeFavorites,
                onCircleFilterChange = onCircleFilterChange,
                onIncludeArchivedChange = onIncludeArchivedChange,
                onIncludeFavoritesChange = onIncludeFavoritesChange,
            )

            // T87: the notes/activities section trails every state (loading/empty/error/
            // populated) as the LazyColumn's last item, so it's reachable regardless of
            // whether the contact list itself has rows — the two result sets are independent.
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize().testTag("contact-list"),
            ) {
                when {
                    uiState.isLoading -> item {
                        LoadingSkeleton(modifier = Modifier.testTag("contact-list-loading"))
                    }
                    uiState.contacts.isEmpty() && uiState.error == null -> item {
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            modifier = Modifier.fillMaxWidth().padding(24.dp),
                        ) {
                            EmptyState(message = stringResource(R.string.contacts_empty))
                            Button(onClick = onImportContacts) {
                                Text(stringResource(R.string.import_title))
                            }
                        }
                    }
                    uiState.contacts.isEmpty() && uiState.error != null -> item {
                        Box(modifier = Modifier.fillMaxWidth().padding(24.dp)) {
                            Text(
                                text = uiState.error.orEmpty(),
                                color = MaterialTheme.colorScheme.error,
                                modifier = Modifier.align(Alignment.Center),
                            )
                        }
                    }
                    else -> {
                        items(uiState.contacts, key = { it.id }) { contact ->
                            ContactListItem(
                                contact = contact,
                                selected = contact.id in uiState.selected,
                                selectMode = selectMode,
                                onToggleFavorite = { onToggleFavorite(contact) },
                                onClick = {
                                    if (selectMode) onToggleSelection(contact.id) else onContactClick(contact.id)
                                },
                                onLongClick = {
                                    selectMode = true
                                    onToggleSelection(contact.id)
                                },
                            )
                        }
                        if (uiState.pagination.isLoadingMore) {
                            item {
                                Box(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
                                    CircularProgressIndicator(
                                        modifier = Modifier.size(32.dp).align(Alignment.Center),
                                    )
                                }
                            }
                        }
                    }
                }
                item {
                    SearchNotesActivitiesSection(
                        query = uiState.searchQuery,
                        searchResult = uiState.searchResult,
                        onContactClick = onContactClick,
                    )
                }
            }
        }
    }

    picker?.let { (target, action) ->
        val names: List<Pair<String, String>> = when (target) {
            BulkPickerTarget.CIRCLE -> uiState.circles.map { it.id to it.name }
            BulkPickerTarget.TAG -> uiState.tags.map { it.id to it.name }
        }
        AlertDialog(
            onDismissRequest = { picker = null },
            title = {
                Text(
                    stringResource(
                        if (target == BulkPickerTarget.CIRCLE) R.string.bulk_pick_circle else R.string.bulk_pick_tag,
                    ),
                )
            },
            text = {
                if (names.isEmpty()) {
                    Text(
                        stringResource(
                            if (target == BulkPickerTarget.CIRCLE) R.string.bulk_no_circles else R.string.bulk_no_tags,
                        ),
                    )
                } else {
                    LazyColumn(modifier = Modifier.heightIn(max = 320.dp)) {
                        items(names, key = { it.first }) { (id, name) ->
                            Text(
                                text = name,
                                style = MaterialTheme.typography.bodyLarge,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable {
                                        when (target) {
                                            BulkPickerTarget.CIRCLE -> pendingCircleId = id
                                            BulkPickerTarget.TAG -> pendingTagId = id
                                        }
                                        pendingAction = action
                                        picker = null
                                    }
                                    .padding(vertical = 12.dp),
                            )
                        }
                    }
                }
            },
            confirmButton = {
                TextButton(onClick = { picker = null }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }

    pendingAction?.let { action ->
        AlertDialog(
            onDismissRequest = {
                pendingAction = null
                pendingCircleId = null
                pendingTagId = null
            },
            title = { Text(stringResource(R.string.bulk_confirm_title)) },
            text = { Text(stringResource(R.string.bulk_confirm_body, uiState.selected.size)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        onRunBulkAction(action, pendingCircleId, pendingTagId)
                        pendingAction = null
                        pendingCircleId = null
                        pendingTagId = null
                    },
                ) {
                    Text(stringResource(R.string.action_confirm))
                }
            },
            dismissButton = {
                TextButton(
                    onClick = {
                        pendingAction = null
                        pendingCircleId = null
                        pendingTagId = null
                    },
                ) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }

    uiState.error?.let { message ->
        LaunchedEffect(message) {
            snackbarHostState.showSnackbar(message)
            onErrorShown()
        }
    }

    uiState.bulkResult?.let { result ->
        val message = stringResource(R.string.bulk_result, result.succeeded, result.failed)
        LaunchedEffect(result) {
            snackbarHostState.showSnackbar(message)
            onBulkResultShown()
        }
    }
}

/**
 * M23: which circle/tag-scoped action a picker dialog is choosing an id for.
 * Archive/unarchive/delete need no id and skip the picker, going straight to
 * the existing confirm dialog.
 */
private enum class BulkPickerTarget { CIRCLE, TAG }

/**
 * M23: the circle filter + archived toggle row above the list — the two list-breadth
 * controls web's ContactsPage filter row owns, missing on Android until now.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun ContactListFilterRow(
    circles: List<Circle>,
    circleFilter: String?,
    includeArchived: Boolean,
    includeFavorites: Boolean,
    onCircleFilterChange: (String?) -> Unit,
    onIncludeArchivedChange: (Boolean) -> Unit,
    onIncludeFavoritesChange: (Boolean) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            ExposedDropdownMenuBox(
                expanded = expanded,
                onExpandedChange = { expanded = it },
                modifier = Modifier.weight(1f),
            ) {
                // #209: a read-only OutlinedTextField hard-clips its single-line
                // value at the field edge with no ellipsis (Compose 1.7 removed
                // the TextField overflow control), so a long circle name rendered
                // "Neighbou" mid-word at 200% font scale. The anchor is therefore
                // built from OutlinedTextFieldDefaults.DecorationBox with the
                // value drawn by a plain Text, which *does* support
                // TextOverflow.Ellipsis — same Material chrome, ellipsising value.
                val displayValue = circleFilter ?: stringResource(R.string.contacts_all_circles)
                Box(
                    // mergeDescendants folds the label + value into the anchor's
                    // own node (the menuAnchor already marks it Role.DropdownList
                    // with a click), so TalkBack announces one coherent control.
                    modifier = Modifier
                        .fillMaxWidth()
                        .menuAnchor(MenuAnchorType.PrimaryNotEditable)
                        .testTag("circle-filter")
                        .semantics(mergeDescendants = true) {},
                ) {
                    OutlinedTextFieldDefaults.DecorationBox(
                        value = displayValue,
                        innerTextField = {
                            Text(
                                text = displayValue,
                                style = MaterialTheme.typography.bodyLarge,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        },
                        enabled = true,
                        singleLine = true,
                        visualTransformation = VisualTransformation.None,
                        interactionSource = remember { MutableInteractionSource() },
                        label = { Text(stringResource(R.string.contacts_filter_circle)) },
                        trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
                    )
                }
                ExposedDropdownMenu(
                    expanded = expanded,
                    onDismissRequest = { expanded = false },
                ) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.contacts_all_circles)) },
                        onClick = {
                            expanded = false
                            onCircleFilterChange(null)
                        },
                    )
                    circles.forEach { circle ->
                        DropdownMenuItem(
                            text = { Text(circle.name) },
                            onClick = {
                                expanded = false
                                onCircleFilterChange(circle.name)
                            },
                        )
                    }
                }
            }
        }
        // #209: at 200% font scale the two labeled switches no longer fit side
        // by side; FlowRow lets the favorites toggle drop to its own line (WCAG
        // 1.4.10) instead of clipping off the row.
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            // #214: see the archived-toggle comment below — the same
            // labeled-switch pattern, applied to the favorites lens.
            Row(
                verticalAlignment = Alignment.CenterVertically,
                // Gap between the switch thumb and its label — without it the
                // text butts straight against the toggle.
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                // #214: a bare Switch has no text/contentDescription of its own — the
                // adjacent "Show archived" Text was a separate, unassociated node, so
                // TalkBack announced the switch with no name at all. Modifier.toggleable
                // on the row merges the label into the switch's accessible name (the
                // standard Material3 labeled-switch pattern) and gives the whole row a
                // touch target wider than the switch's own 52x32dp default.
                modifier = Modifier
                    .heightIn(min = 48.dp)
                    .toggleable(
                        value = includeArchived,
                        onValueChange = onIncludeArchivedChange,
                        role = Role.Switch,
                    )
                    .testTag("archived-toggle"),
            ) {
                Switch(checked = includeArchived, onCheckedChange = null)
                Text(
                    text = stringResource(R.string.contacts_show_archived),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier
                    .heightIn(min = 48.dp)
                    .toggleable(
                        value = includeFavorites,
                        onValueChange = onIncludeFavoritesChange,
                        role = Role.Switch,
                    )
                    .testTag("favorites-toggle"),
            ) {
                Switch(checked = includeFavorites, onCheckedChange = null)
                Text(
                    text = stringResource(R.string.contacts_show_favorites),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
}

/**
 * M23: the inline bulk-action bar shown under selection mode (web's BulkActionsBar).
 * The action row is a horizontalScroll'd Row of seven buttons, so the later ones
 * (tag actions) sit off the initial viewport — tests must performScrollTo() first.
 */
@Composable
private fun BulkActionBar(
    isRunning: Boolean,
    onArchive: () -> Unit,
    onUnarchive: () -> Unit,
    onDelete: () -> Unit,
    onAddCircle: () -> Unit,
    onRemoveCircle: () -> Unit,
    onAddTag: () -> Unit,
    onRemoveTag: () -> Unit,
) {
    Surface(
        tonalElevation = 3.dp,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
        ) {
            TextButton(onClick = onArchive, enabled = !isRunning, modifier = Modifier.testTag("bulk-archive")) {
                Text(stringResource(R.string.bulk_archive))
            }
            TextButton(onClick = onUnarchive, enabled = !isRunning, modifier = Modifier.testTag("bulk-unarchive")) {
                Text(stringResource(R.string.bulk_unarchive))
            }
            TextButton(onClick = onDelete, enabled = !isRunning, modifier = Modifier.testTag("bulk-delete")) {
                Text(stringResource(R.string.action_delete))
            }
            TextButton(onClick = onAddCircle, enabled = !isRunning, modifier = Modifier.testTag("bulk-add-circle")) {
                Text(stringResource(R.string.bulk_add_circle))
            }
            TextButton(onClick = onRemoveCircle, enabled = !isRunning, modifier = Modifier.testTag("bulk-remove-circle")) {
                Text(stringResource(R.string.bulk_remove_circle))
            }
            TextButton(onClick = onAddTag, enabled = !isRunning, modifier = Modifier.testTag("bulk-add-tag")) {
                Text(stringResource(R.string.bulk_add_tag))
            }
            TextButton(onClick = onRemoveTag, enabled = !isRunning, modifier = Modifier.testTag("bulk-remove-tag")) {
                Text(stringResource(R.string.bulk_remove_tag))
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ContactListItem(
    contact: ContactSummary,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    selected: Boolean = false,
    selectMode: Boolean = false,
    // Issue #212: the per-row favorite toggle (web #173). Tapping the star
    // must never navigate into the detail page — the nested clickable's own
    // handler consumes the tap before the row's combinedClickable sees it
    // (Compose's stopPropagation equivalent).
    onToggleFavorite: () -> Unit = {},
    onLongClick: () -> Unit = onClick,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = onClick,
                // #214/#210: without onClickLabel/onLongClickLabel the long-press
                // is invisible in the semantics tree (long-clickable=true with no
                // description of what it does) — unreachable by TalkBack's
                // actions menu and by switch access. onClickLabel replaces the
                // generic "double-tap to activate" with something that says what
                // tapping actually does — which, per the caller's onClick wiring
                // (ContactListScreenContent), is "toggle selection" in select
                // mode and "open the contact" otherwise; a single static label
                // would announce the wrong action in one of the two modes.
                onClickLabel = if (selectMode) {
                    stringResource(R.string.contacts_row_select_action)
                } else {
                    stringResource(R.string.contacts_open_contact)
                },
                onLongClick = onLongClick,
                onLongClickLabel = stringResource(R.string.contacts_row_select_action),
            )
            // #214: the row's own combinedClickable above carries no Role or
            // checked state, and the Checkbox below is decorative
            // (onCheckedChange = null) — without this, TalkBack lost the
            // selected/unselected announcement entirely in select mode, not
            // just its label. Only the checkbox's Role/state, not its click
            // handling, needs to live on the row.
            .then(
                if (selectMode) {
                    Modifier.semantics {
                        role = Role.Checkbox
                        toggleableState = ToggleableState(selected)
                    }
                } else {
                    Modifier
                },
            )
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (selectMode) {
            // #214: a bare Checkbox has no text/contentDescription of its own,
            // and (as an independently screen-reader-focusable nested
            // clickable) it never picks up the row's merged name either — it
            // would announce with no label. The row's own combinedClickable
            // above already toggles selection on tap, so the checkbox here is
            // purely a visual indicator; onCheckedChange = null makes that
            // decorative status explicit instead of adding a second,
            // unlabeled way to trigger the same action.
            Checkbox(
                checked = selected,
                onCheckedChange = null,
            )
        }
        ContactAvatar(
            photoUri = contact.photoThumbnail,
            contentDescription = stringResource(R.string.contacts_photo_description, contact.displayName),
            size = 40.dp,
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = contact.displayName,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            val subtitle = listOfNotNull(contact.primaryEmail, contact.primaryPhone)
                .joinToString(" · ")
            if (subtitle.isNotBlank()) {
                Text(
                    text = subtitle,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        // Issue #212: the always-visible star toggle. Filled when favorite,
        // outlined otherwise; labeled with the contact's name so TalkBack
        // announces who it acts on (web's aria-label does the same).
        AccessibleIconButton(
            onClick = onToggleFavorite,
            modifier = Modifier.testTag("favorite-${contact.id}"),
        ) {
            Icon(
                imageVector = if (contact.isFavorite) Icons.Filled.Star else Icons.Outlined.StarBorder,
                contentDescription = stringResource(
                    if (contact.isFavorite) R.string.contacts_unfavorite_contact else R.string.contacts_favorite_contact,
                    contact.displayName,
                ),
                tint = if (contact.isFavorite) {
                    MaterialTheme.colorScheme.tertiary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant
                },
            )
        }
    }
}

/**
 * T87: the cross-entity (notes/activities) half of the folded search, below the contact list.
 * Collapsed by default and re-collapses on every new [query] — a stale expanded panel left open
 * across a query change would show the old query's hits, matching web's `SearchNotesActivities`.
 * Renders nothing when [searchResult] is null (no query, below the two-character gate, or the
 * request failed — see `ContactListViewModel.loadCrossEntitySearch`'s doc comment) or empty.
 */
@Composable
private fun SearchNotesActivitiesSection(
    query: String,
    searchResult: SearchResult?,
    onContactClick: (Int) -> Unit,
) {
    if (searchResult == null) return
    val notes = searchResult.notes
    val activities = searchResult.activities
    val total = notes.size + activities.size
    val resolvedRelation = searchResult.resolvedRelation
    if (resolvedRelation.isNullOrBlank() && total == 0) return

    var expanded by remember(query) { mutableStateOf(false) }

    Column(modifier = Modifier.padding(top = 8.dp)) {
        if (!resolvedRelation.isNullOrBlank()) {
            Card(
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer),
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            ) {
                Text(
                    text = stringResource(R.string.contacts_search_resolved_relation, resolvedRelation),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSecondaryContainer,
                    modifier = Modifier.padding(12.dp),
                )
            }
        }
        if (total > 0) {
            Card(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { expanded = !expanded }
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.contacts_search_results_header, total),
                            style = MaterialTheme.typography.titleSmall,
                        )
                        Text(
                            text = stringResource(R.string.contacts_search_results_hint),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Icon(
                        imageVector = if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                        contentDescription = null,
                    )
                }
                if (expanded) {
                    Column(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                        if (notes.isNotEmpty()) {
                            Text(
                                text = stringResource(R.string.contacts_search_notes_group),
                                style = MaterialTheme.typography.labelLarge,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            notes.forEach { note ->
                                SearchNoteRow(note = note, onContactClick = onContactClick)
                            }
                        }
                        if (activities.isNotEmpty()) {
                            Text(
                                text = stringResource(R.string.contacts_search_activities_group),
                                style = MaterialTheme.typography.labelLarge,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(top = if (notes.isNotEmpty()) 8.dp else 0.dp),
                            )
                            activities.forEach { activity ->
                                SearchActivityRow(activity = activity, onContactClick = onContactClick)
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SearchNoteRow(
    note: SearchNoteHit,
    onContactClick: (Int) -> Unit,
) {
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Text(text = note.content.orEmpty(), style = MaterialTheme.typography.bodyMedium)
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.padding(top = 4.dp),
        ) {
            val contactId = note.contactId
            val contactName = note.contactName
            if (contactId != null && !contactName.isNullOrBlank()) {
                AssistChip(
                    onClick = { onContactClick(contactId) },
                    label = { Text(contactName) },
                )
            } else {
                AssistChip(onClick = {}, enabled = false, label = { Text(stringResource(R.string.contacts_search_unfiled)) })
            }
            note.date?.take(10)?.let { date ->
                Text(text = date, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun SearchActivityRow(
    activity: SearchActivityHit,
    onContactClick: (Int) -> Unit,
) {
    // "Navigate to theirs" (T87 spec): an activity's own detail screen doesn't exist yet
    // (M9), so a hit navigates to its first participant's contact detail instead — the same
    // destination tapping that contact anywhere else in the app reaches.
    val firstContactId = activity.contacts?.firstOrNull()?.id
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .let { if (firstContactId != null) it.clickable { onContactClick(firstContactId) } else it }
            .padding(vertical = 6.dp),
    ) {
        Text(text = activity.title.orEmpty(), style = MaterialTheme.typography.bodyMedium)
        activity.date?.take(10)?.let { date ->
            Text(text = date, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
