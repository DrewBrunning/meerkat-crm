package com.mycorrhizal.crm.data.local

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query

@Dao
interface PendingInteractionDao {
    @Insert
    suspend fun insert(interaction: PendingInteraction): Long

    @Query("SELECT * FROM pending_interactions WHERE synced = 0 ORDER BY timestampMillis ASC")
    suspend fun getUnsynced(): List<PendingInteraction>

    @Query("UPDATE pending_interactions SET synced = 1, syncedAt = :syncedAt WHERE id = :id")
    suspend fun markSynced(id: Long, syncedAt: String)

    @Query("SELECT COUNT(*) FROM pending_interactions WHERE synced = 0")
    suspend fun countUnsynced(): Int

    @Query("DELETE FROM pending_interactions WHERE synced = 1")
    suspend fun deleteSynced()

    /** ANDROID-02 (issue #479): backfills a row that somehow has no retry key yet. */
    @Query("UPDATE pending_interactions SET idempotencyKey = :key WHERE id = :id")
    suspend fun setIdempotencyKey(id: Long, key: String)

    /** ANDROID-02 (issue #479): drops a stale contact link on reconnect (ADR-0009). */
    @Query("UPDATE pending_interactions SET matchedContactId = NULL WHERE id = :id")
    suspend fun clearMatchedContact(id: Long)

    /**
     * ANDROID-02 (issue #479) recommended action 7: evicts the oldest unsynced
     * rows so the queue stays bounded at [keep]. Called after every insert, in
     * the same transaction — the outbox never grows past the cap no matter how
     * long a device is offline.
     */
    @Query(
        "DELETE FROM pending_interactions WHERE synced = 0 AND id NOT IN " +
            "(SELECT id FROM pending_interactions WHERE synced = 0 " +
            "ORDER BY timestampMillis DESC, id DESC LIMIT :keep)",
    )
    suspend fun trimUnsynced(keep: Int)

    /**
     * Issue #721: exact-match dedupe for [PendingInteractionRepository.recordIfNew]
     * — an already-staged interaction with the same kind / phone / timestamp
     * (the signature a provider-backed reader yields for the same underlying
     * call or message) must not be staged twice by an overlapping run.
     */
    @Query(
        "SELECT COUNT(*) FROM pending_interactions WHERE kind = :kind " +
            "AND phoneNumber = :phoneNumber AND timestampMillis = :timestampMillis",
    )
    suspend fun countExact(kind: String, phoneNumber: String, timestampMillis: Long): Int

    @Query("SELECT * FROM pending_interactions WHERE kind = :kind AND phoneNumber = :phoneNumber AND timestampMillis > :sinceMillis ORDER BY timestampMillis DESC LIMIT 1")
    suspend fun findRecent(kind: String, phoneNumber: String, sinceMillis: Long): PendingInteraction?
}
