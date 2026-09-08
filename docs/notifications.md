---
title: Notifications
nav_order: 5
has_children: false
---

# Notifications

Reminders can reach you through four channels. Email is configured on the server; ntfy, Gotify and browser push are configured per user, under **Settings → Notifications**, because each user has their own topic, token and devices. For who owns what when a channel breaks, how to recognize a failure, and how to diagnose it, see [Integrations: ownership, diagnostics, and what breaks](integration-ownership.md).

| Channel | Configured where | What you need |
|---|---|---|
| Email | Server, in `.env` | A [Resend](https://resend.com) API key, or SMTP host and credentials |
| ntfy | Per user, in the app | Your ntfy server URL and a topic |
| Gotify | Per user, in the app | Your Gotify server URL and an application token |
| Browser push | Per user, in the app | Nothing — but the app must be served over HTTPS |

All enabled channels deliver in the same daily run, at `REMINDER_TIME` in `REMINDER_TIMEZONE`. Those two settings are **server-wide and there is exactly one such clock**: every user on the deployment is scheduled against the operator's zone, and there is no per-user timezone (per-user zones would be a new feature, not a current option). A deployment serving people in several timezones should expect all of them to be woken on the operator's clock.

Two consequences are worth knowing up front:

- **DST** — the daily run fires once per local wall-clock time in `REMINDER_TIMEZONE`. If `REMINDER_TIME` lands in a spring-forward skipped hour it still runs once (normalized to the first valid time after the transition); if it lands in a fall-back repeated hour it runs once, on the earlier pass. It is never skipped and never doubled; a run the server was down for is recovered at next start (see ADR 0011). These rules are pinned in [ADR 0015](adrs/0015-temporal-semantics.md).
- **"Today" follows that same zone.** A birthday or a due reminder is "today" when the calendar day in `REMINDER_TIMEZONE` says so — including a 29-Feb birthday, which is celebrated on **1 March** in a non-leap year (a real decision, not a bug: see [ADR 0015](adrs/0015-temporal-semantics.md)).

## Email

Set either the Resend or the SMTP variables in your `.env` — or both, in which case every email is sent through both. See [Getting Started → Environment variables](getting-started.html#environment-variables).

Email behaves differently from the other three channels in two ways worth knowing:

- **It is opt-in per reminder.** A reminder only produces an email if **Send email notification** is ticked on that reminder. The other channels are all-or-nothing per user: once enabled, they deliver every due reminder.
- **It is a daily digest, and it carries birthdays.** All of a day's reminders arrive in one message, and birthdays falling today are included. The other channels send one message per reminder and **do not** include birthdays — if birthday notifications matter to you, keep email on.

Sending email also requires a valid email address on your account.

## ntfy

Enter your ntfy server's base URL (for example `https://ntfy.sh`, or your own instance) and the topic to publish to, then turn on **Send reminders via ntfy**.

Subscribe to the same topic in the ntfy app or web UI to receive the messages. Note that on a public server such as ntfy.sh, anyone who knows the topic name can read it — choose an unguessable topic, or self-host.

## Gotify

Enter your Gotify server's base URL and an application token, then turn on **Send reminders via Gotify**. Create the token in your Gotify instance under **Apps**.

The token is stored encrypted, using a key derived from `JWT_SECRET_KEY`, and is never returned by the API. The field shows whether a token is already stored; leave it empty when saving to keep the existing one.

Because the key is derived from `JWT_SECRET_KEY`, changing that variable invalidates every stored Gotify token — along with calendar passwords and Immich API keys. They have to be re-entered.

## Browser push

Open **Settings → Notifications**, turn on **Send reminders via browser push**, and click **Enable browser notifications**. Your browser asks for notification permission, and the browser is added to the registered device list. Repeat on each device you want notified; remove a device from the same list.

**This requires HTTPS.** Push notifications are delivered to a service worker, and browsers refuse to register a service worker on a plain-HTTP origin. `localhost` is exempt, so local testing works, but a LAN deployment reached over `http://` cannot register a device. The other three channels have no such requirement.

There is nothing to configure on the server. The VAPID keypair that identifies this instance to the browsers' push services is generated once, on first use, and stored in the database.

A registration can lapse — if you clear site data, or the browser's push service drops the subscription, that device stops receiving notifications and is removed from the list automatically the next time a delivery fails. Re-enable it from the same screen.

## Testing a channel

Each configured channel has a **Send test notification** button. It delivers immediately, without waiting for the daily run and without touching reminder delivery state, and reports the server's actual reason on failure rather than failing quietly — which is the fastest way to find a wrong URL, a bad token, or a blocked address.

## Private addresses and `WEBHOOK_BLOCK_PRIVATE_URLS`

Self-hosted ntfy and Gotify instances usually live on a private address (`http://gotify:8080`, a LAN IP, a Docker service name). Since the server itself makes these requests, posting to user-supplied URLs on internal addresses is an SSRF risk on a shared instance — so it is governed by one setting:

- `WEBHOOK_BLOCK_PRIVATE_URLS=false` (the default) lets the server reach private addresses. Correct for a personal, self-hosted setup.
- `WEBHOOK_BLOCK_PRIVATE_URLS=true` blocks them. Set this on a multi-tenant or cloud deployment, where you do not control who can enter a URL.

The same setting also governs outgoing webhooks. The settings screen warns when a URL you have typed looks like a private address, so a blocked target shows up while you are configuring it rather than as silence at 06:00.

## When notifications do not arrive

- **Nothing at all, on any channel.** Check `REMINDER_TIME` and `REMINDER_TIMEZONE` — the run happens once a day at that time, in that timezone, and the default is `06:00 UTC`, which may be the middle of your night.
- **Email only.** Confirm your account has an email address, and that the reminder has **Send email notification** ticked. Reminders do not email by default.
- **ntfy or Gotify only.** Use **Send test notification** — it surfaces the server's reason. A private-address target with `WEBHOOK_BLOCK_PRIVATE_URLS=true` is the common one.
- **Browser push only.** Confirm the site is served over HTTPS, that the browser's notification permission is granted, and that the device still appears in the registered list.
- **Birthdays specifically.** Only email carries birthdays. The other channels send reminders only.
