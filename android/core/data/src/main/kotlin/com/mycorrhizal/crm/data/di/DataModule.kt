package com.mycorrhizal.crm.data.di

import com.mycorrhizal.crm.data.auth.AndroidLocalAuthCapabilities
import com.mycorrhizal.crm.data.local.AppDatabase
import com.mycorrhizal.crm.data.local.CachedActivityDao
import com.mycorrhizal.crm.data.local.CachedCadencePolicyDao
import com.mycorrhizal.crm.data.local.CachedCircleDao
import com.mycorrhizal.crm.data.local.CachedCircleMemberDao
import com.mycorrhizal.crm.data.local.CachedContactDao
import com.mycorrhizal.crm.data.local.CustomLinkActionDao
import com.mycorrhizal.crm.data.local.CachedContactTagDao
import com.mycorrhizal.crm.data.local.CachedConversationAgendaDao
import com.mycorrhizal.crm.data.local.CachedGiftDao
import com.mycorrhizal.crm.data.local.CachedHouseholdDao
import com.mycorrhizal.crm.data.local.CachedHouseholdMemberDao
import com.mycorrhizal.crm.data.local.CachedLifeEventDao
import com.mycorrhizal.crm.data.local.CachedNoteDao
import com.mycorrhizal.crm.data.local.CachedPreferenceDao
import com.mycorrhizal.crm.data.local.CachedRelationshipEdgeDao
import com.mycorrhizal.crm.data.local.CachedReminderDao
import com.mycorrhizal.crm.data.local.CachedTagDao
import com.mycorrhizal.crm.data.local.LocalDataCleaner
import com.mycorrhizal.crm.data.local.PendingInteractionDao
import com.mycorrhizal.crm.data.local.RoomCacheEncryption
import com.mycorrhizal.crm.data.local.RoomPassphraseStore
import com.mycorrhizal.crm.data.repository.ActivityRepositoryImpl
import com.mycorrhizal.crm.data.repository.AuditRepositoryImpl
import com.mycorrhizal.crm.data.repository.AuthRepositoryImpl
import com.mycorrhizal.crm.data.repository.SystemEventRepositoryImpl
import com.mycorrhizal.crm.data.repository.CadencePolicyRepositoryImpl
import com.mycorrhizal.crm.data.repository.CircleRepositoryImpl
import com.mycorrhizal.crm.data.repository.CustomLinkActionRepositoryImpl
import com.mycorrhizal.crm.data.repository.ExternalIdentityRepositoryImpl
import com.mycorrhizal.crm.data.repository.ImmichRepositoryImpl
import com.mycorrhizal.crm.data.repository.NextcloudRepositoryImpl
import com.mycorrhizal.crm.data.repository.PaperlessRepositoryImpl
import com.mycorrhizal.crm.data.repository.SeafileRepositoryImpl
import com.mycorrhizal.crm.data.repository.ContactRepositoryImpl
import com.mycorrhizal.crm.data.repository.ContactShareRepositoryImpl
import com.mycorrhizal.crm.data.repository.ConversationAgendaRepositoryImpl
import com.mycorrhizal.crm.data.repository.GiftRepositoryImpl
import com.mycorrhizal.crm.data.repository.GraphRepositoryImpl
import com.mycorrhizal.crm.data.repository.HouseholdRepositoryImpl
import com.mycorrhizal.crm.data.repository.LifeEventRepositoryImpl
import com.mycorrhizal.crm.data.repository.MergeRepositoryImpl
import com.mycorrhizal.crm.data.repository.NoteRepositoryImpl
import com.mycorrhizal.crm.data.repository.PreferenceRepositoryImpl
import com.mycorrhizal.crm.data.repository.BulkOperationRepositoryImpl
import com.mycorrhizal.crm.data.repository.PendingInteractionRepositoryImpl
import com.mycorrhizal.crm.data.repository.ReminderRepositoryImpl
import com.mycorrhizal.crm.data.repository.RelationshipEdgeRepositoryImpl
import com.mycorrhizal.crm.data.repository.TagRepositoryImpl
import com.mycorrhizal.crm.data.repository.TrackingSettingsRepositoryImpl
import com.mycorrhizal.crm.data.repository.NotificationSettingsRepositoryImpl
import com.mycorrhizal.crm.data.repository.AppSettingsRepositoryImpl
import com.mycorrhizal.crm.data.repository.UserManagementRepositoryImpl
import com.mycorrhizal.crm.data.repository.WebhookRepositoryImpl
import com.mycorrhizal.crm.data.repository.ApiTokenRepositoryImpl
import com.mycorrhizal.crm.data.repository.LocalAuthSettingsRepositoryImpl
import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.DefaultAppLockController
import com.mycorrhizal.crm.data.session.DefaultSessionManager
import com.mycorrhizal.crm.data.session.SessionDataCleaner
import com.mycorrhizal.crm.data.session.SessionExpiryWiring
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.data.session.SessionPrefsStorage
import com.mycorrhizal.crm.data.session.TokenStorage
import com.mycorrhizal.crm.domain.repository.ActivityRepository
import com.mycorrhizal.crm.domain.repository.AuditRepository
import com.mycorrhizal.crm.domain.repository.AuthRepository
import com.mycorrhizal.crm.domain.repository.SystemEventRepository
import com.mycorrhizal.crm.domain.repository.BulkOperationRepository
import com.mycorrhizal.crm.domain.repository.CadencePolicyRepository
import com.mycorrhizal.crm.domain.repository.CircleRepository
import com.mycorrhizal.crm.domain.repository.CustomLinkActionRepository
import com.mycorrhizal.crm.domain.repository.ExternalIdentityRepository
import com.mycorrhizal.crm.domain.repository.ImmichRepository
import com.mycorrhizal.crm.domain.repository.NextcloudRepository
import com.mycorrhizal.crm.domain.repository.PaperlessRepository
import com.mycorrhizal.crm.domain.repository.SeafileRepository
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.ContactShareRepository
import com.mycorrhizal.crm.domain.repository.ConversationAgendaRepository
import com.mycorrhizal.crm.domain.repository.GiftRepository
import com.mycorrhizal.crm.domain.repository.GraphRepository
import com.mycorrhizal.crm.domain.repository.HouseholdRepository
import com.mycorrhizal.crm.domain.repository.LifeEventRepository
import com.mycorrhizal.crm.domain.repository.MergeRepository
import com.mycorrhizal.crm.domain.repository.NoteRepository
import com.mycorrhizal.crm.domain.repository.PendingInteractionRepository
import com.mycorrhizal.crm.domain.repository.PreferenceRepository
import com.mycorrhizal.crm.domain.repository.ReminderRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.domain.repository.TagRepository
import com.mycorrhizal.crm.domain.repository.TrackingSettingsRepository
import com.mycorrhizal.crm.domain.repository.NotificationSettingsRepository
import com.mycorrhizal.crm.domain.repository.AppSettingsRepository
import com.mycorrhizal.crm.domain.repository.WebhookRepository
import com.mycorrhizal.crm.domain.repository.ApiTokenRepository
import com.mycorrhizal.crm.domain.repository.UserManagementRepository
import com.mycorrhizal.crm.domain.repository.LocalAuthCapabilities
import com.mycorrhizal.crm.domain.repository.LocalAuthSettingsRepository
import com.mycorrhizal.crm.network.ApiClient
import com.mycorrhizal.crm.network.BaseUrlProvider
import com.mycorrhizal.crm.network.NetworkFactory
import com.mycorrhizal.crm.network.SessionExpiryNotifier
import com.mycorrhizal.crm.network.TokenProvider
import androidx.room.Room
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import com.squareup.moshi.Moshi
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import net.zetetic.database.sqlcipher.SupportOpenHelperFactory
import javax.inject.Singleton

