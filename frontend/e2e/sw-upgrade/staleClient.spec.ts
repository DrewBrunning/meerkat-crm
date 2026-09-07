// Issue #475 (WEB-01): the stale-client *backstop* — an open tab that has been
// left running across a deploy must not be stranded on an old, now-
// incompatible bundle.
//
// These tests stage the scenario with the same two-build harness as
// serviceWorkerUpgrade.spec.ts (fixtures.mjs / sw-server.mjs): fixture "a"
// embeds release 0.6.8, fixture "b" embeds 0.6.10 (FIXTURE_VERSIONS), and the
// harness's /health advertises the active build's own identity by default —
// which is exactly what a running build expects to see, so the two suites can
// share a harness. A WEB-01 test then:
//
//  1. loads build A (the tab that was open when the deploy happened),
//  2. "deploys" by flipping the harness to build B and overriding /health to
//     advertise a contract build A cannot satisfy (a floor above it, or a
//     different api_contract_version),
//  3. kicks the app's own poll (the detector checks on window focus), and
//  4. asserts the stale tab is reloaded onto build B — or, for the fail-open
//     spec, that an unreachable /health changes nothing.
//
// The real /service-worker.js is served throughout, so the forced reload goes
// through a genuine service-worker swap, not a test double. No backend is
// involved (the app's API calls 404/fall back to the shell, which the detector
// already fails open against).

import { expect, type Page, test } from '@playwright/test';
import {
  buildLabelOf,
  getSwState,
  loadAppShell,
  resetHealthOverride,
  setActiveProfile,
  setHealthOverride,
  waitForWaitingWorker,
  waitUntil,
} from './helpers';

// The detector (staleClient/detector.ts) checks on window focus — the
// mid-session case the ticket is about. Firing a synthetic focus event is how
// a spec asks a specific open tab to run its poll deterministically instead of
// waiting up to a minute for the interval.
async function triggerStaleClientCheck(page: Page): Promise<void> {
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
}

async function expectOnBuild(page: Page, label: 'a' | 'b'): Promise<void> {
  await waitUntil(async () => buildLabelOf((await getSwState(page)).entry) === label, {
    message: `the tab should be running build ${label}`,
  });
}

test.describe('Stale-client behavior (WEB-01)', () => {
  test.beforeEach(async ({ request }) => {
    await setActiveProfile(request, 'a');
    await resetHealthOverride(request);
  });

  test('a mid-session deploy that raises the floor forces the stale tab onto the new build', async ({
    page,
    request,
  }) => {
    // Build A (0.6.8) is the tab that was open across the deploy.
    await loadAppShell(page);
    await expectOnBuild(page, 'a');

    // The deploy: the server now serves build B (0.6.10) and has raised its
    // min_client_version above what the open tab runs.
    await setActiveProfile(request, 'b');
    await setHealthOverride(request, { min_client_version: '0.6.10' });

    // A long-lived tab notices on its next poll.
    await triggerStaleClientCheck(page);

    // The tab must reload onto build B on its own — no endless erroring, no
    // user action required (the tab is clean), and no reload loop.
    await expectOnBuild(page, 'b');
    const state = await getSwState(page);
    expect(state.hasWaiting, 'no worker should be left waiting after convergence').toBe(false);
    await expect(
      page.getByText('Incompatible version'),
      'a clean forced reload must not leave a blocking dialog behind',
    ).toHaveCount(0);

    // And build B (0.6.10) is above the floor it advertises, so the mechanism
    // is quiescent again rather than reloading forever.
    await page.waitForTimeout(500);
    expect(buildLabelOf((await getSwState(page)).entry)).toBe('b');
  });

  test('a contract-generation mismatch forces a reload — and never loops silently', async ({
    page,
    request,
  }) => {
    await loadAppShell(page);
    await expectOnBuild(page, 'a');

    // The server announces a different api_contract_version than this bundle
    // speaks — the "v2" a future contract generation would announce. Unlike
    // the floor case, build B is ALSO contract v1 (both fixtures ship the same
    // client code), so reloading does not immediately converge: this is the
    // interim state where the server keeps announcing v2 before it serves a v2
    // bundle.
    await setActiveProfile(request, 'b');
    await setHealthOverride(request, { api_contract_version: 'v2' });

    await triggerStaleClientCheck(page);

    // The stale tab is reloaded onto build B automatically (one attempt)...
    await expectOnBuild(page, 'b');

    // ...but B is still blocked by the same v2 announcement. The loop guard
    // (once per cooldown window) must turn the SECOND block into a manual
    // dialog, not another silent reload — no infinite reload loop.
    await expect(
      page.getByText('Incompatible version'),
      'a non-converging block must surface as a manual dialog, not a reload loop',
    ).toBeVisible();

    // The server converges (now serves a compatible contract): the mechanism
    // clears itself and stays quiet on build B.
    await resetHealthOverride(request);
    await triggerStaleClientCheck(page);
    await expect(page.getByText('Incompatible version')).toHaveCount(0);
    expect(buildLabelOf((await getSwState(page)).entry)).toBe('b');
  });

  test('an unreachable /health fails open: the stale tab is never forced', async ({
    page,
    request,
  }) => {
    await loadAppShell(page);
    await expectOnBuild(page, 'a');

    // Deploy happened, but /health is down (server restarting) when the tab
    // next polls. The app must keep working rather than bricking itself.
    await setActiveProfile(request, 'b');
    await setHealthOverride(request, { error: true });
    await triggerStaleClientCheck(page);

    await page.waitForTimeout(1000);
    await expectOnBuild(page, 'a');
    await expect(page.getByText('Incompatible version')).toHaveCount(0);

    // /health recovers; the same tab now sees the newer build and the normal,
    // NON-blocking service-worker update path takes over — a worker waits
    // behind the open tab and the prompt offers the reload, but nothing
    // reloads the tab by itself.
    await resetHealthOverride(request);
    await triggerStaleClientCheck(page);
    await waitForWaitingWorker(page);
    await expectOnBuild(page, 'a');
    await expect(page.getByText('A new version of Mycorrhizal CRM is available.')).toBeVisible();
  });
});
