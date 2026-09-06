package com.mycorrhizal.crm.feature.tracking

import android.app.Application
import android.content.Context
import android.provider.Telephony
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
 * SmsBackfillWorker builds its own SmsHistoryReader(applicationContext.contentResolver)
 * internally, so its input is controlled via ShadowContentResolver.setCursor
 * against the real applicationContext's resolver (the CallLogSyncWorkerTest
 * pattern). Issue #721: the worker gates on the READ_SMS OS grant, granted by
 * default in setup; the denial path is its own test.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = Application::class)
class SmsBackfillWorkerTest {

    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Before
    fun grantSmsPermission() {
        Shadows.shadowOf(context as Application)
            .grantPermissions(android.Manifest.permission.READ_SMS)
    }

    private fun stubSentFolder(rows: List<Array<Any?>>) {
        val cursor = RoboCursor().apply {
            setColumnNames(listOf(Telephony.Sms.ADDRESS, Telephony.Sms.DATE))
            setResults(rows.toTypedArray())
        }
        Shadows.shadowOf(context.contentResolver).setCursor(Telephony.Sms.Sent.CONTENT_URI, cursor)
    }

    private fun worker(
        pendingInteractions: PendingInteractionRepository,
        contacts: ContactRepository,
        settings: TrackingSettingsRepository,
    ) = SmsBackfillWorker(
        appContext = context,
        workerParams = mockk(relaxed = true),
        pendingInteractionRepository = pendingInteractions,
        contactRepository = contacts,
        trackingSettings = settings,
    )

    @Test
    fun `a disabled SMS opt-in short-circuits without reading the provider`() = runTest {
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>()
        coEvery { settings.smsTrackingEnabled() } returns false

        val result = worker(pendingInteractions, contacts, settings).doWork()

        assertTrue(result is androidx.work.ListenableWorker.Result.Success)
        coVerify(exactly = 0) { pendingInteractions.recordIfNew(any()) }
    }

    @Test
    fun `a missing READ_SMS grant is a no-op success, not a worker failure`() = runTest {
        Shadows.shadowOf(context as Application).denyPermissions(android.Manifest.permission.READ_SMS)
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>()
        coEvery { settings.smsTrackingEnabled() } returns true
        coEvery { settings.lastSmsTimestamp() } returns 500L

        val result = worker(pendingInteractions, contacts, settings).doWork()

        assertTrue(result is androidx.work.ListenableWorker.Result.Success)
        coVerify(exactly = 0) { pendingInteractions.recordIfNew(any()) }
        coVerify(exactly = 0) { settings.setLastSmsTimestamp(any()) }
    }

    @Test
    fun `an empty Sent folder is a no-op and does not advance the watermark`() = runTest {
        stubSentFolder(emptyList())
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>()
        coEvery { settings.smsTrackingEnabled() } returns true
        coEvery { settings.lastSmsTimestamp() } returns 500L

        worker(pendingInteractions, contacts, settings).doWork()

        coVerify(exactly = 0) { settings.setLastSmsTimestamp(any()) }
    }

    @Test
    fun `records a matched outgoing text and advances the watermark to the newest entry`() = runTest {
        stubSentFolder(
            listOf(
                arrayOf<Any?>("+15551234567", 6000L),
                arrayOf<Any?>("+15559876543", 8000L),
            ),
        )
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>()
        coEvery { contacts.findByPhone("+15551234567") } returns ContactSummary(id = 11)
        coEvery { contacts.findByPhone("+15559876543") } returns null
        val settings = mockk<TrackingSettingsRepository>(relaxed = true)
        coEvery { settings.smsTrackingEnabled() } returns true
        coEvery { settings.lastSmsTimestamp() } returns 0L

        val result = worker(pendingInteractions, contacts, settings).doWork()

        assertTrue(result is androidx.work.ListenableWorker.Result.Success)
        coVerify {
            pendingInteractions.recordIfNew(
                PendingInteraction(
                    timestampMillis = 6000L,
                    kind = InteractionCapture.KIND_MESSAGE,
                    direction = InteractionCapture.DIR_OUTGOING,
                    phoneNumber = "+15551234567",
                    matchedContactId = 11,
                ),
            )
        }
        coVerify {
            pendingInteractions.recordIfNew(
                PendingInteraction(
                    timestampMillis = 8000L,
                    kind = InteractionCapture.KIND_MESSAGE,
                    direction = InteractionCapture.DIR_OUTGOING,
                    phoneNumber = "+15559876543",
                    matchedContactId = null,
                ),
            )
        }
        coVerify { settings.setLastSmsTimestamp(8000L) }
    }

    @Test
    fun `a null-address row is skipped`() = runTest {
        stubSentFolder(listOf(arrayOf<Any?>(null, 3000L)))
        val pendingInteractions = mockk<PendingInteractionRepository>(relaxed = true)
        val contacts = mockk<ContactRepository>(relaxed = true)
        val settings = mockk<TrackingSettingsRepository>(relaxed = true)
        coEvery { settings.smsTrackingEnabled() } returns true
        coEvery { settings.lastSmsTimestamp() } returns 0L

        worker(pendingInteractions, contacts, settings).doWork()

        coVerify(exactly = 0) { pendingInteractions.recordIfNew(any()) }
    }
}
