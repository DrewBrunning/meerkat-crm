package com.mycorrhizal.crm.e2e

import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.SemanticsNodeInteraction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.mycorrhizal.crm.MainActivity
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Before
import org.junit.Rule
import java.util.UUID

/**
 * Issue #238: the shared harness for the instrumented E2E suite. Each test:
 *
 *  1. registers the shared seed account against the docker-compose.test.yml
 *     backend (idempotent — a 409 from a prior run is fine),
 *  2. sweeps up contacts a crashed previous run left behind,
 *  3. clears the app's persisted session through the app's real SessionManager
 *     (accessed via MainActivity's injected field — the app then flips to the
 *     login screen on its own), and
 *  4. logs in through the real login UI.
 *
 * Because the session is cleared before every test, the login screen — and
 * therefore the login flow itself — is exercised on every single test, and no
 * test can depend on the order the others ran in.
 *
 * The suite talks to a real backend, so Compose's idle detection never knows
 * when a network round-trip is done — every server-data-dependent assertion
 * polls via the wait* helpers below instead of asserting on the current frame.
 */
@OptIn(ExperimentalTestApi::class)
abstract class E2eBaseTest {

    @get:Rule
    val compose = createAndroidComposeRule<MainActivity>()

    protected val backend = E2eBackend()

    private val createdContactIds = mutableListOf<Long>()

    @Before
    open fun e2eSetUp() {
        backend.registerSeedUser()
        backend.login()
        backend.cleanupTestContacts()
        clearSession()
        loginViaUi()
    }

    @After
    open fun e2eTearDown() {
        createdContactIds.forEach { id -> runCatching { backend.deleteContact(id) } }
        createdContactIds.clear()
        runCatching { clearSession() }
    }

    // --- data helpers --------------------------------------------------------

    /** A collision-resistant test contact name, prefixed for the leftover sweep. */
    protected fun uniqueName(label: String): String {
        val token = UUID.randomUUID().toString().replace("-", "").take(8)
        return "${E2eConfig.TEST_CONTACT_PREFIX} $label $token"
    }

    /** Creates a contact via the API and tracks it for teardown. */
    protected fun createTestContact(givenName: String, surname: String): E2eBackend.SeedContact {
        val contact = backend.createContact(givenName, surname)
        createdContactIds += contact.id
        return contact
    }

    // --- UI wait/click helpers -----------------------------------------------

    protected fun waitFor(matcher: SemanticsMatcher, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        compose.waitUntilAtLeastOneExists(matcher, timeoutMs)
    }

    protected fun waitForText(text: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) =
        waitFor(hasText(text), timeoutMs)

    protected fun waitForContentDescription(cd: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) =
        waitFor(hasContentDescription(cd), timeoutMs)

    protected fun waitForTag(tag: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) =
        waitFor(hasTestTag(tag), timeoutMs)

    /** Polls until the text is gone. Callers must first wait for the reload
     *  that should remove it (a transient loading state can mask it). */
    protected fun waitForTextGone(text: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        compose.waitUntil(timeoutMs) {
            compose.onAllNodesWithText(text).fetchSemanticsNodes().isEmpty()
        }
    }

    /** Types [text] into the editable node labeled [label], replacing what was
     *  there (the label node is the field's own semantics node — the
     *  LoginScreenTest/ContactListScreenTest idiom). */
    protected fun replaceTextInField(label: String, text: String) {
        waitForText(label)
        val field = compose.onNodeWithText(label)
        field.performTextClearance()
        field.performTextInput(text)
    }

    protected fun clickContentDescription(cd: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitForContentDescription(cd, timeoutMs)
        compose.onNodeWithContentDescription(cd).performClick()
    }

    /** The first node matching [text] — for lists where a contact legitimately
     *  appears in two sections (dashboard Favorites + Random). */
    protected fun onFirstText(text: String): SemanticsNodeInteraction {
        val all = compose.onAllNodesWithText(text)
        check(all.fetchSemanticsNodes().isNotEmpty()) { "expected a node with text '$text'" }
        return all.get(0)
    }

    /** The last node matching [text] — for drawer items and dialog confirm
     *  buttons, which render after (on top of) the screen content. */
    protected fun onLastText(text: String): SemanticsNodeInteraction {
        val all = compose.onAllNodesWithText(text)
        val size = all.fetchSemanticsNodes().size
        check(size > 0) { "expected a node with text '$text'" }
        return all.get(size - 1)
    }

    // --- app-state helpers ---------------------------------------------------

    /** Drops the app's persisted session via the real SessionManager; the app
     *  recomposes to the login screen on its own (no UI logout navigation). */
    protected fun clearSession() {
        val session = (compose.activity as MainActivity).sessionManager
        runBlocking {
            // The startup hydration (DefaultSessionManager.init) is async; wait
            // for it so a stale logged-in state can't be published after we
            // clear (it reads the already-cleared prefs and stays logged out).
            session.awaitHydrated()
            session.clearSession()
        }
        waitForText("Sign in")
    }

    /** Drives the real login screen. [serverUrl] defaults to the suite's
     *  configured backend; a targeted test (e.g. the issue #528 force-update
     *  suite) can point a login at a different backend instance. */
    protected fun loginViaUi(
        serverUrl: String = E2eConfig.serverUrl,
        username: String = E2eConfig.SEED_USERNAME,
        password: String = E2eConfig.SEED_PASSWORD,
    ) {
        waitForText("Sign in")
        replaceTextInField("Server URL", serverUrl)
        replaceTextInField("Username or email", username)
        // "Password" also labels the mode-segmented button, so the editable
        // field needs the SetText disambiguation (LoginScreenTest's idiom).
        waitForText("Password")
        compose.onNode(hasText("Password") and hasSetTextAction()).performTextInput(password)
        compose.onNodeWithText("Sign in").performClick()
        waitForText("Dashboard")
    }

    /** Opens the app drawer from the current screen and navigates to a
     *  destination by its drawer label. */
    protected fun navigateViaDrawer(label: String) {
        clickContentDescription("Menu")
        waitForText(label)
        // The drawer renders on top, so its item is the last match when the
        // label also appears in the screen behind it.
        onLastText(label).performClick()
        compose.waitForIdle()
    }

    /** Clears and refills the contact-list search field. */
    protected fun searchFor(query: String) {
        replaceTextInField("Search contacts", query)
    }

    /**
     * Navigates back via the screen's top-bar back arrow. Not Espresso.pressBack:
     * Espresso's event injection uses InputManager.getInstance(), which was
     * removed on newer API levels (fails on the API 37 CI emulator), and every
     * destination this suite backs out of has a real Back button anyway.
     */
    protected fun clickBack() {
        clickContentDescription("Back")
    }

    private companion object {
        /** Generous default: a full login + first-load against the docker stack. */
        const val DEFAULT_TIMEOUT_MS = 30_000L
    }
}
