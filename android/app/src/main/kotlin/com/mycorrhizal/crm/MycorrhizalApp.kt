@file:OptIn(
    androidx.compose.material3.ExperimentalMaterial3Api::class,
    androidx.compose.material3.windowsizeclass.ExperimentalMaterial3WindowSizeClassApi::class,
)

package com.mycorrhizal.crm

import androidx.annotation.StringRes
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Contacts
import androidx.compose.material.icons.outlined.Dataset
import androidx.compose.material.icons.outlined.EditNote
import androidx.compose.material.icons.outlined.EventNote
import androidx.compose.material.icons.outlined.FileUpload
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.HomeWork
import androidx.compose.material.icons.outlined.IosShare
import androidx.compose.material.icons.outlined.Label
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.DrawerState
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.VerticalDivider
import androidx.compose.material3.rememberDrawerState
import androidx.compose.material3.windowsizeclass.WindowSizeClass
import androidx.compose.material3.windowsizeclass.WindowWidthSizeClass
import androidx.compose.material3.windowsizeclass.calculateWindowSizeClass
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.activity.compose.BackHandler
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.navArgument
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.mycorrhizal.crm.feature.auth.LoginScreen
import com.mycorrhizal.crm.feature.auth.RegisterScreen
import com.mycorrhizal.crm.feature.auth.ForgotPasswordScreen
import com.mycorrhizal.crm.feature.audit.AuditScreen
import com.mycorrhizal.crm.feature.sysevents.SystemEventsScreen
import com.mycorrhizal.crm.feature.cadence.CadenceScreen
import com.mycorrhizal.crm.feature.circles.CircleDetailScreen
import com.mycorrhizal.crm.feature.circles.CirclesScreen
import com.mycorrhizal.crm.feature.contacts.ContactDetailScreen
import com.mycorrhizal.crm.feature.contacts.ContactFormScreen
import com.mycorrhizal.crm.feature.contacts.ContactListScreen
import com.mycorrhizal.crm.feature.contacts.DashboardScreen
import com.mycorrhizal.crm.feature.contacts.PrepViewScreen
import com.mycorrhizal.crm.feature.contacts.MergeContactsScreen
import com.mycorrhizal.crm.feature.circles.TriageScreen
import com.mycorrhizal.crm.feature.households.HouseholdDetailScreen
import com.mycorrhizal.crm.feature.households.HouseholdsScreen
import com.mycorrhizal.crm.feature.imports.ImportContactsScreen
import com.mycorrhizal.crm.feature.imports.VcfImportScreen
import com.mycorrhizal.crm.feature.network.NetworkScreen
import com.mycorrhizal.crm.feature.relationships.RelationshipsScreen
import com.mycorrhizal.crm.feature.settings.CustomLinkActionsScreen
import com.mycorrhizal.crm.feature.settings.DataScreen
import com.mycorrhizal.crm.feature.settings.ImmichSettingsScreen
import com.mycorrhizal.crm.feature.settings.NotificationChannelsScreen
import com.mycorrhizal.crm.feature.settings.SettingsScreen
import com.mycorrhizal.crm.feature.settings.TwoFactorScreen
import com.mycorrhizal.crm.feature.settings.ApiTokensScreen
import com.mycorrhizal.crm.feature.settings.WebhooksScreen
import com.mycorrhizal.crm.feature.shares.ContactSharesScreen
import com.mycorrhizal.crm.feature.shares.ShareContactScreen
import com.mycorrhizal.crm.feature.timelineentities.ConversationAgendaScreen
import com.mycorrhizal.crm.feature.timelineentities.GiftsScreen
import com.mycorrhizal.crm.feature.timelineentities.LifeEventsScreen
import com.mycorrhizal.crm.feature.timelineentities.PreferencesScreen
import com.mycorrhizal.crm.feature.tags.TagDetailScreen
import com.mycorrhizal.crm.feature.tags.TagsScreen
import com.mycorrhizal.crm.feature.users.UsersScreen
import kotlinx.coroutines.launch
import com.mycorrhizal.crm.feature.timeline.ActivitiesInboxScreen
import com.mycorrhizal.crm.feature.timeline.ActivitiesScreen
import com.mycorrhizal.crm.feature.timeline.ActivityFormScreen
import com.mycorrhizal.crm.feature.timeline.NoteFormScreen
import com.mycorrhizal.crm.feature.timeline.NotesInboxScreen
import com.mycorrhizal.crm.feature.timeline.NotesScreen
import com.mycorrhizal.crm.feature.timeline.ReminderFormScreen
import com.mycorrhizal.crm.feature.timeline.RemindersScreen
import com.mycorrhizal.crm.feature.tracking.DeviceRegistrationViewModel
import com.mycorrhizal.crm.ui.R
import com.mycorrhizal.crm.ui.LocalDarkTheme
import com.mycorrhizal.crm.ui.LocalDrawerOpen
import com.mycorrhizal.crm.ui.LocalServerUrl
import com.mycorrhizal.crm.ui.components.EmptyState
import kotlinx.coroutines.flow.filterNotNull

