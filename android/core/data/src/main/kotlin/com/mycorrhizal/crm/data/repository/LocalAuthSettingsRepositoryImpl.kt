package com.mycorrhizal.crm.data.repository

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.mycorrhizal.crm.domain.repository.AutoLockDelay
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.localAuthDataStore: DataStore<Preferences> by preferencesDataStore(name = "local_auth_prefs")

/**
 * Issue #722: the opt-in app-lock preference. Stored in plain DataStore, not
 * the encrypted envelope — these are *preferences* (on/off, timeout width),
 * not credentials; the secret they gate (the session JWT) stays in
 * `EncryptedTokenStorage`. See the interface doc for the security reasoning.
 */
@Singleton
class LocalAuthSettingsRepositoryImpl @Inject constructor(
    @ApplicationContext private val context: Context,
) : LocalAuthSettingsRepository {

    private val requireLocalAuthKey = booleanPreferencesKey("require_local_auth")
    private val autoLockDelayMinutesKey = longPreferencesKey("auto_lock_delay_minutes")

    override fun requireLocalAuth(): Flow<Boolean> =
        context.localAuthDataStore.data.map { it[requireLocalAuthKey] ?: false }

    override suspend fun setRequireLocalAuth(enabled: Boolean) {
        context.localAuthDataStore.edit { it[requireLocalAuthKey] = enabled }
    }

    override fun autoLockDelay(): Flow<AutoLockDelay> =
        context.localAuthDataStore.data.map { prefs ->
            AutoLockDelay.fromMinutes(prefs[autoLockDelayMinutesKey] ?: AutoLockDelay.DEFAULT.minutes)
        }

    override suspend fun setAutoLockDelay(delay: AutoLockDelay) {
        context.localAuthDataStore.edit { it[autoLockDelayMinutesKey] = delay.minutes }
    }
}
