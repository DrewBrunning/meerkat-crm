package com.mycorrhizal.crm.applock

import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.mycorrhizal.crm.ui.R
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AppLockScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private fun setContent(
        state: AppLockUiState = AppLockUiState(username = "alice"),
        onUnlockRequest: () -> Unit = {},
        onLogout: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            MycorrhizalTheme {
                AppLockContent(
                    state = state,
                    onUnlockRequest = onUnlockRequest,
                    onLogout = onLogout,
                )
            }
        }
    }

    // Verify #1: the lock screen appears with the unlock affordance and no
    // session data beyond the username already shown on the OS lock screen.
    @Test
    fun `shows the lock heading and unlock button`() {
        setContent()

        composeTestRule.onNodeWithText("Your data is locked").assertIsDisplayed()
        composeTestRule.onNodeWithText("Signed in as alice").assertIsDisplayed()
        composeTestRule.onNodeWithText("Unlock with your fingerprint, face or device PIN to continue.")
            .assertIsDisplayed()
        composeTestRule.onNodeWithText("Unlock").assertIsDisplayed()
        composeTestRule.onNodeWithText("Log out").assertIsDisplayed()
    }

    @Test
    fun `the unlock button requests the local auth prompt`() {
        var requested = false
        setContent(onUnlockRequest = { requested = true })

        composeTestRule.onNodeWithText("Unlock").performClick()

        assertTrue(requested)
    }

    @Test
    fun `logout requests the session logout`() {
        var loggedOut = false
        setContent(onLogout = { loggedOut = true })

        composeTestRule.onNodeWithText("Log out").performClick()

        assertTrue(loggedOut)
    }

    // An in-flight prompt shows no button (the OS dialog is up).
    @Test
    fun `an in-flight unlock hides the unlock button`() {
        setContent(state = AppLockUiState(isUnlocking = true, username = "alice"))

        composeTestRule.onNodeWithText("Unlock").assertDoesNotExist()
    }

    // A device that can no longer satisfy the gate shows the reason and keeps
    // logout as the only way off the screen.
    @Test
    fun `an unsupported device shows the reason and no unlock button`() {
        setContent(state = AppLockUiState(username = "alice", canAuthenticate = false))

        composeTestRule.onNodeWithText(
            "Fingerprint, face or device PIN unlock is not available on this device. " +
                "Log out to sign in with your password.",
        ).assertIsDisplayed()
        composeTestRule.onNodeWithText("Unlock").assertDoesNotExist()
        composeTestRule.onNodeWithText("Log out").assertIsDisplayed()
    }

    // Verify: a transient auth failure is shown as an assertive live region so
    // TalkBack announces it (the screen has no auto-dismissing UI of its own).
    @Test
    fun `an auth failure is announced as an assertive live region`() {
        var shown = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                AppLockContent(
                    state = AppLockUiState(errorRes = R.string.app_lock_auth_failed),
                    onUnlockRequest = {},
                    onLogout = {},
                    onErrorShown = { shown = true },
                )
            }
        }

        composeTestRule.onNodeWithText("Couldn't unlock. Please try again.")
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Assertive))
        assertTrue(shown)
    }

    @Test
    fun `heading carries heading semantics`() {
        setContent()

        composeTestRule.onNodeWithText("Your data is locked")
            .assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.Heading))
    }
}
