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

## Interrupted deployments: two windows the client must survive (WEB-03, issue #477)

A deployment is not atomic, and on a single-instance self-hosted deployment there is no rolling
update to smooth it over. The container stops, migrations run, the container starts, and anyone with
the app open lives through it. Two distinct windows can hurt a client, and the app handles them with
two different mechanisms:

### 1. Assets that no longer exist (the asset-skew window)

The static frontend and the API ship in one immutable image; nginx serves the app shell and the
backend is proxied behind it. When the new image starts, the previous build's content-hashed assets
are simply gone. A client that loads an `index.html` from the *previous* build during the swap — an
in-flight response, an HTTP cache revalidation that straddled it, or a browser whose service worker
was cleared — then asks for entry/vendor chunks the new build deleted. Every request 404s, the ES
module graph fails to evaluate, and the page would be a blank white screen whose own bundle
(ErrorBoundary, service worker, stale-client detector) never ran.

The one piece of the app that *does* run in that state is the **asset-skew bootstrap**
(`frontend/public/asset-skew.js`, referenced from `<head>` in `index.html` *outside* the bundle, at
a stable un-hashed URL, excluded from the precache and served `no-cache` so a stale cache can never
shadow it). It watches for a failed load of a `/assets/` module script or modulepreload, waits a
moment for an in-flight deploy to finish, then reloads onto whatever the server serves now. The
recovery is bounded (two automatic attempts, tracked in `sessionStorage`, cleared when a boot
succeeds): a deploy that is genuinely stuck shows a plain "could not be loaded" message with a
retry button instead of spinning the tab forever, and a load that succeeds is never re-armed. No
reload ever discards in-progress work — a broken boot mounts nothing, and #557's drafts live in
`sessionStorage`, which survives.

**Why we do not retain the previous build's hashed assets across a deploy** (the ticket's
recommended-action #2, decided deliberately): this project deploys by swapping an immutable image,
so `/usr/share/nginx/html` is replaced wholesale — there is no in-place file update that a
"keep last release's assets" rule could act on, and keeping old assets would mean a shared,
stateful volume that the current stateless image model deliberately avoids. The residual skew window
is bounded to clients holding a stale `index.html`, and the bootstrap above resolves it.

### 2. A backend that is up but not ready (the starting-up screen)

The backend runs migrations at startup, before it binds its listener. During that window nginx (in
the same container, already up) serves the frontend but proxies every `/api` and `/health` request
to a backend that is not listening yet — `502` — or that answers `503 not_ready` on
`GET /health/ready` (issue #421) while migrations are still pending. Without handling, the app
mounts anyway and fires a wall of failed requests.

`ServerStartingGate` (`frontend/src/components/ServerStartingGate.tsx`, mounted at the root outside
the routed ErrorBoundary) holds the tree back behind a clear "starting up" screen until
`/health/ready` stops reporting not-ready, polling every few seconds, then mounts the app once. It
fails open (`frontend/src/readiness/readiness.ts`): only an authoritative not-ready signal
(`502`/`503`/`504` from `/health/ready`) keeps the gate up; an ambiguous or unreachable answer
mounts the app exactly as if the gate did not exist, so a network error never locks a user out. Once
the gate has passed it stays passed for the life of the page; later in-session server hiccups are
the stale-client detector's and `SessionExpiredGate`'s job.

The WEB-03 scenarios — a stale index whose chunk the new deploy deleted, a deploy that never
completes, rollback convergence, and the not-ready window — are pinned by the same suite (its
harness can withhold any `/assets/` path and flip `/health/ready` between ready and `not_ready`).

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
