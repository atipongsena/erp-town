import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import integration from '../public/art/integration.json' with { type: 'json' }
import layout from '../src/layout.json' with { type: 'json' }

const read = (path: string) => readFileSync(new URL(`../public/art/${path}`, import.meta.url))
const digest = (bytes: Uint8Array) => createHash('sha256').update(bytes).digest('hex')
type Manifest = {
  animations?: { name: string }[]
  extensionsRequired?: string[]
  nodes?: { extras?: { units?: string; gltf_forward?: string; gltf_up?: string } }[]
}
const json = (bytes: Buffer): Manifest => {
  expect(bytes.readUInt32LE(0)).toBe(0x46546c67)
  expect(bytes.readUInt32LE(4)).toBe(2)
  expect(bytes.readUInt32LE(8)).toBe(bytes.length)
  return JSON.parse(bytes.subarray(20, 20 + bytes.readUInt32LE(12)).toString()) as Manifest
}
describe('A development snapshot integration', () => {
  it.each(integration.buildings)(
    'keeps $file byte-identical to the integrated A snapshot',
    (asset) => {
      const bytes = read(asset.file)
      expect(digest(bytes)).toBe(asset.sha256)
      const manifest = json(bytes)
      if (
        ['town-shop.glb', 'town-warehouse.glb', 'town-bank.glb', 'town-hq.glb'].includes(asset.file)
      )
        expect(manifest.animations?.map((a) => a.name)).toContain('inspect_roof')
    },
  )
  it('keeps the compressed environment, all eight transforms and source provenance together', () => {
    const bytes = read(integration.environment.file)
    expect(digest(bytes)).toBe(integration.environment.sha256)
    expect(bytes.byteLength).toBeLessThan(1000000)
    expect(json(bytes).extensionsRequired).toContain('KHR_draco_mesh_compression')
    expect(layout.source_sha256).toBe(integration.sourceBlendSha256)
    expect(new Set(layout.buildings.map((b) => b.name)).size).toBe(8)
    for (const building of layout.buildings)
      expect([...building.position, ...building.quaternion].every(Number.isFinite)).toBe(true)
  })
  it.each(integration.vehicles)('ships the original $file with recorded provenance', (asset) => {
    const bytes = read(asset.file)
    expect(digest(bytes)).toBe(asset.sha256)
    expect(
      json(bytes).nodes?.some(
        (node) =>
          node.extras?.units === 'metres' &&
          node.extras.gltf_forward === '+Z' &&
          node.extras.gltf_up === '+Y',
      ),
    ).toBe(true)
  })
  it('ships local decoders and font license notices', () => {
    expect(
      readFileSync(new URL('../public/draco/draco_decoder.wasm', import.meta.url))
        .subarray(0, 4)
        .toString('hex'),
    ).toBe('0061736d')
    expect(readFileSync(new URL('../public/draco/LICENSE', import.meta.url), 'utf8')).toContain(
      'Apache License',
    )
    expect(readFileSync(new URL('../public/fonts/OFL.txt', import.meta.url), 'utf8')).toContain(
      'SIL OPEN FONT LICENSE',
    )
  })
})
