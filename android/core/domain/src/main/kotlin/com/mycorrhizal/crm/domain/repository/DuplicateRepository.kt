package com.mycorrhizal.crm.domain.repository

import com.mycorrhizal.crm.model.network.DuplicatePairsResponse

/**
 * T93 duplicate detection (web-parity for the "Review duplicates" surface).
 * The candidate pairs are recomputed server-side on every fetch and dismissed
 * pairs are already filtered out, so this client stays stateless across
 * reloads — list() always restarts from page one.
 */
interface DuplicateRepository {
    /** GET /contacts/duplicates — one offset-paginated page, strongest first. */
    suspend fun listPairs(page: Int = 1, limit: Int? = null): Result<DuplicatePairsResponse>

    /** POST /contacts/duplicates/dismiss — permanent "not a duplicate" verdict. */
    suspend fun dismiss(uidA: String, uidB: String): Result<Unit>
}
