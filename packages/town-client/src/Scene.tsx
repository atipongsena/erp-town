import { OrbitControls, useGLTF } from '@react-three/drei'
import { Canvas, type ThreeEvent, useFrame, useThree } from '@react-three/fiber'
import {
  Component,
  type ComponentRef,
  type ReactNode,
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { AgXToneMapping, AnimationMixer, type Material, Mesh, PCFShadowMap, Vector3 } from 'three'
import type { Presence } from './avatar.ts'
import { Characters } from './Characters.tsx'
import { copy, type Language } from './copy.ts'
import { DemoHandoff, type Handoff } from './DemoHandoff.tsx'
import type { DashboardView, TownSelection } from './dashboard-view.ts'
import type { DeliverySimulation } from './delivery-simulation.ts'
import layout from './layout.json'
import type { WorkActor } from './live-work.ts'
import { adaptMaterial, castsTownShadow } from './materials.ts'
import { type SceneTime, sceneEnvironments } from './scene-environment.ts'
import { TownLife } from './TownLife.tsx'
import tokens from './tokens.json'
import { WorkCharacters } from './WorkCharacters.tsx'
import { useWorkClock } from './work-clock.ts'
import type { WorkSimulation } from './work-simulation.ts'
import type { Department, DisplayFact } from './world-view.ts'

const departments: Record<string, Department> = {
  Shop: 'shop',
  Warehouse: 'warehouse',
  Bank: 'bank',
  HQ: 'hq',
  IT: 'it',
  Post: 'post',
}
const assetUrl = (name: string) => `${import.meta.env.BASE_URL}art/${name}.glb`
const vec = (values: number[]): [number, number, number] => [
  values[0] ?? 0,
  values[1] ?? 0,
  values[2] ?? 0,
]

class AssetBoundary extends Component<
  { children: ReactNode; onError: () => void },
  { failed: boolean }
> {
  override state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  override componentDidCatch() {
    this.props.onError()
  }
  override render() {
    return this.state.failed ? null : this.props.children
  }
}

function Model({
  url,
  selected = false,
  reduced = false,
  onClick,
}: {
  url: string
  selected?: boolean
  reduced?: boolean
  onClick?: (event: ThreeEvent<MouseEvent>) => void
}) {
  const gltf = useGLTF(url, `${import.meta.env.BASE_URL}draco/`)
  const { root, owned } = useMemo(() => {
    const scene = gltf.scene.clone(true)
    const owned: Material[] = []
    const adapt = (source: Material) => {
      const result = adaptMaterial(source)
      if (result !== source) owned.push(result)
      return result
    }
    scene.traverse((object) => {
      if (object instanceof Mesh) {
        object.castShadow = Array.isArray(object.material)
          ? object.material.some(castsTownShadow)
          : castsTownShadow(object.material)
        object.receiveShadow = true
        object.material = Array.isArray(object.material)
          ? object.material.map(adapt)
          : adapt(object.material)
      }
    })
    return { root: scene, owned }
  }, [gltf.scene])
  const mixer = useMemo(() => {
    const result = new AnimationMixer(root)
    const clip = gltf.animations.find((a) => a.name === 'inspect_roof')
    if (clip) result.clipAction(clip).play()
    return result
  }, [root, gltf.animations])
  const time = useRef(0)
  const clock = useWorkClock(!reduced)
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    clock.reset()
    if (reduced) {
      time.current = selected ? 0.5 : 0
      mixer.setTime(time.current)
    }
    invalidate()
  }, [selected, reduced, mixer, invalidate, clock])
  useEffect(
    () => () => {
      mixer.stopAllAction()
      mixer.uncacheRoot(root)
      for (const material of owned) material.dispose()
    },
    [mixer, root, owned],
  )
  useFrame(() => {
    const target = selected ? 0.5 : 0
    if (time.current === target) return
    time.current = reduced
      ? target
      : time.current +
        Math.sign(target - time.current) *
          Math.min(Math.abs(target - time.current), clock.step(performance.now(), true))
    mixer.setTime(time.current)
    invalidate()
  })
  // biome-ignore lint/a11y/noStaticElementInteractions: R3F primitive is a Three.js object; the DOM building list provides keyboard controls.
  return <primitive object={root} dispose={null} onClick={onClick} />
}