private data class DrawerDestination(
    val route: String,
    @StringRes val labelRes: Int,
    val icon: ImageVector,
)

/** Primary navigation, matching the web's order. Reachable from the drawer/rail. */
private val primaryDestinations = listOf(
    DrawerDestination("home", R.string.nav_dashboard, Icons.Outlined.Home),
    DrawerDestination("contacts", R.string.nav_contacts, Icons.Outlined.Contacts),
    DrawerDestination("activities", R.string.nav_activities, Icons.Outlined.EventNote),
    DrawerDestination("notes", R.string.nav_notes, Icons.Outlined.EditNote),
)

/** Secondary destinations, below the primary set in the drawer/rail. */
private val secondaryDestinations = listOf(
    DrawerDestination("network", R.string.nav_network, Icons.Outlined.Share),
    DrawerDestination("shares", R.string.nav_shares, Icons.Outlined.IosShare),
    DrawerDestination("circles", R.string.nav_circles, Icons.Outlined.Group),
    DrawerDestination("tags", R.string.nav_tags, Icons.Outlined.Label),
    DrawerDestination("households", R.string.nav_households, Icons.Outlined.HomeWork),
    DrawerDestination("audit", R.string.nav_audit, Icons.Outlined.History),
    // Web exposes the address/relationship-suggestion review as top-level nav;
    // on Android it was reachable only from deep inside Settings. Promoted here
    // (the Settings shortcut stays as a secondary entry point).
    DrawerDestination("data", R.string.settings_data_review, Icons.Outlined.Dataset),
    DrawerDestination("import", R.string.import_title, Icons.Outlined.FileUpload),
    DrawerDestination("settings", R.string.nav_settings, Icons.Outlined.Settings),
)

/** True when the current destination is inside a [DrawerDestination]'s route. */
private fun isSelected(currentRoute: String?, item: DrawerDestination): Boolean {
    val route = currentRoute ?: return false
    return route == item.route || route.startsWith("${item.route}/")
}

/**
 * Navigation to a top-level destination or deep link: always collapse the
 * back stack to the start destination with save/restore, single-top. This is
 * the "natural stack position" contract for both the drawer/rail and for
 * notification deep links — back from a pushed route returns to the dashboard,
 * never to a stale intermediate screen or a blank stack (issue #679).
 * Extracted so the back-stack behavior is unit-testable against a real
 * [NavHostController].
 */
