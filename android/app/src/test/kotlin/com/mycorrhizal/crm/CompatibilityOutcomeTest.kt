package com.mycorrhizal.crm

import com.mycorrhizal.crm.model.AppVersion
import com.mycorrhizal.crm.model.network.ServerHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// Issue #692: the pure resolution of a fetched /health contract into the root
// gate. Covers the branches the MainViewModel orchestration tests cannot reach
// with the debug build's 0.1.0 versionName (e.g. the non-blocking notice, which
// needs a client newer than a still-supported server).
class CompatibilityOutcomeTest {

    private val clientNewer = "0.6.10"

    @Test
    fun `null server fails open to compatible with no version`() {
        val outcome = resolveCompatibilityOutcome(clientVersionName = clientNewer, server = null)
        assertEquals(CompatibilityGate.NotRequired, outcome.gate)
        assertNull(outcome.noticeVersion)
        assertNull(outcome.serverVersion)
    }

    @Test
    fun `a floor above the client forces an update`() {
        val outcome = resolveCompatibilityOutcome(
            clientVersionName = "0.6.0",
            server = ServerHealth(version = "0.6.10", minClientVersion = "0.7.0"),
        )
        assertEquals(CompatibilityGate.ForceUpdate("0.7.0"), outcome.gate)
        assertNull(outcome.noticeVersion)
        assertEquals(AppVersion(0, 6, 10), outcome.serverVersion)
    }

    @Test
    fun `a compatible supported server passes with its version exposed`() {
        val outcome = resolveCompatibilityOutcome(
            clientVersionName = "0.6.0",
            server = ServerHealth(version = "0.6.10"),
        )
        assertEquals(CompatibilityGate.NotRequired, outcome.gate)
        assertNull(outcome.noticeVersion)
        assertEquals(AppVersion(0, 6, 10), outcome.serverVersion)
    }

    @Test
    fun `a pre-baseline server blocks even when the client is not newer`() {
        val outcome = resolveCompatibilityOutcome(
            clientVersionName = "0.1.0",
            server = ServerHealth(version = "0.5.3"),
        )
        assertEquals(CompatibilityGate.ServerTooOld, outcome.gate)
        assertNull(outcome.noticeVersion)
        assertEquals(AppVersion(0, 5, 3), outcome.serverVersion)
    }

    @Test
    fun `a pre-baseline server blocks when the client is newer instead of noticing`() {
        val outcome = resolveCompatibilityOutcome(
            clientVersionName = clientNewer,
            server = ServerHealth(version = "0.5.3"),
        )
        assertEquals(CompatibilityGate.ServerTooOld, outcome.gate)
        assertNull(outcome.noticeVersion)
    }

    @Test
    fun `a newer client on a supported-but-older server raises the notice`() {
        val outcome = resolveCompatibilityOutcome(
            clientVersionName = clientNewer,
            server = ServerHealth(version = "0.6.5"),
        )
        assertEquals(CompatibilityGate.NotRequired, outcome.gate)
        assertEquals("0.6.5", outcome.noticeVersion)
        assertEquals(AppVersion(0, 6, 5), outcome.serverVersion)
    }

    @Test
    fun `an unparseable server version fails open to compatible`() {
        val outcome = resolveCompatibilityOutcome(
            clientVersionName = clientNewer,
            server = ServerHealth(version = "dev"),
        )
        assertEquals(CompatibilityGate.NotRequired, outcome.gate)
        assertNull(outcome.noticeVersion)
        assertNull(outcome.serverVersion)
    }
}
