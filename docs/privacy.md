---
title: Privacy
nav_order: 7
---

# Privacy & data minimization

This page is for someone deciding whether to adopt Mycorrhizal CRM, and for the operator running
an instance for other people. It says plainly what the software stores, what the operator can and
cannot see, and which responsibilities stay with the operator rather than being handled by the
software.

The full, store-by-store technical inventory is
[`security/pii-inventory.md`](security/pii-inventory.md). This page is the summary.

## What an instance stores

Mycorrhizal CRM is a record of your relationships. Most of what it holds is **about other people**
— their names, contact details, birthdays, the notes you write after seeing them, things you have
learned about their preferences, how they are related to each other. Those people did not agree to
be in it and cannot see their entry. That is inherent to what a personal CRM is; it is also the
reason this page exists.

Concretely, an instance stores:

- **Contacts** and everything attached to them: notes, activities, reminders, life events,
  preferences, gifts, custom fields, relationship links, group memberships, photos, attachments.
- **Your account**: username, email address, a bcrypt hash of your password, language and
  date-format preference, and — if you enable them — a two-factor secret and recovery-code
  hashes. Passwords and 2FA secrets are never logged, never exported, and never written into the
  audit trail.
- **Operational records**: an audit trail of changes (used for the Undo button and for
  investigating problems), a full-text search index, delivery records for reminders and webhooks,
  and sync bookkeeping for CardDAV/CalDAV.
- **Anything you connect**: credentials for external services (Nextcloud/WebDAV, Paperless,
  Immich, Seafile, ntfy, Gotify), stored encrypted; calendar and address-book subscriptions.

## What does *not* happen

- **No telemetry.** No analytics, no crash reporting, no usage statistics, no "call home". The
  only data that leaves an instance is what the operator explicitly turns on: CardDAV/CalDAV sync,
  outbound email, push notifications, webhooks, and the optional integrations above.
- **No third-party trackers** in the web app or the Android app.
- **No advertising or profiling.**

## What the operator can and cannot see

An instance can host several independent users. If yours does, the person running it is your data
controller — and the controller for everyone else's data on that instance too.

- The operator has an **admin role** that can create, edit, and delete user accounts, reset
  someone's second factor, and trigger maintenance jobs. That is the whole of it.
- The admin role **cannot read another user's contacts, notes, or activities** through the
  application. There is no API for it.
- The operator **can** delete a user account, which cascades and removes that user's data.
- The operator runs the machine, so they have access to the **database file, the uploaded files,
  and the backups** directly. No self-hosted application can prevent this, and Mycorrhizal CRM
  does not claim to. If you need the operator not to be able to read your data at rest, that is a
  deployment decision (full-disk encryption, an operator you trust) — not something the software
  provides on its own.
- **Usernames are visible to every other user on the same instance** (this is needed for sharing
  a contact with another user). Email addresses and everything else are not.

[Supported versions → The deployment shape](supported-versions.html#the-deployment-shape) states the
cross-user isolation guarantee in full, along with the per-user vs per-instance split and the
registration recommendation for a multi-user instance.

## Deleting a person

When you delete a contact, the contact and everything attached to it is hidden immediately and
permanently removed after a **30-day grace period** (the window that makes Undo possible). The
search index drops it at once. Two things outlive that on purpose:

- **The audit trail** keeps a redacted snapshot of the deleted contact for **90 days**, so a
  change can be investigated or undone. After that it is purged.
- **Backups** taken before the deletion still contain the contact. The application deliberately
  cannot reach into or expire operator backups; pruning them is the operator's job. Restoring an
  old backup brings back anything that had not yet passed the 30-day purge when the snapshot was
  taken.

If someone is mentioned only inside a free-text note or activity that belongs to a *different*
contact, deleting them means editing that note — search will find every mention.

## Your data, your rights

- **Export** everything at any time: `Settings → Export`, in CSV, vCard 3, vCard 4, or JSContact.
  You can also export your own audit log.
- **Delete** your account and all its data; the operator can also do this for you.
- Fields you mark **private** or **secret** are held back from exports and from CardDAV/CalDAV
  sync automatically.

## For operators

Before hosting this for other people, read
[`security/deployment-baseline.md`](security/deployment-baseline.md) (what the application does
*not* secure) and the ["Backup confidentiality & retention"](deployment.md) section of the
deployment guide. You are the data controller for every user on the instance, including their
notes about people they have never met. The software gives you per-user scoping, per-user
deletion, export, and a private/secret filter; the legal and organisational duties of a
controller remain yours.
