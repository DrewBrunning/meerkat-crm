package com.mycorrhizal.crm.feature.settings

import android.content.Context
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.model.network.RelationshipEdge
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import com.mycorrhizal.crm.ui.R
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class SettingsViewModelTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private val authRepository = mockk<AuthRepository>()
    private val trackingSettings = mockk<TrackingSettingsRepository>()
    private val appSettings = mockk<AppSettingsRepository>()
    private val relationshipEdgeRepository = mockk<RelationshipEdgeRepository>()
    private val localAuthSettings = mockk<LocalAuthSettingsRepository>()
    private val localAuthCapabilities = mockk<LocalAuthCapabilities>()
    private val appContext = mockk<Context>(relaxed = true)

    private fun viewModel(
        session: SessionState = SessionState(),
        themePreference: String = AppSettingsRepository.THEME_SYSTEM,
        requireLocalAuth: Boolean = false,
        autoLockDelay: AutoLockDelay = AutoLockDelay.DEFAULT,
        localAuthSupported: Boolean = true,
    ): SettingsViewModel {
        coEvery { trackingSettings.callTrackingEnabled() } returns false
        coEvery { trackingSettings.smsTrackingEnabled() } returns false
        coEvery { trackingSettings.notificationsEnabled() } returns true
        every { authRepository.observeSession() } returns MutableStateFlow(session)
        coEvery { appSettings.themePreference() } returns flowOf(themePreference)
        every { localAuthSettings.requireLocalAuth() } returns MutableStateFlow(requireLocalAuth)
        every { localAuthSettings.autoLockDelay() } returns MutableStateFlow(autoLockDelay)
        every { localAuthCapabilities.canEnableLocalAuth() } returns localAuthSupported
        return SettingsViewModel(
            authRepository,
            trackingSettings,
            appSettings,
            relationshipEdgeRepository,
            localAuthSettings,
            localAuthCapabilities,
            appContext,
        )
    }

    @Test
    fun `exposes the current session`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(
            SessionState(serverUrl = "https://crm.example.com", username = "alice", isAdmin = true, language = "en"),
        )
        advanceUntilIdle()

        val state = vm.uiState.value
        assertEquals("https://crm.example.com", state.session.serverUrl)
        assertEquals("alice", state.session.username)
        assertTrue(state.session.isAdmin)
    }

    @Test
    fun `logout clears the session and emits LoggedOut`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { authRepository.logout() } returns Unit
        val vm = viewModel(
            SessionState(serverUrl = "https://crm.example.com", username = "alice", isLoggedIn = true),
        )
        advanceUntilIdle()

        vm.logout()
        advanceUntilIdle()

        coVerify { authRepository.logout() }
        assertEquals(SettingsEvent.LoggedOut, vm.events.value)
    }

    @Test
    fun `tracking toggles persist their preference`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { trackingSettings.setCallTrackingEnabled(true) } returns Unit
        coEvery { trackingSettings.setSmsTrackingEnabled(true) } returns Unit
        val vm = viewModel()
        advanceUntilIdle()

        vm.setCallTrackingEnabled(true)
        vm.setSmsTrackingEnabled(true)
        advanceUntilIdle()

        assertTrue(vm.uiState.value.callTrackingEnabled)
        assertTrue(vm.uiState.value.smsTrackingEnabled)
        coVerify { trackingSettings.setCallTrackingEnabled(true) }
        coVerify { trackingSettings.setSmsTrackingEnabled(true) }
    }

    // --- Issue #722: the opt-in local app lock ---

    @Test
    fun `the app-lock preference defaults to off`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel()
        advanceUntilIdle()

        assertFalse(vm.uiState.value.requireLocalAuth)
    }

    @Test
    fun `toggling the app lock on persists the preference`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { localAuthSettings.setRequireLocalAuth(true) } returns Unit
        val vm = viewModel(requireLocalAuth = false)
        advanceUntilIdle()

        vm.setRequireLocalAuth(true)
        advanceUntilIdle()

        assertTrue(vm.uiState.value.requireLocalAuth)
        coVerify { localAuthSettings.setRequireLocalAuth(true) }
    }

    @Test
    fun `toggling the app lock off persists the preference`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { localAuthSettings.setRequireLocalAuth(false) } returns Unit
        val vm = viewModel(requireLocalAuth = true)
        advanceUntilIdle()

        vm.setRequireLocalAuth(false)
        advanceUntilIdle()

        assertFalse(vm.uiState.value.requireLocalAuth)
        coVerify { localAuthSettings.setRequireLocalAuth(false) }
    }

    @Test
    fun `a loaded app-lock preference is surfaced`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(requireLocalAuth = true, autoLockDelay = AutoLockDelay.ONE_HOUR)
        advanceUntilIdle()

        assertTrue(vm.uiState.value.requireLocalAuth)
        assertEquals(AutoLockDelay.ONE_HOUR, vm.uiState.value.autoLockDelay)
    }

    // The toggle must not be enableable on a device that cannot satisfy the
    // gate — there would be no way it could ever open.
    @Test
    fun `enabling on an unsupported device is refused`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(localAuthSupported = false)
        advanceUntilIdle()

        assertFalse(vm.uiState.value.localAuthSupported)
        vm.setRequireLocalAuth(true)
        advanceUntilIdle()

        assertFalse(vm.uiState.value.requireLocalAuth)
        coVerify(exactly = 0) { localAuthSettings.setRequireLocalAuth(any()) }
    }

    @Test
    fun `the delay defaults to five minutes`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel()
        advanceUntilIdle()

        assertEquals(AutoLockDelay.DEFAULT, vm.uiState.value.autoLockDelay)
    }

    @Test
    fun `changing the delay persists it while the app lock is on`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { localAuthSettings.setAutoLockDelay(AutoLockDelay.ONE_MINUTE) } returns Unit
        val vm = viewModel(requireLocalAuth = true)
        advanceUntilIdle()

        vm.setAutoLockDelay(AutoLockDelay.ONE_MINUTE)
        advanceUntilIdle()

        assertEquals(AutoLockDelay.ONE_MINUTE, vm.uiState.value.autoLockDelay)
        coVerify { localAuthSettings.setAutoLockDelay(AutoLockDelay.ONE_MINUTE) }
    }

    // The timeout only matters while the lock is on; the UI only shows the
    // dropdown then, and the VM refuses a change while it is off.
    @Test
    fun `changing the delay while the app lock is off is refused`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(requireLocalAuth = false)
        advanceUntilIdle()

        vm.setAutoLockDelay(AutoLockDelay.ONE_HOUR)
        advanceUntilIdle()

        assertEquals(AutoLockDelay.DEFAULT, vm.uiState.value.autoLockDelay)
        coVerify(exactly = 0) { localAuthSettings.setAutoLockDelay(any()) }
    }

    // --- M25 ---

    @Test
    fun `updateLanguage persists server-side, caches locally and emits LocaleChanged`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.updateLanguage("de") } returns Result.success(Unit)
            coEvery { appSettings.setLanguageOverride("de") } returns Unit
            val vm = viewModel(SessionState(language = "en"))
            advanceUntilIdle()

            vm.updateLanguage("de")
            advanceUntilIdle()

            coVerify { authRepository.updateLanguage("de") }
            coVerify { appSettings.setLanguageOverride("de") }
            assertEquals(SettingsEvent.LocaleChanged, vm.events.value)
        }

    @Test
    fun `updateLanguage to the current value is a no-op`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(SessionState(language = "de"))
        advanceUntilIdle()

        vm.updateLanguage("de")
        advanceUntilIdle()

        coVerify(exactly = 0) { authRepository.updateLanguage(any()) }
        assertNull(vm.events.value)
    }

    @Test
    fun `updateDateFormat persists server-side`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { authRepository.updateDateFormat("us") } returns Result.success(Unit)
        val vm = viewModel(SessionState(dateFormat = "eu"))
        advanceUntilIdle()

        vm.updateDateFormat("us")
        advanceUntilIdle()

        coVerify { authRepository.updateDateFormat("us") }
        assertNull(vm.events.value)
    }

    @Test
    fun `setThemePreference persists locally`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { appSettings.setThemePreference(AppSettingsRepository.THEME_DARK) } returns Unit
        val vm = viewModel()
        advanceUntilIdle()

        vm.setThemePreference(AppSettingsRepository.THEME_DARK)
        advanceUntilIdle()

        coVerify { appSettings.setThemePreference(AppSettingsRepository.THEME_DARK) }
    }

    @Test
    fun `changePassword success logs out and emits PasswordChanged`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.changePassword("old", "new-pass") } returns Result.success(Unit)
            coEvery { authRepository.logout() } returns Unit
            val vm = viewModel()
            advanceUntilIdle()

            vm.changePassword("old", "new-pass", "new-pass")
            advanceUntilIdle()

            coVerify { authRepository.changePassword("old", "new-pass") }
            coVerify { authRepository.logout() }
            assertEquals(SettingsEvent.PasswordChanged, vm.events.value)
            assertNull(vm.uiState.value.passwordError)
        }

    @Test
    fun `changePassword with a wrong current password surfaces the server error`() =
        runTest(mainDispatcherRule.testDispatcher) {
            coEvery { authRepository.changePassword("wrong", "new-pass") } returns
                Result.failure(ApiError.Client(400, "Invalid value for field 'current_password'"))
            val vm = viewModel()
            advanceUntilIdle()

            vm.changePassword("wrong", "new-pass", "new-pass")
            advanceUntilIdle()

            assertEquals("Invalid value for field 'current_password'", vm.uiState.value.passwordError)
            assertNull(vm.events.value)
        }

    @Test
    fun `changePassword with mismatched confirm never reaches the server`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel()
            advanceUntilIdle()

            vm.changePassword("old", "new-pass", "different")
            advanceUntilIdle()

            coVerify(exactly = 0) { authRepository.changePassword(any(), any()) }
            assertEquals(R.string.settings_password_mismatch, vm.uiState.value.passwordErrorRes)
            assertNull(vm.uiState.value.passwordError)
        }

    @Test
    fun `themePreference flows into the ui state`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(themePreference = AppSettingsRepository.THEME_DARK)
        advanceUntilIdle()

        assertEquals(AppSettingsRepository.THEME_DARK, vm.uiState.value.themePreference)
    }

    // --- T104 ---

    @Test
    fun `suggestRelationships records the count of newly created edges`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val edge = RelationshipEdge(id = "e1", type = "parent_of")
            coEvery { relationshipEdgeRepository.suggest() } returns Result.success(listOf(edge))
            val vm = viewModel()
            advanceUntilIdle()

            vm.suggestRelationships()
            advanceUntilIdle()

            coVerify { relationshipEdgeRepository.suggest() }
            assertEquals(1, vm.uiState.value.suggestedRelationshipCount)
            assertNull(vm.uiState.value.relationshipSuggestErrorRes)
            assertTrue(!vm.uiState.value.isSuggestingRelationships)
        }

    @Test
    fun `suggestRelationships with no new edges reports zero`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { relationshipEdgeRepository.suggest() } returns Result.success(emptyList())
        val vm = viewModel()
        advanceUntilIdle()

        vm.suggestRelationships()
        advanceUntilIdle()

        assertEquals(0, vm.uiState.value.suggestedRelationshipCount)
        assertNull(vm.uiState.value.relationshipSuggestErrorRes)
    }

    @Test
    fun `suggestRelationships failure surfaces the error resource`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { relationshipEdgeRepository.suggest() } returns Result.failure(ApiError.Client(500, "boom"))
        val vm = viewModel()
        advanceUntilIdle()

        vm.suggestRelationships()
        advanceUntilIdle()

        assertEquals(R.string.settings_suggest_relationships_error, vm.uiState.value.relationshipSuggestErrorRes)
        assertNull(vm.uiState.value.suggestedRelationshipCount)
    }

    @Test
    fun `onRelationshipSuggestBannerShown clears the result banner`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { relationshipEdgeRepository.suggest() } returns Result.success(listOf(RelationshipEdge(id = "e1")))
        val vm = viewModel()
        advanceUntilIdle()

        vm.suggestRelationships()
        advanceUntilIdle()
        assertEquals(1, vm.uiState.value.suggestedRelationshipCount)

        vm.onRelationshipSuggestBannerShown()
        assertNull(vm.uiState.value.suggestedRelationshipCount)
    }
}
