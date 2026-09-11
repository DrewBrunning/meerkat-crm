package com.mycorrhizal.crm.compat

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
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

    // Regression coverage for the bug reported against issue #528: the notice
    // used to dock at the very top of the screen (docked there, it rendered
    // behind the system status bar's icons, which are always the topmost
    // z-layer, so its dismiss button was unreachable). It must now float
    // near the bottom instead -- this mounts it with the same overlay
    // placement MycorrhizalApp uses (a Box, bottom-aligned, offset 48dp) and
    // asserts it actually lands there and stays clickable.
    @Test
    fun `floats near the bottom instead of docking at the top`() {
        var dismissed = false
        val containerHeight = 800.dp
        composeTestRule.setContent {
            MycorrhizalTheme {
                Box(Modifier.height(containerHeight).width(400.dp)) {
                    ServerOutdatedNotice(
                        serverVersion = "0.6.10",
                        currentVersion = "0.7.0",
                        onDismiss = { dismissed = true },
                        modifier = Modifier
                            .align(Alignment.BottomCenter)
                            .padding(start = 16.dp, end = 16.dp, bottom = 48.dp),
                    )
                }
            }
        }

        val noticeBounds = composeTestRule.onNodeWithTag("server-outdated-notice").getBoundsInRoot()
        val rootBounds = composeTestRule.onRoot().getBoundsInRoot()

        // Not docked at the top: it must start well below the root's top
        // edge, where the status bar would otherwise sit behind it.
        assertTrue(
            "expected the notice to float away from the top edge, but it started at ${noticeBounds.top}",
            noticeBounds.top > rootBounds.top + 100.dp,
        )
        // Anchored near the bottom, offset by the 48dp system-nav clearance
        // rather than touching the very bottom edge.
        assertTrue(
            "expected the notice's bottom edge to sit clear of the root's bottom edge, " +
                "but it was at ${noticeBounds.bottom} of ${rootBounds.bottom}",
            rootBounds.bottom - noticeBounds.bottom >= 48.dp,
        )

        composeTestRule.onNodeWithContentDescription("Dismiss").performClick()
        assertTrue(dismissed)
    }
}
