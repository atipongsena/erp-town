import { Html, Line } from '@react-three/drei'
import { type ThreeEvent, useFrame, useThree } from '@react-three/fiber'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Group } from 'three'
import { Character } from './Characters.tsx'
import { departmentNames, type Language } from './copy.ts'
import { DepartmentScreen } from './DepartmentScreen.tsx'
import type { DashboardView } from './dashboard-view.ts'
import type { WorkActor } from './live-work.ts'
import { actorStation } from './work-navigation.ts'
import { WorkSimulation } from './work-simulation.ts'
import type { Department } from './world-view.ts'

export function WorkCharacters({
  actors,
  animated,
  language,
  onSelect,
  simulation,
  onSelectActor,
  dashboard,
}: {
  actors: WorkActor[]
  simulation?: WorkSimulation
  dashboard?: DashboardView
  onSelectActor?: (id: string) => void
  animated: boolean
  language: Language
  onSelect: (department: Department) => void
}) {
  const owned = useMemo(() => new WorkSimulation(), [])
  const runtime = simulation ?? owned
  useEffect(() => {
    if (!simulation) runtime.observe(actors, 'scene', Date.now())
  }, [actors, runtime, simulation])
  return (
    <group name="LiveWorkActors">
      {actors.slice(0, 16).map((actor) => (
        <MovingActor
          key={actor.id}
          actor={actor}
          simulation={runtime}
          onSelectActor={onSelectActor}
          dashboard={dashboard}
          animated={animated}
          language={language}
          onSelect={onSelect}
        />
      ))}
    </group>
  )
}
function MovingActor({
  actor,
  animated,
  language,
  onSelect,
  simulation,
  onSelectActor,
  dashboard,
}: {
  actor: WorkActor
  simulation: WorkSimulation
  dashboard?: DashboardView
  onSelectActor?: (id: string) => void
  animated: boolean
  language: Language
  onSelect: (department: Department) => void
}) {
  const root = useRef<Group>(null)
  const initial = useRef(actorStation(actor.from_department ?? actor.department, actor.kind))
  const [pose, setPose] = useState(() => simulation?.actor(actor.id))
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    const update = () => {
      if (actor.simulation === 'loop' && (!animated || document.visibilityState === 'hidden'))
        return
      simulation?.step(Date.now())
      const current = simulation.actor(actor.id)
      setPose(current ? { ...current } : undefined)
      invalidate()
    }
    update()
    const timer = actor.activity !== 'idle' ? setInterval(update, 250) : undefined
    return () => clearInterval(timer)
  }, [actor, simulation, invalidate, animated])
  useFrame(() => {
    if (!root.current) return
    if (actor.simulation === 'loop' && (!animated || document.visibilityState === 'hidden')) return
    simulation?.step(Date.now())
    const current = simulation?.actor(actor.id)
    if (!current) return
    const position = animated ? current.position : actorStation(actor.department, actor.kind)
    root.current.position.set(...position)
    root.current.rotation.y = current.yaw
  })
  const moving = pose?.phase === 'travelling'
  const route = pose?.route ?? []
  const th = language === 'th'
  const active = actor.activity !== 'idle'
  const working = pose?.phase === 'working'
  const label =
    actor.kind === 'agent'
      ? 'AI'
      : actor.id === 'self'
        ? th
          ? 'คุณ'
          : 'You'
        : th
          ? 'พนักงาน'
          : 'Staff'
  const status =
    pose?.phase === 'queued'
      ? th
        ? 'รอพื้นที่แสดงงาน'
        : 'Waiting for scene resource'
      : moving
        ? th
          ? 'กำลังเดินทาง'
          : 'Travelling'
        : working
          ? th
            ? 'กำลังทำงาน'
            : 'Working'
          : actor.activity === 'travelling'
            ? th
              ? 'ถึงแผนกแล้ว'
              : 'At department'
            : th
              ? 'ว่าง'
              : 'Idle'
  const emote =
    actor.activity === 'reading' ? 'read' : actor.department === 'warehouse' ? 'carry' : 'type'
  return (
    <>
      {moving && animated && route.length > 1 && (
        <Line
          points={route.map(([x, y, z]) => [x, y + 0.09, z])}
          color={actor.kind === 'agent' ? '#bca0d8' : '#dba857'}
          lineWidth={2}
          transparent
          opacity={0.7}
        />
      )}
      {/* biome-ignore lint/a11y/noStaticElementInteractions: Three.js group; the actor label and HUD queue provide keyboard controls. */}
      <group
        ref={root}
        name={`WorkActor-${actor.id}`}
        position={initial.current}
        onClick={(event: ThreeEvent<MouseEvent>) => {
          event.stopPropagation()
          onSelectActor ? onSelectActor(actor.id) : onSelect(actor.department)
        }}
      >
        <group scale={actor.kind === 'agent' ? 2.2 : 2.4}>
          {working && emote === 'type' && (
            <group name="ActiveWorkstation">
              <mesh position={[0, 0.77, 0.65]}>
                <boxGeometry args={[1.1, 0.08, 0.65]} />
                <meshStandardMaterial color="#d4bc98" />
              </mesh>
              {[-0.45, 0.45].map((x) => (
                <mesh key={x} position={[x, 0.37, 0.65]}>
                  <boxGeometry args={[0.06, 0.74, 0.5]} />
                  <meshStandardMaterial color="#48666c" />
                </mesh>
              ))}
              <mesh position={[0, 1.06, 0.85]}>
                <boxGeometry args={[0.58, 0.4, 0.05]} />
                <meshStandardMaterial color="#2c4952" />
              </mesh>
              {dashboard ? (
                <DepartmentScreen
                  view={dashboard}
                  department={actor.department}
                  language={language}
                  onSelect={onSelect}
                />
              ) : (
                <>
                  {' '}
                  <mesh position={[0, 1.06, 0.82]}>
                    <boxGeometry args={[0.48, 0.3, 0.015]} />
                    <meshStandardMaterial
                      color="#89c9b8"
                      emissive="#459784"
                      emissiveIntensity={0.3}
                    />
                  </mesh>
                </>
              )}
              <mesh position={[0, 0.83, 0.5]}>
                <boxGeometry args={[0.46, 0.02, 0.18]} />
                <meshStandardMaterial color="#f4efe5" />
              </mesh>
              <mesh position={[0, 0.48, 0.02]}>
                <boxGeometry args={[0.5, 0.07, 0.5]} />
                <meshStandardMaterial color="#476974" />
              </mesh>
              <mesh position={[0, 0.25, 0.02]}>
                <cylinderGeometry args={[0.055, 0.055, 0.5, 8]} />
                <meshStandardMaterial color="#476974" />
              </mesh>
            </group>
          )}
          <Character
            model={
              actor.kind === 'agent'
                ? 'town-agent-hq'
                : actor.appearance === 'human-coral'
                  ? 'town-human-02'
                  : 'town-human-01'
            }
            emote={working ? emote : 'idle'}
            animated={animated && (moving || working)}
            walking={moving}
          />
        </group>
        <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.01, 0]}>
          <ringGeometry args={[0.65, 0.8, 24]} />
          <meshBasicMaterial
            color={actor.kind === 'agent' ? '#bca0d8' : '#dba857'}
            transparent
            opacity={0.7}
          />
        </mesh>
        <Html position={[0, 5.7, 0]} center zIndexRange={active ? [30, 15] : [10, 0]}>
          <button
            type="button"
            className={`town-actor-label ${actor.kind === 'agent' ? 'agent' : ''}`}
            data-work-actor={actor.id}
            data-work-phase={
              pose?.phase === 'queued'
                ? 'queued'
                : moving
                  ? 'travelling'
                  : working
                    ? 'working'
                    : actor.activity === 'travelling'
                      ? 'arrived'
                      : 'idle'
            }
            data-work-department={actor.department}
            onClick={(event) => {
              event.stopPropagation()
              onSelectActor ? onSelectActor(actor.id) : onSelect(actor.department)
            }}
          >
            {label} · {departmentNames[language][actor.department]} · {status}
            {actor.simulation === 'loop' ? (th ? ' · ภาพสาธิต' : ' · Demo motion') : ''}
          </button>
        </Html>
      </group>
    </>
  )
}
