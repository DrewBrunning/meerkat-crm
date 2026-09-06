package com.mycorrhizal.crm.feature.tracking

import android.content.Context
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject

/**
 * Issue #721: the one-time capture catch-ups the Settings screen kicks when a
 * tracking toggle is enabled on a fresh permission grant (scope #5 — don't
 * wait for the periodic cadence or the next call/text). Seamed so the
 * Settings ViewModel — which lives in a feature module whose unit tests run
 * without WorkManager — can verify the enqueue without touching the real
 * scheduler.
 */
interface TrackingCatchUpScheduler {
    /** Runs CallLogSyncWorker once now (recent calls since the last watermark). */
    fun enqueueCallLogCatchUp()

    /** Runs SmsBackfillWorker once now (recent outgoing texts since the last watermark). */
    fun enqueueSmsBackfill()
}

class TrackingCatchUpSchedulerImpl @Inject constructor(
    @ApplicationContext private val context: Context,
) : TrackingCatchUpScheduler {
    override fun enqueueCallLogCatchUp() =
        TrackingWorkerScheduler.enqueueCallLogCatchUp(context)

    override fun enqueueSmsBackfill() =
        TrackingWorkerScheduler.enqueueSmsBackfill(context)
}
