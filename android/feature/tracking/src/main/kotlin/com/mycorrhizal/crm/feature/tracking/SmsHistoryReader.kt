package com.mycorrhizal.crm.feature.tracking

import android.content.ContentResolver
import android.provider.Telephony
import android.util.Log

/** One outgoing-SMS entry as captured for tracking (§6.2). */
data class SmsHistoryEntry(
    val address: String?,
    val timestampMillis: Long,
)

/**
 * Reads the *sent* SMS folder (§6.2, issue #721). The incoming-SMS broadcast
 * (SmsReceiver) cannot observe texts the user sends — there is no outbound
 * broadcast another app can register for — so outgoing texts are captured by
 * periodically reading the Sent folder and are gated on the READ_SMS grant.
 *
 * The reader deliberately never touches the Inbox: incoming SMS are owned by
 * the broadcast path, and the broadcast + provider timestamp a message
 * differently (PDU header vs provider row date) while the outbox deletes rows
 * once synced — so no watermark or exact-match dedupe can reliably stop a
 * provider Inbox read from double-logging a message the broadcast already
 * captured. Keeping one writer per folder makes duplicates impossible.
 *
 * Only the sender address + timestamp are kept — never the message body
 * (§6.2 privacy boundary).
 */
class SmsHistoryReader(private val contentResolver: ContentResolver) {

    fun readSentSince(sinceMillis: Long, limit: Int = 50): List<SmsHistoryEntry> {
        val out = mutableListOf<SmsHistoryEntry>()
        val projection = arrayOf(
            Telephony.Sms.ADDRESS,
            Telephony.Sms.DATE,
        )
        val cursor = try {
            contentResolver.query(
                Telephony.Sms.Sent.CONTENT_URI,
                projection,
                "${Telephony.Sms.DATE} > ?",
                arrayOf(sinceMillis.toString()),
                "${Telephony.Sms.DATE} DESC LIMIT $limit",
            )
        } catch (e: SecurityException) {
            // Issue #721: a missing READ_SMS grant is a logged no-op, never a crash.
            Log.w(TAG, "SMS-history read denied (READ_SMS missing?); treating as empty", e)
            return out
        } ?: return out
        cursor.use {
            val addressIdx = it.getColumnIndex(Telephony.Sms.ADDRESS)
            val dateIdx = it.getColumnIndexOrThrow(Telephony.Sms.DATE)
            while (it.moveToNext()) {
                out.add(
                    SmsHistoryEntry(
                        address = if (addressIdx >= 0) it.getString(addressIdx) else null,
                        timestampMillis = it.getLong(dateIdx),
                    ),
                )
            }
        }
        return out
    }

    private companion object {
        const val TAG = "SmsHistoryReader"
    }
}
