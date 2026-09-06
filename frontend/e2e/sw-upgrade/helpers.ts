// Issue #476 (WEB-02): shared helpers for the service-worker upgrade suite.
//
// These tests drive a *real* service worker through a real upgrade between two
// real production builds (see fixtures.mjs / sw-server.mjs). The app's own
// registration code runs unchanged -- tests never register a second worker --
// so the lifecycle under test is exactly what ships, not a test double.
//
// Reading SW state out of the page is done by returning primitives only: a
// ServiceWorkerRegistration is not structured-cloneable across the Playwright
// bridge, so every helper reduces the registration to a plain snapshot inside
// the page.

import { type APIRequestContext, expect, type Page } from '@playwright/test';

export type Profile = 'a' | 'b' | 'poison';

export interface SwState {
  hasRegistration: boolean;
  hasInstalling: boolean;
  hasWaiting: boolean;
  hasActive: boolean;
  /** 'installing' | 'installed' | 'activating' | 'activated' | 'redundant' */
  activeState: string | null;
  controllerState: string | null;
  controlled: boolean;
  /** src of the module entry script the loaded document references. */
  entry: string | null;
}

export interface HarnessStatus {
  active: Profile;
  builds: Record<'a' | 'b', { dir: string; files: string[] }>;
}

export function buildLabelOf(entry: string | null): 'a' | 'b' | null {
  if (!entry) return null;
  const match = entry.match(/\/assets\/index-swf([ab])-/);
  return match ? (match[1] as 'a' | 'b') : null;
}

export async function getSwState(page: Page): Promise<SwState> {
  try {
    return await page.evaluate(async () => {
      let registration: ServiceWorkerRegistration | undefined;
      try {
        registration = await navigator.serviceWorker.getRegistration();
      } catch {
        registration = undefined;
      }
      const entryEl = document.querySelector('script[type="module"]');
      const controller = navigator.serviceWorker.controller;
      return {
        hasRegistration: registration != null,
        hasInstalling: registration?.installing != null,
        hasWaiting: registration?.waiting != null,
        hasActive: registration?.active != null,
        activeState: registration?.active?.state ?? null,
        controllerState: controller?.state ?? null,
        controlled: controller !== null,
        entry: entryEl ? entryEl.getAttribute('src') : null,
      };
    });
  } catch (err) {
    // A snapshot taken mid-navigation lands on a destroyed execution context
    // (e.g. the reload triggered by the update prompt). That is not a SW state
    // failure -- report an empty registration and let the caller poll again.
    if (
      err instanceof Error &&
      (err.message.includes('Execution context was destroyed') ||
        err.message.includes('context or browser has been closed') ||
        err.message.includes('Cannot find context'))
    ) {
      return {
        hasRegistration: false,
        hasInstalling: false,
        hasWaiting: false,
        hasActive: false,
        activeState: null,
        controllerState: null,
        controlled: false,
        entry: null,
      };
    }
    throw err;
  }
}

/** Polls a predicate until it is true (or the timeout elapses). */
export async function waitUntil(
  fn: () => Promise<boolean> | boolean,
  opts: { message?: string; timeout?: number } = {},
): Promise<void> {
  await expect
    .poll(fn, {
      timeout: opts.timeout ?? 60_000,
      intervals: [250, 500, 1000],
      message: opts.message,
    })
    .toBe(true);
}

export async function setActiveProfile(
  request: APIRequestContext,
  profile: Profile,
): Promise<void> {
  const response = await request.post('/__swtest/active', { data: { build: profile } });
  expect(response.ok(), `harness should accept profile ${profile}`).toBeTruthy();
}

export async function getHarnessStatus(request: APIRequestContext): Promise<HarnessStatus> {
  const response = await request.get('/__swtest/status');
  expect(response.ok(), 'harness status should be reachable').toBeTruthy();
  return (await response.json()) as HarnessStatus;
}

/**
 * Loads the app shell at / and waits until the app's own registration has
 * installed, activated and taken control (clientsClaim). Returns the state.
 */
