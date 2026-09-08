---
title: Getting Started
nav_order: 2
---

# Getting Started

## Installation

### System requirements

Mycorrhizal CRM runs on Linux (`x86_64` or `arm64`) under Docker (Engine
`>= 23.0` with Compose V2). Browsers: a current Chrome/Edge/Firefox, or
Safari/iOS `16.4+`. The Android app requires Android 8.0+ (`minSdk 26`). The
database directory **must be on local disk** — never a network filesystem.
See [Supported versions](supported-versions.html) for the full statement,
including what "supported" means and what happens on an unsupported version.

### Docker compose

Mycorrhizal CRM ships as a single all-in-one image that bundles the frontend and backend into one container, built locally from source via the repository's `Dockerfile` (no published registry image is required).

Clone the repository, copy the [sample docker compose file](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/docker-compose.yml) as well as [sample env file](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.env.example) and rename the env file to `.env`.

After adjusting the environment variables as needed you can run:

```sh
docker compose up -d --build
```

These steps are exercised on every infra change (and nightly) by the
`deploy-smoke` CI job, which follows this page from an empty state and then runs
a full register → create contact → attach → search → export workflow against the
fresh instance (issue #450).

### Environment variables

| Variable | Description |
|---|---|
| `JWT_SECRET_KEY` | Random string used for JWT signing (minimum 32 bytes; the server refuses to start with a placeholder or weak secret — generate with `openssl rand -base64 32`) |
| `FRONTEND_URL` | Used for CORS headers. Wildcard (`*`) is allowed but not recommended for production use |
| `RESEND_API_KEY` | API key for [Resend](https://resend.com), used to send e-mail notifications. The generous free tier is more than enough for any personal setup |
| `RESEND_FROM_EMAIL` | Sender e-mail address for Resend, needs to be configured in Resend |
| `SMTP_HOST` | SMTP server hostname, used to send e-mail notifications via your own mail server (alternative or in addition to Resend) |
| `SMTP_PORT` | SMTP server port (default `587`; use `465` with `SMTP_USE_TLS=true`) |
| `SMTP_USERNAME` | SMTP auth username (leave empty for unauthenticated relays) |
| `SMTP_PASSWORD` | SMTP auth password |
| `SMTP_FROM_EMAIL` | Sender e-mail address for SMTP |
| `SMTP_USE_TLS` | Set to `true` for implicit TLS (port 465); otherwise STARTTLS is used |
| `CARDDAV_ENABLED` | When set to `true` the application acts as a CardDAV server which allows contacts to be synced with your phone |
| `DISABLE_REGISTRATION` | When set to `true`, new user registration is disabled (existing users can still log in). Default is `false`. **Recommended for any multi-user instance** — see [Supported versions → The deployment shape](supported-versions.html#the-deployment-shape) |
| `DATA_PATH` | Host directory where the database file should be stored |
| `PHOTOS_PATH` | Host directory where the contact photos should be stored |
| `JWT_EXPIRY_HOURS` | Token expiry, i.e. after how many hours you will need to sign into the application again. Default is 96 hours (4 days) |
| `OIDC_PROVIDER_URL` | Issuer URL of your OIDC provider (i.e. endpoint URI for your provider). Required to enable SSO |
| `OIDC_CLIENT_ID` | OAuth2 client ID registered with your OIDC provider |
| `OIDC_CLIENT_SECRET` | OAuth2 client secret registered with your OIDC provider |
| `OIDC_AUTO_PROVISION` | When `true`, a new account is automatically created on first SSO login. Default is `false` |
| `OIDC_TRUST_EMAIL` | When `true`, skips the `email_verified`  requirement when linking an OIDC identity to an existing account by email. Safe to enable for self-hosted providers (e.g. Authentik) where you control all user accounts. Default is `false` |
| `OIDC_SCOPES` | Comma-separated list of OAuth2/OIDC scopes to request. Default is `openid,email,profile` |
| `REMINDER_TIME` | Time of day at which reminder notifications are sent, in `HH:MM` format (24-hour). Default is `06:00`. **Server-wide, not per user** — every user on the deployment shares this one clock. See [Temporal semantics (ADR 0015)](adrs/0015-temporal-semantics.md) for the DST and timezone rules |
| `REMINDER_TIMEZONE` | Timezone used for scheduling reminder emails. Must be a valid [IANA timezone name](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) (e.g. `Europe/Berlin`). Default is `UTC`. Applied to every user (one clock per deployment); a multi-timezone deployment should expect all users on this zone |

SSO is disabled unless all three of `OIDC_PROVIDER_URL`, `OIDC_CLIENT_ID`, and `OIDC_CLIENT_SECRET` are set.

Other variables are found in the [sample env file](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.env.example).

The backend process runs as a non-root user (default UID/GID 1001), and a startup script chowns the data and photo directories to that user. Run `id` on your host to find your UID and GID and set them as `PUID`/`PGID` in your `.env` file if you prefer folders to be owned by your host user (optional).

## Post-Installation Setup

When running Mycorrhizal CRM you can access the application under the specified port (default is `7300`). 
To get started you need to register a user. The first user will automatically receive administrator rights and therefore be able to access the admin panel in the settings menu.

If you plan to host other people on this instance, read [Supported versions → The deployment shape](supported-versions.html#the-deployment-shape) first: it states the cross-user isolation guarantee, what an admin can and cannot see, and why a multi-user instance should run with `DISABLE_REGISTRATION=true`.

## Backup

Make regular backups of your data: the SQLite database **and** the photo and attachments
directories (they live outside the database file). See [Deployment → Backups](deployment.html#backups)
for the tested online and offline procedures — in particular, do not copy the `.db` file while the
server is running, since the database uses WAL mode.
