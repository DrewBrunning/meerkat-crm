package com.mycorrhizal.crm.data.repository

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class TrackingSettingsRepositoryImplTest {

    private lateinit var repository: TrackingSettingsRepositoryImpl

    @Before
    fun setup() = kotlinx.coroutines.runBlocking {
        val context = ApplicationProvider.getApplicationContext<Context>()
        repository = TrackingSettingsRepositoryImpl(context)
        // DataStore's `by preferencesDataStore(...)` delegate keeps its own in-memory cache once
        // warm, and Robolectric reuses that warm instance across test methods in the same class --
        // deleting the backing file directly does NOT invalidate it (DataStore doesn't watch the
        // filesystem for external changes). Reset must go through the real write API instead, so
        // it updates the same in-memory state subsequent reads see. There's no single clear-all on
        // the interface (by design -- see TrackingSettingsRepository), so reset each key.
        repository.setCallTrackingEnabled(false)
        repository.setSmsTrackingEnabled(false)
        repository.setNotificationsEnabled(true)
        repository.setLastCallLogTimestamp(0L)
        repository.setLastSmsTimestamp(0L)
    }

    @Test
    fun `callTrackingEnabled defaults to false`() = runTest {
        assertFalse(repository.callTrackingEnabled())
    }

    @Test
    fun `setCallTrackingEnabled persists the value`() = runTest {
        repository.setCallTrackingEnabled(true)

        assertTrue(repository.callTrackingEnabled())
    }

    @Test
    fun `smsTrackingEnabled defaults to false`() = runTest {
        assertFalse(repository.smsTrackingEnabled())
    }

    @Test
    fun `setSmsTrackingEnabled persists the value`() = runTest {
        repository.setSmsTrackingEnabled(true)

        assertTrue(repository.smsTrackingEnabled())
    }

    @Test
    fun `notificationsEnabled defaults to true`() = runTest {
        assertTrue(repository.notificationsEnabled())
    }

    @Test
    fun `setNotificationsEnabled persists false`() = runTest {
        repository.setNotificationsEnabled(false)

        assertFalse(repository.notificationsEnabled())
    }

    @Test
    fun `lastCallLogTimestamp defaults to zero`() = runTest {
        assertEquals(0L, repository.lastCallLogTimestamp())
    }

    @Test
    fun `setLastCallLogTimestamp persists the value`() = runTest {
        repository.setLastCallLogTimestamp(123_456L)

        assertEquals(123_456L, repository.lastCallLogTimestamp())
    }

    @Test
    fun `lastSmsTimestamp defaults to zero`() = runTest {
        assertEquals(0L, repository.lastSmsTimestamp())
    }

    @Test
    fun `setLastSmsTimestamp persists the value`() = runTest {
        repository.setLastSmsTimestamp(654_321L)

        assertEquals(654_321L, repository.lastSmsTimestamp())
    }

    @Test
    fun `lastInteractionSyncAt is null when never synced`() = runTest {
        assertNull(repository.lastInteractionSyncAt())
    }
}
