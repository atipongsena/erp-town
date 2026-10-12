import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './client-browser',
  workers: 1,
  timeout: 120000,
  reporter: 'list',
  outputDir: '../../test-results/client-live',
  // Exercise the same Vite routing used by the local workspace, not a separate
  // test-only reverse proxy between the browser and ERP.
  webServer: {
    command:
      'pnpm --filter @erp-town/town-client exec vite --host 127.0.0.1 --port 5197 --strictPort',
    url: 'http://127.0.0.1:5197',
    timeout: 30000,
    reuseExistingServer: false,
  },
  use: {
    baseURL: 'http://127.0.0.1:5197',
    ...devices['Desktop Chrome'],
    launchOptions: { args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] },
    // Network traces would persist session cookies and JWTs.
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
})
