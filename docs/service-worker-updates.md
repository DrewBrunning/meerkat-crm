# Service-worker updates and the recovery page

The web app is a PWA: after the first visit a service worker (`frontend/src/service-worker.ts`)
takes over and serves the app shell and its bundles from `CacheStorage`. That is what makes the
app load fast offline and what hosts Web Push. It also has a cost, and it is the point of this page:

> Once a worker is in control, a refresh is served from *cache*, not from the server. A bad deploy
> can therefore keep serving broken content after the deploy that fixed it, and no amount of
> refreshing helps.

## How a normal update reaches a user

1. On a later visit the browser fetches `/service-worker.js` (served `no-cache`, never immutable)
   and notices the bytes changed — a new build is available.
2. The new worker installs and then **waits**: it does not take over while any tab is still open on
   the old build. The app shows a non-blocking "A new version of Mycorrhizal CRM is available"
   prompt (`ServiceWorkerUpdatePrompt`); clicking **Reload** hands control to the new worker
   (`applyUpdate` in `src/serviceWorkerUpdates.ts`) and reloads onto the new build.
3. On activation the old build's precache entries are cleaned up, so repeated releases do not grow
   the cache without bound.

That lifecycle (install → waiting → activate → claim), the cache transition, offline-through-deploy
convergence and rapid repeated releases are all pinned by the service-worker upgrade suite — see
"Service-worker upgrade suite" in `docs/development/testing.md`.

## The escape hatch: `/_recovery.html`

For the case where the worker itself is broken — it installs and takes over but serves a broken
app — there is a recovery page at:

```
https://your-host/_recovery.html
```

It is a self-contained static page (shipped from `frontend/public/`, served by nginx with
`Cache-Control: no-store`) that:

1. deletes **every** `CacheStorage` bucket, and
2. unregisters **every** service worker registration,

then reloads the app from the network, which re-registers a fresh worker from whatever the server
currently serves.

It can be reached from behind a broken worker because the worker is written to leave `/_*`
navigations to the network (`src/service-worker.ts`), and it is excluded from the precache manifest
(`vite.config.ts` → `injectManifest.globIgnores`), so a broken cached copy can never shadow it.

**When to use it:** a page that will not update and is not served by a healthy worker — e.g. after
an interrupted deploy, or when told to by support. It clears local caches only; it does not touch
your account data, which lives on the server.

The full stuck-worker → recovery flow is tested by the service-worker upgrade suite's
escape-hatch spec against a deliberately broken worker.
