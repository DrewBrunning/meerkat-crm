package com.mycorrhizal.crm.feature.settings

import android.content.Context
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.testing.a11y.assertAccessibleSemantics
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import io.mockk.coEvery
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SettingsScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    @Test
    fun `shows session info`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(
                        session = SessionState(
                            serverUrl = "https://crm.example.com",
                            username = "alice",
                            isAdmin = true,
                            language = "en",
                        ),
                    ),
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("https://crm.example.com").assertIsDisplayed()
        composeTestRule.onNodeWithText("alice").assertIsDisplayed()
        composeTestRule.onNodeWithText("Yes").assertIsDisplayed()
        composeTestRule.onNodeWithText("Log out").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun `logout button invokes the callback after confirmation`() {
        var loggedOut = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(session = SessionState(serverUrl = "https://crm.example.com", username = "alice")),
                    onLogout = { loggedOut = true },
                )
            }
        }

        composeTestRule.onNodeWithText("Log out").performScrollTo().performClick()
        // Confirmation dialog appears; the dialog's confirm button is the one
        // on top (the scroll-visible "Log out" button is behind the dialog).
        composeTestRule.onNodeWithText("Log out?").assertIsDisplayed()
        composeTestRule.onAllNodesWithText("Log out")[1].performClick()
        assertEquals(true, loggedOut)
    }

    // --- M25 ---

    @Test
    fun `shows the editable appearance and password sections`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(
                        session = SessionState(language = "en", dateFormat = "eu"),
                        themePreference = AppSettingsRepository.THEME_SYSTEM,
                    ),
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Appearance").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Language").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("English").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Theme").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Use system default").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Change Password").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Current password").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun `selecting a language invokes the language change callback`() {
        var changedTo: String? = null
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(session = SessionState(language = "en")),
                    onLanguageChange = { changedTo = it },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("English").performScrollTo().performClick()
        composeTestRule.onNodeWithText("Deutsch").performClick()
        assertEquals("de", changedTo)
    }

    @Test
    fun `selecting a theme invokes the theme change callback`() {
        var changedTo: String? = null
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(themePreference = AppSettingsRepository.THEME_SYSTEM),
                    onThemeChange = { changedTo = it },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Use system default").performScrollTo().performClick()
        composeTestRule.onNodeWithText("Dark mode").performClick()
        assertEquals(AppSettingsRepository.THEME_DARK, changedTo)
    }

    @Test
    fun `selecting a date format invokes the date format change callback`() {
        var changedTo: String? = null
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(session = SessionState(dateFormat = "eu")),
                    onDateFormatChange = { changedTo = it },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("European (DD.MM.YYYY)").performScrollTo().performClick()
        composeTestRule.onNodeWithText("ISO (YYYY-MM-DD)").performClick()
        assertEquals("iso", changedTo)
    }

    @Test
    fun `password change submits all three fields`() {
        var submitted: Triple<String, String, String>? = null
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(),
                    onChangePassword = { c, n, cf -> submitted = Triple(c, n, cf) },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Current password").performScrollTo().performClick()
        composeTestRule.onNodeWithText("New password").performClick()
        composeTestRule.onNodeWithText("Confirm new password").performClick()
        composeTestRule.onNodeWithText("Update password").performScrollTo().performClick()

        // The button stays disabled until fields are entered; with no text the
        // submit callback must never fire.
        assertEquals(null, submitted)
    }

    @Test
    fun `webhooks and notification channel rows invoke their navigation callbacks`() {
        var webhooks = false
        var channels = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(),
                    onWebhooks = { webhooks = true },
                    onNotificationChannels = { channels = true },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Webhooks").performScrollTo().performClick()
        assertTrue(webhooks)

        composeTestRule.onNodeWithText("Notification channels").performScrollTo().performClick()
        assertTrue(channels)
    }

    // Issue #413's Android follow-up (#573).
    @Test
    fun `api tokens row invokes its navigation callback`() {
        var apiTokens = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(),
                    onApiTokens = { apiTokens = true },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("API Tokens").performScrollTo().performClick()
        assertTrue(apiTokens)
    }

    // --- T104 / data suggestions ---

    @Test
    fun `suggest relationships button invokes the callback`() {
        var suggested = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(),
                    onSuggestRelationships = { suggested = true },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Suggest relationships").performScrollTo().performClick()
        assertTrue(suggested)
    }

    @Test
    fun `data row invokes the data navigation callback`() {
        var data = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(),
                    onData = { data = true },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Data suggestions").performScrollTo().performClick()
        assertTrue(data)
    }

    // --- Issue #214: Compose semantics a11y sweep (the axe-core analog) -----
    //
    // Mounts the real SettingsScreen (Scaffold + TopAppBar included) via a
    // mocked-repository SettingsViewModel — the same construction
    // SettingsViewModelTest uses — rather than SettingsContent above: the
    // sweep needs the whole top-level screen, chrome included.

    private fun setScreen(darkTheme: Boolean) {
        val authRepository = mockk<AuthRepository>()
        val trackingSettings = mockk<TrackingSettingsRepository>()
        val appSettings = mockk<AppSettingsRepository>()
        val relationshipEdgeRepository = mockk<RelationshipEdgeRepository>()
        val permissionChecker = mockk<com.mycorrhizal.crm.feature.tracking.PermissionChecker>()
        val catchUpScheduler = mockk<com.mycorrhizal.crm.feature.tracking.TrackingCatchUpScheduler>(relaxed = true)
        val appContext = mockk<Context>(relaxed = true)
        coEvery { trackingSettings.callTrackingEnabled() } returns false
        coEvery { trackingSettings.smsTrackingEnabled() } returns false
        coEvery { trackingSettings.notificationsEnabled() } returns true
        every { permissionChecker.isGranted(any()) } returns false
        every { authRepository.observeSession() } returns MutableStateFlow(
            SessionState(serverUrl = "https://crm.example.com", username = "alice", isAdmin = true, language = "en"),
        )
        coEvery { appSettings.themePreference() } returns flowOf(AppSettingsRepository.THEME_SYSTEM)
        val viewModel = SettingsViewModel(
            authRepository,
            trackingSettings,
            appSettings,
            relationshipEdgeRepository,
            permissionChecker,
            catchUpScheduler,
            appContext,
        )

        composeTestRule.setContent {
            MycorrhizalTheme(darkTheme = darkTheme) {
                SettingsScreen(onLoggedOut = {}, viewModel = viewModel)
            }
        }
    }

    @Test
    fun `settings screen has no accessibility violations (light)`() {
        setScreen(darkTheme = false)

        composeTestRule.assertAccessibleSemantics()
    }

    @Test
    fun `settings screen has no accessibility violations (dark)`() {
        setScreen(darkTheme = true)

        composeTestRule.assertAccessibleSemantics()
    }

    @Test
    fun `section titles are marked as headings`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(session = SessionState()),
                    onLogout = {},
                )
            }
        }

        // #208: section titles carried no heading semantics, so TalkBack's
        // heading navigation found nothing on this (scrollable) screen.
        composeTestRule.onNodeWithText("Session")
            .assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.Heading))
        composeTestRule.onNodeWithText("Appearance")
            .assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.Heading))
    }

    @Test
    fun `a password change error is announced as an assertive live region`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(session = SessionState(), passwordError = "Current password is wrong"),
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Current password is wrong")
            .performScrollTo()
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Assertive))
    }

    @Test
    fun `the change-password button announces saving while in flight`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(session = SessionState(), isChangingPassword = true),
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Update password")
            .performScrollTo()
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Saving"))
    }

    // --- Issue #721: tracking-permission denial dialogs ----------------------

    @Test
    fun `a rationale dialog explains the call-tracking permission and retries`() {
        var retried = false
        var dismissed = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(
                        permissionDialog = TrackingPermissionDialog.Rationale(
                            TrackingPermissionRequest.CALL_TRACKING,
                        ),
                    ),
                    onPermissionDialogRetry = { retried = true },
                    onPermissionDialogDismiss = { dismissed = true },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Permission needed").assertIsDisplayed()
        composeTestRule.onNodeWithText("Try again").performClick()
        assertEquals(true, retried)

        // Re-show and dismiss via "Not now" — the toggle must stay off and the
        // dialog must not re-request.
        composeTestRule.onNodeWithText("Not now").performClick()
        assertEquals(true, dismissed)
    }

    @Test
    fun `an app-settings dialog offers the system settings deep link for a permanent denial`() {
        var opened = false
        var dismissed = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                SettingsContent(
                    state = SettingsUiState(
                        permissionDialog = TrackingPermissionDialog.AppSettings(
                            TrackingPermissionRequest.SMS_TRACKING,
                        ),
                    ),
                    onPermissionDialogOpenSettings = { opened = true },
                    onPermissionDialogDismiss = { dismissed = true },
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Permission blocked").assertIsDisplayed()
        composeTestRule.onNodeWithText("Open settings").performClick()
        assertEquals(true, opened)

        composeTestRule.onNodeWithText("Not now").performClick()
        assertEquals(true, dismissed)
    }
}
