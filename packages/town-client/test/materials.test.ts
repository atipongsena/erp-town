import { MeshPhysicalMaterial, MeshStandardMaterial } from 'three'
import { describe, expect, it } from 'vitest'
import { adaptMaterial, castsTownShadow } from '../src/materials.ts'

describe('Blender water shader fallback', () => {
  it('casts shadows from solid architecture, not glass, flat ground or emissive decals', () => {
    for (const name of [
      'M_glass',
      'M_vehicle_glass',
      'M_ground_grass',
      'M_water',
      'M_decalLight',
    ]) {
      const m = new MeshStandardMaterial({ name })
      expect(castsTownShadow(m)).toBe(false)
      m.dispose()
    }
    const roof = new MeshStandardMaterial({ name: 'M_roofDept_shop' })
    expect(castsTownShadow(roof)).toBe(true)
    roof.dispose()
  })
  it('keeps the glass color and PBR finish without a town-wide transmission render pass', () => {
    const source = new MeshPhysicalMaterial({
      color: '#9cbfc6',
      transmission: 1,
      thickness: 0.4,
      roughness: 0.3,
    })
    source.name = 'M_glass'
    const result = adaptMaterial(source) as MeshPhysicalMaterial
    expect(result).not.toBe(source)
    expect(result.color.getHexString()).toBe(source.color.getHexString())
    expect(result.transmission).toBe(0)
    expect(source.transmission).toBe(1)
    result.dispose()
    source.dispose()
  })
  it('repairs the unsupported water role without mutating the GLTF cache', () => {
    const source = new MeshStandardMaterial({ color: '#ffffff' })
    source.name = 'M_water'
    const result = adaptMaterial(source)
    expect(result).not.toBe(source)
    expect((result as MeshStandardMaterial).color.getHexString()).toBe('4ba3d6')
    expect(source.color.getHexString()).toBe('ffffff')
    result.dispose()
    source.dispose()
  })
  it('preserves other roles instead of repainting the whole art kit', () => {
    const source = new MeshStandardMaterial({ color: '#ee6f5f' })
    source.name = 'M_roofDept_shop'
    expect(adaptMaterial(source)).toBe(source)
    source.dispose()
  })
})