function CameraRig({
  selected,
  reduced,
  selection,
  following,
  workSimulation,
  deliverySimulation,
  onManual,
}: {
  selected: Department | undefined
  reduced: boolean
  selection?: TownSelection
  following?: boolean
  workSimulation?: WorkSimulation
  deliverySimulation?: DeliverySimulation
  onManual?: () => void
}) {
  const controls = useRef<ComponentRef<typeof OrbitControls>>(null)
  const { camera, size, invalidate } = useThree()
  const clock = useWorkClock(!reduced)
  const target = useMemo(() => {
    const building = layout.buildings.find((b) => departments[b.name] === selected)
    return selected && building
      ? new Vector3(...vec(building.position)).add(new Vector3(0, 3, 0))
      : new Vector3(3, 0, -3)
  }, [selected])
  const current = useRef(new Vector3(3, 0, -3))
  const manual = useRef(false)
  const focus = useRef({ selected, selection })
  useEffect(() => {
    if (focus.current.selected !== selected || focus.current.selection !== selection) {
      manual.current = false
      focus.current = { selected, selection }
      invalidate()
    }
  }, [selected, selection, invalidate])
  const desiredZoom = Math.min(
    size.width / (selected ? 52 : 155),
    size.height / (selected ? 40 : 100),
  )
  useEffect(() => {
    clock.reset()
    if (target.distanceTo(current.current) > 0.01 || camera.zoom !== desiredZoom) invalidate()
  }, [target, desiredZoom, camera, invalidate, clock])
  useFrame(() => {
    if (!controls.current || manual.current) return
    if (following && selection) {
      if (selection.type === 'actor') {
        const a = workSimulation?.actor(selection.id)
        if (a) target.set(...a.position).add(new Vector3(0, 3, 0))
      } else if (selection.type === 'trip') {
        const t = deliverySimulation?.trip(selection.id)
        if (t) target.set(t.pose.x, 3, t.pose.z)
      }
    }
    // Keep inspection easing gradual even when a foreground render is slow.
    const amount = reduced
      ? 1
      : 1 - Math.exp(-Math.min(clock.step(performance.now(), true), 0.1) * 7)
    const previous = current.current.clone()
    current.current.lerp(target, amount)
    camera.zoom += (desiredZoom - camera.zoom) * amount
    // Finish the transition exactly. Status polling must not creep an almost
    // settled camera forward and make an idle town appear to keep moving.
    if (current.current.distanceTo(target) <= 0.01 && Math.abs(camera.zoom - desiredZoom) <= 0.01) {
      current.current.copy(target)
      camera.zoom = desiredZoom
    }
    const movement = current.current.clone().sub(previous)
    camera.position.add(movement)
    controls.current.target.copy(current.current)
    camera.updateProjectionMatrix()
    controls.current.update()
    if (current.current.distanceTo(target) > 0.01 || Math.abs(camera.zoom - desiredZoom) > 0.01)
      invalidate()
  })
  return (
    <OrbitControls
      ref={controls}
      enableRotate={false}
      enablePan
      onStart={() => {
        manual.current = true
        onManual?.()
      }}
      enableZoom
      enableDamping={false}
    />
  )
}

function RenderEvidence({
  onReady,
}: {
  onReady: (stats: { triangles: number; calls: number }) => void
}) {
  const frames = useRef(0)
  const frameNumber = useRef(0)
  const lastFrame = useRef(0)
  useFrame(({ gl, invalidate }) => {
    const now = performance.now()
    gl.domElement.dataset.renderFrame = String(++frameNumber.current)
    gl.domElement.dataset.renderCalls = String(gl.info.render.calls)
    gl.domElement.dataset.renderTriangles = String(gl.info.render.triangles)
    gl.domElement.dataset.renderTextures = String(gl.info.memory.textures)
    gl.domElement.dataset.renderGeometries = String(gl.info.memory.geometries)
    gl.domElement.dataset.renderFrameMs = String(lastFrame.current ? now - lastFrame.current : 0)
    lastFrame.current = now
    if (frames.current >= 3) return
    frames.current++
    if (frames.current === 3) {
      gl.domElement.dataset.sceneReady = 'true'
      onReady({ triangles: gl.info.render.triangles, calls: gl.info.render.calls })
    } else invalidate()
  })
  return null
}

