package com.mycorrhizal.crm.feature.contacts

import android.Manifest
import android.app.Activity
import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Call
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.Map
import androidx.compose.material.icons.outlined.Message
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.InputChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
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
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.res.stringResource
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import androidx.core.view.WindowCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.hilt.navigation.compose.hiltViewModel
import coil3.compose.AsyncImage
import com.mycorrhizal.crm.model.network.Address
import com.mycorrhizal.crm.model.network.Card
import com.mycorrhizal.crm.model.network.Circle
import com.mycorrhizal.crm.model.network.ContactRecordResponse
import com.mycorrhizal.crm.model.network.Email
import com.mycorrhizal.crm.model.network.ExternalIdentity
import com.mycorrhizal.crm.model.network.ExternalSystems
import com.mycorrhizal.crm.model.network.FieldDefinition
import com.mycorrhizal.crm.model.network.ImmichAssetSummary
import com.mycorrhizal.crm.model.network.ImmichPerson
import com.mycorrhizal.crm.model.network.ImmichPersonSummary
import com.mycorrhizal.crm.model.network.OnlineService
import com.mycorrhizal.crm.model.network.Phone
import com.mycorrhizal.crm.model.network.ReminderCompletion
import com.mycorrhizal.crm.model.network.Tag
import com.mycorrhizal.crm.model.network.fieldValueDisplay
import com.mycorrhizal.crm.model.util.DateFormat
import com.mycorrhizal.crm.model.util.DateFormat.display
import com.mycorrhizal.crm.ui.LocalServerUrl
import com.mycorrhizal.crm.ui.components.AccessibleIconButton
import com.mycorrhizal.crm.ui.components.EmptyState
import com.mycorrhizal.crm.ui.LocalDarkTheme
import com.mycorrhizal.crm.ui.LocalDrawerOpen
import com.mycorrhizal.crm.ui.components.LoadingSkeleton
import com.mycorrhizal.crm.ui.theme.AppTypography
import com.mycorrhizal.crm.ui.theme.MycorrhizalFonts
import com.mycorrhizal.crm.feature.timeline.TimelineSection
import com.mycorrhizal.crm.feature.timeline.toTimelineItems
import com.mycorrhizal.crm.ui.R
import com.yalantis.ucrop.UCrop
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

private fun androidx.compose.ui.graphics.Color.toArgbCompat(): Int =
    android.graphics.Color.argb(
        (alpha * 255).toInt(),
        (red * 255).toInt(),
        (green * 255).toInt(),
        (blue * 255).toInt(),
    )

/**
 * The scroll window over which the contact name cross-fades out of the content
 * and into the collapsing app bar.
 *
 * This spans the two previous behaviors. A 96dp snap threshold felt "too
 * aggressive" (the bar switched while the photo still dominated), and waiting
 * for the whole ~204dp header block (photo + name + nickname + birthday) to
 * leave felt "too delayed". Fading across 120–180dp puts the midpoint at
 * ~150dp — exactly where the name reaches the top edge (24dp top padding +
 * 120dp photo + 8dp spacing = 152dp) — so the transfer reads as seamless and
 * tracks the scroll gesture continuously like an iOS interactive transition.
 */
