package com.mycorrhizal.crm.data.session

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard test for the device-grant storage backend (issue #722, MASVS-L1
 * STORAGE-1 / CRYPTO-1, same rationale as [EncryptedTokenStorageGuardTest]).
 * The plaintext grant is a long-lived credential — "possession of this token"
 * mints a session — so it must live in [EncryptedSharedPreferences] behind a
 * Keystore [MasterKey], never in a plain `SharedPreferences`. [EncryptedDeviceGrantTokenStorage]
 * cannot be exercised directly on the JVM (no Keystore under Robolectric), so
 * the test asserts on the source and the DI wiring, like its token-store twin.
 */
class EncryptedDeviceGrantStorageGuardTest {

    private val storageSource =
        File("src/main/kotlin/com/mycorrhizal/crm/data/session/DeviceGrantTokenStorage.kt").readText()

    private val diSource =
        File("src/main/kotlin/com/mycorrhizal/crm/data/di/DataModule.kt").readText()

    @Test
    fun `the grant is stored via EncryptedSharedPreferences, not plain SharedPreferences`() {
        assertTrue(
            "EncryptedDeviceGrantTokenStorage must use EncryptedSharedPreferences.create",
            storageSource.contains("EncryptedSharedPreferences.create"),
        )
        assertFalse(
            "EncryptedDeviceGrantTokenStorage must not fall back to a plain getSharedPreferences",
            storageSource.contains("getSharedPreferences"),
        )
    }

    @Test
    fun `the encryption key is a Keystore-backed MasterKey with AES256_GCM`() {
        assertTrue(storageSource.contains("MasterKey.Builder"))
        assertTrue(storageSource.contains("MasterKey.KeyScheme.AES256_GCM"))
    }

    @Test
    fun `pref keys and values use the recommended encryption schemes`() {
        assertTrue(storageSource.contains("PrefKeyEncryptionScheme.AES256_SIV"))
        assertTrue(storageSource.contains("PrefValueEncryptionScheme.AES256_GCM"))
    }

    @Test
    fun `the DI graph wires the encrypted store as the DeviceGrantTokenStorage implementation`() {
        assertTrue(
            "provideDeviceGrantTokenStorage must return an EncryptedDeviceGrantTokenStorage",
            diSource.contains("EncryptedDeviceGrantTokenStorage(context)"),
        )
    }
}
