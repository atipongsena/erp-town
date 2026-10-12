// Isolated ERP acceptance, deterministic provider, no external model requests.
import { spawn } from 'node:child_process'
import {
  benchExecute,
  ERP_URL,
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
        `[agent-acceptance] ${signal}: waiting for the bounded test before restoring the site`,
      )
    })
  }
  await prepareTestSite({ scheduler: false })
  console.log('[agent-acceptance] test.localhost snapshot saved and seeded')
  try {
    await benchExecute('erp_town.tests.factories.ensure_test_company')
    const keys = await benchExecute<{
      agent_key: string
      agent: { key: string; secret: string }
      delegator: { key: string; secret: string }
    }>('erp_town.setup.testing.issue_agent_acceptance_keys')
    if (!keys?.agent.key || !keys.agent.secret || !keys.delegator.key || !keys.delegator.secret)
      throw new Error('Isolated credentials unavailable')
    process.exitCode = await new Promise<number>((resolve, reject) => {
      const child = spawn(
        'go',
        [
          'test',
          '-race',
          '-tags',
          'integration',
          './internal/e2e',
          '-run',
          '^TestAgentLive$',
          '-count=1',
          '-v',
          '-timeout=3m',
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
            TOWN_TEST_AGENT_KEYS: JSON.stringify(keys),
          },
        },
      )
      child.once('error', reject)
      child.once('close', (code) => resolve(code ?? 1))
    })
  } finally {
    await restoreTestSite()
    console.log('[agent-acceptance] test.localhost database restored')
  }
}
main().catch(() => {
  console.error('[agent-acceptance] failed; credentials and ERP responses suppressed')
  process.exitCode = 1
})