export function Scene({
  language,
  selected,
  reduced,
  economy,
  sceneTime = 'day',
  onSelect,
  onError,
  onReady,
  presences = [],
  aiCount = 0,
  motionEnabled = false,
  handoff,
  crewFacts,
  onSelectTruck,
  workActors,
  deliveryTrips = [],
  onTripComplete,
  workSimulation,
  deliverySimulation,
  selection,
  following,
  onManualCamera,
  onSelectActor,
  onSelectTrip,
  dashboard,
}: {
  language: Language
  selected: Department | undefined
  reduced: boolean
  economy: boolean
  sceneTime?: SceneTime
  onSelect: (d: Department | undefined) => void
  onError: () => void
  onReady: (stats: { triangles: number; calls: number }) => void
  presences?: Presence[]
  aiCount?: number
  motionEnabled?: boolean
  handoff?: Handoff
  crewFacts?: DisplayFact[]
  onSelectTruck?: () => void
  workActors?: WorkActor[]
  dashboard?: DashboardView
  selection?: TownSelection
  following?: boolean
  onManualCamera?: () => void
  onSelectActor?: (id: string) => void
  onSelectTrip?: (id: string) => void
  workSimulation?: WorkSimulation
  deliverySimulation?: DeliverySimulation
  deliveryTrips?: { id: string; name: string }[]
  onTripComplete?: (id: string) => void
}) {
  const [available] = useState(() => {
    const probe = document.createElement('canvas')
    const context = probe.getContext('webgl2')
    if (!context) return false
    context.getExtension('WEBGL_lose_context')?.loseContext()
    return true
  })
  if (!available) return <div className="canvas-fallback">{copy[language].noWebgl}</div>
  const environment = sceneEnvironments[sceneTime]
  return (
    <Canvas
      role="img"
      aria-label={language === 'th' ? 'เมือง ERP แบบสามมิติ' : 'ERP town in 3D'}
      orthographic
      camera={{ position: [118, 116, 112], zoom: 5, near: 0.1, far: 700 }}
      shadows={!economy}
      dpr={economy ? 1 : [1, 1.5]}
      frameloop="demand"
      gl={{ antialias: !economy, toneMapping: AgXToneMapping }}
      onPointerMissed={() => onSelect(undefined)}
      onCreated={({ gl }) => {
        gl.shadowMap.type = PCFShadowMap
      }}
      fallback={<div className="canvas-fallback">{copy[language].noWebgl}</div>}
    >
      <color attach="background" args={[environment.background]} />
      <hemisphereLight args={[environment.sky, environment.ground, environment.ambient]} />
      <directionalLight
        position={environment.position}
        color={environment.light}
        intensity={environment.intensity}
        castShadow={!economy}
        shadow-mapSize={[2048, 2048]}
        shadow-camera-left={-90}
        shadow-camera-right={90}
        shadow-camera-top={90}
        shadow-camera-bottom={-90}
        shadow-camera-far={260}
        shadow-bias={-0.0002}
        shadow-normalBias={0.06}
      />
      <CameraRig
        selected={selected}
        reduced={reduced}
        selection={selection}
        following={following}
        workSimulation={workSimulation}
        deliverySimulation={deliverySimulation}
        onManual={onManualCamera}
      />
      <AssetBoundary onError={onError}>
        <TownLife
          animated={motionEnabled}
          awake={
            workActors === undefined ||
            workActors.some((a) => a.activity !== 'idle') ||
            deliveryTrips.length > 0
          }
          language={language}
          onSelectTruck={onSelectTruck}
          onSelectTrip={onSelectTrip}
          trips={deliveryTrips}
          simulation={deliverySimulation}
          onTripComplete={onTripComplete}
        />
      </AssetBoundary>
      <Suspense fallback={null}>
        <AssetBoundary onError={onError}>
          {workActors ? (
            <WorkCharacters
              actors={workActors}
              simulation={workSimulation}
              onSelectActor={onSelectActor}
              dashboard={dashboard}
              animated={motionEnabled}
              language={language}
              onSelect={onSelect}
            />
          ) : (
            <Characters
              presences={presences}
              aiCount={aiCount}
              animated={motionEnabled}
              language={language}
              crewFacts={crewFacts}
              onSelect={onSelect}
            />
          )}
        </AssetBoundary>
        {handoff && (
          <DemoHandoff
            key={`${handoff.from}:${handoff.to}`}
            route={handoff}
            animated={motionEnabled}
          />
        )}
        <AssetBoundary onError={onError}>
          <Model url={assetUrl('town-environment')} />
        </AssetBoundary>
        {layout.buildings.map((building) => {
          const department = departments[building.name]
          return (
            <group
              key={building.name}
              name={building.name}
              position={vec(building.position)}
              quaternion={[
                building.quaternion[0] ?? 0,
                building.quaternion[1] ?? 0,
                building.quaternion[2] ?? 0,
                building.quaternion[3] ?? 1,
              ]}
            >
              <AssetBoundary onError={onError}>
                <Model
                  url={assetUrl(`town-${building.name.toLowerCase()}`)}
                  selected={!!department && selected === department}
                  reduced={reduced}
                  onClick={
                    department
                      ? (event) => {
                          event.stopPropagation()
                          onSelect(department)
                        }
                      : undefined
                  }
                />
              </AssetBoundary>
              {department && selected === department && (
                <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.09, 0]}>
                  <ringGeometry args={[8, 8.22, 64]} />
                  <meshBasicMaterial color={tokens.dept[department].base} depthWrite={false} />
                </mesh>
              )}
            </group>
          )
        })}
        <RenderEvidence onReady={onReady} />
      </Suspense>
    </Canvas>
  )
}
