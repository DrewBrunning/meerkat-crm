// Issue #476 (WEB-02): the escape hatch.
//
// A service-worker failure is the one client bug that survives the remedy: a
// bad worker keeps serving its broken cache after the deploy that fixed it,
// and a plain refresh never reaches the network -- the user is stuck until
// something reaches past the worker. The product answer is a static recovery
// page (/_recovery.html -> public/_recovery.html + /_recovery.js) that clears
// every CacheStorage bucket and unregisters every worker, then reloads from
// the network. See the nginx locations and the vite.config.ts globIgnores for
// how it is served and kept out of the precache.
//
// This spec stages the failure: it installs a deliberately broken worker
// ("poison", served by the harness in place of service-worker.js), confirms
// the user is stuck behind it, then drives the documented escape and confirms
// the client converges on the fixed build.

import { expect, test } from '@playwright/test';
import {
  activateWaitingWorker,
  buildLabelOf,
  getSwState,
  loadAppShell,
  setActiveProfile,
  triggerUpdate,
  waitForWaitingWorker,
  waitUntil,
} from './helpers';

test.describe('Service worker escape hatch', () => {
  test.beforeEach(async ({ request }) => {
    await setActiveProfile(request, 'a');
  });

  test('recovers a user stuck on a broken worker via /_recovery.html', async ({
    page,
    request,
  }) => {
    await loadAppShell(page);

    // Deploy the broken worker over the healthy one (this is what a bad
    // release looks like to a client that checks for updates).
    await setActiveProfile(request, 'poison');
    await triggerUpdate(page);
    await waitForWaitingWorker(page);
    await activateWaitingWorker(page);

    // Now the user is stuck: every reload is answered with the broken worker's
    // canned document, never the app or the server.
    await page.goto('/');
    await expect(page.locator('#sw-broken')).toHaveText('sw-broken-deploy', {
      timeout: 15_000,
    });
    await page.goto('/');
    await expect(page.locator('#sw-broken')).toHaveText('sw-broken-deploy', {
      timeout: 15_000,
    });

    // The operator fixes the deploy (serves the good build B again).
    await setActiveProfile(request, 'b');

    // The documented escape hatch: /_recovery.html is reachable from behind
    // the broken worker (the worker and the product SW both leave /_* alone),
    // is served by the network, and runs the clear-caches + unregister +
    // reload dance on load.
    await page.goto('/_recovery.html');
    await expect(page.getByRole('heading', { name: /Recovering Mycorrhizal CRM/i })).toBeVisible();

    // The recovery page redirects back to the app; with the worker and caches
    // gone the reload comes from the network and converges on the fixed build
    // B -- including re-registering a fresh (healthy) worker.
    await waitUntil(
      async () => {
        const s = await getSwState(page);
        return (
          s.entry !== null && buildLabelOf(s.entry) === 'b' && s.hasRegistration && s.hasActive
        );
      },
      { message: 'after the escape hatch the app should converge on build B' },
    );

    await expect(page.locator('#sw-broken')).toHaveCount(0);
    await expect
      .poll(async () => buildLabelOf((await getSwState(page)).entry), {
        message: 'the recovered app should stay on build B',
      })
      .toBe('b');
  });
});
