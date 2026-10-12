import type { WorkActor } from './live-work.ts'
import {
  actorStation,
  at,
  crossingZones,
  navigationLayout,
  pedestrianRoute,
  routeLength,
  type WorkPosition,
} from './work-navigation.ts'

export type WorkPhase = 'queued' | 'travelling' | 'arrived' | 'working' | 'idle'
export type WorkPose = {
  id: string
  intent: WorkActor
  position: WorkPosition
  yaw: number
  phase: WorkPhase
  route: WorkPosition[]
  waiting?: 'crossing' | 'work-bay' | 'departure'
}
export type WorkSimulationView = { actors: WorkPose[]; overflow: number; reservations: string[] }
type Track = WorkPose & {
  path: WorkPosition[]
  cumulative: number[]
  length: number
  distance: number
  key: string
  order: number
}
export function compileRoute(path: readonly WorkPosition[]) {
  const cumulative = [0]
  for (let i = 1; i < path.length; i++) {
    const a = at(path, i - 1),
      b = at(path, i)
    cumulative.push(at(cumulative, i - 1) + Math.hypot(b[0] - a[0], b[1] - a[1], b[2] - a[2]))
  }
  return cumulative
}
export function sampleRoute(
  path: readonly WorkPosition[],
  cumulative: readonly number[],
  distance: number,
) {
  if (!path.length) throw new Error('A route must contain a position')
  const length = cumulative.at(-1) ?? 0
  const d = Math.max(0, Math.min(length, distance))
  let low = 1,
    high = path.length - 1
  while (low < high) {
    const middle = (low + high) >> 1
    if (at(cumulative, middle) < d) low = middle + 1
    else high = middle
  }
  const a = at(path, Math.max(0, low - 1)),
    b = path[low] ?? a
  const segment = (cumulative[low] ?? 0) - (cumulative[low - 1] ?? 0)
  const t = segment > 0 ? (d - (cumulative[low - 1] ?? 0)) / segment : 0
  return {
    position: a.map((v, i) => v + (at(b, i) - v) * t) as WorkPosition,
    yaw: Math.atan2(b[0] - a[0], b[2] - a[2]),
    arrived: d >= length,
  }
}
export function turnTowards(from: number, to: number, amount: number) {
  return (
    from + Math.atan2(Math.sin(to - from), Math.cos(to - from)) * Math.min(1, Math.max(0, amount))
  )
}

