// Self-starting, bounded ERP live demo. Run: node scripts/live-demo.ts [--rounds=1|2|3]
// Only test.localhost is seeded; its database is restored after the host stops.
import { type ChildProcess, spawn } from 'node:child_process'
import { mkdir, open, readFile, unlink } from 'node:fs/promises'
import path from 'node:path'
import { createInterface } from 'node:readline'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import {
  benchExecute,
  prepareTestSite,
  ROOT,
  restoreTestSite,
} from '../tests/integration/helpers.ts'

interface Credential {
  user: string
  key: string
  secret: string
}
interface AgentKeys {
  agent_key: string
  agent: Credential
  delegator: Credential
}
interface DemoKeys {
  staff: Record<string, Credential>
  viewer: { user: string; password: string }
}

export function demoRounds(args: string[]): number {
  if (args.length > 1 || (args.length === 1 && !/^--rounds=[123]$/.test(args[0] ?? '')))
    throw new Error('Usage: node scripts/live-demo.ts [--rounds=1|2|3]')
  return args.length === 0 ? 3 : Number(args[0]?.split('=')[1])
}

export function startOutboxPump(
  publish: () => Promise<unknown>,
  onError: () => void,
  interval = 2500,
) {
  const abort = new AbortController()
  let stopped = false
  const done = (async () => {
    while (!stopped) {
      try {
        await publish()
      } catch {
        stopped = true
        onError()
        return
      }
      if (stopped) return
      try {
        await delay(interval, undefined, { signal: abort.signal })
      } catch {
        return
      }
    }
  })()
  return {
    stop: async () => {
      stopped = true
      abort.abort()
      // The caller must wait here before restoring the ERP snapshot. An
      // already-started bench transaction is never overlapped with restore.
      await done
    },
  }
}

async function acquireLock(lockPath: string) {
  try {
    const lock = await open(lockPath, 'wx')
    await lock.writeFile(JSON.stringify({ pid: process.pid, site: 'test.localhost' }))
    return lock
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error
    const prior: unknown = JSON.parse(await readFile(lockPath, 'utf8'))
    if (
      !prior ||
      typeof prior !== 'object' ||
      !('pid' in prior) ||
      !Number.isSafeInteger(prior.pid) ||
      Number(prior.pid) <= 0 ||
      !('site' in prior) ||
      prior.site !== 'test.localhost'
    )
      throw new Error('Unrecognized live demo lock; refusing to touch a running test site')
    try {
      process.kill(Number(prior.pid), 0)
    } catch (probeError) {
      if ((probeError as NodeJS.ErrnoException).code !== 'ESRCH') throw probeError
      // Only the exact stale launcher marker is removed. prepareTestSite then
      // restores any snapshot left by that interrupted launcher before seeding.
      await unlink(lockPath)
      const lock = await open(lockPath, 'wx')
      await lock.writeFile(JSON.stringify({ pid: process.pid, site: 'test.localhost' }))
      return lock
    }
    throw new Error('A live demo launcher is already running')
  }
}

async function checked(command: string, args: string[], cwd: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, { cwd, stdio: 'inherit' })
    child.once('error', reject)
    child.once('exit', (code) => (code === 0 ? resolve() : reject(new Error('Demo build failed'))))
  })
}

