package com.mycorrhizal.crm.feature.tracking

import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.PendingInteraction
import com.mycorrhizal.crm.domain.repository.PendingInteractionRepository
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject

/**
 * Reads the *sent* SMS folder since the last watermark and stages outgoing
 * texts as PendingInteractions (§6.2, issue #721). Incoming texts are captured
 * instantly by SmsReceiver; the backfill exists because nothing observes
 * outgoing texts except a periodic read of the Sent folder. It runs on a
 * periodic cadence (see [TrackingWorkerScheduler]) and once immediately after
 * the SMS-tracking permission is granted, so the first run also stages recent
 * pre-grant outgoing history rather than waiting for the next poll.
 *
 * Issue #721 hardening: gated on both the opt-in flag AND the READ_SMS OS
 * grant (revoked mid-enqueue → logged no-op, never a crash), and every row is
 * written through [PendingInteractionRepository.recordIfNew] so an overlapping
 * periodic + one-shot run cannot stage the same text twice.
 */
@HiltWorker
class SmsBackfillWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted workerParams: WorkerParameters,
    private val pendingInteractionRepository: PendingInteractionRepository,
    private val contactRepository: ContactRepository,
    private val trackingSettings: TrackingSettingsRepository,
) : CoroutineWorker(appContext, workerParams) {

    override suspend fun doWork(): Result {
        if (!trackingSettings.smsTrackingEnabled()) return Result.success()

        if (!hasSmsPermission()) {
            Log.w(TAG, "READ_SMS not granted; SMS backfill is a no-op")
            return Result.success()
        }

        val since = trackingSettings.lastSmsTimestamp()
        val entries = SmsHistoryReader(applicationContext.contentResolver).readSentSince(since)
        if (entries.isEmpty()) return Result.success()

        var maxTs = since
        entries.forEach { entry ->
            if (entry.timestampMillis > maxTs) maxTs = entry.timestampMillis
            val number = entry.address ?: return@forEach
            val contact = runCatching { contactRepository.findByPhone(number) }.getOrNull()
            pendingInteractionRepository.recordIfNew(
                PendingInteraction(
                    timestampMillis = entry.timestampMillis,
                    kind = InteractionCapture.KIND_MESSAGE,
                    direction = InteractionCapture.DIR_OUTGOING,
                    phoneNumber = number,
                    matchedContactId = contact?.id,
                ),
            )
        }

        // Advance the watermark past everything we saw so the next run is
        // incremental (new outgoing texts only).
        trackingSettings.setLastSmsTimestamp(maxTs)
        return Result.success()
    }

    private fun hasSmsPermission(): Boolean =
        ContextCompat.checkSelfPermission(
            applicationContext,
            TrackingPermissions.READ_SMS,
        ) == PackageManager.PERMISSION_GRANTED

    private companion object {
        const val TAG = "SmsBackfillWorker"
    }
}
