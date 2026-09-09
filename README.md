<p align="center">
<img width="192" height="192" alt="mark-mycelium-light-192" src="https://github.com/user-attachments/assets/5f8e7a54-b8e6-408a-b594-9131739822da" />
</p>

# Mycorrhizal CRM

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/DrewBrunning/mycorrhizal-crm?include_prereleases&sort=semver&label=Release)](https://github.com/DrewBrunning/mycorrhizal-crm/releases)

[![Tests](https://github.com/DrewBrunning/mycorrhizal-crm/actions/workflows/unit-tests.yml/badge.svg)](https://github.com/DrewBrunning/mycorrhizal-crm/actions/workflows/unit-tests.yml)
[![Coverage](https://codecov.io/gh/DrewBrunning/mycorrhizal-crm/graph/badge.svg)](https://codecov.io/gh/DrewBrunning/mycorrhizal-crm)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/DrewBrunning/mycorrhizal-crm/badge)](https://scorecard.dev/viewer/?uri=github.com/DrewBrunning/mycorrhizal-crm)
[![OpenSSF Baseline](https://www.bestpractices.dev/projects/14433/baseline)](https://www.bestpractices.dev/projects/14433)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/14433/badge)](https://www.bestpractices.dev/projects/14433)

[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://golang.org)
[![SQLite](https://img.shields.io/badge/SQLite-003B57?logo=sqlite&logoColor=white)](https://www.sqlite.org/)
[![React](https://img.shields.io/badge/React-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org/)
[![Kotlin](https://img.shields.io/badge/Kotlin-7F52FF?logo=kotlin&logoColor=white)](https://kotlinlang.org/)
[![Docker](https://img.shields.io/badge/Docker-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)

Mycorrhizal CRM is a self-hosted contact relationship management solution. It is a fork of [Meerkat CRM](https://github.com/fbuchner/meerkat-crm) by Frederic Buchner.

> ⚠️ Mycorrhizal CRM is a **structural fork** of [Meerkat CRM](https://github.com/fbuchner/meerkat-crm). Because this project introduces custom database schemas, modified tables, and expanded data types (such as bidirectional relationships and custom field mappings), **it is not directly database-compatible with upstream Meerkat.** 
> 
> * Direct database migrations from upstream Meerkat are **NOT** supported at this time.
> * Syncing Options: You can sync contacts between Meerkat and Mycorrhizal using CardDAV (though data not supported by standard CardDAV specs will not sync) or by exporting data from one and importing it into the other (though data not defined in the vCard 3.0 RFC is not guaranteed to persist across the export and import).

## Features & Enhancements On Top Of Upstream Meerkat

Mycorrhizal builds heavily upon the solid foundation of Meerkat, adding modern protocol support, deeper structural relationships, and lifestyle tracking utilities.

Everything in this section is **built and working today** — nothing below is aspirational. (Planned-but-not-built work lives in [GitHub Issues](https://github.com/DrewBrunning/mycorrhizal-crm/issues), not here.)

### Modern Data Formats & Syncing
- **Expanded Protocol Support:** In addition to vCard 3.0 and CardDAV/CalDAV, Mycorrhizal adds full support for **vCard 4.0** and **JSContact**.
- **Flexible Export:** Granular selective field export so you can choose exactly which fields get exported for available formats.
- **Field Sensitivity:** Mark fields as private or secret to exclude them from external sync, contact shares, and the vCard/JSContact exports entirely. Your own full CSV backup still contains them — it is a backup, not a share.
- **Serve Interactions as CalDAV:** Expose activities and life events to a calendar client, and eventually two-way calendar sync.

### Relationships, Households & Pets
- **Bidirectional Relationship Graphs:** Relationships are no longer strictly unidirectional. Creating a connection automatically maps it both ways and facilitates relationship-based searching.
- **Multi-Hop Graph Traversal:** Explore how you're connected to someone through intermediaries, not just direct links.
- **Household Tracking:** Automatically suggests relationships for contacts sharing the same address. Search by a household to pull lists for event invites, mail, or holiday cards.
- **Pets as Contacts:** Add pets directly to your CRM and search for owners using their pet's name with the relationship support.
- **Circles & Tags:** Two distinct grouping mechanisms — circles for the social groups a person belongs to, tags for free-form labeling.

### Data Management & Organization
- **Contact Merging:** Seamlessly merge duplicate contact records.
- **Custom Fields & Mappings:** Support for custom fields, including custom mappings to vCard fields to enable extended properties.
- **Repurposed General Notes:** Upstream Meerkat's journaling notes have been refactored into general notes that can be cleanly associated with any contact at a later point, including a capture inbox for notes you file later.
- **Full-Text Search:** SQLite FTS5 search across contacts, notes and addresses, with relationship synonyms and household scoping.
- **Bulk Operations:** Apply circle, tag and delete operations across many contacts at once.
- **One-Time Cross-User Sharing:** Share specific contacts with other users on the same instance, including granular selection of which fields are shared. *(Note: This is a one-time point-in-time copy/share to the target user rather than an ongoing real-time sync).*
- **Files & Documents:** Upload and associate documents and files with a contact
- **Full Backup & Restore:** `make backup` produces a consistent online SQLite snapshot (safe while the server runs), with a documented restore procedure covering the database, photos and attachments together. A scheduled integrity check and a periodic restore drill (actually restoring the latest backup into a scratch database and comparing row counts) run automatically to catch a corrupt or non-restorable backup before you'd ever need it. See [Deployment → Backups](https://drewbrunning.github.io/mycorrhizal-crm/deployment.html#backups).

### Staying In Touch
- **Favorites:** Star key contacts for a dedicated filter and a dashboard shortlist.
- **Cadence & Relationship Health:** Set how often you intend to be in touch with someone and see who has gone quiet. Cadence resets on a real interaction, not on ticking off a task.
- **Prep View:** A per-person briefing pulling together recent history, open agenda items and life events before you see or call someone.
- **Conversation Agenda:** Keep a running list of things to raise next time you talk to a given person.
- **Notification Channels:** Reminders can be delivered by email, [ntfy](https://ntfy.sh), [Gotify](https://gotify.net), or browser push — see [Notifications](#notifications) below.

### Tracking & Integrations
- **Expanded Life Event Reminders:** Automated reminders for major life events like anniversaries, complementing existing birthday tracking, organised into categories.
- **Gift Tracking:** Modeled after [Monica](https://github.com/monicahq/monica), allowing you to track gift ideas, past gifts given, and received items, with links and notes.
- **Immich Integration:** Link contacts directly to identified persons/faces in an [Immich](https://github.com/immich-app/immich) instance to easily view photos of individuals right from their profile.
- **File Sharing Integrations:** Link contacts to documents in [Paperless-ngx](https://docs.paperless-ngx.com/) and to files/folders in [Seafile](https://www.seafile.com/) or [Nextcloud](https://nextcloud.com/)/[ownCloud](https://owncloud.com/) (via WebDAV). These are read-only links — the files stay on your own server.
- **External Links:** Deep-link a contact into other systems you run, with a configurable link-type registry (`tel:`, `sms:`, WhatsApp, and anything else you define).
- **Audit Trail:** A per-record history of what changed and when.

### Account Security
- **Two-Factor Authentication:** TOTP (RFC 6238) as a second factor on interactive login, with single-use recovery codes for when you lose the device. SSO via OIDC remains available as an alternative; CardDAV/API-token auth is unaffected either way.

### Native Apps
- **Android app:** A Kotlin/Jetpack Compose client in `android/` — login (including OIDC SSO), contact list + detail with offline cache and favorites, a tablet two-pane layout (NavigationRail + list/detail), dashboard and prep view, call/SMS tracking with a quick-capture overlay, device-contacts and VCF import, circles, tags, households, relationships and the network graph, the timeline (life events, gifts, preferences, agenda), reminders, cadence, contact sharing, audit trail, FCM push notifications, and per-user settings (profile, notification channels, webhooks, link actions).

---

## Notifications

Reminders can be delivered through four channels. Email is configured server-side; the other three are configured per user, in the app under **Settings → Notifications**, because each user has their own topic, token and devices.

| Channel | Configured | What you need |
|---|---|---|
| **Email** | Server (`.env`) | Either a [Resend](https://resend.com) API key, or SMTP host/credentials. Both may be set, in which case each email is sent through both. |
| **ntfy** | Per user, in-app | Your ntfy server URL and a topic. Works with the public ntfy.sh or a self-hosted instance. |
| **Gotify** | Per user, in-app | Your Gotify server URL and an application token. The token is stored encrypted at rest. |
| **Browser push** | Per user, in-app | Nothing to configure. The VAPID keypair is generated once on first use and stored in the database. |

`REMINDER_TIME` and `REMINDER_TIMEZONE` control *when* the daily reminder run happens; they apply to every enabled channel, not just email.

The only server-side setting for ntfy/Gotify/push is `WEBHOOK_BLOCK_PRIVATE_URLS`. It defaults to `false` so the server can reach a self-hosted ntfy or Gotify on a private address — set it to `true` on a multi-tenant or cloud deployment, where posting to internal addresses on user-supplied URLs would be an SSRF risk.

> **⚠️ Browser push requires HTTPS.** Push notifications are delivered to a service worker, and browsers refuse to register one on a plain-HTTP origin. `localhost` is exempt, so local testing works, but a LAN deployment reached over `http://` cannot register a device. The other three channels have no such requirement.

Because the app registers a service worker, it is served cache-first. A newly deployed version therefore announces itself with a "new version available" prompt instead of appearing silently — reload when you see it.

---

## Installation

### Docker (Recommended)

Mycorrhizal CRM ships as a single all-in-one image that bundles the frontend and
backend into one container, built locally from source (no published registry
image is required). The easiest way to run it is with Docker Compose:

1. **Download the Docker Compose file:**
    ```sh
    curl -O https://raw.githubusercontent.com/DrewBrunning/mycorrhizal-crm/main/docker-compose.yml
    curl -O https://raw.githubusercontent.com/DrewBrunning/mycorrhizal-crm/main/.env.example
    ```

2. **Configure environment:**
    ```sh
    # Copy the environment template
    cp .env.example .env

    # Edit with your settings
    nano .env
    ```

3. **Build and start the container:**
    ```sh
    docker compose up -d --build
    ```

4. **Access the application:**
    Open http://localhost:7300 in your browser.


## Contributing

### Bugs and feature requests
This application is currently in beta. Bugs are expected in testing, but are hopefully few and far-between. Please submit issues via GitHub.

### Development
To set up this repository for development, follow these steps:

1. **Clone the repository:**
    ```sh
    git clone https://github.com/DrewBrunning/mycorrhizal-crm.git
    cd mycorrhizal-crm
    ```

1. **Run the backend:**
Ensure you have [Go](https://golang.org/doc/install) installed. Then, set up your environment configuration:
   ```sh
    cd backend
    # Copy the example environment file and configure it with your settings
    cp .env.example .env
    
    # Install dependencies and run
    go mod tidy
    source .env
    go run main.go
   ```
   The project uses an SQLite database for storage. Database migrations run automatically on startup.


1. **Run the frontend (in a second terminal):**
   ```sh
   cd frontend

   yarn install
   yarn start
   ```

You can find a more comprehensive overview for developers in the [developer README](README-developer.md).
