import { expect, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

test('explicit avatar consent uses the real skinned asset and opt-out removes presence', async ({
  page,
}) => {
  let consent = false
  let posted = 0
  let humanLoads = 0
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('request', (r) => {
    if (r.url().endsWith('/art/town-human-01.glb')) humanLoads++
  })
  await page.route('**/town/api/me', (r) =>
    r.fulfill({ json: { user: 'test', companies: ['ACME'], roles: [] } }),
  )
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from(wire.snapshot, 'hex'))),
  )
  await page.route('**/town/api/avatar/preference**', (r) => {
    if (r.request().method() === 'PUT') {
      const body = r.request().postDataJSON()
      expect(Object.keys(body).sort()).toEqual(['appearance', 'company', 'consent'])
      consent = body.consent
      return r.fulfill({ json: { ok: true } })
    }
    return r.fulfill({ json: { consent, appearance: 'human-blue' } })
  })
  await page.route('**/town/api/avatar/presence**', (r) => {
    if (r.request().method() === 'POST') {
      posted++
      expect(r.request().postDataJSON()).toEqual({
        company: 'ACME',
        department: 'hq',
        emote: 'idle',
      })
      return r.fulfill({ json: { ok: true } })
    }
    return r.fulfill({
      json: consent
        ? [
            {
              id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
              department: 'hq',
              emote: 'idle',
              appearance: 'human-blue',
            },
          ]
        : [],
    })
  })
  await page.goto('/?lang=en')
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  const checkbox = page.getByRole('checkbox', { name: 'Show my avatar' })
  await expect(checkbox).not.toBeChecked()
  expect(posted).toBe(0)
  await checkbox.check()
  await expect.poll(() => posted).toBe(1)
  await expect.poll(() => humanLoads).toBe(1)
  await page.waitForTimeout(400)
  await page.screenshot({ path: 'test-results/town-avatar-en.png' })
  await checkbox.uncheck()
  await expect(checkbox).not.toBeChecked()
  expect(consent).toBe(false)
  expect(errors).toEqual([])
})