export async function loadAppShell(page: Page): Promise<SwState> {
  await page.goto('/');
  await waitUntil(
    async () => {
      const s = await getSwState(page);
      return s.hasRegistration && s.hasActive && s.controlled && s.controllerState === 'activated';
    },
    { message: 'the app should register an active, controlling service worker' },
  );
  return getSwState(page);
}

/** Asks the registered service worker to check for an update. */
export async function triggerUpdate(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const registration = await navigator.serviceWorker.getRegistration();
    if (!registration) throw new Error('no service worker registration');
    await registration.update();
  });
}

/**
 * Waits until a freshly fetched worker has finished installing and is sitting
 * in the waiting state (an old worker still controls the open tab).
 */
export async function waitForWaitingWorker(page: Page): Promise<void> {
  await waitUntil(async () => (await getSwState(page)).hasWaiting, {
    message: 'the new worker should finish installing and wait for the open tab',
  });
}

/**
 * Hands control to the waiting worker the way the app's applyUpdate() does --
 * SKIP_WAITING, then wait for it to activate and claim the page.
 */
export async function activateWaitingWorker(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        navigator.serviceWorker
          .getRegistration()
          .then((registration) => {
            const waiting = registration?.waiting;
            if (!waiting) {
              resolve();
              return;
            }
            let done = false;
            const finish = () => {
              if (done) return;
              done = true;
              window.clearTimeout(timer);
              resolve();
            };
            // Fallback so a stuck worker fails the test with a timeout rather
            // than hanging the suite forever.
            const timer = window.setTimeout(finish, 45_000);
            navigator.serviceWorker.addEventListener('controllerchange', finish);
            waiting.postMessage({ type: 'SKIP_WAITING' });
          })
          .catch(() => resolve());
      }),
  );
  await waitUntil(
    async () => {
      const s = await getSwState(page);
      return s.controlled && s.controllerState === 'activated' && !s.hasWaiting;
    },
    { message: 'the waiting worker should activate and take control' },
  );
}

/**
 * Reloads the page (through whatever service worker is controlling it) and
 * waits until the loaded document's module entry belongs to `label`.
 */
export async function reloadExpectingShell(page: Page, label: 'a' | 'b'): Promise<void> {
  await page.goto('/');
  await waitUntil(async () => buildLabelOf((await getSwState(page)).entry) === label, {
    message: `the reloaded document should be served from build ${label}`,
  });
}

/**
 * Reads the CacheStorage buckets: cache names and, for the workbox precache
 * cache, the URL pathname of every entry.
 */
export async function cacheInventory(page: Page): Promise<{
  names: string[];
  precacheName: string | null;
  precacheEntries: string[];
}> {
  return page.evaluate(async () => {
    const names = (await caches.keys()).sort();
    const precacheName = names.find((name) => name.includes('workbox-precache')) ?? null;
    const precacheEntries: string[] = [];
    if (precacheName) {
      const cache = await caches.open(precacheName);
      const requests = await cache.keys();
      for (const request of requests) {
        precacheEntries.push(new URL(request.url).pathname);
      }
      precacheEntries.sort();
    }
    return { names, precacheName, precacheEntries };
  });
}

/** Every /assets/* pathname the current document actually requested. */
export async function loadedAssetPaths(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const paths = new Set<string>();
    for (const entry of performance.getEntriesByType('resource')) {
      const url = new URL(entry.name);
      if (url.pathname.startsWith('/assets/')) {
        paths.add(url.pathname);
      }
    }
    // Module scripts referenced by the (possibly SW-served) document may not
    // all be in performance entries on every engine; include the entry script
    // the document references.
    for (const el of document.querySelectorAll('script[type="module"],link[rel="modulepreload"]')) {
      const src = el.getAttribute('src') || el.getAttribute('href');
      if (src && src.startsWith('/assets/')) {
        paths.add(src);
      }
    }
    return [...paths].sort();
  });
}
