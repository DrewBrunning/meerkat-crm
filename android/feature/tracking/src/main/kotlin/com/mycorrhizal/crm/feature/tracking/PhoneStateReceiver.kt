package com.mycorrhizal.crm.feature.tracking

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.telephony.TelephonyManager

/**
 * Reacts to phone-state changes to stage finished calls (§6.1). Runs the
 * CallLogSyncWorker on every state change: it is idempotent because the
 * worker reads only calls newer than the persisted watermark, so the final
 * IDLE broadcast (call ended) picks up the just-finished call and earlier
 * RINGING/OFFHOOK broadcasts stage nothing new. This avoids relying on
 * cross-broadcast static state, which manifest receivers can't safely keep.
 */
class PhoneStateReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != TelephonyManager.ACTION_PHONE_STATE_CHANGED) return
        TrackingWorkerScheduler.enqueueCallLogCatchUp(context)
    }

    companion object {
        /** Kept as an alias for callers/tests; the name lives in TrackingWorkerScheduler. */
        const val UNIQUE_CALL_LOG_SYNC = TrackingWorkerScheduler.UNIQUE_CALL_LOG_SYNC
    }
}
