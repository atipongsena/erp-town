import { useFrame, useThree } from '@react-three/fiber'
import { useEffect, useRef } from 'react'
import type { Group } from 'three'
import type { Department } from './world-view.ts'

export type Handoff = { from: Department; to: Department }
// Sample road-side checkpoints in the existing town's glTF coordinates.
const checkpoints: Record<Department, [number, number]> = {
  shop: [-22, -3],
  warehouse: [14, 22],
  bank: [12, -3],
  hq: [-5, -30],
  it: [42, -3],
  post: [-32, -3],
}
export function DemoHandoff({ route, animated }: { route: Handoff; animated: boolean }) {
  const marker = useRef<Group>(null)
  const elapsed = useRef(0)
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    elapsed.current = 0
    invalidate()
  }, [invalidate])
  useFrame((_state, delta) => {
    if (!marker.current) return
    if (animated && document.visibilityState !== 'hidden') {
      elapsed.current += Math.min(delta, 0.05)
    }
    const [x0, z0] = checkpoints[route.from]
    const [x1, z1] = checkpoints[route.to]
    const progress = (elapsed.current % 6) / 6
    // Turn at a checkpoint rather than claiming a physical delivery route.
    const x = progress < 0.5 ? x0 + (x1 - x0) * progress * 2 : x1
    const z = progress < 0.5 ? z0 : z0 + (z1 - z0) * (progress - 0.5) * 2
    marker.current.position.set(x, 3.2 + Math.sin(progress * Math.PI) * 1.2, z)
    marker.current.rotation.y = progress * Math.PI * 2
  })
  return (
    <group ref={marker} name="DemoDocumentHandoff">
      <mesh>
        <boxGeometry args={[2.7, 0.4, 1.9]} />
        <meshStandardMaterial color="#fff5ca" />
      </mesh>
      <mesh position={[0, 0.23, 0]} rotation={[-Math.PI / 2, 0, 0]}>
        <ringGeometry args={[0.42, 0.52, 4]} />
        <meshBasicMaterial color="#b98637" />
      </mesh>
      <mesh position={[0, -0.65, 0]} rotation={[-Math.PI / 2, 0, 0]}>
        <ringGeometry args={[1.65, 1.9, 24]} />
        <meshBasicMaterial color="#ffe393" transparent opacity={0.65} />
      </mesh>
    </group>
  )
}
