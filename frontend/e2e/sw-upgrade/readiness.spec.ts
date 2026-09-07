// Issue #477 (WEB-03): a client loading while the backend is up-but-not-ready.
//
// This app's server runs migrations before it binds its listener. During a
// deploy there is a window where the frontend (served by nginx) loads but the
// backend behind it cannot serve yet: /health/ready answers 503 while
// migrations run, and nginx answers 502/504 while the backend is not listening
// at all. Without handling, the app mounts anyway and fires a wall of failed
// requests with no explanation.
//
// <ServerStartingGate> (src/components/ServerStartingGate.tsx) holds the tree
// back behind a clear "starting up" state until /health/ready stops reporting
// not-ready, then mounts the app once. The harness stages the window by
// flipping its /health/ready between ready and not_ready (sw-server.mjs), which
// mirrors the backend's own readiness contract (issue #421).
//
// The fail-open half (an ambiguous/unreachable readiness answer mounts the app
// as before) is unit-tested in src/readiness/readiness.test.ts — it needs a
// response the harness cannot produce without breaking the shared origin.

import { expect, test } from '@playwright/test';
import { resetHarness, setReady } from './helpers';

test.describe('Interrupted deployment — backend not ready (WEB-03)', () => {
  test.beforeEach(async ({ request }) => {
    await resetHarness(request);
  });

  test('a client loading mid-migration sees a starting-up state, then the app once ready', async ({
    page,
    request,
  }) => {
    // The server is up but not ready: the frontend is being served (nginx is
    // up) while the backend is still migrating behind it.
    await setReady(request, false);

    const apiRequests: string[] = [];
    page.on('request', (req) => {
      if (req.url().includes('/api/')) {
        apiRequests.push(req.url());
      }
    });

    await page.goto('/');

    // A clear "starting up" state — not a wall of failed requests and not a
    // half-mounted app.
    await expect(page.getByTestId('server-starting-message')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Login' })).toHaveCount(0);
    expect(apiRequests, 'no app API request may fire while the server is starting').toEqual([]);

    // The migration finishes; the readiness poll releases the app.
    await setReady(request, true);
    await expect(page.getByRole('button', { name: 'Login' })).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId('server-starting-message')).toHaveCount(0);
  });
});
