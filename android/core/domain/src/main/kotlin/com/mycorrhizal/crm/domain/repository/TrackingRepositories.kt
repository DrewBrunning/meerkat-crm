package com.mycorrhizal.crm.domain.repository

/** A device-detected interaction staged for optional server sync (§6.1/6.2). */
data class PendingInteraction(
    val id: Long = 0,
    val timestampMillis: Long,
    val kind: String,
    val direction: String? = null,
    val phoneNumber: String? = null,
    val matchedContactId: Int? = null,
    val synced: Boolean = false,
    val syncedAt: String? = null,
    /**
     * ANDROID-02 (issue #479): the CON-04/ADR-0010 idempotency key for this
     * row's server sync — an opaque per-row UUID, generated once when the row
     * is recorded (and backfilled for pre-existing rows by the v17 migration).
     *
     * The outbox is a retry queue: an attempt whose response was lost after
     * the server committed is retried, and without a key the retry creates a
     * duplicate server Activity. Sending this key on every attempt makes the
     * server replay its stored response instead (exactly-once). Null only for
     * rows written before the key column existed and not yet backfilled.
     */
    val idempotencyKey: String? = null,
)

/**
 * ANDROID-02 (issue #479) recommended action 7: the unsynced outbox is
 * bounded. A device offline long enough that its call/SMS captures would grow
 * without limit is a storage problem and eventually a crash, so `record()`
 * evicts the *oldest* unsynced rows past this cap (keeping the newest — the
 * interactions a user is likeliest to care about). See the repository
 * implementation's doc comment for the full decision.
 */
const val OUTBOX_UNSYNCED_CAP: Int = 500

/**
 * Staging of device-detected interactions (Phase 4 §6.1/6.2). Call/SMS
 * tracking writes a PendingInteraction locally instead of hitting the server
 * directly; the sync worker converts unsynced rows into server Activities.
 */
interface PendingInteractionRepository {
    suspend fun record(interaction: PendingInteraction)
    suspend fun unsynced(): List<PendingInteraction>
    suspend fun markSynced(id: Long, syncedAt: String)
    suspend fun deleteSynced()

    /**
     * Records [interaction] unless an identical row already exists — same
     * kind, phone number and timestamp — returning true only when a new row
     * was staged.
     *
     * Issue #721: the provider-backed readers (call-log catch-up, SMS backfill)
     * can be triggered by two independent WorkManager chains at once (a
     * phone-state one-shot next to the periodic catch-up, or the grant-time
     * backfill next to the periodic one), and both advance the watermark only
     * *after* staging. Without a dedupe the overlapping runs would stage the
     * same interaction twice — and since rows are deleted once synced, the
     * duplicate would only surface on the server. The identical-row check is
     * exact (provider DATE is a single, shared timestamp source per reader), so
     * it is safe; a missed watermark advance can only ever *under*-report, never
     * double-report.
     */
    suspend fun recordIfNew(interaction: PendingInteraction): Boolean

    /**
     * Persists [key] as the row's idempotency key. Used to backfill a row that
     * somehow still has none (an upgrade edge that the migration should have
     * covered) so the worker never syncs a keyless row.
     */
    suspend fun setIdempotencyKey(id: Long, key: String)

    /**
     * Drops the row's [PendingInteraction.matchedContactId] link.
     *
     * ANDROID-02 (issue #479) recommended action 4: on reconnect a queued
     * interaction can reference a contact the server deleted meanwhile (its
     * soft delete hides it from `CreateActivity`'s contact lookup, so the
     * create 404s "One or more contacts not found"). Per ADR-0009 each queued
     * write resolves on its own; for an outbox row the interaction itself is
     * the value and the contact link is best-effort (the capture path already
     * syncs unmatched numbers as unassociated Activities), so the resolution
     * is to drop the stale link and sync unassociated rather than leave the
     * row stuck in the queue forever.
     */
    suspend fun clearMatchedContact(id: Long)
}

/** User prefs controlling which device features are enabled (§8.3 opt-in toggles). */
interface TrackingSettingsRepository {
    suspend fun callTrackingEnabled(): Boolean
    suspend fun setCallTrackingEnabled(enabled: Boolean)
    suspend fun smsTrackingEnabled(): Boolean
    suspend fun setSmsTrackingEnabled(enabled: Boolean)
    suspend fun notificationsEnabled(): Boolean
    suspend fun setNotificationsEnabled(enabled: Boolean)
    /** Last timestamp processed by the call-log reader (dedupe). */
    suspend fun lastCallLogTimestamp(): Long
    suspend fun setLastCallLogTimestamp(ts: Long)
    /** Last timestamp processed by the SMS-history reader (dedupe). */
    suspend fun lastSmsTimestamp(): Long
    suspend fun setLastSmsTimestamp(ts: Long)
    /** When the periodic interaction sync last ran. */
    suspend fun lastInteractionSyncAt(): Long?
}
