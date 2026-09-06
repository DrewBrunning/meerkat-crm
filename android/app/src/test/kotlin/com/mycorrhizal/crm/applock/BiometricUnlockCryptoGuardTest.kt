package com.mycorrhizal.crm.applock

import org.junit.Assert.assertTrue
import org.junit.Test

// CodeQL `android/insecure-local-authentication` (remediation): the biometric
// prompt must consume its authentication result cryptographically. Source-scan
// pins that the unlock prompt is bound to a Keystore user-authentication-
// required key via a CryptoObject and that the success callback runs a real
// doFinal through the vault — drift here would silently re-introduce the
// finding.
class BiometricUnlockCryptoGuardTest {

    private val prompterSource = java.io.File(
        "src/main/kotlin/com/mycorrhizal/crm/applock/AppUnlockPrompter.kt",
    ).readText()
    private val vaultSource = java.io.File(
        "src/main/kotlin/com/mycorrhizal/crm/applock/BiometricUnlockVault.kt",
    ).readText()

    @Test
    fun `the prompt authenticates with a CryptoObject and consumes the result`() {
        assertTrue(
            "authenticate must pass a CryptoObject bound to the auth key",
            prompterSource.contains("BiometricPrompt.CryptoObject(prepared.cipher)"),
        )
        assertTrue(
            "the success callback must consume the authentication result's crypto object",
            prompterSource.contains("result.cryptoObject") &&
                prompterSource.contains("vault.finish(prepared, cipher)"),
        )
        assertTrue(vaultSource.contains("fun finish"))
    }

    @Test
    fun `the gate key requires a fresh user authentication and dies on biometric change`() {
        assertTrue(vaultSource.contains("setUserAuthenticationRequired(true)"))
        assertTrue(vaultSource.contains("setInvalidatedByBiometricEnrollment(true)"))
        assertTrue(vaultSource.contains("KeyProperties.AUTH_BIOMETRIC_STRONG"))
        assertTrue(vaultSource.contains("KeyProperties.AUTH_DEVICE_CREDENTIAL"))
    }
}
