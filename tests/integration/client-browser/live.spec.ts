import { expect, request, test } from '@playwright/test'
import { type Entity, WorldClientState } from '../../../packages/world-proto/src/index.ts'

test('real session, projection snapshot, ERP event patch, Classic link and disconnect', async ({
  page,
}) => {
  const baseURL = process.env.TOWN_CLIENT_BASE_URL
  if (!baseURL?.startsWith('http://127.0.0.1:')) throw new Error('Local test server required')
  const state = new WorldClientState('ACME-TH')
  const updates: { version: bigint; entities: Entity[] }[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('websocket', (socket) => {
    // Vite has its own text HMR socket; the world protocol applies only here.
    if (new URL(socket.url()).pathname !== '/town/ws') return
    socket.on('framereceived', ({ payload }) => {
      if (typeof payload === 'string') throw new Error('World frame must be binary')
      state.acceptBinary(payload)
      updates.push({ version: state.version, entities: state.entities() })
    })
  })
  await page.goto('/?lang=th')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('กรุณาเข้าสู่ ERP ก่อน แล้วลองเชื่อมต่อใหม่')

  const login = await page.request.post('/api/method/login', {
    data: { usr: 'sales@acme-th.test', pwd: process.env.SEED_USER_PASSWORD },
  })
  expect(login.status(), 'seeded user login').toBe(200)
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('เชื่อมต่อแล้ว', { timeout: 15000 })
  expect(state.ready, 'the first binary frame must establish a valid company snapshot').toBe(true)
  const initialVersion = state.version
  const avatar = page.getByRole('checkbox', { name: 'แสดงตัวละครของฉัน' })
  await expect(avatar).not.toBeChecked()
  const appeared = page.waitForResponse(
    (r) => r.url().endsWith('/town/api/avatar/presence') && r.request().method() === 'POST',
  )
  await avatar.check()
  expect((await appeared).status()).toBe(200)
  await expect(avatar).toBeEnabled()
  await expect(avatar).toBeChecked()
  const presence = await page.request.get('/town/api/avatar/presence?company=ACME-TH')
  expect(presence.status()).toBe(200)
  const visible = await presence.json()
  expect(visible).toHaveLength(1)
  expect(Object.keys(visible[0]).sort()).toEqual(['appearance', 'department', 'emote', 'id'])
  expect(JSON.stringify(visible)).not.toContain('sales@acme-th.test')
  const optedOut = page.waitForResponse(
    (r) => r.url().endsWith('/town/api/avatar/preference') && r.request().method() === 'PUT',
  )
  await avatar.uncheck()
  expect((await optedOut).status()).toBe(200)
  await expect(avatar).toBeEnabled()
  await expect(avatar).not.toBeChecked()
  expect(
    await (await page.request.get('/town/api/avatar/presence?company=ACME-TH')).json(),
  ).toEqual([])
  expect((await page.request.get('/town/api/avatar/preference?company=ETT')).status()).toBe(403)

  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Bangkok' }).format(new Date())
  const created = await page.request.post('/town/api/sales-orders', {
    headers: { 'X-Town-Request': '1' },
    data: {
      company: 'ACME-TH',
      customer: 'CUST-001',
      transaction_date: today,
      delivery_date: today,
      lines: [{ item_code: 'ITEM-002', qty: '1' }],
    },
  })
  expect(created.status(), 'gateway creates real ERP draft').toBe(201)
  const { name } = (await created.json()) as { name: string }
  const submitted = await page.request.post(
    `/town/api/documents/sales-order/${encodeURIComponent(name)}/submit`,
    {
      headers: { 'X-Town-Request': '1' },
    },
  )
  expect(submitted.status(), 'gateway submits real ERP order').toBe(200)
  expect((await submitted.json()).submitted).toBe(true)
  await expect
    .poll(
      () =>
        updates.some(
          (update) =>
            update.version > initialVersion &&
            update.entities.some(
              (entity) =>
                entity.key?.name === name && entity.eventType === 'erp.sales_order.submitted',
            ),
        ),
      { timeout: 20000 },
    )
    .toBe(true)
  const range = new URLSearchParams({
    company: 'ACME-TH',
    from: new Date(Date.now() - 15 * 60000).toISOString(),
    until: new Date().toISOString(),
    limit: '100',
  })
  const history = await page.request.get(`/town/api/history?${range}`)
  expect(history.status()).toBe(200)
  const retained = await history.json()
  expect(retained.transitions).toContainEqual(
    expect.objectContaining({
      name,
      doctype: 'Sales Order',
      kind: 'document',
      status: 'submitted',
    }),
  )
  for (const transition of retained.transitions) {
    expect(Object.keys(transition).sort()).toEqual(['doctype', 'kind', 'name', 'status', 'time'])
  }
  expect(JSON.stringify(retained)).not.toMatch(
    /sales@acme-th.test|CUST-001|grand_total|on_behalf_of|TownAvatar/,
  )
  range.set('company', 'ETT')
  expect((await page.request.get(`/town/api/history?${range}`)).status()).toBe(403)
  await page.getByRole('button', { name: /ร้านค้า/ }).click()
  await expect(page.getByText(name, { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: `เปิดเอกสาร ${name}` })).toHaveAttribute(
    'href',
    `/desk/sales-order/${encodeURIComponent(name)}`,
  )
  const inspection = page.locator('.inspection')
  await expect(inspection.getByText('รอส่งของและวางบิล', { exact: true })).toBeVisible()
  const link = await page.getByRole('link', { name: `เปิดเอกสาร ${name}` }).getAttribute('href')
  expect((await page.request.get(link as string)).status(), 'Classic ERP route').toBe(200)
  const fields = updates.flatMap((update) =>
    update.entities.map((entity) => new TextDecoder().decode(entity.dataJson)),
  )
  // The existing event contract allows party codes. The client must not render them.
  expect(fields.some((value) => /grand_total|net_total/.test(value))).toBe(false)
  await expect(page.getByText('CUST-001', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: 'test-results/client-live/town-connected-th.png' })
  // Replay uses the real pre-order snapshot, never another ERP mutation.
  await page
    .getByRole('combobox', { name: 'ดูสถานะย้อนหลัง', exact: true })
    .selectOption(initialVersion.toString())
  await expect(page.getByText(name, { exact: true })).toHaveCount(0)
  await expect(page.locator('.document-desk')).toHaveCount(0)
  await expect(page.locator('[data-amount]')).toHaveCount(0)
  await page.screenshot({ path: 'test-results/client-live/town-replay-th.png' })
  await page.getByRole('button', { name: 'กลับสู่ปัจจุบัน', exact: true }).click()
  await expect(page.getByText(name, { exact: true })).toBeVisible()
  await expect(page.locator('.document-desk')).toBeVisible()
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Connected')
  await expect(inspection.getByText('Awaiting delivery and billing', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Disconnected')
  await expect(page.locator('.replay-panel')).toHaveCount(0)
  await expect(page.getByText(name, { exact: true })).toHaveCount(0)
  await expect(page.locator('.department-count').first()).toHaveText('—')
  state.reset()
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Connected')
  // A reconnect clears selection together with the old permission scope.
  await page.getByRole('button', { name: /^Shop/ }).click()
  await expect(page.getByText(name, { exact: true })).toBeVisible()
  // The real Go room closes at JWT expiry. The UI must clear the old company's facts.
  await expect(page.getByRole('status')).toHaveText('Reconnect to check your access', {
    timeout: 35000,
  })
  await expect(page.getByText(name, { exact: true })).toHaveCount(0)
  await expect(page.locator('.department-count').first()).toHaveText('—')
  expect(
    await page.evaluate(() => Object.keys(localStorage).concat(Object.keys(sessionStorage))),
  ).toEqual([])
  expect(errors).toEqual([])
})

test('the document UI completes sales to cash with real departmental sessions', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/?lang=th')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  const desk = page.locator('.document-desk')
  const connect = async (user?: string) => {
    if (await page.getByRole('button', { name: 'ตัดการเชื่อมต่อ', exact: true }).count()) {
      await page.getByRole('button', { name: 'ตัดการเชื่อมต่อ', exact: true }).click()
    }
    await expect(page.locator('[data-amount]')).toHaveCount(0)
    if (user) {
      const login = await page.request.post('/api/method/login', {
        data: { usr: user, pwd: process.env.SEED_USER_PASSWORD },
      })
      expect(login.status()).toBe(200)
      const identity = await page.request.get('/town/api/me')
      expect((await identity.json()).user).toBe(user)
    }
    await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
    await expect(page.getByRole('status')).toHaveText('เชื่อมต่อแล้ว', { timeout: 15000 })
  }
  const open = async (kind: string, name: string) => {
    await desk.getByRole('combobox', { name: 'ประเภทเอกสาร', exact: true }).selectOption(kind)
    await expect(page.getByRole('status')).toHaveText('เชื่อมต่อแล้ว')
    await desk.getByLabel('เลขที่เอกสาร', { exact: true }).fill(name)
    await desk.getByRole('button', { name: 'เปิดรายการ', exact: true }).click()
    await expect(desk.getByRole('heading', { name, exact: true })).toBeVisible()
    await expect(desk.locator('.document-state')).toBeVisible()
  }
  const create = async (route: string, button: string) => {
    const pending = page.waitForResponse(
      (r) => r.url().endsWith(`/town/api/${route}`) && r.request().method() === 'POST',
    )
    await desk.getByRole('button', { name: button, exact: true }).click()
    const response = await pending
    expect(response.status(), `${route} real ERP create`).toBe(201)
    const { name } = (await response.json()) as { name: string }
    await expect(desk.getByRole('heading', { name, exact: true })).toBeVisible()
    await expect(desk.locator('.document-state')).toHaveText('ฉบับร่าง')
    return name
  }
  const submit = async () => {
    await desk.getByRole('button', { name: 'ยืนยัน / ส่งอนุมัติ', exact: true }).click()
    await expect(desk.locator('.document-state')).toHaveText('ยืนยันแล้ว', { timeout: 15000 })
  }
  await connect('sales@acme-th.test')
  await page.getByRole('button', { name: /ร้านค้า/ }).click()
  await desk.getByRole('button', { name: 'สร้างใบสั่งขาย', exact: true }).click()
  await desk.getByLabel('รหัสลูกค้า', { exact: true }).fill('CUST-001')
  await desk.getByLabel('รหัสสินค้า', { exact: true }).fill('ITEM-002')
  const so = await create('sales-orders', 'บันทึกฉบับร่าง')
  const payable = await desk.locator('[data-amount="rounded_total"]').innerText()
  expect(payable).toMatch(/^[0-9]+(\.[0-9]+)?$/)
  await submit()
  // Standard Sales User can create Delivery Notes in this ERP. Department labels
  // follow ERP policy; verify an actually unauthenticated caller is rejected.
  const anonymous = await request.newContext({ baseURL: process.env.TOWN_CLIENT_BASE_URL })
  try {
    const denied = await anonymous.post('/town/api/delivery-notes', {
      headers: { 'X-Town-Request': '1' },
      data: { company: 'ACME-TH', sales_order: so },
    })
    expect(denied.status(), 'anonymous caller cannot create a delivery').toBe(401)
  } finally {
    await anonymous.dispose()
  }

  await connect('stock@acme-th.test')
  await page.getByRole('button', { name: /โกดัง/ }).click()
  await open('sales-order', so)
  const dn = await create('delivery-notes', 'ร่างใบส่งของ')
  await expect(desk.getByText(`อ้างอิงเอกสาร: ${so}`, { exact: true })).toBeVisible()
  await submit()

  await connect('accounts@acme-th.test')
  await page.getByRole('button', { name: /ธนาคาร/ }).click()
  await open('delivery-note', dn)
  const si = await create('sales-invoices', 'ร่างใบแจ้งหนี้')
  await expect(desk.locator('[data-amount="rounded_total"]')).toHaveText(payable)
  await submit()
  await expect(desk.locator('[data-amount="outstanding"]')).toHaveText(payable)
  // Refresh the short-lived room session before collecting payment.
  await connect()
  await page.getByRole('button', { name: /ธนาคาร/ }).click()
  await open('sales-invoice', si)
  await desk.getByLabel('วิธีรับเงิน', { exact: true }).fill('เงินสด')
  const pe = await create('payment-entries', 'ร่างรายการรับเงินเต็มยอด')
  await expect(desk.locator('[data-amount="paid"]')).toHaveText(payable)
  await expect(desk.getByText(`อ้างอิงเอกสาร: ${si}`, { exact: true })).toBeVisible()
  const unpaid = await page.request.get(`/town/api/sales-invoices/${encodeURIComponent(si)}`)
  expect((await unpaid.json()).outstanding, 'payment draft does not settle invoice').toBe(payable)
  await submit()
  await open('sales-invoice', si)
  const paid = await page.request.get(`/town/api/sales-invoices/${encodeURIComponent(si)}`)
  const settled = (await paid.json()) as { outstanding: string }
  expect(typeof settled.outstanding, 'amount remains a decimal string').toBe('string')
  expect(settled.outstanding, 'real ERP invoice is settled').toMatch(/^0(\.0+)?$/)
  await expect(desk.locator('[data-amount="outstanding"]')).toHaveText(settled.outstanding)
  await expect(desk.getByRole('button', { name: 'ร่างรายการรับเงินเต็มยอด', exact: true })).toHaveCount(
    0,
  )
  const receipt = await page.request.get(`/town/api/payment-entries/${encodeURIComponent(pe)}`)
  expect(await receipt.json()).toMatchObject({
    status: 'Submitted',
    paid: payable,
    against_invoices: [si],
  })
  await desk.locator('.document-amounts').scrollIntoViewIfNeeded()
  await page.screenshot({ path: 'test-results/client-live/town-invoice-settled-th.png' })
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(desk.getByText('Outstanding', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.locator('[data-amount]')).toHaveCount(0)
  await expect(page.locator('.document-detail')).toHaveCount(0)
  expect(
    await page.evaluate(() => Object.keys(localStorage).concat(Object.keys(sessionStorage))),
  ).toEqual([])
  expect(errors).toEqual([])
})
