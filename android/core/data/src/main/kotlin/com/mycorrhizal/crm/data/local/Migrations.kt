package com.mycorrhizal.crm.data.local

import androidx.room.migration.Migration
import androidx.sqlite.SQLiteConnection
import androidx.sqlite.execSQL

/**
 * T76: adds `phonesNormalized` (offline phone search — see [PhoneKey]) to `cached_contacts`
 * and its FTS mirror `cached_contacts_fts`.
 *
 * Hand-written rather than relying on `fallbackToDestructiveMigration` (this app's general
 * cache-rebuild policy, [AppDatabase]): `pending_interactions` is a real not-yet-synced
 * outbox — device call/SMS tracking staged for server sync (§6.1/6.2) — and a destructive
 * migration drops every table in the database, that one included. No other table's schema
 * changed for this version bump, so no other SQL is needed here.
 *
 * FTS4 virtual tables cannot `ALTER TABLE ... ADD COLUMN`, so `cached_contacts_fts` is dropped
 * and recreated with the new column list, then rebuilt from `cached_contacts`' current rows
 * via the FTS `'rebuild'` command (a fresh external-content FTS table starts with an empty
 * index; only future trigger-driven writes populate it otherwise). Room's own migration
 * pipeline drops and recreates every `@Fts4` entity's sync triggers around *every* registered
 * migration automatically (`RoomOpenDelegate.onPreMigrate`/`onPostMigrate`) — this migration
 * only needs to handle the table-level DDL delta, not the triggers themselves.
 */
val MIGRATION_13_14: Migration = object : Migration(startVersion = 13, endVersion = 14) {
    override fun migrate(connection: SQLiteConnection) {
        connection.execSQL("ALTER TABLE `cached_contacts` ADD COLUMN `phonesNormalized` TEXT")
        connection.execSQL("DROP TABLE IF EXISTS `cached_contacts_fts`")
        connection.execSQL(
            "CREATE VIRTUAL TABLE IF NOT EXISTS `cached_contacts_fts` USING FTS4(`fn` TEXT, " +
                "`firstname` TEXT, `lastname` TEXT, `primaryEmail` TEXT, `primaryPhone` TEXT, " +
                "`phonesNormalized` TEXT, `org` TEXT, content=`cached_contacts`)",
        )
        connection.execSQL("INSERT INTO `cached_contacts_fts`(`cached_contacts_fts`) VALUES('rebuild')")
    }
}

/**
 * M12: adds the `cached_cadence_policies` table (M12's CadencePolicyRepository
 * mirrors server policies into the cache, following the timeline-entity
 * full-resync pattern). A new table only — no existing table's schema changed.
 *
 * A hand-written migration is required rather than relying on the destructive
 * fallback for the same reason as [MIGRATION_13_14]: the destructive path drops
 * *every* table, `pending_interactions` (a real not-yet-synced outbox) included.
 */
val MIGRATION_14_15: Migration = object : Migration(startVersion = 14, endVersion = 15) {
    override fun migrate(connection: SQLiteConnection) {
        connection.execSQL(
            "CREATE TABLE IF NOT EXISTS `cached_cadence_policies` (`id` TEXT NOT NULL, " +
                "`entityId` TEXT NOT NULL, `targetIntervalDays` INTEGER NOT NULL, " +
                "`qualifyingTypes` TEXT NOT NULL, `updatedAt` TEXT, `deleted` INTEGER NOT NULL, " +
                "PRIMARY KEY(`id`))",
        )
    }
}

/**
 * Issue #212 (web #173): adds `isFavorite` to `cached_contacts` — the
 * CRM-local favorite flag mirrors into the cache like `archived` so the star
 * survives offline. A single `ALTER TABLE ADD COLUMN` (rows preserved).
 *
 * Hand-written rather than relying on `fallbackToDestructiveMigration` for
 * the same reason as the two migrations above: the destructive path drops
 * every table, `pending_interactions` (a real not-yet-synced outbox)
 * included. The cache itself is rebuildable, but the outbox is not, so any
 * version bump must keep this out of the destructive path.
 */
val MIGRATION_15_16: Migration = object : Migration(startVersion = 15, endVersion = 16) {
    override fun migrate(connection: SQLiteConnection) {
        connection.execSQL("ALTER TABLE `cached_contacts` ADD COLUMN `isFavorite` INTEGER NOT NULL DEFAULT 0")
    }
}

/**
 * ANDROID-02 (issue #479): adds `pending_interactions.idempotencyKey` — the
 * per-row CON-04/ADR-0010 retry key the sync worker sends as its
 * `Idempotency-Key` header, so a retry after an ambiguous failure (server
 * committed, response lost) replays server-side instead of creating a
 * duplicate Activity.
 *
 * A single additive `ALTER TABLE ADD COLUMN` (nullable, rows preserved), then a
 * SQL-side backfill so every *existing* unsynced row — a device that was
 * offline when it upgraded — has a key too and is immediately retry-safe; no
 * code path ever has to handle a keyless row specially.
 *
 * Hand-written rather than relying on `fallbackToDestructiveMigration` for the
 * same reason as every migration since 13: the destructive path drops every
 * table, `pending_interactions` (a real not-yet-synced outbox) included.
 */
