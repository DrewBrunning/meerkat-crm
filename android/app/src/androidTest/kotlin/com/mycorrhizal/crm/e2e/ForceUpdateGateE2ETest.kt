package com.mycorrhizal.crm.e2e

import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.mycorrhizal.crm.MainActivity
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Issues #528 + #692: the blocking force-update screen against a REAL backend
 * that declares a floor above this debug build's versionName (0.1.0).
 *
 * The suite's default backend (docker-compose.test.yml, port 7300) reports no
 * floor, which is the policy's default posture. This test targets a second
 * backend instance (docker-compose.compat-test.yml, port 7301) that boots the
 * SAME all-in-one image with MIN_CLIENT_VERSION=0.9.0 set. That instance only
 * exists in the CI legs that start it (android-e2e / android-e2e-min-sdk), so
 * the test skips when it is not reachable — a local run without the second
 * backend is not a failure.
 *
 * Since issue #692 the server enforces its floor at authentication, so a
 * below-floor client's login would be refused there. The force-update screen is
 * the UX layer that pre-empts that: the app resolves the compat backend's
 * /health as soon as its URL is configured (LoginViewModel persists the URL as
 * it is typed; MainViewModel re-checks on URL change, even without a session),
 * so the blocking gate appears before any credential work — the auth form is
 * replaced, never left behind a sign-in the server would refuse. This pins the
 * real-app half of that flow: configure the below-floor server, the gate
 * appears, and the app never proceeds to the dashboard.
 *
 * The other two states (compatible / server-outdated notice) are covered by
 * MainViewModelTest + CompatibilityResolverTest; this test pins the one thing
 * those cannot: the real app, real /health, real pre-login gate.
 */
@OptIn(ExperimentalTestApi::class)
@RunWith(AndroidJUnit4::class)
class ForceUpdateGateE2ETest {

    @get:Rule
    val compose = createAndroidComposeRule<MainActivity>()

    private val backend = E2eBackend(serverUrl = COMPAT_BACKEND_URL)

    @Before
    fun setUp() {
        // Clean skip when the dedicated compat backend is not running.
        assumeTrue("compat backend at $COMPAT_BACKEND_URL is not reachable", backend.isReachable())
        backend.registerSeedUser()
        clearSession()
    }

    @After
    fun tearDown() {
        // Leave the app fully logged out (server URL included) so the next
        // test starts from a blank login screen.
        runCatching { clearSession() }
    }

    @Test
    fun configuringBelowFloorServer_showsForceUpdateGateAndBlocksDashboard() {
        waitForText("Sign in")

        // Configuring the below-floor server URL raises the pre-login gate: the
        // server's /health (resolved on URL change) declares a floor above this
        // build, so the auth form is swapped for the blocking screen instead of
        // submitting the user into a session the server would refuse.
        replaceTextInField("Server URL", COMPAT_BACKEND_URL)

        // The blocking screen appears and names the versions + server URL. The
        // versions live inside the one formatted message node, so match them as
        // substrings.
        waitForText("Update required")
        waitForSubstringText("0.9.0")
        waitForSubstringText("0.1.0")
        waitForText("Server: $COMPAT_BACKEND_URL")

        // No credentials were ever offered against the refused server, and the
        // app never proceeds to the dashboard.
        compose.waitUntil(5_000) {
            compose.onAllNodesWithText("Username or email").fetchSemanticsNodes().isEmpty() &&
                compose.onAllNodesWithText("Dashboard").fetchSemanticsNodes().isEmpty()
        }
    }

    @Test
    fun preLoginForceUpdateGate_returnsToTheAuthFlow() {
        waitForText("Sign in")
        replaceTextInField("Server URL", COMPAT_BACKEND_URL)

        waitForText("Update required")

        // No session exists behind a pre-login gate, so the escape is "Back to
        // sign in" (to point at a different server), not "Log out".
        waitForText("Back to sign in")
        compose.onNodeWithText("Back to sign in").performClick()

        waitForText("Sign in")
    }

    // --- minimal shared helpers (mirrors E2eBaseTest) ------------------------

    private fun waitForText(text: String, timeoutMs: Long = 30_000) {
        compose.waitUntilAtLeastOneExists(hasText(text), timeoutMs)
    }

    private fun waitForSubstringText(text: String, timeoutMs: Long = 30_000) {
        compose.waitUntilAtLeastOneExists(hasText(text, substring = true), timeoutMs)
    }

    private fun replaceTextInField(label: String, text: String) {
        waitForText(label)
        val field = compose.onNodeWithText(label)
        field.performTextClearance()
        field.performTextInput(text)
    }

    private fun clearSession() {
        val session = compose.activity.sessionManager
        runBlocking {
            session.awaitHydrated()
            session.clearSession(keepServerUrl = false)
        }
        waitForText("Sign in")
    }

    private companion object {
        /** The dedicated compat backend started by android-e2e's CI legs (see
         *  docker-compose.compat-test.yml) — reached via adb reverse like the
         *  suite's main backend. */
        const val COMPAT_BACKEND_URL = "http://127.0.0.1:7301"
    }
}
