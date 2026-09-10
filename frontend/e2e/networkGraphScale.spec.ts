import { expect, SKIP_A11Y_SCAN, test } from './fixtures';
import { buildScaleGraph, GRAPH_SCALE_PROFILES } from './graphScaleProfiles';

/**
 * Network-graph render cliff (issue #556, recommended action 2).
 *
 * "Benchmark the graph against the #468 scale profiles, and find the cliff
 * deliberately... The deliverable is a number, not a pass." This drives the
 * synthetic #468 profile graphs through the real NetworkGraph and records, per
 * size, whether the canvas mounts or the #556 fallback kicks in and -- while
 * the canvas is up -- the worst requestAnimationFrame gap over a settle window
 * (a blocked main thread inflates it). The table goes to the run log and to
 * docs/development/web-perf-budgets.md.
 *
 * Deterministic assertion: the #556 ceiling behaviour holds -- a graph over
 * GRAPH_RENDER_NODE/EDGE_CEILING degrades to the notice, one under it mounts
 * the canvas. Wall-clock jank is a trend (noisy runner), not asserted.
 *
 * @perf-heavy: nightly e2e schedule only (RUN_PERF_HEAVY=1), per #447's
 * two-tier split.
 */

// Keep in sync with src/utils/graphBudget.ts.
const NODE_CEILING = 2000;
const EDGE_CEILING = 6000;

const GRAPH_ROUTE = '**/api/v1/graph';

// The `large` profile ships a ~2.6 MB /graph payload the page must fetch,
// JSON-parse, set into state and reconcile before it can even decide to
// degrade -- comfortably past the 5 s default assertion timeout on a shared
// CI runner (observed: the notice mounts ~1 s after the default expiry). This
// is a nightly @perf-heavy benchmark with a 360 s test budget, so wait
// generously for the render decision; `wall` below still records how long it
// actually took.
const DECISION_TIMEOUT = 30_000;

/** Worst rAF gap (ms) over `windowMs` -- a proxy for main-thread blocking. */
async function measureJank(
  page: import('@playwright/test').Page,
  windowMs: number,
): Promise<number> {
  return page.evaluate(async (ms) => {
    return await new Promise<number>((resolve) => {
      let worst = 0;
      let last = performance.now();
      const end = last + ms;
      const tick = () => {
        const now = performance.now();
        worst = Math.max(worst, now - last);
        last = now;
        if (now < end) requestAnimationFrame(tick);
        else resolve(worst);
      };
      requestAnimationFrame(tick);
    });
  }, windowMs);
}

test.describe('Network graph scale benchmark', {
  tag: '@perf-heavy',
  // A render-cost measurement that deliberately mounts thousands of list rows
  // (the degraded fallback at `large`). The per-test axe scan on that DOM
  // adds minutes for no signal; the network page's a11y is covered by
  // accessibility.spec.ts and networkPerf.spec.ts.
  annotation: { type: SKIP_A11Y_SCAN, description: 'perf benchmark; a11y covered elsewhere' },
}, () => {
  test.skip(
    !process.env.RUN_PERF_HEAVY,
    'perf-heavy: runs in the nightly e2e schedule only (set RUN_PERF_HEAVY=1)',
  );
  test.describe.configure({ timeout: 360_000 });

  test('records the render cliff across the #468 profiles and enforces the ceiling behaviour', async ({
    page,
  }) => {
    const rows: string[] = [];

    for (const profile of GRAPH_SCALE_PROFILES) {
      const graph = buildScaleGraph(profile);
      const overBudget = graph.nodes.length > NODE_CEILING || graph.edges.length > EDGE_CEILING;

      await page.unroute(GRAPH_ROUTE);
      await page.route(GRAPH_ROUTE, (route) => route.fulfill({ json: graph }));

      const started = Date.now();
      await page.goto('/network');
      const notice = page.getByTestId('graph-over-budget');
      const canvas = page.locator('canvas');

      let jank = 0;
      if (overBudget) {
        await expect(notice).toBeVisible({ timeout: DECISION_TIMEOUT });
        await expect(canvas).toHaveCount(0);
      } else {
        await expect(canvas).toHaveCount(1, { timeout: DECISION_TIMEOUT });
        await expect(notice).toHaveCount(0);
        jank = await measureJank(page, 2000);
      }
      const wall = Date.now() - started;

      rows.push(
        `| ${profile.name} | ${graph.nodes.length} | ${graph.edges.length} | ` +
          `${overBudget ? 'notice (degraded)' : 'canvas'} | ${jank ? `${jank.toFixed(0)} ms` : '—'} | ${wall} ms |`,
      );
    }

    const table = [
      '',
      'Network graph render cliff (synthetic #468 profiles):',
      '',
      '| profile | nodes | edges | rendered | worst rAF gap | wall to decision |',
      '|---|--:|--:|---|--:|--:|',
      ...rows,
      '',
    ].join('\n');
    console.log(table);
    test.info().annotations.push({ type: 'graph-scale-benchmark', description: table });

    // Deterministic: the ceiling puts `typical` on the canvas and `large`
    // on the degraded notice. (Jank numbers above are trend-only.)
    expect(rows.find((r) => r.startsWith('| typical |'))).toContain('canvas');
    expect(rows.find((r) => r.startsWith('| large |'))).toContain('degraded');
  });
});
