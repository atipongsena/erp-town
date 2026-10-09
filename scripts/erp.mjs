#!/usr/bin/env node
// Cross-platform wrapper around `docker compose` and `bench` for the ERP Town dev stack and the
// pilot profile (`pilot:*` commands, infra/docker/pilot).
// Spawns `docker` directly (no shell), so it behaves the same in PowerShell, cmd and Git Bash.
// Exit code = exit code of the docker child process.

import { execFileSync, spawn } from 'node:child_process'
import { copyFileSync, existsSync, readFileSync } from 'node:fs'
import https from 'node:https'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const COMPOSE = ['compose', '-f', 'infra/docker/compose.yaml']
const ENV_FILE = 'infra/docker/.env'
const ENV_EXAMPLE = 'infra/docker/.env.example'
const BENCH_SERVICE = 'bench'
const BENCH_DIR = '/home/frappe/frappe-bench'
const DEV_SITE = 'town.localhost'
const TEST_SITE = 'test.localhost'
const PING_URL = 'http://localhost:8080/api/method/ping'
const PING_INTERVAL_MS = 5_000
const PING_TIMEOUT_MS = 45 * 60_000
const PILOT_CONTAINERFILE = 'infra/docker/pilot/Containerfile'
const PILOT_IMAGE_REPO = 'erp-town/erpnext'
const TOWN_CONTAINERFILE = 'infra/docker/town/Containerfile'
const TOWN_IMAGE_REPO = 'erp-town/town'
// inputs of the pilot images (build context allowlists, Containerfile.dockerignore): a tag is "-dirty"
// when they differ from HEAD
const PILOT_INPUTS = ['frappe-apps/erp_town', 'infra/docker/pilot', 'infra/docker/town', 'go']
const PG_SERVICE = 'postgres'
const PILOT_PG_DATABASE = 'town_pilot'
const PILOT_SERVICES = [
  'create-site',
  'backend',
  'websocket',
  'frontend',
  'town',
  'queue-short',
  'queue-long',
  'scheduler',
  'caddy',
  'backup',
]
// keys `pilot:up` needs (infra/docker/.env or the environment); see .env.example
const PILOT_REQUIRED = [
  'SITE_NAME',
  'DB_ROOT_PASSWORD',
  'PILOT_ADMIN_PASSWORD',
  'LETSENCRYPT_EMAIL',
  'PILOT_TOWN_JWT_PRIVATE_KEY',
  'TOWN_PG_PASSWORD',
]
// dev-only defaults `pilot:up` refuses (compose falls back to them when unset)
const PILOT_REFUSED = { TOWN_PG_PASSWORD: 'town' }

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

Pilot (profile pilot, image ${PILOT_IMAGE_REPO}:v16-<git short sha>; drill: infra/docker/pilot/RESTORE.md):
  pilot:build [args...]      docker build -f ${PILOT_CONTAINERFILE} -t ${PILOT_IMAGE_REPO}:v16-<sha>[-dirty] .
                             and ${TOWN_CONTAINERFILE} -t ${TOWN_IMAGE_REPO}:<same tag> .
  pilot:up [args...]         create database ${PILOT_PG_DATABASE} if missing, docker compose --profile pilot up -d,
                             then wait for https://$SITE_NAME/api/method/ping
  pilot:down                 stop and remove the pilot containers only (volumes and dev stack stay)
  pilot:bench <args...>      run \`bench <args>\` in the pilot "backend" container
  pilot:backup               one backup now: bench backup --with-files + pg_dump ${PILOT_PG_DATABASE} -> S3 (BACKUP_S3_* in .env)
  The image is PILOT_IMAGE if set, else the tag of the current HEAD (build it first).

  help, --help, -h           show this help

Compose file: infra/docker/compose.yaml (profiles dev, pilot). Desk: http://localhost:8080
`

