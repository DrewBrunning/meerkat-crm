package com.mycorrhizal.crm

import com.mycorrhizal.crm.data.session.AppLockState
import org.junit.Assert.assertEquals
import org.junit.Test

// Issue #722: the root's surface decision is the security-critical rule
// "never render the authenticated tree before the app-lock gate has cleared",
// factored out of MycorrhizalApp so it is unit-testable without a host.
class RootSurfaceTest {

    @Test
    fun `logged out always shows the auth flow regardless of gate state`() {
        AppLockState.entries.forEach { lock ->
            assertEquals(RootSurface.Auth, rootSurface(isLoggedIn = false, appLockState = lock))
        }
    }

    @Test
    fun `a logged-in session whose gate is undecided renders nothing`() {
        assertEquals(RootSurface.GateResolving, rootSurface(isLoggedIn = true, appLockState = AppLockState.Resolving))
    }

    @Test
    fun `a logged-in session behind a required gate shows the app lock`() {
        assertEquals(RootSurface.AppLock, rootSurface(isLoggedIn = true, appLockState = AppLockState.Locked))
    }

    @Test
    fun `a logged-in session with the gate cleared shows the main tree`() {
        assertEquals(RootSurface.Main, rootSurface(isLoggedIn = true, appLockState = AppLockState.Unlocked))
    }
}
