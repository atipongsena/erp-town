import assert from 'node:assert/strict'
import { createServer } from 'node:net'
import test from 'node:test'
import { availablePort, workspaceEnvironment } from './dev-workspace.ts'

test('selects another loopback port without stopping an existing program', async () => {
  const occupied = createServer()
  await new Promise<void>((resolve) => occupied.listen(0, '127.0.0.1', resolve))
  try {
    const address = occupied.address()
    assert.ok(address && typeof address !== 'string')
    const selected = await availablePort([address.port, 0])
    assert.notEqual(selected, address.port)
    assert.equal(occupied.listening, true)
  } finally {
    await new Promise<void>((resolve) => occupied.close(() => resolve()))
  }
})

test('world mode requires its own reader and keeps credentials out of the browser', () => {
  const local = { TOWN_JWT_PRIVATE_KEY: 'fixture-secret' }
  assert.throws(() => workspaceEnvironment(local, 8091, true), /reader/)
  const environment = workspaceEnvironment(
    {
      ...local,
      TOWN_WORLD_API_KEY: 'reader-key',
      TOWN_WORLD_API_SECRET: 'reader-secret',
    },
    8091,
    true,
  )
  assert.equal(environment.town.TOWN_ROLES, 'gateway,outbox,world')
  assert.equal(environment.town.TOWN_EXPERIENCE_ENABLED, 'true')
  assert.equal(environment.town.TOWN_WORLD_API_KEY, 'reader-key')
  assert.equal(JSON.stringify(environment.frontend).includes('reader'), false)
  for (const database of [
    'postgres://town@remote.test:5433/town',
    'postgres://town@127.0.0.1:5433/town_pilot',
    'postgres://town@127.0.0.1:5433/town?host=remote.test',
  ]) {
    assert.throws(
      () =>
        workspaceEnvironment(
          {
            ...local,
            TOWN_WORLD_API_KEY: 'key',
            TOWN_WORLD_API_SECRET: 'secret',
            TOWN_DATABASE_URL: database,
          },
          8091,
          true,
        ),
      /approved local/,
    )
  }
})

test('the connection launcher cannot migrate data or pass secrets to Vite', () => {
  const environment = workspaceEnvironment(
    {
      TOWN_JWT_PRIVATE_KEY: 'fixture-secret',
      TOWN_WORLD_API_KEY: 'reader-key',
      TOWN_DATABASE_URL: 'private-database',
      TOWN_ROLES: 'world,outbox',
    },
    8091,
  )
  assert.equal(environment.town.TOWN_ROLES, 'gateway')
  assert.equal(environment.town.TOWN_HTTP_ADDR, '127.0.0.1:8091')
  assert.equal(environment.town.TOWN_JWT_PRIVATE_KEY, 'fixture-secret')
  assert.equal(environment.frontend.TOWN_DEV_GATEWAY_URL, 'http://127.0.0.1:8091')
  assert.equal(environment.frontend.TOWN_DEV_ERP_SITE, 'town.localhost')
  assert.equal(JSON.stringify(environment.frontend).includes('fixture-secret'), false)
  assert.equal('TOWN_DATABASE_URL' in environment.town, false)
  assert.equal('TOWN_WORLD_API_KEY' in environment.town, false)
})
