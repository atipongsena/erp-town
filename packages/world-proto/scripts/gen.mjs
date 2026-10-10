import { spawnSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const goDir = path.resolve(packageDir, '../../go')
const resolved = spawnSync('go', ['-C', goDir, 'tool', '-n', 'buf'], {
  encoding: 'utf8',
  cwd: packageDir,
})
if (resolved.status !== 0) {
  process.stderr.write(resolved.stderr ?? 'Cannot resolve pinned Buf tool\n')
  process.exit(resolved.status ?? 1)
}
const buf = resolved.stdout.trim()
for (const args of [['lint'], ['generate']]) {
  const result = spawnSync(buf, args, { cwd: packageDir, stdio: 'inherit' })
  if (result.error) throw result.error
  if (result.status !== 0) process.exit(result.status ?? 1)
}

// Keep generated output consistent with the repository's EOF whitespace rules.
const generatedTS = path.join(packageDir, 'src/gen/world/v1/world_pb.ts')
writeFileSync(generatedTS, `${readFileSync(generatedTS, 'utf8').trimEnd()}\n`)
