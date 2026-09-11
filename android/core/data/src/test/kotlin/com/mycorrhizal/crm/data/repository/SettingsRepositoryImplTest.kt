package com.mycorrhizal.crm.data.repository

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
import com.mycorrhizal.crm.network.ApiError
import io.mockk.coEvery
import io.mockk.mockk
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WebhookRepositoryImplTest {

    private val apiClient = mockk<ApiClient>()
    private val repository = WebhookRepositoryImpl(apiClient)

    @Test
    fun `list returns the webhooks on success`() = runTest {
        coEvery { apiClient.listWebhooks() } returns Result.success(
            listOf(Webhook(id = 1, name = "Hook 1")),
        )

        val result = repository.list()

        assertTrue(result.isSuccess)
        assertEquals(listOf("Hook 1"), result.getOrThrow().map { it.name })
    }

    @Test
    fun `list failure is normalized through mapError`() = runTest {
        coEvery { apiClient.listWebhooks() } returns Result.failure(ApiError.Server(500, "boom"))

        val result = repository.list()

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }

    @Test
    fun `create forwards the input and returns the created webhook`() = runTest {
        val input = WebhookInput(name = "Hook", url = "https://example.com", events = listOf("contact.created"), isActive = true)
        coEvery { apiClient.createWebhook(input) } returns Result.success(
            WebhookCreateResponse(id = 1, name = "Hook", secret = "s3cr3t"),
        )

        val result = repository.create(input)

        assertTrue(result.isSuccess)
        assertEquals("s3cr3t", result.getOrThrow().secret)
    }

    @Test
    fun `update forwards id and input`() = runTest {
        val input = WebhookInput(name = "Renamed", url = "https://example.com", events = emptyList(), isActive = false)
        coEvery { apiClient.updateWebhook(1, input) } returns Result.success(Webhook(id = 1, name = "Renamed"))

        val result = repository.update(1, input)

        assertTrue(result.isSuccess)
        assertEquals("Renamed", result.getOrThrow().name)
    }

    @Test
    fun `delete delegates to the api client`() = runTest {
        coEvery { apiClient.deleteWebhook(1) } returns Result.success(Unit)

        val result = repository.delete(1)

        assertTrue(result.isSuccess)
    }

    @Test
    fun `test triggers a delivery attempt`() = runTest {
        coEvery { apiClient.testWebhook(1) } returns Result.success(
            WebhookDelivery(id = 9, webhookId = 1, eventType = "test"),
        )

        val result = repository.test(1)

        assertTrue(result.isSuccess)
        assertEquals(9, result.getOrThrow().id)
    }

    @Test
    fun `deliveries returns the delivery history`() = runTest {
        coEvery { apiClient.getWebhookDeliveries(1) } returns Result.success(
            listOf(WebhookDelivery(id = 1, webhookId = 1, eventType = "contact.created")),
        )

        val result = repository.deliveries(1)

        assertTrue(result.isSuccess)
        assertEquals(1, result.getOrThrow().size)
    }
}

class ApiTokenRepositoryImplTest {

    private val apiClient = mockk<ApiClient>()
    private val repository = ApiTokenRepositoryImpl(apiClient)

    @Test
    fun `list returns the tokens on success`() = runTest {
        coEvery { apiClient.listApiTokens() } returns Result.success(
            listOf(ApiToken(id = 1, name = "Sync script")),
        )

        val result = repository.list()

        assertTrue(result.isSuccess)
        assertEquals(listOf("Sync script"), result.getOrThrow().map { it.name })
    }

    @Test
    fun `list failure is normalized through mapError`() = runTest {
        coEvery { apiClient.listApiTokens() } returns Result.failure(ApiError.Server(500, "boom"))

        val result = repository.list()

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }

    @Test
    fun `create forwards the input and returns the plaintext once`() = runTest {
        val input = ApiTokenInput(name = "New token", expiresInDays = 90, scope = "full")
        coEvery { apiClient.createApiToken(input) } returns Result.success(
            ApiTokenCreateResponse(id = 1, name = "New token", token = "s3cr3t"),
        )

        val result = repository.create(input)

        assertTrue(result.isSuccess)
        assertEquals("s3cr3t", result.getOrThrow().token)
    }

    @Test
    fun `revoke delegates to the api client`() = runTest {
        coEvery { apiClient.revokeApiToken(1) } returns Result.success(Unit)

        val result = repository.revoke(1)

        assertTrue(result.isSuccess)
    }

    @Test
    fun `revoke failure is normalized through mapError`() = runTest {
        coEvery { apiClient.revokeApiToken(1) } returns Result.failure(ApiError.Client(404, "not found"))

        val result = repository.revoke(1)

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }

    @Test
    fun `revokeAll returns the revoked count`() = runTest {
        coEvery { apiClient.revokeAllApiTokens() } returns Result.success(RevokeAllApiTokensResponse(revoked = 2))

        val result = repository.revokeAll()

        assertTrue(result.isSuccess)
        assertEquals(2, result.getOrThrow().revoked)
    }

