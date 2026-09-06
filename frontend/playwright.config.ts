import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for Mycorrhizal CRM E2E tests.
 *
 * Run tests with: npx playwright test
 * Run with UI:    npx playwright test --ui
 * Run specific:   npx playwright test auth.spec.ts
 */
export default defineConfig({
  testDir: './e2e',

  // Issue #476 (WEB-02): the service-worker upgrade suite lives under
  // e2e/sw-upgrade/ but runs under its OWN config (playwright.sw.config.ts)
  // against its own two-build harness on :7300 -- never under this config,
  // which runs against the single-build docker stack. Excluding it here is
  // what keeps `npx playwright test` from accidentally trying to run those
  // specs against the wrong server. The project-level testIgnore below
  // repeats the pattern for the authenticated project.
  testIgnore: [/sw-upgrade/],

  fullyParallel: true,

  // Fail the build on CI if you accidentally left test.only in the source code
  forbidOnly: !!process.env.CI,

  // Retry on CI only
  retries: process.env.CI ? 1 : 0,

  // Tests authenticate once via the `setup` project and reuse the saved
  // storageState, so they no longer log in through the UI on every test.
  // That removes the serial-login bottleneck that forced workers: 1. We still
  // cap workers on CI to keep SQLite write contention predictable.
  workers: process.env.CI ? 2 : undefined,

  // Reporter to use
  reporter: [['html', { open: 'never' }], ['list']],

  // Shared settings for all projects
  use: {
    baseURL: 'http://localhost:7300',

    // Collect trace when retrying the failed test
    trace: 'on-first-retry',

    screenshot: 'only-on-failure',
    video: 'on-first-retry',
  },

  projects: [
    // Authenticates once and writes playwright/.auth/user.json.
    {
      name: 'setup',
      testMatch: /.*\.setup\.ts/,
    },
    // All other specs reuse the saved auth state. Specs that need a
    // logged-out state (e.g. auth.spec.ts) opt out via test.use({ storageState }).
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'playwright/.auth/user.json',
      },
      dependencies: ['setup'],
      testIgnore: [/.*\.setup\.ts/, /sw-upgrade/],
    },
  ],

  // Global setup for test data seeding
  globalSetup: './e2e/global-setup.ts',

  timeout: 30000,
  expect: {
    timeout: 5000,
  },
});
