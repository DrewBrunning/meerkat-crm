package com.mycorrhizal.crm.model.network

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

/**
 * T93 duplicate-scan tiers, mirroring backend/models/duplicate.go's
 * DuplicatePairReason constants. A pair can match on several; the response
 * carries the full set, never the first hit.
 */
object DuplicateReasons {
    const val EMAIL = "email"
    const val NAME = "name"
    const val PHONE = "phone"
}

/**
 * One candidate pair from GET /contacts/duplicates (T93). [a] and [b] are the
 * same slim summaries the contacts-list endpoint returns (ContactSummary), so
 * the review surface can render them with the existing row pieces. [reasons]
 * is the full set of matched tiers in canonical order (email, name, phone);
 * [confidence] is a 0-1 heuristic that is purely a function of which tiers
 * matched (phone+email ~0.95, name alone ~0.5).
 */
@JsonClass(generateAdapter = true)
data class DuplicatePair(
    val a: ContactSummary = ContactSummary(),
    val b: ContactSummary = ContactSummary(),
    val reasons: List<String> = emptyList(),
    val confidence: Double = 0.0,
) {
    /** Stable review key: both VCardUIDs ordered, so (A,B) and (B,A) collapse. */
    val reviewKey: String
        get() {
            val uids = listOfNotNull(a.uid, b.uid).sorted()
            return uids.joinToString("|").ifBlank { "id:${a.id}|id:${b.id}" }
        }
}

/** GET /contacts/duplicates response envelope (offset pagination). */
@JsonClass(generateAdapter = true)
data class DuplicatePairsResponse(
    val pairs: List<DuplicatePair> = emptyList(),
    val total: Int = 0,
    val page: Int = 0,
    val limit: Int = 0,
)

/** POST /contacts/duplicates/dismiss request body (T93). */
@JsonClass(generateAdapter = true)
data class DuplicateDismissalInput(
    @Json(name = "uid_a") val uidA: String,
    @Json(name = "uid_b") val uidB: String,
)
