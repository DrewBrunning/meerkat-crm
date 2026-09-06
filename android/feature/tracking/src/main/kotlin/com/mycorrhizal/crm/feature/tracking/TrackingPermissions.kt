package com.mycorrhizal.crm.feature.tracking

import android.Manifest

/**
 * Issue #721: the dangerous OS permissions each opt-in tracking toggle needs.
 *
 * These are the single source of truth for the permission sets — the Settings
 * screen requests exactly one of these arrays when a toggle is turned on, and
 * the capture workers gate on the same grants before touching a provider. The
 * distribution constraint (F-Droid/Obtainium only, no Play policy) lets the
 * app use READ_SMS/READ_CALL_LOG freely; a Play-friendly build would be a
 * separate, feature-stripped flavor and must not change these arrays.
 */
object TrackingPermissions {

    const val READ_CALL_LOG = Manifest.permission.READ_CALL_LOG
    const val READ_PHONE_STATE = Manifest.permission.READ_PHONE_STATE
    const val READ_SMS = Manifest.permission.READ_SMS
    const val RECEIVE_SMS = Manifest.permission.RECEIVE_SMS

    /**
     * "Log calls as activities": READ_CALL_LOG reads the call-log provider,
     * READ_PHONE_STATE is what the system requires before PHONE_STATE broadcasts
     * are delivered and CallDetectionService may register a PhoneStateListener.
     */
    val CALL_TRACKING: Array<String> = arrayOf(READ_CALL_LOG, READ_PHONE_STATE)

    /**
     * "Log messages as activities": READ_SMS reads the SMS provider for the
     * outgoing-text backfill (the broadcast path cannot observe texts the user
     * sends), RECEIVE_SMS is what the system requires before SMS_RECEIVED
     * broadcasts are delivered to SmsReceiver.
     */
    val SMS_TRACKING: Array<String> = arrayOf(READ_SMS, RECEIVE_SMS)
}