/** Quote one argument for bash (single quotes, embedded ' escaped). */
function shQuote(arg) {
  if (/^[\w@%+=:,./-]+$/.test(arg)) return arg
  return `'${arg.replaceAll("'", `'\\''`)}'`
}

/** Run `docker <args>` with inherited stdio; resolves with the child's exit code. */
function docker(args, env = process.env) {
  return new Promise((resolve) => {
    const child = spawn('docker', args, { cwd: ROOT, stdio: 'inherit', env })
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

function composeIn(profile, args, env) {
  const envFile = existsSync(path.join(ROOT, ENV_FILE)) ? ['--env-file', ENV_FILE] : []
  return docker([...COMPOSE, '--profile', profile, ...envFile, ...args], env)
}

const compose = (...args) => composeIn('dev', args)

/** Run a command and capture stdout; never throws. */
function capture(cmd, args) {
  try {
    const out = execFileSync(cmd, args, {
      cwd: ROOT,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
    return { ok: true, out: out.trim() }
  } catch {
    return { ok: false, out: '' }
  }
}

function git(...args) {
  const res = capture('git', args)
  if (!res.ok) throw new Error(`git ${args.join(' ')} failed (is this a git checkout?)`)
  return res.out
}

/** v16-<short sha of HEAD>, with -dirty when the image inputs have uncommitted changes. */
function pilotTag() {
  const sha = git('rev-parse', '--short', 'HEAD')
  const dirty = git('status', '--porcelain', '--', ...PILOT_INPUTS) !== ''
  return `v16-${sha}${dirty ? '-dirty' : ''}`
}

/**
 * Pilot compose call with PILOT_IMAGE and PILOT_TOWN_IMAGE set (explicit env vars, else the HEAD tag,
 * which must exist for both images).
 */
function pilotCompose(...args) {
  const tag = process.env.PILOT_IMAGE && process.env.PILOT_TOWN_IMAGE ? '' : pilotTag()
  const image = process.env.PILOT_IMAGE || `${PILOT_IMAGE_REPO}:${tag}`
  const townImage = process.env.PILOT_TOWN_IMAGE || `${TOWN_IMAGE_REPO}:${tag}`
  for (const img of [image, townImage]) {
    if (!capture('docker', ['image', 'inspect', '--format', '{{.Id}}', img]).ok)
      throw new Error(
        `image ${img} not found: run \`pnpm erp:pilot:build\` (or set PILOT_IMAGE / PILOT_TOWN_IMAGE)`,
      )
  }
  console.log(`erp: pilot images ${image}, ${townImage}`)
  return composeIn('pilot', args, {
    ...process.env,
    PILOT_IMAGE: image,
    PILOT_TOWN_IMAGE: townImage,
  })
}

/**
 * Create database ${PILOT_PG_DATABASE} when it is missing. The postgres init script only runs on a
 * fresh volume, so an existing dev volume needs this before `town` starts. Idempotent.
 */
async function ensurePilotDatabase() {
  const envFile = existsSync(path.join(ROOT, ENV_FILE)) ? ['--env-file', ENV_FILE] : []
  const exec = [...COMPOSE, ...envFile, 'exec', '-T', PG_SERVICE]
  const probe = `SELECT 1 FROM pg_database WHERE datname='${PILOT_PG_DATABASE}'`
  const found = capture('docker', [...exec, 'psql', '-U', 'town', '-d', 'town', '-tAc', probe])
  if (!found.ok)
    throw new Error(`cannot query the ${PG_SERVICE} service (is it running? \`pnpm erp:up\`)`)
  if (found.out === '1') return 0
  console.log(`erp: creating database ${PILOT_PG_DATABASE}`)
  return docker([
    ...exec,
    'psql',
    '-U',
    'town',
    '-d',
    'town',
    '-v',
    'ON_ERROR_STOP=1',
    '-c',
    `CREATE DATABASE ${PILOT_PG_DATABASE}`,
  ])
}

