package com.mycorrhizal.crm.feature.tracking

import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.telephony.PhoneStateListener
import android.telephony.TelephonyManager
import androidx.core.content.ContextCompat
import com.mycorrhizal.crm.domain.repository.ActivityRepository
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.ui.R
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/**
 * Foreground service that shows the quick-capture overlay when a call ends
 * (§6.5/§6.7). Runs only while call tracking is enabled; the overlay appears
 * for a short window after a call, pre-filling the activity form with the
 * called contact so the interaction can be logged without leaving the call
 * screen. Self-stops after 5 minutes of no call activity. The call-log
 * staging itself is handled by PhoneStateReceiver + CallLogSyncWorker; this
 * service is purely the opt-in overlay UX.
 */
@AndroidEntryPoint
class CallDetectionService : Service() {

    private val handler = Handler(Looper.getMainLooper())

    @Inject
    lateinit var contactRepository: ContactRepository

    @Inject
    lateinit var activityRepository: ActivityRepository

    // Owned by this service instance (not a static/singleton holder) so the
    // View it retains while shown is scoped to the service's lifetime;
    // dismiss()'d and dropped in onDestroy. internal (not private) so
    // CallDetectionServiceTest can assert the granted/denied permission
    // branches actually differ, not just that neither throws.
    internal var quickCaptureOverlay: QuickCaptureOverlay? = null

    private val phoneStateListener = object : PhoneStateListener() {
        override fun onCallStateChanged(state: Int, phoneNumber: String?) =
            handleCallStateChanged(state, phoneNumber)
    }

    // Extracted from phoneStateListener (issue #320 Phase C) so the call-idle
    // -> show-overlay decision is directly testable -- Robolectric has no
    // shadow for simulating a real TelephonyManager/PhoneStateListener
    // callback, so this was otherwise only reachable via reflection.
    internal fun handleCallStateChanged(state: Int, phoneNumber: String?) {
        if (state == TelephonyManager.CALL_STATE_IDLE) {
            val hasOverlayPermission = ContextCompat.checkSelfPermission(
                this@CallDetectionService,
                android.Manifest.permission.SYSTEM_ALERT_WINDOW,
            ) == PackageManager.PERMISSION_GRANTED
            if (hasOverlayPermission) {
                (quickCaptureOverlay ?: QuickCaptureOverlay(
                    contactRepository = contactRepository,
                    activityRepository = activityRepository,
                ).also { quickCaptureOverlay = it })
                    .show(this@CallDetectionService, phoneNumber)
            }
            resetSelfStop()
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(
            NOTIFICATION_ID,
            NotificationBuilder.cadence(
                applicationContext,
                applicationContext.getString(R.string.app_name),
                applicationContext.getString(R.string.call_service_notification_text),
            ),
        )
        val telephonyManager = getSystemService(TelephonyManager::class.java)
        // Issue #721: if READ_PHONE_STATE was revoked while the service was
        // running and the system then restarts it (START_STICKY), listen()
        // throws SecurityException — a missing grant is a logged no-op (the
        // call-log capture keeps working via its own grant check), never a
        // crash loop.
        try {
            telephonyManager.listen(phoneStateListener, PhoneStateListener.LISTEN_CALL_STATE)
        } catch (e: SecurityException) {
            android.util.Log.w(TAG, "READ_PHONE_STATE missing; phone-state listening disabled", e)
        }
        resetSelfStop()
        return START_STICKY
    }

    private fun resetSelfStop() {
        handler.removeCallbacksAndMessages(null)
        handler.postDelayed({ stopSelf() }, SELF_STOP_MS)
    }

    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        quickCaptureOverlay?.dismiss()
        quickCaptureOverlay = null
        val telephonyManager = getSystemService(TelephonyManager::class.java)
        try {
            telephonyManager.listen(phoneStateListener, PhoneStateListener.LISTEN_NONE)
        } catch (e: SecurityException) {
            android.util.Log.w(TAG, "READ_PHONE_STATE missing while unregistering listener", e)
        }
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        const val NOTIFICATION_ID = 1001
        private const val SELF_STOP_MS = 5 * 60 * 1000L
        private const val TAG = "CallDetectionService"
    }
}