/**
 * Core dependency graph. Binding at the lowest scope that covers each
 * dependency's lifecycle, per ticket §1.4:
 *  - @Singleton: OkHttp/ApiClient, Room database, session manager
 *  - Repositories are @Singleton here too — they hold no mutable UI state
 *    and are stateless wrappers over Api + Dao (a later phase may narrow to
 *    @ViewModelScoped; the ticket's recommendation is noted there).
 */
@Module
@InstallIn(SingletonComponent::class)
object DataModule {

    /** Issue #385: the Room mirror file name (must match AppDatabase's builder). */
    const val DB_NAME = "mycorrhizal-cache.db"

    @Provides
    @Singleton
    fun provideMoshi(): Moshi = NetworkFactory.moshi()

    @Provides
    @Singleton
    fun provideApiClient(okHttpClient: okhttp3.OkHttpClient, moshi: Moshi): ApiClient =
        ApiClient(okHttpClient, moshi)

    @Provides
    @Singleton
    // Room's Builder.addMigrations is vararg-only (the List-taking overload lives on the
    // unrelated MigrationContainer.Builder, not this one), so spreading REGISTERED_MIGRATIONS
    // is unavoidable here — a one-time call on the DB-open path, not a hot loop.
    @Suppress("SpreadOperator")
    fun provideDatabase(
        @ApplicationContext context: android.content.Context,
        passphraseStore: RoomPassphraseStore,
    ): AppDatabase {
        // Issue #385: the Room mirror (offline PII) is SQLCipher-encrypted. On
        // first launch of an encrypted build this also re-encrypts a
        // pre-existing plaintext DB in place (header-checked; see
        // RoomCacheEncryption) before Room opens it.
        val dbFile = context.getDatabasePath(DB_NAME)
        val passphrase = passphraseStore.getOrCreate()
        RoomCacheEncryption.ensureEncrypted(dbFile, passphrase)
        return Room.databaseBuilder(
            context,
            AppDatabase::class.java,
            DB_NAME,
        )
            .openHelperFactory(SupportOpenHelperFactory(passphrase.toByteArray()))
            // T76/#480: REGISTERED_MIGRATIONS (Migrations.kt) is the single source of truth
            // for which version bumps have a hand-written migration — so pending_interactions
            // (a real not-yet-synced outbox) survives those bumps; the destructive fallback
            // remains for any other/unexpected version gap, per this cache's general
            // rebuild-from-server policy (see AppDatabase's doc comment).
            // MigrationVersionCoverageTest guards against a future gap being left unregistered
            // by accident.
            .addMigrations(*com.mycorrhizal.crm.data.local.REGISTERED_MIGRATIONS.toTypedArray()) // # pragma: no cover — provideDatabase needs a real Android Keystore (RoomPassphraseStore); see RoomEncryptionGuardTest
            .fallbackToDestructiveMigration()
            .build()
    }

