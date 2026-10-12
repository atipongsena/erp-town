import type { WorkActor } from './live-work.ts'
import { WorkClock } from './work-clock.ts'
import type { WorkSimulationView } from './work-simulation.ts'
import type { Department } from './world-view.ts'

type Track = { actor: WorkActor; route: Department[]; index: number; work: number; dwell: number }
const routes: Record<string, Department[]> = {
  'demo-shop': ['shop', 'post', 'hq'],
  'demo-warehouse': ['warehouse', 'shop', 'hq'],
  'demo-bank': ['bank', 'hq'],
  'demo-assistant': ['it', 'shop'],
}

/** Independent presentation cycles. This class cannot call ERP or invent task references. */
export class DemoMotion {
  #scope = ''
  #actors = new Map<string, Track>()
  #clock = new WorkClock()
  reset() {
    this.#scope = ''
    this.#actors.clear()
    this.#clock.reset()
  }
  observe(actors: readonly WorkActor[], scope: string) {
    if (scope !== this.#scope) {
      this.reset()
      this.#scope = scope
    }
    const ids = new Set(actors.slice(0, 16).map((a) => a.id))
    for (const id of this.#actors.keys()) if (!ids.has(id)) this.#actors.delete(id)
    for (const [index, source] of actors.slice(0, 16).entries()) {
      if (this.#actors.has(source.id)) continue
      const route = routes[source.id] ?? [source.department, source.kind === 'agent' ? 'it' : 'hq']
      const target = route[0] ?? source.department
      this.#actors.set(source.id, {
        actor: {
          id: source.id,
          kind: source.kind,
          appearance: source.appearance,
          from_department: source.from_department ?? source.department,
          department: target,
          activity: 'working',
          simulation: 'loop',
        },
        route,
        index: 0,
        work: 0,
        dwell: 4 + index * 2,
      })
    }
  }
  step(nowMs: number, work: WorkSimulationView, active: boolean) {
    const dt = this.#clock.step(nowMs, active)
    if (!dt) return
    for (const track of this.#actors.values()) {
      if (work.actors.find((a) => a.id === track.actor.id)?.phase !== 'working') continue
      track.work += dt
      if (track.work < track.dwell) continue
      track.work = 0
      track.index = (track.index + 1) % track.route.length
      track.actor = {
        ...track.actor,
        from_department: track.actor.department,
        department: track.route[track.index] ?? track.actor.department,
      }
    }
  }
  view(): WorkActor[] {
    return [...this.#actors.values()].map((t) => ({ ...t.actor }))
  }
}
