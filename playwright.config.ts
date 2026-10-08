import { defineConfig, devices } from '@playwright/test'

// `pnpm test:e2e`: against the running dev stack (`pnpm erp:up`). The global setup seeds test.localhost
// (restored afterwards); specs that need the test site use http://test.localhost:8080 (Chromium resolves
// *.localhost to 127.0.0.1). Screenshot baselines are committed next to the specs
// (`<spec>-snapshots/<name>-chromium-<platform>.png`); re-record with `pnpm test:e2e --update-snapshots`.
export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/e2e/global-setup.ts',
  // the specs share the seeded test site
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  timeout: 60_000,
  globalTimeout: 15 * 60_000,
  use: {
    baseURL: 'http://localhost:8080',
    trace: 'retain-on-failure',
  },
  expect: {
    toHaveScreenshot: { animations: 'disabled', caret: 'hide' },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
