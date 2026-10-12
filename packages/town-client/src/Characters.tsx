import { Html, useGLTF } from '@react-three/drei'
import { useFrame, useThree } from '@react-three/fiber'
import { type ReactNode, useEffect, useMemo, useRef } from 'react'
import {
  type AnimationAction,
  AnimationMixer,
  type Group,
  type Material,
  Mesh,
  MeshStandardMaterial,
  type Skeleton,
  SkinnedMesh,
} from 'three'
import { clone } from 'three/addons/utils/SkeletonUtils.js'
import type { Presence } from './avatar.ts'
import type { Language } from './copy.ts'
import { useWorkClock } from './work-clock.ts'
import type { Department, DisplayFact } from './world-view.ts'

// Forecourts, outside the roads and the warehouse turning reservation.
const stations: Record<Department, [number, number, number]> = {
  shop: [-21, 0.12, -8.2],
  warehouse: [24.55, 0.96, 18.6],
  bank: [21, 0.12, -7.7],
  hq: [7, 0.12, -35.9],
  it: [44, 0.12, -7.8],
  post: [-35.7, 0.12, -11],
}
export function Character({
  model,
  emote,
  tint,
  animated = false,
  walking = false,
}: {
  model: string
  emote: string
  tint?: string
  animated?: boolean
  walking?: boolean
}) {
  const gltf = useGLTF(`${import.meta.env.BASE_URL}art/${model}.glb`)
  const { root, materials, skeletons } = useMemo(() => {
    const root = clone(gltf.scene)
    const materials = new Map<Material, Material>()
    const skeletons = new Set<Skeleton>()
    const material = (source: Material) => {
      if (!tint) return source
      let result = materials.get(source)
      if (!result) {
        result = source.clone()
        if (result instanceof MeshStandardMaterial && result.name === 'M_wall')
          result.color.set(tint)
        materials.set(source, result)
      }
      return result
    }
    root.traverse((object) => {
      if (object instanceof SkinnedMesh) skeletons.add(object.skeleton)
      if (object instanceof Mesh) {
        object.castShadow = false
        object.receiveShadow = true
        object.material = Array.isArray(object.material)
          ? object.material.map(material)
          : material(object.material)
      }
    })
    return { root, materials, skeletons }
  }, [gltf.scene, tint])
  const invalidate = useThree((s) => s.invalidate)
  const mixer = useMemo(() => new AnimationMixer(root), [root])
  const previousAction = useRef<AnimationAction | undefined>(undefined)
  const blending = useRef(0)
  const clock = useWorkClock(animated)
  useEffect(
    () => () => {
      // SkeletonUtils creates owned skeletons; cached geometry/materials stay shared.
      for (const skeleton of skeletons) skeleton.dispose()
      for (const material of materials.values()) material.dispose()
    },
    [materials, skeletons],
  )
  useEffect(() => {
    const name =
      animated && walking
        ? 'walk'
        : emote === 'wave'
          ? 'raise_hand'
          : emote === 'type'
            ? 'sit_type'
            : emote
    const clip =
      gltf.animations.find((a) => a.name === name) ?? gltf.animations.find((a) => a.name === 'idle')
    if (clip) {
      const action = mixer.clipAction(clip)
      if (previousAction.current !== action) {
        if (animated && previousAction.current) {
          previousAction.current.fadeOut(0.15)
          action.reset().fadeIn(0.15).play()
          blending.current = 0.15
        } else {
          mixer.stopAllAction()
          action.reset().play()
          mixer.setTime(emote === 'idle' ? 0.4 : 0.8)
        }
        previousAction.current = action
      }
    }
    invalidate()
  }, [mixer, gltf.animations, emote, invalidate, animated, walking])
  useEffect(
    () => () => {
      mixer.stopAllAction()
      mixer.uncacheRoot(root)
    },
    [mixer, root],
  )
  useFrame(() => {
    if ((!animated && blending.current <= 0) || document.visibilityState === 'hidden') return
    const delta = clock.step(performance.now(), true)
    mixer.update(delta)
    if (blending.current > 0) {
      blending.current -= delta
      invalidate()
    }
  })
  return <primitive object={root} dispose={null} />
}
function ActorMotion({
  animated,
  robot = false,
  phase,
  children,
}: {
  animated: boolean
  robot?: boolean
  phase: number
  children: ReactNode
}) {
  const group = useRef<Group>(null)
  const time = useRef(phase)
  useEffect(() => {
    if (!animated && group.current) {
      group.current.position.set(0, 0, 0)
      group.current.rotation.y = 0
    }
  }, [animated])
  useFrame((_state, delta) => {
    if (!animated || !group.current || document.visibilityState === 'hidden') return
    time.current += Math.min(delta, 0.05)
    const t = time.current * 0.7
    if (robot) {
      group.current.position.y = Math.sin(t * 2) * 0.16
      group.current.rotation.y = Math.sin(t) * 0.25
    } else {
      // A small patrol along the department forecourt, never a claimed live location.
      group.current.position.x = Math.sin(t) * 0.22
      group.current.position.z = Math.cos(t) * 0.12
      group.current.rotation.y = Math.atan2(Math.cos(t) * 0.22, -Math.sin(t) * 0.12)
    }
  })
  return <group ref={group}>{children}</group>
}
export function Characters({
  presences,
  aiCount,
  animated = false,
  language,
  crewFacts,
  onSelect,
}: {
  presences: Presence[]
  aiCount: number
  animated?: boolean
  language: Language
  crewFacts?: DisplayFact[]
  onSelect: (department: Department) => void
}) {
  // Coarse department positions only. Opaque ids identify a live entry, never a
  // path or person. Bound the visible population to preserve renderer budgets.
  const humans = presences.slice(0, 2)
  return (
    <group name="TownCharacters">
      {humans.map((p, index) => {
        const station: [number, number, number] =
          p.department === 'warehouse' ? [11.5, 0.12, 18.5] : stations[p.department]
        return (
          <group
            key={p.id}
            position={[station[0] + 2.5 + index * 1.3, station[1], station[2]]}
            scale={1.8}
          >
            <ActorMotion animated={animated} phase={index * 3}>
              <Character
                model={p.appearance === 'human-coral' ? 'town-human-02' : 'town-human-01'}
                tint={
                  { 'human-blue': '#87afd7', 'human-coral': '#e8afa0', 'human-green': '#97b98e' }[
                    p.appearance
                  ]
                }
                emote={p.emote}
                animated={animated}
                walking
              />
              <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -0.08, 0]}>
                <circleGeometry args={[0.48, 24]} />
                <meshBasicMaterial color="#52675f" transparent opacity={0.18} depthWrite={false} />
              </mesh>
            </ActorMotion>
          </group>
        )
      })}
      {crewFacts !== undefined &&
        (['shop', 'warehouse', 'bank'] as const).map((department, index) => {
          const count = crewFacts.filter(
            (f) => f.department === department && f.status !== 'cancelled',
          ).length
          const th = language === 'th'
          const title = {
            shop: th ? 'ฝ่ายขาย' : 'Sales crew',
            warehouse: th ? 'ทีมคลัง' : 'Warehouse crew',
            bank: th ? 'บัญชี' : 'Accounts crew',
          }[department]
          const position = stations[department]
          return (
            <group key={department} name={`DepartmentCrew-${department}`} position={position}>
              <group scale={1.8}>
                <Character
                  model={index === 1 ? 'town-human-02' : 'town-human-01'}
                  emote={count ? (department === 'warehouse' ? 'carry' : 'read') : 'idle'}
                  animated={animated}
                />
              </group>
              <mesh position={[0, 0.03, 0]} rotation={[-Math.PI / 2, 0, 0]}>
                <circleGeometry args={[1.05, 24]} />
                <meshBasicMaterial color="#fff3d7" transparent opacity={0.5} depthWrite={false} />
              </mesh>
              {department === 'warehouse' ? (
                <mesh position={[-1.2, 0.4, 0]}>
                  <boxGeometry args={[0.65, 0.7, 0.65]} />
                  <meshStandardMaterial color="#c2a366" />
                </mesh>
              ) : (
                <mesh position={[-1.2, 0.8, 0]}>
                  <boxGeometry args={[0.65, 1.5, 0.7]} />
                  <meshStandardMaterial color={department === 'shop' ? '#e8afa0' : '#79b9ac'} />
                </mesh>
              )}
              {/* Keep the dock label to the right of its worker, clear of the truck cab. */}
              <Html
                position={[0, 4.5, 0]}
                center={department !== 'warehouse'}
                zIndexRange={[20, 0]}
              >
                <button
                  type="button"
                  className="town-actor-label"
                  onClick={() => onSelect(department)}
                >
                  {title} ·{' '}
                  {count ? (th ? `งาน ${count}` : `${count} records`) : th ? 'พร้อมรับงาน' : 'ready'}
                </button>
              </Html>
            </group>
          )
        })}
      {[
        { id: 'ai-west', x: 43 },
        { id: 'ai-east', x: 46 },
      ]
        .slice(0, crewFacts !== undefined ? 1 : Math.min(1, Math.max(0, aiCount)))
        .map((slot) => (
          <group key={slot.id} position={[44, 0.35, -7.8]}>
            <group scale={1.8}>
              <ActorMotion animated={animated} robot phase={0}>
                <Character
                  model="town-agent-hq"
                  emote={aiCount > 0 ? 'wave' : 'idle'}
                  animated={animated}
                />
                <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -0.5, 0]}>
                  <ringGeometry args={[0.65, 0.78, 24]} />
                  <meshBasicMaterial color="#cfb2ec" />
                </mesh>
              </ActorMotion>
            </group>
            {crewFacts !== undefined && (
              <Html position={[0, 4.5, 0]} center zIndexRange={[20, 0]}>
                <button
                  type="button"
                  className="town-actor-label agent"
                  onClick={() => onSelect('it')}
                >
                  {aiCount > 0
                    ? language === 'th'
                      ? 'AI · กำลังทำงาน'
                      : 'AI · running'
                    : language === 'th'
                      ? 'AI ในเครื่อง · พร้อมทดลอง'
                      : 'Local AI · ready'}
                </button>
              </Html>
            )}
          </group>
        ))}
    </group>
  )
}
