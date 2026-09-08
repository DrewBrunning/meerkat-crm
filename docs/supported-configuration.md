---
title: Supported configuration
nav_order: 18
---

# Supported configuration: multi-user per instance

This page states the **supported deployment shape** — as opposed to the supported
*versions*, which are the [runtime matrix](development/supported-runtime-matrix.md)
(engineering) and, for operators, the [deployment guide](deployment.md). It is written
for two readers: an operator deciding how to run an instance, and someone deciding
whether to accept an account on an instance **someone else** runs.

## The decision

**Multi-user-per-instance is a supported `1.0.0` configuration.** One deployment hosts
multiple independent users, each with their own contacts, relationship graph,
integrations, notifications, and settings. Cross-user isolation is a **release-blocking
correctness property**, not a best-effort nicety: "user A must not be able to reach
user B's data through any endpoint" is tested against every route
(see [The isolation guarantee](#the-isolation-guarantee) below).

The implementation has assumed multi-user from the start — `users.email`/`username` are
unique, `DeleteUser` hard-deletes so a re-registration is not blocked forever,
`admin_user_controller.go` has its own cascade sweep, and `contact_shares`
(migration `000008`) moves a filtered contact copy between two users on one instance.
This page confirms and documents that; it is not describing new work.

## Per-user vs per-instance

Everything a user creates or connects is scoped to that user and invisible to every
other user:

| Per-user (scoped, private to the user) |
|---|
| Contacts and everything hung off them: notes, activities, reminders, life events, preferences, gifts, custom field values, photos, attachments |
| The relationship graph: relationship edges, circles, households, tags and their memberships |
| Custom **field definitions** (each user defines their own) |
| Integrations and their stored credentials + cached remote data: CardDAV/CalDAV subscriptions, Immich, Paperless, Seafile, WebDAV/Nextcloud |
| Notification channels and delivery records: email, ntfy, Gotify, Web Push, and registered mobile devices |
| Account settings, language, date-format preference, API tokens, 2FA secret and recovery codes |
| The full-text search index rows (filtered by `user_id` in every query) |
| Audit-trail rows (each row carries `user_id`; used for that user's Undo) |
| The user's default **self-contact**, created at registration |

Some things are **one per instance** and shared by everyone on it:

| Per-instance (shared) |
|---|
| The single SQLite database file, and its one writer |
| The in-process `gocron` scheduler. It fires once per interval; the work inside a tick — cadence recomputation, CardDAV/CalDAV sync, reach-out scanning — iterates over **all** users, so its cost scales with user count independently of how much data any one user has (this is the axis [#468](https://github.com/DrewBrunning/mycorrhizal-crm/issues/468) / [#498](https://github.com/DrewBrunning/mycorrhizal-crm/issues/498) add to the scale profiles) |
| `REMINDER_TIME` / `REMINDER_TIMEZONE` — one clock for the whole deployment, not per user ([ADR 0015](adrs/0015-temporal-semantics.md)) |
| IP-based auth rate limiters |
| OIDC/SSO provider configuration, the update check, outbound email transport |
| `JWT_SECRET_KEY`, `COOKIE_*`, `FRONTEND_URL`, and the uploaded-files directory |
| The **admin role** (see [What an admin can and cannot see](#what-an-admin-can-and-cannot-see)) |
| Operator backups — they contain every user's data; the application deliberately cannot reach into or expire them |

## The isolation guarantee

If you hold an account on an instance someone else runs, this is what the software
guarantees about the other account holders:

**No other user can read, list, search, export, or modify any of your contacts, your
relationship graph, your notes, activities, reminders, life events, custom fields,
integrations, or settings — through any API endpoint.**

How it is enforced:

- Every handler scopes its query by `user_id`. Graph entities that are keyed by a
  contact UID rather than directly by `user_id` (relationship edges, circle members,
  contact tags, household members, field values, sync links, calendar links) are
  resolved with **both** clauses — e.g. `relationship_edge_controller.go` resolves a
  contact with `vcard_uid = ? AND user_id = ?`, so naming another user's contact UID
  does not reach it.
- This is guarded mechanically, not only by review:
  `backend/routes/authorization_matrix_test.go` enumerates **every registered route**
  from the live router and probes each against six personas — including
  **"authenticated user B → user A's resource"**. A new route that is not scoped, or
  has no declared authorization row, **fails CI**. `backend/cmd/bolacheck` is the
  companion static check.
- Fields you mark **private** or **secret** are additionally held back from exports and
  from CardDAV/CalDAV sync, in the query rather than in the caller.

What another user **can** see: your **username**. It is shown in the user directory
because it is how a contact share is addressed to you. Your email address and
everything else are not visible to other users.

The one sanctioned path for data to cross a user boundary is **contact sharing**
(`contact_shares`, migration `000008`): you pick one of your contacts, choose which
sections to include, and the server freezes a filtered JSContact snapshot addressed to
another user, who accepts or declines. It is always sender-initiated, and `secret` /
`private` items are only included if you explicitly opt in. See
[#555](https://github.com/DrewBrunning/mycorrhizal-crm/issues/555) for the detailed
cross-user data-flow review.

## What an admin can and cannot see

The first registered account is an admin (see [Registration](#registration)). An admin
**cannot read another user's contacts, notes, activities, or any other owned
resource** — there is no API for it. The admin routes are user-account CRUD, 2FA reset,
job triggers, and instance health/diagnostics only.

`authorization_matrix_test.go` pins this: on every non-admin item route the `admin`
persona gets the same `404`/`403` as any other non-owner — never a `2xx`. So the
statement is backed by a test, not by a reading of the current handlers.

An admin **can**:

- create, edit, disable, and delete user accounts (a delete cascades and removes that
  user's data);
- reset another user's second factor;
- trigger maintenance jobs and read instance-wide health, job-run, and integrity
  information.

Separately, whoever **operates** the machine has direct filesystem access to the
database file, the uploaded files, and the backups. No self-hosted application can
prevent this and Mycorrhizal CRM does not claim to; if you need the operator to be
unable to read your data at rest, that is a deployment decision (full-disk encryption,
an operator you trust). See
[`security/deployment-baseline.md`](security/deployment-baseline.md) and
[`privacy.md`](privacy.md).

## Intended scale

Mycorrhizal CRM is designed for a **small group of operator-vetted accounts** — a
household, or a handful of people the operator knows and chooses to host. The isolation
guarantee protects against accident and curiosity between people who broadly trust each
other; the resource limits ([#415](https://github.com/DrewBrunning/mycorrhizal-crm/issues/415))
are calibrated for that setting, not for defending a shared instance against its own
account holders.

Running an instance **open to arbitrary strangers** is possible — the isolation
guarantee still holds and is still tested — but it is not the configuration the abuse
controls and operational tooling are tuned for, and it puts the operator in the
position of data controller for people they have never met (see
[`privacy.md`](privacy.md) and
[`security/pii-inventory.md`](security/pii-inventory.md) §7). Mycorrhizal CRM is
MIT-licensed: how you run your instance is ultimately your call, and this section is a
recommendation, not a restriction.

**For any instance with more than one user, run with `DISABLE_REGISTRATION=true` and
create accounts deliberately** from the admin panel.

## Registration

`POST /api/v1/register` — behaviour, as implemented in
`backend/controllers/user_controller.go`:

| | |
|---|---|
| **Default** | Open. Anyone who can reach the instance can create an account. |
| **`DISABLE_REGISTRATION=true`** | Registration returns `403` with `code: registration_disabled`. Existing users still log in; an admin can still create accounts. |
| **First account** | Automatically an admin (`IsAdmin` is set when the user table is empty). |
| **Every later account** | A normal user. `IsAdmin` cannot be set through the registration request — the input DTO excludes it (no mass assignment). |
| **Protections** | Auth rate-limited; minimum-client-version enforced; optional [HIBP](https://haveibeenpwned.com/) breached-password check when `HIBP_CHECK_ENABLED=true`. |
| **SSO** | With OIDC configured, `OIDC_AUTO_PROVISION=true` creates an account on first SSO login; otherwise an unmatched SSO user must be registered first. See [Deployment → OIDC](deployment.md#single-sign-on-oidc). |

Admins can also create accounts directly via `POST /api/v1/admin/users` (the admin
panel in Settings).

These behaviours are covered by tests in
`backend/controllers/user_controller_test.go`.

## How this is verified

| Claim | Evidence |
|---|---|
| Cross-user isolation holds on every route, including the graph entities keyed by contact UID | `backend/routes/authorization_matrix_test.go` (every route × six personas, fails on an unscoped or undeclared route); `backend/cmd/bolacheck` |
| An admin cannot read another user's owned resources | same test — `admin` persona gets `404`/`403` on every non-admin item route |
| Registration behaviour matches this page | `backend/controllers/user_controller_test.go` |
| Per-user job/index cost is a distinct scale axis | [`development/scale-profiles.md`](development/scale-profiles.md) (user-count column), [`development/capacity-under-constraint.md`](development/capacity-under-constraint.md) ("Many users" column, `LOADSMOKE_USERS`) |
| The operator's data-controller position over other users' data | [`privacy.md`](privacy.md), [`security/pii-inventory.md`](security/pii-inventory.md) §7 |

---

Issue [#558](https://github.com/DrewBrunning/mycorrhizal-crm/issues/558). Milestone
`v0.6.11`; gate [#541](https://github.com/DrewBrunning/mycorrhizal-crm/issues/541). The
`1.0.0` gate ([#525](https://github.com/DrewBrunning/mycorrhizal-crm/issues/525))
tracks "the supported deployment shape is documented" and "cross-user isolation holds".
