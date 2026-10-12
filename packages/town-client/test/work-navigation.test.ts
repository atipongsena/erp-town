import { describe, expect, it } from 'vitest'
import layout from '../../../go/internal/livedemo/work-layout.json'
import { carriagewayContains } from '../src/traffic.ts'
import {
  actorStation,
  advanceRoute,
  pedestrianRoute,
  routeLength,
  truckTripLength,
  truckTripPose,
  workStations,
} from '../src/work-navigation.ts'

// Independent canonical kit footprints in exported X/Z coordinates.
const obstacles: [number, number, number, number][] = [
  [-22, -17.5, -11, -9.5], // shop
  [12.3, 8, 26.3, 19.4], // warehouse including raised loading dock
  [12.65, -17.8, 23.35, -8], // bank including entry steps
  [-10, -50, 10, -38], // castle
  [44.5, -15.5, 51.5, -9.5], // IT ground floor
  [-45.2, -15, -37, -7], // post
  [24.8, -15.2, 29.6, -9.2], // cafe
  [-13.6, 12.7, -7.6, 25.3], // park shophouses
  [26.9, 11.1, 29.6, 16.3], // warehouse crates/pallets
  [28.8, 7, 29.8, 11], // forklift
  [26.8, 17.8, 29.5, 18.9], // dock warning strip
  [7.7, -34.25, 9.3, -33.75], // east civic plaza bench
  [-37.15, -14.93, -36.94, -14.5], // post letterbox
]

describe('work journeys on the canonical town plan', () => {
  it('matches the shared server route distances and millisecond deadlines', () => {
    for (const row of layout.parity) {
      const kind = row.kind as 'human' | 'agent'
      const path = pedestrianRoute(
        actorStation(row.from as keyof typeof workStations, kind),
        row.to as keyof typeof workStations,
        kind,
      )
      expect(routeLength(path)).toBeCloseTo(row.length, 6)
      expect(Math.ceil((routeLength(path) / layout.walk_speed) * 1000)).toBe(row.duration_ms)
    }
  })
  it('separates agent workstations from staff and keeps their complete desks off the road', () => {
    for (const department of Object.keys(workStations) as (keyof typeof workStations)[]) {
      const human = actorStation(department, 'human')
      const agent = actorStation(department, 'agent')
      expect(human).toEqual(workStations[department])
      expect(Math.hypot(agent[0] - human[0], agent[2] - human[2])).toBeGreaterThanOrEqual(3.5)
      // WorkCharacters desk and chair scaled by 2.2, facing +Z while working.
      for (const dx of [-1.21, 0, 1.21]) {
        for (const dz of [-0.814, 0, 2.145]) {
          expect(carriagewayContains(agent[0] + dx, agent[2] + dz), `${department} desk`).toBe(
            false,
          )
        }
      }
      for (const [x0, z0, x1, z1] of obstacles) {
        expect(
          agent[0] - 1.21 < x1 &&
            agent[0] + 1.21 > x0 &&
            agent[2] - 0.814 < z1 &&
            agent[2] + 2.145 > z0,
          `${department} desk obstacle`,
        ).toBe(false)
      }
      const route = pedestrianRoute(workStations.it, department, 'agent')
      expect(route.at(-1)).toEqual(agent)
      expect(advanceRoute(route, routeLength(route) + 5)).toMatchObject({
        position: agent,
        arrived: true,
      })
    }
  })
  it('reaches every department along paths clear of buildings and the roundabout', () => {
    const departments = Object.keys(workStations) as (keyof typeof workStations)[]
    const stations = [
      ...Object.values(workStations),
      ...departments.map((d) => actorStation(d, 'agent')),
    ]
    for (const from of stations) {
      for (const kind of ['human', 'agent'] as const) {
        for (const department of Object.keys(workStations) as (keyof typeof workStations)[]) {
          const route = pedestrianRoute(from, department, kind)
          const length = routeLength(route)
          expect(route[0]).toEqual(from)
          expect(route.at(-1)).toEqual(actorStation(department, kind))
          for (let distance = 0; distance <= length; distance += 0.25) {
            const {
              position: [x, , z],
            } = advanceRoute(route, distance)
            expect(Math.hypot(x, z), `${department} circle at ${x},${z}`).toBeGreaterThan(12)
            expect(Math.hypot(x, z + 33.6)).toBeGreaterThan(3.4) // fountain + actor clearance
            for (const [x0, z0, x1, z1] of obstacles) {
              expect(
                x > x0 - 0.5 && x < x1 + 0.5 && z > z0 - 0.5 && z < z1 + 0.5,
                `${department} obstacle ${x},${z}`,
              ).toBe(false)
            }
          }
        }
      }
    }
  })

  it('advances once, clamps arrival, and changes destinations without teleporting', () => {
    const first = pedestrianRoute(workStations.shop, 'warehouse')
    const middle = advanceRoute(first, routeLength(first) * 0.47)
    const retargeted = pedestrianRoute(middle.position, 'hq')
    expect(advanceRoute(retargeted, 0).position).toEqual(middle.position)
    expect(advanceRoute(first, -100).position).toEqual(workStations.shop)
    expect(advanceRoute(first, routeLength(first) + 100)).toMatchObject({
      position: workStations.warehouse,
      arrived: true,
      remaining: 0,
    })
    for (let distance = 0; distance < routeLength(retargeted); distance += 0.1) {
      const a = advanceRoute(retargeted, distance).position
      const b = advanceRoute(retargeted, distance + 0.1).position
      expect(Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2])).toBeLessThanOrEqual(0.100001)
    }
  })

  it('makes one finite truck trip to the gate without driving off the road', () => {
    expect(truckTripPose(0).x).toBeCloseTo(37.4)
    expect(truckTripPose(0).z).toBeCloseTo(20)
    expect(truckTripPose(truckTripLength + 100)).toEqual(truckTripPose(truckTripLength))
    expect(truckTripPose(truckTripLength).x).toBeCloseTo(76)
    // Actual vehicle bounds, including mirrors/wheels, + 5 cm clearance.
    for (let distance = 0; distance <= truckTripLength; distance += 0.1) {
      const pose = truckTripPose(distance)
      for (const x of [-1.43, 0, 1.32]) {
        for (const z of [-3.34, 0, 3.35]) {
          const wx = pose.x + Math.cos(pose.yaw) * x + Math.sin(pose.yaw) * z
          const wz = pose.z - Math.sin(pose.yaw) * x + Math.cos(pose.yaw) * z
          expect(carriagewayContains(wx, wz), `trip footprint ${distance}: ${wx},${wz}`).toBe(true)
        }
      }
      const next = truckTripPose(distance + 0.1)
      expect(Math.hypot(pose.x - next.x, pose.z - next.z)).toBeLessThanOrEqual(0.100001)
    }
  })
})
