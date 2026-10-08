// Wraps one command (the Go integration run, `pnpm go:test:integration`) in the test-site lifecycle of
// helpers.ts, run by Node's type stripping:
//
//   node tests/integration/test-site.ts run [--scheduler] -- <cmd> [args...]
//
// Seeds test.localhost (`prepareTestSite`), runs <cmd> in the repo root with inherited stdio and env
// TEST_API_KEY / TEST_API_SECRET (Administrator, process env only), TOWN_TEST_ERP_URL, TOWN_TEST_ERP_SITE,
// TOWN_TEST_STREAM and TOWN_TEST_REDIS_URL (plus SEED_USER_PASSWORD when infra/docker/.env sets the seed's
// password), then always restores the site (`restoreTestSite`). Exits with the command's exit code. One
// wrapper per whole `go test ./...` run: a Go TestMain runs once per package and would re-seed the site
// for each.

import { spawn } from 'node:child_process'
import {
  ERP_URL,
  localEnv,
  prepareTestSite,
  ROOT,
  restoreTestSite,
  TEST_SITE,
  TEST_STREAM,
} from './helpers.ts'

const TEST_REDIS_URL = 'redis://127.0.0.1:6380/0'
const USAGE = 'usage: node tests/integration/test-site.ts run [--scheduler] -- <cmd> [args...]'

interface Invocation {
  scheduler: boolean
  cmd: string
  args: string[]
}

function parse(argv: string[]): Invocation {
  const [verb, ...rest] = argv
  const sep = rest.indexOf('--')
  if (verb !== 'run' || sep < 0) throw new Error(USAGE)
  const flags = rest.slice(0, sep)
  const [cmd, ...args] = rest.slice(sep + 1)
  const unknown = flags.filter((f) => f !== '--scheduler')
  if (!cmd || unknown.length > 0) throw new Error(USAGE)
  return { scheduler: flags.includes('--scheduler'), cmd, args }
}

function runChild(cmd: string, args: string[]): Promise<number> {
  const seedPassword = localEnv('SEED_USER_PASSWORD') // the staff users' password the seed set
  return new Promise((resolve) => {
    const child = spawn(cmd, args, {
      cwd: ROOT,
      stdio: 'inherit',
      env: {
        ...process.env,
        TOWN_TEST_ERP_URL: ERP_URL,
        TOWN_TEST_ERP_SITE: TEST_SITE,
        TOWN_TEST_STREAM: TEST_STREAM,
        TOWN_TEST_REDIS_URL: TEST_REDIS_URL,
        ...(seedPassword ? { SEED_USER_PASSWORD: seedPassword } : {}),
      },
    })
    child.on('error', (e) => {
      console.error(`[test-site] cannot start ${cmd}: ${e.message}`)
      resolve(127)
    })
    child.on('close', (code, signal) => resolve(code ?? (signal ? 1 : 0)))
  })
}

async function main(): Promise<number> {
  let inv: Invocation
  try {
    inv = parse(process.argv.slice(2))
  } catch (e) {
    console.error((e as Error).message)
    return 2
  }
  // Ctrl-C reaches the child too (same console); keep running so the site is always restored
  const ignore = () =>
    console.warn('[test-site] interrupted: waiting for the command, then restoring')
  process.on('SIGINT', ignore)
  process.on('SIGTERM', ignore)

  const started = Date.now()
  await prepareTestSite({ scheduler: inv.scheduler })
  console.log(`[test-site] ${TEST_SITE} seeded in ${Math.round((Date.now() - started) / 1000)} s`)
  let code = 1
  try {
    code = await runChild(inv.cmd, inv.args)
  } finally {
    await restoreTestSite()
    console.log(`[test-site] ${TEST_SITE} restored`)
  }
  return code
}

main().then(
  (code) => process.exit(code),
  (e: unknown) => {
    console.error('[test-site]', e instanceof Error ? e.message : e)
    process.exit(1)
  },
)