/** KEY=VALUE pairs of infra/docker/.env (no interpolation), overridden by the process environment. */
function envSettings() {
  const settings = {}
  const file = path.join(ROOT, ENV_FILE)
  if (existsSync(file)) {
    for (const line of readFileSync(file, 'utf8').split(/\r?\n/)) {
      const m = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$/.exec(line)
      if (m) settings[m[1]] = m[2].replace(/^(['"])(.*)\1$/, '$2')
    }
  }
  for (const key of [...Object.keys(settings), ...PILOT_REQUIRED]) {
    if (process.env[key] !== undefined) settings[key] = process.env[key]
  }
  return settings
}

/** Create infra/docker/.env from .env.example (dev-only defaults) when it is missing. */
function ensureEnvFile() {
  if (existsSync(path.join(ROOT, ENV_FILE))) return
  copyFileSync(path.join(ROOT, ENV_EXAMPLE), path.join(ROOT, ENV_FILE))
  console.log(`erp: created ${ENV_FILE} from ${ENV_EXAMPLE} (dev-only defaults; edit to change)`)
}

/** `docker compose exec` flags: allocate a TTY only when we have one (scripts/CI capture output). */
function execFlags() {
  return process.stdin.isTTY && process.stdout.isTTY ? [] : ['-T']
}

function bench(args) {
  const cmd = `cd ${BENCH_DIR} && bench ${args.map(shQuote).join(' ')}`.trimEnd()
  return compose('exec', ...execFlags(), BENCH_SERVICE, 'bash', '-lc', cmd)
}

/** GET https://<site>/api/method/ping through caddy; a *.localhost site is reached at 127.0.0.1. */
function pilotPing(site) {
  const host = site === 'localhost' || site.endsWith('.localhost') ? '127.0.0.1' : site
  return new Promise((resolve) => {
    const req = https.request(
      {
        host,
        servername: site,
        path: '/api/method/ping',
        headers: { Host: site },
        // readiness probe only: the smoke site's certificate comes from caddy's internal CA
        rejectUnauthorized: false,
        timeout: PING_INTERVAL_MS,
      },
      (res) => {
        let body = ''
        res.on('data', (chunk) => {
          body += chunk
        })
        res.on('end', () => {
          try {
            resolve(res.statusCode === 200 && JSON.parse(body)?.message === 'pong')
          } catch {
            resolve(false)
          }
        })
      },
    )
    req.on('timeout', () => req.destroy())
    req.on('error', () => resolve(false))
    req.end()
  })
}

async function waitForPilot(site) {
  const started = Date.now()
  const deadline = started + PING_TIMEOUT_MS
  console.log(`erp: waiting for https://${site}/api/method/ping (first create-site: 5-15 min)...`)
  while (Date.now() < deadline) {
    if (await pilotPing(site)) {
      const secs = Math.round((Date.now() - started) / 1000)
      console.log(`\nerp: pilot is up at https://${site} (${secs}s)`)
      return 0
    }
    const mins = Math.floor((Date.now() - started) / 60_000)
    process.stdout.write(`\rerp: still waiting... ${mins} min elapsed`)
    await new Promise((r) => setTimeout(r, PING_INTERVAL_MS))
  }
  console.error(
    '\nerp: timed out waiting for the pilot. Check `pnpm erp:logs create-site` / caddy.',
  )
  return 1
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
    ensureEnvFile()
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
  async 'pilot:build'(args) {
    const tag = pilotTag()
    const revision = `org.opencontainers.image.revision=${git('rev-parse', 'HEAD')}`
    for (const [file, repo] of [
      [PILOT_CONTAINERFILE, PILOT_IMAGE_REPO],
      [TOWN_CONTAINERFILE, TOWN_IMAGE_REPO],
    ]) {
      console.log(`erp: building ${repo}:${tag}`)
      const code = await docker([
        'build',
        '-f',
        file,
        '-t',
        `${repo}:${tag}`,
        '--label',
        revision,
        ...args,
        '.',
      ])
      if (code !== 0) return code
    }
    return 0
  },
  async 'pilot:up'(args) {
    ensureEnvFile()
    const settings = envSettings()
    const missing = PILOT_REQUIRED.filter((key) => !settings[key])
    if (missing.length > 0)
      throw new Error(`pilot:up: set ${missing.join(', ')} in ${ENV_FILE} (see ${ENV_EXAMPLE})`)
    for (const [key, devValue] of Object.entries(PILOT_REFUSED)) {
      if (settings[key] === devValue)
        throw new Error(
          `pilot:up: ${key} is the dev-only default "${devValue}"; set a strong unique value in ${ENV_FILE} ` +
            '(on an existing postgres volume also ALTER ROLE, see infra/docker/pilot/RESTORE.md)',
        )
    }
    // shared services (postgres) must be up and town_pilot must exist before `town` starts
    let code = await pilotCompose('up', '-d', '--wait', PG_SERVICE)
    if (code === 0) code = await ensurePilotDatabase()
    if (code === 0) code = await pilotCompose('up', '-d', ...args)
    return code === 0 ? waitForPilot(settings.SITE_NAME) : code
  },
  'pilot:down'(args) {
    if (args.length > 0) throw new Error(`pilot:down: unexpected arguments: ${args.join(' ')}`)
    // not `compose down`: that would also stop mariadb/redis, which the dev stack shares
    return composeIn('pilot', ['rm', '--stop', '--force', ...PILOT_SERVICES])
  },
  'pilot:bench'(args) {
    if (args.length === 0)
      throw new Error('pilot:bench: missing arguments (e.g. `--site pilot.localhost list-apps`)')
    return pilotCompose('exec', ...execFlags(), 'backend', 'bench', ...args)
  },
  'pilot:backup'(args) {
    if (args.length > 0) throw new Error(`pilot:backup: unexpected arguments: ${args.join(' ')}`)
    return pilotCompose('run', '--rm', '--no-deps', '-T', 'backup', 'erp-town-backup')
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
