package com.mycorrhizal.crm.network

import com.mycorrhizal.crm.model.network.ServerHealth
import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

// Issue #528: GET /health parsing. The endpoint is public and unversioned, and
// the check must fail open — the client only reads the compatibility fields.
class HealthEndpointTest {

    private lateinit var server: MockWebServer
    private lateinit var client: ApiClient

    @Before
    fun setup() {
        server = MockWebServer()
        server.start()
        val okHttp = OkHttpClient.Builder()
            .addInterceptor(
                BaseUrlInterceptor(BaseUrlProvider { server.url("/").toString().trimEnd('/') }),
            )
            .build()
        val moshi = Moshi.Builder()
            .add(KotlinJsonAdapterFactory())
            .build()
        client = ApiClient(okHttp, moshi)
    }

    @After
    fun teardown() {
        server.shutdown()
    }

    @Test
    fun `getHealth hits the unversioned health path`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"status":"healthy","version":"0.6.10","api_contract_version":"v1"}"""),
        )

        val result = client.getHealth()

        assertTrue(result.isSuccess)
        assertEquals("/health", server.takeRequest().path)
    }

    @Test
    fun `getHealth parses the compatibility fields`() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody(
                    """{"status":"healthy","version":"0.6.10","commit":"abc1234",
                        "api_contract_version":"v1","min_client_version":"0.6.0"}""",
                ),
        )

        val health = client.getHealth().getOrThrow()

        assertEquals("0.6.10", health.version)
        assertEquals("v1", health.apiContractVersion)
        assertEquals("0.6.0", health.minClientVersion)
    }

    @Test
    fun `getHealth tolerates the pre-contract server that omits the fields`() = runBlocking {
        // A server older than this feature returns no min_client_version /
        // api_contract_version — that is "no floor declared", not an error.
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"status":"healthy","version":"0.5.0"}"""),
        )

        val health = client.getHealth().getOrThrow()

        assertEquals("0.5.0", health.version)
        assertNull(health.minClientVersion)
        assertNull(health.apiContractVersion)
    }

    @Test
    fun `getHealth parses an empty response body as a failure`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(200).setBody(""))

        val result = client.getHealth()

        assertTrue(result.isFailure)
    }

    @Test
    fun `getHealth maps an http error to a typed ApiError`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(503).setBody("""{"status":"unhealthy"}"""))

        val result = client.getHealth()

        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }

    @Test
    fun `getHealth fails open on a network error`() = runBlocking {
        server.shutdown()

        val result = client.getHealth()

        // The caller resolves Result.failure to "compatible"; the transport
        // error must surface as a typed failure, never a crash.
        assertTrue(result.isFailure)
        assertTrue(result.exceptionOrNull() is ApiError)
    }
}
