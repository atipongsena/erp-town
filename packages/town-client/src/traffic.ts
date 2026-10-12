// Matches art-direction/assets/art/scripts/build_greybox.py ROADS after Blender Y -> -Z.
// Thai left-hand traffic follows the outer lane clockwise. Each bend is a tangent arc.
const west = -31.4
const east = 37.4
const north = -25.4
const south = 33.4
const radius = 8
const horizontal = east - west - radius * 2
const vertical = south - north - radius * 2
const bend = (Math.PI * radius) / 2
export const trafficLength = 2 * (horizontal + vertical) + 4 * bend
type Segment = { length: number; x: number; z: number; yaw: number; turn?: boolean }
const segments: Segment[] = [
  { length: horizontal, x: west + radius, z: north, yaw: Math.PI / 2 },
  { length: bend, x: east - radius, z: north + radius, yaw: Math.PI / 2, turn: true },
  { length: vertical, x: east, z: north + radius, yaw: 0 },
  { length: bend, x: east - radius, z: south - radius, yaw: 0, turn: true },
  { length: horizontal, x: east - radius, z: south, yaw: -Math.PI / 2 },
  { length: bend, x: west + radius, z: south - radius, yaw: -Math.PI / 2, turn: true },
  { length: vertical, x: west, z: south - radius, yaw: -Math.PI },
  { length: bend, x: west + radius, z: north + radius, yaw: -Math.PI, turn: true },
]
export function trafficPose(distance: number): { x: number; z: number; yaw: number } {
  let remaining = ((distance % trafficLength) + trafficLength) % trafficLength
  for (const s of segments) {
    if (remaining <= s.length) {
      if (s.turn) {
        const yaw = s.yaw - remaining / radius
        return { x: s.x + radius * Math.cos(yaw), z: s.z - radius * Math.sin(yaw), yaw }
      }
      return {
        x: s.x + remaining * Math.sin(s.yaw),
        z: s.z + remaining * Math.cos(s.yaw),
        yaw: s.yaw,
      }
    }
    remaining -= s.length
  }
  return trafficPose(0)
}
export function carriagewayContains(x: number, z: number): boolean {
  return (
    (x >= -33.5 && x <= 39.5 && (Math.abs(z + 24) <= 3.5 || Math.abs(z - 32) <= 3.5)) ||
    (z >= -27.5 && z <= 35.5 && (Math.abs(x + 30) <= 3.5 || Math.abs(x - 36) <= 3.5)) ||
    (x >= -33.5 && x <= 92 && Math.abs(z) <= 4.5 && Math.hypot(x, z) >= 5.5) ||
    (Math.abs(x) <= 4.5 && z >= -27.5 && z <= 35.5 && Math.hypot(x, z) >= 5.5) ||
    (Math.hypot(x, z) >= 5.5 && Math.hypot(x, z) <= 12)
  )
}
