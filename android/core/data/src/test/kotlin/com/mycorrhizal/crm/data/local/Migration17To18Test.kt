package com.mycorrhizal.crm.data.local

import android.content.Context
import android.database.sqlite.SQLiteDatabase
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * I18N-02 (issue #485): [MIGRATION_17_18] switches the offline FTS mirror from
 * Room's default `simple` tokenizer to `unicode61`, so an offline search for
 * "garcia" finds a cached "García" exactly as the server's FTS5 search does.
 *
 * The "before" shape is the exported v17 schema (v16 + `pending_interactions.
 * idempotencyKey`, FTS mirror still on `simple`) built by
 * [LocalDatabaseSchemaFixtures.createV17Tables]. An accented cached row is
 * seeded before the hop; the migration's `'rebuild'` reindexes it under the new
 * tokenizer, which is what this test proves.
 *
 * Issue #385 (SQLCipher): runs against the plain framework SQLite factory, same
 * caveat as the other `Migration*Test`s — the encrypted real-device counterpart
 * is `RoomMigrationEncryptedTest` in `app/src/androidTest`.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class Migration17To18Test {

    private lateinit var context: Context
    private lateinit var dbFile: java.io.File

    @Before
    fun setup() {
        context = ApplicationProvider.getApplicationContext()
        dbFile = context.getDatabasePath("migration-17-18-test.db")
        dbFile.delete()
    }

    @After
    fun teardown() {
        dbFile.delete()
    }

    private fun createV17Database() {
        val db = SQLiteDatabase.openOrCreateDatabase(dbFile, null)
        LocalDatabaseSchemaFixtures.createV17Tables(db)
        // Accented row seeded pre-migration (raw SQL; there are no FTS sync
        // triggers on the hand-built fixture yet). fn is what searchFts matches.
        db.execSQL(
            "INSERT INTO cached_contacts (id, fn, firstname, lastname, archived, deleted) " +
                "VALUES (1, 'García Ruiz', 'García', 'Ruiz', 0, 0)",
        )
        db.execSQL(
            "INSERT INTO cached_contacts (id, fn, firstname, lastname, archived, deleted) " +
                "VALUES (2, 'Straße', 'Straße', '', 0, 0)",
        )
        db.version = 17
        db.close()
    }

    @Test
    fun `offline search folds accents and case after the v18 hop`() = runBlocking {
        createV17Database()

        val db = Room.databaseBuilder(context, AppDatabase::class.java, dbFile.absolutePath)
            .addMigrations(*REGISTERED_MIGRATIONS.toTypedArray())
            .build()

        // Accent-insensitive, exactly like the server: "garcia" finds "García".
        var found = db.cachedContactDao().searchFts("garcia")
        assertEquals(1, found.size)
        assertEquals("García Ruiz", found[0].fn)

        // Case + accent insensitive in the other direction too.
        found = db.cachedContactDao().searchFts("GARCÍA")
        assertEquals(1, found.size)

        // The server's documented non-fold is inherited: ß is not ss.
        found = db.cachedContactDao().searchFts("strasse")
        assertEquals(0, found.size)
        found = db.cachedContactDao().searchFts("straße")
        assertEquals(1, found.size)

        // A fresh row written through the DAO after the hop keeps the recreated
        // content-sync triggers working under the new tokenizer.
        db.cachedContactDao().upsert(
            CachedContact(id = 3, fn = "İstanbul", firstname = "İstanbul"),
        )
        found = db.cachedContactDao().searchFts("istanbul")
        assertEquals(1, found.size)
        assertEquals("İstanbul", found[0].fn)

        db.close()
    }
}
