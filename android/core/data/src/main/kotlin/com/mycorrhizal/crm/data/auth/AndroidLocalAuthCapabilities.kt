package com.mycorrhizal.crm.data.auth

import android.content.Context
import android.os.Build
import androidx.biometric.BiometricManager
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Issue #722: device posture for the local gate, backed by Android's
 * `BiometricManager`. The gate authenticates with a Class 3
 * (BIOMETRIC_STRONG) biometric or — on devices with a secure lock screen but
 * no strong biometric enrolled — the device credential (PIN / pattern /
 * password) as the fallback. The device-credential fallback needs the API 30
 * biometric framework, so on older Android the posture (and the prompt, see
 * `AppUnlockPrompter`) is strong-biometric-only. Both questions the UI needs
 * are derived from the same authenticator set the prompt will use, so "can
 * enable" and "what will the prompt accept" can never disagree.
 */
@Singleton
class AndroidLocalAuthCapabilities @Inject constructor(
    @ApplicationContext context: Context,
) : LocalAuthCapabilities {

    private val biometricManager = BiometricManager.from(context)

    private val gateAuthenticators: Int =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            BiometricManager.Authenticators.BIOMETRIC_STRONG or
                BiometricManager.Authenticators.DEVICE_CREDENTIAL
        } else {
            BiometricManager.Authenticators.BIOMETRIC_STRONG
        }

    override fun canEnableLocalAuth(): Boolean =
        biometricManager.canAuthenticate(gateAuthenticators) == BiometricManager.BIOMETRIC_SUCCESS

    override fun hasStrongBiometric(): Boolean =
        biometricManager.canAuthenticate(BiometricManager.Authenticators.BIOMETRIC_STRONG) ==
            BiometricManager.BIOMETRIC_SUCCESS
}