    @Provides
    @Singleton
    fun provideRoomPassphraseStore(@ApplicationContext context: android.content.Context): RoomPassphraseStore =
        RoomPassphraseStore(context)

    @Provides
    @Singleton
    fun provideSessionDataCleaner(
        @ApplicationContext context: android.content.Context,
        database: AppDatabase,
    ): SessionDataCleaner = LocalDataCleaner(context, database)

    @Provides
    fun provideCachedContactDao(db: AppDatabase): CachedContactDao = db.cachedContactDao()

    @Provides
    fun provideCachedActivityDao(db: AppDatabase): CachedActivityDao = db.cachedActivityDao()

    @Provides
    fun provideCachedNoteDao(db: AppDatabase): CachedNoteDao = db.cachedNoteDao()

    @Provides
    fun provideCachedReminderDao(db: AppDatabase): CachedReminderDao = db.cachedReminderDao()

    @Provides
    fun provideCachedCircleDao(db: AppDatabase): CachedCircleDao = db.cachedCircleDao()

    @Provides
    fun provideCachedCircleMemberDao(db: AppDatabase): CachedCircleMemberDao =
        db.cachedCircleMemberDao()

    @Provides
    fun provideCachedTagDao(db: AppDatabase): CachedTagDao = db.cachedTagDao()

    @Provides
    fun provideCachedContactTagDao(db: AppDatabase): CachedContactTagDao =
        db.cachedContactTagDao()

    @Provides
    fun provideCachedHouseholdDao(db: AppDatabase): CachedHouseholdDao =
        db.cachedHouseholdDao()

    @Provides
    fun provideCachedHouseholdMemberDao(db: AppDatabase): CachedHouseholdMemberDao =
        db.cachedHouseholdMemberDao()

    @Provides
    fun provideCachedRelationshipEdgeDao(db: AppDatabase): CachedRelationshipEdgeDao =
        db.cachedRelationshipEdgeDao()

