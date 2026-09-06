import { expect, test } from './fixtures';

// The service worker must stay registered across page loads.
//
// This is a regression guard for a bug that shipped in v0.2.0 and was found by
// review before v0.3.0: index.tsx still called CRA's scaffolded
// serviceWorkerRegistration.unregister(), which runs on every page load and
// tears down whatever registration it finds. Since a PushSubscription is owned
// by the service worker registration, N9's browser-push channel could never
// survive a reload -- a user enabled it, the subscription was destroyed on the
// next load, and the server's stored row went stale and was pruned on the
// following 404/410. Silently.
//
// Nothing in the unit tests or the rest of the e2e suite could see this:
// notifications.spec.ts exercises push-subscription CRUD through the API,
// which never involves a browser registration at all. It only shows up across
// a real page load, which is exactly what this spec does.
//
// Note this runs against the production build served by the all-in-one image
// (nginx on 7300) -- register() is deliberately a no-op in development builds,
// so a dev-server run would prove nothing.
test.describe('Service worker', () => {
  test('registers and survives a page reload, so a push subscription can outlive it', async ({
    page,
  }) => {
    await page.goto('/');

    // Registration happens on window's load event, so poll rather than assume
    // it has already happened by the time the navigation promise resolves.
    await expect
      .poll(
        () => page.evaluate(async () => (await navigator.serviceWorker.getRegistrations()).length),
        { message: 'the app should register its service worker on load', timeout: 15000 },
      )
      .toBeGreaterThan(0);

    const scope = await page.evaluate(async () => {
      const registrations = await navigator.serviceWorker.getRegistrations();
      return registrations[0].scope;
    });

    await page.reload();

    // The bug: after this reload the count was 0. Give the old unregister()
    // path time to have run before asserting, so a pass cannot be a race.
    await page.waitForTimeout(1500);

    const after = await page.evaluate(async () => {
      const registrations = await navigator.serviceWorker.getRegistrations();
      return { count: registrations.length, scopes: registrations.map((r) => r.scope) };
    });

    expect(after.count, 'the registration must survive a reload').toBeGreaterThan(0);
    expect(after.scopes).toContain(scope);
  });

  test('serves the service worker script with a revalidating cache header', async ({ page }) => {
    // /service-worker.js would otherwise be caught by nginx's 1-year immutable
    // rule for *.js, which is how a deployment gets permanently stuck on an old
    // worker: the file that decides whether any future update is seen would
    // itself be the one cached hardest.
    const response = await page.request.get('/service-worker.js');

    expect(response.status()).toBe(200);
    expect(response.headers()['cache-control']).toContain('no-cache');
  });

  test('serves the /_recovery.* escape hatch fresh from the network', async ({ page }) => {
    // Issue #476 (WEB-02): /_recovery.html is the way out for a user stranded
    // on a broken service worker. It must always come from the server (nginx
    // serves it no-store) so a stale copy can never shadow it, and it must be
    // loadable even while a broken worker is in control -- the worker is
    // written to leave /_* navigations to the network. This spec pins the
    // nginx side of that contract; the sw-upgrade suite
    // (playwright.sw.config.ts) pins the full broken-worker -> recovery flow.
    const recovery = await page.request.get('/_recovery.html');
    expect(recovery.status()).toBe(200);
    expect(recovery.headers()['content-type']).toContain('text/html');
    expect(recovery.headers()['cache-control']).toContain('no-store');
    const html = await recovery.text();
    expect(html).toContain('Recovering Mycorrhizal CRM');
    expect(html).toContain('/_recovery.js');

    const script = await page.request.get('/_recovery.js');
    expect(script.status()).toBe(200);
    expect(script.headers()['content-type']).toContain('javascript');
    expect(script.headers()['cache-control']).toContain('no-store');
    const body = await script.text();
    expect(body).toContain('getRegistrations');
    expect(body).toContain('caches');
  });
});