val MIGRATION_16_17: Migration = object : Migration(startVersion = 16, endVersion = 17) {
    override fun migrate(connection: SQLiteConnection) {
        connection.execSQL("ALTER TABLE `pending_interactions` ADD COLUMN `idempotencyKey` TEXT")
        connection.execSQL(
            "UPDATE `pending_interactions` SET `idempotencyKey` = lower(hex(randomblob(16))) " +
                "WHERE `idempotencyKey` IS NULL",
        )
    }
}

/**
 * I18N-02 (issue #485): switches `cached_contacts_fts` to the `unicode61`
 * tokenizer so offline search matches the server's (see [CachedContactFts]'s
 * doc comment). Only the virtual table changes — the content table and every
 * other table are untouched, so a hand-written migration keeps this out of the
 * destructive path (whose cost here, as ever, is `pending_interactions`).
 *
 * FTS4 virtual tables cannot change their tokenizer in place, so the mirror is
 * dropped and recreated with `tokenize=unicode61`, then rebuilt from
 * `cached_contacts`' current rows via the FTS `'rebuild'` command (a fresh
 * external-content FTS table starts with an empty index). Room's own migration
 * pipeline drops and recreates every `@Fts4` entity's sync triggers around
 * every registered migration automatically (`RoomOpenDelegate.onPreMigrate`/
 * `onPostMigrate`), so the new table gets its content-sync triggers back and
 * the search stays populated after the hop.
 */
val MIGRATION_17_18: Migration = object : Migration(startVersion = 17, endVersion = 18) {
    override fun migrate(connection: SQLiteConnection) {
        connection.execSQL("DROP TABLE IF EXISTS `cached_contacts_fts`")
        connection.execSQL(
            "CREATE VIRTUAL TABLE IF NOT EXISTS `cached_contacts_fts` USING FTS4(" +
                "`fn` TEXT, `firstname` TEXT, `lastname` TEXT, `primaryEmail` TEXT, " +
                "`primaryPhone` TEXT, `phonesNormalized` TEXT, `org` TEXT, " +
                "content=`cached_contacts`, tokenize=unicode61)",
        )
        connection.execSQL("INSERT INTO `cached_contacts_fts`(`cached_contacts_fts`) VALUES('rebuild')")
    }
}

/**
 * The [AppDatabase] schema version. A `const val` (rather than a bare `18` in the `@Database`
 * annotation) so [MigrationVersionCoverageTest] can read the exact same value the annotation
 * compiles with, instead of a second, independently-maintained copy of the number.
 */
const val CURRENT_VERSION: Int = 18

/**
 * Issue #480: the lowest [AppDatabase] version this repo has any evidence of shipping.
 * This file's very first commit (the M1 Android app, PR #80, 2026-08-10) already declared
 * `version = 13` — no release of this app ever ran a lower version, so [MigrationVersionCoverageTest]
 * starts its version-pair sweep here rather than at 1. See [AppDatabase]'s doc comment.
 */
const val EARLIEST_KNOWN_VERSION: Int = 13

/**
 * Single source of truth for every hand-written migration this database registers, keyed by
 * `startVersion`. `DataModule.provideDatabase` builds its `.addMigrations(...)` call from this
 * list (never a second, hand-copied list — see ADR-style precedent throughout this repo for why a
 * second copy of a mapping table always drifts), and [MigrationVersionCoverageTest] walks it
 * against [EARLIEST_KNOWN_VERSION]..[CURRENT_VERSION] to prove no version pair in that range
 * was left to the destructive fallback by accident.
 */
val REGISTERED_MIGRATIONS: List<Migration> = listOf(
    MIGRATION_13_14,
    MIGRATION_14_15,
    MIGRATION_15_16,
    MIGRATION_16_17,
    MIGRATION_17_18,
)

/**
 * Version pairs (`startVersion` only — each covers `start` -> `start + 1`) in
 * [EARLIEST_KNOWN_VERSION]`..`[CURRENT_VERSION] that are deliberately left to
 * `fallbackToDestructiveMigration` rather than a hand-written [Migration] — i.e. an explicit,
 * reviewed decision that losing `pending_interactions` (the one non-rebuildable table) on that
 * specific upgrade is acceptable, not an oversight. Empty today: every registered version bump
 * since [EARLIEST_KNOWN_VERSION] has a real migration in [REGISTERED_MIGRATIONS]. Add an entry
 * here — with a comment explaining why — the day that stops being true; leaving a gap in neither
 * list is what [MigrationVersionCoverageTest] fails the build for.
 */
val ACCEPTED_DESTRUCTIVE_GAPS: Set<Int> = emptySet()
