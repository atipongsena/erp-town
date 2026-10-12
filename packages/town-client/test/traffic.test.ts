import { describe, expect, it } from 'vitest'
import { carriagewayContains, trafficLength, trafficPose } from '../src/traffic.ts'

describe('traffic on the exported Thai street plan', () => {
  it('keeps the entire truck footprint on the carriageway, including every turn', () => {
    // Use the actual mesh bounds, including bumpers/wheels, rather than the kit's body size.
    const bytes = readFileSync(new URL('../public/art/town-truck.glb', import.meta.url))
    const model = JSON.parse(bytes.subarray(20, 20 + bytes.readUInt32LE(12)).toString()) as {
      meshes: { primitives: { attributes: { POSITION: number } }[] }[]
      accessors: { min: number[]; max: number[] }[]
    }
    const bounds = model.meshes.flatMap((m) =>
      m.primitives.map((p) => model.accessors[p.attributes.POSITION]),
    )
    const minX = Math.min(...bounds.map((b) => b?.min[0] ?? 0)) - 0.05
    const maxX = Math.max(...bounds.map((b) => b?.max[0] ?? 0)) + 0.05
    const minZ = Math.min(...bounds.map((b) => b?.min[2] ?? 0)) - 0.05
    const maxZ = Math.max(...bounds.map((b) => b?.max[2] ?? 0)) + 0.05
    for (let distance = 0; distance < trafficLength; distance += 0.1) {
      const pose = trafficPose(distance)
      for (const lateral of [minX, 0, maxX]) {
        for (const forward of [minZ, 0, maxZ]) {
          const x = pose.x + Math.cos(pose.yaw) * lateral + Math.sin(pose.yaw) * forward
          const z = pose.z - Math.sin(pose.yaw) * lateral + Math.cos(pose.yaw) * forward
          expect(carriagewayContains(x, z), `truck at ${distance}: ${x},${z}`).toBe(true)
        }
      }
    }
  })
  it('uses the left lane and remains continuous across turns and the loop seam', () => {
    expect(trafficPose(10)).toMatchObject({ z: -25.4, yaw: Math.PI / 2 })
    for (let distance = 0; distance <= trafficLength; distance += 0.05) {
      const a = trafficPose(distance)
      const b = trafficPose(distance + 0.05)
      expect(Math.hypot(a.x - b.x, a.z - b.z)).toBeLessThanOrEqual(0.05001)
      expect(Math.abs(Math.atan2(Math.sin(a.yaw - b.yaw), Math.cos(a.yaw - b.yaw)))).toBeLessThan(
        0.013,
      )
    }
    expect(trafficPose(trafficLength)).toEqual(trafficPose(0))
    expect(trafficPose(-10)).toEqual(trafficPose(trafficLength - 10))
  })
})

import { readFileSync } from 'node:fs'
