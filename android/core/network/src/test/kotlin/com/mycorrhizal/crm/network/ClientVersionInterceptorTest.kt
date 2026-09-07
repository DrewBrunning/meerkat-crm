package com.mycorrhizal.crm.network

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test

class ClientVersionInterceptorTest {

    private lateinit var server: MockWebServer

    @Before
    fun setup() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun teardown() {
        server.shutdown()
    }

    private fun interceptor(version: String?): ClientVersionInterceptor = ClientVersionInterceptor(
        versionProvider = ClientVersionProvider { version },
        baseUrlProvider = BaseUrlProvider { server.url("/").toString().trimEnd('/') },
    )

    @Test
    fun `adds the X-Client-Version header when a version is available`() {
        val client = OkHttpClient.Builder().addInterceptor(interceptor("0.6.10")).build()
        server.enqueue(MockResponse().setResponseCode(200))

        client.newCall(Request.Builder().url(server.url("/test")).build()).execute()

        val recorded = server.takeRequest()
        assertEquals("0.6.10", recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }

    @Test
    fun `omits the header when no version is available`() {
        val client = OkHttpClient.Builder().addInterceptor(interceptor(null)).build()
        server.enqueue(MockResponse().setResponseCode(200))

        client.newCall(Request.Builder().url(server.url("/test")).build()).execute()

        val recorded = server.takeRequest()
        assertNull(recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }

    @Test
    fun `omits the header when the version is blank rather than sending an invalid value`() {
        val client = OkHttpClient.Builder().addInterceptor(interceptor("  ")).build()
        server.enqueue(MockResponse().setResponseCode(200))

        client.newCall(Request.Builder().url(server.url("/test")).build()).execute()

        val recorded = server.takeRequest()
        assertNull(recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }

    @Test
    fun `does not overwrite an existing header`() {
        val client = OkHttpClient.Builder().addInterceptor(interceptor("0.6.10")).build()
        server.enqueue(MockResponse().setResponseCode(200))

        val request = Request.Builder()
            .url(server.url("/test"))
            .header(ClientVersionInterceptor.CLIENT_VERSION_HEADER, "0.1.0")
            .build()
        client.newCall(request).execute()

        val recorded = server.takeRequest()
        assertEquals("0.1.0", recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }

    @Test
    fun `omits the header when the request targets a different host`() {
        // Coil reuses this client for external image hosts; the app version
        // must not leak to a host other than the configured server.
        val interceptor = ClientVersionInterceptor(
            versionProvider = ClientVersionProvider { "0.6.10" },
            baseUrlProvider = BaseUrlProvider { "https://configured.example.com" },
        )
        val client = OkHttpClient.Builder().addInterceptor(interceptor).build()
        server.enqueue(MockResponse().setResponseCode(200))

        client.newCall(Request.Builder().url(server.url("/x")).build()).execute()

        val recorded = server.takeRequest()
        assertNull(recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }

    @Test
    fun `omits the header when the base url is not configured`() {
        val interceptor = ClientVersionInterceptor(
            versionProvider = ClientVersionProvider { "0.6.10" },
            baseUrlProvider = BaseUrlProvider { "" },
        )
        val client = OkHttpClient.Builder().addInterceptor(interceptor).build()
        server.enqueue(MockResponse().setResponseCode(200))

        client.newCall(Request.Builder().url(server.url("/x")).build()).execute()

        val recorded = server.takeRequest()
        assertNull(recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }

    @Test
    fun `NetworkFactory wires the header provider into the built client`() {
        val client = NetworkFactory.okHttpClient(
            tokenProvider = TokenProvider { null },
            baseUrlProvider = BaseUrlProvider { server.url("/").toString().trimEnd('/') },
            clientVersionProvider = ClientVersionProvider { "0.6.10" },
        )
        server.enqueue(MockResponse().setResponseCode(200))

        client.newCall(Request.Builder().url(server.url("/health")).build()).execute()

        val recorded = server.takeRequest()
        assertEquals("0.6.10", recorded.getHeader(ClientVersionInterceptor.CLIENT_VERSION_HEADER))
    }
}
