import { createHash } from 'node:crypto'
import { copyFileSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const source = resolve(process.argv[2] ?? '../ERP_Town-art-direction')
const art = join(root, 'public/art')
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex')
mkdirSync(art, { recursive: true })
const files = []
const vehicles = []
for (const name of ['shop', 'warehouse', 'bank', 'hq', 'it', 'post', 'clocktower', 'citygate']) {
  const path = `assets/art/models/buildings/town-${name}.glb`
  const bytes = readFileSync(join(source, path))
  writeFileSync(join(art, `town-${name}.glb`), bytes)
  files.push({ source: path, file: `town-${name}.glb`, sha256: digest(bytes) })
}
for (const name of ['car', 'truck']) {
  const path = `assets/art/models/vehicles/town-${name}.glb`
  const bytes = readFileSync(join(source, path))
  writeFileSync(join(art, `town-${name}.glb`), bytes)
  vehicles.push({ source: path, file: `town-${name}.glb`, sha256: digest(bytes) })
}
// Character snapshots are refreshed separately by the art checkout's export_characters.py.
const previous = JSON.parse(readFileSync(join(art, 'integration.json'), 'utf8'))
const fonts = join(root, 'public/fonts')
mkdirSync(fonts, { recursive: true })
for (const name of readdirSync(join(source, 'packages/design-tokens/css/fonts'))) {
  if (name.endsWith('.woff2'))
    copyFileSync(join(source, 'packages/design-tokens/css/fonts', name), join(fonts, name))
}
copyFileSync(
  join(source, 'packages/design-tokens/tokens/tokens.json'),
  join(root, 'src/tokens.json'),
)
copyFileSync(join(source, 'assets/art/manifest.json'), join(art, 'source-manifest.json'))
const layout = JSON.parse(readFileSync(join(art, 'layout.json'), 'utf8'))
const sourceHash = digest(readFileSync(join(source, 'assets/art/showcase/town.blend')))
if (layout.source_sha256 !== sourceHash)
  throw new Error('Environment/layout source differs; export again before integration')
copyFileSync(join(art, 'layout.json'), join(root, 'src/layout.json'))
writeFileSync(
  join(art, 'integration.json'),
  `${JSON.stringify(
    {
      sourceCheckout: source,
      sourceBlendSha256: sourceHash,
      approval: 'Development snapshot; G-A3/G-A4 remain open.',
      buildings: files,
      vehicles,
      characters: previous.characters ?? [],
      environment: {
        file: 'town-environment.glb',
        sha256: digest(readFileSync(join(art, 'town-environment.glb'))),
      },
    },
    null,
    2,
  )}\n`,
)
console.log(
  'Integrated eight buildings, environment, layout, tokens and local fonts; source untouched.',
)
