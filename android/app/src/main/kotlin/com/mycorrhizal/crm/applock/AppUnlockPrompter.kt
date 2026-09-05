package com.mycorrhizal.crm.applock

import android.os.Build
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricPrompt
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import kotlin.coroutines.resume
import kotlinx.coroutines.suspendCancellableCoroutine

/**
 * Device seam for the OS local-auth prompt (issue #722). The composable layer
 * calls [requestUnlock]; tests substitute a fake that returns a canned
 * [AppLockAuthOutcome] without an Activity or an OS dialog.
 */
interface AppUnlockPrompter {
    suspend fun requestUnlock(title: String, subtitle: String, negativeButtonText: String?): AppLockAuthOutcome
}

/**
 * [BiometricPrompt]-backed prompter. The gate authenticates with a Class 3
 * (BIOMETRIC_STRONG) biometric or the device credential (the PIN/pattern/
 * password that already secures the phone) as the fallback — see
 * [androidAppLockAuthenticators] and the ADR. No biometric sample ever reaches
 * the app: the Keystore-authenticated comparison happens entirely in the OS.
 *
 * A negative "use password" button is deliberately not set on modern Android:
 * with [BiometricManager.Authenticators.DEVICE_CREDENTIAL] in the allowed set
 * the OS owns the cancel/fallback affordance (setting one would throw), and
 * the user is never offered a lower-security path than the one they opted into.
 * On pre-API-30 devices the platform cannot prompt the device credential at
 * all, so the prompt is strong-biometric-only and carries a negative (cancel)
 * button instead — see [platformSupportsDeviceCredential].
 */
class BiometricAppUnlockPrompter(
    private val activity: FragmentActivity,
) : AppUnlockPrompter {

    override suspend fun requestUnlock(
        title: String,
        subtitle: String,
        negativeButtonText: String?,
    ): AppLockAuthOutcome = suspendCancellableCoroutine { continuation ->
        val prompt = BiometricPrompt(
            activity,
            ContextCompat.getMainExecutor(activity),
            object : BiometricPrompt.AuthenticationCallback() {
                override fun onAuthenticationError(errorCode: Int, errString: CharSequence) {
                    continuation.resume(when {
                        errorCode in CANCELLED_ERRORS -> AppLockAuthOutcome.Cancelled
                        errorCode in UNAVAILABLE_ERRORS -> AppLockAuthOutcome.NotAvailable
                        else -> AppLockAuthOutcome.Error
                    })
                }

                override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                    continuation.resume(AppLockAuthOutcome.Success)
                }

                override fun onAuthenticationFailed() {
                    // A transient mismatch (e.g. wrong fingerprint) is
                    // non-terminal — the OS lets the user retry, and we
                    // leave the prompt up rather than showing an error.
                }
            },
        )
        prompt.authenticate(androidAppUnlockPromptInfo(title, subtitle, negativeButtonText))
    }

    companion object {
        /** The user dismissed the prompt (system back / negative area). */
        private val CANCELLED_ERRORS = setOf(
            BiometricPrompt.ERROR_CANCELED,
            BiometricPrompt.ERROR_USER_CANCELED,
            BiometricPrompt.ERROR_NEGATIVE_BUTTON,
        )

        /** The device cannot currently satisfy the gate. */
        private val UNAVAILABLE_ERRORS = setOf(
            BiometricPrompt.ERROR_NO_BIOMETRICS,
            BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL,
            BiometricPrompt.ERROR_HW_UNAVAILABLE,
        )
    }
}

/**
 * Whether the platform can prompt the device credential (PIN/pattern/password)
 * as a fallback. The device-credential authenticator needs the Android 11
 * biometric framework; on older releases the prompt is strong-biometric-only
 * and needs its own negative (cancel) button.
 */
internal fun platformSupportsDeviceCredential(): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.R

/** The authenticator set for the current platform (see [androidAppLockAuthenticatorsFor]). */
internal fun androidAppLockAuthenticators(): Int =
    androidAppLockAuthenticatorsFor(platformSupportsDeviceCredential())

/**
 * The authenticator set the app lock accepts: a Class 3 biometric, or — on
 * API 30+ only ([deviceCredentialSupported]) — the device's own credential
 * (PIN/pattern/password) when no such biometric is enrolled. Mirrored by the
 * capability check in `core:data` (`AndroidLocalAuthCapabilities`) — see the
 * ADR for why weak (Class 2) biometrics alone are not offered. Pure so a JVM
 * test can pin both platform branches without SDK emulation.
 */
internal fun androidAppLockAuthenticatorsFor(deviceCredentialSupported: Boolean): Int =
    if (deviceCredentialSupported) {
        BiometricManager.Authenticators.BIOMETRIC_STRONG or
            BiometricManager.Authenticators.DEVICE_CREDENTIAL
    } else {
        BiometricManager.Authenticators.BIOMETRIC_STRONG
    }

/**
 * Pure so a guard test can pin the authenticator set and the
 * no-negative-button-on-modern rule. [negativeButtonText] is set only when the
 * platform cannot prompt the device credential (pre-API 30), where the prompt
 * is strong-biometric-only and needs its own negative (cancel) button.
 */
internal fun androidAppUnlockPromptInfo(
    title: String,
    subtitle: String,
    negativeButtonText: String?,
    deviceCredentialSupported: Boolean = platformSupportsDeviceCredential(),
): BiometricPrompt.PromptInfo {
    val builder = BiometricPrompt.PromptInfo.Builder()
        .setTitle(title)
        .setSubtitle(subtitle)
        .setAllowedAuthenticators(androidAppLockAuthenticatorsFor(deviceCredentialSupported))
    if (!deviceCredentialSupported) {
        builder.setNegativeButtonText(negativeButtonText.orEmpty())
    }
    return builder.build()
}
