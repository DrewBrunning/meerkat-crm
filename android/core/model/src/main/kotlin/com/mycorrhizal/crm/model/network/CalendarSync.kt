package com.mycorrhizal.crm.model.network

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

/**
 * Calendar (CalDAV/iCal) and contact (CardDAV) subscription management, with
 * the sync-health surface issue #390 added (last success/failure/attempt,
 * consecutive-failure count, incident start, per-run tallies) plus the INT-04
 * (#467) terminal-failure fields. Android follow-up: issue #628. Mirrors
 * `backend/openapi.yaml`'s `CalendarSubscriptionResponse` /
 * `ContactSubscriptionResponse` and web's `frontend/src/api/calendars.ts` /
 * `contactSubscriptions.ts` exactly.
 */

/** GET/POST/PUT /api/v1/calendars response for one subscription — never includes the password. */
@JsonClass(generateAdapter = true)
data class CalendarSubscription(
    val id: Int = 0,
    val name: String = "",
    val url: String = "",
    val username: String = "",
    @Json(name = "has_password") val hasPassword: Boolean = false,
    @Json(name = "sync_enabled") val syncEnabled: Boolean = false,
    @Json(name = "past_days") val pastDays: Int = 0,
    @Json(name = "future_days") val futureDays: Int = 0,
    @Json(name = "last_synced_at") val lastSyncedAt: String? = null,
    @Json(name = "last_sync_status") val lastSyncStatus: String = "",
    @Json(name = "last_sync_error") val lastSyncError: String = "",
    @Json(name = "created_at") val createdAt: String? = null,
    /** Sync-health last-known-good state (issue #390). Timestamps are null until the relevant run has happened. */
    @Json(name = "last_attempt_at") val lastAttemptAt: String? = null,
    @Json(name = "last_success_at") val lastSuccessAt: String? = null,
    @Json(name = "last_failure_at") val lastFailureAt: String? = null,
    @Json(name = "consecutive_failures") val consecutiveFailures: Int = 0,
    @Json(name = "incident_first_failure_at") val incidentFirstFailureAt: String? = null,
    @Json(name = "last_run_duration_ms") val lastRunDurationMs: Long? = null,
    @Json(name = "last_run_stats") val lastRunStats: Map<String, Int> = emptyMap(),
    /** Terminal (permanent-until-human) failure state (INT-04, issue #467). */
    @Json(name = "terminal_failure_at") val terminalFailureAt: String? = null,
    @Json(name = "terminal_reason") val terminalReason: String = "",
)

/** POST/PUT /api/v1/calendars request body. On update, an empty password keeps the stored one. */
@JsonClass(generateAdapter = true)
data class CalendarSubscriptionInput(
    val name: String,
    val url: String,
    val username: String? = null,
    val password: String? = null,
    @Json(name = "clear_password") val clearPassword: Boolean? = null,
    @Json(name = "sync_enabled") val syncEnabled: Boolean? = null,
    @Json(name = "past_days") val pastDays: Int? = null,
    @Json(name = "future_days") val futureDays: Int? = null,
)

/** GET /api/v1/calendars — `{ calendars: [...] }`. */
@JsonClass(generateAdapter = true)
data class CalendarSubscriptionsResponse(
    val calendars: List<CalendarSubscription> = emptyList(),
)

/** POST /api/v1/calendars/{id}/sync response. */
@JsonClass(generateAdapter = true)
data class CalendarSyncResult(
    val message: String = "",
    val created: Int = 0,
    val updated: Int = 0,
    val skipped: Int = 0,
)

/**
 * GET /api/v1/contact-subscriptions response for one CardDAV subscription —
 * never includes the password or sync token. Android has no CRUD UI for
 * these yet (issue #628 scope note 4 — web has none either); the surface
 * here is read-only health reporting.
 */
@JsonClass(generateAdapter = true)
data class ContactSubscription(
    val id: Int = 0,
    val name: String = "",
    val url: String = "",
    val username: String = "",
    @Json(name = "has_password") val hasPassword: Boolean = false,
    @Json(name = "sync_enabled") val syncEnabled: Boolean = false,
    @Json(name = "last_synced_at") val lastSyncedAt: String? = null,
    @Json(name = "last_sync_status") val lastSyncStatus: String = "",
    @Json(name = "last_sync_error") val lastSyncError: String = "",
    @Json(name = "created_at") val createdAt: String? = null,
    @Json(name = "last_attempt_at") val lastAttemptAt: String? = null,
    @Json(name = "last_success_at") val lastSuccessAt: String? = null,
    @Json(name = "last_failure_at") val lastFailureAt: String? = null,
    @Json(name = "consecutive_failures") val consecutiveFailures: Int = 0,
    @Json(name = "incident_first_failure_at") val incidentFirstFailureAt: String? = null,
    @Json(name = "last_run_duration_ms") val lastRunDurationMs: Long? = null,
    @Json(name = "last_run_stats") val lastRunStats: Map<String, Int> = emptyMap(),
    @Json(name = "terminal_failure_at") val terminalFailureAt: String? = null,
    @Json(name = "terminal_reason") val terminalReason: String = "",
    /** Unreviewed local edits this subscription's syncs have overwritten (issue #395). */
    @Json(name = "pending_conflicts") val pendingConflicts: Int = 0,
)

/** GET /api/v1/contact-subscriptions — `{ contact_subscriptions: [...] }`. */
@JsonClass(generateAdapter = true)
data class ContactSubscriptionsResponse(
    @Json(name = "contact_subscriptions") val contactSubscriptions: List<ContactSubscription> = emptyList(),
)
