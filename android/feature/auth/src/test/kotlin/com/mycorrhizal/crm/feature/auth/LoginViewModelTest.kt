package com.mycorrhizal.crm.feature.auth

import app.cash.turbine.test
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.LoginOutcome
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.domain.usecase.LoginUseCase
import com.mycorrhizal.crm.domain.usecase.LoginWithApiTokenUseCase
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import com.mycorrhizal.crm.ui.R
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class LoginViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private class Harness(
        val viewModel: LoginViewModel,
        val authRepository: AuthRepository,
        val sessionManager: SessionManager,
    )

    private fun harness(): Harness {
        val authRepository = mockk<AuthRepository>()
        coEvery { authRepository.observeSession() } returns MutableStateFlow(SessionState())
        coEvery { authRepository.complete2faLogin(any()) } returns Result.success(Unit)
        val sessionManager = mockk<SessionManager>()
        every { sessionManager.observeSession() } returns MutableStateFlow(SessionState())
        coEvery { sessionManager.setServerUrl(any()) } returns Unit
        val viewModel = LoginViewModel(
            loginUseCase = LoginUseCase(authRepository),
            loginWithApiTokenUseCase = LoginWithApiTokenUseCase(authRepository),
            sessionManager = sessionManager,
            authRepository = authRepository,
        )
        return Harness(viewModel, authRepository, sessionManager)
    }

    private fun submit(
        h: Harness,
        serverUrl: String = "https://crm.example.com",
        identifier: String = "alice",
        password: String = "secret",
        apiToken: String = "",
    ) {
        h.viewModel.onSubmit(serverUrl, identifier, password, apiToken)
    }

    @Test
    fun `initial state is empty`() {
        val h = harness()
        val state = h.viewModel.uiState.value
        assertFalse(state.isLoading)
        assertEquals("", state.serverUrl)
        assertFalse(state.twoFactorStep)
    }

    @Test
    fun `submit with blank server url shows error`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        submit(h, serverUrl = "")

        assertEquals(R.string.login_error_valid_server_url, h.viewModel.uiState.value.errorRes)
    }

    @Test
    fun `server url with userinfo is rejected before any request`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        submit(h, serverUrl = "https://attacker@crm.example.com")
        advanceUntilIdle()

        assertEquals(R.string.login_error_valid_server_url, h.viewModel.uiState.value.errorRes)
        coVerify(exactly = 0) { h.authRepository.login(any(), any()) }
    }

    @Test
    fun `credentials are not part of the UI state`() {
        val h = harness()
        val state = h.viewModel.uiState.value
        // LoginUiState carries no credential fields; passwords/tokens are
        // passed straight to submit and never retained.
        assertEquals(LoginUiState().copy(mode = state.mode), state)
    }

    @Test
    fun `blank identifier shows validation error without calling repository`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        submit(h, identifier = "")
        advanceUntilIdle()

        assertEquals(R.string.login_error_identifier_required, h.viewModel.uiState.value.errorRes)
        coVerify(exactly = 0) { h.authRepository.login(any(), any()) }
    }

    @Test
    fun `successful password login emits LoggedIn`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.SessionEstablished)

        h.viewModel.events.test {
            submit(h, serverUrl = "https://crm.example.com/")
            assertTrue(awaitItem() is LoginEvent.ServerUrlUpdated)
            assertTrue(awaitItem() is LoginEvent.LoggedIn)
        }
        // Trailing slash is trimmed before persisting.
        coVerify { h.sessionManager.setServerUrl("https://crm.example.com") }
    }

    @Test
    fun `failed password login surfaces the error message`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login(any(), any()) } returns Result.failure(
            Exception("Invalid credentials"),
        )

        submit(h, password = "wrong")
        advanceUntilIdle()

        // LoginUseCase maps the failure to its message.
        assertEquals("Invalid credentials", h.viewModel.uiState.value.error)
        assertFalse(h.viewModel.uiState.value.isLoading)
    }

    // N8 (#814): a 2FA account's password step must NOT complete the login —
    // the UI moves to the code step and LoggedIn is deferred until step 2.
    @Test
    fun `2fa account moves to the code step instead of logging in`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.TwoFactorRequired)

        submit(h)
        advanceUntilIdle()

        val state = h.viewModel.uiState.value
        assertTrue(state.twoFactorStep)
        assertFalse(state.isLoading)
        coVerify(exactly = 0) { h.authRepository.complete2faLogin(any()) }
    }

    // Regression (issue #814): non-2FA password login is untouched.
    @Test
    fun `non 2fa login never enters the code step`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login(any(), any()) } returns
            Result.success(LoginOutcome.SessionEstablished)

        submit(h)
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.twoFactorStep)
    }

    @Test
    fun `2fa password failure stays on the credentials step`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login(any(), any()) } returns Result.failure(
            ApiError.Client(401, "Invalid username or password"),
        )

        submit(h, password = "wrong")
        advanceUntilIdle()

        assertFalse(h.viewModel.uiState.value.twoFactorStep)
        assertEquals("Invalid username or password", h.viewModel.uiState.value.error)
    }

    // N8 (#814): the whole flow must finish before LoggedIn is emitted (the
    // screen fires the autofill "save password" commit only on LoggedIn).
    @Test
    fun `2fa code success emits LoggedIn`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.TwoFactorRequired)
        coEvery { h.authRepository.complete2faLogin("123456") } returns Result.success(Unit)

        h.viewModel.events.test {
            submit(h)
            assertTrue(awaitItem() is LoginEvent.ServerUrlUpdated)
            h.viewModel.onSubmitTwoFactorCode("123456")
            assertTrue(awaitItem() is LoginEvent.LoggedIn)
        }
        assertFalse(h.viewModel.uiState.value.isLoading)
        coVerify { h.authRepository.complete2faLogin("123456") }
    }

    @Test
    fun `wrong 2fa code keeps the code step and shows an invalid code error`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.TwoFactorRequired)
        coEvery { h.authRepository.complete2faLogin("000000") } returns
            Result.failure(ApiError.Client(400, "Invalid value for field 'code'"))

        submit(h)
        advanceUntilIdle()
        h.viewModel.onSubmitTwoFactorCode("000000")
        advanceUntilIdle()

        val state = h.viewModel.uiState.value
        assertTrue(state.twoFactorStep)
        assertEquals(R.string.login_error_two_factor_invalid, state.errorRes)
        assertFalse(state.isLoading)
    }

    // N8 (#814): an expired/consumed challenge must restart at step 1, not
    // wedge the UI on the code step (a retry there can never succeed).
    @Test
    fun `expired 2fa challenge returns to the credentials step`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.TwoFactorRequired)
        coEvery { h.authRepository.complete2faLogin(any()) } returns Result.failure(
            ApiError.Client(401, "Invalid or expired two-factor session. Please sign in again."),
        )

        submit(h)
        advanceUntilIdle()
        assertTrue(h.viewModel.uiState.value.twoFactorStep)

        h.viewModel.onSubmitTwoFactorCode("123456")
        advanceUntilIdle()

        val state = h.viewModel.uiState.value
        assertFalse("must return to the credentials step", state.twoFactorStep)
        assertEquals(R.string.login_error_two_factor_expired, state.errorRes)
        assertFalse(state.isLoading)
    }

    // N8 (#814): the 429 lockout must surface the server's message and stop —
    // mirror the web's auth.ts 429 branch rather than a generic invalid-code.
    @Test
    fun `2fa lockout surfaces the server lockout text`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", "secret") } returns
            Result.success(LoginOutcome.TwoFactorRequired)
        coEvery { h.authRepository.complete2faLogin(any()) } returns Result.failure(
            ApiError.Client(429, "Too many failed login attempts. Please try again later."),
        )

        submit(h)
        advanceUntilIdle()
        h.viewModel.onSubmitTwoFactorCode("123456")
        advanceUntilIdle()

        val state = h.viewModel.uiState.value
        assertTrue(state.twoFactorStep)
        assertEquals("Too many failed login attempts. Please try again later.", state.error)
        assertFalse(state.isLoading)
    }

    @Test
    fun `back to credentials leaves the code step`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login(any(), any()) } returns
            Result.success(LoginOutcome.TwoFactorRequired)

        submit(h)
        advanceUntilIdle()
        assertTrue(h.viewModel.uiState.value.twoFactorStep)

        h.viewModel.onBackToCredentials()

        assertFalse(h.viewModel.uiState.value.twoFactorStep)
        assertNull(h.viewModel.uiState.value.errorRes)
    }

    @Test
    fun `api token mode validates the token prefix`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        h.viewModel.onModeChange(LoginMode.API_TOKEN)
        submit(h, apiToken = "not-a-token")
        advanceUntilIdle()

        assertEquals(R.string.login_error_token_prefix, h.viewModel.uiState.value.errorRes)
        coVerify(exactly = 0) { h.authRepository.loginWithApiToken(any()) }
    }

    // Regression (issue #814): API-token login bypasses 2FA and is untouched.
    @Test
    fun `successful api token login emits LoggedIn`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.loginWithApiToken("mycorrhizal_abc") } returns Result.success(Unit)

        h.viewModel.onModeChange(LoginMode.API_TOKEN)
        h.viewModel.events.test {
            submit(h, apiToken = "mycorrhizal_abc")
            assertTrue(awaitItem() is LoginEvent.ServerUrlUpdated)
            assertTrue(awaitItem() is LoginEvent.LoggedIn)
        }
        assertFalse(h.viewModel.uiState.value.twoFactorStep)
    }

    // Issue #678: the authenticating leg of the session state machine. While a
    // login attempt is in flight the UI must show the loading state; it must
    // not be possible to wedge it by submitting again mid-flight.
    @Test
    fun `an in-flight login shows loading until it resolves`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        val gate = kotlinx.coroutines.CompletableDeferred<Result<LoginOutcome>>()
        coEvery { h.authRepository.login("alice", "secret") } coAnswers { gate.await() }

        submit(h)
        advanceUntilIdle()
        assertTrue("authenticating state must be visible mid-flight", h.viewModel.uiState.value.isLoading)

        gate.complete(Result.success(LoginOutcome.SessionEstablished))
        advanceUntilIdle()
        assertFalse(h.viewModel.uiState.value.isLoading)
    }

    @Test
    fun `submitting again while a login is in flight is ignored`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        val gate = kotlinx.coroutines.CompletableDeferred<Result<LoginOutcome>>()
        coEvery { h.authRepository.login("alice", "secret") } coAnswers { gate.await() }

        submit(h)
        advanceUntilIdle()
        submit(h)
        advanceUntilIdle()

        coVerify(exactly = 1) { h.authRepository.login("alice", "secret") }

        gate.complete(Result.success(LoginOutcome.SessionEstablished))
        advanceUntilIdle()
    }

    // Issue #678: the re-authenticating leg. A failed attempt leaves the
    // session logged out (the 401 wiring clears it); a subsequent attempt must
    // be able to reach authenticated again — the machine must not wedge on the
    // failure.
    @Test
    fun `a failed login can be retried after the session is cleared`() = runTest(mainDispatcherRule.testDispatcher) {
        val h = harness()
        coEvery { h.authRepository.login("alice", any()) } returnsMany listOf(
            Result.failure(Exception("Invalid credentials")),
            Result.success(LoginOutcome.SessionEstablished),
        )

        submit(h, password = "first-try")
        advanceUntilIdle()
        assertEquals("Invalid credentials", h.viewModel.uiState.value.error)
        assertFalse(h.viewModel.uiState.value.isLoading)

        // Re-authentication: the machine must not wedge on the failure — a
        // second attempt resolves to the authenticated (non-loading, no-error)
        // state again.
        submit(h, password = "second-try")
        advanceUntilIdle()
        assertFalse(h.viewModel.uiState.value.isLoading)
        assertNull(h.viewModel.uiState.value.error)
        coVerify(exactly = 2) { h.authRepository.login("alice", any()) }
    }
}
