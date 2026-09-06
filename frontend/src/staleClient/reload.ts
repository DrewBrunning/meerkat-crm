// The forced reload (issue #475, WEB-01): how a tab whose contract has gone
// stale actually gets onto the build the server now serves.
//
// A plain window.location.reload() is NOT enough when a service worker is in
// control: the reload is served from the worker's cache, which is the old,
// now-incompatible build — the very thing we are trying to leave. The reliable
// sequence is the same one the update prompt's applyUpdate uses:
//
//   1. Get the current registration. If a new worker is already installed and
//      waiting (the deploy's new build — workbox re-precaches on a new
//      service-worker.js, so any real deploy produces one), hand it control
//      via SKIP_WAITING and reload once controllerchange fires.
//   2. If nothing is waiting yet, ask the browser registration.update() to go
//      fetch the current service-worker.js — the idle-tab case where the
//      browser's own ~24h check has not run — then do step 1 again.
//   3. No registration at all (a non-PWA load) falls back to a plain reload.
//
// The loop guard is what makes this safe to drive automatically: before any
// reload we stamp sessionStorage, and the detector refuses to auto-reload
// again inside the cooldown window, so a server that keeps advertising an
// unsatisfiable floor cannot spin the tab. After one failed attempt the user
// gets the manual "Reload now" dialog instead of an endless silent loop.
import { applyUpdate } from '../serviceWorkerUpdates';

export const RELOAD_COOLDOWN_MS = 60_000;
// If a waiting worker never takes over (a broken worker — WEB-02 documents
// the manual /_recovery.html escape for the truly stuck case), don't hang the
// user: fall back to a plain reload, which at least re-enters the SW update
// path the next time the detector polls.
const RELOAD_SW_FALLBACK_MS = 10_000;

const RELOAD_GUARD_KEY = 'mycorrhizal:staleReload:at';

// sessionStorage can throw in privacy modes; the guard is best-effort only.
let inMemoryLastReload = 0;

function readLastReload(): number {
  try {
    const raw = window.sessionStorage.getItem(RELOAD_GUARD_KEY);
    return raw ? Number(raw) : 0;
  } catch {
    return inMemoryLastReload;
  }
}

function writeLastReload(): void {
  inMemoryLastReload = Date.now();
  try {
    window.sessionStorage.setItem(RELOAD_GUARD_KEY, String(inMemoryLastReload));
  } catch {
    // in-memory fallback above already recorded the time
  }
}

/** Marks that a forced reload was just initiated. */
export function markForcedReload(): void {
  writeLastReload();
}

/** True when a forced reload happened within the cooldown window. */
export function hasRecentlyForcedReload(): boolean {
  const last = readLastReload();
  return last > 0 && Date.now() - last < RELOAD_COOLDOWN_MS;
}

/**
 * Reloads the tab onto whatever build the server currently serves, routing
 * through the service worker so the old cached shell is not re-served.
 * Resolves once the reload has been *initiated* (the navigation itself ends
 * the page). Marks the loop guard first.
 */
export async function forceReloadToCurrentBuild(): Promise<void> {
  markForcedReload();

  if (!('serviceWorker' in navigator)) {
    window.location.reload();
    return;
  }

  let registration: ServiceWorkerRegistration | undefined;
  try {
    registration = await navigator.serviceWorker.getRegistration();
  } catch {
    window.location.reload();
    return;
  }

  if (!registration) {
    window.location.reload();
    return;
  }

  // No worker installing or waiting yet — ask the browser to go check the
  // server for a new one. A real deploy always changes service-worker.js (the
  // workbox precache manifest is inlined in it), so this finds the new build.
  if (!registration.waiting && !registration.installing) {
    try {
      await registration.update();
    } catch {
      // Offline / unreachable: reloading would just re-serve the stale cache.
      // Fail open — the caller keeps the block visible and retries later.
      return;
    }
  }

  // update() resolves once the fresh worker has *finished installing*, so it
  // normally sits in `waiting` by now. But if a worker is still installing
  // (a concurrent update check started one, or the browser resolved update()
  // early), swapping now is impossible — the new worker has not finished
  // precaching. Wait for it to land in `waiting` (or give up after the
  // fallback window) before taking over.
  if (!registration.waiting && registration.installing) {
    await waitForWorkerWaiting(registration);
  }

  if (registration.waiting) {
    // applyUpdate posts SKIP_WAITING and reloads on the first
    // controllerchange. The fallback timer is the only addition here: if the
    // swap stalls (a broken worker), stop waiting and reload anyway rather
    // than leaving the user on an infinite spinner.
    applyUpdate(registration);
    window.setTimeout(() => window.location.reload(), RELOAD_SW_FALLBACK_MS);
    return;
  }

  // update() found no new worker (e.g. a server-only hotfix left the bundle
  // untouched) — the bundle is fine; reload is enough.
  window.location.reload();
}

/**
 * Waits for a worker that is still installing to reach the waiting state (the
 * point at which SKIP_WAITING can hand it control). Resolves early if the
 * install fails (installing becomes null) and on a timeout so a wedged worker
 * can never hang the forced reload.
 */
function waitForWorkerWaiting(registration: ServiceWorkerRegistration): Promise<void> {
  return new Promise((resolve) => {
    const installing = registration.installing;
    if (!installing || registration.waiting) {
      resolve();
      return;
    }
    const onChange = () => {
      if (registration.waiting || !registration.installing) {
        window.clearTimeout(timer);
        installing.removeEventListener('statechange', onChange);
        resolve();
      }
    };
    const timer = window.setTimeout(onChange, RELOAD_SW_FALLBACK_MS);
    installing.addEventListener('statechange', onChange);
  });
}
