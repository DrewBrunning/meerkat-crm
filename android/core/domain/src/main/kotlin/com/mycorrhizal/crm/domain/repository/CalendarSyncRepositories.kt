package com.mycorrhizal.crm.domain.repository

import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.CalendarSubscriptionInput
import com.mycorrhizal.crm.model.network.CalendarSyncResult
import com.mycorrhizal.crm.model.network.ContactSubscription

/**
 * Calendar (CalDAV/iCal) subscription CRUD + sync-now (issue #390's Android
 * follow-up, #628). Mirrors web's `frontend/src/api/calendars.ts` exactly;
 * every returned [CalendarSubscription] carries the full sync-health surface.
 */
interface CalendarSubscriptionRepository {
    suspend fun list(): Result<List<CalendarSubscription>>
    suspend fun create(input: CalendarSubscriptionInput): Result<CalendarSubscription>
    suspend fun update(id: Int, input: CalendarSubscriptionInput): Result<CalendarSubscription>
    suspend fun delete(id: Int): Result<Unit>
    suspend fun sync(id: Int): Result<CalendarSyncResult>
}

/**
 * Contact (CardDAV) subscriptions — read-only on Android (issue #628 scope
 * note 4: web has no create/edit/delete UI for these either, so a full
 * management screen is out of scope; only the sync-health list is wired).
 */
interface ContactSubscriptionRepository {
    suspend fun list(): Result<List<ContactSubscription>>
}