    @Provides
    fun provideCachedLifeEventDao(db: AppDatabase): CachedLifeEventDao = db.cachedLifeEventDao()

    @Provides
    fun provideCachedGiftDao(db: AppDatabase): CachedGiftDao = db.cachedGiftDao()

    @Provides
    fun provideCachedPreferenceDao(db: AppDatabase): CachedPreferenceDao = db.cachedPreferenceDao()

    @Provides
    fun provideCachedConversationAgendaDao(db: AppDatabase): CachedConversationAgendaDao =
        db.cachedConversationAgendaDao()

    @Provides
    fun provideCachedCadencePolicyDao(db: AppDatabase): CachedCadencePolicyDao =
        db.cachedCadencePolicyDao()

    @Provides
    fun providePendingInteractionDao(db: AppDatabase): PendingInteractionDao =
        db.pendingInteractionDao()

    @Provides
    fun provideCustomLinkActionDao(db: AppDatabase): CustomLinkActionDao =
        db.customLinkActionDao()

    @Provides
    @Singleton
    fun provideSessionExpiryNotifier(): SessionExpiryNotifier = SessionExpiryNotifier()

    @Provides
    @Singleton
    fun provideSessionManager(
        tokenStorage: TokenStorage,
        prefsStorage: SessionPrefsStorage,
        localDataCleaner: SessionDataCleaner,
        sessionExpiryNotifier: SessionExpiryNotifier,
    ): DefaultSessionManager {
        val manager = DefaultSessionManager(tokenStorage, prefsStorage, localDataCleaner)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        // Issue #678: a 401 on any API call must clear the session so the app
        // lands on the auth flow rather than a stuck or half-rendered screen.
        // The wiring is a plain class so the behavior is unit-tested.
        SessionExpiryWiring(sessionExpiryNotifier, manager).start(scope)
        // Hydrate the stored JWT/server URL into memory asynchronously so a
        // returning user is already logged in on launch (H3 review fix).
        scope.launch { manager.init() }
        return manager
    }

    // Issue #722: the app-lock gate controller + its process-lifecycle wiring.
    // ProcessLifecycleOwner (not the Activity's lifecycle) is the source of
    // background/foreground events so launching one of the app's own
    // activities (e.g. the uCrop crop screen) never counts as backgrounding.
    @Provides
    @Singleton
    fun provideAppLockController(
        localAuthSettings: LocalAuthSettingsRepository,
        sessionManager: SessionManager,
    ): DefaultAppLockController {
        val controller = DefaultAppLockController(localAuthSettings, sessionManager)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        controller.start(scope)
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) = controller.onAppForegrounded()
            override fun onStop(owner: LifecycleOwner) = controller.onAppBackgrounded()
        })
        return controller
    }
}

@Module
@InstallIn(SingletonComponent::class)
abstract class DataBindsModule {
    @Binds
    @Singleton
    abstract fun bindSessionManager(impl: DefaultSessionManager): SessionManager

    @Binds
    @Singleton
    abstract fun bindTokenProvider(impl: SessionManager): TokenProvider

    @Binds
    @Singleton
    abstract fun bindBaseUrlProvider(impl: SessionManager): BaseUrlProvider

    @Binds
    @Singleton
    abstract fun bindAuthRepository(impl: AuthRepositoryImpl): AuthRepository

    @Binds
    @Singleton
    abstract fun bindAuditRepository(impl: AuditRepositoryImpl): AuditRepository

    @Binds
    @Singleton
    abstract fun bindSystemEventRepository(impl: SystemEventRepositoryImpl): SystemEventRepository

    @Binds
    @Singleton
    abstract fun bindContactRepository(impl: ContactRepositoryImpl): ContactRepository

    @Binds
    @Singleton
    abstract fun bindActivityRepository(impl: ActivityRepositoryImpl): ActivityRepository

    @Binds
    @Singleton
    abstract fun bindNoteRepository(impl: NoteRepositoryImpl): NoteRepository

    @Binds
    @Singleton
    abstract fun bindReminderRepository(impl: ReminderRepositoryImpl): ReminderRepository

