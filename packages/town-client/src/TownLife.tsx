import { Html, Line, useGLTF } from '@react-three/drei'
import { type ThreeEvent, useFrame, useThree } from '@react-three/fiber'
import { type ComponentRef, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { type Group, type Material, Mesh, type MeshBasicMaterial } from 'three'
import type { Language } from './copy.ts'
import { DeliverySimulation, type DeliveryTrip } from './delivery-simulation.ts'
import { adaptMaterial } from './materials.ts'
import { trafficLength, trafficPose } from './traffic.ts'
import { truckTripPath, truckTripPose } from './work-navigation.ts'

/** Recorded transport stays finite; explicitly marked demo traffic circulates. */
export function TownLife({
  animated,
  language,
  onSelectTruck,
  onSelectTrip,
  trips = [],
  onTripComplete,
  awake = true,
  simulation,
}: {
  animated: boolean
  language: Language
  onSelectTrip?: (id: string) => void
  onSelectTruck?: () => void
  trips?: DeliveryTrip[]
  onTripComplete?: (id: string) => void
  awake?: boolean
  simulation?: DeliverySimulation
}) {
  const owned = useMemo(() => new DeliverySimulation(), [])
  const runtime = simulation ?? owned
  useEffect(() => {
    if (!simulation) runtime.observe(trips, 'scene', Date.now())
  }, [trips, runtime, simulation])
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = () => {
      if (!animated || !awake || document.visibilityState === 'hidden') return
      invalidate()
      timer = setTimeout(tick, 1000 / 30)
    }
    const visibility = () => {
      clearTimeout(timer)
      tick()
    }
    tick()
    document.addEventListener('visibilitychange', visibility)
    return () => {
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [animated, awake, invalidate])
  return (
    <group name="TownLife">
      <Suspense fallback={null}>
        {trips.slice(0, 1).map((trip) => (
          <DeliveryTruck
            key={trip.id}
            trip={trip}
            simulation={runtime}
            animated={animated}
            language={language}
            onSelectTruck={onSelectTrip ? () => onSelectTrip(trip.id) : onSelectTruck}
            onComplete={onTripComplete}
          />
        ))}
        {!trips.some((t) => t.loop) && (
          <group name="DockTruck" position={[14.05, 0.06, 22.7]}>
            <VehicleModel model="truck" onSelect={onSelectTruck} />
            <TruckLabel language={language} onSelect={onSelectTruck} />
          </group>
        )}
      </Suspense>
      {[0, 1, 2].map((phase) => (
        <WaterRipple key={phase} phase={phase} animated={false} />
      ))}
    </group>
  )
}
function DeliveryTruck({
  animated,
  trip,
  language,
  onSelectTruck,
  onComplete,
  simulation,
}: {
  animated: boolean
  trip: DeliveryTrip
  simulation: DeliverySimulation
  language: Language
  onSelectTruck?: () => void
  onComplete?: (id: string) => void
}) {
  const root = useRef<Group>(null)
  const done = useRef(false)
  const route = useRef<ComponentRef<typeof Line>>(null)
  const [display, setDisplay] = useState(() => simulation.trip(trip.id))
  const path = useMemo(
    () =>
      trip.loop
        ? Array.from({ length: Math.ceil(trafficLength) + 1 }, (_, i): [number, number, number] => {
            const pose = trafficPose(Math.min(i, trafficLength))
            return [pose.x, 0.15, pose.z]
          })
        : truckTripPath().map(([x, y, z]): [number, number, number] => [x, y + 0.09, z]),
    [trip.loop],
  )
  const initial = trip.loop ? trafficPose(0) : truckTripPose(0)
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    invalidate()
    const timer = setInterval(() => {
      if (trip.loop && (!animated || document.visibilityState === 'hidden')) return
      const current = simulation.trip(trip.id)
      setDisplay(current ? { ...current } : undefined)
    }, 250)
    return () => clearInterval(timer)
  }, [invalidate, simulation, trip.id, trip.loop, animated])
  useFrame(() => {
    if (!root.current || done.current) return
    if (trip.loop && (!animated || document.visibilityState === 'hidden')) return
    simulation.step(Date.now())
    const state = simulation.trip(trip.id)
    if (!state) return
    if (route.current)
      route.current.visible =
        animated && !state.waiting && state.phase !== 'queued' && state.phase !== 'finished'
    const pose = state.pose
    root.current.position.set(pose.x, 0.06, pose.z)
    root.current.rotation.y = pose.yaw
    if (state.phase === 'finished') {
      done.current = true
      onComplete?.(trip.id)
    } else if (animated) invalidate()
  })
  return (
    <>
      {animated && !done.current && (
        <Line ref={route} points={path} color="#83acd6" lineWidth={2} transparent opacity={0.65} />
      )}
      <group
        ref={root}
        name={`DeliveryTrip-${trip.id}`}
        position={[initial.x, 0.06, initial.z]}
        rotation={[0, initial.yaw, 0]}
      >
        <VehicleModel model="truck" onSelect={onSelectTruck} />
        <Html position={[0, 4.1, 0]} center zIndexRange={[20, 0]}>
          <button
            type="button"
            className="town-actor-label truck"
            data-delivery-trip={trip.id}
            data-delivery-distance={display?.distance.toFixed(2)}
            data-delivery-laps={display?.laps}
            onClick={(event) => {
              event.stopPropagation()
              onSelectTruck?.()
            }}
          >
            {trip.loop
              ? language === 'th'
                ? 'รถสาธิต · วิ่งวน'
                : 'Demo truck · circulating'
              : `${trip.name} · ${language === 'th' ? 'บันทึกส่งของ' : 'Dispatch recorded'}`}
          </button>
        </Html>
      </group>
    </>
  )
}
function VehicleModel({ model, onSelect }: { model: string; onSelect?: () => void }) {
  const gltf = useGLTF(
    `${import.meta.env.BASE_URL}art/town-${model}.glb`,
    `${import.meta.env.BASE_URL}draco/`,
  )
  const { root, owned } = useMemo(() => {
    const root = gltf.scene.clone(true)
    const owned = new Map<Material, Material>()
    const adapt = (source: Material) => {
      if (owned.has(source)) return owned.get(source) ?? source
      const material = adaptMaterial(source)
      if (material !== source) owned.set(source, material)
      return material
    }
    root.traverse((object) => {
      if (object instanceof Mesh)
        object.material = Array.isArray(object.material)
          ? object.material.map(adapt)
          : adapt(object.material)
    })
    return { root, owned }
  }, [gltf.scene])
  useEffect(
    () => () => {
      for (const material of owned.values()) material.dispose()
    },
    [owned],
  )
  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: Three primitive; TruckLabel and the sidebar provide keyboard controls.
    <primitive
      object={root}
      dispose={null}
      onClick={
        onSelect
          ? (e: ThreeEvent<MouseEvent>) => {
              e.stopPropagation()
              onSelect()
            }
          : undefined
      }
    />
  )
}
function TruckLabel({ language, onSelect }: { language: Language; onSelect?: () => void }) {
  if (!onSelect) return null
  return (
    <Html position={[0, 4.1, 0]} center zIndexRange={[20, 0]}>
      <button
        type="button"
        className="town-actor-label truck"
        onClick={(event) => {
          event.stopPropagation()
          onSelect?.()
        }}
      >
        {language === 'th' ? 'รถบรรทุก · ดูงาน' : 'Truck · inspect'}
      </button>
    </Html>
  )
}
function WaterRipple({ animated, phase }: { animated: boolean; phase: number }) {
  const ring = useRef<Mesh>(null)
  const time = useRef(phase * 1.7)
  useFrame((_state, delta) => {
    if (!ring.current) return
    if (animated && document.visibilityState !== 'hidden') time.current += Math.min(delta, 0.05)
    const progress = (time.current % 5) / 5
    ring.current.scale.setScalar(1 + progress * 2)
    ;(ring.current.material as MeshBasicMaterial).opacity = Math.sin(progress * Math.PI) * 0.2
  })
  return (
    <mesh
      ref={ring}
      position={[-65 + phase * 3, 0.1, 48 + phase * 3]}
      rotation={[-Math.PI / 2, 0, 0]}
    >
      <ringGeometry args={[1.5, 1.6, 32]} />
      <meshBasicMaterial color="#d7f1f7" transparent depthWrite={false} />
    </mesh>
  )
}
