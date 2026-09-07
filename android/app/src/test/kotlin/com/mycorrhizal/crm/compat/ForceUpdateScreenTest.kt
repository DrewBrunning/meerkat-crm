package com.mycorrhizal.crm.compat

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// Issue #528: the blocking force-update screen. Stateless rendering + the
// logout escape hatch; the gate that decides when it shows is covered by
// RootSurfaceTest / MainViewModelTest.
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ForceUpdateScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private fun setContent(
        requiredVersion: String = "0.6.0",
        currentVersion: String = "0.1.0",
        serverUrl: String? = "https://crm.example.com",
        onLogout: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            MycorrhizalTheme {
                ForceUpdateScreen(
                    requiredVersion = requiredVersion,
                    currentVersion = currentVersion,
                    serverUrl = serverUrl,
                    onLogout = onLogout,
                )
            }
        }
    }

    @Test
    fun `shows the required current and server versions`() {
        setContent(requiredVersion = "0.6.0", currentVersion = "0.5.0", serverUrl = "https://crm.example.com")

        composeTestRule.onNodeWithText("Update required").assertIsDisplayed()
        // The message and the server line both name the actionable versions.
        composeTestRule.onNodeWithText("0.6.0", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("0.5.0", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("Server: https://crm.example.com").assertIsDisplayed()
    }

    @Test
    fun `omits the server line when no server URL is known`() {
        setContent(serverUrl = null)

        composeTestRule.onNodeWithText("Server:", substring = true).assertDoesNotExist()
    }

    @Test
    fun `logout ends the session`() {
        var loggedOut = false
        setContent(onLogout = { loggedOut = true })

        composeTestRule.onNodeWithText("Log out").performClick()

        assertTrue(loggedOut)
    }

    @Test
    fun `screen is reachable by its test tag`() {
        setContent()

        composeTestRule.onNodeWithTag("force-update-screen").assertIsDisplayed()
    }
}
