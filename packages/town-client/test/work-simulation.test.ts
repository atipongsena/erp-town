import { describe, expect, it } from 'vitest'
import type { WorkActor } from '../src/live-work.ts'
import { actorStation } from '../src/work-navigation.ts'
import { WorkSimulation } from '../src/work-simulation.ts'

const worker = (id = 'a'): WorkActor => ({
  id,
  kind: 'human',
  from_department: 'hq',
  department: 'shop',
  activity: 'typing',
})
describe('bounded presentation work runtime', () => {
  it('reserves imminent scheduled crossings for trucks without delaying the server schedule', () => {
    const sim = new WorkSimulation()
    const start = Date.parse('2026-10-12T00:00:00Z')
    sim.observe(
      [
        {
          ...worker(),
          from_department: 'it',
          department: 'shop',
          activity: 'travelling',
          travel: { started_at: '2026-10-12T00:00:00Z', duration_ms: 10000 },
        },
      ],
      'A',
      start,
    )
    let reserved = false
    for (let elapsed = 0; elapsed <= 10000; elapsed += 100) {
      sim.step(start + elapsed)
      if (!sim.reserveCrossing('east-crossing', 'trip:test')) reserved = true
      sim.releaseCrossings('trip:test')
    }
    expect(reserved).toBe(true)
    expect(sim.actor('a')?.phase).toBe('arrived')
    expect(sim.reserveCrossing('east-crossing', 'trip:test')).toBe(true)
  })
  it('retargets from its current pose, cancels routes and releases scoped work', () => {
    const sim = new WorkSimulation()
    sim.observe([worker()], 'A', 0)
    sim.step(1000)
    const middle = sim.view().actors[0]?.position
    sim.observe([{ ...worker(), department: 'bank' }], 'A', 1000)
    expect(sim.view().actors[0]?.position).toEqual(middle)
    sim.observe([{ ...worker(), activity: 'idle' }], 'A', 1100)
    expect(sim.view().actors[0]?.route).toEqual([])
    sim.observe([], 'B', 1200)
    expect(sim.view().actors).toEqual([])
  })
  it('follows scheduled absolute time after hidden tabs without extending server deadlines', () => {
    const sim = new WorkSimulation()
    const start = Date.parse('2026-10-12T00:00:00Z')
    const actor = {
      ...worker(),
      activity: 'travelling' as const,
      travel: { started_at: '2026-10-12T00:00:02Z', duration_ms: 10000 },
    }
    sim.observe([actor], 'A', start)
    expect(sim.view().actors[0]?.phase).toBe('queued')
    sim.step(start + 7000)
    expect(sim.view().actors[0]?.phase).toBe('travelling')
    sim.step(start + 12000)
    expect(sim.view().actors[0]).toMatchObject({
      phase: 'arrived',
      position: actorStation('shop'),
      route: [],
    })
  })
  it('reserves work bays FIFO and unblocks the next worker on cancellation', () => {
    const sim = new WorkSimulation()
    sim.observe(
      [
        { ...worker('a'), from_department: 'shop' },
        { ...worker('b'), from_department: 'shop' },
      ],
      'A',
      0,
    )
    expect(sim.view().actors.map((a) => a.phase)).toEqual(['working', 'queued'])
    expect(sim.view().actors[1]?.position).not.toEqual(sim.view().actors[0]?.position)
    sim.observe([{ ...worker('b'), from_department: 'shop' }], 'A', 1)
    expect(sim.view().actors[0]?.phase).toBe('working')
  })
  it('bounds actors and never preserves a stale task after completion', () => {
    const sim = new WorkSimulation()
    sim.observe(
      Array.from({ length: 20 }, (_, i) => worker(String(i))),
      'A',
      0,
    )
    expect(sim.view().actors).toHaveLength(16)
    expect(sim.view().overflow).toBe(4)
    sim.observe([{ ...worker(), activity: 'idle', task_ref: undefined }], 'A', 0)
    expect(sim.view().actors[0]?.intent.task_ref).toBeUndefined()
  })
  it('allows only one actor in a crossing and leaves no reservation after reset', () => {
    const sim = new WorkSimulation()
    sim.observe(
      [worker('a'), worker('b')].map((a) => ({
        ...a,
        from_department: 'bank',
        department: 'warehouse',
      })),
      'A',
      0,
    )
    for (let t = 0; t < 40000; t += 100) {
      sim.step(t)
      const crossing = sim
        .view()
        .actors.filter(
          (a) =>
            a.position[0] > 27 && a.position[0] < 29 && a.position[2] > -7 && a.position[2] < 7,
        )
      expect(crossing.length).toBeLessThanOrEqual(1)
    }
    sim.reset()
    expect(sim.view().reservations).toEqual([])
  })
})
