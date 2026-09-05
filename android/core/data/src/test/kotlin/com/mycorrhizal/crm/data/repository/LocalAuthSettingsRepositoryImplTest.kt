package com.mycorrhizal.crm.data.repository

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class LocalAuthSettingsRepositoryImplTest {

    private lateinit var repository: LocalAuthSettingsRepositoryImpl

    @Before
    fun setup() = kotlinx.coroutines.runBlocking {
        val context = ApplicationProvider.getApplicationContext<Context>()
        repository = LocalAuthSettingsRepositoryImpl(context)
        // DataStore's `by preferencesDataStore(...)` delegate keeps its own
        // in-memory cache once warm, and Robolectric reuses that warm instance
        // across test methods — reset through the write API, never by deleting
        // the backing file (see TrackingSettingsRepositoryImplTest).
        repository.setRequireLocalAuth(false)
        repository.setAutoLockDelay(AutoLockDelay.DEFAULT)
    }

    @Test
    fun `requireLocalAuth defaults to off`() = runTest {
        assertFalse(repository.requireLocalAuth().first())
    }

    @Test
    fun `setRequireLocalAuth persists the value`() = runTest {
        repository.setRequireLocalAuth(true)
        assertTrue(repository.requireLocalAuth().first())

        repository.setRequireLocalAuth(false)
        assertFalse(repository.requireLocalAuth().first())
    }

    @Test
    fun `autoLockDelay defaults to five minutes`() = runTest {
        assertEquals(AutoLockDelay.DEFAULT, repository.autoLockDelay().first())
        assertEquals(AutoLockDelay.FIVE_MINUTES, AutoLockDelay.DEFAULT)
    }

    @Test
    fun `setAutoLockDelay persists the value`() = runTest {
        repository.setAutoLockDelay(AutoLockDelay.ONE_HOUR)
        assertEquals(AutoLockDelay.ONE_HOUR, repository.autoLockDelay().first())
    }

    @Test
    fun `an absent or unknown persisted delay falls back to the default`() = runTest {
        assertEquals(AutoLockDelay.FIVE_MINUTES, AutoLockDelay.fromMinutes(99_999L))
        assertEquals(AutoLockDelay.FIVE_MINUTES, AutoLockDelay.DEFAULT)
    }
}
