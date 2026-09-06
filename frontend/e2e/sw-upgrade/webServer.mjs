// Issue #476 (WEB-02): Playwright webServer entry point for the
// service-worker upgrade suite (playwright.sw.config.ts).
//
// It first makes sure the two fixture builds exist (building them from source
// on a fresh checkout -- this is what makes the suite self-contained under CI),
// then starts the sw-upgrade harness server on port 7300 (or SW_TEST_PORT) and
// stays up until Playwright tears it down.

import { ensureFixtures } from './fixtures.mjs';
import { createSwUpgradeServer, DEFAULT_PORT } from './sw-server.mjs';

const port = Number(process.env.SW_TEST_PORT || DEFAULT_PORT);
const force = process.env.SW_TEST_FIXTURE_REBUILD === '1';

await ensureFixtures(force);

const server = await createSwUpgradeServer(port);
console.log(`[sw-upgrade] harness listening on http://localhost:${port}`);

for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => {
    server.close(() => process.exit(0));
  });
}