    @Binds
    @Singleton
    abstract fun bindCircleRepository(impl: CircleRepositoryImpl): CircleRepository

    @Binds
    @Singleton
    abstract fun bindTagRepository(impl: TagRepositoryImpl): TagRepository

    @Binds
    @Singleton
    abstract fun bindHouseholdRepository(impl: HouseholdRepositoryImpl): HouseholdRepository

    @Binds
    @Singleton
    abstract fun bindRelationshipEdgeRepository(impl: RelationshipEdgeRepositoryImpl): RelationshipEdgeRepository

    @Binds
    @Singleton
    abstract fun bindLifeEventRepository(impl: LifeEventRepositoryImpl): LifeEventRepository

    @Binds
    @Singleton
    abstract fun bindGiftRepository(impl: GiftRepositoryImpl): GiftRepository

    @Binds
    @Singleton
    abstract fun bindGraphRepository(impl: GraphRepositoryImpl): GraphRepository

    @Binds
    @Singleton
    abstract fun bindPreferenceRepository(impl: PreferenceRepositoryImpl): PreferenceRepository

    @Binds
    @Singleton
    abstract fun bindConversationAgendaRepository(impl: ConversationAgendaRepositoryImpl): ConversationAgendaRepository

    @Binds
    @Singleton
    abstract fun bindCadencePolicyRepository(impl: CadencePolicyRepositoryImpl): CadencePolicyRepository

    @Binds
    @Singleton
    abstract fun bindMergeRepository(impl: MergeRepositoryImpl): MergeRepository

    @Binds
    @Singleton
    abstract fun bindBulkOperationRepository(impl: BulkOperationRepositoryImpl): BulkOperationRepository

    @Binds
    @Singleton
    abstract fun bindPendingInteractionRepository(impl: PendingInteractionRepositoryImpl): PendingInteractionRepository

    @Binds
    @Singleton
    abstract fun bindCustomLinkActionRepository(impl: CustomLinkActionRepositoryImpl): CustomLinkActionRepository

    @Binds
    @Singleton
    abstract fun bindExternalIdentityRepository(impl: ExternalIdentityRepositoryImpl): ExternalIdentityRepository

    @Binds
    @Singleton
    abstract fun bindImmichRepository(impl: ImmichRepositoryImpl): ImmichRepository

    @Binds
    @Singleton
    abstract fun bindPaperlessRepository(impl: PaperlessRepositoryImpl): PaperlessRepository

    @Binds
    @Singleton
    abstract fun bindSeafileRepository(impl: SeafileRepositoryImpl): SeafileRepository

    @Binds
    @Singleton
    abstract fun bindNextcloudRepository(impl: NextcloudRepositoryImpl): NextcloudRepository

    @Binds
    @Singleton
    abstract fun bindTrackingSettingsRepository(impl: TrackingSettingsRepositoryImpl): TrackingSettingsRepository

    @Binds
    @Singleton
    abstract fun bindContactShareRepository(impl: ContactShareRepositoryImpl): ContactShareRepository

    @Binds
    @Singleton
    abstract fun bindWebhookRepository(impl: WebhookRepositoryImpl): WebhookRepository

    @Binds
    @Singleton
    abstract fun bindApiTokenRepository(impl: ApiTokenRepositoryImpl): ApiTokenRepository

    @Binds
    @Singleton
    abstract fun bindNotificationSettingsRepository(impl: NotificationSettingsRepositoryImpl): NotificationSettingsRepository

    @Binds
    @Singleton
    abstract fun bindAppSettingsRepository(impl: AppSettingsRepositoryImpl): AppSettingsRepository

    @Binds
    @Singleton
    abstract fun bindUserManagementRepository(impl: UserManagementRepositoryImpl): UserManagementRepository

    @Binds
    @Singleton
    abstract fun bindLocalAuthSettingsRepository(
        impl: LocalAuthSettingsRepositoryImpl,
    ): LocalAuthSettingsRepository

    @Binds
    @Singleton
    abstract fun bindLocalAuthCapabilities(impl: AndroidLocalAuthCapabilities): LocalAuthCapabilities

    @Binds
    @Singleton
    abstract fun bindAppLockController(impl: DefaultAppLockController): AppLockController
}
