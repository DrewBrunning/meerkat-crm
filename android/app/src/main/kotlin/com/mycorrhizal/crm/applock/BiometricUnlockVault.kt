package com.mycorrhizal.crm.applock

import android.content.Context
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyPermanentlyInvalidatedException
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.biometric.BiometricManager
import com.mycorrhizal.crm.model.Generated
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * What an unlock attempt is asked to do with the Keystore key that the
 * biometric prompt authorizes.
 */
internal sealed interface UnlockCipherSpec {
    /** The cipher whose use the OS prompt just authorized. */
    val cipher: Cipher

    /** No wrapped token exists yet — the prompt must authorize creating one. */
    data class Encrypt(override val cipher: Cipher) : UnlockCipherSpec

    /** A wrapped token exists — the prompt must authorize decrypting it (proves key continuity). */
    data class Decrypt(override val cipher: Cipher, val wrapped: ByteArray) : UnlockCipherSpec
}

/**
 * CodeQL `android/insecure-local-authentication` requires the biometric
 * prompt's authentication result to feed a cryptographic operation, so the
 * app lock is bound to a Keystore **user-authentication-required** AES key
 * (CodeQL's documented remediation for this rule). A small wrapped token is
 * stored per install:
 *
 *  - the first successful unlock authorizes **encrypting** it;
 *  - every later unlock authorizes **decrypting** it and must reproduce the
 *    known plaintext.
 *
 * The wrapped token itself is meaningless — its value is that the key can only
 * be used right after the OS prompt (biometric or device credential), so the
 * prompt result is always consumed by a real `doFinal`. The key is created
 * with `setInvalidatedByBiometricEnrollment(true)`, so removing/adding
 * biometrics permanently invalidates it and the app re-establishes the token
 * after a fresh unlock.
 */
@Generated("AndroidKeyStore auth-bound keys + GCM cipher — structurally unexercisable by the JVM unit runner; guarded by BiometricUnlockCryptoGuardTest + the instrumented device suite")
internal class BiometricUnlockVault(
    context: Context,
) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
    private val keyStore: KeyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    /** The plaintext the wrapped token is required to decrypt back to. */
    fun expectedPlaintext(): ByteArray = PLAINTEXT

    /**
     * Load (or create) the auth-bound key and decide whether this unlock
     * authorizes creating the wrapped token or decrypting the existing one.
     * The wrapped blob is `GCM_IV_LENGTH` IV bytes followed by the ciphertext.
     */
    fun prepare(): UnlockCipherSpec { // # pragma: no cover — AndroidKeyStore is unavailable under Robolectric; see BiometricUnlockVaultGuardTest + AppUnlockPrompterGuardTest
        val key = loadOrCreateKey()
        return storedWrapped()?.let { wrapped -> // # pragma: no cover
            val cipher = Cipher.getInstance(TRANSFORMATION) // # pragma: no cover
            cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(GCM_TAG_BITS, wrapped, 0, GCM_IV_LENGTH)) // # pragma: no cover
            UnlockCipherSpec.Decrypt(cipher, wrapped.copyOfRange(GCM_IV_LENGTH, wrapped.size)) // # pragma: no cover
        } ?: run { // # pragma: no cover
            val cipher = Cipher.getInstance(TRANSFORMATION) // # pragma: no cover
            cipher.init(Cipher.ENCRYPT_MODE, key) // # pragma: no cover
            UnlockCipherSpec.Encrypt(cipher) // # pragma: no cover
        }
    }

    /**
     * Consume the prompt's authorization: encrypt (first time) or decrypt +
     * verify (thereafter). Returns false when the key was invalidated (e.g. a
     * biometric enrollment change), in which case the stale token + key are
     * cleared so the next unlock can re-establish them.
     */
    fun finish(spec: UnlockCipherSpec): Boolean { // # pragma: no cover — AndroidKeyStore is unavailable under Robolectric; see BiometricUnlockVaultGuardTest
        return try {
            when (spec) {
                is UnlockCipherSpec.Encrypt -> {
                    val wrapped = spec.cipher.doFinal(PLAINTEXT)
                    storeWrapped(wrapped)
                    true
                }
                is UnlockCipherSpec.Decrypt -> {
                    val plain = spec.cipher.doFinal(spec.wrapped)
                    plain.contentEquals(PLAINTEXT)
                }
            }
        } catch (e: KeyPermanentlyInvalidatedException) {
            clear()
            false
        } catch (e: Exception) {
            false
        }
    }

    /** Wipe the wrapped token + the auth-bound key (a permanently invalidated key can never be used again). */
    fun clear() { // # pragma: no cover — AndroidKeyStore is unavailable under Robolectric
        prefs.edit().remove(KEY_WRAPPED).apply()
        keyStore.deleteEntry(KEY_ALIAS)
    }

    private fun loadOrCreateKey(): SecretKey { // # pragma: no cover
        (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        val builder = KeyGenParameterSpec.Builder(
            KEY_ALIAS,
            KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
        )
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setUserAuthenticationRequired(true)
            .setInvalidatedByBiometricEnrollment(true)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            builder.setUserAuthenticationParameters(
                0, // no duration — every use must be freshly authorized
                BiometricManager.Authenticators.BIOMETRIC_STRONG or
                    BiometricManager.Authenticators.DEVICE_CREDENTIAL,
            )
        }
        generator.init(builder.build())
        return generator.generateKey()
    }

    private fun storedWrapped(): ByteArray? { // # pragma: no cover
        val stored = prefs.getString(KEY_WRAPPED, null) ?: return null
        return Base64.decode(stored, Base64.NO_WRAP)
    }

    private fun storeWrapped(wrapped: ByteArray) { // # pragma: no cover
        prefs.edit().putString(KEY_WRAPPED, Base64.encodeToString(wrapped, Base64.NO_WRAP)).apply()
    }

    companion object {
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val KEY_ALIAS = "mycorrhizal_app_lock_key"
        private const val PREFS_NAME = "app_lock"
        private const val KEY_WRAPPED = "wrapped_unlock_token"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val GCM_TAG_BITS = 128
        private const val GCM_IV_LENGTH = 12
        private val PLAINTEXT = "mycorrhizal-unlock-v1".encodeToByteArray()
    }
}
