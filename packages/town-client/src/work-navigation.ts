import layout from '../../../go/internal/livedemo/work-layout.json'
import { trafficLength, trafficPose } from './traffic.ts'
import type { Department } from './world-view.ts'
export const navigationLayout = layout
export const crossingZones = layout.zones as {
  id: string
  min: [number, number]
  max: [number, number]
}[]

export type WorkPosition = [number, number, number]
export const workStations = layout.stations.human as Record<Department, WorkPosition>
const agentStations = layout.stations.agent as Record<Department, WorkPosition>
export function actorStation(
  department: Department,
  kind: 'human' | 'agent' = 'human',
): WorkPosition {
  return [...(kind === 'agent' ? agentStations : workStations)[department]]
}

const nodes = layout.nodes as WorkPosition[]
const edges = layout.edges as [number, number][]
const distance = (a: WorkPosition, b: WorkPosition) =>
  Math.hypot(b[0] - a[0], b[1] - a[1], b[2] - a[2])
export function at<T>(array: readonly T[], index: number): T {
  const value = array[index]
  if (value === undefined) throw new Error(`Invalid route graph index ${index}`)
  return value
}
function projection(p: WorkPosition, a: WorkPosition, b: WorkPosition): WorkPosition {
  const lengthSquared = distance(a, b) ** 2
  const t = Math.max(
    0,
    Math.min(
      1,
      ((p[0] - a[0]) * (b[0] - a[0]) +
        (p[1] - a[1]) * (b[1] - a[1]) +
        (p[2] - a[2]) * (b[2] - a[2])) /
        lengthSquared,
    ),
  )
  return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t]
}

/** Retargeting starts at the actor's current position, never at a fresh spawn. */
export function pedestrianRoute(
  from: WorkPosition,
  department: Department,
  kind: 'human' | 'agent' = 'human',
): WorkPosition[] {
  const destination = actorStation(department, kind)
  if (distance(from, destination) < 0.000001) return [[...from]]
  let nearest = at(edges, 0)
  let snapped = projection(from, at(nodes, nearest[0]), at(nodes, nearest[1]))
  for (const edge of edges) {
    const candidate = projection(from, at(nodes, edge[0]), at(nodes, edge[1]))
    if (distance(from, candidate) < distance(from, snapped)) {
      nearest = edge
      snapped = candidate
    }
  }
  const points = [...nodes, snapped]
  const start = points.length - 1
  const goal = nodes.findIndex((p) => distance(p, destination) < 0.000001)
  const neighbours = points.map(() => [] as number[])
  const connected: [number, number][] = [...edges, [start, nearest[0]], [start, nearest[1]]]
  for (const [a, b] of connected) {
    at(neighbours, a).push(b)
    at(neighbours, b).push(a)
  }
  const costs = points.map(() => Number.POSITIVE_INFINITY)
  const previous = points.map(() => -1)
  const open = new Set(points.map((_, i) => i))
  costs[start] = 0
  while (open.size) {
    const current = [...open].reduce((a, b) => (at(costs, a) <= at(costs, b) ? a : b))
    if (current === goal) break
    open.delete(current)
    for (const next of at(neighbours, current)) {
      const cost = at(costs, current) + distance(at(points, current), at(points, next))
      if (cost < at(costs, next)) {
        costs[next] = cost
        previous[next] = current
      }
    }
  }
  const path: WorkPosition[] = []
  for (let current = goal; current !== -1; current = at(previous, current))
    path.unshift([...at(points, current)])
  if (distance(from, at(path, 0)) > 0.000001) path.unshift([...from])
  else path[0] = [...from]
  return path.filter((p, i) => !i || distance(p, at(path, i - 1)) > 0.000001)
}

export function routeLength(path: readonly WorkPosition[]): number {
  return path.reduce((sum, p, i) => sum + (i ? distance(at(path, i - 1), p) : 0), 0)
}

export function advanceRoute(
  path: readonly WorkPosition[],
  travelled: number,
): {
  position: WorkPosition
  yaw: number
  arrived: boolean
  remaining: number
} {
  if (!path.length) throw new Error('A work route needs at least one position')
  const length = routeLength(path)
  let remaining = Math.max(0, Number.isFinite(travelled) ? travelled : 0)
  const arrived = remaining >= length
  const totalRemaining = Math.max(0, length - remaining)
  for (let i = 1; i < path.length; i++) {
    const a = at(path, i - 1)
    const b = at(path, i)
    const segment = distance(a, b)
    if (remaining <= segment && segment > 0) {
      const t = remaining / segment
      return {
        position: [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t],
        yaw: Math.atan2(b[0] - a[0], b[2] - a[2]),
        arrived,
        remaining: totalRemaining,
      }
    }
    remaining -= segment
  }
  const last = at(path, path.length - 1)
  const prior = path.at(-2) ?? last
  return {
    position: [...last],
    yaw: Math.atan2(last[0] - prior[0], last[2] - prior[2]),
    arrived: true,
    remaining: 0,
  }
}

// One delivery handoff starts on EastSt beside the warehouse. No inferred GPS,
// dock exit, or endless ambient loop: clamp at the outbound bridge destination.
const startDistance = 52.8 + Math.PI * 4 + 37.4
const exitRadius = 4
const exitZ = -2.25
const exitStartZ = exitZ - exitRadius
const loopDistance = trafficLength + (52.8 + Math.PI * 4 + exitStartZ + 17.4) - startDistance
const exitArcLength = (exitRadius * Math.PI) / 2
const exitX = 37.4 + exitRadius
export const truckTripLength = loopDistance + exitArcLength + 76 - exitX
export function truckTripPose(travelled: number): {
  x: number
  z: number
  yaw: number
  arrived: boolean
  remaining: number
} {
  const d = Math.max(0, Math.min(truckTripLength, Number.isFinite(travelled) ? travelled : 0))
  const state = { arrived: d >= truckTripLength, remaining: truckTripLength - d }
  if (d <= loopDistance) return { ...trafficPose(startDistance + d), ...state }
  if (d <= loopDistance + exitArcLength) {
    const yaw = (d - loopDistance) / exitRadius
    return {
      x: exitX - exitRadius * Math.cos(yaw),
      z: exitStartZ + exitRadius * Math.sin(yaw),
      yaw,
      ...state,
    }
  }
  return { x: exitX + d - loopDistance - exitArcLength, z: exitZ, yaw: Math.PI / 2, ...state }
}
export function truckTripPath(): WorkPosition[] {
  const path: WorkPosition[] = []
  for (let d = 0; d < truckTripLength; d += 1) {
    const p = truckTripPose(d)
    path.push([p.x, 0.06, p.z])
  }
  const p = truckTripPose(truckTripLength)
  path.push([p.x, 0.06, p.z])
  return path
}
