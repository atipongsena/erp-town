import { expect, test } from '@playwright/test'

test('desktop showcase keeps its controls beside the town with a full-height sidebar', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 900 })
  await page.goto('/?mode=demo&lang=en')
  const town = await page.locator('.town-view').boundingBox()
  const controls = await page.locator('.sidebar').boundingBox()
  const workspace = await page.locator('.workspace').boundingBox()
  expect(town).not.toBeNull()
  expect(controls).not.toBeNull()
  expect(workspace).not.toBeNull()
  expect(controls?.height ?? 0).toBeGreaterThan((workspace?.height ?? 0) * 0.8)
  expect(controls?.x ?? 0).toBeGreaterThanOrEqual((town?.x ?? 0) + (town?.width ?? 0) - 1)
})

test('offline showcase runs concurrent staff and a looping truck without backend requests', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  const backend: string[] = []
  page.on('request', (request) => {
    if (/\/town\/(api|ws)|\/api\/|\/login|\/desk/.test(request.url())) backend.push(request.url())
  })
  await page.goto('/?mode=demo&lang=en')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  await expect(
    page.getByText('Concurrent demo motion · no ERP writes', { exact: true }),
  ).toBeVisible()
  await expect(page.locator('[data-work-actor]')).toHaveCount(4)
  const truck = page.locator('[data-delivery-distance]')
  await expect(truck).toHaveCount(1)
  const distance = await truck.getAttribute('data-delivery-distance')
  await expect.poll(() => truck.getAttribute('data-delivery-distance')).not.toBe(distance)
  await page.getByLabel('View as').selectOption('warehouse')
  await expect(page.locator('[data-work-actor]')).toHaveCount(0)
  await expect(truck).toHaveCount(0)
  expect(backend).toEqual([])
})

test('demo characters and handoffs keep drawing while idle, and motion can be paused', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.addInitScript(() => {
    const probe = { frames: 0, enabled: false }
    Object.assign(window, { demoMotionProbe: probe })
    const draw = WebGL2RenderingContext.prototype.drawElements
    window.requestAnimationFrame = (
      (original) => (callback: FrameRequestCallback) =>
        original((time) => {
          if (probe.enabled) probe.frames++
          callback(time)
        })
    )(window.requestAnimationFrame.bind(window))
    Object.assign(probe, { draws: 0 })
    WebGL2RenderingContext.prototype.drawElements = function (...args) {
      if (probe.enabled) (probe as typeof probe & { draws: number }).draws++
      return draw.apply(this, args)
    }
  })
  await page.goto('/?mode=demo&lang=en')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  await page.waitForTimeout(1800)
  await page.evaluate(() => {
    ;(window as unknown as { demoMotionProbe: { enabled: boolean } }).demoMotionProbe.enabled = true
  })
  const initial = await page.locator('canvas').screenshot()
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as unknown as { demoMotionProbe: { draws: number } }).demoMotionProbe.draws,
      ),
    )
    .toBeGreaterThan(1000)
  await page.waitForTimeout(500)
  expect((await page.locator('canvas').screenshot()).equals(initial)).toBe(false)
  await page.getByRole('checkbox', { name: 'Town animation', exact: true }).uncheck()
  await page.waitForTimeout(1800)
  await page.evaluate(() => {
    ;(window as unknown as { demoMotionProbe: { draws: number } }).demoMotionProbe.draws = 0
  })
  await page.waitForTimeout(600)
  expect(
    await page.evaluate(
      () => (window as unknown as { demoMotionProbe: { draws: number } }).demoMotionProbe.draws,
    ),
  ).toBe(0)
})

test('people and AI complete a demo handoff without live API or socket calls', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => {
    if (/\/town\/(api|ws)|\/api\/|\/login|\/desk/.test(request.url())) requests.push(request.url())
  })
  let sockets = 0
  page.on('websocket', (socket) => {
    // Vite's hot-reload socket is unrelated to the world connection.
    if (new URL(socket.url()).pathname.startsWith('/town/')) sockets++
  })
  await page.goto('/?mode=demo&lang=th&connect=1')
  await expect(page.getByText('โหมดจำลอง', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'เตรียมร่างและส่งต่อ' })).toBeDisabled()
  const role = page.getByLabel('ดูในบทบาท')
  await role.selectOption('ai')
  await page.getByRole('button', { name: 'เตรียมร่างและส่งต่อ' }).click()
  await expect(page.getByRole('button', { name: 'ตรวจและยืนยัน', exact: true })).toBeDisabled()
  await role.selectOption('manager')
  await page.getByRole('button', { name: 'ตรวจและยืนยัน', exact: true }).click()
  await role.selectOption('warehouse')
  await page.getByRole('button', { name: 'ตรวจและยืนยัน', exact: true }).click()
  await expect(page.getByText('DEMO-DN-001', { exact: true })).toBeVisible()
  await expect(page.getByText('฿2,140.00', { exact: true })).toHaveCount(0)
  await role.selectOption('ai')
  await page.getByRole('button', { name: 'เตรียมร่างและส่งต่อ' }).click()
  await expect(page.getByText('DEMO-SI-001', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'ตรวจและยืนยัน', exact: true })).toBeDisabled()
  await role.selectOption('accountant')
  await page.getByRole('button', { name: 'ตรวจและยืนยัน', exact: true }).click()
  await role.selectOption('ai')
  await expect(page.getByRole('button', { name: 'ตรวจหลักฐานและรับเงิน' })).toBeDisabled()
  await role.selectOption('accountant')
  await page.getByRole('button', { name: 'ตรวจหลักฐานและรับเงิน' }).click()
  await expect(page.getByText('DEMO-PE-001', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'งานนี้เสร็จแล้ว' })).toBeVisible()
  await role.selectOption('warehouse')
  await expect(page.getByText('บทบาทนี้ไม่มีสิทธิดูข้อมูลในแผนกนี้', { exact: true })).toBeVisible()
  await expect(page.getByText('DEMO-PE-001', { exact: true })).toHaveCount(0)
  await expect(page.getByText('DEMO-SI-001', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await page.getByRole('button', { name: 'Access & permissions', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'This role’s permissions' })).toBeVisible()
  await expect(
    page.getByText('Prices, tax and totals are hidden; only dispatch information is shown.'),
  ).toBeVisible()
  await expect(page.getByRole('link', { name: 'Live workspace' })).toHaveAttribute(
    'href',
    '/?lang=en',
  )
  expect(requests).toEqual([])
  expect(sockets).toBe(0)
})

test('all departments, playback, reset and role changes work at phone width', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/?mode=demo&lang=en')
  await expect(
    page.getByRole('navigation', { name: 'Around town' }).getByRole('button'),
  ).toHaveCount(6)
  await page.getByRole('button', { name: 'Play story', exact: true }).click()
  await expect(page.locator('progress')).toHaveAttribute('value', '2', { timeout: 10000 })
  await page.getByRole('button', { name: 'Pause', exact: true }).click()
  await page.getByRole('button', { name: 'Reset', exact: true }).click()
  await expect(page.locator('progress')).toHaveAttribute('value', '1')
  await page.getByRole('button', { name: 'Access & permissions', exact: true }).click()
  await expect(page.locator('.demo-rights tbody tr')).toHaveCount(6)
  for (const row of await page.locator('.demo-rights tbody tr').all()) {
    await expect(row.locator('td').first()).toHaveText('Yes')
    await expect(row.locator('td').nth(1)).toHaveText('—')
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})
