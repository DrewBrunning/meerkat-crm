package com.mycorrhizal.crm.feature.settings

import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.model.network.MessageResponse
import com.mycorrhizal.crm.model.network.TwoFactorConfirmResponse
import com.mycorrhizal.crm.model.network.TwoFactorSetupResponse
import com.mycorrhizal.crm.model.network.TwoFactorStatusResponse
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class TwoFactorViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val authRepository = mockk<AuthRepository>()

    private fun enabledVm(enabled: Boolean): TwoFactorViewModel {
        coEvery { authRepository.getTwoFactorStatus() } returns
            Result.success(TwoFactorStatusResponse(enabled = enabled))
        val vm = TwoFactorViewModel(authRepository)
        return vm
    }

    @Test
    fun `load reflects 2fa disabled`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()

        val state = vm.uiState.value
        assertFalse(state.loading)
        assertEquals(false, state.enabled)
        assertEquals(null, state.error)
    }

    @Test
    fun `load reflects 2fa enabled`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()

        assertEquals(true, vm.uiState.value.enabled)
    }

    @Test
    fun `load failure surfaces the error and leaves enabled unknown`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { authRepository.getTwoFactorStatus() } returns
            Result.failure(ApiError.Server(500, "boom"))
        val vm = TwoFactorViewModel(authRepository)
        advanceUntilIdle()

        val state = vm.uiState.value
        assertFalse(state.loading)
        assertEquals(null, state.enabled)
        assertEquals("Server error (500)", state.error)
    }

    @Test
    fun `startSetup surfaces the pending secret and otpauth url`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()
        coEvery { authRepository.setupTwoFactor() } returns Result.success(
            TwoFactorSetupResponse(secret = "JBSWY3DPEHPK3PXP", otpauthUrl = "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP"),
        )

        vm.startSetup()
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals("JBSWY3DPEHPK3PXP", state.setup?.secret)
        assertFalse(state.busy)
    }

    @Test
    fun `setup on an oidc account surfaces the server's 403 message`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()
        coEvery { authRepository.setupTwoFactor() } returns Result.failure(
            ApiError.Client(403, "Two-factor authentication is unavailable for accounts that sign in with SSO"),
        )

        vm.startSetup()
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals("Two-factor authentication is unavailable for accounts that sign in with SSO", state.error)
        assertNull(state.setup)
    }

    @Test
    fun `confirmSetup enables 2fa and shows the recovery codes exactly once`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()
        coEvery { authRepository.setupTwoFactor() } returns Result.success(
            TwoFactorSetupResponse(secret = "JBSWY3DPEHPK3PXP", otpauthUrl = "otpauth://totp/x"),
        )
        coEvery { authRepository.confirmTwoFactor("123456") } returns Result.success(
            TwoFactorConfirmResponse(message = "Two-factor authentication enabled", recoveryCodes = listOf("AAAAA-BBBBB-CCCCC")),
        )

        vm.startSetup()
        advanceUntilIdle()
        vm.confirmSetup("123456")
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals(true, state.enabled)
        assertNull(state.setup)
        assertEquals(listOf("AAAAA-BBBBB-CCCCC"), state.recoveryCodes)
        assertFalse(state.busy)
    }

    @Test
    fun `a wrong code while confirming keeps the wizard open with a localized error`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()
        coEvery { authRepository.setupTwoFactor() } returns Result.success(
            TwoFactorSetupResponse(secret = "JBSWY3DPEHPK3PXP", otpauthUrl = "otpauth://totp/x"),
        )
        coEvery { authRepository.confirmTwoFactor(any()) } returns Result.failure(
            ApiError.Client(400, "Invalid value for field 'code'"),
        )

        vm.startSetup()
        advanceUntilIdle()
        vm.confirmSetup("000000")
        advanceUntilIdle()

        val state = vm.uiState.value
        assertNotNull(state.setup)
        assertEquals(com.mycorrhizal.crm.ui.R.string.settings_two_factor_invalid_code, state.errorRes)
        assertFalse(state.busy)
    }

    @Test
    fun `disabling with a live code turns 2fa off`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()
        coEvery { authRepository.disableTwoFactor("654321") } returns Result.success(
            MessageResponse(message = "Two-factor authentication disabled"),
        )

        vm.requestDisable()
        assertEquals(TwoFactorPrompt.DISABLE, vm.uiState.value.prompt)
        vm.submitPromptCode("654321")
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals(false, state.enabled)
        assertNull(state.prompt)
        assertFalse(state.busy)
        coVerify { authRepository.disableTwoFactor("654321") }
    }

    @Test
    fun `regenerating recovery codes shows the fresh set exactly once`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()
        coEvery { authRepository.regenerateRecoveryCodes("123456") } returns Result.success(
            TwoFactorConfirmResponse(recoveryCodes = listOf("NEWAA-BBBBB-CCCCC")),
        )

        vm.requestRegenerate()
        vm.submitPromptCode("123456")
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals(listOf("NEWAA-BBBBB-CCCCC"), state.recoveryCodes)
        assertNull(state.prompt)
        assertFalse(state.busy)
    }

    @Test
    fun `a wrong code while disabling keeps the prompt with a localized error`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()
        coEvery { authRepository.disableTwoFactor(any()) } returns Result.failure(
            ApiError.Client(400, "Invalid value for field 'code'"),
        )

        vm.requestDisable()
        vm.submitPromptCode("000000")
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals(TwoFactorPrompt.DISABLE, state.prompt)
        assertEquals(com.mycorrhizal.crm.ui.R.string.settings_two_factor_invalid_code, state.errorRes)
        assertFalse(state.busy)
    }

    @Test
    fun `dismissing the recovery codes clears them`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()
        coEvery { authRepository.regenerateRecoveryCodes(any()) } returns Result.success(
            TwoFactorConfirmResponse(recoveryCodes = listOf("NEWAA-BBBBB-CCCCC")),
        )

        vm.requestRegenerate()
        vm.submitPromptCode("123456")
        advanceUntilIdle()
        assertNotNull(vm.uiState.value.recoveryCodes)

        vm.dismissRecoveryCodes()

        assertNull(vm.uiState.value.recoveryCodes)
    }

    @Test
    fun `closing the setup wizard drops the pending secret`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()
        coEvery { authRepository.setupTwoFactor() } returns Result.success(
            TwoFactorSetupResponse(secret = "JBSWY3DPEHPK3PXP", otpauthUrl = "otpauth://totp/x"),
        )

        vm.startSetup()
        advanceUntilIdle()
        assertNotNull(vm.uiState.value.setup)

        vm.closeSetup()

        assertNull(vm.uiState.value.setup)
        // The secret is transient — nothing is left in state after closing.
        assertNull(vm.uiState.value.error)
    }

    @Test
    fun `dismissing a code prompt clears it`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()

        vm.requestRegenerate()
        assertEquals(TwoFactorPrompt.REGENERATE, vm.uiState.value.prompt)
        vm.dismissPrompt()

        assertNull(vm.uiState.value.prompt)
    }

    @Test
    fun `a blank prompt code is ignored without calling the repository`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(true)
        advanceUntilIdle()

        vm.requestDisable()
        vm.submitPromptCode("   ")
        advanceUntilIdle()

        assertFalse(vm.uiState.value.busy)
        coVerify(exactly = 0) { authRepository.disableTwoFactor(any()) }
        coVerify(exactly = 0) { authRepository.regenerateRecoveryCodes(any()) }
    }

    @Test
    fun `onErrorShown clears the error`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = enabledVm(false)
        advanceUntilIdle()
        coEvery { authRepository.setupTwoFactor() } returns Result.failure(
            ApiError.Client(403, "Two-factor authentication is unavailable for accounts that sign in with SSO"),
        )

        vm.startSetup()
        advanceUntilIdle()
        assertNotNull(vm.uiState.value.error)

        vm.onErrorShown()

        assertNull(vm.uiState.value.error)
        assertNull(vm.uiState.value.errorRes)
    }
}