async function main() {
  const rounds = demoRounds(process.argv.slice(2))
  const temporary = path.join(ROOT, 'go/tmp')
  await mkdir(temporary, { recursive: true })
  const lockPath = path.join(temporary, 'live-demo.lock')
  // Refuse concurrent demo launchers. Never remove another launcher's lock.
  const lock = await acquireLock(lockPath)
  let child: ChildProcess | undefined
  let pump: ReturnType<typeof startOutboxPump> | undefined
  let stopping = false
  let prepared = false
  const stop = () => {
    stopping = true
    if (child && child.exitCode === null) child.stdin?.write('stop\n')
  }
  process.once('SIGINT', stop)
  process.once('SIGTERM', stop)
  // Terminal tools can request a graceful stop without killing the process and
  // losing the snapshot restoration, including Windows sessions without a PTY.
  const controls = createInterface({ input: process.stdin })
  controls.on('line', (line) => {
    if (line.trim() === 'stop') stop()
  })
  const binary = path.join(
    temporary,
    process.platform === 'win32' ? 'town-live-demo.exe' : 'town-live-demo',
  )
  try {
    const ping = await fetch('http://127.0.0.1:8080/api/method/ping', {
      headers: { 'X-Frappe-Site-Name': 'test.localhost' },
      signal: AbortSignal.timeout(5000),
    })
    if (!ping.ok) throw new Error('Start the existing local ERP stack first')
    await checked(
      process.execPath,
      [path.join(ROOT, 'node_modules/typescript/bin/tsc'), '--noEmit'],
      path.join(ROOT, 'packages/town-client'),
    )
    await checked(
      process.execPath,
      [path.join(ROOT, 'packages/town-client/node_modules/vite/bin/vite.js'), 'build'],
      path.join(ROOT, 'packages/town-client'),
    )
    await checked('go', ['build', '-o', binary, './cmd/town-live-demo'], path.join(ROOT, 'go'))
    if (stopping) return
    await prepareTestSite({ scheduler: true })
    prepared = true
    console.log('[live-demo] test.localhost snapshot saved; isolated ERP seeded')
    const reader = await benchExecute<Credential>(
      'erp_town.setup.testing.issue_client_projection_keys',
    )
    const agent = await benchExecute<AgentKeys>(
      'erp_town.setup.testing.issue_agent_acceptance_keys',
    )
    const demo = await benchExecute<DemoKeys>('erp_town.setup.live_demo.provision')
    if (
      !reader?.key ||
      !reader.secret ||
      !agent?.agent.key ||
      !demo?.staff.shop?.key ||
      !demo.viewer.password
    )
      throw new Error('Isolated demo identity setup failed')
    // Provisioning gives the seeded sales human a fresh pair; use that same
    // pair for delegated draft policy so no stale credential survives setup.
    agent.delegator = demo.staff.shop
    if (stopping) return
    pump = startOutboxPump(
      () => benchExecute('erp_town.setup.live_demo.publish_pending'),
      () => {
        console.error('[live-demo] event delivery failed; demo stopped without an automatic retry')
        process.exitCode = 1
        stop()
      },
    )
    const environment = Object.fromEntries(
      Object.entries(process.env).filter(
        ([name]) =>
          !/^(TOWN_|PILOT_|BACKUP_|TEST_)|SECRET|PASSWORD|TOKEN|API_KEY|PRIVATE_KEY/i.test(name),
      ),
    )
    child = spawn(binary, [], {
      cwd: path.join(ROOT, 'go'),
      env: environment,
      stdio: ['pipe', 'inherit', 'inherit'],
    })
    child.stdin?.write(`${JSON.stringify({ reader, agent, ...demo, rounds })}\n`)
    const result = await new Promise<number>((resolve, reject) => {
      child?.once('error', reject)
      child?.once('close', (code) => resolve(code ?? 1))
    })
    if (!stopping) process.exitCode = result
  } finally {
    await pump?.stop()
    if (child?.exitCode === null) {
      child.stdin?.write('stop\n')
      await new Promise<void>((resolve) => child?.once('close', () => resolve()))
    }
    try {
      if (prepared) {
        await restoreTestSite()
        console.log('[live-demo] test.localhost database restored; demo stopped')
      }
    } finally {
      await lock.close()
      await unlink(lockPath)
      process.off('SIGINT', stop)
      process.off('SIGTERM', stop)
      controls.close()
    }
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(() => {
    // Setup/ERP errors can include secrets. Never echo their raw bodies.
    console.error('[live-demo] failed; no credentials or ERP response bodies were logged')
    process.exitCode = 1
  })
}
