package com.mycorrhizal.crm.data.session

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard test for the 401/session-expiry wiring (issue #678). The DI graph
 * cannot be booted on the JVM, so — following [EncryptedTokenStorageGuardTest]
 * and [RoomEncryptionGuardTest]'s pattern — this asserts on the source: it
 * fails if the session manager stops wiring [SessionExpiryWiring] to the
 * notifier, if the app's OkHttp chain drops [SessionExpiryInterceptor], or if
 * the wiring stops calling `clearSession` on the signal. The behavioral core
 * is covered directly by [SessionExpiryInterceptorTest] and
 * [SessionExpiryWiringTest].
 */
class SessionExpiryWiringGuardTest {

    private val dataModuleSource =
        File("src/main/kotlin/com/mycorrhizal/crm/data/di/DataModule.kt").readText()

    private val wiringSource =
        File("src/main/kotlin/com/mycorrhizal/crm/data/session/SessionExpiryWiring.kt").readText()

    private val appNetworkModuleSource =
        File("../../app/src/main/kotlin/com/mycorrhizal/crm/di/AppNetworkModule.kt").readText()

    @Test
    fun `the session manager is wired to the session-expiry notifier`() {
        assertTrue(
            "DataModule must provide a SessionExpiryNotifier",
            dataModuleSource.contains("fun provideSessionExpiryNotifier"),
        )
        assertTrue(
            "provideSessionManager must start SessionExpiryWiring",
            dataModuleSource.contains("SessionExpiryWiring(") &&
                dataModuleSource.contains(".start(scope)"),
        )
        // Issue #722: the wiring must first try a device-grant exchange so an
        // expired-but-valid session resumes on a biometric-enrolled device.
        assertTrue(
            "the 401 path must attempt a device-grant refresh before clearing",
            dataModuleSource.contains("refresher = { deviceGrantManager.get().refreshSessionFromStoredGrant() }"),
        )
    }

    @Test
    fun `a session-expiry signal clears the session`() {
        assertTrue(
            "SessionExpiryWiring must register a listener on the notifier",
            wiringSource.contains("sessionExpiryNotifier.register"),
        )
        assertTrue(
            "the registered listener must clear the session",
            wiringSource.contains("sessionManager.clearSession()"),
        )
    }

    @Test
    fun `the app's OkHttp chain carries the session-expiry interceptor`() {
        assertTrue(
            "AppNetworkModule must build a SessionExpiryInterceptor with the base-url host check",
            appNetworkModuleSource.contains("SessionExpiryInterceptor(sessionExpiryNotifier, baseUrlProvider)"),
        )
        assertTrue(
            "AppNetworkModule must pass the interceptor into NetworkFactory",
            appNetworkModuleSource.contains("sessionExpiryInterceptor ="),
        )
    }
}
