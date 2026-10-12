import { type ThreeEvent, useFrame, useThree } from '@react-three/fiber'
import { useEffect, useMemo, useRef } from 'react'
import { CanvasTexture, Frustum, Matrix4, type Mesh, SRGBColorSpace, Vector3 } from 'three'
import type { Language } from './copy.ts'
import type { DashboardView } from './dashboard-view.ts'
import { departmentScreenContent } from './screen-content.ts'
import type { Department } from './world-view.ts'
export function DepartmentScreen({
  view,
  department,
  language,
  onSelect,
}: {
  view: DashboardView
  department: Department
  language: Language
  onSelect: (d: Department) => void
}) {
  const { camera, invalidate } = useThree()
  const mesh = useRef<Mesh>(null)
  const surface = useMemo(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 512
    canvas.height = 256
    const texture = new CanvasTexture(canvas)
    texture.colorSpace = SRGBColorSpace
    return {
      canvas,
      texture,
      context: canvas.getContext('2d'),
      frustum: new Frustum(),
      matrix: new Matrix4(),
      point: new Vector3(),
    }
  }, [])
  const content = departmentScreenContent(view, department, language)
  const pending = useRef(content)
  pending.current = content
  const painted = useRef({
    scope: '',
    signature: '',
    at: 0,
    total: undefined as number | undefined,
  })
  useEffect(() => {
    if (painted.current.signature === content.signature) return
    if (
      painted.current.scope !== content.scope ||
      content.total === undefined ||
      (painted.current.total !== undefined &&
        content.total !== undefined &&
        content.total < painted.current.total)
    ) {
      surface.context?.clearRect(0, 0, 512, 256)
      surface.texture.needsUpdate = true
      painted.current.signature = ''
      painted.current.at = 0
    }
    invalidate()
    const timer = setTimeout(
      invalidate,
      Math.max(0, 500 - (performance.now() - painted.current.at)),
    )
    return () => clearTimeout(timer)
  }, [content.signature, content.scope, content.total, surface, invalidate])
  useEffect(() => () => surface.texture.dispose(), [surface])
  useFrame(() => {
    const current = pending.current,
      previous = painted.current,
      ctx = surface.context
    if (
      !mesh.current ||
      !ctx ||
      current.signature === previous.signature ||
      performance.now() - previous.at < 500
    )
      return
    mesh.current.getWorldPosition(surface.point)
    surface.frustum.setFromProjectionMatrix(
      surface.matrix.multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse),
    )
    if (!surface.frustum.containsPoint(surface.point)) return
    ctx.fillStyle = '#163b44'
    ctx.fillRect(0, 0, 512, 256)
    ctx.fillStyle = '#bdebd4'
    ctx.font = '600 28px Plex, sans-serif'
    ctx.fillText(current.title, 24, 44, 464)
    ctx.font = '20px Plex, sans-serif'
    ctx.fillStyle = '#faf6de'
    ctx.fillText(current.company || 'ERP', 24, 78, 464)
    current.lines.forEach((line, i) => {
      ctx.fillText(line, 24, 122 + i * 32, 464)
    })
    ctx.fillStyle = '#91bac0'
    ctx.font = '18px Plex, sans-serif'
    ctx.fillText(
      current.source === 'historical'
        ? language === 'th'
          ? 'ย้อนหลัง · อ่านอย่างเดียว'
          : 'Historical · read only'
        : current.source === 'live'
          ? 'ERP · visible records'
          : '—',
      24,
      234,
      464,
    )
    surface.texture.needsUpdate = true
    painted.current = {
      scope: current.scope,
      signature: current.signature,
      at: performance.now(),
      total: current.total,
    }
  })
  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: R3F mesh, with department navigation as the keyboard equivalent.
    <mesh
      ref={mesh}
      name={`DepartmentScreen-${department}`}
      position={[0, 1.06, 0.89]}
      onClick={(e: ThreeEvent<MouseEvent>) => {
        e.stopPropagation()
        onSelect(department)
      }}
    >
      <planeGeometry args={[0.48, 0.3]} />
      <meshBasicMaterial map={surface.texture} toneMapped={false} />
    </mesh>
  )
}
