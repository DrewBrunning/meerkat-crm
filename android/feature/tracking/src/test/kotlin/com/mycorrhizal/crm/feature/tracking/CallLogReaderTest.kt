package com.mycorrhizal.crm.feature.tracking

import android.content.ContentResolver
import android.database.MatrixCursor
import android.provider.CallLog
import io.mockk.every
import io.mockk.mockk
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class CallLogReaderTest {

    private val contentResolver = mockk<ContentResolver>()
    private val reader = CallLogReader(contentResolver)

    private val fullProjection = arrayOf(
        CallLog.Calls.NUMBER,
        CallLog.Calls.TYPE,
        CallLog.Calls.DATE,
        CallLog.Calls.DURATION,
        CallLog.Calls.CACHED_NAME,
    )

    @Test
    fun `readSince maps cursor rows to CallLogEntry`() {
        val cursor = MatrixCursor(fullProjection).apply {
            addRow(arrayOf<Any>("+15551234567", CallLog.Calls.INCOMING_TYPE, 1_000L, 42L, "Jane"))
        }
        every {
            contentResolver.query(CallLog.Calls.CONTENT_URI, fullProjection, any(), any(), any())
        } returns cursor

        val entries = reader.readSince(sinceMillis = 0L)

        assertEquals(1, entries.size)
        val entry = entries.single()
        assertEquals("+15551234567", entry.number)
        assertEquals(CallLog.Calls.INCOMING_TYPE, entry.type)
        assertEquals(1_000L, entry.timestampMillis)
        assertEquals(42L, entry.durationSeconds)
        assertEquals("Jane", entry.cachedName)
    }

    @Test
    fun `a missing CACHED_NAME column maps to a null name`() {
        val projectionWithoutName = arrayOf(
            CallLog.Calls.NUMBER,
            CallLog.Calls.TYPE,
            CallLog.Calls.DATE,
            CallLog.Calls.DURATION,
        )
        val cursor = MatrixCursor(projectionWithoutName).apply {
            addRow(arrayOf<Any>("+15551234567", CallLog.Calls.MISSED_TYPE, 2_000L, 0L))
        }
        every {
            contentResolver.query(CallLog.Calls.CONTENT_URI, fullProjection, any(), any(), any())
        } returns cursor

        val entries = reader.readSince(sinceMillis = 0L)

        assertNull(entries.single().cachedName)
    }

    @Test
    fun `a null cursor from the provider is an empty list`() {
        every {
            contentResolver.query(CallLog.Calls.CONTENT_URI, fullProjection, any(), any(), any())
        } returns null

        val entries = reader.readSince(sinceMillis = 0L)

        assertTrue(entries.isEmpty())
    }

    @Test
    fun `a missing READ_CALL_LOG grant (SecurityException) is an empty list, not a crash`() {
        every {
            contentResolver.query(CallLog.Calls.CONTENT_URI, fullProjection, any(), any(), any())
        } throws SecurityException("Permission Denial: reading com.android.providers.contacts")

        val entries = reader.readSince(sinceMillis = 0L)

        assertTrue(entries.isEmpty())
    }
}
