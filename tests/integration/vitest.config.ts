import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'

// `pnpm test:integration`: against the running dev stack (`pnpm erp:up`), on test.localhost only.
// One file at a time: the files share the test site, its single worker and the redis-town container.
export default defineConfig({
  test: {
    root: path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..'),
    include: ['tests/integration/**/*.test.ts'],
    globalSetup: ['tests/integration/setup.ts'],
    fileParallelism: false,
    silent: false, // show the measurements the tests log (latency, recovery, bulk publish time)
    testTimeout: 60_000,
    hookTimeout: 120_000,
    teardownTimeout: 120_000,
  },
})
