package com.mycorrhizal.crm.network

import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.Interceptor
import okhttp3.Response

/** Supplies the app's own versionName synchronously (issue #692). */
fun interface ClientVersionProvider {
    fun versionName(): String?
}

/**
 * Adds the app's own versionName as `X-Client-Version` on every request to the
 * configured API server (issue #692). This is the one-direction auth exchange
 * the compatibility policy calls for: the client tells the server its version,
 * and the server logs it and refuses authentication when the version is below
 * its configured MIN_CLIENT_VERSION floor — the authoritative backstop for a
 * client that skips its own client-side gate.
 *
 * The header is only attached when the request targets the configured server
 * origin (the same host check [AuthInterceptor] uses): this OkHttpClient is
 * shared with Coil for image loading, and the app's version must not leak to
 * arbitrary external image hosts.
 *
 * A blank value is omitted entirely rather than sent as an empty string — the
 * server treats a present-but-invalid value as a rejected client once a floor
 * is declared, so an unstamped build must not advertise a value it cannot back.
 */
class ClientVersionInterceptor(
    private val versionProvider: ClientVersionProvider,
    private val baseUrlProvider: BaseUrlProvider,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        if (request.header(CLIENT_VERSION_HEADER) != null) {
            return chain.proceed(request)
        }
        val version = versionProvider.versionName()
        if (version.isNullOrBlank()) {
            return chain.proceed(request)
        }
        val base = baseUrlProvider.baseUrl().toHttpUrlOrNull()
        if (base == null || request.url.host != base.host) {
            return chain.proceed(request)
        }
        return chain.proceed(
            request.newBuilder().header(CLIENT_VERSION_HEADER, version).build(),
        )
    }

    companion object {
        /** Mirrors backend/middleware/client_version.go's ClientVersionHeader. */
        const val CLIENT_VERSION_HEADER = "X-Client-Version"
    }
}
