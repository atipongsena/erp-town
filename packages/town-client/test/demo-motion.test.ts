import { expect, it } from 'vitest'
import { DemoMotion } from '../src/demo-motion.ts'
import type { WorkActor } from '../src/live-work.ts'
import { WorkSimulation } from '../src/work-simulation.ts'

const actors: WorkActor[] = [
  { id: 'demo-shop', kind: 'human', department: 'hq', activity: 'idle' },
  { id: 'demo-warehouse', kind: 'human', department: 'hq', activity: 'idle' },
  { id: 'demo-bank', kind: 'human', department: 'hq', activity: 'idle' },
  { id: 'demo-assistant', kind: 'agent', department: 'it', activity: 'idle' },
]

it('runs independent repeating jobs, retains identities and leaves the authoritative input untouched', () => {
  const before = structuredClone(actors)
  const loop = new DemoMotion()
  const work = new WorkSimulation()
  loop.observe(actors, 'ACME')
  let concurrent = false
  const departments = new Map<string, Set<string>>()
  for (let time = 0; time <= 300000; time += 100) {
    loop.step(time, work.view(), true)
    const intents = loop.view()
    work.observe(intents, 'ACME', time)
    if (work.view().actors.filter((a) => ['working', 'travelling'].includes(a.phase)).length >= 3)
      concurrent = true
    for (const actor of intents) {
      const visited = departments.get(actor.id) ?? new Set<string>()
      visited.add(actor.department)
      departments.set(actor.id, visited)
      expect(actor.travel).toBeUndefined()
      expect(actor.task_ref).toBeUndefined()
      expect(actor.simulation).toBe('loop')
    }
  }
  expect(concurrent).toBe(true)
  expect(loop.view().map((a) => a.id)).toEqual(actors.map((a) => a.id))
  expect([...departments.values()].every((v) => v.size >= 2)).toBe(true)
  expect(actors).toEqual(before)
  expect(work.view().actors.some((a) => a.phase !== 'idle')).toBe(true)
})

it('does not advance cycles while paused/hidden and releases all state on scope loss', () => {
  const loop = new DemoMotion()
  const work = new WorkSimulation()
  loop.observe(actors, 'A')
  work.observe(loop.view(), 'A', 0)
  const before = loop.view()
  for (let time = 0; time <= 300000; time += 100) loop.step(time, work.view(), false)
  expect(loop.view()).toEqual(before)
  loop.observe([], 'B')
  expect(loop.view()).toEqual([])
  loop.reset()
  expect(loop.view()).toEqual([])
})

it('pauses live presentation poses without releasing a job or jumping after a long pause', () => {
  const work = new WorkSimulation()
  const source = actors[0]
  if (!source) throw new Error('Missing test actor')
  work.observe(
    [{ ...source, from_department: 'hq', department: 'shop', activity: 'working' }],
    'A',
    0,
  )
  work.step(100)
  const before = work.actor('demo-shop')?.position
  if (!before) throw new Error('Missing starting pose')
  work.setPaused(true, 100)
  work.step(60000)
  expect(work.actor('demo-shop')?.position).toEqual(before)
  work.setPaused(false, 60000)
  work.step(60100)
  const after = work.actor('demo-shop')?.position
  if (!after) throw new Error('Missing resumed pose')
  expect(after).not.toEqual(before)
  expect(
    Math.hypot(after[0] - before[0], after[1] - before[1], after[2] - before[2]),
  ).toBeLessThanOrEqual(0.81)
})
