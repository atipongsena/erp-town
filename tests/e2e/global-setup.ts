// Playwright global setup of `pnpm test:e2e`: snapshot + seed test.localhost (see
// tests/integration/helpers.ts), then run one sales flow on a fixed date so the printed documents (names,
// Buddhist-Era dates, amounts in words) are the same on every run. The teardown restores the snapshot.

import { prepareTestSite, restoreTestSite, salesFlow } from '../integration/helpers.ts'

/** Document date of the printed samples: after the seeded opening stock (1 Jan of last year) and inside
 * the seeded fiscal years (last, this and next year), so valid until the end of 2027. */
export const PRINT_SAMPLE_DATE = '2026-01-15'

export default async function globalSetup(): Promise<() => Promise<void>> {
  await prepareTestSite()
  try {
    const { so, dn, si, pe } = await salesFlow({
      customer: 'CUST-001',
      items: [
        ['ITEM-001', 2, 135],
        ['ITEM-005', 1, 450],
        ['ITEM-009', 12, 25],
      ],
      date: PRINT_SAMPLE_DATE, // seeded selling rates given: the price list starts on the seed day
    })
    process.env.PRINT_SAMPLES = JSON.stringify({
      'Sales Order': so.name,
      'Delivery Note': dn.name,
      'Sales Invoice': si.name,
      'Payment Entry': pe.name,
    })
  } catch (e) {
    await restoreTestSite()
    throw e
  }
  return restoreTestSite
}
