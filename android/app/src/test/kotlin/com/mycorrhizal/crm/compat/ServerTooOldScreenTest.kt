package com.mycorrhizal.crm.compat

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// Issue #692: the blocking "server too old" gate shown when the configured
// server reports a version below this app's 0.6.0 baseline. Stateless
// rendering; the gate that decides when it shows is covered by RootSurfaceTest
// / MainViewModelTest / CompatibilityOutcomeTest.
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ServerTooOldScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    @Test
    fun `names the server version the app baseline and the server URL`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerTooOldScreen(
                    serverVersion = "0.5.3",
                    requiredVersion = "0.6.0",
                    serverUrl = "https://crm.example.com",
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Server needs an upgrade").assertIsDisplayed()
        composeTestRule.onNodeWithText("0.5.3", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("0.6.0", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("Server: https://crm.example.com").assertIsDisplayed()
    }

    @Test
    fun `logged in logout ends the session`() {
        var loggedOut = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerTooOldScreen(
                    serverVersion = "0.5.3",
                    requiredVersion = "0.6.0",
                    serverUrl = null,
                    loggedIn = true,
                    onLogout = { loggedOut = true },
                )
            }
        }

        composeTestRule.onNodeWithText("Log out").performClick()
        assertTrue(loggedOut)
    }

    @Test
    fun `pre-login escape returns to sign in instead of logging out`() {
        var backToLogin = false
        var loggedOut = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerTooOldScreen(
                    serverVersion = "0.5.3",
                    requiredVersion = "0.6.0",
                    serverUrl = null,
                    loggedIn = false,
                    onLogout = { loggedOut = true },
                    onBackToLogin = { backToLogin = true },
                )
            }
        }

        composeTestRule.onNodeWithText("Log out").assertDoesNotExist()
        composeTestRule.onNodeWithText("Back to sign in").performClick()
        assertTrue(backToLogin)
        assertFalse(loggedOut)
    }

    @Test
    fun `screen is reachable by its test tag`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerTooOldScreen(
                    serverVersion = "0.5.3",
                    requiredVersion = "0.6.0",
                    serverUrl = null,
                    onLogout = {},
                )
            }
        }

        composeTestRule.onNodeWithTag("server-too-old-screen").assertIsDisplayed()
    }
}
