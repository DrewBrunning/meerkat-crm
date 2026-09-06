package com.mycorrhizal.crm.e2e

import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.WorkManager
import com.mycorrhizal.crm.MainActivity
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.feature.tracking.TrackingPermissions
import com.mycorrhizal.crm.feature.tracking.TrackingWorkerScheduler
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Issue #721: the call/SMS tracking toggles must now request (and respect) the
 * OS runtime permissions. The grant/deny state machine itself is covered by
 * unit tests; this instrumented test proves the wiring end to end on the real
 * app against the real backend harness: the Settings toggle flip — through the
 * real ViewModel, the real permission check, the real DataStore flag and the
 * real WorkManager scheduler — actually persists the opt-in and kicks the
 * grant-time catch-up worker, and a system-settings revoke is reflected the
 * next time the screen reconciles.
 *
 * Permissions are granted/revoked through UiAutomation rather than driving the
 * system permission dialog (the dialog's button text is device/locale-bound and
 * would make this suite flaky); the toggle -> enable decision itself is
 * exercised by the unit suite for both the grant and denial outcomes.
 */
@RunWith(AndroidJUnit4::class)
class TrackingPermissionsE2eTest : E2eBaseTest() {

    private val trackingSettings: TrackingSettingsRepository
        get() = (compose.activity as MainActivity).trackingSettings

    private val appContext
        get() = compose.activity.applicationContext

    private val packageName
        get() = compose.activity.packageName

    private val uiAutomation
        get() = InstrumentationRegistry.getInstrumentation().uiAutomation

    private fun grant(permission: String) =
        uiAutomation.grantRuntimePermission(packageName, permission)

    private fun revoke(permission: String) {
        // revokeRuntimePermission throws if the permission was never granted.
        runCatching { uiAutomation.revokeRuntimePermission(packageName, permission) }
    }

    private fun assertCallTracking(expect: Boolean) {
        compose.waitUntil(30_000) {
            runBlocking { trackingSettings.callTrackingEnabled() } == expect
        }
    }

    private fun assertSmsTracking(expect: Boolean) {
        compose.waitUntil(30_000) {
            runBlocking { trackingSettings.smsTrackingEnabled() } == expect
        }
    }

    private fun toggleCallTrackingOnce() {
        waitForText("Log calls as activities")
        compose.onNodeWithText("Log calls as activities")
            .performScrollTo()
            .performClick()
    }

    private fun waitForWork(name: String) {
        compose.waitUntil(30_000) {
            runBlocking {
                WorkManager.getInstance(appContext)
                    .getWorkInfosForUniqueWork(name)
                    .get()
            }.isNotEmpty()
        }
    }

    @Before
    fun permissionSetup() {
        runBlocking {
            trackingSettings.setCallTrackingEnabled(false)
            trackingSettings.setSmsTrackingEnabled(false)
        }
        TrackingPermissions.CALL_TRACKING.forEach { revoke(it) }
        TrackingPermissions.SMS_TRACKING.forEach { revoke(it) }
    }

    @After
    fun permissionTearDown() {
        runBlocking {
            trackingSettings.setCallTrackingEnabled(false)
            trackingSettings.setSmsTrackingEnabled(false)
        }
        TrackingPermissions.CALL_TRACKING.forEach { revoke(it) }
        TrackingPermissions.SMS_TRACKING.forEach { revoke(it) }
    }

    @Test
    fun grantedCallTrackingThroughSettings_persistsAndKicksTheImmediateCatchUp() {
        TrackingPermissions.CALL_TRACKING.forEach { grant(it) }
        navigateViaDrawer("Settings")

        toggleCallTrackingOnce()
        assertCallTracking(true)

        // The grant-time catch-up one-shot is enqueued (issue #721 scope #5).
        waitForWork(TrackingWorkerScheduler.UNIQUE_CALL_LOG_SYNC)

        // Toggle back off through the same UI.
        toggleCallTrackingOnce()
        assertCallTracking(false)
    }

    @Test
    fun grantedSmsTrackingThroughSettings_persistsAndKicksTheImmediateBackfill() {
        TrackingPermissions.SMS_TRACKING.forEach { grant(it) }
        navigateViaDrawer("Settings")

        waitForText("Log messages as activities")
        compose.onNodeWithText("Log messages as activities")
            .performScrollTo()
            .performClick()
        assertSmsTracking(true)

        waitForWork(TrackingWorkerScheduler.UNIQUE_SMS_BACKFILL_ONCE)

        compose.onNodeWithText("Log messages as activities")
            .performScrollTo()
            .performClick()
        assertSmsTracking(false)
    }

    @Test
    fun aRevokedCallPermission_isReflectedAsOffAfterReconciliation() {
        TrackingPermissions.CALL_TRACKING.forEach { grant(it) }
        navigateViaDrawer("Settings")
        toggleCallTrackingOnce()
        assertCallTracking(true)

        // Revoke READ_CALL_LOG in system settings (as the user would); the
        // stored "on" flag must not be allowed to lie.
        revoke(TrackingPermissions.READ_CALL_LOG)
        revoke(TrackingPermissions.READ_PHONE_STATE)

        // Leave and re-enter Settings so the screen reconciles against the OS
        // grant state (fresh ViewModel / resume refresh both do this).
        navigateViaDrawer("Dashboard")
        waitForText("Dashboard")
        navigateViaDrawer("Settings")
        waitForText("Log calls as activities")
        assertCallTracking(false)
    }
}
