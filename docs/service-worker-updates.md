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

## The stale-contract backstop: automatic forced reload

The update prompt above is *offered*; a user who ignores it stays on the old build, which is fine
while the old build keeps working. It stops being fine when the server deploys a release that this
tab's build is genuinely incompatible with — its `min_client_version` now sits above this tab's
build, or the server announces a different `api_contract_version` than the bundle speaks
(`docs/client-compatibility-policy.md`, issue #475). The app then **forces** the tab onto the build
the server serves instead of letting it error:

- On load, on an interval, and whenever the tab gains focus (the long-lived-tab-across-a-deploy
  case), the app polls the unauthenticated `GET /health` and classifies the result
  (`frontend/src/staleClient/`): compatible / update-available / blocked.
- **update-available** (server release newer than this bundle) is non-blocking: it kicks
  `registration.update()` so the ordinary update prompt above can surface — the browser's own
  ~24 h update check is not enough for a tab that never navigates.
- **blocked** triggers a forced reload that goes *through the service worker* (fetch the new
  worker, `SKIP_WAITING`, reload on `controllerchange`) rather than a bare reload, which the old
  controlling worker would serve from its own stale cache. The reload happens automatically only
  when no form is dirty; unsaved input routes through a consent dialog instead (every editing
  surface reports into the shared dirty registry via `useBeforeUnloadGuard`), and a reload that
  cannot converge surfaces a manual "Reload now" dialog rather than looping forever.
- **Fail open:** an unreachable or malformed `/health` changes nothing. A network blip must never
  brick a PWA whose main feature is working offline.

The WEB-01 scenarios are covered by the same suite (its `/health` harness endpoint serves the
active build's own identity and lets a test raise the floor, flip `api_contract_version`, or fail
`/health`).

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
