package com.mycorrhizal.crm.feature.tracking

import android.content.ContentResolver
import android.provider.CallLog
import android.util.Log

/** One call-log entry as captured for tracking (§6.1). */
data class CallLogEntry(
    val number: String?,
    val type: Int,
    val timestampMillis: Long,
    val durationSeconds: Long,
    val cachedName: String?,
)

object CallLogKinds {
    const val INCOMING = CallLog.Calls.INCOMING_TYPE
    const val OUTGOING = CallLog.Calls.OUTGOING_TYPE
    const val MISSED = CallLog.Calls.MISSED_TYPE
}

/**
 * Reads recent call-log entries (§6.1). Runs on the caller's thread (the
 * worker dispatches to IO). Dedupe is handled by the caller passing a
 * [sinceMillis] watermark; the reader itself is a pure content-provider read.
 */
class CallLogReader(private val contentResolver: ContentResolver) {

    /**
     * Issue #721: a missing READ_CALL_LOG grant (revoked in system settings
     * while the worker was enqueued, or a direct call in a test) makes
     * [ContentResolver.query] throw [SecurityException]. That is a state of
     * the world to degrade from, never a reason for the worker to crash — the
     * caller already gates on the grant; this catch is the belt-and-braces
     * turn of the reader into a logged no-op.
     */
    fun readSince(sinceMillis: Long, limit: Int = 50): List<CallLogEntry> {
        val out = mutableListOf<CallLogEntry>()
        val projection = arrayOf(
            CallLog.Calls.NUMBER,
            CallLog.Calls.TYPE,
            CallLog.Calls.DATE,
            CallLog.Calls.DURATION,
            CallLog.Calls.CACHED_NAME,
        )
        val cursor = try {
            contentResolver.query(
                CallLog.Calls.CONTENT_URI,
                projection,
                "${CallLog.Calls.DATE} > ?",
                arrayOf(sinceMillis.toString()),
                "${CallLog.Calls.DATE} DESC LIMIT $limit",
            )
        } catch (e: SecurityException) {
            Log.w(TAG, "Call-log read denied (READ_CALL_LOG missing?); treating as empty", e)
            return out
        } ?: return out
        cursor.use {
            val numIdx = it.getColumnIndexOrThrow(CallLog.Calls.NUMBER)
            val typeIdx = it.getColumnIndexOrThrow(CallLog.Calls.TYPE)
            val dateIdx = it.getColumnIndexOrThrow(CallLog.Calls.DATE)
            val durIdx = it.getColumnIndexOrThrow(CallLog.Calls.DURATION)
            val nameIdx = it.getColumnIndex(CallLog.Calls.CACHED_NAME)
            while (it.moveToNext()) {
                out.add(
                    CallLogEntry(
                        number = it.getString(numIdx),
                        type = it.getInt(typeIdx),
                        timestampMillis = it.getLong(dateIdx),
                        durationSeconds = it.getLong(durIdx),
                        cachedName = if (nameIdx >= 0) it.getString(nameIdx) else null,
                    ),
                )
            }
        }
        return out
    }

    private companion object {
        const val TAG = "CallLogReader"
    }
}
