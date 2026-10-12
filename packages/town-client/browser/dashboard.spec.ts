import { expect, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

for (const size of [
  { width: 1366, height: 768 },
  { width: 1920, height: 1080 },
]) {
  test(`game HUD at ${size.width} has real counts, clear selection and background input`, async ({
    page,
  }) => {
    await page.setViewportSize(size)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.route('**/town/api/me', (r) => r.fulfill({ json: { companies: ['ACME'] } }))
    await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
    await page.route('**/town/api/avatar/preference**', (r) =>
      r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
    )
    await page.routeWebSocket('**/town/ws', (s) =>
      s.onMessage(() => s.send(Buffer.from(wire.snapshot, 'hex'))),
    )
    await page.goto('/?lang=en&connect=1')
    await expect(page.getByRole('status')).toHaveText('Connected')
    const hud = page.locator('.town-dashboard')
    await expect(hud).toBeVisible()
    await expect(hud.locator('[data-kpi="documents"]')).toHaveText('1')
    await hud.getByRole('button', { name: 'Select SO-TEST', exact: true }).click()
    await expect(hud.locator('[data-selection]')).toContainText('SO-TEST')
    const boxes = await page
      .locator('.hud-kpis,.hud-flow,.hud-queue,.sidebar')
      .evaluateAll((nodes) =>
        nodes.map((n) => {
          const b = n.getBoundingClientRect()
          return { x: b.x, y: b.y, w: b.width, h: b.height }
        }),
      )
    for (let i = 0; i < boxes.length; i++)
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i],
          b = boxes[j]
        if (!a || !b) throw new Error('Panel bounds missing')
        expect(a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y).toBe(false)
      }
    expect(await hud.evaluate((n) => getComputedStyle(n).pointerEvents)).toBe('none')
    await page.screenshot({ path: test.info().outputPath(`dashboard-${size.width}-en.png`) })
    await page.getByRole('button', { name: 'ไทย', exact: true }).click()
    await expect(hud).toContainText('เอกสารที่มองเห็น')
    await page.screenshot({ path: test.info().outputPath(`dashboard-${size.width}-th.png`) })
  })
}

test('narrow layout keeps selection, work and document controls in one collapsible drawer', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/?lang=en')
  await expect(page.locator('.sidebar .hud-flow')).toBeVisible()
  await expect(page.locator('.sidebar .hud-queue')).toBeVisible()
  await expect(page.locator('.sidebar .hud-selection')).toBeVisible()
  await page.getByRole('button', { name: 'Close work drawer', exact: true }).click()
  await expect(page.locator('.sidebar')).toBeHidden()
  await page.getByRole('button', { name: 'Open work drawer', exact: true }).click()
  await expect(page.locator('.sidebar')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
})

test('scene and queue share actor selection; manual camera movement ends follow', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.route('**/town/api/me', (r) => r.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.route('**/town/api/avatar/preference**', (r) =>
    r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  await page.route('**/town/api/live-demo', (r) =>
    r.fulfill({
      json: {
        enabled: true,
        mode: 'erp-test',
        company: 'ACME',
        round: 1,
        stage: 'sales-review',
        phase: 'working',
        documents: [],
        actors: [{ id: 'shared-actor', kind: 'human', department: 'shop', activity: 'working' }],
      },
    }),
  )
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from(wire.snapshot, 'hex'))),
  )
  await page.goto('/?lang=en&liveDemo=1')
  await expect(page.locator('[data-work-actor=shared-actor]')).toBeVisible()
  await page.getByRole('button', { name: 'Select actor shared-actor', exact: true }).click()
  await expect(page.locator('.hud-selection')).toContainText('shared-actor')
  await page.getByRole('button', { name: 'Follow in scene', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Stop following', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  const canvas = page.locator('canvas[data-scene-ready=true]')
  const box = await canvas.boundingBox()
  if (!box) throw new Error('Canvas bounds unavailable')
  await page.mouse.move(box.x + box.width * 0.48, box.y + box.height * 0.6)
  await page.mouse.wheel(0, 100)
  await expect(page.getByRole('button', { name: 'Follow in scene', exact: true })).toHaveAttribute(
    'aria-pressed',
    'false',
  )
  await page.getByRole('button', { name: 'Clear selection', exact: true }).click()
  await page.locator('[data-work-actor=shared-actor]').click()
  await expect(page.locator('.hud-selection')).toContainText('shared-actor')
  await page.getByRole('button', { name: 'Expand', exact: true }).click()
  await expect(page.locator('.sidebar')).toBeHidden()
  await page.getByRole('button', { name: 'Collapse', exact: true }).click()
  await expect(page.locator('.sidebar')).toBeVisible()
})