internal fun NavHostController.navigateToRoot(route: String) {
    navigate(route) {
        popUpTo(graph.findStartDestination().id) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
}

/**
 * Issue #150: whether the route lives inside the contacts workflow — the one
 * section that becomes a two-pane list/detail on expanded widths. `merge` is
 * included because it is part of the contact workflow (reached from the detail
 * screen). Pure so the host-level width-class tests assert the boundary.
 */
internal fun isInContactsSection(route: String?): Boolean =
    route == "contacts" ||
        route == "merge" ||
        route?.startsWith("contacts/") == true ||
        route?.startsWith("merge/") == true

private fun androidx.compose.ui.graphics.Color.toArgbCompat(): Int =
    android.graphics.Color.argb(
        (alpha * 255).toInt(),
        (red * 255).toInt(),
        (green * 255).toInt(),
        (blue * 255).toInt(),
    )

@Composable
fun MycorrhizalApp(
    darkTheme: Boolean,
    mainViewModel: MainViewModel = hiltViewModel(),
    deepLinks: kotlinx.coroutines.flow.Flow<android.net.Uri?> = kotlinx.coroutines.flow.flowOf(null),
    onDeepLinkHandled: () -> Unit = {},
    // #203: the OIDC-return failure Toast is replaced by LoginScreen's own
    // SnackbarHostState — both failure paths in MainActivity.handleOidcReturn
    // leave the session logged out, so this is only ever consumed by the
    // unauthenticated branch below, never MainScaffold.
    oidcError: kotlinx.coroutines.flow.Flow<String?> = kotlinx.coroutines.flow.flowOf(null),
    onOidcErrorShown: () -> Unit = {},
) {
    val session by mainViewModel.session.collectAsStateWithLifecycle()

    // Issue #202: the status-bar style is owned here, above the isLoggedIn
    // branch, so the auth screens get it too. They have no green app bar — the
    // bone surface runs straight up behind the status bar — but MainActivity's
    // `enableEdgeToEdge` (which only runs in onCreate) leaves the launch-time
    // green-app-bar style in place for them: white icons on cream, 1.09:1.
    // One effect for both trees keeps the primary-vs-everything-else inversion
    // rule in exactly one place (see the detailed note in MainScaffold's doc
    // above -- the rule still applies as written there). The drawer state is
    // hoisted here because this effect needs it; MainScaffold takes it as a
    // parameter.
    //
    // The contact detail screen owns the bar while it is composed (per its
    // collapse state); this effect's keys don't change on navigation, so it
    // never re-runs over it -- the same guarantee MainScaffold's effect gave.
    val drawerState = rememberDrawerState(initialValue = DrawerValue.Closed)
    val drawerOpen = drawerState.isOpen
    val activity = LocalContext.current as android.app.Activity
    val primaryArgb = MaterialTheme.colorScheme.primary.toArgbCompat()
    val surfaceArgb = MaterialTheme.colorScheme.surface.toArgbCompat()
    val surfaceContainerLowArgb = MaterialTheme.colorScheme.surfaceContainerLow.toArgbCompat()
    LaunchedEffect(session.isLoggedIn, darkTheme, drawerOpen, primaryArgb, surfaceArgb, surfaceContainerLowArgb) {
        val onPrimaryBar = session.isLoggedIn && !drawerOpen
        activity.window.statusBarColor = when {
            onPrimaryBar -> primaryArgb
            session.isLoggedIn -> surfaceContainerLowArgb
            else -> surfaceArgb
        }
        WindowCompat.getInsetsController(activity.window, activity.window.decorView)
            .isAppearanceLightStatusBars = if (onPrimaryBar) darkTheme else !darkTheme
    }

    if (!session.isLoggedIn) {
        // M26: the unauthenticated tree is a tiny router over the auth
        // screens — login, register, forgot-password — since they are not
        // part of the main NavHost. The system back button returns to the
        // login screen from the register/forgot screens instead of exiting
        // the app (review-pass fix).
        var authScreen by rememberSaveable { mutableStateOf(AuthScreen.LOGIN) }
        val context = LocalContext.current
        val oidcErrorState by oidcError.collectAsStateWithLifecycle(initialValue = null)
        BackHandler(enabled = authScreen != AuthScreen.LOGIN) {
            authScreen = AuthScreen.LOGIN
        }
        when (authScreen) {
            AuthScreen.LOGIN -> LoginScreen(
                onLoggedIn = { /* session flow flips isLoggedIn, recomposition swaps the tree */ },
                onSignInWithSso = { serverUrl ->
                    // M6 §4: `client=android` makes the backend redirect back to
                    // the mycorrhizal://oidc/callback deep link (MainActivity)
                    // instead of the web cookie path — without it this whole
                    // native flow is unreachable (review-pass fix).
                    val url = serverUrl.trim().trimEnd('/') + "/api/v1/auth/oidc/login?client=android"
                    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
                    runCatching { context.startActivity(intent) }
                },
                onRegisterClick = { authScreen = AuthScreen.REGISTER },
                onForgotPasswordClick = { authScreen = AuthScreen.FORGOT_PASSWORD },
                oidcError = oidcErrorState,
                onOidcErrorShown = onOidcErrorShown,
            )
            AuthScreen.REGISTER -> RegisterScreen(
                onRegistered = { /* auto-login flips isLoggedIn */ },
                onBack = { authScreen = AuthScreen.LOGIN },
            )
            AuthScreen.FORGOT_PASSWORD -> ForgotPasswordScreen(
                onBack = { authScreen = AuthScreen.LOGIN },
            )
        }
        return
    }

    MainScaffold(
        darkTheme = darkTheme,
        drawerState = drawerState,
        serverUrl = session.serverUrl.orEmpty(),
        deepLinks = deepLinks,
        onDeepLinkHandled = onDeepLinkHandled,
    )
}

/** The M26 unauthenticated-tree router destinations. */
private enum class AuthScreen { LOGIN, REGISTER, FORGOT_PASSWORD }

@Composable
private fun MainScaffold(
    darkTheme: Boolean,
    drawerState: DrawerState,
    serverUrl: String,
    deepLinks: kotlinx.coroutines.flow.Flow<android.net.Uri?> = kotlinx.coroutines.flow.flowOf(null),
    onDeepLinkHandled: () -> Unit = {},
) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentDestination = backStackEntry?.destination
    val currentRoute = currentDestination?.route
    val scope = rememberCoroutineScope()
    val activity = LocalContext.current as android.app.Activity
    // M25: the language setting needs the whole activity recreated so
    // attachBaseContext re-wraps resources in the new locale.
    val recreateActivity = { activity.recreate() }

    // Issue #150: the phone experience (modal drawer + single-pane screens) and
    // the tablet experience (NavigationRail + two-pane contacts list/detail)
    // are the same route graph in two frames, split on the window width class.
    // The drawer is never opened at Expanded, so drawerState.isOpen stays false
    // there and every screen below reads the tablet-side default.
    val windowSizeClass = calculateWindowSizeClass(activity)
    val isTwoPane = windowSizeClass.widthSizeClass == WindowWidthSizeClass.Expanded

    // M5 §6.6 (issue #152): consume notification deep links as they arrive and
    // drive the NavHost. The session-flow guard means a tap that lands while the
    // auth tree is up is deferred until a session exists (the link is retained
    // by the flow until it is consumed).
    LaunchedEffect(deepLinks) {
        deepLinks.filterNotNull().collect { uri ->
            deepLinkRoute(uri)?.let { route ->
                navController.navigateToRoot(route)
            }
            onDeepLinkHandled()
        }
    }

    // M5 §5a (issue #152): registers/deletes this install's FCM device with the
    // server as the session flips between logged-in and logged-out. Deliberately
    // not stored: it only reacts to session transitions.
    hiltViewModel<DeviceRegistrationViewModel>()

    // The status bar is owned by MycorrhizalApp (issue #202), not this
    // scaffold, so the unauthenticated tree gets it too. Its documented
    // behaviour is unchanged for this tree:
    //
    // Default status bar for the always-green app-bar screens: brand green
    // (primary) background, icons following the theme. The contact detail
    // overrides this per its collapse state. When the drawer is open its
    // surfaceContainerLow surface shows under the status bar, so the status
    // bar follows that surface instead (other apps do the same inversion).
    //
    // isAppearanceLightStatusBars has two different rules depending on which
    // color role is behind the bar: `primary` is the one M3 role in this
    // palette that inverts between themes (mycelium is dark-toned in light
    // mode, myceliumDark is light-toned in dark mode -- deliberately, per
    // Theme.kt), so primary-role bars need `darkTheme` (inverted). Every
    // other role here (surface/surfaceContainerLow) follows the intuitive
    // direction, so it needs `!darkTheme`.

    // Issue #150: a destination tap navigates the same way from the rail and the
    // drawer. Closing the drawer is a no-op at Expanded (it never opens), so one
    // callback serves both frames.
    val onDestinationClick = { route: String ->
        scope.launch { drawerState.close() }
        navController.navigateToRoot(route)
    }
    val onMenuClick: () -> Unit = { scope.launch { drawerState.open() } }

    // M5 §3.1: the server origin reaches every avatar so relative photo paths
    // resolve to per-server absolute URLs (which are also Coil's disk-cache
    // keys — see LocalServerUrl).
    CompositionLocalProvider(
        LocalDrawerOpen provides drawerState.isOpen,
        LocalDarkTheme provides darkTheme,
        LocalServerUrl provides serverUrl,
    ) {
        MainNavScaffold(
            currentRoute = currentRoute,
            windowSizeClass = windowSizeClass,
            drawerState = drawerState,
            onDestinationClick = onDestinationClick,
        ) {
            AdaptiveMainContent(
                isTwoPane = isTwoPane,
                currentRoute = currentRoute,
                // The NavHost is the content in single-pane mode and the
                // *detail* pane in two-pane mode — the same graph either way,
                // only the frame around it changes.
                content = {
                    AppNavGraph(
                        navController = navController,
                        isTwoPane = isTwoPane,
                        onMenuClick = onMenuClick,
                        onLocaleChanged = recreateActivity,
                    )
                },
                // Issue #150: at Expanded, the contacts list is a persistent
                // left pane outside the NavHost (so it survives selecting
                // contacts and drilling into their sub-screens). The hamburger
                // is omitted (onMenuClick = null): at Expanded the drawer is
                // replaced by the rail.
                listPane = {
                    ContactListScreen(
                        onContactClick = { id -> navController.navigate("contacts/$id") },
                        onCreateContact = { navController.navigate("contacts/new") },
                        onImportContacts = { navController.navigate("import") },
                        onMenuClick = null,
                    )
                },
            )
        }
    }
}

