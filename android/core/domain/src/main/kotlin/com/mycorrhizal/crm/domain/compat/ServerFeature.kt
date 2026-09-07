package com.mycorrhizal.crm.domain.compat

import com.mycorrhizal.crm.model.AppVersion

/**
 * The v0.6.0 baseline floor shared by every baseline [ServerFeature] and by the
 * "server too old" gate ([ServerCapabilities.isServerSupported]). Matches the
 * backend's migration floor: the oldest server this app will talk to.
 */
private val SERVER_BASELINE: AppVersion = AppVersion(0, 6, 0)

/**
 * The exhaustive registry of server-backed capabilities this app can present
 * to a user, each with the oldest server release that provides it (issue #692).
 *
 * This is the "feature → minServerVersion" map the issue calls for. It is the
 * single place a floor lives for a UI surface: a screen that offers a
 * capability checks [ServerCapabilities.isSupported] against the server version
 * the session fetched from /health (once per session, issue #528) and hides or
 * disables the entry point when the connected server predates [minServerVersion].
 *
 * ## How a floor is chosen
 *
 * A floor is the FIRST server release whose route table shipped the endpoints a
 * capability needs — derived from the git tags (v0.6.0 … v0.6.9) rather than
 * invented. The whole authenticated surface shares one baseline,
 * [ServerCapabilities.MIN_SUPPORTED_SERVER_VERSION] (0.6.0), because the route
 * table is effectively unchanged across the v0.6.0–v0.6.9 line: a server below
 * the baseline is refused outright (the "server too old" gate), and a server at
 * or above it supports everything on the baseline. Only capabilities whose
 * endpoints arrived after v0.6.0 carry a higher floor:
 *
 *  - [SYSTEM_EVENTS]       — /admin/system-events* first shipped in v0.6.2
 *  - [AUDIT_EXPORT]        — /audit/export first shipped in v0.6.1
 *  - [API_TOKENS_ADVANCED] — /api-tokens/revoke-all + /:id/rotate in v0.6.1
 *  - [DEVICE_GRANT_SIGNIN] — /auth/device/session + /auth/device/grants* are
 *    newer than every released tag (they ship in the v0.6.10 cycle)
 *
 * Raising a floor (moving a capability onto a newer server) is a MAINT-02
 * breaking-change event and needs a recorded rationale, exactly like the
 * server-side MIN_CLIENT_VERSION floor (docs/client-compatibility-policy.md).
 *
 * Entry points whose capability is baseline-floored are NOT individually
 * gated in their screens: the "server too old" gate above the whole tree makes
 * those checks tautological, and gating them would be dead code. This registry
 * still lists them so the map is complete and a future floor raise on any of
 * them is a data-only change.
 */
enum class ServerFeature(val minServerVersion: AppVersion) {

    // --- The v0.6.0 baseline (the oldest server this app will talk to) ---

    /** Contact list/detail/create/edit/delete and the flat list contract. */
    CONTACTS(SERVER_BASELINE),
    /** Per-contact activity timeline (list/create/edit/delete). */
    CONTACT_ACTIVITIES(SERVER_BASELINE),
    /** Per-contact notes timeline (list/create/edit/delete). */
    CONTACT_NOTES(SERVER_BASELINE),
    /** Per-contact reminders (list/create/edit/delete/complete). */
    CONTACT_REMINDERS(SERVER_BASELINE),
    /** Relationship edges between contacts. */
    CONTACT_RELATIONSHIPS(SERVER_BASELINE),
    /** Cadence policies / overdue reach-outs. */
    CONTACT_CADENCE(SERVER_BASELINE),
    /** Life events on a contact. */
    CONTACT_LIFE_EVENTS(SERVER_BASELINE),
    /** Gift ideas on a contact. */
    CONTACT_GIFTS(SERVER_BASELINE),
    /** Contact preferences. */
    CONTACT_PREFERENCES(SERVER_BASELINE),
    /** Conversation agenda. */
    CONTACT_CONVERSATION_AGENDA(SERVER_BASELINE),
    /** The prep-view briefing (GET /contacts/:id/briefing). */
    CONTACT_PREP(SERVER_BASELINE),
    /** Contact attachments (list/download/upload). */
    CONTACT_ATTACHMENTS(SERVER_BASELINE),
    /** Contact merge (preview + commit). */
    CONTACT_MERGE(SERVER_BASELINE),
    /** Contact shares inbox/outbox + recipient directory. */
    CONTACT_SHARES(SERVER_BASELINE),
    /** Contact network graph from a contact. */
    CONTACT_NETWORK(SERVER_BASELINE),
    /** The top-level network graph. */
    NETWORK(SERVER_BASELINE),
    /** Dashboard composite (birthdays, reminders, cadence, reach-out). */
    DASHBOARD(SERVER_BASELINE),
    /** Activities inbox (drawer "activities"). */
    ACTIVITIES_INBOX(SERVER_BASELINE),
    /** Notes inbox (drawer "notes"). */
    NOTES_INBOX(SERVER_BASELINE),
    /** Circles + members. */
    CIRCLES(SERVER_BASELINE),
    /** Tags. */
    TAGS(SERVER_BASELINE),
    /** Households + members + address suggestions. */
    HOUSEHOLDS(SERVER_BASELINE),
    /** Audit log view + undo. */
    AUDIT_LOG(SERVER_BASELINE),
    /** Data review: relationship/address suggestions + scan/apply. */
    DATA_REVIEW(SERVER_BASELINE),
    /** Dataset export (CSV / vCard 3+4 / JSContact). */
    DATASET_EXPORT(SERVER_BASELINE),
    /** Import (device contacts, vCard file, records). */
    IMPORT(SERVER_BASELINE),
    /** Two-factor status/setup/confirm/disable/recovery + the 2FA login step. */
    TWO_FACTOR(SERVER_BASELINE),
    /** Webhooks + deliveries + test. */
    WEBHOOKS(SERVER_BASELINE),
    /** Notification channel (ntfy/Gotify) configuration + test. */
    NOTIFICATION_CHANNELS(SERVER_BASELINE),
    /** API token create/list/revoke. */
    API_TOKENS(SERVER_BASELINE),
    /** Immich configuration + people. */
    IMMICH(SERVER_BASELINE),
    /** Admin user management. */
    ADMIN_USERS(SERVER_BASELINE),

    // --- Capabilities whose endpoints shipped after v0.6.0 ---

    /** Admin system events / subsystem health / error aggregation (v0.6.2+). */
    SYSTEM_EVENTS(AppVersion(0, 6, 2)),
    /** Audit-log CSV export (v0.6.1+). */
    AUDIT_EXPORT(AppVersion(0, 6, 1)),
    /** API-token rotation + revoke-all (v0.6.1+). */
    API_TOKENS_ADVANCED(AppVersion(0, 6, 1)),
    /** Biometric device-grant sign-in: enroll/exchange/revoke (v0.6.10+). */
    DEVICE_GRANT_SIGNIN(AppVersion(0, 6, 10)),
}
