// Spec §9.3 / §10.3: SO → DN → SI → PE over REST puts the four sales-flow events on the town stream, in
// order, schema-valid, with the causation chain of spec §3.1; p95 submit → stream latency < 1 s.

import { validateEvent } from '@erp-town/event-schema'
import { afterAll, describe, expect, it } from 'vitest'
import {
  closeTownRedis,
  createSalesOrder,
  getList,
  newTownRedis,
  nextMatchingEntry,
  percentile,
  reportMetrics,
  SALES_FLOW_TYPES,
  type StreamEntry,
  salesFlow,
  streamTail,
  submitSalesOrder,
  waitForEvents,
} from './helpers.ts'

const SO_SUBMITTED = 'erp.sales_order.submitted'
const LATENCY_SAMPLES = 20
const P95_LIMIT_MS = 1_000
const SYSTEM_TZ_OFFSET = '+07:00' // seeded System Settings time_zone Asia/Bangkok (no DST)

const isSalesFlow = (e: StreamEntry) =>
  (SALES_FLOW_TYPES as readonly string[]).includes(e.event.type)

/** epoch ms of a Frappe datetime (`YYYY-MM-DD HH:MM:SS.ffffff`, system time zone) */
const frappeEpochMs = (value: string) =>
  Date.parse(`${value.replace(' ', 'T').slice(0, 23)}${SYSTEM_TZ_OFFSET}`)

afterAll(closeTownRedis)

describe('sales flow over REST', () => {
  it('puts the four events on the stream within 5 s, ordered, valid and causally chained', async () => {
    const from = await streamTail()
    const { so, dn, si, pe } = await salesFlow()

    const entries = await waitForEvents(
      from,
      (es) => es.filter((e) => e.event.correlation_id === so.name && isSalesFlow(e)).length >= 4,
      { timeoutMs: 5_000 },
    )
    const chain = entries.filter((e) => e.event.correlation_id === so.name)

    expect(chain.map((e) => e.event.type)).toEqual([...SALES_FLOW_TYPES])
    expect(chain.map((e) => [e.event.subject.doctype, e.event.subject.name])).toEqual([
      ['Sales Order', so.name],
      ['Delivery Note', dn.name],
      ['Sales Invoice', si.name],
      ['Payment Entry', pe.name],
    ])
    for (const { event } of chain) {
      expect(validateEvent(event), JSON.stringify(event)).toEqual({ valid: true })
      expect(event.seq).toBe(1)
      expect(event.actor).toEqual({ kind: 'user', id: 'user.Administrator', on_behalf_of: null })
    }
    expect(new Set(chain.map((e) => e.event.id)).size).toBe(4)
    expect(chain[0]?.event.causation_id).toBeNull()
    for (let i = 1; i < chain.length; i++) {
      expect(chain[i]?.event.causation_id).toBe(chain[i - 1]?.event.id)
    }
  })

  it(`p95 submit → stream latency of ${LATENCY_SAMPLES} SO submits is < ${P95_LIMIT_MS} ms`, async () => {
    const reader = newTownRedis()
    const fromRequest: number[] = [] // submit request sent → event read from the stream (client clock)
    const fromResponse: number[] = [] // submit response received → event read from the stream
    const outbox: number[] = [] // outbox row written in the submit → XADD (server clock, stream id time)
    try {
      for (let i = 0; i < LATENCY_SAMPLES; i++) {
        const draft = await createSalesOrder()
        const from = await streamTail()
        const sent = performance.now()
        const so = await submitSalesOrder(draft)
        const received = performance.now()
        const { entry, seenAt } = await nextMatchingEntry(
          reader,
          from,
          (e) => e.type === SO_SUBMITTED && e.subject.name === so.name,
          { timeoutMs: 10_000 },
        )
        fromRequest.push(seenAt - sent)
        fromResponse.push(Math.max(0, seenAt - received))
        const [row] = await getList<{ creation: string }>('Town Event Outbox', {
          filters: { event_id: entry.event.id },
          fields: ['creation'],
        })
        if (!row) throw new Error(`no outbox row for ${entry.event.id}`)
        outbox.push(Number(entry.streamId.split('-')[0]) - frappeEpochMs(row.creation))
      }
    } finally {
      reader.disconnect()
    }
    const report = {
      p95_request_to_stream_ms: Math.round(percentile(fromRequest, 95)),
      p95_response_to_stream_ms: Math.round(percentile(fromResponse, 95)),
      p95_outbox_to_stream_ms: Math.round(percentile(outbox, 95)),
      max_request_to_stream_ms: Math.round(Math.max(...fromRequest)),
      median_request_to_stream_ms: Math.round(percentile(fromRequest, 50)),
    }
    reportMetrics('sales-flow-latency', { samples: LATENCY_SAMPLES, ...report })
    expect(report.p95_request_to_stream_ms).toBeLessThan(P95_LIMIT_MS)
    expect(report.p95_outbox_to_stream_ms).toBeLessThan(P95_LIMIT_MS)
    expect(report.p95_response_to_stream_ms).toBeLessThan(P95_LIMIT_MS)
  })
})
