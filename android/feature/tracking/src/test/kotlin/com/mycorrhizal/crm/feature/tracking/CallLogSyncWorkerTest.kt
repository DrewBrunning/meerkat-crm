package com.mycorrhizal.crm.feature.tracking

import android.app.Application
import android.content.Context
import android.provider.CallLog
import androidx.test.core.app.ApplicationProvider
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.PendingInteraction
import com.mycorrhizal.crm.domain.repository.PendingInteractionRepository
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.model.network.ContactSummary
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows
import org.robolectric.annotation.Config
import org.robolectric.fakes.RoboCursor

/**
 * CallLogSyncWorker builds its own CallLogReader(applicationContext.contentResolver)
 * internally rather than taking one injected, so its input is controlled via
 * ShadowContentResolver.setCursor against the real applicationContext's
 * resolver -- not by mocking ContentResolver directly (see CallLogReaderTest
 * for that simpler pattern, which only works because CallLogReader there is
 * constructed directly with a mocked resolver).
 *
 * Issue #721: the worker now also gates on the READ_CALL_LOG OS grant (granted
 * by default in setup; the denial path is its own test), so every test that
 * expects the provider to be read must grant it first.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = Application::class)
class CallLogSyncWorkerTest {

    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Before
    fun grantCallLogPermission() {
        Shadows.shadowOf(context as Application)
            .grantPermissions(android.Manifest.permission.READ_CALL_LOG)
    }

    private fun stubCallLog(rows: List<Array<Any?>>) {
        val cursor = RoboCursor().apply {
            setColumnNames(
                listOf(CallLog.Calls.NUMBER, CallLog.Calls.TYPE, CallLog.Calls.DATE, CallLog.Calls.DURATION, CallLog.Calls.CACHED_NAME),
            )
            setResults(rows.toTypedArray())
        }
        Shadows.shadowOf(context.contentResolver).setCursor(CallLog.Calls.CONTENT_URI, cursor)
    }

    @Test
    fun `a disabled call-tracking opt-in short-circuits without querying the call log`() = runTest {
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>()
        coEvery { settings.callTrackingEnabled() } returns false

        val worker = CallLogSyncWorker(
            appContext = context,
            workerParams = mockk(relaxed = true),
            pendingInteractionRepository = pendingInteractions,
            contactRepository = contacts,
            trackingSettings = settings,
        )
        worker.doWork()

        coVerify(exactly = 0) { pendingInteractions.recordIfNew(any()) }
    }

    @Test
    fun `a missing READ_CALL_LOG grant is a no-op success, not a worker failure`() = runTest {
        Shadows.shadowOf(context as Application).denyPermissions(android.Manifest.permission.READ_CALL_LOG)
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>()
        coEvery { settings.callTrackingEnabled() } returns true
        coEvery { settings.lastCallLogTimestamp() } returns 500L

        val worker = CallLogSyncWorker(
            appContext = context,
            workerParams = mockk(relaxed = true),
            pendingInteractionRepository = pendingInteractions,
            contactRepository = contacts,
            trackingSettings = settings,
        )
        val result = worker.doWork()

        // Missing grant = logged no-op, never a crash/retry (issue #721).
        assertTrue(result is androidx.work.ListenableWorker.Result.Success)
        coVerify(exactly = 0) { pendingInteractions.recordIfNew(any()) }
        coVerify(exactly = 0) { settings.setLastCallLogTimestamp(any()) }
    }

    @Test
    fun `an empty call log is a no-op and does not advance the watermark`() = runTest {
        stubCallLog(emptyList())
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>()
        coEvery { settings.callTrackingEnabled() } returns true
        coEvery { settings.lastCallLogTimestamp() } returns 500L

        val worker = CallLogSyncWorker(
            appContext = context,
            workerParams = mockk(relaxed = true),
            pendingInteractionRepository = pendingInteractions,
            contactRepository = contacts,
            trackingSettings = settings,
        )
        worker.doWork()

        coVerify(exactly = 0) { settings.setLastCallLogTimestamp(any()) }
    }

    @Test
    fun `records a matched incoming call and advances the watermark to the newest entry`() = runTest {
        stubCallLog(
            listOf(
                arrayOf<Any?>("+15551234567", CallLog.Calls.INCOMING_TYPE, 5000L, 30L, "Jane"),
                arrayOf<Any?>("+15559876543", CallLog.Calls.OUTGOING_TYPE, 7000L, 60L, null),
            ),
        )
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>()
        coEvery { contacts.findByPhone("+15551234567") } returns ContactSummary(id = 9)
        coEvery { contacts.findByPhone("+15559876543") } returns null
        val settings = mockk<TrackingSettingsRepository>(relaxed = true)
        coEvery { settings.callTrackingEnabled() } returns true
        coEvery { settings.lastCallLogTimestamp() } returns 0L

        val worker = CallLogSyncWorker(
            appContext = context,
            workerParams = mockk(relaxed = true),
            pendingInteractionRepository = pendingInteractions,
            contactRepository = contacts,
            trackingSettings = settings,
        )
        val result = worker.doWork()

        assertTrue(result is androidx.work.ListenableWorker.Result.Success)
        coVerify {
            pendingInteractions.recordIfNew(
                PendingInteraction(
                    timestampMillis = 5000L,
                    kind = InteractionCapture.KIND_CALL,
                    direction = InteractionCapture.DIR_INCOMING,
                    phoneNumber = "+15551234567",
                    matchedContactId = 9,
                ),
            )
        }
        coVerify {
            pendingInteractions.recordIfNew(
                PendingInteraction(
                    timestampMillis = 7000L,
                    kind = InteractionCapture.KIND_CALL,
                    direction = InteractionCapture.DIR_OUTGOING,
                    phoneNumber = "+15559876543",
                    matchedContactId = null,
                ),
            )
        }
        coVerify { settings.setLastCallLogTimestamp(7000L) }
    }

    @Test
    fun `a missed call maps to the missed direction`() = runTest {
        stubCallLog(listOf(arrayOf<Any?>("+15551234567", CallLog.Calls.MISSED_TYPE, 1000L, 0L, null)))
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        coEvery { contacts.findByPhone(any()) } returns null
        val settings = mockk<TrackingSettingsRepository>(relaxed = true)
        coEvery { settings.callTrackingEnabled() } returns true
        coEvery { settings.lastCallLogTimestamp() } returns 0L

        val worker = CallLogSyncWorker(
            appContext = context,
            workerParams = mockk(relaxed = true),
            pendingInteractionRepository = pendingInteractions,
            contactRepository = contacts,
            trackingSettings = settings,
        )
        worker.doWork()

        coVerify {
            pendingInteractions.recordIfNew(
                PendingInteraction(
                    timestampMillis = 1000L,
                    kind = InteractionCapture.KIND_CALL,
                    direction = InteractionCapture.DIR_MISSED,
                    phoneNumber = "+15551234567",
                    matchedContactId = null,
                ),
            )
        }
    }
}