/** Mutable poses are presentation only. No ERP callbacks or completion writes. */
export class WorkSimulation {
  #scope = ''
  #actors = new Map<string, Track>()
  #zones = new Map<string, string>()
  #bays = new Map<string, string>()
  #last = 0
  #order = 0
  #overflow = 0
  #paused = false
  setPaused(paused: boolean, nowMs: number) {
    if (this.#paused !== paused) this.#last = nowMs
    this.#paused = paused
  }
  reset() {
    this.#actors.clear()
    this.#zones.clear()
    this.#bays.clear()
    this.#scope = ''
    this.#last = 0
    this.#overflow = 0
    this.#paused = false
  }
  observe(actors: readonly WorkActor[], scope: string, nowMs: number) {
    if (scope !== this.#scope) {
      this.reset()
      this.#scope = scope
      this.#last = nowMs
    }
    this.#overflow = Math.max(0, actors.length - 16)
    const visible = actors.slice(0, 16)
    const ids = new Set(visible.map((a) => a.id))
    for (const id of this.#actors.keys())
      if (!ids.has(id)) {
        this.#release(id)
        this.#actors.delete(id)
      }
    for (const intent of visible) {
      let actor = this.#actors.get(intent.id)
      if (!actor) {
        const position = actorStation(intent.from_department ?? intent.department, intent.kind)
        actor = {
          id: intent.id,
          intent,
          position,
          yaw: 0,
          phase: 'idle',
          route: [],
          path: [position],
          cumulative: [0],
          length: 0,
          distance: 0,
          key: '',
          order: this.#order++,
        }
        this.#actors.set(intent.id, actor)
      }
      const key = `${intent.department}:${intent.kind}:${intent.travel?.started_at ?? ''}:${intent.travel?.duration_ms ?? ''}`
      actor.intent = {
        ...intent,
        task_ref: intent.task_ref ? { ...intent.task_ref } : undefined,
        travel: intent.travel ? { ...intent.travel } : undefined,
      }
      if (intent.activity === 'idle') {
        this.#release(actor.id)
        actor.key = ''
        actor.phase = 'idle'
        actor.route = []
        actor.waiting = undefined
        continue
      }
      if (actor.key !== key) {
        this.#release(actor.id)
        actor.key = key
        const from = intent.travel
          ? actorStation(intent.from_department ?? intent.department, intent.kind)
          : actor.position
        actor.path = pedestrianRoute(from, intent.department, intent.kind)
        actor.cumulative = compileRoute(actor.path)
        actor.length = routeLength(actor.path)
        actor.distance = 0
        actor.route = actor.path
        actor.phase = 'travelling'
      }
    }
    this.step(nowMs)
  }
  #release(id: string) {
    for (const [k, v] of this.#zones) if (v === id) this.#zones.delete(k)
    for (const [k, v] of this.#bays) if (v === id) this.#bays.delete(k)
  }
  step(nowMs: number) {
    const dt = Math.min(0.5, Math.max(0, (nowMs - this.#last) / 1000))
    this.#last = nowMs
    if (this.#paused) return
    for (const a of [...this.#actors.values()].sort(
      (a, b) => a.order - b.order || a.id.localeCompare(b.id),
    )) {
      if (a.intent.activity === 'idle') continue
      a.waiting = undefined
      let next = Math.min(a.length, a.distance + dt * navigationLayout.walk_speed)
      if (a.intent.travel) {
        const elapsed = nowMs - Date.parse(a.intent.travel.started_at)
        next =
          a.length *
          Math.min(
            1,
            Math.max(0, a.intent.travel.duration_ms ? elapsed / a.intent.travel.duration_ms : 1),
          )
        if (elapsed < 0) {
          a.phase = 'queued'
          a.waiting = 'departure'
          a.route = []
          a.position = [...at(a.path, 0)]
          continue
        }
      } else {
        // Reserve only the next crossing. Ray/box entry keeps large frame steps
        // from skipping a conflict zone. Work bay waits never hold a crossing.
        for (const zone of crossingZones) {
          const interval = this.#interval(a, zone.min, zone.max)
          if (!interval) continue
          const [entry, exit] = interval
          const owner = this.#zones.get(zone.id)
          if (a.distance > exit + 0.05) {
            if (owner === a.id) this.#zones.delete(zone.id)
            continue
          }
          if (next >= entry && a.distance <= exit) {
            if ((owner && owner !== a.id) || this.#scheduledConflict(zone.id, a.id, nowMs)) {
              next = Math.min(next, Math.max(a.distance, entry - 0.06))
              a.waiting = 'crossing'
            } else this.#zones.set(zone.id, a.id)
          }
        }
      }
      a.distance = next
      const pose = sampleRoute(a.path, a.cumulative, next)
      a.position = pose.position
      a.yaw = turnTowards(a.yaw, pose.yaw, dt * 12)
      a.phase = a.waiting ? 'queued' : 'travelling'
      a.route = a.phase === 'travelling' ? a.path : []
      if (pose.arrived) {
        for (const [k, v] of this.#zones) if (v === a.id) this.#zones.delete(k)
        a.route = []
        a.phase = 'arrived'
        a.yaw = a.intent.department === 'hq' && a.intent.kind === 'human' ? -Math.PI / 2 : 0
        if (a.intent.activity !== 'travelling') {
          const bay = `${a.intent.kind}:${a.intent.department}`
          const owner = this.#bays.get(bay)
          if (!owner || owner === a.id || a.intent.travel) {
            this.#bays.set(bay, a.id)
            a.phase = 'working'
          } else {
            a.phase = 'queued'
            a.waiting = 'work-bay'
            const rank =
              1 +
              [...this.#actors.values()].filter(
                (other) =>
                  other.id !== owner &&
                  other.order < a.order &&
                  other.intent.activity !== 'idle' &&
                  other.intent.kind === a.intent.kind &&
                  other.intent.department === a.intent.department,
              ).length
            // Waiting places stay beside the bay; never stack bodies at its anchor.
            a.position =
              a.length > 0
                ? sampleRoute(a.path, a.cumulative, Math.max(0, a.length - rank * 2.8)).position
                : [a.position[0] + rank * 2.8, a.position[1], a.position[2]]
          }
        }
      }
    }
  }
  #interval(a: Track, min: number[], max: number[]): [number, number] | undefined {
    let entry = Infinity,
      exit = -Infinity
    for (let i = 1; i < a.path.length; i++) {
      const p = at(a.path, i - 1),
        q = at(a.path, i)
      let lo = 0,
        hi = 1
      for (const [axis, index] of [
        [0, 0],
        [2, 1],
      ] as const) {
        const delta = q[axis] - p[axis]
        if (Math.abs(delta) < 1e-9) {
          if (p[axis] < at(min, index) || p[axis] > at(max, index)) {
            hi = -1
            break
          }
        } else {
          const x = (at(min, index) - p[axis]) / delta,
            y = (at(max, index) - p[axis]) / delta
          lo = Math.max(lo, Math.min(x, y))
          hi = Math.min(hi, Math.max(x, y))
        }
      }
      if (lo <= hi) {
        const base = at(a.cumulative, i - 1),
          length = at(a.cumulative, i) - base
        entry = Math.min(entry, base + length * lo)
        exit = Math.max(exit, base + length * hi)
      }
    }
    return Number.isFinite(entry) ? [entry, exit] : undefined
  }
  actor(id: string): WorkPose | undefined {
    return this.#actors.get(id)
  }
  reserveCrossing(zone: string, owner: string) {
    if (this.#scheduledConflict(zone, owner, this.#last)) return false
    const held = this.#zones.get(zone)
    if (held && held !== owner) return false
    this.#zones.set(zone, owner)
    return true
  }
  #scheduledConflict(zoneId: string, owner: string, nowMs: number) {
    const zone = crossingZones.find((z) => z.id === zoneId)
    if (!zone) return false
    return [...this.#actors.values()].some((a) => {
      const schedule = a.intent.travel
      if (a.id === owner || !schedule || a.intent.activity === 'idle' || a.length === 0)
        return false
      const interval = this.#interval(a, zone.min, zone.max)
      if (!interval) return false
      const start = Date.parse(schedule.started_at)
      const entry = start + (interval[0] / a.length) * schedule.duration_ms
      const exit = start + (interval[1] / a.length) * schedule.duration_ms
      // Give scheduled pedestrians right of way before a vehicle enters; this
      // exceeds the truck's 500-ms step and short crossing traversal time.
      return nowMs + 1000 >= entry && nowMs <= exit
    })
  }
  releaseCrossings(owner: string) {
    for (const [k, v] of this.#zones) if (v === owner) this.#zones.delete(k)
  }
  view(): WorkSimulationView {
    return {
      actors: [...this.#actors.values()].map((a) => ({
        id: a.id,
        intent: a.intent,
        position: [...a.position],
        yaw: a.yaw,
        phase: a.phase,
        route: a.route.map((p) => [...p]),
        waiting: a.waiting,
      })),
      overflow: this.#overflow,
      reservations: [...this.#zones.keys(), ...this.#bays.keys()],
    }
  }
}
