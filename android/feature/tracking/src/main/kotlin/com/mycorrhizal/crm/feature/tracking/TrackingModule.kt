package com.mycorrhizal.crm.feature.tracking

import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

/**
 * M5 §5a (issue #152): DI bindings for the tracking/push module.
 */
@Module
@InstallIn(SingletonComponent::class)
abstract class TrackingModule {

    @Binds
    abstract fun bindDeviceRegistrationStore(
        impl: SharedPrefsDeviceRegistrationStore,
    ): DeviceRegistrationStore

    // Issue #721: the OS-grant query seam and the grant-time catch-up enqueue
    // seam, both consumed by the Settings feature's ViewModel.
    @Binds
    abstract fun bindPermissionChecker(impl: AndroidPermissionChecker): PermissionChecker

    @Binds
    abstract fun bindTrackingCatchUpScheduler(
        impl: TrackingCatchUpSchedulerImpl,
    ): TrackingCatchUpScheduler
}

@Module
@InstallIn(SingletonComponent::class)
object TrackingProvidesModule {

    @Provides
    fun provideFcmTokenSource(impl: FirebaseFcmTokenSource): FcmTokenSource = impl

    @Provides
    fun provideFcmAvailability(impl: FirebaseFcmAvailability): FcmAvailability = impl
}
