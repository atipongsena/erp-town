// Local-only browser/Go/ERP acceptance. No credentials are written to disk.
// Run: pnpm build:client, then node tests/integration/client-acceptance.ts
import { spawn } from 'node:child_process'
import {
  benchExecute,
  ERP_URL,
  localEnv,
  prepareTestSite,
  ROOT,
  restoreTestSite,
  TEST_SITE,
  TEST_STREAM,
} from './helpers.ts'

async function main() {
  for (const signal of ['SIGINT', 'SIGTERM'] as const) {
    process.on(signal, () => {
      console.warn(
        `[client-acceptance] ${signal}: waiting for the bounded test before restoring the site`,
      )
    })
  }
  await prepareTestSite({ scheduler: true })
  console.log('[client-acceptance] test.localhost snapshot saved and seeded')
  try {
    // An actual second company makes the permission denial meaningful.
    await benchExecute('erp_town.tests.factories.ensure_test_company')
    const reader = await benchExecute<{ user: string; key: string; secret: string }>(
      'erp_town.setup.testing.issue_client_projection_keys',
    )
    if (!reader?.key || !reader.secret) throw new Error('Test projection reader setup failed')
    const code = await new Promise<number>((resolve, reject) => {
      const child = spawn(
        'go',
        [
          'test',
          '-race',
          '-tags',
          'integration',
          './internal/e2e',
          '-run',
          '^TestClientLive$',
          '-count=1',
          '-v',
          '-timeout=5m',
        ],
        {
          cwd: `${ROOT}/go`,
          stdio: 'inherit',
          env: {
            ...process.env,
            TOWN_TEST_ERP_URL: ERP_URL,
            TOWN_TEST_ERP_SITE: TEST_SITE,
            TOWN_TEST_STREAM: TEST_STREAM,
            TOWN_TEST_REDIS_URL: 'redis://127.0.0.1:6380/0',
            TOWN_TEST_WORLD_KEY: reader.key,
            TOWN_TEST_WORLD_SECRET: reader.secret,
            TOWN_TEST_NODE: process.execPath,
            SEED_USER_PASSWORD: localEnv('SEED_USER_PASSWORD') ?? 'town-dev-123',
          },
        },
      )
      child.once('error', reject)
      child.once('close', (status) => resolve(status ?? 1))
    })
    process.exitCode = code
  } finally {
    await restoreTestSite()
    console.log('[client-acceptance] test.localhost database restored')
  }
}

main().catch(() => {
  // Setup errors may include credentials from an ERP response. Never echo them.
  console.error(
    '[client-acceptance] failed; inspect the failing check without exposing credentials',
  )
  process.exitCode = 1
})