/**
 * Issue #150: the app frame. Below [WindowWidthSizeClass.Expanded] this is the
 * existing modal drawer wrapping [content]; at Expanded the drawer is replaced
 * by a [NavigationRail]. Stateless so host-level width-class tests can drive it
 * with a fake [WindowSizeClass] and slot content.
 */
@Composable
internal fun MainNavScaffold(
    currentRoute: String?,
    windowSizeClass: WindowSizeClass,
    drawerState: DrawerState,
    onDestinationClick: (String) -> Unit,
    content: @Composable () -> Unit,
) {
    if (windowSizeClass.widthSizeClass == WindowWidthSizeClass.Expanded) {
        Row(Modifier.fillMaxSize()) {
            MycorrhizalNavigationRail(
                currentRoute = currentRoute,
                onDestinationClick = onDestinationClick,
            )
            VerticalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Box(Modifier.weight(1f).fillMaxHeight()) { content() }
        }
    } else {
        ModalNavigationDrawer(
            drawerState = drawerState,
            drawerContent = {
                DrawerContent(
                    currentRoute = currentRoute,
                    onDestinationClick = onDestinationClick,
                )
            },
        ) {
            content()
        }
    }
}

/**
 * Issue #150: the tablet/desktop-width app navigation — the same destination set
 * as the drawer, as a [NavigationRail]. The primary destinations stay pinned at
 * the top; the secondary set scrolls (twelve labeled items rarely fit a tablet
 * viewport), which keeps every drawer destination reachable at Expanded. Labels
 * are always shown: the rail only appears where there is room for them.
 *
 * The width is pinned to M3's [NavigationRail] container width: the rail's inner
 * [Column] only declares `widthIn(min=80.dp)` and the `HorizontalDivider` below
 * applies `fillMaxWidth()`, so without an explicit width the divider would pull
 * the rail out to the whole Row and squeeze the content to nothing (issue #150).
 */
private val NavigationRailWidth = 80.dp

