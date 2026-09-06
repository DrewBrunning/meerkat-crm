package com.mycorrhizal.crm.domain.repository

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class AutoLockDelayTest {

    @Test
    fun `each delay round-trips through its minute value`() {
        AutoLockDelay.entries.forEach { delay ->
            assertEquals(delay, AutoLockDelay.fromMinutes(delay.minutes))
        }
    }

    @Test
    fun `immediately has a zero grace period`() {
        assertEquals(0L, AutoLockDelay.IMMEDIATELY.minutes)
    }

    @Test
    fun `the default is a five-minute grace period`() {
        assertEquals(AutoLockDelay.FIVE_MINUTES, AutoLockDelay.DEFAULT)
    }

    @Test
    fun `an unknown persisted value falls back to the default rather than failing`() {
        assertEquals(AutoLockDelay.DEFAULT, AutoLockDelay.fromMinutes(99_999L))
        assertEquals(AutoLockDelay.DEFAULT, AutoLockDelay.fromMinutes(-1L))
    }

    @Test
    fun `delay options are distinct`() {
        val minuteValues = AutoLockDelay.entries.map { it.minutes }
        assertEquals(minuteValues.size, minuteValues.toSet().size)
        assertNotEquals(AutoLockDelay.IMMEDIATELY, AutoLockDelay.ONE_HOUR)
    }
}
