package com.mycorrhizal.crm.feature.settings

import android.content.Context
import com.mycorrhizal.crm.data.auth.DeviceGrantManager
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import com.mycorrhizal.crm.domain.repository.BiometricEnrollmentStatus
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.SessionState
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.feature.tracking.PermissionChecker
import com.mycorrhizal.crm.feature.tracking.TrackingCatchUpScheduler
import com.mycorrhizal.crm.feature.tracking.TrackingPermissions
import com.mycorrhizal.crm.model.network.RelationshipEdge
import com.mycorrhizal.crm.network.ApiError
import com.mycorrhizal.crm.testing.MainDispatcherRule
import com.mycorrhizal.crm.ui.R
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.mockk
import io.mockk.verify
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
    private val permissionChecker = mockk<PermissionChecker>()
    private val catchUpScheduler = mockk<TrackingCatchUpScheduler>(relaxed = true)
    private val localAuthSettings = mockk<LocalAuthSettingsRepository>()
    private val localAuthCapabilities = mockk<LocalAuthCapabilities>()
    private val deviceGrantManager = mockk<DeviceGrantManager>()
    private val appContext = mockk<Context>(relaxed = true)

    /** A factory defaulting to "no tracking permissions granted, nothing stored". */
    private fun viewModel(
        session: SessionState = SessionState(),
        themePreference: String = AppSettingsRepository.THEME_SYSTEM,
        callStored: Boolean = false,
        smsStored: Boolean = false,
        callGranted: Boolean = false,
        smsGranted: Boolean = false,
    ): SettingsViewModel {
        coEvery { trackingSettings.callTrackingEnabled() } returns callStored
        coEvery { trackingSettings.smsTrackingEnabled() } returns smsStored
        coEvery { trackingSettings.notificationsEnabled() } returns true
        coEvery { trackingSettings.setCallTrackingEnabled(any()) } returns Unit
        coEvery { trackingSettings.setSmsTrackingEnabled(any()) } returns Unit
        every { authRepository.observeSession() } returns MutableStateFlow(session)
        coEvery { appSettings.themePreference() } returns flowOf(themePreference)
        every { localAuthSettings.requireLocalAuth() } returns MutableStateFlow(false)
        every { localAuthSettings.autoLockDelay() } returns MutableStateFlow(AutoLockDelay.DEFAULT)
        every { localAuthSettings.biometricEnrollmentStatus() } returns MutableStateFlow(BiometricEnrollmentStatus.UNASKED)
        every { localAuthCapabilities.canEnableLocalAuth() } returns true
        every { permissionChecker.isGranted(any()) } returns false
        every {
            permissionChecker.isGranted(TrackingPermissions.READ_CALL_LOG)
        } returns callGranted
        every {
            permissionChecker.isGranted(TrackingPermissions.READ_PHONE_STATE)
        } returns callGranted
        every {
            permissionChecker.isGranted(TrackingPermissions.READ_SMS)
        } returns smsGranted
        every {
            permissionChecker.isGranted(TrackingPermissions.RECEIVE_SMS)
        } returns smsGranted
        return SettingsViewModel(
            authRepository,
            trackingSettings,
            appSettings,
            relationshipEdgeRepository,
            localAuthSettings,
            localAuthCapabilities,
            deviceGrantManager,
            permissionChecker,
            catchUpScheduler,
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

    // --- Issue #721: tracking toggles are permission-gated --------------------

    @Test
    fun `enabling call tracking with the grant present enables, persists and kicks the catch-up`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = true)
            advanceUntilIdle()

            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()

            assertTrue(vm.uiState.value.callTrackingEnabled)
            coVerify { trackingSettings.setCallTrackingEnabled(true) }
            verify { catchUpScheduler.enqueueCallLogCatchUp() }
            // No system dialog is needed when the grant is already present.
            assertNull(vm.uiState.value.pendingPermissionRequest)
        }

    @Test
    fun `enabling SMS tracking with the grant present enables, persists and kicks the backfill`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(smsGranted = true)
            advanceUntilIdle()

            vm.setSmsTrackingEnabled(true)
            advanceUntilIdle()

            assertTrue(vm.uiState.value.smsTrackingEnabled)
            coVerify { trackingSettings.setSmsTrackingEnabled(true) }
            verify { catchUpScheduler.enqueueSmsBackfill() }
            assertNull(vm.uiState.value.pendingPermissionRequest)
        }

    @Test
    fun `enabling call tracking without the grant requests the permission dialog and stays off`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()

            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()

            // The toggle must not flip on, and the flag must not be persisted,
            // until the OS grant actually lands.
            assertFalse(vm.uiState.value.callTrackingEnabled)
            assertEquals(
                TrackingPermissionRequest.CALL_TRACKING,
                vm.uiState.value.pendingPermissionRequest,
            )
            coVerify(exactly = 0) { trackingSettings.setCallTrackingEnabled(true) }
            verify(exactly = 0) { catchUpScheduler.enqueueCallLogCatchUp() }
        }

    @Test
    fun `enabling SMS tracking without the grant requests the permission dialog and stays off`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(smsGranted = false)
            advanceUntilIdle()

            vm.setSmsTrackingEnabled(true)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.smsTrackingEnabled)
            assertEquals(
                TrackingPermissionRequest.SMS_TRACKING,
                vm.uiState.value.pendingPermissionRequest,
            )
            coVerify(exactly = 0) { trackingSettings.setSmsTrackingEnabled(true) }
        }

    @Test
    fun `a call permission grant result enables tracking and persists the flag`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()
            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()

            // The OS dialog was shown; the user granted everything. The grant is
            // re-read from the real permission state, which is now present.
            every { permissionChecker.isGranted(TrackingPermissions.READ_CALL_LOG) } returns true
            every { permissionChecker.isGranted(TrackingPermissions.READ_PHONE_STATE) } returns true

            vm.onPermissionRequestResult(TrackingPermissionRequest.CALL_TRACKING, permanentlyDenied = false)
            advanceUntilIdle()

            assertTrue(vm.uiState.value.callTrackingEnabled)
            assertNull(vm.uiState.value.pendingPermissionRequest)
            assertNull(vm.uiState.value.permissionDialog)
            coVerify { trackingSettings.setCallTrackingEnabled(true) }
            verify { catchUpScheduler.enqueueCallLogCatchUp() }
        }

    @Test
    fun `a denied call permission result leaves the flag false and raises the rationale dialog`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()
            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()

            vm.onPermissionRequestResult(TrackingPermissionRequest.CALL_TRACKING, permanentlyDenied = false)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.callTrackingEnabled)
            assertEquals(
                TrackingPermissionDialog.Rationale(TrackingPermissionRequest.CALL_TRACKING),
                vm.uiState.value.permissionDialog,
            )
            coVerify(exactly = 0) { trackingSettings.setCallTrackingEnabled(true) }
        }

    @Test
    fun `a permanently denied call permission raises the open-settings dialog`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()
            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()

            vm.onPermissionRequestResult(TrackingPermissionRequest.CALL_TRACKING, permanentlyDenied = true)
            advanceUntilIdle()

            assertEquals(
                TrackingPermissionDialog.AppSettings(TrackingPermissionRequest.CALL_TRACKING),
                vm.uiState.value.permissionDialog,
            )
            assertNull(vm.uiState.value.pendingPermissionRequest)
        }

    @Test
    fun `a denied SMS permission result raises the SMS rationale dialog`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(smsGranted = false)
            advanceUntilIdle()
            vm.setSmsTrackingEnabled(true)
            advanceUntilIdle()

            vm.onPermissionRequestResult(TrackingPermissionRequest.SMS_TRACKING, permanentlyDenied = false)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.smsTrackingEnabled)
            assertEquals(
                TrackingPermissionDialog.Rationale(TrackingPermissionRequest.SMS_TRACKING),
                vm.uiState.value.permissionDialog,
            )
            coVerify(exactly = 0) { trackingSettings.setSmsTrackingEnabled(true) }
        }

    @Test
    fun `a rationale retry re-requests the same permission set`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()
            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()
            vm.onPermissionRequestResult(TrackingPermissionRequest.CALL_TRACKING, permanentlyDenied = false)
            advanceUntilIdle()

            vm.onPermissionDialogRetry()
            advanceUntilIdle()

            assertNull(vm.uiState.value.permissionDialog)
            assertEquals(
                TrackingPermissionRequest.CALL_TRACKING,
                vm.uiState.value.pendingPermissionRequest,
            )
        }

    @Test
    fun `dismissing the permission dialog clears it`() = runTest(mainDispatcherRule.testDispatcher) {
        val vm = viewModel(callGranted = false)
        advanceUntilIdle()
        vm.setCallTrackingEnabled(true)
        advanceUntilIdle()
        vm.onPermissionRequestResult(TrackingPermissionRequest.CALL_TRACKING, permanentlyDenied = true)
        advanceUntilIdle()
        assertTrue(vm.uiState.value.permissionDialog is TrackingPermissionDialog.AppSettings)

        vm.onPermissionDialogDismiss()
        advanceUntilIdle()

        assertNull(vm.uiState.value.permissionDialog)
        assertNull(vm.uiState.value.pendingPermissionRequest)
    }

    @Test
    fun `disabling tracking while a request is pending cancels the request and beats its late result`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()
            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()
            assertEquals(
                TrackingPermissionRequest.CALL_TRACKING,
                vm.uiState.value.pendingPermissionRequest,
            )

            vm.setCallTrackingEnabled(false)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.callTrackingEnabled)
            assertNull(vm.uiState.value.pendingPermissionRequest)
            coVerify { trackingSettings.setCallTrackingEnabled(false) }

            // The OS still delivers the dialog outcome after the cancel (the
            // dialog is modal, the user could not toggle mid-flight — but a
            // late/cancelled flow must not re-enable behind the user's back).
            every { permissionChecker.isGranted(TrackingPermissions.READ_CALL_LOG) } returns true
            every { permissionChecker.isGranted(TrackingPermissions.READ_PHONE_STATE) } returns true
            vm.onPermissionRequestResult(TrackingPermissionRequest.CALL_TRACKING, permanentlyDenied = false)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.callTrackingEnabled)
            coVerify(exactly = 1) { trackingSettings.setCallTrackingEnabled(false) }
            verify(exactly = 0) { catchUpScheduler.enqueueCallLogCatchUp() }
        }

    @Test
    fun `a double enable while a request is pending does not stack a second request`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callGranted = false)
            advanceUntilIdle()

            vm.setCallTrackingEnabled(true)
            vm.setCallTrackingEnabled(true)
            advanceUntilIdle()

            // Exactly one pending request drives exactly one system dialog.
            assertEquals(
                TrackingPermissionRequest.CALL_TRACKING,
                vm.uiState.value.pendingPermissionRequest,
            )
        }

    @Test
    fun `a stored call-tracking flag whose permission was revoked is reverted on refresh`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callStored = true, callGranted = false)
            advanceUntilIdle()

            // The stored flag said "on" but the OS grant is gone (revoked in
            // system settings): the toggle must render off and the flag be
            // persisted off so the capture workers stop being fooled.
            assertFalse(vm.uiState.value.callTrackingEnabled)
            coVerify { trackingSettings.setCallTrackingEnabled(false) }
        }

    @Test
    fun `a stored SMS flag whose permission was revoked is reverted on refresh`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(smsStored = true, smsGranted = false)
            advanceUntilIdle()

            assertFalse(vm.uiState.value.smsTrackingEnabled)
            coVerify { trackingSettings.setSmsTrackingEnabled(false) }
        }

    @Test
    fun `a stored call flag with the grant still present stays on`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val vm = viewModel(callStored = true, callGranted = true)
            advanceUntilIdle()

            assertTrue(vm.uiState.value.callTrackingEnabled)
            coVerify(exactly = 0) { trackingSettings.setCallTrackingEnabled(false) }
        }

    @Test
    fun `refreshPermissionState reconciles after a permission is revoked`() =
        runTest(mainDispatcherRule.testDispatcher) {
            // Start granted + on (the state right after the user enabled it).
            val vm = viewModel(callStored = true, callGranted = true)
            advanceUntilIdle()
            assertTrue(vm.uiState.value.callTrackingEnabled)

            // The permission is revoked while the Settings screen stays open;
            // the resume-driven refresh must flip the toggle off.
            every { permissionChecker.isGranted(TrackingPermissions.READ_CALL_LOG) } returns false
            every { permissionChecker.isGranted(TrackingPermissions.READ_PHONE_STATE) } returns false
            vm.refreshPermissionState()
            advanceUntilIdle()

            assertFalse(vm.uiState.value.callTrackingEnabled)
            coVerify { trackingSettings.setCallTrackingEnabled(false) }
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

    // --- Issue #722: fully biometric login ---

    @Test
    fun `enrolling biometric sign-in calls the device grant manager`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { deviceGrantManager.enroll(any()) } returns Result.success(Unit)
        val vm = viewModel()
        advanceUntilIdle()

        vm.performBiometricEnroll()

        coVerify { deviceGrantManager.enroll(any()) }
        assertFalse(vm.uiState.value.isBiometricBusy)
        assertEquals(null, vm.uiState.value.biometricErrorRes)
    }

    @Test
    fun `a failed enrollment surfaces an error`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { deviceGrantManager.enroll(any()) } returns Result.failure(Exception("network"))
        val vm = viewModel()
        advanceUntilIdle()

        vm.performBiometricEnroll()

        assertEquals(R.string.biometric_enroll_error, vm.uiState.value.biometricErrorRes)
        assertFalse(vm.uiState.value.isBiometricBusy)
    }

    @Test
    fun `removing biometric sign-in calls the device grant manager`() = runTest(mainDispatcherRule.testDispatcher) {
        coEvery { deviceGrantManager.removeEnrollment() } returns Result.success(Unit)
        val vm = viewModel()
        advanceUntilIdle()

        vm.performBiometricRemove()

        coVerify { deviceGrantManager.removeEnrollment() }
        assertFalse(vm.uiState.value.isBiometricBusy)
    }

}
