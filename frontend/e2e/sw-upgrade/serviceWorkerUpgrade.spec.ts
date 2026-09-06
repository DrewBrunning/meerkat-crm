// Issue #476 (WEB-02): service-worker upgrade tests.
//
// A service-worker upgrade is the hardest thing in a PWA to get right because
// the failure is persistent: a bad worker keeps serving its broken cache after
// the deploy that fixed it, and a refresh never reaches the network. These
// tests stage the upgrade itself -- serve build A, install its worker, then
// serve build B and drive the update -- against REAL production builds and the
// app's own registration code.
//
// Why this suite exists separately from e2e/serviceWorker.spec.ts:
//
// - The main suite runs against the all-in-one docker image on :7300, which
//   serves a single fixed build. An upgrade needs an origin that serves build A
//   and then build B from the same URL, so this suite runs against its own
//   harness (playwright.sw.config.ts -> e2e/sw-upgrade/) instead. See
//   sw-server.mjs for how the two builds are staged and swapped.
// - `frontend-dev` (`yarn start`) never compiles service-worker.ts into a real
//   /service-worker.js -- requests fall through to index.html and registration
//   fails on the wrong MIME type (CLAUDE.md, T51). Every build here is a real
//   `vite build`; the fixtures differ only in the entry chunk label
//   (vite.config.ts, MYCORRHIZAL_SW_FIXTURE), which is enough to make the
//   service-worker.js byte-different and the update real.
// - No backend is required: these tests assert on the app shell, the worker
//   lifecycle and CacheStorage, none of which needs the API. The app still
//   registers its worker normally on window load.
//
// Both browser engines are exercised (playwright.sw.config.ts projects), so a
// lifecycle bug that only Firefox hits (like the wrong-MIME registration
// failure) is caught here, not in production.

import { expect, test } from '@playwright/test';
import {
  activateWaitingWorker,
  buildLabelOf,
  cacheInventory,
  getHarnessStatus,
  getSwState,
  loadAppShell,
  loadedAssetPaths,
  reloadExpectingShell,
  setActiveProfile,
  triggerUpdate,
  waitForWaitingWorker,
  waitUntil,
} from './helpers';

