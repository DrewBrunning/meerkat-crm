package com.mycorrhizal.crm.data.local

import android.database.sqlite.SQLiteDatabase

/**
 * Hand-written DDL for pre-issue-#480 [AppDatabase] versions (13, 14, 15) and
 * the first version captured from exported Room schema JSON (16), shared by the
 * `Migration*Test` classes.
 *
 * There is no exported Room schema JSON below version 16 (see [AppDatabase]'s
 * doc comment), so `MigrationTestHelper` can't build those "before" databases
 * from JSON the way it would for a migration registered from here on — these
 * functions build the exact CREATE TABLE statements by hand instead, captured
 * from the real generated schema at each version. Version 16 onwards *does*
 * have exported JSON, but the fixture keeps hand-writing for symmetry with its
 * older siblings and because it is the shape of a *shipped* v16 file, which is
 * what the migration tests mean to start from.
 *
 * Each function builds the *complete* schema for its version, not just the
 * tables that changed since the version before it: Room's `onValidateSchema`
 * (run by the real `Room.databaseBuilder` every `Migration*Test` drives its
 * migration through) checks every table after a migration runs, not just the
 * ones a given [Migration] touched, so an incomplete "before" database fails
 * validation on an unrelated table rather than testing anything real.
 *
 * Note the `pending_interactions` table deliberately has NO `idempotencyKey`
 * column through v16 — that column arrives in [MIGRATION_16_17] — so
 * [createV16Tables] is the migration test's "before" shape for the v16→v17 hop.
 */
object LocalDatabaseSchemaFixtures {

    private const val CACHED_CONTACTS_PRE_T76 =
        "CREATE TABLE `cached_contacts` (`id` INTEGER NOT NULL, `uid` TEXT, `firstname` TEXT, " +
            "`lastname` TEXT, `nickname` TEXT, `fn` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, " +
            "`birthday` TEXT, `org` TEXT, `photoThumbnail` TEXT, `circles` TEXT, `archived` INTEGER NOT NULL, " +
            "`deleted` INTEGER NOT NULL, `deviceLookupKey` TEXT, `card` TEXT, `crm` TEXT, `updatedAt` TEXT, " +
            "PRIMARY KEY(`id`))"

    private const val CACHED_CONTACTS_T76 =
        "CREATE TABLE `cached_contacts` (`id` INTEGER NOT NULL, `uid` TEXT, `firstname` TEXT, " +
            "`lastname` TEXT, `nickname` TEXT, `fn` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, " +
            "`birthday` TEXT, `org` TEXT, `photoThumbnail` TEXT, `circles` TEXT, `archived` INTEGER NOT NULL, " +
            "`deleted` INTEGER NOT NULL, `deviceLookupKey` TEXT, `card` TEXT, `crm` TEXT, `updatedAt` TEXT, " +
            "`phonesNormalized` TEXT, PRIMARY KEY(`id`))"

    private const val CACHED_CONTACTS_IS_FAVORITE =
        "CREATE TABLE `cached_contacts` (`id` INTEGER NOT NULL, `uid` TEXT, `firstname` TEXT, " +
            "`lastname` TEXT, `nickname` TEXT, `fn` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, " +
            "`birthday` TEXT, `org` TEXT, `photoThumbnail` TEXT, `circles` TEXT, `archived` INTEGER NOT NULL, " +
            "`deleted` INTEGER NOT NULL, `deviceLookupKey` TEXT, `card` TEXT, `crm` TEXT, `updatedAt` TEXT, " +
            "`phonesNormalized` TEXT, `isFavorite` INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(`id`))"

    private const val CACHED_CONTACTS_FTS_PRE_T76 =
        "CREATE VIRTUAL TABLE `cached_contacts_fts` USING FTS4(`fn` TEXT, `firstname` TEXT, " +
            "`lastname` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, `org` TEXT, content=`cached_contacts`)"

