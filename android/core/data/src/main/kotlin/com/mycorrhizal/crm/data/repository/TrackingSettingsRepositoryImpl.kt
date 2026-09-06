package com.mycorrhizal.crm.data.repository

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.first

private val Context.trackingDataStore: DataStore<Preferences> by preferencesDataStore(name = "tracking_prefs")

@Singleton
class TrackingSettingsRepositoryImpl @Inject constructor(
    @ApplicationContext private val context: Context,
) : TrackingSettingsRepository {

    private val callTracking = booleanPreferencesKey("call_tracking_enabled")
    private val smsTracking = booleanPreferencesKey("sms_tracking_enabled")
    private val notifications = booleanPreferencesKey("notifications_enabled")
    private val lastCallLogTs = longPreferencesKey("last_call_log_timestamp")
    private val lastSmsTs = longPreferencesKey("last_sms_timestamp")
    private val lastSyncAt = longPreferencesKey("last_interaction_sync_at")

    override suspend fun callTrackingEnabled(): Boolean =
        context.trackingDataStore.data.first()[callTracking] ?: false

    override suspend fun setCallTrackingEnabled(enabled: Boolean) {
        context.trackingDataStore.edit { it[callTracking] = enabled }
    }

    override suspend fun smsTrackingEnabled(): Boolean =
        context.trackingDataStore.data.first()[smsTracking] ?: false

    override suspend fun setSmsTrackingEnabled(enabled: Boolean) {
        context.trackingDataStore.edit { it[smsTracking] = enabled }
    }

    override suspend fun notificationsEnabled(): Boolean =
        context.trackingDataStore.data.first()[notifications] ?: true

    override suspend fun setNotificationsEnabled(enabled: Boolean) {
        context.trackingDataStore.edit { it[notifications] = enabled }
    }

    override suspend fun lastCallLogTimestamp(): Long =
        context.trackingDataStore.data.first()[lastCallLogTs] ?: 0L

    override suspend fun setLastCallLogTimestamp(ts: Long) {
        context.trackingDataStore.edit { it[lastCallLogTs] = ts }
    }

    override suspend fun lastSmsTimestamp(): Long =
        context.trackingDataStore.data.first()[lastSmsTs] ?: 0L

    override suspend fun setLastSmsTimestamp(ts: Long) {
        context.trackingDataStore.edit { it[lastSmsTs] = ts }
    }

    override suspend fun lastInteractionSyncAt(): Long? =
        context.trackingDataStore.data.first()[lastSyncAt]
}
