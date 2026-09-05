package com.mycorrhizal.crm.feature.auth

import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.model.network.AuthConfig
import com.mycorrhizal.crm.model.network.PasswordStrength
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.just
import io.mockk.mockk
import io.mockk.runs
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class RegisterViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val authRepository = mockk<AuthRepository>()
    private val sessionManager = mockk<SessionManager>()

    private fun viewModel(): RegisterViewModel {
        coEvery { sessionManager.serverUrl() } returns "https://crm.example.com"
        coEvery { sessionManager.setServerUrl(any()) } just runs
        // Registration-availability check fired from init — defaults to
        // enabled so the existing submit-flow tests below don't each need
        // to stub it. A test that wants the disabled case must re-stub
        // AFTER calling viewModel() (before advanceUntilIdle()) so its
        // coEvery is the one still in effect when init's launch actually
        // runs — see "registration disabled" below.
        coEvery { authRepository.getAuthConfig() } returns Result.success(AuthConfig(registrationDisabled = false))
        return RegisterViewModel(authRepository, sessionManager)
    }

    private fun weakStrength() = PasswordStrength(isValid = false, score = 1, feedback = "Password is too short")
    private fun strongStrength() = PasswordStrength(isValid = true, score = 4, feedback = "Password is very strong")

    @Test
    fun `a known-weak password blocks submit without calling register`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.checkPasswordStrength("short") } returns Result.success(weakStrength())
            val vm = viewModel()
            advanceUntilIdle()

            vm.onPasswordChange("short")
            advanceUntilIdle()
            assertTrue(vm.uiState.value.passwordChecked)

            vm.submit("alice", "alice@example.com", "short")
            advanceUntilIdle()

            assertFalse(vm.uiState.value.isLoading)
            assertEquals("Password is too short", vm.uiState.value.error)
            coVerify(exactly = 0) { authRepository.register(any(), any(), any()) }
        }

    @Test
    fun `submit while the strength check is still in flight is blocked`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.checkPasswordStrength(any()) } coAnswers {
                kotlinx.coroutines.delay(1000)
                Result.success(strongStrength())
            }
            val vm = viewModel()
            advanceUntilIdle()

            vm.onPasswordChange("hunter2hunter2")
            // Do NOT advance past the debounce: the verdict hasn't landed.
            vm.submit("alice", "alice@example.com", "hunter2hunter2")
            advanceUntilIdle()

            assertTrue(vm.uiState.value.isLoading.not())
            coVerify(exactly = 0) { authRepository.register(any(), any(), any()) }
        }

    @Test
    fun `a strong password submits register and then auto-logs-in`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.checkPasswordStrength("hunter2hunter2") } returns Result.success(strongStrength())
            coEvery { authRepository.register("alice", "alice@example.com", "hunter2hunter2") } returns Result.success(Unit)
            coEvery { authRepository.login("alice@example.com", "hunter2hunter2") } returns
                Result.success(com.mycorrhizal.crm.domain.repository.LoginOutcome.SessionEstablished)
            val vm = viewModel()
            advanceUntilIdle()

            vm.onPasswordChange("hunter2hunter2")
            advanceUntilIdle()

            vm.submit("alice", "alice@example.com", "hunter2hunter2")
            advanceUntilIdle()

            coVerify { authRepository.register("alice", "alice@example.com", "hunter2hunter2") }
            coVerify { authRepository.login("alice@example.com", "hunter2hunter2") }
            assertEquals(RegisterEvent.Registered, vm.events.first())
        }

    @Test
    fun `a duplicate account surfaces the server message and does not login`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.checkPasswordStrength("hunter2hunter2") } returns Result.success(strongStrength())
            coEvery { authRepository.register(any(), any(), any()) } returns
                Result.failure(ApiError.Client(409, "User already exists"))
            val vm = viewModel()
            advanceUntilIdle()

            vm.onPasswordChange("hunter2hunter2")
            advanceUntilIdle()

            vm.submit("alice", "alice@example.com", "hunter2hunter2")
            advanceUntilIdle()

            assertFalse(vm.uiState.value.isLoading)
            assertEquals("User already exists", vm.uiState.value.error)
            coVerify(exactly = 0) { authRepository.login(any(), any()) }
        }

    @Test
    fun `a failed auto-login after a successful register points the user at sign-in`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.checkPasswordStrength("hunter2hunter2") } returns Result.success(strongStrength())
            coEvery { authRepository.register(any(), any(), any()) } returns Result.success(Unit)
            coEvery { authRepository.login(any(), any()) } returns
                Result.failure(ApiError.Client(500, "boom"))
            val vm = viewModel()
            advanceUntilIdle()

            vm.onPasswordChange("hunter2hunter2")
            advanceUntilIdle()

            vm.submit("alice", "alice@example.com", "hunter2hunter2")
            advanceUntilIdle()

            assertEquals(com.mycorrhizal.crm.ui.R.string.register_created_login_failed, vm.uiState.value.errorRes)
            assertTrue(vm.uiState.value.isLoading.not())
        }

    @Test
    fun `blank fields block submit with a localized error`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel()
            advanceUntilIdle()

            vm.submit("", "", "")
            advanceUntilIdle()

            assertTrue(vm.uiState.value.errorRes != null)
            coVerify(exactly = 0) { authRepository.register(any(), any(), any()) }
        }

    @Test
    fun `an invalid server url blocks submit`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.checkPasswordStrength(any()) } returns Result.success(strongStrength())
            val vm = viewModel()
            advanceUntilIdle()
            vm.onServerUrlChange("not a url")

            vm.onPasswordChange("hunter2hunter2")
            advanceUntilIdle()

            vm.submit("alice", "alice@example.com", "hunter2hunter2")
            advanceUntilIdle()

            assertEquals(com.mycorrhizal.crm.ui.R.string.login_error_valid_server_url, vm.uiState.value.errorRes)
            coVerify(exactly = 0) { authRepository.register(any(), any(), any()) }
        }

    // Android testing feedback: DISABLE_REGISTRATION was only ever enforced
    // by the eventual 403 on submit — RegisterScreen now checks up front.

    @Test
    fun `registration disabled on the server is reflected in ui state on load`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel()
            coEvery { authRepository.getAuthConfig() } returns Result.success(AuthConfig(registrationDisabled = true))
            advanceUntilIdle()

            assertTrue(vm.uiState.value.registrationDisabled)
        }

    @Test
    fun `registration enabled on the server leaves ui state unset`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel()
            advanceUntilIdle()

            assertFalse(vm.uiState.value.registrationDisabled)
        }

    @Test
    fun `a failed availability check leaves the form usable`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel()
            coEvery { authRepository.getAuthConfig() } returns Result.failure(ApiError.Client(500, "boom"))
            advanceUntilIdle()

            assertFalse(vm.uiState.value.registrationDisabled)
        }

    @Test
    fun `no server url yet does not fire an availability check`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { sessionManager.serverUrl() } returns null
            coEvery { sessionManager.setServerUrl(any()) } just runs
            val vm = RegisterViewModel(authRepository, sessionManager)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.registrationDisabled)
            coVerify(exactly = 0) { authRepository.getAuthConfig() }
        }
}
