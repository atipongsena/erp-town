// Local launcher. Default is gateway-only; --world requires provisioned schema/reader.
import { type ChildProcess, spawn } from 'node:child_process'
import { mkdir, readFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import path from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { parseEnv } from 'node:util'

const root = fileURLToPath(new URL('../', import.meta.url))

export async function availablePort(candidates: number[]): Promise<number> {
  for (const port of candidates) {
    const result = await new Promise<number | undefined>((resolve) => {
      const probe = createServer()
      probe.once('error', () => resolve(undefined))
      probe.listen(port, '127.0.0.1', () => {
        const address = probe.address()
        const selected = address && typeof address !== 'string' ? address.port : undefined
        probe.close(() => resolve(selected))
      })
    })
    if (result !== undefined) return result
  }
  throw new Error('No free workspace port; existing services were left running')
}

export function workspaceEnvironment(
  local: Record<string, string | undefined>,
  townPort: number,
  world = false,
) {
  if (!local.TOWN_JWT_PRIVATE_KEY)
    throw new Error('Configure the existing TOWN_JWT_PRIVATE_KEY before starting the workspace')
  if (world && (!local.TOWN_WORLD_API_KEY || !local.TOWN_WORLD_API_SECRET))
    throw new Error('World mode requires the separately provisioned reader')
  const database =
    local.TOWN_DATABASE_URL ?? 'postgres://town:town@127.0.0.1:5433/town?sslmode=disable'
  if (world) {
    const target = new URL(database)
    if (
      !['postgres:', 'postgresql:'].includes(target.protocol) ||
      target.hostname !== '127.0.0.1' ||
      target.port !== '5433' ||
      target.pathname !== '/town' ||
      [...target.searchParams.keys()].some((key) => key !== 'sslmode')
    )
      throw new Error('World workspace requires the approved local town database')
  }
  return {
    town: {
      TOWN_ROLES: world ? 'gateway,outbox,world' : 'gateway',
      TOWN_HTTP_ADDR: `127.0.0.1:${townPort}`,
      TOWN_ERP_URL: 'http://127.0.0.1:8080',
      TOWN_ERP_SITE: 'town.localhost',
      TOWN_JWT_PRIVATE_KEY: local.TOWN_JWT_PRIVATE_KEY,
      ...(world
        ? {
            TOWN_WORLD_API_KEY: local.TOWN_WORLD_API_KEY,
            TOWN_WORLD_API_SECRET: local.TOWN_WORLD_API_SECRET,
            TOWN_DATABASE_URL: database,
            TOWN_REDIS_URL: 'redis://127.0.0.1:6380/0',
            TOWN_STREAM: 'town:events',
            TOWN_EXPERIENCE_ENABLED: 'true',
          }
        : {}),
    },
    frontend: {
      TOWN_DEV_GATEWAY_URL: `http://127.0.0.1:${townPort}`,
      TOWN_DEV_ERP_URL: 'http://127.0.0.1:8080',
      TOWN_DEV_ERP_SITE: 'town.localhost',
    },
  }
}

async function main() {
  const args = process.argv.slice(2)
  if (args.some((arg) => arg !== '--world')) throw new Error('Usage: dev-workspace.ts [--world]')
  const world = args.includes('--world')
  const ping = await fetch('http://127.0.0.1:8080/api/method/ping', {
    signal: AbortSignal.timeout(5000),
  })
  if (!ping.ok) throw new Error('Start the existing ERP dev stack first')
  const local = parseEnv(await readFile(path.join(root, 'infra/docker/.env'), 'utf8'))
  const townPort = await availablePort([8090, 8091, 8092])
  const frontendPort = await availablePort([5173, 5174, 5175])
  const env = workspaceEnvironment(
    {
      ...local,
      TOWN_JWT_PRIVATE_KEY: process.env.TOWN_JWT_PRIVATE_KEY ?? local.TOWN_JWT_PRIVATE_KEY,
    },
    townPort,
    world,
  )
  const base = Object.fromEntries(
    Object.entries(process.env).filter(
      ([name]) => !/^(TOWN_|PILOT_|BACKUP_)|SECRET|PASSWORD|TOKEN|API_KEY|PRIVATE_KEY/i.test(name),
    ),
  )
  const binary = path.join(
    root,
    'go/tmp',
    process.platform === 'win32' ? 'town-workspace.exe' : 'town-workspace',
  )
  await mkdir(path.dirname(binary), { recursive: true })
  await new Promise<void>((resolve, reject) => {
    const build = spawn('go', ['build', '-o', binary, './cmd/town'], {
      cwd: path.join(root, 'go'),
      env: base,
      stdio: 'inherit',
    })
    build.once('error', reject)
    build.once('exit', (code) => (code === 0 ? resolve() : reject(new Error('Town build failed'))))
  })
  const children: ChildProcess[] = []
  let closing = false
  const stop = () => {
    if (closing) return
    closing = true
    for (const child of children) if (child.exitCode === null) child.kill()
  }
  const launch = (
    command: string,
    args: string[],
    environment: Record<string, string | undefined>,
  ) => {
    const child = spawn(command, args, {
      cwd: root,
      env: { ...base, ...environment },
      stdio: 'inherit',
    })
    children.push(child)
    child.on('error', () => {
      process.exitCode = 1
      stop()
    })
    child.on('exit', (code) => {
      if (!closing) {
        process.exitCode = code ?? 1
        stop()
      }
    })
    return child
  }
  process.once('SIGINT', stop)
  process.once('SIGTERM', stop)
  try {
    launch(
      binary,
      ['serve', `--roles=${env.town.TOWN_ROLES}`, '--require-provisioned-schema'],
      env.town,
    )
    let ready = false
    for (let i = 0; i < 40 && !closing; i++) {
      try {
        const status = await fetch(`http://127.0.0.1:${townPort}/town/api/healthz`, {
          signal: AbortSignal.timeout(1000),
        })
        if (status.ok && (await status.json()).status === 'ok') {
          ready = true
          break
        }
      } catch {
        /* Service startup is bounded below. */
      }
      await delay(250)
    }
    if (!ready) throw new Error('Town gateway did not become ready')
    launch(
      process.execPath,
      [
        path.join(root, 'packages/town-client/node_modules/vite/bin/vite.js'),
        '--host',
        '127.0.0.1',
        '--port',
        String(frontendPort),
        '--strictPort',
        '--config',
        path.join(root, 'packages/town-client/vite.config.ts'),
        path.join(root, 'packages/town-client'),
      ],
      env.frontend,
    )
    console.log(`[workspace] http://127.0.0.1:${frontendPort}/?lang=th`)
    console.log(
      world
        ? '[workspace] World, avatars and history enabled. No accounts or migrations were changed by this launcher.'
        : '[workspace] ERP login and document API enabled. Start --world after reader/schema provisioning to enable town data.',
    )
  } catch (error) {
    stop()
    throw error
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(() => {
    console.error(
      '[workspace] Startup failed. Check ERP availability, the existing JWT configuration and free local ports. Secret values were not logged.',
    )
    process.exitCode = 1
  })
}
