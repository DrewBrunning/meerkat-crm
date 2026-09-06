package com.mycorrhizal.crm.feature.tracking

import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.PendingInteractionRepository
import com.mycorrhizal.crm.domain.repository.PendingInteraction
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject

/**
 * Reads the call log since the last watermark and stages new calls as
 * PendingInteractions (§6.1). Triggered by PhoneStateReceiver / a one-time
 * request on grant; also run periodically as a catch-up (see
 * [TrackingWorkerScheduler]).
 *
 * Issue #721: the worker now respects the *OS grant* as well as the opt-in
 * flag — READ_CALL_LOG can be revoked in system settings while this worker is
 * enqueued, and a missing grant must be a logged no-op, never a crash. Each
 * staged call is written through [PendingInteractionRepository.recordIfNew]
 * so an overlapping periodic + one-shot run cannot stage the same call twice.
 */
@HiltWorker
class CallLogSyncWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted workerParams: WorkerParameters,
    private val pendingInteractionRepository: PendingInteractionRepository,
    private val contactRepository: ContactRepository,
    private val trackingSettings: TrackingSettingsRepository,
) : CoroutineWorker(appContext, workerParams) {

    override suspend fun doWork(): Result {
        // Respect the opt-in toggle (§8.3).
        if (!trackingSettings.callTrackingEnabled()) return Result.success()

        // Respect the OS grant (§8.3 + issue #721): without READ_CALL_LOG the
        // provider read would throw SecurityException — a missing grant is a
        // no-op, not an error.
        if (!hasCallLogPermission()) {
            Log.w(TAG, "READ_CALL_LOG not granted; call-log sync is a no-op")
            return Result.success()
        }

        val since = trackingSettings.lastCallLogTimestamp()
        val entries = CallLogReader(applicationContext.contentResolver).readSince(since)
        if (entries.isEmpty()) return Result.success()

        var maxTs = since
        entries.forEach { entry ->
            if (entry.timestampMillis > maxTs) maxTs = entry.timestampMillis
            val number = entry.number ?: return@forEach
            val contact = runCatching { contactRepository.findByPhone(number) }.getOrNull()
            val direction = when (entry.type) {
                CallLogKinds.INCOMING -> InteractionCapture.DIR_INCOMING
                CallLogKinds.OUTGOING -> InteractionCapture.DIR_OUTGOING
                CallLogKinds.MISSED -> InteractionCapture.DIR_MISSED
                else -> null
            }
            pendingInteractionRepository.recordIfNew(
                PendingInteraction(
                    timestampMillis = entry.timestampMillis,
                    kind = InteractionCapture.KIND_CALL,
                    direction = direction,
                    phoneNumber = number,
                    matchedContactId = contact?.id,
                ),
            )
        }

        // Advance the watermark past everything we saw so the next run is
        // incremental (new calls only).
        trackingSettings.setLastCallLogTimestamp(maxTs)
        return Result.success()
    }

    private fun hasCallLogPermission(): Boolean =
        ContextCompat.checkSelfPermission(
            applicationContext,
            TrackingPermissions.READ_CALL_LOG,
        ) == PackageManager.PERMISSION_GRANTED

    private companion object {
        const val TAG = "CallLogSyncWorker"
    }
}