    @Test
    fun `rotate returns the reissued token with a fresh plaintext`() = runTest {
        coEvery { apiClient.rotateApiToken(1) } returns Result.success(
            ApiTokenCreateResponse(id = 8, name = "Sync script", token = "rotated456"),
        )

        val result = repository.rotate(1)

        assertTrue(result.isSuccess)
        assertEquals(8, result.getOrThrow().id)
        assertEquals("rotated456", result.getOrThrow().token)
    }
}

class CalendarSubscriptionRepositoryImplTest {

    private val apiClient = mockk<ApiClient>()
    private val repository = CalendarSubscriptionRepositoryImpl(apiClient)

    @Test
    fun `list returns the calendars on success`() = runTest {
        coEvery { apiClient.listCalendarSubscriptions() } returns Result.success(
            listOf(CalendarSubscription(id = 1, name = "Personal")),
        )

        val result = repository.list()

        assertTrue(result.isSuccess)
        assertEquals(listOf("Personal"), result.getOrThrow().map { it.name })
    }

    @Test
    fun `list failure is normalized through mapError`() = runTest {
        coEvery { apiClient.listCalendarSubscriptions() } returns Result.failure(ApiError.Server(500, "boom"))

        val result = repository.list()

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }

    @Test
    fun `create forwards the input and returns the created subscription`() = runTest {
        val input = CalendarSubscriptionInput(name = "Personal", url = "https://example.com/a.ics")
        coEvery { apiClient.createCalendarSubscription(input) } returns Result.success(
            CalendarSubscription(id = 9, name = "Personal"),
        )

        val result = repository.create(input)

        assertTrue(result.isSuccess)
        assertEquals(9, result.getOrThrow().id)
    }

    @Test
    fun `update forwards id and input`() = runTest {
        val input = CalendarSubscriptionInput(name = "Renamed", url = "https://example.com/a.ics")
        coEvery { apiClient.updateCalendarSubscription(9, input) } returns Result.success(
            CalendarSubscription(id = 9, name = "Renamed"),
        )

        val result = repository.update(9, input)

        assertTrue(result.isSuccess)
        assertEquals("Renamed", result.getOrThrow().name)
    }

    @Test
    fun `delete delegates to the api client`() = runTest {
        coEvery { apiClient.deleteCalendarSubscription(9) } returns Result.success(Unit)

        val result = repository.delete(9)

        assertTrue(result.isSuccess)
    }

    @Test
    fun `sync returns the run tallies`() = runTest {
        coEvery { apiClient.syncCalendarSubscription(9) } returns Result.success(
            CalendarSyncResult(created = 2, updated = 1, skipped = 0),
        )

        val result = repository.sync(9)

        assertTrue(result.isSuccess)
        assertEquals(2, result.getOrThrow().created)
    }

    @Test
    fun `sync failure is normalized through mapError`() = runTest {
        coEvery { apiClient.syncCalendarSubscription(9) } returns Result.failure(ApiError.Client(400, "unreachable"))

        val result = repository.sync(9)

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }
}

class ContactSubscriptionRepositoryImplTest {

    private val apiClient = mockk<ApiClient>()
    private val repository = ContactSubscriptionRepositoryImpl(apiClient)

    @Test
    fun `list returns the contact subscriptions on success`() = runTest {
        coEvery { apiClient.listContactSubscriptions() } returns Result.success(
            listOf(ContactSubscription(id = 1, name = "Address Book")),
        )

        val result = repository.list()

        assertTrue(result.isSuccess)
        assertEquals(listOf("Address Book"), result.getOrThrow().map { it.name })
    }

    @Test
    fun `list failure is normalized through mapError`() = runTest {
        coEvery { apiClient.listContactSubscriptions() } returns Result.failure(ApiError.Server(500, "boom"))

        val result = repository.list()

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }
}

class NotificationSettingsRepositoryImplTest {

    private val apiClient = mockk<ApiClient>()
    private val repository = NotificationSettingsRepositoryImpl(apiClient)

    @Test
    fun `config returns the current settings`() = runTest {
        coEvery { apiClient.getNotificationConfig() } returns Result.success(
            NotificationConfig(ntfyUrl = "https://ntfy.sh", notifyNtfy = true),
        )

        val result = repository.config()

        assertTrue(result.isSuccess)
        assertTrue(result.getOrThrow().notifyNtfy)
    }

    @Test
    fun `save forwards the input`() = runTest {
        val input = NotificationConfigInput(ntfyUrl = "https://ntfy.sh", notifyNtfy = true)
        coEvery { apiClient.saveNotificationConfig(input) } returns Result.success(
            NotificationConfig(ntfyUrl = "https://ntfy.sh", notifyNtfy = true),
        )

        val result = repository.save(input)

        assertTrue(result.isSuccess)
    }

    @Test
    fun `test surfaces a failed channel test`() = runTest {
        coEvery { apiClient.testNotificationChannel("ntfy") } returns Result.success(
            NotificationTestResult(ok = false, error = "unauthorized"),
        )

        val result = repository.test("ntfy")

        assertTrue(result.isSuccess)
        assertEquals("unauthorized", result.getOrThrow().error)
    }

    @Test
    fun `test failure is normalized through mapError`() = runTest {
        coEvery { apiClient.testNotificationChannel("gotify") } returns Result.failure(ApiError.Client(401, "unauthorized"))

        val result = repository.test("gotify")

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }
}
