package com.mycorrhizal.crm.compat

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
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

// Issue #528: the non-blocking "server is older than this app" notice. It must
// render with both versions named and dismiss on demand; whether it appears is
// the root ViewModel's decision (MainViewModelTest).
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ServerOutdatedNoticeTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    @Test
    fun `renders the message naming both versions`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerOutdatedNotice(serverVersion = "0.6.10", currentVersion = "0.7.0", onDismiss = {})
            }
        }

        composeTestRule.onNodeWithText("0.6.10", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("0.7.0", substring = true).assertIsDisplayed()
    }

    @Test
    fun `dismisses the notice`() {
        var dismissed = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerOutdatedNotice(serverVersion = "0.6.10", currentVersion = "0.7.0", onDismiss = { dismissed = true })
            }
        }

        composeTestRule.onNodeWithContentDescription("Dismiss").performClick()

        assertTrue(dismissed)
    }

    @Test
    fun `notice is reachable by its test tag`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                ServerOutdatedNotice(serverVersion = "0.6.10", currentVersion = "0.7.0", onDismiss = {})
            }
        }

        composeTestRule.onNodeWithTag("server-outdated-notice").assertIsDisplayed()
    }
}
