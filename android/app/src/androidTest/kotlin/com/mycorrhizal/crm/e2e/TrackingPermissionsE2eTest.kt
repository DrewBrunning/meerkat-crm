package com.mycorrhizal.crm.e2e

import android.os.ParcelFileDescriptor.AutoCloseInputStream
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
 * OS runtime permissions. The grant/deny state machine is covered by unit tests
 * (`SettingsViewModelTest`); this instrumented test proves the GRANT wiring end
 * to end on the real app against the real backend harness: the Settings toggle
 * flip — through the real ViewModel, the real permission check, the real
 * DataStore flag and the real WorkManager scheduler — actually persists the
 * opt-in and kicks the grant-time catch-up worker.
 *
 * Permissions are granted through UiAutomation rather than driving the system
 * permission dialog (the dialog's button text is device/locale-bound and would
 * make this suite flaky).
 *
 * WHY THERE IS NO INSTRUMENTED REVOKE TEST: revoking a runtime permission the
 * app currently holds makes Android force-stop the app process (verified on
 * API 35/37: `pm revoke` against a live app SIGKILLs it, while `pm grant` does
 * not restart it). Instrumentation shares that process, so an in-process revoke
 * always ends the run as "Process crashed" — no test can revoke and keep
 * running. The revoke/reconcile paths are therefore pinned at the unit level
 * instead: `SettingsViewModelTest` "a stored call-tracking flag whose permission
 * was revoked is reverted on refresh", "a stored SMS flag whose permission was
 * revoked is reverted on refresh" and "refreshPermissionState reconciles after
 * a permission is revoked". Leftover grants across tests are harmless: every
 * test grants exactly what it needs (grant is idempotent and never restarts the
 * process) and the DataStore opt-in flags are reset in @Before/@After.
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

    private fun grant(permission: String) {
        // UiAutomation.grantRuntimePermission(String, String) is API 28+; this
        // suite also runs on the API 26 minSdk floor (android-tests.yml's
        // android-e2e-min-sdk job), so grant via `pm grant` over the shell
        // (UiAutomation.executeShellCommand is API 21+). Reading the output to
        // EOF blocks until the command has finished.
        AutoCloseInputStream(
            uiAutomation.executeShellCommand("pm grant $packageName $permission"),
        ).use { it.readBytes() }
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
        // Grants-only baseline: grants are idempotent and never restart the
        // process (see the class doc). Revokes are deliberately absent — a
        // revoke of a held permission force-stops the app and kills this
        // instrumentation, so the revoke/reconcile scenarios live in
        // SettingsViewModelTest instead.
        runBlocking {
            trackingSettings.setCallTrackingEnabled(false)
            trackingSettings.setSmsTrackingEnabled(false)
        }
    }

    @After
    fun permissionTearDown() {
        runBlocking {
            trackingSettings.setCallTrackingEnabled(false)
            trackingSettings.setSmsTrackingEnabled(false)
        }
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
}