@Composable
private fun MycorrhizalNavigationRail(
    currentRoute: String?,
    onDestinationClick: (String) -> Unit,
) {
    NavigationRail(
        modifier = Modifier.width(NavigationRailWidth).testTag("navigation-rail"),
        containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
    ) {
        primaryDestinations.forEach { item ->
            RailDestinationItem(item = item, currentRoute = currentRoute, onDestinationClick = onDestinationClick)
        }
        Spacer(Modifier.height(8.dp))
        HorizontalDivider(
            modifier = Modifier.padding(horizontal = 12.dp),
            color = MaterialTheme.colorScheme.outlineVariant,
        )
        Spacer(Modifier.height(8.dp))
        Column(
            modifier = Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState()),
        ) {
            secondaryDestinations.forEach { item ->
                RailDestinationItem(item = item, currentRoute = currentRoute, onDestinationClick = onDestinationClick)
            }
        }
    }
}

@Composable
private fun RailDestinationItem(
    item: DrawerDestination,
    currentRoute: String?,
    onDestinationClick: (String) -> Unit,
) {
    NavigationRailItem(
        selected = isSelected(currentRoute, item),
        onClick = { onDestinationClick(item.route) },
        icon = { Icon(item.icon, contentDescription = null) },
        label = {
            Text(
                stringResource(item.labelRes),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        },
        alwaysShowLabel = true,
        modifier = Modifier.testTag("rail-${item.route}"),
    )
}

/** The phone/compact-width navigation drawer — unchanged destination content. */
@Composable
private fun DrawerContent(
    currentRoute: String?,
    onDestinationClick: (String) -> Unit,
) {
    ModalDrawerSheet {
        // #208: the drawer's sheet title carried no heading semantics.
        Text(
            text = stringResource(R.string.app_name),
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier
                .padding(16.dp)
                .semantics { heading() },
        )
        HorizontalDivider()
        primaryDestinations.forEach { item ->
            NavigationDrawerItem(
                // T100: labelLarge is 14sp -- Material's chip/button
                // size, too small for the app's only global nav. Bumped
                // here rather than in Theme.kt because labelLarge is
                // also the M3 default for Button and Snackbar, so a
                // global change would resize every button in the app.
                // (T99 removed the serif family this override also
                // used to carry.)
                label = {
                    Text(
                        stringResource(item.labelRes),
                        style = MaterialTheme.typography.labelLarge.copy(fontSize = 16.sp),
                    )
                },
                selected = isSelected(currentRoute, item),
                onClick = { onDestinationClick(item.route) },
                icon = { Icon(item.icon, contentDescription = null) },
                modifier = Modifier.padding(horizontal = 8.dp),
            )
        }
        secondaryDestinations.forEach { item ->
            NavigationDrawerItem(
                // T100/T99: see the primaryDestinations loop's
                // matching comment above.
                label = {
                    Text(
                        stringResource(item.labelRes),
                        style = MaterialTheme.typography.labelLarge.copy(fontSize = 16.sp),
                    )
                },
                selected = isSelected(currentRoute, item),
                onClick = { onDestinationClick(item.route) },
                icon = { Icon(item.icon, contentDescription = null) },
                modifier = Modifier.padding(horizontal = 8.dp),
            )
        }
    }
}

/**
 * Issue #150: the two-pane contacts split for expanded widths — a persistent
 * list pane on the left, the selected destination on the right. Stateless so
 * host-level tests can assert both panes compose side by side.
 */
@Composable
internal fun ContactTwoPane(
    listPane: @Composable () -> Unit,
    detailPane: @Composable () -> Unit,
) {
    Row(Modifier.fillMaxSize()) {
        Box(
            modifier = Modifier
                .weight(0.38f)
                .fillMaxHeight()
                .testTag("two-pane-list"),
        ) {
            listPane()
        }
        VerticalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        Box(
            modifier = Modifier
                .weight(0.62f)
                .fillMaxHeight()
                .testTag("two-pane-detail"),
        ) {
            detailPane()
        }
    }
}

/**
 * Issue #150: picks between the single-pane [content] and the two-pane contacts
 * split. The split applies only at Expanded *and* on a contacts-section route;
 * everywhere else (including all non-contacts routes at Expanded) [content]
 * stays full-width. Stateless so host-level width-class tests drive it directly.
 */
@Composable
internal fun AdaptiveMainContent(
    isTwoPane: Boolean,
    currentRoute: String?,
    content: @Composable () -> Unit,
    listPane: @Composable () -> Unit = {},
) {
    if (isTwoPane && isInContactsSection(currentRoute)) {
        ContactTwoPane(listPane = listPane, detailPane = content)
    } else {
        content()
    }
}

/**
 * Issue #150: the right-hand pane's empty state on the bare `contacts` route at
 * Expanded — the list itself lives in the persistent left pane, so this is the
 * "nothing selected yet" hint.
 */
@Composable
private fun ContactListPlaceholder() {
    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = stringResource(R.string.nav_contacts),
                        style = MaterialTheme.typography.titleLarge,
                    )
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
    ) { padding ->
        EmptyState(
            message = stringResource(R.string.contacts_select_contact),
            modifier = Modifier.fillMaxSize().padding(padding),
        )
    }
}

