// Spec §8 / §9.3 / §10.4: while redis-town is down documents still submit and their events wait in the
// outbox as `Pending`; once it is back the sweeper publishes each of them exactly once.

import { afterAll, describe, expect, it } from 'vitest'
import {
  closeTownRedis,
  compose,
  createSalesOrder,
  getList,
  reportMetrics,
  streamTail,
  submitSalesOrder,
  waitForEvents,
  waitUntil,
} from './helpers.ts'

const OUTBOX = 'Town Event Outbox'
const ORDERS = 3
const RECOVERY_LIMIT_MS = 150_000 // two sweeps (scheduler tick 60 s) + backoff

interface OutboxRow {
  event_id: string
  subject_name: string
  status: string
  attempts: number
  stream_id: string | null
}

const outboxRows = (names: string[]) =>
  getList<OutboxRow>(OUTBOX, {
    filters: { subject_doctype: 'Sales Order', subject_name: ['in', names] },
    fields: ['event_id', 'subject_name', 'status', 'attempts', 'stream_id'],
  })

let redisStopped = false

afterAll(async () => {
  if (redisStopped) await compose('start', 'redis-town') // never leave the stack without redis-town
  await closeTownRedis()
})

describe('redis-town outage', () => {
  it('keeps events Pending while down and publishes each exactly once after restart', async () => {
    const from = await streamTail()
    await closeTownRedis() // reconnect after the restart instead of retrying against a stopped container
    const names: string[] = []
    try {
      redisStopped = true
      await compose('stop', 'redis-town')

      for (let i = 0; i < ORDERS; i++) {
        const so = await submitSalesOrder(await createSalesOrder())
        expect(so.docstatus).toBe(1)
        names.push(so.name)
      }
      // the after-commit publish job has tried and failed: rows stay Pending with a backoff
      const rows = await waitUntil(
        () => outboxRows(names),
        (rs) => rs.length === ORDERS && rs.every((r) => r.attempts >= 1),
        { timeoutMs: 30_000, what: 'a failed publish attempt per outbox row' },
      )
      expect(rows.map((r) => r.status)).toEqual(Array(ORDERS).fill('Pending'))
    } finally {
      await compose('start', 'redis-town')
      redisStopped = false
    }

    const restarted = Date.now()
    const entries = await waitForEvents(
      from,
      (es) => names.every((n) => es.some((e) => e.event.subject.name === n)),
      { timeoutMs: RECOVERY_LIMIT_MS, intervalMs: 2_000 },
    )
    reportMetrics('redis-outage', {
      orders: ORDERS,
      recovered_after_restart_ms: Date.now() - restarted,
    })

    const ours = entries.filter((e) => names.includes(e.event.subject.name))
    expect(ours).toHaveLength(ORDERS)
    expect(ours.map((e) => e.event.type)).toEqual(Array(ORDERS).fill('erp.sales_order.submitted'))
    const ids = entries.map((e) => e.event.id)
    expect(new Set(ids).size).toBe(ids.length)

    const rows = await outboxRows(names)
    expect(rows.every((r) => r.status === 'Published')).toBe(true)
    expect(new Set(rows.map((r) => r.stream_id))).toEqual(new Set(ours.map((e) => e.streamId)))
  }, 240_000)
})
