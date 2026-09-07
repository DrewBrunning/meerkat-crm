package com.mycorrhizal.crm.network

import com.mycorrhizal.crm.model.MoshiProvider
import com.squareup.moshi.Moshi
import okhttp3.OkHttpClient
import java.util.concurrent.TimeUnit

/**
 * Builds the OkHttp stack and Moshi instance. Interceptor order:
 * BaseUrl → Auth → Retry, with HTTP logging appended for debug builds.
 *
 * OkHttp runs interceptors in registration order. BaseUrl must run before
 * Auth: Auth's host check compares the request URL against the configured
 * server, so it has to see the rewritten URL (requests are built against a
 * placeholder origin) rather than the placeholder.
 */
object NetworkFactory {
    const val CONNECT_TIMEOUT_SECONDS = 30L
    const val READ_TIMEOUT_SECONDS = 30L

    fun moshi(): Moshi = MoshiProvider.get()

    fun okHttpClient(
        tokenProvider: TokenProvider,
        baseUrlProvider: BaseUrlProvider,
        debug: Boolean = false,
        sessionExpiryInterceptor: SessionExpiryInterceptor? = null,
        clientVersionProvider: ClientVersionProvider? = null,
    ): OkHttpClient {
        val builder = OkHttpClient.Builder()
            .addInterceptor(BaseUrlInterceptor(baseUrlProvider))
            // Issue #692: the version header must see the rewritten URL (its
            // host check compares against the configured server), so it runs
            // after BaseUrl and before Auth, exactly like AuthInterceptor.
            .addInterceptor(
                ClientVersionInterceptor(
                    versionProvider = clientVersionProvider ?: ClientVersionProvider { null },
                    baseUrlProvider = baseUrlProvider,
                ),
            )
            .addInterceptor(AuthInterceptor(tokenProvider, baseUrlProvider))
            .addInterceptor(RetryInterceptor())
            .connectTimeout(CONNECT_TIMEOUT_SECONDS, TimeUnit.SECONDS)
            .readTimeout(READ_TIMEOUT_SECONDS, TimeUnit.SECONDS)

        // Issue #678: 401 detection is optional — callers that own the session
        // (the app) pass it in; library/test callers that don't care about
        // session expiry can omit it.
        if (sessionExpiryInterceptor != null) {
            builder.addInterceptor(sessionExpiryInterceptor)
        }

        if (debug) {
            builder.addInterceptor(
                okhttp3.logging.HttpLoggingInterceptor().apply {
                    level = okhttp3.logging.HttpLoggingInterceptor.Level.BASIC
                },
            )
        }
        return builder.build()
    }
}
