package com.mycorrhizal.crm.feature.auth

import androidx.compose.ui.autofill.ContentType
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class LoginScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private fun setContent(
        uiState: LoginUiState = LoginUiState(),
        onServerUrlChange: (String) -> Unit = {},
        onModeChange: (LoginMode) -> Unit = {},
        onSubmit: (String, String, String, String) -> Unit = { _, _, _, _ -> },
        onTwoFactorSubmit: (String) -> Unit = {},
        onBackToCredentials: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            MycorrhizalTheme {
                LoginScreenContent(
                    uiState = uiState,
                    onServerUrlChange = onServerUrlChange,
                    onModeChange = onModeChange,
                    onSubmit = onSubmit,
                    onTwoFactorSubmit = onTwoFactorSubmit,
                    onBackToCredentials = onBackToCredentials,
                )
            }
        }
    }

    @Test
    fun `renders server url and credential fields`() {
        setContent()
        composeTestRule.onNodeWithText("Server URL").assertIsDisplayed()
        composeTestRule.onNodeWithText("Username or email").performScrollTo().assertIsDisplayed()
        composeTestRule.onNode(hasText("Password") and hasSetTextAction()).performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Sign in").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun `typing server url forwards the change`() {
        var value: String? = null
        setContent(onServerUrlChange = { value = it })
        composeTestRule.onNodeWithText("Server URL").performTextInput("https://crm.example.com")
        assertEquals("https://crm.example.com", value)
    }

    @Test
    fun `api token mode swaps the credential field`() {
        var mode: LoginMode? = null
        setContent(onModeChange = { mode = it })
        composeTestRule.onNodeWithText("API token").performClick()
        assertEquals(LoginMode.API_TOKEN, mode)
    }

    @Test
    fun `submit passes the entered values to the callback`() {
        var captured: List<String>? = null
        setContent(onSubmit = { serverUrl, identifier, password, apiToken ->
            captured = listOf(serverUrl, identifier, password, apiToken)
        })
        composeTestRule.onNodeWithText("Server URL").performTextInput("https://crm.example.com")
        composeTestRule.onNodeWithText("Username or email").performTextInput("alice")
        composeTestRule.onNode(hasText("Password") and hasSetTextAction()).performTextInput("secret")
        composeTestRule.onNodeWithText("Sign in").performScrollTo().performClick()

        assertEquals(listOf("https://crm.example.com", "alice", "secret", ""), captured)
    }

    // Password managers/Autofill match a field by its semantics ContentType,
    // not its label text -- a typo here (e.g. NewPassword instead of
    // Password, which tells the OS this is account *creation*) would compile
    // and pass every other test in this file, since none of them touch
    // semantics. This is what actually makes autofill work on this screen.
    //
    // The identifier field's ContentType is a *combined* one
    // (Username + EmailAddress) -- verified empirically that
    // androidx.compose.ui.autofill.ContentType's `+` result has no
    // equals()/hashCode() override, so two separately-constructed combined
    // values (this test's vs. LoginScreenContent's) are never
    // SemanticsMatcher.expectValue-equal even when built from the identical
    // expression. keyIsDefined is the strongest assertion actually available
    // for a combined ContentType; the single, non-combined Password field
    // doesn't have that limitation, so it gets the exact-value assertion.
    @Test
    fun `the identifier and password fields advertise their content type`() {
        setContent()

        composeTestRule.onNodeWithText("Username or email").performScrollTo()
            .assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.ContentType))
        composeTestRule.onNode(hasText("Password") and hasSetTextAction()).performScrollTo()
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.ContentType, ContentType.Password))
    }

    @Test
    fun `loading state hides the submit button`() {
        setContent(uiState = LoginUiState(isLoading = true))
        composeTestRule.onNodeWithText("Sign in").assertDoesNotExist()
    }

    // M26: the login screen links the register and forgot-password flows.
    @Test
    fun `register and forgot-password links are present and invoke their callbacks`() {
        var registered = false
        var forgot = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                LoginScreenContent(
                    uiState = LoginUiState(),
                    onServerUrlChange = {},
                    onModeChange = {},
                    onSubmit = { _, _, _, _ -> },
                    onRegisterClick = { registered = true },
                    onForgotPasswordClick = { forgot = true },
                )
            }
        }

        composeTestRule.onNodeWithText("No account? Create one").performScrollTo().performClick()
        composeTestRule.onNodeWithText("Forgot password?").performScrollTo().performClick()

        assertEquals(true, registered)
        assertEquals(true, forgot)
    }

    // #203: the OIDC native-return failure (MainActivity) used to be a Toast
    // -- it's now injected as `oidcError` and shown through this screen's own
    // SnackbarHostState, same as the screen's own submit errors.
    @Test
    fun `an injected oidc error is shown via the snackbar`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                LoginScreenContent(
                    uiState = LoginUiState(),
                    onServerUrlChange = {},
                    onModeChange = {},
                    onSubmit = { _, _, _, _ -> },
                    oidcError = "Single sign-on failed",
                    onOidcErrorShown = {},
                )
            }
        }

        composeTestRule.waitForIdle()
        composeTestRule.onNodeWithText("Single sign-on failed").assertIsDisplayed()
    }

    @Test
    fun `the snackbar host is an assertive live region`() {
        setContent()

        // Not exercising assertIsDisplayed() -- an empty SnackbarHost with no
        // active message has zero visible size, which Compose UI testing
        // treats as "not displayed" even though the node (and its live
        // region) genuinely exists in the tree.
        composeTestRule.onNode(SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Assertive))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.LiveRegion, LiveRegionMode.Assertive))
    }

    @Test
    fun `the loading spinner announces itself while signing in`() {
        setContent(uiState = LoginUiState(isLoading = true))

        composeTestRule.onNode(SemanticsMatcher.expectValue(SemanticsProperties.ContentDescription, listOf("Saving")))
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.ContentDescription, listOf("Saving")))
    }

    // N8 (#814): a 2FA account moves the login form to the code-entry step.
    @Test
    fun `two-factor step shows the code prompt instead of the credentials form`() {
        setContent(uiState = LoginUiState(twoFactorStep = true, mode = LoginMode.PASSWORD))

        composeTestRule.onNodeWithText("Two-factor authentication").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText(
            "Enter the 6-digit code from your authenticator app, or one of your recovery codes.",
        ).performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Verification code").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Back to sign in").performScrollTo().assertIsDisplayed()

        // The credentials form is hidden while the code step is up.
        composeTestRule.onNodeWithText("Server URL").assertDoesNotExist()
        composeTestRule.onNodeWithText("Username or email").assertDoesNotExist()
        // No SSO/register links on the second step (web parity).
        composeTestRule.onNodeWithText("No account? Create one").assertDoesNotExist()
    }

    @Test
    fun `submitting a code on the two-factor step forwards the code`() {
        var submitted: String? = null
        setContent(
            uiState = LoginUiState(twoFactorStep = true, mode = LoginMode.PASSWORD),
            onTwoFactorSubmit = { submitted = it },
        )
        composeTestRule.onNodeWithText("Verification code").performScrollTo().performTextInput("123456")
        composeTestRule.onNodeWithText("Sign in").performScrollTo().performClick()

        assertEquals("123456", submitted)
    }

    @Test
    fun `the back link leaves the code step`() {
        var backed = false
        setContent(
            uiState = LoginUiState(twoFactorStep = true, mode = LoginMode.PASSWORD),
            onBackToCredentials = { backed = true },
        )
        composeTestRule.onNodeWithText("Back to sign in").performScrollTo().performClick()

        assertEquals(true, backed)
    }
}
