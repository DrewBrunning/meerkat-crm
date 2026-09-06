package com.mycorrhizal.crm.data.repository

import androidx.room.RoomDatabase
import androidx.room.withTransaction
import com.mycorrhizal.crm.data.local.AppDatabase
import com.mycorrhizal.crm.data.local.PendingInteractionDao
import com.mycorrhizal.crm.domain.repository.OUTBOX_UNSYNCED_CAP
import com.mycorrhizal.crm.domain.repository.PendingInteraction as DomainPendingInteraction
import com.mycorrhizal.crm.domain.repository.PendingInteractionRepository
import java.util.UUID
import javax.inject.Inject

/**
 * Room-backed [PendingInteractionRepository].
 *
 * ANDROID-02 (issue #479): `record()` is the single write path into the
 * outbox, so it is where both the retry-key assignment and the bounded-queue
 * policy live:
 *
 *  - Every row gets an [idempotencyKey] (a fresh UUID) when none is supplied.
 *    The sync worker sends it as the CON-04/ADR-0010 `Idempotency-Key` header,
 *    so an ambiguous-failure retry replays server-side instead of duplicating
 *    the Activity. The key is assigned here — the moment the logical operation
 *    enters the outbox — not by the worker, so it is stable for the row's whole
 *    life and unique across a user's devices (each device has its own UUIDs).
 *
 *  - The unsynced queue is bounded at [OUTBOX_UNSYNCED_CAP]: after every
 *    insert, the oldest unsynced rows past the cap are evicted in the same
 *    transaction. The cap is a deliberate decision (issue #479 recommended
 *    action 7) — an outbox that grows without limit while offline is a storage
 *    problem and eventually a crash, and keeping the *newest* interactions (a
 *    user is likeliest to care about the call they just made) while dropping
 *    the oldest is the least-bad behavior at the limit. Reaching the cap
 *    requires roughly [OUTBOX_UNSYNCED_CAP] captures with no successful sync
 *    in between — months of offline call/SMS tracking at normal volumes.
 */
class PendingInteractionRepositoryImpl @Inject constructor(
    private val dao: PendingInteractionDao,
    private val database: AppDatabase,
) : PendingInteractionRepository {
    override suspend fun record(interaction: DomainPendingInteraction) {
        database.withTransaction {
            dao.insert(interaction.toEntity())
            dao.trimUnsynced(OUTBOX_UNSYNCED_CAP)
        }
    }

    override suspend fun unsynced(): List<DomainPendingInteraction> =
        dao.getUnsynced().map { it.toDomain() }

    override suspend fun markSynced(id: Long, syncedAt: String) {
        dao.markSynced(id, syncedAt)
    }

    override suspend fun deleteSynced() {
        dao.deleteSynced()
    }

    override suspend fun recordIfNew(interaction: DomainPendingInteraction): Boolean {
        val phoneNumber = interaction.phoneNumber
        return database.withTransaction {
            if (phoneNumber != null &&
                dao.countExact(interaction.kind, phoneNumber, interaction.timestampMillis) > 0
            ) {
                false
            } else {
                dao.insert(interaction.toEntity())
                dao.trimUnsynced(OUTBOX_UNSYNCED_CAP)
                true
            }
        }
    }

    override suspend fun setIdempotencyKey(id: Long, key: String) {
        dao.setIdempotencyKey(id, key)
    }

    override suspend fun clearMatchedContact(id: Long) {
        dao.clearMatchedContact(id)
    }
}

private fun com.mycorrhizal.crm.data.local.PendingInteraction.toDomain(): DomainPendingInteraction =
    DomainPendingInteraction(
        id = id,
        timestampMillis = timestampMillis,
        kind = kind,
        direction = direction,
        phoneNumber = phoneNumber,
        matchedContactId = matchedContactId,
        synced = synced,
        syncedAt = syncedAt,
        idempotencyKey = idempotencyKey,
    )

private fun DomainPendingInteraction.toEntity(): com.mycorrhizal.crm.data.local.PendingInteraction =
    com.mycorrhizal.crm.data.local.PendingInteraction(
        id = id,
        timestampMillis = timestampMillis,
        kind = kind,
        direction = direction,
        phoneNumber = phoneNumber,
        matchedContactId = matchedContactId,
        synced = synced,
        syncedAt = syncedAt,
        idempotencyKey = idempotencyKey ?: UUID.randomUUID().toString(),
    )
