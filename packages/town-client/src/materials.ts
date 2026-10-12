import { type Material, MeshPhysicalMaterial, MeshStandardMaterial } from 'three'
import tokens from './tokens.json'
/** Transparent panes, floor surfaces and decals do not need a shadow draw. */
export function castsTownShadow(material: Material): boolean {
  return (
    !material.name.startsWith('M_ground_') &&
    ![
      'M_glass',
      'M_vehicle_glass',
      'M_water',
      'M_window_warm',
      'M_hazard',
      'M_decalLight',
      'M_decalDark',
      'M_emissiveSignal',
    ].includes(material.name)
  )
}

/** A's procedural water shader is not representable in glTF; use its shared color role. */
export function adaptMaterial(source: Material): Material {
  // Small stylized windows do not need scene-wide screen-space refraction.
  if (
    ['M_glass', 'M_vehicle_glass'].includes(source.name) &&
    source instanceof MeshPhysicalMaterial &&
    source.transmission > 0
  ) {
    const result = source.clone()
    result.transmission = 0
    result.thickness = 0
    return result
  }
  if (source.name !== 'M_water') return source
  return new MeshStandardMaterial({
    name: source.name,
    color: tokens.world.seaDeep,
    roughness: 0.5,
    metalness: 0,
  })
}
