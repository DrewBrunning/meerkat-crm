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

/** EncryptedSharedPreferences-backed [DeviceGrantTokenStorage]. */
class EncryptedDeviceGrantTokenStorage(context: Context) : DeviceGrantTokenStorage {

    private val prefs: SharedPreferences = run {
        val masterKey = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            context,
            FILE_NAME,
            masterKey,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    override suspend fun save(token: String, id: Long) {
        prefs.edit()
            .putString(KEY_GRANT, token)
            .putLong(KEY_GRANT_ID, id)
            .apply()
    }

    override suspend fun loadToken(): String? = prefs.getString(KEY_GRANT, null)

    override suspend fun loadGrantId(): Long? =
        prefs.getLong(KEY_GRANT_ID, 0L).takeIf { it != 0L }

    override suspend fun clear() {
        prefs.edit()
            .remove(KEY_GRANT)
            .remove(KEY_GRANT_ID)
            .apply()
    }

    companion object {
        private const val FILE_NAME = "secure_device_grant"
        private const val KEY_GRANT = "device_grant"
        private const val KEY_GRANT_ID = "device_grant_id"
    }
}
