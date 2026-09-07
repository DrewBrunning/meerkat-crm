package com.mycorrhizal.crm.domain.compat

import com.mycorrhizal.crm.model.network.ServerHealth
import org.junit.Assert.assertEquals
import org.junit.Test

// Issue #528: the three-state decision + the fail-open rule. The resolver is
// the single implementation of the policy (docs/client-compatibility-policy.md)
// shared by every caller, so each branch and each fail-open input is pinned.
class CompatibilityResolverTest {

    private fun resolve(client: String, server: ServerHealth?): Compatibility =
        CompatibilityResolver.resolve(client, server)

    // --- the blocking force-update state --------------------------------------

    @Test
    fun `client below the declared floor forces an update`() {
        val server = ServerHealth(version = "0.6.10", minClientVersion = "0.6.0", apiContractVersion = "v1")
        assertEquals(Compatibility.ForceUpdate("0.6.0"), resolve("0.5.9", server))
    }

    @Test
    fun `client exactly at the floor is compatible`() {
        val server = ServerHealth(minClientVersion = "0.6.0")
        assertEquals(Compatibility.Compatible, resolve("0.6.0", server))
    }

    @Test
    fun `client above the floor is compatible`() {
        val server = ServerHealth(minClientVersion = "0.6.0")
        assertEquals(Compatibility.Compatible, resolve("0.6.10", server))
    }

    @Test
    fun `a prerelease client at the floor number is not forced below it`() {
        // "0.7.0-rc.1" parses to the 0.7.0 triple; an equal-numbered rc is a
        // transient build and fail-open prefers not to strand it.
        val server = ServerHealth(minClientVersion = "0.7.0")
        assertEquals(Compatibility.Compatible, resolve("0.7.0-rc.1", server))
    }

    // --- the non-blocking server-outdated state -------------------------------

    @Test
    fun `client newer than the server raises the upgrade notice`() {
        val server = ServerHealth(version = "0.6.10", apiContractVersion = "v1")
        assertEquals(Compatibility.ServerUpdateRecommended("0.6.10"), resolve("0.7.0", server))
    }

    @Test
    fun `equal client and server versions are compatible`() {
        val server = ServerHealth(version = "0.6.10")
        assertEquals(Compatibility.Compatible, resolve("0.6.10", server))
    }

    @Test
    fun `client older than the server is compatible when no floor is declared`() {
        // The default posture: an old client keeps working against a new server.
        val server = ServerHealth(version = "0.6.10")
        assertEquals(Compatibility.Compatible, resolve("0.1.0", server))
    }

    @Test
    fun `a blocking floor beats the server-outdated notice when both could apply`() {
        // A server that both sits below the client's version AND declares a
        // floor above it (an old server whose operator already raised the
        // floor): the client is below the floor, so an update is required no
        // matter that the client is also newer than the server's build.
        val server = ServerHealth(version = "0.5.0", minClientVersion = "0.7.0")
        assertEquals(Compatibility.ForceUpdate("0.7.0"), resolve("0.6.0", server))
    }

    // --- fail open -------------------------------------------------------------

    @Test
    fun `null server fails open to compatible`() {
        // A network error or an absent body resolves to "no constraint".
        assertEquals(Compatibility.Compatible, resolve("0.1.0", null))
    }

    @Test
    fun `unparseable client version fails open`() {
        val server = ServerHealth(minClientVersion = "0.6.0")
        assertEquals(Compatibility.Compatible, resolve("not-a-version", server))
    }

    @Test
    fun `unparseable floor is ignored rather than treated as a constraint`() {
        val server = ServerHealth(minClientVersion = "garbage")
        assertEquals(Compatibility.Compatible, resolve("0.1.0", server))
    }

    @Test
    fun `unparseable server version skips the newer-client notice`() {
        // An unstamped test binary reports "dev".
        val server = ServerHealth(version = "dev")
        assertEquals(Compatibility.Compatible, resolve("0.7.0", server))
    }

    @Test
    fun `absent floor and absent version are compatible`() {
        assertEquals(Compatibility.Compatible, resolve("0.1.0", ServerHealth()))
    }
}
