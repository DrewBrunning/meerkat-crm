package com.mycorrhizal.crm.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

// Issue #528: the client/server version parser. The comparison semantics are
// exercised through CompatibilityResolver's tests; here we pin the parse shape
// itself, including the fail-open null for anything unparseable.
class AppVersionTest {

    @Test
    fun `parses full major minor patch triples`() {
        assertEquals(AppVersion(0, 6, 10), AppVersion.parse("0.6.10"))
        assertEquals(AppVersion(1, 2, 3), AppVersion.parse("1.2.3"))
        assertEquals(AppVersion(0, 1, 0), AppVersion.parse("0.1.0"))
    }

    @Test
    fun `parses git-tag style leading v`() {
        assertEquals(AppVersion(0, 6, 10), AppVersion.parse("v0.6.10"))
    }

    @Test
    fun `parses partial versions by zero padding`() {
        assertEquals(AppVersion(0, 6, 0), AppVersion.parse("0.6"))
        assertEquals(AppVersion(1, 0, 0), AppVersion.parse("1"))
    }

    @Test
    fun `ignores prerelease and build metadata`() {
        assertEquals(AppVersion(0, 7, 0), AppVersion.parse("0.7.0-rc.1"))
        assertEquals(AppVersion(0, 7, 0), AppVersion.parse("0.7.0+build.7"))
        assertEquals(AppVersion(0, 6, 10), AppVersion.parse("0.6.10-alpha.1+build.42"))
    }

    @Test
    fun `trims surrounding whitespace`() {
        assertEquals(AppVersion(0, 6, 10), AppVersion.parse("  0.6.10  "))
    }

    @Test
    fun `unparseable values fail open to null`() {
        // null/blank, the unstamped "dev" build, and genuinely garbage strings
        // must never parse: callers treat null as "cannot decide => compatible".
        assertNull(AppVersion.parse(null))
        assertNull(AppVersion.parse(""))
        assertNull(AppVersion.parse("   "))
        assertNull(AppVersion.parse("dev"))
        assertNull(AppVersion.parse("latest"))
        assertNull(AppVersion.parse("0.6.10.1"))
        assertNull(AppVersion.parse("banana"))
        assertNull(AppVersion.parse("0,6"))
        assertNull(AppVersion.parse("0.6."))
        assertNull(AppVersion.parse("-rc.1"))
    }

    @Test
    fun `comparison is numeric major minor patch`() {
        assertTrue(AppVersion(0, 6, 10) > AppVersion(0, 6, 9))
        assertTrue(AppVersion(0, 6, 0) < AppVersion(0, 7, 0))
        assertTrue(AppVersion(1, 0, 0) > AppVersion(0, 9, 99))
        assertTrue(AppVersion(0, 6, 10) > AppVersion(0, 6, 2))
        assertEquals(0, AppVersion(0, 6, 0).compareTo(AppVersion(0, 6, 0)))
    }

    @Test
    fun `string form is the padded triple`() {
        assertEquals("0.6.0", AppVersion(0, 6, 0).toString())
        assertEquals("0.6.10", AppVersion(0, 6, 10).toString())
    }
}