test.describe('Service worker upgrade', () => {
  test.beforeEach(async ({ request }) => {
    // The harness's active build is shared server state; every test pins the
    // starting point it expects so a failed earlier test can't leak into the
    // next one.
    await setActiveProfile(request, 'a');
  });

  test('serves build A and precaches exactly its manifest (recovery page excluded)', async ({
    page,
    request,
  }) => {
    const state = await loadAppShell(page);
    expect(buildLabelOf(state.entry)).toBe('a');

    // An activated worker has finished installing, so the precache bucket must
    // already hold exactly the files of build A's injected manifest.
    const inventory = await cacheInventory(page);
    expect(inventory.precacheName).not.toBeNull();

    const status = await getHarnessStatus(request);
    const expected = [...status.builds.a.files].sort();
    expect(inventory.precacheEntries).toEqual(expected);

    // The escape-hatch page must never be precached (vite.config.ts
    // globIgnores): a broken worker could otherwise serve a stale copy of the
    // one page whose job is to recover from a broken worker.
    expect(inventory.precacheEntries).not.toContain('/_recovery.html');
    expect(inventory.precacheEntries).not.toContain('/_recovery.js');

    // No unbounded cache growth on a clean install: only the workbox precache
    // bucket plus (once the login page fetches a logo) the bounded runtime
    // images bucket exist.
    const unexpected = inventory.names.filter(
      (name) => name !== 'images' && !name.includes('workbox-precache'),
    );
    expect(unexpected, `unexpected cache buckets: ${inventory.names.join(', ')}`).toEqual([]);
  });

  test('a new deploy waits behind the open tab until it is applied', async ({ page, request }) => {
    await loadAppShell(page);
    await expect.poll(() => getSwState(page).then((s) => buildLabelOf(s.entry))).toBe('a');

    // Deploy build B while the tab is open on build A.
    await setActiveProfile(request, 'b');
    await triggerUpdate(page);

    // The new worker installs and then WAITS: build A still controls the open
    // tab, so B must not take over on its own (skipWaiting is only ever
    // triggered by the app, never automatically).
    await waitForWaitingWorker(page);
    const waiting = await getSwState(page);
    expect(waiting.activeState).toBe('activated');
    expect(buildLabelOf(waiting.entry)).toBe('a');

    // "The user sees nothing change": a reload is still served by build A, and
    // the waiting worker is still waiting afterwards -- an open tab is exactly
    // the state that keeps a deploy from activating on its own.
    await reloadExpectingShell(page, 'a');
    const afterReload = await getSwState(page);
    expect(afterReload.hasWaiting, 'the new worker stays waiting behind the reload').toBe(true);
  });

  test('the update prompt offers a reload that applies the waiting worker', async ({
    page,
    request,
  }) => {
    await loadAppShell(page);
    await setActiveProfile(request, 'b');
    await triggerUpdate(page);
    await waitForWaitingWorker(page);

    // The product surfaces the waiting worker as an update prompt (WEB-01's
    // UX); the old build keeps serving until the user acts on it.
    await expect(
      page.getByText('A new version of Mycorrhizal CRM is available.'),
      'an update prompt should appear once a new worker is waiting',
    ).toBeVisible();
    await expect.poll(() => getSwState(page).then((s) => buildLabelOf(s.entry))).toBe('a');

    // Clicking the prompt's reload is the documented way to hand control to B:
    // applyUpdate() SKIP_WAITINGs B, waits for controllerchange, then reloads.
    await page.getByRole('button', { name: 'Reload' }).click();

    await waitUntil(
      async () => {
        const s = await getSwState(page);
        return buildLabelOf(s.entry) === 'b' && !s.hasWaiting;
      },
      { message: 'the update prompt reload should switch the app to build B' },
    );
  });

  test('activation cleans the old build out of the precache (no unbounded growth)', async ({
    page,
    request,
  }) => {
    const aState = await loadAppShell(page);
    const aEntry = aState.entry;
    expect(aEntry).not.toBeNull();

    const before = await cacheInventory(page);
    expect(before.precacheName).not.toBeNull();

    await setActiveProfile(request, 'b');
    await triggerUpdate(page);
    await waitForWaitingWorker(page);
    await activateWaitingWorker(page);
    await reloadExpectingShell(page, 'b');

    // Same precache bucket, now holding exactly build B's manifest -- build A's
    // entry chunk (its only unique file) must have been deleted on activation.
    const after = await cacheInventory(page);
    expect(after.precacheName).toBe(before.precacheName);

    const status = await getHarnessStatus(request);
    const expectedB = [...status.builds.b.files].sort();
    expect(after.precacheEntries).toEqual(expectedB);
    expect(after.precacheEntries).not.toContain(aEntry);

    // No accumulation: the precache bucket is reused (its name must not drift
    // between worker versions), and no second precache bucket appeared. The
    // runtime `images` bucket may appear between the two snapshots -- the
    // reload that applied the update also let the app fetch a logo -- but that
    // bucket is size-bounded by its own ExpirationPlugin, not by this test.
    const precacheNames = (names: string[]) =>
      names.filter((name) => name.includes('workbox-precache'));
    expect(precacheNames(after.names)).toEqual(precacheNames(before.names));
    expect(after.names.filter((name) => name !== 'images')).toEqual(
      before.names.filter((name) => name !== 'images'),
    );
  });

  test('no mixed-version asset loading across an upgrade', async ({ page, request }) => {
    const status = await getHarnessStatus(request);
    const aFiles = new Set(status.builds.a.files);
    const bFiles = new Set(status.builds.b.files);

    const aState = await loadAppShell(page);
    const aEntry = aState.entry;
    expect(aEntry).not.toBeNull();
    // The two fixtures are now genuinely distinct builds (issue #475 stamps a
    // different release version into each), so build B's entry is looked up
    // from the harness's actual file set rather than derived by relabelling
    // build A's hash.
    const bEntry = [...bFiles].find((path) => path.includes('swfb-')) ?? null;
    expect(bEntry, 'the harness should serve a distinct build-B entry').not.toBeNull();
    const bEntryPath = bEntry!;

    // While B is installed-but-waiting, the still-live build-A document must
    // keep loading only build-A assets -- never a chunk that only B knows.
    await setActiveProfile(request, 'b');
    await triggerUpdate(page);
    await waitForWaitingWorker(page);
    let loaded = await loadedAssetPaths(page);
    expect(loaded).not.toContain(bEntryPath);
    for (const path of loaded) {
      expect(aFiles.has(path), `build-A shell loaded a foreign asset: ${path}`).toBe(true);
    }

    // After B takes over and the page reloads through it, only build-B assets
    // load -- build A's unique entry is gone and nothing from A leaks in.
    await activateWaitingWorker(page);
    await reloadExpectingShell(page, 'b');
    loaded = await loadedAssetPaths(page);
    expect(loaded).toContain(bEntryPath);
    expect(loaded).not.toContain(aEntry);
    for (const path of loaded) {
      expect(bFiles.has(path), `build-B shell loaded a foreign asset: ${path}`).toBe(true);
    }
  });

  test('a user offline through a deploy converges on the new build after reconnecting', async ({
    page,
    context,
    request,
  }) => {
    const aState = await loadAppShell(page);
    expect(buildLabelOf(aState.entry)).toBe('a');

    // The deploy happens while the user is offline.
    await context.setOffline(true);
    await setActiveProfile(request, 'b');

    // Offline reloads keep working (served by build A's worker from cache) --
    // the user is not stuck, just on the old build.
    await page.goto('/');
    await waitUntil(
      async () => {
        const s = await getSwState(page);
        return buildLabelOf(s.entry) === 'a';
      },
      { message: 'offline reloads should keep serving the cached build A shell' },
    );

    // An update check while offline fails without breaking anything.
    await page.evaluate(async () => {
      try {
        const registration = await navigator.serviceWorker.getRegistration();
        await registration?.update();
      } catch {
        // offline -- expected
      }
    });

    // Reconnecting lets the update through; the client converges on build B.
    await context.setOffline(false);
    await triggerUpdate(page);
    await waitForWaitingWorker(page);
    await activateWaitingWorker(page);
    await reloadExpectingShell(page, 'b');
  });

  test('repeated rapid updates keep the precache bounded', async ({ page, request }) => {
    const status = await getHarnessStatus(request);

    await loadAppShell(page);
    const baseline = await cacheInventory(page);
    expect(baseline.precacheName).not.toBeNull();

    const knownNames = new Set(baseline.names);
    let previousLabel: 'a' | 'b' = 'a';

    // A client that checks periodically sees a series of quick releases as a
    // rapid succession of updates. Alternate the fixtures a few times and
    // assert each transition cleans up after itself.
    const targets: ('a' | 'b')[] = ['b', 'a', 'b'];
    for (const target of targets) {
      await setActiveProfile(request, target);
      await triggerUpdate(page);
      await waitForWaitingWorker(page);
      await activateWaitingWorker(page);
      await reloadExpectingShell(page, target);

      const inventory = await cacheInventory(page);
      const expected = [...status.builds[target].files].sort();

      // Precise: after activation the precache holds exactly the new build's
      // manifest -- the previous build's unique entry chunk is gone, so the
      // cache can never accumulate one dead version after another.
      expect(inventory.precacheEntries).toEqual(expected);

      // Bounded: no new cache buckets appeared, and the precache bucket name
      // did not drift between workers.
      for (const name of inventory.names) {
        expect(knownNames.has(name) || name === 'images', `unexpected bucket: ${name}`).toBe(true);
        knownNames.add(name);
      }
      expect(inventory.precacheName).toBe(baseline.precacheName);

      const previousUnique = status.builds[previousLabel].files.filter(
        (file) => !status.builds[target].files.includes(file),
      );
      for (const file of previousUnique) {
        expect(
          inventory.precacheEntries,
          `a dead file from the previous build survived the upgrade: ${file}`,
        ).not.toContain(file);
      }

      previousLabel = target;
    }

    // After all the churn, still no more than the precache + images buckets.
    const finalInventory = await cacheInventory(page);
    const unexpected = finalInventory.names.filter(
      (name) => name !== 'images' && !name.includes('workbox-precache'),
    );
    expect(unexpected).toEqual([]);
  });
});
