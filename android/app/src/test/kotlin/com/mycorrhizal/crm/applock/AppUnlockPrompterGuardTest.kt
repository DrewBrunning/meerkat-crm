package com.mycorrhizal.crm.applock

import androidx.biometric.BiometricManager
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Issue #722: pins the exact local-auth posture the OS prompt is built with —
 * a Class 3 biometric or (API 30+) the device credential, never a weak
 * (Class 2) biometric, and never a negative "use password" button where the OS
 * owns the fallback. A drift here would silently weaken the gate the Settings
 * toggle describes.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class AppUnlockPrompterGuardTest {

    @Test
    fun `on modern Android the prompt accepts strong biometrics or the device credential`() {
        assertTrue(platformSupportsDeviceCredential())

        val expected = BiometricManager.Authenticators.BIOMETRIC_STRONG or
            BiometricManager.Authenticators.DEVICE_CREDENTIAL
        assertEquals(expected, androidAppLockAuthenticators())
        assertEquals(expected, androidAppUnlockPromptInfo("t", "s", null).allowedAuthenticators)
    }

    @Test
    fun `on modern Android the prompt carries no negative button`() {
        // With DEVICE_CREDENTIAL allowed, BiometricPrompt.Builder throws if a
        // negative button is set — the OS provides its own cancel/fallback.
        // (PromptInfo's default is the empty string, not null.)
        assertTrue(androidAppUnlockPromptInfo("t", "s", null).negativeButtonText.isNullOrEmpty())
    }

    // Android < 11 has no device-credential authenticator: the prompt is
    // strong-biometric-only and must carry its own negative (cancel) button,
    // or BiometricPrompt.Builder/authenticate would reject the device-credential
    // bit at prompt time on that OS.
    @Test
    fun `without device-credential support the prompt is strong-biometric-only with a negative button`() {
        val strongOnly = BiometricManager.Authenticators.BIOMETRIC_STRONG
        assertEquals(strongOnly, androidAppLockAuthenticatorsFor(deviceCredentialSupported = false))
        val info = androidAppUnlockPromptInfo("t", "s", "Cancel", deviceCredentialSupported = false)
        assertEquals(strongOnly, info.allowedAuthenticators)
        assertEquals("Cancel", info.negativeButtonText.toString())
    }

    @Test
    fun `with device-credential support the negative button is never set`() {
        val expected = BiometricManager.Authenticators.BIOMETRIC_STRONG or
            BiometricManager.Authenticators.DEVICE_CREDENTIAL
        assertEquals(expected, androidAppLockAuthenticatorsFor(deviceCredentialSupported = true))
        val info = androidAppUnlockPromptInfo("t", "s", "Cancel", deviceCredentialSupported = true)
        assertEquals(expected, info.allowedAuthenticators)
        assertTrue(info.negativeButtonText.isNullOrEmpty())
    }
}
