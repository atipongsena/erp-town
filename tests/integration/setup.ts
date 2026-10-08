// Vitest global setup of `pnpm test:integration` (see helpers.ts): snapshot + seed test.localhost, issue
// Administrator API keys (process env only), point its Town Settings at `town:test:events` and run its
// erp_town scheduled jobs (publish sweeper). Teardown disables the scheduler and restores the snapshot,
// so `pnpm erp:test` is unaffected.

import { prepareTestSite, restoreTestSite } from './helpers.ts'

export default async function setup(): Promise<() => Promise<void>> {
  const started = Date.now()
  await prepareTestSite({ scheduler: true })
  console.log(
    `[integration] test.localhost seeded in ${Math.round((Date.now() - started) / 1000)} s`,
  )
  return async () => {
    await restoreTestSite()
    console.log('[integration] test.localhost restored, scheduler disabled')
  }
}