/**
 * The single route graph behind both phone and tablet. [isTwoPane] only changes
 * the bare `contacts` destination: at Expanded the list is the persistent left
 * pane, so that destination shows the placeholder instead. [onMenuClick] is the
 * app-level drawer opener on phones; at Expanded there is no drawer, so the
 * hamburger is omitted from every drawer-based screen (menu is null).
 */
@Composable
private fun AppNavGraph(
    navController: NavHostController,
    isTwoPane: Boolean,
    onMenuClick: () -> Unit,
    onLocaleChanged: () -> Unit,
) {
    // Issue #150: no drawer at Expanded — drawer-based screens hide their
    // hamburger when this is null.
    val menu: (() -> Unit)? = if (isTwoPane) null else onMenuClick

    NavHost(
        navController = navController,
        startDestination = "home",
    ) {
        composable("contacts") {
            if (isTwoPane) {
                ContactListPlaceholder()
            } else {
                ContactListScreen(
                    onContactClick = { id -> navController.navigate("contacts/$id") },
                    onCreateContact = { navController.navigate("contacts/new") },
                    onMenuClick = menu,
                    onImportContacts = { navController.navigate("import") },
                )
            }
        }
        composable("import") {
            ImportContactsScreen(
                onMenuClick = menu,
                onImported = {},
                onImportVcf = { navController.navigate("import/vcf") },
            )
        }
        // M9 item 4: VCF-file import — a sibling path to this screen's device-contacts one.
        composable("import/vcf") {
            VcfImportScreen(
                onBack = { navController.popBackStack() },
                onDone = { navController.popBackStack() },
            )
        }
        composable(
            route = "merge/{keepId}",
            arguments = listOf(navArgument("keepId") { type = NavType.LongType }),
        ) { entry ->
            val keepId = entry.arguments?.getLong("keepId") ?: 0L
            MergeContactsScreen(
                onBack = { navController.popBackStack() },
                keepId = keepId,
            )
        }
        composable(
            route = "contacts/{contactId}",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) { entry ->
            val contactId = entry.arguments?.getInt("contactId") ?: 0
            ContactDetailScreen(
                onBack = { navController.popBackStack() },
                onEdit = { id -> navController.navigate("contacts/$id/edit") },
                onDeleted = { navController.popBackStack() },
                onStayInTouch = { contact ->
                    val name = contact.card?.displayName.orEmpty()
                    val message = navController.context.getString(
                        R.string.contact_stay_in_touch_message,
                        name,
                    )
                    navController.navigate(
                        "contacts/$contactId/reminders/new" +
                            "?message=${Uri.encode(message)}&recurrence=quarterly",
                    )
                },
                onViewActivities = { id -> navController.navigate("contacts/$id/activities") },
                onViewNotes = { id -> navController.navigate("contacts/$id/notes") },
                onViewReminders = { id -> navController.navigate("contacts/$id/reminders") },
                onViewRelationships = { id -> navController.navigate("contacts/$id/relationships") },
                onViewCadence = { id -> navController.navigate("contacts/$id/cadence") },
                onOpenInContacts = { lookupKey ->
                    openInContacts(navController.context, lookupKey)
                },
                onMerge = { id -> navController.navigate("merge/$id") },
                onViewLifeEvents = { id -> navController.navigate("contacts/$id/life-events") },
                onViewGifts = { id -> navController.navigate("contacts/$id/gifts") },
                onViewPreferences = { id -> navController.navigate("contacts/$id/preferences") },
                onViewAgenda = { id -> navController.navigate("contacts/$id/agenda") },
                onViewPrep = { id -> navController.navigate("contacts/$id/prep") },
                onExploreConnections = { id -> navController.navigate("contacts/$id/network") },
                onShareContact = { uid ->
                    if (uid.isNotBlank()) {
                        navController.navigate("contacts/$contactId/share?uid=${Uri.encode(uid)}")
                    }
                },
                onEditActivity = { id -> navController.navigate("contacts/$contactId/activities/$id/edit") },
                onEditNote = { id -> navController.navigate("contacts/$contactId/notes/$id/edit") },
                onEditReminder = { id -> navController.navigate("contacts/$contactId/reminders/$id/edit") },
            )
        }
        composable(
            route = "contacts/new",
        ) {
            ContactFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/edit",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            ContactFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/activities",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) { entry ->
            val contactId = entry.arguments?.getInt("contactId") ?: 0
            ActivitiesScreen(
                onBack = { navController.popBackStack() },
                onCreateActivity = {
                    navController.navigate("contacts/$contactId/activities/new")
                },
                onEditActivity = { activityId ->
                    navController.navigate("contacts/$contactId/activities/$activityId/edit")
                },
                onContactClick = { id -> navController.navigate("contacts/$id") },
            )
        }
        composable(
            route = "contacts/{contactId}/activities/new",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            ActivityFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/activities/{activityId}/edit",
            arguments = listOf(
                navArgument("contactId") { type = NavType.IntType },
                navArgument("activityId") { type = NavType.IntType },
            ),
        ) {
            ActivityFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/notes",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) { entry ->
            val contactId = entry.arguments?.getInt("contactId") ?: 0
            NotesScreen(
                onBack = { navController.popBackStack() },
                onCreateNote = { navController.navigate("contacts/$contactId/notes/new") },
                onEditNote = { noteId -> navController.navigate("contacts/$contactId/notes/$noteId/edit") },
            )
        }
        composable(
            route = "contacts/{contactId}/notes/new",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            NoteFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/notes/{noteId}/edit",
            arguments = listOf(
                navArgument("contactId") { type = NavType.IntType },
                navArgument("noteId") { type = NavType.IntType },
            ),
        ) {
            NoteFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/reminders",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) { entry ->
            val contactId = entry.arguments?.getInt("contactId") ?: 0
            RemindersScreen(
                onBack = { navController.popBackStack() },
                onCreateReminder = { navController.navigate("contacts/$contactId/reminders/new") },
                onEditReminder = { reminderId ->
                    navController.navigate("contacts/$contactId/reminders/$reminderId/edit")
                },
            )
        }
        composable(
            route = "contacts/{contactId}/reminders/new?message={message}&recurrence={recurrence}",
            arguments = listOf(
                navArgument("contactId") { type = NavType.IntType },
                navArgument("message") { type = NavType.StringType; defaultValue = "" },
                navArgument("recurrence") { type = NavType.StringType; defaultValue = "" },
            ),
        ) {
            ReminderFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/reminders/{reminderId}/edit",
            arguments = listOf(
                navArgument("contactId") { type = NavType.IntType },
                navArgument("reminderId") { type = NavType.IntType },
            ),
        ) {
            ReminderFormScreen(
                onSaved = { navController.popBackStack() },
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/relationships",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            RelationshipsScreen(
                onBack = { navController.popBackStack() },
                onNavigateToContact = { id -> navController.navigate("contacts/$id") },
            )
        }
        composable(
            route = "contacts/{contactId}/cadence",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            CadenceScreen(
                onBack = { navController.popBackStack() },
            )
        }
        composable(
            route = "contacts/{contactId}/life-events",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            LifeEventsScreen(onBack = { navController.popBackStack() })
        }
        composable(
            route = "contacts/{contactId}/gifts",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            GiftsScreen(onBack = { navController.popBackStack() })
        }
        composable(
            route = "contacts/{contactId}/preferences",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            PreferencesScreen(onBack = { navController.popBackStack() })
        }
        composable(
            route = "contacts/{contactId}/agenda",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            ConversationAgendaScreen(onBack = { navController.popBackStack() })
        }
        // M11: the N2 prep-view briefing, reached from the contact detail's
        // ⋮ action menu (web reaches it from ContactHeader.tsx).
        composable(
            route = "contacts/{contactId}/prep",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) { entry ->
            val contactId = entry.arguments?.getInt("contactId") ?: 0
            PrepViewScreen(
                onBack = { navController.popBackStack() },
                onOpenContact = { id -> navController.navigate("contacts/$id") },
            )
        }
        // M15: the standalone contact-shares inbox/outbox (drawer-reachable,
        // mirroring web's ContactSharesPage.tsx).
        composable("shares") {
            ContactSharesScreen(
                onMenuClick = menu,
            )
        }
        // M15: the "Share this contact" flow, reached from ContactDetailScreen's
        // ⋮ action menu (mirroring web's ShareContactDialog.tsx from ContactHeader).
        // The contact's VCard UID is passed through navigation so the screen does
        // not re-fetch the contact — matching web's vcardUID-as-prop.
        composable(
            route = "contacts/{contactId}/share?uid={uid}",
            arguments = listOf(
                navArgument("contactId") { type = NavType.IntType },
                navArgument("uid") { type = NavType.StringType; defaultValue = "" },
            ),
        ) {
            ShareContactScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // M9: contact-agnostic drawer entries — the N4 unfiled-notes inbox and the
        // all-contacts activities feed (matching web's NotesPage.tsx/ActivitiesPage.tsx),
        // replacing the PlaceholderScreen stub. onNoteClick/onActivityClick reuse the
        // existing per-contact edit routes with a contactId of 0 (never a real id) — see
        // NotesInboxScreen/ActivitiesInboxScreen's doc comments for why that's safe.
        composable("notes") {
            NotesInboxScreen(
                onMenuClick = menu,
                onNoteClick = { id -> navController.navigate("contacts/0/notes/$id/edit") },
            )
        }
        composable("activities") {
            ActivitiesInboxScreen(
                onMenuClick = menu,
                onActivityClick = { id -> navController.navigate("contacts/0/activities/$id/edit") },
                onContactClick = { id -> navController.navigate("contacts/$id") },
            )
        }
        composable("home") {
            DashboardScreen(
                onOpenContact = { id -> navController.navigate("contacts/$id") },
                onMenuClick = menu,
            )
        }

        composable("settings") {
            SettingsScreen(
                onMenuClick = menu,
                onLoggedOut = { navController.popBackStack() },
                onCustomLinks = { navController.navigate("custom-links") },
                onWebhooks = { navController.navigate("webhooks") },
                // Issue #413's Android follow-up (#573): API token management.
                onApiTokens = { navController.navigate("api-tokens") },
                // Issue #814 Phase 2: TOTP two-factor enrollment/management.
                onTwoFactor = { navController.navigate("two-factor") },
                onNotificationChannels = { navController.navigate("notification-channels") },
                onImmichSettings = { navController.navigate("immich-settings") },
                // M26: the one-time legacy circle/tag cleanup tool.
                onCircleTagTriage = { navController.navigate("circle-tag-triage") },
                // T104 + address suggestions: the Data review surface.
                onData = { navController.navigate("data") },
                // Issue #348: admin user management.
                onManageUsers = { navController.navigate("admin/users") },
                // Issue #424: the operational-event timeline.
                onSystemEvents = { navController.navigate("admin/system-events") },
                onLocaleChanged = onLocaleChanged,
            )
        }
        // Issue #348: admin user management, reached from Settings (gated on
        // SessionState.isAdmin there). The route lives outside the drawer set.
        composable("admin/users") {
            UsersScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // Issue #424: the operational-event timeline (web's /system-events),
        // reached from Settings (gated on SessionState.isAdmin there). Admin-only,
        // instance-wide — the route lives outside the drawer set.
        composable("admin/system-events") {
            SystemEventsScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // T104 + address suggestions: the "propose data" review screen.
        composable("data") {
            DataScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // M16: the read-only audit log (web's /audit), reachable from the
        // drawer. Contact rows link to the contact detail when the UID
        // resolves.
        composable("audit") {
            AuditScreen(
                onBack = { navController.popBackStack() },
                onOpenContact = { id -> navController.navigate("contacts/$id") },
            )
        }
        composable("custom-links") {
            CustomLinkActionsScreen(
                onBack = { navController.popBackStack() },
            )
        }
        composable("webhooks") {
            WebhooksScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // Issue #413's Android follow-up (#573): API token management.
        composable("api-tokens") {
            ApiTokensScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // Issue #814 Phase 2: TOTP two-factor enrollment/management.
        composable("two-factor") {
            TwoFactorScreen(
                onBack = { navController.popBackStack() },
            )
        }
        composable("notification-channels") {
            NotificationChannelsScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // Issue #236: the Immich connection-config settings screen.
        composable("immich-settings") {
            ImmichSettingsScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // M26: one-time legacy circle/tag cleanup (reached from Settings).
        composable("circle-tag-triage") {
            TriageScreen(
                onBack = { navController.popBackStack() },
            )
        }
        composable("circles") {
            CirclesScreen(
                onMenuClick = menu,
                onOpenCircle = { id -> navController.navigate("circles/$id") },
            )
        }
        composable(
            route = "circles/{circleId}",
            arguments = listOf(navArgument("circleId") { type = NavType.StringType }),
        ) {
            CircleDetailScreen(
                onBack = { navController.popBackStack() },
            )
        }
        composable("tags") {
            TagsScreen(
                onMenuClick = menu,
                onOpenTag = { id -> navController.navigate("tags/$id") },
            )
        }
        composable(
            route = "tags/{tagId}",
            arguments = listOf(navArgument("tagId") { type = NavType.StringType }),
        ) {
            TagDetailScreen(
                onBack = { navController.popBackStack() },
            )
        }
        // M14: the ego-centric network list (drawer entry — "start from"
        // defaults to the self contact, else a picker; the contact-detail
        // entry below passes the viewed contact as the starting point).
        composable("network") {
            NetworkScreen(
                showMenu = true,
                onMenuClick = menu,
                onOpenContact = { id -> navController.navigate("contacts/$id") },
            )
        }
        composable(
            route = "contacts/{contactId}/network",
            arguments = listOf(navArgument("contactId") { type = NavType.IntType }),
        ) {
            NetworkScreen(
                showMenu = false,
                onBack = { navController.popBackStack() },
                onOpenContact = { id -> navController.navigate("contacts/$id") },
            )
        }
        composable("households") {
            HouseholdsScreen(
                onMenuClick = menu,
                onOpenHousehold = { id -> navController.navigate("households/$id") },
            )
        }
        composable(
            route = "households/{householdId}",
            arguments = listOf(navArgument("householdId") { type = NavType.StringType }),
        ) {
            HouseholdDetailScreen(
                onBack = { navController.popBackStack() },
                onNavigateToContact = { id -> navController.navigate("contacts/$id") },
            )
        }
    }
}

/** Launches the native Contacts QuickContact card for an imported contact (§7.5.4). */
private fun openInContacts(context: android.content.Context, lookupKey: String) {
    val lookupUri = com.mycorrhizal.crm.feature.imports.DeviceContactLink.quickContactLookupUri(lookupKey)
        ?: return
    val intent = android.content.Intent(android.provider.ContactsContract.QuickContact.ACTION_QUICK_CONTACT).apply {
        data = lookupUri
    }
    try {
        context.startActivity(intent)
    } catch (_: Exception) {
        // No handler / missing contact — nothing to do.
    }
}
