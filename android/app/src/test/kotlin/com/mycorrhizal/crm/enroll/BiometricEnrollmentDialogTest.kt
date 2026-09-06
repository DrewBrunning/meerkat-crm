package com.mycorrhizal.crm.enroll

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
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

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class BiometricEnrollmentDialogTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private fun setDialog(
        error: Boolean = false,
        onEnroll: () -> Unit = {},
        onNotNow: () -> Unit = {},
        onNever: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            MycorrhizalTheme {
                BiometricEnrollmentDialog(
                    isBusy = false,
                    error = error,
                    onEnroll = onEnroll,
                    onNotNow = onNotNow,
                    onNeverAskAgain = onNever,
                )
            }
        }
    }

    // Issue #722: the dialog offers all three enrollment choices.
    @Test
    fun `the dialog shows set up, not now and never ask again`() {
        setDialog()

        composeTestRule.onNodeWithText("Sign in with biometrics?").assertIsDisplayed()
        composeTestRule.onNodeWithText("Set up").assertIsDisplayed()
        composeTestRule.onNodeWithText("Not now").assertIsDisplayed()
        composeTestRule.onNodeWithText("Never ask again").assertIsDisplayed()
    }

    @Test
    fun `each choice invokes its callback`() {
        var enroll = false
        var notNow = false
        var never = false
        setDialog(onEnroll = { enroll = true }, onNotNow = { notNow = true }, onNever = { never = true })

        composeTestRule.onNodeWithText("Set up").performClick()
        assertTrue(enroll)

        composeTestRule.onNodeWithText("Not now").performClick()
        assertTrue(notNow)

        composeTestRule.onNodeWithText("Never ask again").performClick()
        assertTrue(never)
    }
}
