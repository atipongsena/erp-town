import { trafficLength, trafficPose } from './traffic.ts'
import {
  crossingZones,
  navigationLayout,
  truckTripLength,
  truckTripPose,
} from './work-navigation.ts'
import type { WorkSimulation } from './work-simulation.ts'
export type DeliveryTrip = { id: string; name: string; loop?: boolean }
export type DeliveryPose = {
  id: string
  name: string
  phase: 'queued' | 'departing' | 'travelling' | 'finished' | 'cancelled'
  distance: number
  pose: ReturnType<typeof truckTripPose>
  waiting?: 'crossing'
  loop?: boolean
  laps?: number
}
export type DeliverySimulationView = { trips: DeliveryPose[]; overflow: number }
/** Finite recorded dispatch or explicit demo circulation; neither executes business commands. */
export class DeliverySimulation {
  #scope = ''
  #trips = new Map<string, DeliveryPose>()
  #seen = new Set<string>()
  #last = 0
  #overflow = 0
  #paused = false
  constructor(private readonly resources?: WorkSimulation) {}
  setPaused(paused: boolean, nowMs: number) {
    if (this.#paused !== paused) this.#last = nowMs
    this.#paused = paused
  }
  reset() {
    for (const id of this.#trips.keys()) this.resources?.releaseCrossings(`trip:${id}`)
    this.#trips.clear()
    this.#seen.clear()
    this.#scope = ''
    this.#overflow = 0
    this.#last = 0
    this.#paused = false
  }
  observe(trips: readonly DeliveryTrip[], scope: string, nowMs: number) {
    if (scope !== this.#scope) {
      this.reset()
      this.#scope = scope
      this.#last = nowMs
    }
    const ids = new Set(trips.map((t) => t.id))
    for (const [id] of this.#trips)
      if (!ids.has(id)) {
        this.resources?.releaseCrossings(`trip:${id}`)
        this.#trips.delete(id)
      }
    this.#overflow = 0
    for (const trip of trips) {
      if (this.#seen.has(trip.id)) continue
      if (this.#trips.size >= 8 || this.#seen.size >= 4096) {
        this.#overflow++
        continue
      }
      this.#seen.add(trip.id)
      this.#trips.set(trip.id, {
        ...trip,
        phase: 'queued',
        distance: 0,
        ...(trip.loop ? { laps: 0 } : {}),
        pose: trip.loop ? loopPose(0) : truckTripPose(0),
      })
    }
    this.step(nowMs)
  }
  step(nowMs: number) {
    const dt = Math.min(0.5, Math.max(0, (nowMs - this.#last) / 1000))
    this.#last = nowMs
    if (this.#paused) return
    const trip = [...this.#trips.values()].find(
      (t) => t.phase !== 'finished' && t.phase !== 'cancelled',
    )
    if (!trip) return
    if (trip.phase === 'queued') {
      trip.phase = 'departing'
      return
    }
    const current = trip.pose
    const crossingSpeed =
      Math.abs(current.x - 37.4) < 2 && (Math.abs(current.z + 8.6) < 3 || Math.abs(current.z) < 4)
        ? 5
        : navigationLayout.truck_speed
    const length = trip.loop ? trafficLength : truckTripLength
    const distance = trip.distance + dt * crossingSpeed
    const next = trip.loop ? distance % length : Math.min(length, distance)
    const pose = trip.loop ? loopPose(next) : truckTripPose(next)
    trip.waiting = undefined
    const owner = `trip:${trip.id}`
    for (const zone of crossingZones) {
      const inside =
        pose.x >= zone.min[0] &&
        pose.x <= zone.max[0] &&
        pose.z >= zone.min[1] &&
        pose.z <= zone.max[1]
      if (inside && this.resources && !this.resources.reserveCrossing(zone.id, owner)) {
        trip.waiting = 'crossing'
        return
      }
    }
    // One active vehicle holds at most its current crossing.
    if (
      !crossingZones.some(
        (z) => pose.x >= z.min[0] && pose.x <= z.max[0] && pose.z >= z.min[1] && pose.z <= z.max[1],
      )
    )
      this.resources?.releaseCrossings(owner)
    trip.distance = next
    if (trip.loop && distance >= length) trip.laps = (trip.laps ?? 0) + 1
    trip.pose = pose
    trip.phase = next === 0 ? 'departing' : pose.arrived ? 'finished' : 'travelling'
    if (pose.arrived) this.resources?.releaseCrossings(owner)
  }
  trip(id: string) {
    return this.#trips.get(id)
  }
  view(): DeliverySimulationView {
    return {
      trips: [...this.#trips.values()].map((t) => ({ ...t, pose: { ...t.pose } })),
      overflow: this.#overflow,
    }
  }
}
function loopPose(distance: number) {
  return { ...trafficPose(distance), arrived: false, remaining: trafficLength - distance }
}