    private const val CACHED_CONTACTS_FTS_T76 =
        "CREATE VIRTUAL TABLE `cached_contacts_fts` USING FTS4(`fn` TEXT, `firstname` TEXT, " +
            "`lastname` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, `phonesNormalized` TEXT, `org` TEXT, " +
            "content=`cached_contacts`)"

    /** The I18N-02 v18 shape: `cached_contacts_fts` recreated with `tokenize=unicode61`. */
    private const val CACHED_CONTACTS_FTS_UNICODE61 =
        "CREATE VIRTUAL TABLE `cached_contacts_fts` USING FTS4(`fn` TEXT, `firstname` TEXT, " +
            "`lastname` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, `phonesNormalized` TEXT, `org` TEXT, " +
            "content=`cached_contacts`, tokenize=unicode61)"

    /** Every table that is identical across v13 through v16 — written once, shared by all four. */
    private fun createUnchangedTables(db: SQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE `cached_activities` (`id` INTEGER NOT NULL, `title` TEXT, `description` TEXT, " +
                "`location` TEXT, `date` TEXT, `type` TEXT, `deleted` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_notes` (`id` INTEGER NOT NULL, `content` TEXT, `date` TEXT, " +
                "`deleted` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_reminders` (`id` INTEGER NOT NULL, `message` TEXT, `remindAt` TEXT, " +
                "`recurrence` TEXT, `completed` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_circles` (`id` TEXT NOT NULL, `name` TEXT NOT NULL, `updatedAt` TEXT, " +
                "PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_circle_members` (`id` INTEGER NOT NULL, `circleId` TEXT NOT NULL, " +
                "`memberVCardUid` TEXT NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_tags` (`id` TEXT NOT NULL, `name` TEXT NOT NULL, `updatedAt` TEXT, " +
                "PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_contact_tags` (`id` INTEGER NOT NULL, `tagId` TEXT NOT NULL, " +
                "`contactVCardUid` TEXT NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_households` (`id` TEXT NOT NULL, `name` TEXT NOT NULL, `type` TEXT NOT NULL, " +
                "`updatedAt` TEXT, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_household_members` (`id` INTEGER NOT NULL, `householdId` TEXT NOT NULL, " +
                "`memberVCardUid` TEXT NOT NULL, `role` TEXT, `since` TEXT, `until` TEXT, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_relationship_edges` (`id` TEXT NOT NULL, `sourceId` TEXT NOT NULL, " +
                "`targetId` TEXT NOT NULL, `type` TEXT NOT NULL, `directional` INTEGER NOT NULL, " +
                "`status` TEXT NOT NULL, `sensitivity` TEXT NOT NULL, `updatedAt` TEXT, `deleted` INTEGER NOT NULL, " +
                "PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_life_events` (`id` TEXT NOT NULL, `entityId` TEXT NOT NULL, `type` TEXT, " +
                "`category` TEXT, `date` TEXT, `description` TEXT, `remind` INTEGER, `updatedAt` TEXT, " +
                "`deleted` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_gifts` (`id` TEXT NOT NULL, `entityId` TEXT NOT NULL, `status` TEXT NOT NULL, " +
                "`occasion` TEXT, `description` TEXT NOT NULL, `updatedAt` TEXT, `deleted` INTEGER NOT NULL, " +
                "PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_preferences` (`id` TEXT NOT NULL, `entityId` TEXT NOT NULL, " +
                "`category` TEXT NOT NULL, `key` TEXT, `value` TEXT NOT NULL, `sensitivity` TEXT NOT NULL, " +
                "`updatedAt` TEXT, `deleted` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `cached_conversation_agenda` (`id` TEXT NOT NULL, `entityId` TEXT NOT NULL, " +
                "`content` TEXT NOT NULL, `referenceUrl` TEXT, `discussedAt` TEXT, `updatedAt` TEXT, " +
                "`deleted` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE TABLE `pending_interactions` (`id` INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, " +
                "`timestampMillis` INTEGER NOT NULL, `kind` TEXT NOT NULL, `direction` TEXT, `phoneNumber` TEXT, " +
                "`matchedContactId` INTEGER, `synced` INTEGER NOT NULL, `syncedAt` TEXT)",
        )
        db.execSQL(
            "CREATE TABLE `custom_link_actions` (`id` INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, " +
                "`protocol` TEXT NOT NULL, `label` TEXT NOT NULL, `kind` TEXT NOT NULL, `mimeType` TEXT NOT NULL, " +
                "`intentUriTemplate` TEXT NOT NULL)",
        )
    }

