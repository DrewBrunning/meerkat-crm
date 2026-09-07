// Issue #477 (WEB-03): the asset-skew window — a stale index.html referencing
// hashed chunks the current deploy no longer serves.
//
// A deployment is not atomic. A client that loads an index.html from the
// previous build (an in-flight response, an HTTP cache that straddled the
// swap, a browser whose service worker is gone) asks for entry/vendor chunks
// whose names only that build knew — and the new image deleted them. Each of
// those requests 404s, the ES module graph fails to evaluate, and the page
// would be a blank white screen whose own bundle (ErrorBoundary, service
// worker, stale-client detector) never even ran.
//
// The production copy of this suite's counterpart runs in
// frontend/public/asset-skew.js, referenced from <head> in index.html OUTSIDE
// the bundle so it survives a broken module load. It notices the failed asset,
// waits a moment for an in-flight deploy to finish, reloads onto whatever the
// server serves now, and stops (showing a clear message with a retry) after a
// bounded number of consecutive broken loads.
//
// These specs stage it with the two-build harness:
//
//  1. the harness serves build A's index.html (the stale document) while a
//     chunk only A knows has been removed (blocked -> 404),
//  2. a client loads it (a fresh context: no service worker and no HTTP cache
//     to mask the 404 — this is exactly the client the skew window catches),
//  3. the deploy "completes" (the harness flips to build B and unblocks), and
//     the asset-skew bootstrap reloads the client onto B.
//
// The service worker is blocked in this suite (context option) on purpose:
// the whole scenario is a *network*-served load, and a worker (or its
// precache) would serve the missing chunk from cache and hide the 404 that
// makes the skew a skew.

import { expect, test } from '@playwright/test';
import {
  blockAsset,
  buildLabelOf,
  getHarnessStatus,
  getSwState,
  resetBlockedAssets,
  resetHarness,
  setActiveProfile,
  waitUntil,
} from './helpers';

test.describe('Interrupted deployment — asset skew (WEB-03)', () => {
  // Every spec in this file drives network loads that must reach the harness;
  // an installed worker or warm HTTP cache would serve the "removed" chunk and
  // the 404 that defines the scenario would never happen.
  test.use({ serviceWorkers: 'block' });

  test.beforeEach(async ({ request }) => {
    await resetHarness(request);
  });

  function entryOf(status: Awaited<ReturnType<typeof getHarnessStatus>>, label: 'a' | 'b'): string {
    const entry = status.builds[label].files.find((file) => file.includes(`swf${label}-`));
    if (!entry) throw new Error(`build ${label} should have a distinct entry chunk`);
    return entry;
  }

  test('a stale index whose chunk the new build deleted reloads onto the current build', async ({
    page,
    request,
  }) => {
    const status = await getHarnessStatus(request);
    const aEntry = entryOf(status, 'a');
    // The deploy deleted build A's unique entry chunk: any client still holding
    // A's index.html now asks for a file the server no longer serves.
    await blockAsset(request, aEntry);

    // Seed a draft (issue #557's sessionStorage persistence) so the recovery
    // can be asserted to not lose what a user had in progress.
    await page.route('**/seed', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<!doctype html><html><body>seed</body></html>',
      }),
    );
    await page.goto('/seed');
    await page.evaluate(() =>
      sessionStorage.setItem(
        'mycorrhizal:draft:note-dialog:unsent',
        '{"content":"note in progress"}',
      ),
    );
    await page.unroute('**/seed');

    // The client requests a chunk from A that the server no longer has.
    const brokenRequests: string[] = [];
    page.on('response', (response) => {
      if (response.status() === 404 && response.url().includes('/assets/')) {
        brokenRequests.push(response.url());
      }
    });
    await page.goto('/');
    await waitUntil(() => brokenRequests.length > 0, {
      message: 'the load of build A should hit the removed chunk',
    });

    // The deploy completes: the server now serves build B, whose chunks all
    // exist. The asset-skew bootstrap notices the broken boot and reloads.
    await setActiveProfile(request, 'b');
    await resetBlockedAssets(request);

    // The client converges on the current build — resolved by a reload, not
    // left as an opaque unhandled chunk-load error.
    await waitUntil(async () => buildLabelOf((await getSwState(page)).entry) === 'b', {
      message: 'the skew recovery should reload the client onto build B',
    });

    // No "interrupted deployment" fallback was needed, and no opaque error
    // screen was left behind — the app actually mounted.
    await expect(page.getByText(/could not be loaded/i)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Login' })).toBeVisible({
      timeout: 15_000,
    });

    // No scenario loses in-progress user input (issue #477's criterion #6,
    // mechanism from #557): the draft that was in sessionStorage when the
    // broken load happened is still there after the recovery reload.
    const draft = await page.evaluate(() =>
      sessionStorage.getItem('mycorrhizal:draft:note-dialog:unsent'),
    );
    expect(draft, 'a pre-existing draft must survive the skew recovery').toContain(
      'note in progress',
    );
  });

  test('a deploy that never completes shows a clear message instead of reloading forever', async ({
    page,
    request,
  }) => {
    const status = await getHarnessStatus(request);
    const aEntry = entryOf(status, 'a');
    await blockAsset(request, aEntry);

    // Count document loads: the bootstrap is allowed a bounded number of
    // automatic retries, then must stop and put a manual way forward in front
    // of the user — never an infinite reload loop and never a blank page.
    let documentLoads = 0;
    page.on('load', () => {
      documentLoads += 1;
    });

    await page.goto('/');

    await expect(page.getByText(/could not be loaded/i)).toBeVisible({ timeout: 20_000 });
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();

    // Initial load + the bounded automatic retries, and no more: the loop is
    // broken even though the server is still broken.
    expect(documentLoads).toBeLessThanOrEqual(3);
    await page.waitForTimeout(1_500);
    expect(
      documentLoads,
      'a broken deploy must not spin an unbounded reload loop',
    ).toBeLessThanOrEqual(3);

    // The app never mounted (so there is nothing to hit an ErrorBoundary dead
    // end either) — the user got a clear message and a way forward instead.
    await expect(page.getByRole('button', { name: 'Login' })).toHaveCount(0);
  });
});