private val HeaderFadeStartDp = 120.dp
private val HeaderFadeEndDp = 180.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ContactDetailScreen(
    onBack: () -> Unit,
    onEdit: (Int) -> Unit = {},
    onStayInTouch: (ContactRecordResponse) -> Unit = {},
    onDeleted: () -> Unit = {},
    onViewActivities: (Int) -> Unit = {},
    onViewNotes: (Int) -> Unit = {},
    onViewReminders: (Int) -> Unit = {},
    // N7: the contact-attachments sub-screen (issue #710, web parity).
    onViewAttachments: (Int) -> Unit = {},
    onViewRelationships: (Int) -> Unit = {},
    onViewCadence: (Int) -> Unit = {},
    onOpenInContacts: (String) -> Unit = {},
    onMerge: (Int) -> Unit = {},
    onViewLifeEvents: (Int) -> Unit = {},
    onViewGifts: (Int) -> Unit = {},
    onViewPreferences: (Int) -> Unit = {},
    onViewAgenda: (Int) -> Unit = {},
    // M11: the N2 prep-view briefing (replaces the "coming soon" stub).
    onViewPrep: (Int) -> Unit = {},
    // M15: the "Share this contact" flow (replaces the "coming soon" stub).
    // Carries the contact's VCard UID — the Share screen needs only that to
    // POST the share, and ContactDetailViewModel already loaded it.
    onShareContact: (String) -> Unit = {},
    // M14: the ego-centric network list, starting from this contact.
    onExploreConnections: (Int) -> Unit = {},
    onEditActivity: (Int) -> Unit = {},
    onEditNote: (Int) -> Unit = {},
    onEditReminder: (Int) -> Unit = {},
    viewModel: ContactDetailViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    // Reload when returning from the edit form so the detail shows saved changes.
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) viewModel.load()
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    // M24: one-shot events — a finished delete (navigate back) and an exported file (share
    // sheet). Consumed exactly once, then cleared via viewModel.onEventShown().
    val event by viewModel.events.collectAsStateWithLifecycle()
    val context = LocalContext.current
    // stringResource (not context.getString), so a Configuration change
    // (locale) invalidates these instead of the launcher callbacks below
    // capturing a stale string -- lint's LocalContextGetResourceValueCall.
    val cropFailedMessage = stringResource(R.string.profile_picture_crop_failed)
    val photoTooLargeMessage = stringResource(R.string.profile_picture_error_too_large)
    LaunchedEffect(event) {
        when (event) {
            is ContactDetailEvent.ContactDeleted -> {
                viewModel.onEventShown()
                onDeleted()
            }
            is ContactDetailEvent.ExportReady -> {
                val ready = event as ContactDetailEvent.ExportReady
                viewModel.onEventShown()
                shareExportedVcf(context, state.contact, ready.version, ready.bytes)
            }
            null -> Unit
        }
    }
    var showDeleteConfirm by remember { mutableStateOf(false) }
    var showArchiveConfirm by remember { mutableStateOf(false) }
    var showUnarchiveConfirm by remember { mutableStateOf(false) }
    // M20: undoing a completed reminder deletes its timeline entry — web's
    // ContactDetailPage confirms (`window.confirm`), so Android confirms too
    // (M17's delete-is-confirmed-first rule).
    var pendingUndoCompletionId by remember { mutableStateOf<Int?>(null) }
    // Issue #220: external-link deletes and Immich unlinking confirm first
    // (M17), matching web's `window.confirm` in ExternalLinkPanel.
    var pendingExternalLinkDelete by remember { mutableStateOf<ExternalIdentity?>(null) }
    var pendingImmichUnlink by remember { mutableStateOf(false) }
    val snackbarHostState = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()

    // Issue #219/#220: tapping either avatar opens the profile-photo flow. The
    // avatar is the entry point (web's ContactHeader): the gallery photo picker
    // (ActivityResultContracts.PickVisualMedia — no runtime permission needed)
    // when Immich isn't configured, else a source chooser (gallery / Immich).
    // Both paths run the picked photo through uCrop (web's react-easy-crop
    // equivalent), then upload the cropped image.
    // uCrop activity result: the cropped output is uploaded directly.
    val cropLauncher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            val data = result.data
            val outputUri = data?.let { UCrop.getOutput(it) }
            if (outputUri != null) {
                scope.launch {
                    val bytes = withContext(Dispatchers.IO) {
                        context.contentResolver.openInputStream(outputUri)?.use { it.readBytes() }
                    }
                    if (bytes != null && bytes.isNotEmpty()) {
                        viewModel.uploadPhoto(bytes, "image/jpeg")
                    }
                }
            } else if (data != null && UCrop.getError(data) != null) {
                scope.launch {
                    snackbarHostState.showSnackbar(cropFailedMessage)
                }
            }
        }
    }
    val launchCrop: (Uri) -> Unit = { sourceUri ->
        cropLauncher.launch(ucropIntent(context, sourceUri))
    }
    val photoPicker = rememberLauncherForActivityResult(
        ActivityResultContracts.PickVisualMedia(),
    ) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            val resolver = context.contentResolver
            // Probe the provider's declared size BEFORE reading anything — a
            // photo picker can offer a multi-GB video that would OOM the app
            // before a post-read size check ever ran (mirrors VcfImportScreen).
            val size = withContext(Dispatchers.IO) { queryPhotoSize(resolver, uri) }
            if (size != null && size > ContactDetailViewModel.MAX_PHOTO_SIZE_BYTES) {
                snackbarHostState.showSnackbar(photoTooLargeMessage)
                return@launch
            }
            val bytes = withContext(Dispatchers.IO) { readPhotoBytes(resolver, uri) }
            if (bytes.isEmpty()) return@launch
            // Backstop for providers that report no size: never feed a file
            // the server would reject with its own 400 into the crop step.
            if (bytes.size > ContactDetailViewModel.MAX_PHOTO_SIZE_BYTES) {
                snackbarHostState.showSnackbar(photoTooLargeMessage)
                return@launch
            }
            withContext(Dispatchers.IO) { writePhotoBytes(context, bytes) }?.let { launchCrop(Uri.fromFile(it)) }
        }
    }
    var photoSourceChooserOpen by remember { mutableStateOf(false) }
    var immichPickerOpen by remember { mutableStateOf(false) }
    // Issue #236: the Paperless/Seafile/Nextcloud create/link pickers — open/close is
    // local UI state, exactly like immichPickerOpen; the data behind each dialog lives
    // in the ViewModel.
    var paperlessPickerOpen by remember { mutableStateOf(false) }
    var seafilePickerOpen by remember { mutableStateOf(false) }
    var nextcloudPickerOpen by remember { mutableStateOf(false) }
    val onUploadProfilePicture = {
        if (state.immichConfigured) photoSourceChooserOpen = true
        else photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
    }

    // M24: action failures (delete/archive/export/membership writes) set `state.error`, which
    // the EmptyState branch already shows when there is no contact to render. For a loaded
    // contact the error had no surface at all — show it as a snackbar there (mirroring
    // ContactFormScreen/ContactListScreen), and leave the EmptyState to handle load failures.
    LaunchedEffect(state.error) {
        val message = state.error
        if (message != null && state.contact != null) {
            snackbarHostState.showSnackbar(message)
            viewModel.onErrorShown()
        }
    }

    // Drives the collapsing app bar: at the top the bar is transparent so the
    // large photo + name (the first list item) sit on the bone surface; as the
    // user scrolls, the name cross-fades out of the content and into the bar
    // while it turns green (Android Contacts / iOS-style interactive gesture —
    // the transition tracks the scroll position continuously, not a binary
    // show/hide).
    //
    // The fade spans the window between the two previous behaviors: it starts
    // at 120dp (where a 96dp threshold felt "too aggressive" — it switched
    // while the photo was still dominant) and completes at 180dp (where waiting
    // for the whole ~204dp header block felt "too delayed"). Midpoint is 150dp
    // — the point where the name reaches the top edge (24dp padding + 120dp
    // photo + 8dp spacing), so the transfer reads as seamless.
    val listState = rememberLazyListState()
    val collapseStartPx = with(LocalDensity.current) { HeaderFadeStartDp.toPx() }
    val collapseEndPx = with(LocalDensity.current) { HeaderFadeEndDp.toPx() }
    val collapseProgress by remember {
        derivedStateOf {
            if (listState.firstVisibleItemIndex > 0) {
                1f
            } else {
                ((listState.firstVisibleItemScrollOffset - collapseStartPx) /
                    (collapseEndPx - collapseStartPx)).coerceIn(0f, 1f)
            }
        }
    }
    // Icon color: dark over the bone surface at the top, white once the green
    // bar collapses in — lerped continuously so the tint follows the drag.
    val onSurface = MaterialTheme.colorScheme.onSurface
    val onPrimary = MaterialTheme.colorScheme.onPrimary
    val barIconColor = lerp(onSurface, onPrimary, collapseProgress)

    // The status bar follows the collapse: background (bone/boneDark) at the
    // top, primary (brand green) once the bar collapses in (overrides the
    // theme's default green for this screen). isAppearanceLightStatusBars has
    // two rules depending on the color role behind the bar -- see
    // MycorrhizalApp.kt's MainScaffold for the full explanation: `primary` is
    // the one M3 role that inverts between themes, so it needs `darkTheme`;
    // `background` follows the intuitive direction, so it needs `!darkTheme`.
    // While the navigation drawer is open, the app-level scaffold owns the
    // status bar, so this screen stays out of the way and re-applies its own
    // state when the drawer closes.
    val drawerOpen = LocalDrawerOpen.current
    val darkTheme = LocalDarkTheme.current
    val window = (LocalContext.current as android.app.Activity).window
    val collapsedColor = MaterialTheme.colorScheme.primary.toArgbCompat()
    val expandedColor = MaterialTheme.colorScheme.background.toArgbCompat()
    LaunchedEffect(drawerOpen, darkTheme, collapsedColor, expandedColor) {
        if (drawerOpen) return@LaunchedEffect
        snapshotFlow { collapseProgress }
            .collect { progress ->
                val collapsedNow = progress > 0.5f
                window.statusBarColor = if (collapsedNow) collapsedColor else expandedColor
                WindowCompat.getInsetsController(window, window.decorView)
                    .isAppearanceLightStatusBars = if (collapsedNow) darkTheme else !darkTheme
            }
    }
    // Restore the app-wide default (green app bar) when leaving this screen,
    // so the always-green sub-screens (life events, gifts, ...) get the right
    // status-bar icons rather than inheriting this screen's last state.
    //
    // Only restores if the drawer isn't open at dispose time: MainScaffold's
    // own effect already owns (and has already applied) the correct bar state
    // for that case, and writing here too would race with, and could stomp,
    // that effect -- this used to hardcode "drawer closed" unconditionally,
    // which was wrong whenever this screen was disposed while the drawer was
    // open. This effect must stay keyed on Unit (it should fire only on
    // unmount, not on every drawer/theme change while still on screen), so it
    // needs rememberUpdatedState to see the *live* values at actual dispose
    // time rather than the stale ones captured at first composition.
    val latestDrawerOpen = rememberUpdatedState(drawerOpen)
    val latestDarkTheme = rememberUpdatedState(darkTheme)
    val latestCollapsedColor = rememberUpdatedState(collapsedColor)
    DisposableEffect(Unit) {
        onDispose {
            if (latestDrawerOpen.value) return@onDispose
            window.statusBarColor = latestCollapsedColor.value
            WindowCompat.getInsetsController(window, window.decorView)
                .isAppearanceLightStatusBars = latestDarkTheme.value
        }
    }

    Scaffold(
        topBar = {
            // Transparent at the top (the large header shows through); green
            // with a small avatar + name once collapsed.
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            Icons.AutoMirrored.Outlined.ArrowBack,
                            contentDescription = stringResource(R.string.cd_back),
                            tint = barIconColor,
                        )
                    }
                },
                title = {
                    // Always composed so it can cross-fade in with the scroll;
                    // the alpha tracks collapseProgress (0 = hidden over the
                    // bone surface, 1 = fully shown once the bar is green).
                    state.contact?.let { contact ->
                        val card = contact.card
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                            modifier = Modifier.graphicsLayer { alpha = collapseProgress },
                        ) {
                            ContactAvatar(
                                photoUri = card?.photoUri ?: contact.photoThumbnail,
                                contentDescription = stringResource(
                                    R.string.contacts_photo_description,
                                    card?.displayName.orEmpty(),
                                ),
                                size = 36.dp,
                                onClick = onUploadProfilePicture,
                                onClickLabel = stringResource(R.string.contacts_photo_change),
                            )
                            Text(
                                text = card?.displayName ?: stringResource(R.string.contact_title_fallback),
                                style = MaterialTheme.typography.titleLarge,
                                color = barIconColor,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                },
                actions = {
                    state.contact?.let { contact ->
                        IconButton(onClick = { onEdit(contact.id) }) {
                            Icon(
                                Icons.Outlined.Edit,
                                contentDescription = stringResource(R.string.contact_edit),
                                tint = barIconColor,
                            )
                        }
                        // M24: the top-level action menu (archive/unarchive, export, stay in
                        // touch, share/prep stubs, delete). Wrapped in a Box so the
                        // DropdownMenu anchors to the ⋮ button rather than the whole bar.
                        var menuExpanded by remember { mutableStateOf(false) }
                        Box {
                            IconButton(onClick = { menuExpanded = true }) {
                                Icon(
                                    Icons.Outlined.MoreVert,
                                    contentDescription = stringResource(R.string.contact_actions_menu),
                                    tint = barIconColor,
                                )
                            }
                            DropdownMenu(
                                expanded = menuExpanded,
                                onDismissRequest = { menuExpanded = false },
                            ) {
                                if (contact.archived) {
                                    DropdownMenuItem(
                                        text = { Text(stringResource(R.string.contact_unarchive)) },
                                        onClick = {
                                            menuExpanded = false
                                            showUnarchiveConfirm = true
                                        },
                                    )
                                } else {
                                    DropdownMenuItem(
                                        text = { Text(stringResource(R.string.contact_archive)) },
                                        onClick = {
                                            menuExpanded = false
                                            showArchiveConfirm = true
                                        },
                                    )
                                }
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.contact_export_vcf4)) },
                                    onClick = {
                                        menuExpanded = false
                                        viewModel.exportVcf()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.contact_export_vcf3)) },
                                    onClick = {
                                        menuExpanded = false
                                        viewModel.exportVcf(version = 3)
                                    },
                                )
                                if (!contact.archived) {
                                    DropdownMenuItem(
                                        text = { Text(stringResource(R.string.contact_stay_in_touch)) },
                                        onClick = {
                                            menuExpanded = false
                                            onStayInTouch(contact)
                                        },
                                    )
                                }
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.contact_share)) },
                                    onClick = {
                                        menuExpanded = false
                                        onShareContact(contact.uid.orEmpty())
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.contact_prep_view)) },
                                    onClick = {
                                        menuExpanded = false
                                        onViewPrep(contact.id)
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.contact_network)) },
                                    onClick = {
                                        menuExpanded = false
                                        onExploreConnections(contact.id)
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.contact_delete), color = MaterialTheme.colorScheme.error) },
                                    onClick = {
                                        menuExpanded = false
                                        showDeleteConfirm = true
                                    },
                                )
                            }
                        }
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    // Transparent at the top, brand green once collapsed —
                    // lerped continuously so the color follows the drag rather
                    // than snapping. The nav/title/action icons are tinted
                    // explicitly from the same continuous progress.
                    containerColor = lerp(Color.Transparent, MaterialTheme.colorScheme.primary, collapseProgress),
                    scrolledContainerColor = MaterialTheme.colorScheme.primary,
                    navigationIconContentColor = barIconColor,
                    titleContentColor = barIconColor,
                    actionIconContentColor = barIconColor,
                ),
            )
        },
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { padding ->
        Box(modifier = Modifier.fillMaxSize().padding(padding)) {
            when {
                state.isLoading -> LoadingSkeleton()
                state.contact == null && (state.errorRes != null || state.error != null) ->
                    EmptyState(state.errorRes?.let { stringResource(it) } ?: state.error.orEmpty())
                state.contact == null -> EmptyState(stringResource(R.string.contact_not_found))
                else -> ContactDetailContent(
                    contact = state.contact!!,
                    listState = listState,
                    headerContentAlpha = 1f - collapseProgress,
                    deviceLookupKey = state.deviceLookupKey,
                    dateFormat = state.dateFormat,
                    fieldDefinitions = state.fieldDefinitions,
                    fieldValuesByDefinitionId = state.fieldValuesByDefinitionId,
                    allCircles = state.allCircles,
                    contactCircles = state.contactCircles,
                    allTags = state.allTags,
                    contactTags = state.contactTags,
                    onAddCircle = viewModel::addCircle,
                    onRemoveCircle = viewModel::removeCircle,
                    onAddTag = viewModel::addTag,
                    onRemoveTag = viewModel::removeTag,
                    onOpenInContacts = onOpenInContacts,
                    onViewActivities = onViewActivities,
                    onViewNotes = onViewNotes,
                    onViewReminders = onViewReminders,
                    onViewAttachments = onViewAttachments,
                    onViewRelationships = onViewRelationships,
                    onViewCadence = onViewCadence,
                    onMerge = onMerge,
                    onViewLifeEvents = onViewLifeEvents,
                    onViewGifts = onViewGifts,
                    onViewPreferences = onViewPreferences,
                    onViewAgenda = onViewAgenda,
                    onEditActivity = onEditActivity,
                    onEditNote = onEditNote,
                    onEditReminder = onEditReminder,
                    onCompleteReminder = viewModel::completeReminder,
                    completions = state.completions,
                    onUndoCompletion = { id -> pendingUndoCompletionId = id },
                    onUploadProfilePicture = onUploadProfilePicture,
                    // Issue #212: the header star toggle (web #173).
                    onToggleFavorite = viewModel::toggleFavorite,
                    externalIdentities = state.externalIdentities,
                    immichSummary = state.immichSummary,
                    onDeleteExternalIdentity = { pendingExternalLinkDelete = it },
                    onUnlinkImmich = { pendingImmichUnlink = true },
                    // Issue #236: the "Add link" entry points, gated per system.
                    paperlessConfigured = state.paperlessConfigured,
                    seafileConfigured = state.seafileConfigured,
                    nextcloudConfigured = state.nextcloudConfigured,
                    onAddPaperlessLink = {
                        paperlessPickerOpen = true
                        viewModel.loadPaperlessDocuments()
                    },
                    onAddSeafileLink = {
                        seafilePickerOpen = true
                        viewModel.loadSeafileLibraries()
                    },
                    onAddNextcloudLink = {
                        nextcloudPickerOpen = true
                        viewModel.loadNextcloudDir()
                    },
                )
            }
        }
    }

    // M24: confirm-before-destructive-action dialogs. Delete and archive match web's
    // confirmation semantics (soft-delete per /CLAUDE.md); unarchive is confirmed too, since
    // the ticket calls for confirmation on both directions.
    val contact = state.contact
    if (showDeleteConfirm && contact != null) {
        AlertDialog(
            onDismissRequest = { showDeleteConfirm = false },
            title = { Text(stringResource(R.string.contact_delete_title)) },
            text = {
                Text(
                    stringResource(
                        R.string.contact_delete_confirm,
                        contact.card?.displayName.orEmpty(),
                    ),
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !state.isMutating,
                    onClick = {
                        showDeleteConfirm = false
                        viewModel.deleteContact()
                    },
                ) {
                    Text(stringResource(R.string.action_delete), color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { showDeleteConfirm = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
    if (showArchiveConfirm && contact != null) {
        AlertDialog(
            onDismissRequest = { showArchiveConfirm = false },
            title = { Text(stringResource(R.string.contact_archive_title)) },
            text = {
                Text(
                    stringResource(
                        R.string.contact_archive_confirm,
                        contact.card?.displayName.orEmpty(),
                    ),
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !state.isMutating,
                    onClick = {
                        showArchiveConfirm = false
                        viewModel.setArchived(archived = true)
                    },
                ) {
                    Text(stringResource(R.string.contact_archive))
                }
            },
            dismissButton = {
                TextButton(onClick = { showArchiveConfirm = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
    if (showUnarchiveConfirm && contact != null) {
        AlertDialog(
            onDismissRequest = { showUnarchiveConfirm = false },
            title = { Text(stringResource(R.string.contact_unarchive_title)) },
            text = {
                Text(
                    stringResource(
                        R.string.contact_unarchive_confirm,
                        contact.card?.displayName.orEmpty(),
                    ),
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !state.isMutating,
                    onClick = {
                        showUnarchiveConfirm = false
                        viewModel.setArchived(archived = false)
                    },
                ) {
                    Text(stringResource(R.string.contact_unarchive))
                }
            },
            dismissButton = {
                TextButton(onClick = { showUnarchiveConfirm = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
    pendingUndoCompletionId?.let { completionId ->
        AlertDialog(
            onDismissRequest = { pendingUndoCompletionId = null },
            title = { Text(stringResource(R.string.reminder_completion_undo_title)) },
            text = { Text(stringResource(R.string.reminder_completion_undo_confirm)) },
            confirmButton = {
                TextButton(
                    enabled = !state.isMutating,
                    onClick = {
                        pendingUndoCompletionId = null
                        viewModel.undoCompletion(completionId)
                    },
                ) {
                    Text(stringResource(R.string.reminder_completion_undo), color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingUndoCompletionId = null }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }

    // Issue #219/#220: the profile-photo source chooser (gallery / Immich) —
    // shown only when Immich is configured, mirroring web's upload dialog
    // (the Immich entry point appears only when configured).
    if (photoSourceChooserOpen) {
        AlertDialog(
            onDismissRequest = { photoSourceChooserOpen = false },
            title = { Text(stringResource(R.string.profile_picture_source_title)) },
            text = {
                Column {
                    TextButton(
                        modifier = Modifier.fillMaxWidth(),
                        onClick = {
                            photoSourceChooserOpen = false
                            photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                        },
                    ) {
                        Text(stringResource(R.string.profile_picture_source_gallery))
                    }
                    TextButton(
                        modifier = Modifier.fillMaxWidth(),
                        onClick = {
                            photoSourceChooserOpen = false
                            immichPickerOpen = true
                        },
                    ) {
                        Text(stringResource(R.string.profile_picture_source_immich))
                    }
                }
            },
            confirmButton = {},
            dismissButton = {
                TextButton(onClick = { photoSourceChooserOpen = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }

    // Issue #220: external-link delete / Immich unlink confirms (M17 —
    // delete-is-confirmed-first, matching web's `window.confirm`).
    pendingExternalLinkDelete?.let { identity ->
        AlertDialog(
            onDismissRequest = { pendingExternalLinkDelete = null },
            title = { Text(stringResource(R.string.external_links_remove_title)) },
            text = {
                Text(stringResource(R.string.external_links_remove_confirm, identity.system.orEmpty(), identity.externalId))
            },
            confirmButton = {
                TextButton(
                    enabled = !state.isExternalLinkMutating,
                    onClick = {
                        pendingExternalLinkDelete = null
                        viewModel.deleteExternalIdentity(identity.id)
                    },
                ) {
                    Text(stringResource(R.string.action_delete), color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingExternalLinkDelete = null }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
    if (pendingImmichUnlink) {
        AlertDialog(
            onDismissRequest = { pendingImmichUnlink = false },
            title = { Text(stringResource(R.string.immich_unlink_title)) },
            text = { Text(stringResource(R.string.immich_unlink_confirm)) },
            confirmButton = {
                TextButton(
                    enabled = !state.isExternalLinkMutating,
                    onClick = {
                        pendingImmichUnlink = false
                        viewModel.unlinkImmichPerson()
                    },
                ) {
                    Text(stringResource(R.string.immich_unlink), color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingImmichUnlink = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }

    // Issue #219/#220: the "choose from Immich" profile-photo picker.
    if (immichPickerOpen) {
        val contactUid = state.contact?.uid.orEmpty()
        ImmichPhotoPickerDialog(
            contactUid = contactUid,
            isLinked = state.immichSummary != null || state.externalIdentities.any { it.system == ExternalSystems.IMMICH },
            people = state.immichPeople,
            peopleLoading = state.immichPeopleLoading,
            assets = state.immichAssets,
            assetsLoading = state.immichAssetsLoading,
            onLoadPeople = viewModel::loadImmichPeople,
            onLinkPerson = viewModel::linkImmichPerson,
            onLoadAssets = viewModel::loadImmichAssets,
            onPick = { assetId ->
                viewModel.pickImmichAsset(assetId) { bytes ->
                    scope.launch {
                        withContext(Dispatchers.IO) { writePhotoBytes(context, bytes) }?.let { launchCrop(Uri.fromFile(it)) }
                    }
                }
            },
            onDismiss = { immichPickerOpen = false },
        )
    }

    // Issue #236: the Paperless/Seafile/Nextcloud create/link pickers. Tapping an item
    // closes the dialog immediately and fires the link call — success refreshes the
    // panel, failure surfaces via the screen's existing error snackbar (see the
    // ViewModel's doc comment on this section).
    if (paperlessPickerOpen) {
        PaperlessDocumentPickerDialog(
            documents = state.paperlessDocuments,
            loading = state.paperlessDocumentsLoading,
            onSelect = { document ->
                paperlessPickerOpen = false
                viewModel.linkPaperlessDocument(document)
            },
            onDismiss = { paperlessPickerOpen = false },
        )
    }
    if (seafilePickerOpen) {
        SeafileFilePickerDialog(
            libraries = state.seafileLibraries,
            browseRepoId = state.seafileBrowseRepoId,
            browsePath = state.seafileBrowsePath,
            browseItems = state.seafileBrowseItems,
            loading = state.seafileBrowseLoading,
            onEnterLibrary = { repoId -> viewModel.enterSeafileDir(repoId) },
            onEnterDir = { path -> viewModel.enterSeafileDir(state.seafileBrowseRepoId.orEmpty(), path) },
            onBack = viewModel::backSeafileDir,
            onSelect = { item ->
                seafilePickerOpen = false
                viewModel.linkSeafileItem(item)
            },
            onDismiss = { seafilePickerOpen = false },
        )
    }
    if (nextcloudPickerOpen) {
        NextcloudFilePickerDialog(
            browsePath = state.nextcloudBrowsePath,
            browseItems = state.nextcloudBrowseItems,
            loading = state.nextcloudBrowseLoading,
            onEnterDir = viewModel::loadNextcloudDir,
            onSelect = { item ->
                nextcloudPickerOpen = false
                viewModel.linkNextcloudItem(item)
            },
            onDismiss = { nextcloudPickerOpen = false },
        )
    }
}

@Composable
@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
fun ContactDetailContent(
    contact: ContactRecordResponse,
    listState: LazyListState = rememberLazyListState(),
    /** 0→1 as the name fades out of the content into the collapsing app bar. */
    headerContentAlpha: Float = 1f,
    deviceLookupKey: String? = null,
    /** The signed-in user's `date_format` preference; falls back to "eu" when absent. */
    dateFormat: String? = null,
    /** T84 (read-only slice): the user's custom field definitions and this contact's values. */
    fieldDefinitions: List<FieldDefinition> = emptyList(),
    fieldValuesByDefinitionId: Map<String, Any?> = emptyMap(),
    // M24: inline circle/tag editors. `all*` back the add menus, `contact*` are the currently
    // applied memberships derived from the join rows.
    allCircles: List<Circle> = emptyList(),
    contactCircles: List<Circle> = emptyList(),
    allTags: List<Tag> = emptyList(),
    contactTags: List<Tag> = emptyList(),
    onAddCircle: (Circle) -> Unit = {},
    onRemoveCircle: (Circle) -> Unit = {},
    onAddTag: (Tag) -> Unit = {},
    onRemoveTag: (Tag) -> Unit = {},
    onOpenInContacts: (String) -> Unit = {},
    onViewActivities: (Int) -> Unit = {},
    onViewNotes: (Int) -> Unit = {},
    onViewReminders: (Int) -> Unit = {},
    // N7: the contact-attachments sub-screen (issue #710, web parity).
    onViewAttachments: (Int) -> Unit = {},
    onViewRelationships: (Int) -> Unit = {},
    onViewCadence: (Int) -> Unit = {},
    onMerge: (Int) -> Unit = {},
    onViewLifeEvents: (Int) -> Unit = {},
    onViewGifts: (Int) -> Unit = {},
    onViewPreferences: (Int) -> Unit = {},
    onViewAgenda: (Int) -> Unit = {},
    onEditActivity: (Int) -> Unit = {},
    onEditNote: (Int) -> Unit = {},
    onEditReminder: (Int) -> Unit = {},
    onCompleteReminder: (Int) -> Unit = {},
    completions: List<ReminderCompletion> = emptyList(),
    onUndoCompletion: (Int) -> Unit = {},
    /**
     * Issue #219: opens the profile-photo upload flow. The large avatar is the
     * entry point (web's `ContactHeader`); null renders the avatar plain.
     */
    onUploadProfilePicture: (() -> Unit)? = null,
    // Issue #212: the header star toggle (web #173) — optimistic in the
    // ViewModel with a rollback on failure.
    onToggleFavorite: () -> Unit = {},
    // Issue #220: the External Links panel (ExternalIdentity substrate). The
    // Immich link renders as a rich row (thumbnail, person name, photo count)
    // via [immichSummary]; other systems render generically.
    externalIdentities: List<ExternalIdentity> = emptyList(),
    immichSummary: ImmichPersonSummary? = null,
    onDeleteExternalIdentity: (ExternalIdentity) -> Unit = {},
    onUnlinkImmich: () -> Unit = {},
    // Issue #236: whether each file/document integration is configured — gates
    // its "Add link" row below, mirroring web's FileLinksPanel.
    paperlessConfigured: Boolean = false,
    seafileConfigured: Boolean = false,
    nextcloudConfigured: Boolean = false,
    onAddPaperlessLink: () -> Unit = {},
    onAddSeafileLink: () -> Unit = {},
    onAddNextcloudLink: () -> Unit = {},
) {
    val card = contact.card
    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .testTag("contact-detail-list"),
        state = listState,
    ) {
        item {
            // Large photo + name header on the bone surface. It sits under the
            // transparent app bar; scrolling collapses the app bar over it
            // (Android Contacts pattern).
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 24.dp, vertical = 24.dp)
                    // Cross-fade the large header out as the name transfers
                    // into the collapsing app bar (mirrors the bar-title alpha).
                    .graphicsLayer { alpha = headerContentAlpha },
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                ContactAvatar(
                    photoUri = card?.photoUri ?: contact.photoThumbnail,
                    contentDescription = stringResource(
                        R.string.contacts_photo_description,
                        card?.displayName.orEmpty(),
                    ),
                    size = 120.dp,
                    onClick = onUploadProfilePicture,
                    onClickLabel = stringResource(R.string.contacts_photo_change),
                )
                // #208: the contact name is the de facto page heading but
                // carried no heading semantics, so TalkBack's heading
                // navigation skipped it. The favorite star sits beside it
                // (web's ContactHeader), kept out of the heading's semantics.
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Text(
                        text = card?.displayName ?: stringResource(R.string.contact_title_fallback),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.semantics { heading() },
                    )
                    // Issue #212: the always-visible star toggle, mirroring web
                    // ContactHeader — optimistic flip + rollback in the
                    // ViewModel so the star never silently disagrees with the DB.
                    AccessibleIconButton(onClick = onToggleFavorite) {
                        Icon(
                            imageVector = if (contact.isFavorite) Icons.Filled.Star else Icons.Outlined.StarBorder,
                            contentDescription = stringResource(
                                if (contact.isFavorite) R.string.contacts_unfavorite_contact else R.string.contacts_favorite_contact,
                                card?.displayName.orEmpty(),
                            ),
                            tint = if (contact.isFavorite) {
                                MaterialTheme.colorScheme.tertiary
                            } else {
                                MaterialTheme.colorScheme.onSurfaceVariant
                            },
                        )
                    }
                }
                val nickname = card?.nicknames?.firstOrNull()?.name
                val birthday = card?.anniversaries?.firstOrNull { it.kind == "birth" }?.date?.partial
                if (nickname != null) {
                    Text(
                        text = "\"$nickname\"",
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (birthday != null) {
                    Text(
                        text = stringResource(
                            R.string.contact_birthday_label,
                            birthday.display(dateFormat ?: DateFormat.EU),
                        ),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        if (!card?.personalInfo.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_personal_info)) {
                    card?.personalInfo?.forEach { info ->
                        InfoRow("${info.kind.orEmpty()}: ${info.value.orEmpty()}")
                    }
                }
            }
        }
        if (!card?.phones.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_phone)) {
                    card?.phones?.forEach { PhoneRow(it) }
                }
            }
        }
        if (!card?.addresses.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_address)) {
                    card?.addresses?.forEach { AddressRow(it) }
                }
            }
        }
        if (!card?.emails.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_email)) {
                    card?.emails?.forEach { EmailRow(it) }
                }
            }
        }
        val onlineServices = (card?.imppAddresses.orEmpty() +
            card?.socialProfiles.orEmpty() +
            card?.otherOnlineServices.orEmpty())
        if (onlineServices.isNotEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_online_services)) {
                    onlineServices.forEach { OnlineServiceRow(it) }
                }
            }
        }
        if (!card?.links.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_links)) {
                card?.links?.forEach { link ->
                    val uri = link.uri.orEmpty()
                    LinkRow(
                        uri = uri,
                        label = link.label ?: uri,
                        linkType = MobileLinkRegistry.forProtocol(link.label),
                    )
                }
                }
            }
        }
        if (!card?.organizations.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_organization)) {
                    card?.organizations?.forEach { org ->
                        org.name?.let { InfoRow(it) }
                    }
                }
            }
        }
        if (!card?.notes.isNullOrEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_notes)) {
                    card?.notes?.forEach { note ->
                        note.note?.let { InfoRow(it) }
                    }
                }
            }
        }
        // M24: circles and tags are now editable inline from the detail page (previously
        // circles were a read-only comma-joined string and tags weren't displayed at all).
        item {
            SectionCard(stringResource(R.string.contact_circles)) {
                ChipEditorRow(
                    items = contactCircles,
                    allItems = allCircles,
                    label = { it.name },
                    isApplied = { c -> contactCircles.any { it.id == c.id } },
                    onAdd = onAddCircle,
                    onRemove = onRemoveCircle,
                    emptyText = stringResource(R.string.contact_circles_empty),
                    addLabel = stringResource(R.string.contact_circles_add),
                )
            }
        }
        item {
            SectionCard(stringResource(R.string.contact_tags)) {
                ChipEditorRow(
                    items = contactTags,
                    allItems = allTags,
                    label = { it.name },
                    isApplied = { t -> contactTags.any { it.id == t.id } },
                    onAdd = onAddTag,
                    onRemove = onRemoveTag,
                    emptyText = stringResource(R.string.contact_tags_empty),
                    addLabel = stringResource(R.string.contact_tags_add),
                )
            }
        }
        // T84 (read-only slice): one row per definition, iterated over the definitions list
        // rather than the values map — this is what makes a value whose definition was deleted
        // since it was set (definitions and values are fetched independently and can disagree)
        // simply unreachable rather than something that needs a special-case skip.
        if (fieldDefinitions.isNotEmpty()) {
            item {
                SectionCard(stringResource(R.string.contact_custom_fields)) {
                    fieldDefinitions.forEach { definition ->
                        val value = fieldValuesByDefinitionId[definition.id]
                        // "—" mirrors the web app's own hardcoded no-value placeholder
                        // (CustomFieldValueRow.tsx) — a punctuation glyph, not translated text.
                        val display = fieldValueDisplay(definition, value).ifBlank { "—" }
                        InfoRow("${definition.label.orEmpty()}: $display")
                    }
                }
            }
        }
        if (externalIdentities.isNotEmpty() || paperlessConfigured || seafileConfigured || nextcloudConfigured) {
            item {
                // Issue #220: the External Links panel (ExternalIdentity
                // substrate). Rendered when there's a link to show OR a
                // configured system to add one for (issue #236 widened the
                // guard so the "Add link" rows are reachable before any link
                // exists — mirrors web's FileLinksPanel `configured.<system>`
                // gate). The Immich link is a rich row (thumbnail + person
                // name + photo count); other systems render as
                // system/external_id with open+delete.
                SectionCard(stringResource(R.string.external_links_title)) {
                    externalIdentities.forEach { identity ->
                        if (identity.system == ExternalSystems.IMMICH) {
                            ImmichLinkRow(
                                identity = identity,
                                summary = immichSummary,
                                contactUid = contact.uid,
                                onUnlink = onUnlinkImmich,
                            )
                        } else {
                            GenericExternalLinkRow(identity, onDelete = { onDeleteExternalIdentity(identity) })
                        }
                    }
                    if (paperlessConfigured) {
                        AddExternalLinkRow(stringResource(R.string.external_links_add_paperless), onAddPaperlessLink)
                    }
                    if (seafileConfigured) {
                        AddExternalLinkRow(stringResource(R.string.external_links_add_seafile), onAddSeafileLink)
                    }
                    if (nextcloudConfigured) {
                        AddExternalLinkRow(stringResource(R.string.external_links_add_nextcloud), onAddNextcloudLink)
                    }
                }
            }
        }
        item {
            // Unified timeline: the contact's activities/notes/reminders merged
            // newest-first (Phase 2 item 10). Tapping a row routes to its edit form.
            SectionTitle(stringResource(R.string.contact_timeline))
            TimelineSection(
                items = contact.toTimelineItems(completions),
                onEditActivity = onEditActivity,
                onEditNote = onEditNote,
                onEditReminder = onEditReminder,
                onCompleteReminder = onCompleteReminder,
                onUndoCompletion = onUndoCompletion,
            )
        }
        if (deviceLookupKey != null) {
            item {
                androidx.compose.material3.ListItem(
                    headlineContent = { Text(stringResource(R.string.contact_open_in_contacts), style = MaterialTheme.typography.bodyLarge) },
                    trailingContent = {
                        Icon(
                            imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                            contentDescription = null,
                        )
                    },
                    modifier = Modifier.fillMaxWidth().clickable { onOpenInContacts(deviceLookupKey) },
                )
            }
        }
        item {
            // Entry points to the per-type list screens (full management view).
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_activities), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewActivities(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_notes), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewNotes(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_reminders), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewReminders(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_attachments), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewAttachments(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_relationships), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewRelationships(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_cadence), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewCadence(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_merge), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onMerge(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_life_events), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewLifeEvents(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_gifts), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewGifts(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_preferences), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewPreferences(contact.id) },
            )
            androidx.compose.material3.ListItem(
                headlineContent = { Text(stringResource(R.string.contact_agenda), style = MaterialTheme.typography.bodyLarge) },
                trailingContent = {
                    Icon(
                        imageVector = Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                    )
                },
                modifier = Modifier.fillMaxWidth().clickable { onViewAgenda(contact.id) },
            )
        }
        item { Box(modifier = Modifier.size(32.dp)) }
    }
}


@Composable
private fun SectionCard(title: String, content: @Composable () -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHighest),
    ) {
        Column(modifier = Modifier.padding(vertical = 8.dp)) {
            // T63 Android port: field-group captions ("Phone", "Address",
            // "Email", ...) get IBM Plex Mono, mirroring the web's
            // EditableField/EditableArrayField caption treatment (contrast
            // against the sans/mono field values below). labelLarge itself
            // stays global-sans (it's also the Button/NavigationDrawerItem
            // default) -- scoped here via .copy(fontFamily = ...) instead.
            // #208: section titles carried no heading semantics, so
            // TalkBack's heading navigation skipped every card on this
            // screen. One fix here covers all SectionCard call sites.
            Text(
                text = title,
                style = MaterialTheme.typography.labelLarge.copy(fontFamily = MycorrhizalFonts.mono),
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier
                    .padding(horizontal = 16.dp, vertical = 8.dp)
                    .semantics { heading() },
            )
            content()
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    // T63 Android port: see SectionCard's matching comment above.
    // #208: see SectionCard's matching comment above.
    Text(
        text = text,
        style = MaterialTheme.typography.labelLarge.copy(fontFamily = MycorrhizalFonts.mono),
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .semantics { heading() },
    )}

@Composable
private fun InfoRow(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodyLarge,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
    )
}

@Composable
private fun EmailRow(email: Email) {
    val context = LocalContext.current
    val address = email.address.orEmpty()
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = address,
            style = AppTypography.mono,
            modifier = Modifier.weight(1f),
        )
        // M7: the type is `contexts[0]` (web parity — the form's type dropdown writes
        // there), with `label` as a fallback for legacy flat-Type data (backend's
        // buildEmails maps ContactEmail.Type -> Email.Label).
        (email.contexts?.firstOrNull() ?: email.label)?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        if (address.isNotBlank()) {
            IconButton(onClick = { context.startActivity(FieldActions.emailIntent(address)) }) {
                Icon(
                    Icons.Outlined.Email,
                    contentDescription = stringResource(R.string.cd_compose_email),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            IconButton(onClick = { FieldActions.copyText(context, "email", address) }) {
                Icon(
                    Icons.Outlined.ContentCopy,
                    contentDescription = stringResource(R.string.cd_copy_email),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

@Composable
private fun PhoneRow(phone: Phone) {
    val context = LocalContext.current
    val number = phone.number.orEmpty()
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = number,
            style = AppTypography.mono,
            modifier = Modifier.weight(1f),
        )
        // M7: the type is `features[0] ?: contexts[0]` (web parity), matching what
        // the form's type dropdown reads. `label` is X-ABLabel, not the type.
        val typeToken = phone.features?.firstOrNull() ?: phone.contexts?.firstOrNull()
        if (!typeToken.isNullOrBlank()) {
            Text(typeToken, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (number.isNotBlank()) {
            IconButton(onClick = { context.startActivity(FieldActions.dialIntent(number)) }) {
                Icon(
                    Icons.Outlined.Call,
                    contentDescription = stringResource(R.string.cd_call),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            // SMS for mobile numbers (T34 phone detection). Mirrors the web
            // app's `phoneHasToken` (ContactInformation.tsx): a phone is SMS-able
            // if `features`, `contexts`, or the type/label carries a cell/mobile
            // token. The backend populates `features` only on vCard import —
            // CRM-created contacts carry the type in `label`, so checking only
            // `features` (as this did) hid the SMS button for those.
            if (phone.isMobile()) {
                IconButton(onClick = { context.startActivity(FieldActions.smsIntent(number)) }) {
                    Icon(
                        Icons.Outlined.Message,
                        contentDescription = stringResource(R.string.cd_text),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
            }
            IconButton(onClick = { FieldActions.copyText(context, "phone", number) }) {
                Icon(
                    Icons.Outlined.ContentCopy,
                    contentDescription = stringResource(R.string.cd_copy_phone),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

/** T34 phone-feature detection, mirroring the web app's `phoneHasToken`. */
private fun Phone.isMobile(): Boolean {
    val tokens = (features.orEmpty() + contexts.orEmpty() + listOfNotNull(label))
        .map { it.trim().lowercase() }
    return tokens.any { it == "cell" || it == "mobile" }
}

@Composable
private fun AddressRow(address: Address) {
    val context = LocalContext.current
    val text = address.full
        ?: address.components?.joinToString(", ") { it.value.orEmpty() }
        ?: ""
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.weight(1f),
        )
        if (text.isNotBlank()) {
            IconButton(onClick = { context.startActivity(FieldActions.mapIntent(text)) }) {
                Icon(
                    Icons.Outlined.Map,
                    contentDescription = stringResource(R.string.cd_open_maps),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            IconButton(onClick = { FieldActions.copyText(context, "address", text) }) {
                Icon(
                    Icons.Outlined.ContentCopy,
                    contentDescription = stringResource(R.string.cd_copy_address),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

@Composable
private fun OnlineServiceRow(service: OnlineService) {
    val context = LocalContext.current
    val handle = service.uri ?: service.user.orEmpty()
    val serviceName = service.service ?: service.label.orEmpty()
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                if (serviceName.isNotBlank()) {
                    Text(
                        text = serviceName,
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (handle.isNotBlank()) {
                    Text(
                        text = handle,
                        style = MaterialTheme.typography.bodyLarge,
                        maxLines = 1,
                        overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                    )
                }
            }
            if (handle.isNotBlank()) {
                IconButton(onClick = { FieldActions.copyText(context, "url", handle) }) {
                    Icon(
                        Icons.Outlined.ContentCopy,
                        contentDescription = stringResource(R.string.cd_copy_link),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
            }
        }
        // Item 12 (§7.6) enrichment: the service (e.g. "Signal") maps to a
        // registry protocol, whose actions show once the app is detected.
        MobileLinkRegistry.forProtocol(serviceName)?.let { linkType ->
            if (handle.isNotBlank()) {
                MobileLinkActions(linkType = linkType, handle = handle)
            }
        }
    }
}

@Composable
private fun LinkRow(
    uri: String,
    label: String,
    linkType: MobileLinkType?,
) {
    val context = LocalContext.current
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(
                text = label,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.primary,
                maxLines = 1,
                overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (uri.isNotBlank()) {
                IconButton(onClick = { context.startActivity(FieldActions.browserIntent(uri)) }) {
                    Icon(
                        Icons.Outlined.OpenInNew,
                        contentDescription = stringResource(R.string.cd_open_link),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
                IconButton(onClick = { FieldActions.copyText(context, "url", uri) }) {
                    Icon(
                        Icons.Outlined.ContentCopy,
                        contentDescription = stringResource(R.string.cd_copy_link),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
            }
        }
        if (linkType != null) {
            MobileLinkActions(linkType = linkType, handle = uri)
        }
    }
}

/**
 * Item 12 (§7.6) enrichment chips for a known-protocol link. Requests
 * READ_CONTACTS inline (not at startup, per §8.3) so the resolver can query
 * ContactsContract.Data for the installed-app MIMETYPEs; a denied permission
 * degrades to nothing (the caller already shows the plain link + copy row).
 */
@Composable
private fun MobileLinkActions(
    linkType: MobileLinkType,
    handle: String,
) {
    val context = LocalContext.current
    var availableActions by remember { mutableStateOf<List<MobileLinkAction>>(emptyList()) }
    var permissionAsked by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        permissionAsked = true
        if (granted) {
            scope.launch {
                val resolver = MobileLinkActionResolver(context.contentResolver)
                availableActions = resolver.resolveAvailableActions(linkType, handle)
            }
        }
    }

    LaunchedEffect(linkType, handle) {
        val granted = ContextCompat.checkSelfPermission(
            context, Manifest.permission.READ_CONTACTS,
        ) == PackageManager.PERMISSION_GRANTED
        if (granted) {
            val resolver = MobileLinkActionResolver(context.contentResolver)
            availableActions = try {
                resolver.resolveAvailableActions(linkType, handle)
            } catch (_: SecurityException) {
                emptyList()
            }
        } else if (!permissionAsked) {
            permissionLauncher.launch(Manifest.permission.READ_CONTACTS)
        }
    }

    if (availableActions.isNotEmpty()) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.padding(top = 4.dp),
        ) {
            availableActions.forEach { action ->
                AssistChip(
                    onClick = {
                        try {
                            context.startActivity(action.intentBuilder(handle))
                        } catch (_: Exception) {
                            // No handler for the deep link — fall back to the browser.
                            context.startActivity(FieldActions.browserIntent(handle))
                        }
                    },
                    label = { Text(stringResource(action.label)) },
                    leadingIcon = {
                        Icon(
                                imageVector = when (action.kind) {
                                    MobileActionKind.MESSAGE -> Icons.Outlined.Message
                                    MobileActionKind.VOICE_CALL -> Icons.Outlined.Call
                                    MobileActionKind.VIDEO_CALL -> Icons.Outlined.Videocam
                                    MobileActionKind.APP_OPEN -> Icons.Outlined.OpenInNew
                                    MobileActionKind.APP_CALL -> Icons.Outlined.Call
                                },
                                contentDescription = stringResource(action.label),
                                modifier = Modifier.size(18.dp),
                            )
                        },
                        modifier = Modifier.height(32.dp),
                    )
                }
            }
        }
    }

// --- M24: inline circle/tag chip editor -------------------------------------

/**
 * A chip list with an "add" overflow menu. `items` are the currently applied entities
 * (rendered as removable chips); the add menu lists `allItems` minus the applied ones.
 * Generic over the entity type so circles and tags share one implementation.
 */
@Composable
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
private fun <T> ChipEditorRow(
    items: List<T>,
    allItems: List<T>,
    label: (T) -> String,
    isApplied: (T) -> Boolean,
    onAdd: (T) -> Unit,
    onRemove: (T) -> Unit,
    emptyText: String,
    addLabel: String,
) {
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        if (items.isEmpty()) {
            Text(
                text = emptyText,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(vertical = 4.dp),
            )
        }
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            items.forEach { item ->
                InputChip(
                    selected = true,
                    onClick = { onRemove(item) },
                    label = { Text(label(item)) },
                    trailingIcon = {
                        Icon(
                            Icons.Outlined.Close,
                            contentDescription = stringResource(R.string.contact_remove),
                            modifier = Modifier.size(16.dp),
                        )
                    },
                )
            }
        }
        val addable = allItems.filter { !isApplied(it) }
        if (addable.isNotEmpty()) {
            var menuExpanded by remember { mutableStateOf(false) }
            Box(modifier = Modifier.padding(top = 4.dp)) {
                AssistChip(
                    onClick = { menuExpanded = true },
                    label = { Text(addLabel) },
                    leadingIcon = {
                        Icon(
                            Icons.Outlined.Add,
                            contentDescription = null,
                            modifier = Modifier.size(18.dp),
                        )
                    },
                    modifier = Modifier.height(32.dp),
                )
                DropdownMenu(
                    expanded = menuExpanded,
                    onDismissRequest = { menuExpanded = false },
                ) {
                    addable.forEach { item ->
                        DropdownMenuItem(
                            text = { Text(label(item)) },
                            onClick = {
                                menuExpanded = false
                                onAdd(item)
                            },
                        )
                    }
                }
            }
        }
    }
}

/**
 * Writes an exported vCard to the cache and hands it to the share sheet via FileProvider, so
 * the user can save the file (Files) or send it to any app. `version` null → vCard 4.0;
 * 3 → vCard 3.0 (filename suffix).
 */
private fun shareExportedVcf(
    context: Context,
    contact: ContactRecordResponse?,
    version: Int?,
    bytes: ByteArray,
) {
    val dir = File(context.cacheDir, "exports").apply { mkdirs() }
    val safeName = contact?.card?.displayName
        ?.replace(Regex("""[^A-Za-z0-9._-]"""), "_")
        ?: "contact"
    val suffix = if (version == 3) "-v3" else ""
    val file = File(dir, "$safeName$suffix.vcf")
    file.writeBytes(bytes)

    val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
    val intent = Intent(Intent.ACTION_SEND).apply {
        type = "text/vcard"
        putExtra(Intent.EXTRA_STREAM, uri)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    try {
        context.startActivity(Intent.createChooser(intent, context.getString(R.string.contact_export_share_title)))
    } catch (_: Exception) {
        // No activity can handle the share — nothing to do.
    }
}

/**
 * Issue #219: the picked image's declared byte size, read only from the
 * provider's cursor (never the file contents), so an oversized pick can be
 * rejected without allocating it. null when the provider omits the column.
 */
private fun queryPhotoSize(resolver: ContentResolver, uri: Uri): Long? {
    var size: Long? = null
    resolver.query(uri, null, null, null, null)?.use { cursor ->
        if (cursor.moveToFirst()) {
            val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
            if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) size = cursor.getLong(sizeIndex)
        }
    }
    return size
}

/** Reads the picked image's bytes off the main thread (post-size-probe). */
private fun readPhotoBytes(resolver: ContentResolver, uri: Uri): ByteArray =
    resolver.openInputStream(uri)?.use { it.readBytes() } ?: ByteArray(0)

/** Writes photo bytes to a cache temp file (the uCrop source), or null on IO failure. */
private fun writePhotoBytes(context: Context, bytes: ByteArray): File? {
    val dir = File(context.cacheDir, "photos").apply { mkdirs() }
    return try {
        File(dir, "photo_${System.currentTimeMillis()}.jpg").apply { writeBytes(bytes) }
    } catch (_: Exception) {
        null
    }
}

/**
 * Issue #219: the uCrop intent for the profile-photo crop step (web's
 * react-easy-crop equivalent). Circular 1:1 crop with a 400px result — the
 * backend's `processAndSavePhoto` caps stored photos at 400x400, so the crop
 * output matches what the server would produce itself.
 */
private fun ucropIntent(context: Context, sourceUri: Uri): Intent {
    val dir = File(context.cacheDir, "crops").apply { mkdirs() }
    val dest = File(dir, "crop_${System.currentTimeMillis()}.jpg")
    val options = UCrop.Options().apply {
        setCircleDimmedLayer(true)
        setCompressionFormat(Bitmap.CompressFormat.JPEG)
        setCompressionQuality(90)
    }
    return UCrop.of(sourceUri, Uri.fromFile(dest))
        .withAspectRatio(1f, 1f)
        .withMaxResultSize(400, 400)
        .withOptions(options)
        .getIntent(context)
}

/**
 * Issue #220: the Immich row of the External Links panel. The linked person's
 * thumbnail (proxied through the backend, rendered via Coil's auth stack),
 * person name, photo count, open-in-Immich, and an unlink action. Unlinking
 * keeps the person's ExternalActivity history (web parity) — it is not the
 * generic external-identity delete.
 */
@Composable
private fun ImmichLinkRow(
    identity: ExternalIdentity,
    summary: ImmichPersonSummary?,
    contactUid: String?,
    onUnlink: () -> Unit,
) {
    val context = LocalContext.current
    val serverOrigin = LocalServerUrl.current
    val personName = summary?.personName
        ?: (identity.metadata?.get("person_name") as? String)
        ?: stringResource(R.string.immich_linked_person)
    val safeUrl = identity.url?.takeIf { it.startsWith("http://") || it.startsWith("https://") }
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (contactUid != null) {
            AsyncImage(
                model = resolvePhotoUri("/api/v1/immich/contacts/$contactUid/thumbnail", serverOrigin),
                contentDescription = stringResource(R.string.immich_link_thumbnail_desc),
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(8.dp)),
            )
        }
        Column(modifier = Modifier.weight(1f)) {
            Text(personName, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                stringResource(R.string.immich_photo_count, summary?.photoCount ?: 0),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (safeUrl != null) {
            IconButton(onClick = { context.startActivity(FieldActions.browserIntent(safeUrl)) }) {
                Icon(
                    Icons.Outlined.OpenInNew,
                    contentDescription = stringResource(R.string.cd_open_link),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
        IconButton(onClick = onUnlink) {
            Icon(
                Icons.Outlined.Delete,
                // #205: the row action names the linked person so TalkBack
                // doesn't read a bare "Remove link" on every row.
                contentDescription = stringResource(R.string.external_links_remove_named, personName),
                tint = MaterialTheme.colorScheme.error,
            )
        }
    }
}

/**
 * Issue #220: a generic ExternalIdentity row (any system except Immich):
 * system + external_id, open-link when the URL is http(s) (an unsafe URL is
 * shown as text, never as a launchable link — web's T41 render-time guard),
 * and a delete action.
 */
@Composable
private fun GenericExternalLinkRow(identity: ExternalIdentity, onDelete: () -> Unit) {
    val context = LocalContext.current
    val safeUrl = identity.url?.takeIf { it.startsWith("http://") || it.startsWith("https://") }
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(identity.system.orEmpty(), style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                identity.externalId,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (safeUrl != null) {
            IconButton(onClick = { context.startActivity(FieldActions.browserIntent(safeUrl)) }) {
                Icon(
                    Icons.Outlined.OpenInNew,
                    contentDescription = stringResource(R.string.cd_open_link),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
        IconButton(onClick = onDelete) {
            Icon(
                Icons.Outlined.Delete,
                // #205: the row action names the linked system so TalkBack
                // doesn't read a bare "Remove link" on every row.
                contentDescription = stringResource(R.string.external_links_remove_named, identity.system.orEmpty()),
                tint = MaterialTheme.colorScheme.error,
            )
        }
    }
}

/**
 * Issue #236: one "Add <system> link" row in the External Links panel, shown
 * only when that system is configured (mirrors web's FileLinksPanel gate).
 */
@Composable
private fun AddExternalLinkRow(label: String, onClick: () -> Unit) {
    TextButton(
        onClick = onClick,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 8.dp),
    ) {
        Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.padding(end = 8.dp))
        Text(label)
    }
}
