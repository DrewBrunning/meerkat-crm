package com.mycorrhizal.crm.data.repository

import com.mycorrhizal.crm.domain.repository.ApiTokenRepository
import com.mycorrhizal.crm.domain.repository.CalendarSubscriptionRepository
import com.mycorrhizal.crm.domain.repository.ContactSubscriptionRepository
import com.mycorrhizal.crm.domain.repository.NotificationSettingsRepository
import com.mycorrhizal.crm.domain.repository.WebhookRepository
import com.mycorrhizal.crm.model.network.ApiToken
import com.mycorrhizal.crm.model.network.ApiTokenCreateResponse
import com.mycorrhizal.crm.model.network.ApiTokenInput
import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.CalendarSubscriptionInput
import com.mycorrhizal.crm.model.network.CalendarSyncResult
import com.mycorrhizal.crm.model.network.ContactSubscription
import com.mycorrhizal.crm.model.network.NotificationConfig
import com.mycorrhizal.crm.model.network.NotificationConfigInput
import com.mycorrhizal.crm.model.network.NotificationTestResult
import com.mycorrhizal.crm.model.network.RevokeAllApiTokensResponse
import com.mycorrhizal.crm.model.network.Webhook
import com.mycorrhizal.crm.model.network.WebhookCreateResponse
import com.mycorrhizal.crm.model.network.WebhookDelivery
import com.mycorrhizal.crm.model.network.WebhookInput
import com.mycorrhizal.crm.network.ApiClient
import com.mycorrhizal.crm.network.toApiError
import javax.inject.Inject
import javax.inject.Singleton

/** M25: webhook CRUD + test + delivery history, thin over [ApiClient]. */
@Singleton
class WebhookRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : WebhookRepository {

    override suspend fun list(): Result<List<Webhook>> =
        apiClient.listWebhooks().mapError()

    override suspend fun create(input: WebhookInput): Result<WebhookCreateResponse> =
        apiClient.createWebhook(input).mapError()

    override suspend fun update(id: Int, input: WebhookInput): Result<Webhook> =
        apiClient.updateWebhook(id, input).mapError()

    override suspend fun delete(id: Int): Result<Unit> =
        apiClient.deleteWebhook(id).mapError()

    override suspend fun test(id: Int): Result<WebhookDelivery> =
        apiClient.testWebhook(id).mapError()

    override suspend fun deliveries(id: Int): Result<List<WebhookDelivery>> =
        apiClient.getWebhookDeliveries(id).mapError()

    private fun <T> Result<T>.mapError(): Result<T> =
        fold(onSuccess = { Result.success(it) }, onFailure = { Result.failure(it.toApiError()) })
}

/** Issue #413 / #573: API token list/create/revoke/rotate, thin over [ApiClient]. */
@Singleton
class ApiTokenRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : ApiTokenRepository {

    override suspend fun list(): Result<List<ApiToken>> =
        apiClient.listApiTokens().mapError()

    override suspend fun create(input: ApiTokenInput): Result<ApiTokenCreateResponse> =
        apiClient.createApiToken(input).mapError()

    override suspend fun revoke(id: Int): Result<Unit> =
        apiClient.revokeApiToken(id).mapError()

    override suspend fun revokeAll(): Result<RevokeAllApiTokensResponse> =
        apiClient.revokeAllApiTokens().mapError()

    override suspend fun rotate(id: Int): Result<ApiTokenCreateResponse> =
        apiClient.rotateApiToken(id).mapError()

    private fun <T> Result<T>.mapError(): Result<T> =
        fold(onSuccess = { Result.success(it) }, onFailure = { Result.failure(it.toApiError()) })
}

/** Issue #390's Android follow-up (#628): calendar (CalDAV/iCal) subscription CRUD + sync-now, thin over [ApiClient]. */
@Singleton
class CalendarSubscriptionRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : CalendarSubscriptionRepository {

    override suspend fun list(): Result<List<CalendarSubscription>> =
        apiClient.listCalendarSubscriptions().mapError()

    override suspend fun create(input: CalendarSubscriptionInput): Result<CalendarSubscription> =
        apiClient.createCalendarSubscription(input).mapError()

    override suspend fun update(id: Int, input: CalendarSubscriptionInput): Result<CalendarSubscription> =
        apiClient.updateCalendarSubscription(id, input).mapError()

    override suspend fun delete(id: Int): Result<Unit> =
        apiClient.deleteCalendarSubscription(id).mapError()

    override suspend fun sync(id: Int): Result<CalendarSyncResult> =
        apiClient.syncCalendarSubscription(id).mapError()

    private fun <T> Result<T>.mapError(): Result<T> =
        fold(onSuccess = { Result.success(it) }, onFailure = { Result.failure(it.toApiError()) })
}

/**
 * Issue #628 scope note 4: contact (CardDAV) subscriptions are read-only on
 * Android — web has no create/edit/delete UI for these either.
 */
@Singleton
class ContactSubscriptionRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : ContactSubscriptionRepository {

    override suspend fun list(): Result<List<ContactSubscription>> =
        apiClient.listContactSubscriptions().mapError()

    private fun <T> Result<T>.mapError(): Result<T> =
        fold(onSuccess = { Result.success(it) }, onFailure = { Result.failure(it.toApiError()) })
}

/** M25: ntfy/Gotify channel config + per-channel test, thin over [ApiClient]. */
@Singleton
class NotificationSettingsRepositoryImpl @Inject constructor(
    private val apiClient: ApiClient,
) : NotificationSettingsRepository {

    override suspend fun config(): Result<NotificationConfig> =
        apiClient.getNotificationConfig().mapError()

    override suspend fun save(input: NotificationConfigInput): Result<NotificationConfig> =
        apiClient.saveNotificationConfig(input).mapError()

    override suspend fun test(channel: String): Result<NotificationTestResult> =
        apiClient.testNotificationChannel(channel).mapError()

    private fun <T> Result<T>.mapError(): Result<T> =
        fold(onSuccess = { Result.success(it) }, onFailure = { Result.failure(it.toApiError()) })
}
