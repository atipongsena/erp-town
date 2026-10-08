// Spec §9.5 / §10.7: `scenario.run(days=0, bulk=400)` → every Sales Invoice event `Published` within 60 s
// of the call returning.

import { describe, expect, it } from 'vitest'
import { benchExecute, getCount, reportMetrics, waitUntil } from './helpers.ts'

const OUTBOX = 'Town Event Outbox'
const BULK = 400
const PUBLISH_LIMIT_MS = 60_000
const SI_EVENTS = { event_type: 'erp.sales_invoice.submitted' }

describe('bulk load', () => {
  it(`publishes all ${BULK} bulk invoice events within ${PUBLISH_LIMIT_MS / 1000} s`, async () => {
    const before = await getCount(OUTBOX, SI_EVENTS)
    const started = Date.now()
    const counts = await benchExecute<Record<string, number>>('erp_town.setup.scenario.run', {
      days: 0,
      bulk: BULK,
    })
    const returned = Date.now()
    expect(counts?.['Sales Invoice']).toBe(BULK)

    const progress = () =>
      Promise.all([
        getCount(OUTBOX, SI_EVENTS),
        getCount(OUTBOX, { ...SI_EVENTS, status: ['!=', 'Published'] }),
      ])
    const [total, unpublished] = await waitUntil(
      progress,
      ([t, u]) => t === before + BULK && u === 0,
      {
        timeoutMs: PUBLISH_LIMIT_MS,
        intervalMs: 500,
        what: `all ${BULK} invoice events Published`,
      },
    )
    const report = {
      scenario_call_s: Math.round((returned - started) / 100) / 10,
      published_after_return_s: Math.round((Date.now() - returned) / 100) / 10,
    }
    reportMetrics('bulk-load', { invoices: BULK, ...report })
    expect(total).toBe(before + BULK)
    expect(unpublished).toBe(0)
  }, 180_000)
})
