package com.mycorrhizal.crm.data.session

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * Stores the plaintext device grant (issue #722's fully-biometric-login
 * layer). The grant is a long-lived credential — "possession of this token"
 * is what the server accepts as proof the enrolled device is present — so it
 * gets the same EncryptedSharedPreferences + Keystore `MasterKey` envelope the
 * session JWT already uses (`EncryptedTokenStorage`), never plaintext prefs.
 */
interface DeviceGrantTokenStorage {
    suspend fun save(token: String, id: Long)
    suspend fun loadToken(): String?
    suspend fun loadGrantId(): Long?
    suspend fun clear()
}

/**
 * EncryptedSharedPreferences-backed [DeviceGrantTokenStorage]. Every method is
 * exercised only via instrumented tests on a real Keystore (the same reason
 * `EncryptedTokenStorage` and `RoomPassphraseStore` cannot run under
 * Robolectric); the backend is pinned by [EncryptedDeviceGrantStorageGuardTest].
 */
class EncryptedDeviceGrantTokenStorage(context: Context) : DeviceGrantTokenStorage {

    private val prefs: SharedPreferences = run {
        val masterKey = MasterKey.Builder(context) // # pragma: no cover — needs a real Android Keystore (see EncryptedDeviceGrantStorageGuardTest)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM) // # pragma: no cover
            .build() // # pragma: no cover
        EncryptedSharedPreferences.create( // # pragma: no cover
            context, // # pragma: no cover
            FILE_NAME, // # pragma: no cover
            masterKey, // # pragma: no cover
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV, // # pragma: no cover
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM, // # pragma: no cover
        )
    }

    override suspend fun save(token: String, id: Long) { // # pragma: no cover — needs a real Android Keystore (see EncryptedDeviceGrantStorageGuardTest)
        prefs.edit() // # pragma: no cover
            .putString(KEY_GRANT, token) // # pragma: no cover
            .putLong(KEY_GRANT_ID, id) // # pragma: no cover
            .apply() // # pragma: no cover
    }

    override suspend fun loadToken(): String? = prefs.getString(KEY_GRANT, null) // # pragma: no cover — needs a real Android Keystore

    override suspend fun loadGrantId(): Long? =
        prefs.getLong(KEY_GRANT_ID, 0L).takeIf { it != 0L } // # pragma: no cover — needs a real Android Keystore

    override suspend fun clear() { // # pragma: no cover — needs a real Android Keystore (see EncryptedDeviceGrantStorageGuardTest)
        prefs.edit() // # pragma: no cover
            .remove(KEY_GRANT) // # pragma: no cover
            .remove(KEY_GRANT_ID) // # pragma: no cover
            .apply() // # pragma: no cover
    }

    companion object {
        private const val FILE_NAME = "secure_device_grant"
        private const val KEY_GRANT = "device_grant"
        private const val KEY_GRANT_ID = "device_grant_id"
    }
}
