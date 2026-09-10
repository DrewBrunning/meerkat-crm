package com.mycorrhizal.crm.storage

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.mycorrhizal.crm.data.local.AppDatabase
import com.mycorrhizal.crm.data.local.CachedContact
import com.mycorrhizal.crm.data.local.PhoneKey
import com.mycorrhizal.crm.data.local.PendingInteraction
import com.mycorrhizal.crm.data.local.RoomCacheEncryption
import java.io.DataInputStream
import java.io.File
import java.io.FileInputStream
import kotlinx.coroutines.runBlocking
import net.zetetic.database.sqlcipher.SupportOpenHelperFactory
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Issue #385: end-to-end verification of the SQLCipher Room-mirror encryption.
 * These are instrumented (device/emulator) tests on purpose — SQLCipher's
 * `libsqlcipher.so` is an Android-native binary that cannot load in the
 * Robolectric JVM, so the encryption, wrong-key fail-closed, and the
 * plaintext→encrypted transition can only run here (and, implicitly, on every
 * E2E boot of the real app, which opens the encrypted DB through DataModule).
 *
 * The JVM-side migration/DAO logic is covered by the plain-factory Robolectric
 * tests (`Migration13To14Test` etc.) — see their doc comments.
 */
@RunWith(AndroidJUnit4::class)
class RoomCacheEncryptionTest {

    private val context: Context = ApplicationProvider.getApplicationContext()
    private val passphrase = "0a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20212223242526272829"

    /** The 16-byte header every plaintext SQLite file begins with. */
    private val plaintextHeader = "SQLite format 3\u0000"

    private val dbFiles = mutableListOf<File>()

    @Before
    fun setUp() {
        System.loadLibrary("sqlcipher")
    }

    @After
    fun tearDown() {
        dbFiles.forEach { file ->
            file.delete()
            deleteSidecars(file)
        }
        dbFiles.clear()
    }

    private fun dbFile(name: String): File {
        val file = context.getDatabasePath("room-encryption-$name.db")
        file.delete()
        deleteSidecars(file)
        dbFiles += file
        return file
    }

    private fun deleteSidecars(file: File) {
        listOf("-journal", "-wal", "-shm").forEach { suffix ->
            File(file.parentFile, file.name + suffix).delete()
        }
    }

    /** Builds a plaintext (framework SQLite) Room database at the current schema. */
    private fun buildPlaintextDb(file: File): AppDatabase =
        Room.databaseBuilder(context, AppDatabase::class.java, file.absolutePath).build()

    /** Builds a SQLCipher-encrypted Room database. */
    private fun buildEncryptedDb(file: File, key: String = passphrase): AppDatabase =
        Room.databaseBuilder(context, AppDatabase::class.java, file.absolutePath)
            .openHelperFactory(SupportOpenHelperFactory(key.toByteArray()))
            .build()

    @Test
    fun sqlcipherCreatedDatabaseIsEncryptedOnDisk() {
        val file = dbFile("created")
        val db = buildEncryptedDb(file)
        runBlocking {
            db.cachedContactDao().upsert(
                CachedContact(id = 1, fn = "Dana White", primaryPhone = "(800) 555-1234"),
            )
        }
        db.close()

        assertNotEquals("The file header must not be a plaintext SQLite header", plaintextHeader, headerOf(file))
        assertTrue(RoomCacheEncryption.isEncrypted(file))
    }

    @Test
    fun encryptedDatabaseRoundTripsWithTheSamePassphrase() {
        val file = dbFile("roundtrip")
        val db = buildEncryptedDb(file)
        runBlocking { db.cachedContactDao().upsert(CachedContact(id = 1, fn = "Dana White")) }
        db.close()

        val reopened = buildEncryptedDb(file)
        runBlocking { assertEquals("Dana White", reopened.cachedContactDao().getById(1)?.fn) }
        reopened.close()
    }

    @Test
    fun wrongPassphraseFailsClosed() {
        val file = dbFile("wrongkey")
        val db = buildEncryptedDb(file)
        runBlocking { db.cachedContactDao().upsert(CachedContact(id = 1, fn = "Dana White")) }
        db.close()

        val wrongKey = buildEncryptedDb(file, key = "f".repeat(64))
        try {
            runBlocking { wrongKey.cachedContactDao().getById(1) }
            fail("Opening an encrypted DB with the wrong key must throw")
        } catch (expected: Exception) {
            // SQLCipher reports "file is encrypted or is not a database" (or an
            // associated SQLiteException) — the point is it must not return data.
        } finally {
            wrongKey.close()
        }
    }

    @Test
    fun plaintextDatabaseIsReencryptedInPlacePreservingRowsOutboxAndFtsSearch() {
        val file = dbFile("transition")
        val plaintext = buildPlaintextDb(file)
        runBlocking {
            plaintext.cachedContactDao().upsert(
                CachedContact(
                    id = 1,
                    fn = "Dana White",
                    primaryPhone = "555-0100",
                    phonesNormalized = PhoneKey.flatten(listOf("555-0100")),
                ),
            )
            plaintext.pendingInteractionDao().insert(
                PendingInteraction(timestampMillis = 1000, kind = "call", phoneNumber = "5551234", synced = false),
            )
        }
        plaintext.close()
        assertEquals("A plaintext Room DB must start with the plaintext header", plaintextHeader, headerOf(file))

        RoomCacheEncryption.ensureEncrypted(file, passphrase)

        assertTrue("The transitioned file must be encrypted", RoomCacheEncryption.isEncrypted(file))
        assertNotEquals("The plaintext header must be gone", plaintextHeader, headerOf(file))

        val encrypted = buildEncryptedDb(file)
        runBlocking {
            val contact = encrypted.cachedContactDao().getById(1)
            assertEquals("Dana White", contact?.fn)
            // The normalized phone column must survive the transition for FTS
            // search to have anything to match.
            assertEquals(PhoneKey.flatten(listOf("555-0100")), contact?.phonesNormalized)

            // The non-rebuildable outbox survives the transition.
            val pending = encrypted.pendingInteractionDao().getUnsynced()
            assertEquals(1, pending.size)
            assertEquals("5551234", pending[0].phoneNumber)

            // The FTS mirror is re-established and searchable inside the encrypted DB
            // (name via the bare-prefix path, phone via the column MATCH path).
            assertEquals(1, encrypted.cachedContactDao().searchFts("Dana").size)
            val found = encrypted.cachedContactDao().searchFtsMatch("phonesNormalized:5550100*")
            assertEquals(1, found.size)
        }
        encrypted.close()
    }

    @Test
    fun ensureEncryptedIsNoOpOnAlreadyEncryptedDatabase() {
        val file = dbFile("idempotent")
        val db = buildEncryptedDb(file)
        runBlocking { db.cachedContactDao().upsert(CachedContact(id = 1, fn = "Dana White")) }
        db.close()

        RoomCacheEncryption.ensureEncrypted(file, passphrase)

        val reopened = buildEncryptedDb(file)
        runBlocking { assertEquals("Dana White", reopened.cachedContactDao().getById(1)?.fn) }
        reopened.close()
    }

    private fun headerOf(file: File): String {
        // InputStream.readNBytes(int) is API 33+ (java.io desugaring does not
        // cover it), and this suite also runs on the API 26 minSdk floor
        // (android-tests.yml's android-e2e-min-sdk job). DataInputStream.readFully
        // has been the fixed-length read since API 1.
        val header = ByteArray(16)
        DataInputStream(FileInputStream(file)).use { it.readFully(header) }
        return String(header, Charsets.ISO_8859_1)
    }
}