    /**
     * The pre-T76 shape: `cached_contacts`/`cached_contacts_fts` with no `phonesNormalized`
     * column. Does not set `db.version` — callers own that, matching how each `Migration*Test`
     * needs it set right before closing the connection.
     */
    fun createV13Tables(db: SQLiteDatabase) {
        db.execSQL(CACHED_CONTACTS_PRE_T76)
        db.execSQL(CACHED_CONTACTS_FTS_PRE_T76)
        createUnchangedTables(db)
    }

    /**
     * The T76 shape (post [MIGRATION_13_14]): `cached_contacts` gains `phonesNormalized` as its
     * last column (an `ALTER TABLE ADD COLUMN` appends, it does not reorder), and
     * `cached_contacts_fts` is rebuilt with that column added to its list — exactly what
     * [MIGRATION_13_14] does. Does not set `db.version`.
     */
    fun createV14Tables(db: SQLiteDatabase) {
        db.execSQL(CACHED_CONTACTS_T76)
        db.execSQL(CACHED_CONTACTS_FTS_T76)
        createUnchangedTables(db)
    }

    /**
     * The M12 shape (post [MIGRATION_14_15]): adds `cached_cadence_policies`, the only table
     * that migration touches. Does not set `db.version`.
     */
    fun createV15Tables(db: SQLiteDatabase) {
        createV14Tables(db)
        db.execSQL(
            "CREATE TABLE `cached_cadence_policies` (`id` TEXT NOT NULL, " +
                "`entityId` TEXT NOT NULL, `targetIntervalDays` INTEGER NOT NULL, " +
                "`qualifyingTypes` TEXT NOT NULL, `updatedAt` TEXT, `deleted` INTEGER NOT NULL, " +
                "PRIMARY KEY(`id`))",
        )
    }

    /**
     * The issue-#212 shape (post [MIGRATION_15_16]): adds `cached_contacts.isFavorite`, the
     * only table that migration touches. This is the exported-schema v16 shape minus the
     * `pending_interactions.idempotencyKey` column that arrives in [MIGRATION_16_17] — the
     * "before" database for the v16→v17 hop. Does not set `db.version`.
     */
    fun createV16Tables(db: SQLiteDatabase) {
        db.execSQL(CACHED_CONTACTS_IS_FAVORITE)
        db.execSQL(CACHED_CONTACTS_FTS_T76)
        createUnchangedTables(db)
        db.execSQL(
            "CREATE TABLE `cached_cadence_policies` (`id` TEXT NOT NULL, " +
                "`entityId` TEXT NOT NULL, `targetIntervalDays` INTEGER NOT NULL, " +
                "`qualifyingTypes` TEXT NOT NULL, `updatedAt` TEXT, `deleted` INTEGER NOT NULL, " +
                "PRIMARY KEY(`id`))",
        )
    }

    /**
     * The ANDROID-02 v17 shape (post [MIGRATION_16_17]): v16 plus
     * `pending_interactions.idempotencyKey` — but the FTS mirror still uses the
     * `simple` tokenizer, which is what a real v17 install upgrading to v18 has
     * on disk. This is the "before" database for the v17→v18 hop. Does not set
     * `db.version`.
     */
    fun createV17Tables(db: SQLiteDatabase) {
        createV16Tables(db)
        db.execSQL("ALTER TABLE `pending_interactions` ADD COLUMN `idempotencyKey` TEXT")
    }
}
