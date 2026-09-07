package com.mycorrhizal.crm

import com.mycorrhizal.crm.data.session.AppLockState
import org.junit.Assert.assertEquals
import org.junit.Test

// Issue #722: the root's surface decision is the security-critical rule
// "never render the authenticated tree before the app-lock gate has cleared",
// factored out of MycorrhizalApp so it is unit-testable without a host.
// Issue #528: the compatibility gate joins the decision — a required
// force-update supersedes the app-lock gate (it renders no session data).
// Issue #692: both blocking compatibility gates (force-update AND server-too-old)
// are decided BEFORE the logged-out auth tree too — against a server that will
// refuse this build (floor above client) or that predates this app's baseline,
// the auth screen is not worth showing.
class RootSurfaceTest {

    private val noUpdate: CompatibilityGate = CompatibilityGate.NotRequired

    private fun gate(requiredVersion: String): CompatibilityGate =
        CompatibilityGate.ForceUpdate(requiredVersion)

    @Test
    fun `logged out with no blocking gate shows the auth flow`() {
        AppLockState.entries.forEach { lock ->
            assertEquals(
                RootSurface.Auth,
                rootSurface(isLoggedIn = false, compatibilityGate = noUpdate, appLockState = lock),
            )
        }
    }

    @Test
    fun `a required force-update supersedes the auth screen and the app lock`() {
        AppLockState.entries.forEach { lock ->
            for (loggedIn in listOf(false, true)) {
                assertEquals(
                    "force-update must show while lock=$lock loggedIn=$loggedIn",
                    RootSurface.ForceUpdate,
                    rootSurface(isLoggedIn = loggedIn, compatibilityGate = gate("0.6.0"), appLockState = lock),
                )
            }
        }
    }

    @Test
    fun `a server-too-old gate supersedes the auth screen and the app lock`() {
        AppLockState.entries.forEach { lock ->
            for (loggedIn in listOf(false, true)) {
                assertEquals(
                    "server-too-old must show while lock=$lock loggedIn=$loggedIn",
                    RootSurface.ServerTooOld,
                    rootSurface(
                        isLoggedIn = loggedIn,
                        compatibilityGate = CompatibilityGate.ServerTooOld,
                        appLockState = lock,
                    ),
                )
            }
        }
    }

    @Test
    fun `a logged-in session whose gate is undecided renders nothing`() {
        assertEquals(
            RootSurface.GateResolving,
            rootSurface(isLoggedIn = true, compatibilityGate = noUpdate, appLockState = AppLockState.Resolving),
        )
    }

    @Test
    fun `a logged-in session behind a required gate shows the app lock`() {
        assertEquals(
            RootSurface.AppLock,
            rootSurface(isLoggedIn = true, compatibilityGate = noUpdate, appLockState = AppLockState.Locked),
        )
    }

    @Test
    fun `a logged-in session with the gate cleared shows the main tree`() {
        assertEquals(
            RootSurface.Main,
            rootSurface(isLoggedIn = true, compatibilityGate = noUpdate, appLockState = AppLockState.Unlocked),
        )
    }

    @Test
    fun `a required force-update supersedes the app-lock gate and the main tree`() {
        AppLockState.entries.forEach { lock ->
            assertEquals(
                "force-update must show while lock state is $lock",
                RootSurface.ForceUpdate,
                rootSurface(isLoggedIn = true, compatibilityGate = gate("0.6.0"), appLockState = lock),
            )
        }
    }
}
