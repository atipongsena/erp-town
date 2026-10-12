import { defineConfig, devices } from '@playwright/test'

// Isolated client checks: no live ERP, database seed or credential provisioning.
export default defineConfig({
  testDir: './browser',
  fullyParallel: false,
  workers: 1,
  timeout: 60000,
  reporter: 'list',
  outputDir: 'test-results',
  webServer: {
    command: process.env.TOWN_CLIENT_PREVIEW
      ? 'pnpm exec vite preview --host 127.0.0.1 --port 5173 --strictPort'
      : 'pnpm dev',
    url: 'http://127.0.0.1:5173',
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
  },
  use: {
    baseURL: 'http://127.0.0.1:5173',
    ...devices['Desktop Chrome'],
    launchOptions: { args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] },
    trace: 'retain-on-failure',
  },
})
