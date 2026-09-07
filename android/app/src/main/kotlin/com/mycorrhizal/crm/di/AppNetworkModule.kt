package com.mycorrhizal.crm.di

import com.mycorrhizal.crm.BuildConfig
import com.mycorrhizal.crm.network.BaseUrlProvider
import com.mycorrhizal.crm.network.ClientVersionProvider
import com.mycorrhizal.crm.network.NetworkFactory
import com.mycorrhizal.crm.network.SessionExpiryInterceptor
import com.mycorrhizal.crm.network.SessionExpiryNotifier
import com.mycorrhizal.crm.network.TokenProvider
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import okhttp3.OkHttpClient
import javax.inject.Singleton

/**
 * Provides the application-scoped OkHttpClient with the ticket §2.2
 * interceptor chain. The ACTUAL order is BaseUrl → Auth → Retry (then Logging
 * in debug) — see NetworkFactory; BaseUrl must run before Auth so Auth's host
 * check sees the rewritten URL. The token and base-URL providers come from the
 * SessionManager (wired in core:data). This exact ordering is load-bearing for
 * M5 §3.1 (Coil reuses this client for photo URLs).
 *
 * Issue #678: the SessionExpiryInterceptor (registered after Retry, before the
 * debug logging interceptor) watches every response for a 401 and signals
 * SessionExpiryNotifier; the session wiring clears the session on that signal
 * so the app lands back on the auth flow.
 */
@Module
@InstallIn(SingletonComponent::class)
object AppNetworkModule {
    @Provides
    @Singleton
    fun provideOkHttpClient(
        tokenProvider: TokenProvider,
        baseUrlProvider: BaseUrlProvider,
        sessionExpiryNotifier: SessionExpiryNotifier,
    ): OkHttpClient = NetworkFactory.okHttpClient(
        tokenProvider = tokenProvider,
        baseUrlProvider = baseUrlProvider,
        debug = BuildConfig.DEBUG,
        sessionExpiryInterceptor = SessionExpiryInterceptor(sessionExpiryNotifier, baseUrlProvider),
        // Issue #692: the app advertises its versionName on every API request
        // so the server can log it and enforce its MIN_CLIENT_VERSION floor at
        // authentication. BuildConfig is readable here because this module is
        // in :app; the interceptor keeps the header off non-API hosts.
        clientVersionProvider = ClientVersionProvider { BuildConfig.VERSION_NAME },
    )
}
