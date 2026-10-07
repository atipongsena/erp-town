#!/usr/bin/env node
// Cross-platform wrapper around `docker compose` and `bench` for the ERP Town dev stack.
// Spawns `docker` directly (no shell), so it behaves the same in PowerShell, cmd and Git Bash.
// Exit code = exit code of the docker child process.

import { spawn } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const COMPOSE = ['compose', '-f', 'infra/docker/compose.yaml', '--profile', 'dev']
const BENCH_SERVICE = 'bench'
const BENCH_DIR = '/home/frappe/frappe-bench'
const DEV_SITE = 'town.localhost'
const TEST_SITE = 'test.localhost'
const PING_URL = 'http://localhost:8080/api/method/ping'
const PING_INTERVAL_MS = 5_000
const PING_TIMEOUT_MS = 45 * 60_000

const HELP = `Usage: node scripts/erp.mjs <command> [args...]   (or: pnpm erp:<command> -- [args...])

Commands:
  up [args...]               docker compose --profile dev up -d, then wait for ${PING_URL} (max 45 min)
  down [args...]             docker compose --profile dev down
  logs [service] [args...]   follow logs (all services, or one, e.g. bench)
  shell                      interactive bash in the bench container (${BENCH_DIR})
  bench <args...>            run \`bench <args>\` in ${BENCH_DIR} of service "${BENCH_SERVICE}"
  test [--module m] [...]    bench --site ${TEST_SITE} run-tests --app erp_town [--module m]
  seed                       bench --site ${DEV_SITE} execute erp_town.setup.seed.run
  scenario [--days N] [--bulk N]
                             bench --site ${DEV_SITE} execute erp_town.setup.scenario.run --kwargs '{"days":N,"bulk":N}'
  help, --help, -h           show this help

Compose file: infra/docker/compose.yaml (profile dev). Desk: http://localhost:8080
`

/** Quote one argument for bash (single quotes, embedded ' escaped). */
function shQuote(arg) {
  if (/^[\w@%+=:,./-]+$/.test(arg)) return arg
  return `'${arg.replaceAll("'", `'\\''`)}'`
}

/** Run `docker <args>` with inherited stdio; resolves with the child's exit code. */
function docker(args) {
  return new Promise((resolve) => {
    const child = spawn('docker', args, { cwd: ROOT, stdio: 'inherit' })
    // Ctrl+C reaches the child through the shared console; wait for it to exit instead of dying first.
    const ignore = () => {}
    process.on('SIGINT', ignore)
    child.on('error', (err) => {
      process.off('SIGINT', ignore)
      console.error(
        err.code === 'ENOENT'
          ? 'erp: `docker` not found on PATH. Install/start Docker Desktop.'
          : `erp: failed to start docker: ${err.message}`,
      )
      resolve(127)
    })
    child.on('exit', (code, signal) => {
      process.off('SIGINT', ignore)
      resolve(code ?? (signal ? 1 : 0))
    })
  })
}

function compose(...args) {
  return docker([...COMPOSE, ...args])
}

/** `docker compose exec` flags: allocate a TTY only when we have one (scripts/CI capture output). */
function execFlags() {
  return process.stdin.isTTY && process.stdout.isTTY ? [] : ['-T']
}

function bench(args) {
  const cmd = `cd ${BENCH_DIR} && bench ${args.map(shQuote).join(' ')}`.trimEnd()
  return compose('exec', ...execFlags(), BENCH_SERVICE, 'bash', '-lc', cmd)
}

async function waitForPing() {
  const deadline = Date.now() + PING_TIMEOUT_MS
  const started = Date.now()
  console.log(`erp: waiting for ${PING_URL} (first boot can take 15-30 min)...`)
  while (Date.now() < deadline) {
    try {
      const res = await fetch(PING_URL, { signal: AbortSignal.timeout(PING_INTERVAL_MS) })
      if (res.ok) {
        const body = await res.json()
        if (body?.message === 'pong') {
          const secs = Math.round((Date.now() - started) / 1000)
          console.log(`erp: Desk is up at http://localhost:8080 (${secs}s)`)
          return 0
        }
      }
    } catch {
      // not up yet
    }
    const mins = Math.floor((Date.now() - started) / 60_000)
    process.stdout.write(`\rerp: still waiting... ${mins} min elapsed`)
    await new Promise((r) => setTimeout(r, PING_INTERVAL_MS))
  }
  console.error('\nerp: timed out after 45 min waiting for ping. Check `pnpm erp:logs bench`.')
  return 1
}

function parseNonNegativeInt(name, value) {
  if (!/^\d+$/.test(value))
    throw new Error(`--${name} must be a non-negative integer, got "${value}"`)
  return Number(value)
}

const commands = {
  async up(args) {
    const code = await compose('up', '-d', ...args)
    return code === 0 ? waitForPing() : code
  },
  down: (args) => compose('down', ...args),
  logs: (args) => compose('logs', '-f', '--tail=200', ...args),
  shell: () => compose('exec', ...execFlags(), '-w', BENCH_DIR, BENCH_SERVICE, 'bash', '-l'),
  bench(args) {
    if (args.length === 0) throw new Error('bench: missing arguments (e.g. `bench --version`)')
    return bench(args)
  },
  test: (args) => bench(['--site', TEST_SITE, 'run-tests', '--app', 'erp_town', ...args]),
  seed(args) {
    if (args.length > 0) throw new Error(`seed: unexpected arguments: ${args.join(' ')}`)
    return bench(['--site', DEV_SITE, 'execute', 'erp_town.setup.seed.run'])
  },
  scenario(args) {
    const { values } = parseArgs({
      args,
      options: { days: { type: 'string' }, bulk: { type: 'string' } },
      strict: true,
      allowPositionals: false,
    })
    const kwargs = {}
    if (values.days !== undefined) kwargs.days = parseNonNegativeInt('days', values.days)
    if (values.bulk !== undefined) kwargs.bulk = parseNonNegativeInt('bulk', values.bulk)
    return bench([
      '--site',
      DEV_SITE,
      'execute',
      'erp_town.setup.scenario.run',
      '--kwargs',
      JSON.stringify(kwargs),
    ])
  },
}

async function main(argv) {
  const [cmd, ...rest] = argv
  // `pnpm erp:x -- args` forwards the `--` separator; drop it.
  const args = rest[0] === '--' ? rest.slice(1) : rest
  if (cmd === undefined || cmd === 'help' || cmd === '--help' || cmd === '-h') {
    process.stdout.write(HELP)
    return 0
  }
  const run = Object.hasOwn(commands, cmd) ? commands[cmd] : undefined
  if (!run) {
    process.stderr.write(`erp: unknown command "${cmd}"\n\n${HELP}`)
    return 2
  }
  try {
    return await run(args)
  } catch (err) {
    console.error(`erp: ${err.message}`)
    return 2
  }
}

process.exitCode = await main(process.argv.slice(2))
