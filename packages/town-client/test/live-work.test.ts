import { describe, expect, it } from 'vitest'
import { actorIntents, DeliveryTransitions, decodeLiveDemo } from '../src/live-work.ts'
import type { DisplayFact } from '../src/world-view.ts'

const delivery = (eventId: string, status: DisplayFact['status'] = 'billing'): DisplayFact => ({
  id: 'dn',
  doctype: 'Delivery Note',
  name: 'DN-1',
  kind: 'document',
  scope: '',
  department: 'warehouse',
  status,
  eventId,
  eventType: 'erp.delivery_note.submitted',
})
describe('actual work, not ambient scenery', () => {
  it('requires an explicit demo-loop capability and rejects unknown motion modes', () => {
    const raw = {
      enabled: true,
      mode: 'erp-test',
      company: 'ACME',
      round: 1,
      stage: 'completed',
      phase: 'idle',
      actors: [],
      documents: [],
      motion_mode: 'loop',
    }
    expect(decodeLiveDemo(raw, 'ACME')?.motion_mode).toBe('loop')
    expect(decodeLiveDemo({ ...raw, motion_mode: 'unknown' }, 'ACME')).toBeUndefined()
    expect(decodeLiveDemo({ ...raw, motion_mode: undefined }, 'ACME')?.motion_mode).toBeUndefined()
  })
  it('does not manufacture another dispatch when a known subject is removed and reappears', () => {
    const flow = new DeliveryTransitions()
    flow.observe([delivery('old')], true)
    flow.observe([], true)
    expect(flow.observe([delivery('old')], true)).toEqual([])
    expect(flow.observe([delivery('new')], true)).toEqual([{ id: 'new', name: 'DN-1' }])
    flow.observe([], true)
    expect(flow.observe([delivery('new')], true)).toEqual([])
  })
  it('accepts bounded UTC travel schedules and rejects malformed timing', () => {
    const raw = {
      enabled: true,
      mode: 'erp-test',
      company: 'ACME-TH',
      round: 1,
      stage: 'sales-review',
      phase: 'travelling',
      layout_version: 'town-walk-v1',
      stage_started_at: '2026-10-12T00:00:00Z',
      stage_duration_ms: 12000,
      documents: [],
      actors: [
        {
          id: 'demo-shop',
          kind: 'human',
          department: 'shop',
          from_department: 'hq',
          activity: 'travelling',
          travel: { started_at: '2026-10-12T00:00:02Z', duration_ms: 10000 },
        },
      ],
    }
    expect(decodeLiveDemo(raw, 'ACME-TH')?.actors[0]?.travel?.duration_ms).toBe(10000)
    expect(decodeLiveDemo({ ...raw, layout_version: undefined }, 'ACME-TH')).toBeUndefined()
    expect(
      decodeLiveDemo({ ...raw, layout_version: 'different-layout' }, 'ACME-TH'),
    ).toBeUndefined()
    expect(
      decodeLiveDemo(
        {
          ...raw,
          actors: [{ ...raw.actors[0], travel: { started_at: 'yesterday', duration_ms: -1 } }],
        },
        'ACME-TH',
      ),
    ).toBeUndefined()
  })
  it('never turns document backlog or a running AI without tool evidence into working people', () => {
    expect(actorIntents([delivery('old')], [], undefined)).toEqual([])
    const ai: DisplayFact = {
      ...delivery(''),
      id: 'ai',
      doctype: 'Town Agent Run',
      name: 'RUN-1',
      kind: 'agent_run',
      department: 'it',
      status: 'ai-running',
    }
    expect(actorIntents([ai], [], undefined)[0]).toMatchObject({
      department: 'it',
      activity: 'idle',
    })
    expect(
      actorIntents(
        [{ ...ai, work: { department: 'shop', activity: 'working' } }],
        [],
        undefined,
      )[0],
    ).toMatchObject({ department: 'shop', activity: 'working' })
    expect(
      actorIntents(
        [{ ...ai, status: 'ai-completed', work: { department: 'shop', activity: 'working' } }],
        [],
        undefined,
      )[0],
    ).toMatchObject({ activity: 'idle' })
  })
  it('moves the same human identity to the server department and uses real work emotes', () => {
    const person = {
      id: 'person',
      department: 'warehouse' as const,
      appearance: 'human-blue' as const,
      emote: 'carry' as const,
    }
    expect(actorIntents([], [person], undefined)[0]).toMatchObject({
      id: 'human:person',
      department: 'warehouse',
      activity: 'carry',
    })
    expect(
      actorIntents([], [{ ...person, department: 'bank', emote: 'read' }], undefined)[0],
    ).toMatchObject({ id: 'human:person', department: 'bank', activity: 'reading' })
  })
  it('does not replay snapshots, duplicate deliveries, cancellation, reconnect or old history as new transport', () => {
    const flow = new DeliveryTransitions()
    expect(flow.observe([delivery('old')], true)).toEqual([])
    expect(flow.observe([delivery('old')], true)).toEqual([])
    expect(flow.observe([delivery('new')], true)).toEqual([{ id: 'new', name: 'DN-1' }])
    expect(flow.observe([delivery('new')], true)).toEqual([])
    expect(
      flow.observe(
        [{ ...delivery('invoice-update'), eventType: 'erp.delivery_note.billing_updated' }],
        true,
      ),
    ).toEqual([])
    expect(flow.observe([delivery('cancel', 'cancelled')], true)).toEqual([])
    expect(flow.observe([], false)).toEqual([])
    expect(flow.observe([delivery('newer')], true)).toEqual([])
  })
  it('rejects demo data with wrong company, unknown actors or private payload instead of silently using it', () => {
    const data = {
      enabled: true,
      mode: 'erp-test',
      company: 'ACME',
      round: 1,
      phase: 'running',
      stage: 'review-order',
      actors: [{ id: 'sales', kind: 'human', department: 'shop', activity: 'working' }],
      documents: [],
    }
    expect(decodeLiveDemo(data, 'ACME')?.actors[0]).toMatchObject({
      id: 'sales',
      department: 'shop',
    })
    expect(decodeLiveDemo(data, 'OTHER')).toBeUndefined()
    expect(
      decodeLiveDemo({ ...data, actors: [{ ...data.actors[0], department: 'secret' }] }, 'ACME'),
    ).toBeUndefined()
    expect(
      decodeLiveDemo({ ...data, actors: [{ ...data.actors[0], prompt: 'private' }] }, 'ACME'),
    ).toBeUndefined()
  })
})
