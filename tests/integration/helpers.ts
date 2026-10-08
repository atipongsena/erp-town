// Shared helpers of the integration (Vitest) and e2e (Playwright) suites. They run against the dev stack
// (`pnpm erp:up`) on the test site `test.localhost`, never on the dev site `town.localhost`.
//
// - `erp(path, init)`: REST call to http://127.0.0.1:8080 for test.localhost (header `X-Frappe-Site-Name`)
//   authenticated as Administrator with `TEST_API_KEY`/`TEST_API_SECRET` from the process environment.
// - `readStream(fromId)` / `waitForEvents(...)`: the town stream `town:test:events` on redis-town
//   (127.0.0.1:6380); the suites point test.localhost's Town Settings at it so the dev stream
//   `town:events` is never touched.
// - `compose(...args)`: `docker compose -f infra/docker/compose.yaml --profile dev ...`.
// - `prepareTestSite()` / `restoreTestSite()`: snapshot test.localhost's database, seed it and issue API
//   keys; afterwards restore the snapshot, so `pnpm erp:test` keeps running on an unseeded site.

import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { TownEventEnvelope } from '@erp-town/event-schema'
import { Redis } from 'ioredis'

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
export const ERP_URL = 'http://127.0.0.1:8080'
export const TEST_SITE = 'test.localhost'
export const TEST_STREAM = 'town:test:events'
const COMPOSE_FILE = 'infra/docker/compose.yaml'
const ENV_FILE = 'infra/docker/.env'
const BENCH_DIR = '/home/frappe/frappe-bench'
// inside the bench container (bench-data volume): survives container restarts, so an aborted run is undone
// by the next one
const SNAPSHOT = `${BENCH_DIR}/sites/${TEST_SITE}/private/backups/erp-town-suite-snapshot.sql`

// --- seeded master data (erp_town/setup/seed.py) ---------------------------------------------------------
export const COMPANY = 'บริษัท ตัวอย่างเทรดดิ้ง จำกัด'
export const WAREHOUSE_NAME = 'คลังหลัก'
export const PRICE_LIST = 'ขายมาตรฐาน'
export const CURRENCY = 'THB'
export const CASH = 'เงินสด'
export const ACTION_CONFIRM = 'ยืนยัน' // Sales Order workflow `อนุมัติ SO เกินวงเงิน`: Draft → submitted
export const SALES_FLOW_TYPES = [
  'erp.sales_order.submitted',
  'erp.delivery_note.submitted',
  'erp.sales_invoice.submitted',
  'erp.payment_entry.submitted',
] as const

// --- processes -----------------------------------------------------------------------------------------

export interface RunResult {
  code: number
  stdout: string
  stderr: string
}

/** Run a command in the repo root, capturing its output (never echoed: it may hold credentials). */
export function run(cmd: string, args: string[], { check = true } = {}): Promise<RunResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { cwd: ROOT, stdio: ['ignore', 'pipe', 'pipe'] })
    const out: Buffer[] = []
    const err: Buffer[] = []
    child.stdout.on('data', (b: Buffer) => out.push(b))
    child.stderr.on('data', (b: Buffer) => err.push(b))
    child.on('error', reject)
    child.on('close', (code) => {
      const result = {
        code: code ?? 1,
        stdout: Buffer.concat(out).toString('utf8'),
        stderr: Buffer.concat(err).toString('utf8'),
      }
      if (check && result.code !== 0) {
        const tail = result.stderr.trim().split('\n').slice(-15).join('\n')
        reject(new Error(`\`${cmd} ${args[0] ?? ''}…\` exited with ${result.code}:\n${tail}`))
      } else resolve(result)
    })
  })
}

/** `docker compose -f infra/docker/compose.yaml --profile dev [--env-file infra/docker/.env] ...args` */
export function compose(...args: string[]): Promise<RunResult> {
  const envFile = existsSync(path.join(ROOT, ENV_FILE)) ? ['--env-file', ENV_FILE] : []
  return run('docker', ['compose', '-f', COMPOSE_FILE, '--profile', 'dev', ...envFile, ...args])
}

/** `bench --site test.localhost <args>` through scripts/erp.mjs. */
export function bench(...args: string[]): Promise<RunResult> {
  return run(process.execPath, ['scripts/erp.mjs', 'bench', '--site', TEST_SITE, ...args])
}

/** `bench --site test.localhost execute <method> [--kwargs]`; returns the method's JSON result. */
export async function benchExecute<T = unknown>(
  method: string,
  kwargs?: Record<string, unknown>,
): Promise<T | undefined> {
  const extra = kwargs ? ['--kwargs', JSON.stringify(kwargs)] : []
  const { stdout } = await bench('execute', method, ...extra)
  // `bench execute` prints the return value as JSON on the last line (after anything the method printed)
  const last = stdout.trim().split('\n').at(-1)?.trim()
  if (!last) return undefined
  try {
    return JSON.parse(last) as T
  } catch {
    return last as T // a plain string result is printed as is
  }
}

/** bash script in the bench container (bench dir as cwd). */
function benchShell(script: string): Promise<RunResult> {
  return compose('exec', '-T', 'bench', 'bash', '-c', `cd ${BENCH_DIR} && ${script}`)
}

/** Value of `key` in the environment, else in the local infra/docker/.env (git-ignored). */
export function localEnv(key: string): string | undefined {
  if (process.env[key]) return process.env[key]
  const file = path.join(ROOT, ENV_FILE)
  if (!existsSync(file)) return undefined
  for (const line of readFileSync(file, 'utf8').split(/\r?\n/)) {
    const m = /^\s*([A-Z0-9_]+)\s*=\s*(.*)\s*$/.exec(line)
    if (m?.[1] === key) return m[2]?.replace(/^(['"])(.*)\1$/, '$2')
  }
  return undefined
}

// --- REST ----------------------------------------------------------------------------------------------

export type Doc = Record<string, unknown> & { doctype: string; name: string }

export class ErpError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

/** fetch `ERP_URL + path` on test.localhost as Administrator (token from TEST_API_KEY/TEST_API_SECRET). */
export function erp(pathname: string, init: RequestInit = {}): Promise<Response> {
  const key = process.env.TEST_API_KEY
  const secret = process.env.TEST_API_SECRET
  if (!key || !secret)
    throw new Error('TEST_API_KEY/TEST_API_SECRET not set: run through the suite setup')
  const headers = new Headers(init.headers)
  headers.set('X-Frappe-Site-Name', TEST_SITE)
  headers.set('Authorization', `token ${key}:${secret}`)
  headers.set('Accept', 'application/json')
  if (init.body !== undefined && !headers.has('Content-Type'))
    headers.set('Content-Type', 'application/json')
  return fetch(ERP_URL + pathname, { ...init, headers })
}

async function body<T>(res: Response): Promise<T> {
  const text = await res.text()
  let parsed: Record<string, unknown> = {}
  try {
    parsed = JSON.parse(text)
  } catch {
    // not JSON: reported below
  }
  if (!res.ok) {
    const messages = typeof parsed._server_messages === 'string' ? parsed._server_messages : ''
    const exception = typeof parsed.exception === 'string' ? parsed.exception : text.slice(0, 500)
    throw new ErpError(res.status, `${res.status} ${exception} ${messages}`.trim())
  }
  return parsed as T
}

/** POST /api/method/<method> with a JSON body; returns `message`. */
export async function call<T = unknown>(method: string, args: Record<string, unknown> = {}) {
  const res = await erp(`/api/method/${method}`, { method: 'POST', body: JSON.stringify(args) })
  return (await body<{ message: T }>(res)).message
}

const resource = (doctype: string, name?: string) =>
  `/api/resource/${encodeURIComponent(doctype)}${name === undefined ? '' : `/${encodeURIComponent(name)}`}`

export async function getDoc(doctype: string, name: string): Promise<Doc> {
  return (await body<{ data: Doc }>(await erp(resource(doctype, name)))).data
}

export async function insertDoc(doc: Record<string, unknown> & { doctype: string }): Promise<Doc> {
  const res = await erp(resource(doc.doctype), { method: 'POST', body: JSON.stringify(doc) })
  return (await body<{ data: Doc }>(res)).data
}

export async function updateDoc(
  doctype: string,
  name: string,
  values: Record<string, unknown>,
): Promise<Doc> {
  const res = await erp(resource(doctype, name), { method: 'PUT', body: JSON.stringify(values) })
  return (await body<{ data: Doc }>(res)).data
}

/** Submit a draft (no workflow): `docstatus` 0 → 1 through a save. */
export const submitDoc = (doc: Doc) => updateDoc(doc.doctype, doc.name, { docstatus: 1 })

export async function getList<T = Record<string, unknown>>(
  doctype: string,
  {
    filters = {},
    fields = ['name'],
    limit = 0,
    orderBy,
  }: { filters?: unknown; fields?: string[]; limit?: number; orderBy?: string } = {},
): Promise<T[]> {
  const q = new URLSearchParams({
    filters: JSON.stringify(filters),
    fields: JSON.stringify(fields),
    limit_page_length: String(limit),
  })
  if (orderBy) q.set('order_by', orderBy)
  return (await body<{ data: T[] }>(await erp(`${resource(doctype)}?${q}`))).data
}

export const getCount = (doctype: string, filters: unknown = {}) =>
  call<number>('frappe.client.get_count', { doctype, filters })

// --- town stream ---------------------------------------------------------------------------------------

export type TownEvent = TownEventEnvelope & Record<string, unknown>
export interface StreamEntry {
  streamId: string
  raw: string
  event: TownEvent
}

let redis: Redis | undefined

/** Shared client for redis-town at 127.0.0.1:6380 (reconnects on its own after an outage). */
export function townRedis(): Redis {
  if (!redis) {
    redis = new Redis({ host: '127.0.0.1', port: 6380, maxRetriesPerRequest: 2 })
    redis.on('error', () => {}) // connection errors surface on the commands that need it
  }
  return redis
}

/** A new connection for blocking reads (XREAD BLOCK would hold up the shared client). */
export function newTownRedis(): Redis {
  const client = new Redis({ host: '127.0.0.1', port: 6380, maxRetriesPerRequest: 2 })
  client.on('error', () => {})
  return client
}

export async function closeTownRedis(): Promise<void> {
  const client = redis
  redis = undefined
  if (client) await client.quit().catch(() => client.disconnect())
}

/** Id of the newest entry of the stream (`0-0` when empty): read with `readStream` from here. */
export async function streamTail(stream = TEST_STREAM): Promise<string> {
  const last = await townRedis().xrevrange(stream, '+', '-', 'COUNT', 1)
  return last[0]?.[0] ?? '0-0'
}

function parseEntries(entries: [string, string[]][]): StreamEntry[] {
  return entries.map(([streamId, fields]) => {
    const i = fields.indexOf('event')
    const raw = i >= 0 ? (fields[i + 1] ?? '') : ''
    return { streamId, raw, event: JSON.parse(raw) as TownEvent }
  })
}

/** Every entry after `fromId` (exclusive). */
export async function readStream(fromId: string, stream = TEST_STREAM): Promise<StreamEntry[]> {
  return parseEntries(await townRedis().xrange(stream, `(${fromId}`, '+'))
}

/** Poll the stream after `fromId` until `done(entries)`; returns the entries (throws on timeout). */
export async function waitForEvents(
  fromId: string,
  done: (entries: StreamEntry[]) => boolean,
  { timeoutMs = 5_000, intervalMs = 100, stream = TEST_STREAM } = {},
): Promise<StreamEntry[]> {
  const deadline = Date.now() + timeoutMs
  let lastError: unknown
  for (;;) {
    try {
      const entries = await readStream(fromId, stream)
      if (done(entries)) return entries
    } catch (e) {
      lastError = e // e.g. redis-town still starting after an outage
    }
    if (Date.now() > deadline)
      throw new Error(
        `stream ${stream}: condition not met within ${timeoutMs} ms${lastError ? ` (last error: ${lastError})` : ''}`,
      )
    await sleep(intervalMs)
  }
}

/**
 * Block on the stream after `fromId` until an entry matches; resolves with it and `performance.now()` at
 * the moment it was read. Uses its own connection (`client`), so several reads can wait at once.
 */
export async function nextMatchingEntry(
  client: Redis,
  fromId: string,
  match: (e: TownEvent) => boolean,
  { timeoutMs = 5_000, stream = TEST_STREAM } = {},
): Promise<{ entry: StreamEntry; seenAt: number }> {
  const deadline = Date.now() + timeoutMs
  let last = fromId
  while (Date.now() < deadline) {
    const block = Math.max(1, Math.min(1_000, deadline - Date.now()))
    const res = await client.xread('BLOCK', block, 'STREAMS', stream, last)
    const seenAt = performance.now()
    for (const [, entries] of res ?? []) {
      for (const entry of parseEntries(entries)) {
        last = entry.streamId
        if (match(entry.event)) return { entry, seenAt }
      }
    }
  }
  throw new Error(`stream ${stream}: no matching event within ${timeoutMs} ms`)
}

// --- misc ----------------------------------------------------------------------------------------------

/** Log measurements and keep them in test-results/metrics/<name>.json (git-ignored) for the run report. */
export function reportMetrics(name: string, metrics: Record<string, unknown>): void {
  const dir = path.join(ROOT, 'test-results', 'metrics')
  mkdirSync(dir, { recursive: true })
  const record = { name, at: new Date().toISOString(), ...metrics }
  writeFileSync(
    path.join(dir, `${name}.json`),
    `${JSON.stringify(record, null, 2)}
`,
  )
  console.log(`[${name}]`, JSON.stringify(metrics))
}

export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** Nearest-rank percentile (`p` in 0..100). */
export function percentile(values: number[], p: number): number {
  if (values.length === 0) return Number.NaN
  const sorted = [...values].sort((a, b) => a - b)
  const rank = Math.ceil((p / 100) * sorted.length)
  return sorted[Math.min(sorted.length, Math.max(1, rank)) - 1] as number
}

export async function waitUntil<T>(
  probe: () => Promise<T>,
  ok: (value: T) => boolean,
  {
    timeoutMs,
    intervalMs = 1_000,
    what = 'condition',
  }: { timeoutMs: number; intervalMs?: number; what?: string },
): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const value = await probe()
    if (ok(value)) return value
    if (Date.now() > deadline)
      throw new Error(`${what} not reached within ${timeoutMs} ms (last: ${JSON.stringify(value)})`)
    await sleep(intervalMs)
  }
}

// --- sales flow over REST (seeded company, as Administrator) -------------------------------------------

export interface OrderOptions {
  customer?: string
  /** `rate` defaults to the price list rate on the document date (seeded prices are valid from the seed day) */
  items?: [itemCode: string, qty: number, rate?: number][]
  /** document date (YYYY-MM-DD) of every document of the flow; default today */
  date?: string
}

let cachedWarehouse: string | undefined
let cachedTaxTemplate: string | undefined | null

export async function seededWarehouse(): Promise<string> {
  if (!cachedWarehouse) {
    const [wh] = await getList<{ name: string }>('Warehouse', {
      filters: { warehouse_name: WAREHOUSE_NAME, company: COMPANY },
    })
    if (!wh) throw new Error('seeded warehouse not found: did the seed run?')
    cachedWarehouse = wh.name
  }
  return cachedWarehouse
}

async function salesTaxTemplate(): Promise<string | null> {
  if (cachedTaxTemplate === undefined) {
    const [t] = await getList<{ name: string }>('Sales Taxes and Charges Template', {
      filters: { company: COMPANY, is_default: 1, disabled: 0 },
    })
    cachedTaxTemplate = t?.name ?? null
  }
  return cachedTaxTemplate
}

const posting = (date?: string) =>
  date ? { set_posting_time: 1, posting_date: date, posting_time: '10:00:00' } : {}

/** Draft Sales Order for a seeded customer without a credit limit (so `ยืนยัน` submits it directly). */
export async function createSalesOrder({
  customer = 'CUST-001',
  items = [['ITEM-002', 1]],
  date,
}: OrderOptions = {}): Promise<Doc> {
  const warehouse = await seededWarehouse()
  const taxes = await salesTaxTemplate()
  const so = await insertDoc({
    doctype: 'Sales Order',
    company: COMPANY,
    customer,
    order_type: 'Sales',
    currency: CURRENCY,
    selling_price_list: PRICE_LIST,
    ...(date ? { transaction_date: date } : {}),
    delivery_date: date ?? new Date().toISOString().slice(0, 10),
    ...(taxes ? { taxes_and_charges: taxes } : {}),
    items: items.map(([item_code, qty, rate]) => ({
      item_code,
      qty,
      warehouse,
      ...(rate === undefined ? {} : { rate }),
    })),
  })
  if (!(Number(so.grand_total) > 0))
    throw new Error(`${so.name} has no total: item prices missing?`)
  return so
}

/** Submit through the approval workflow, as a user does in Desk. */
export async function submitSalesOrder(so: Doc): Promise<Doc> {
  return call<Doc>('frappe.model.workflow.apply_workflow', {
    doc: JSON.stringify(so),
    action: ACTION_CONFIRM,
  })
}

export async function deliver(soName: string, date?: string): Promise<Doc> {
  const draft = await call<Doc>(
    'erpnext.selling.doctype.sales_order.sales_order.make_delivery_note',
    { source_name: soName },
  )
  return submitDoc(await insertDoc({ ...draft, ...posting(date) }))
}

export async function invoice(dnName: string, date?: string): Promise<Doc> {
  const draft = await call<Doc>(
    'erpnext.stock.doctype.delivery_note.delivery_note.make_sales_invoice',
    { source_name: dnName },
  )
  const due = date ? { due_date: date, payment_schedule: [] } : {}
  return submitDoc(await insertDoc({ ...draft, ...posting(date), ...due }))
}

export async function pay(siName: string, date?: string): Promise<Doc> {
  const draft = await call<Doc>(
    'erpnext.accounts.doctype.payment_entry.payment_entry.get_payment_entry',
    { dt: 'Sales Invoice', dn: siName },
  )
  const day = date ?? new Date().toISOString().slice(0, 10)
  return submitDoc(
    await insertDoc({
      ...draft,
      mode_of_payment: CASH,
      reference_no: siName,
      reference_date: day,
      ...(date ? { posting_date: date } : {}),
    }),
  )
}

/** SO → DN → SI → PE, each submitted; returns the four documents. */
export async function salesFlow(opts: OrderOptions = {}) {
  const so = await submitSalesOrder(await createSalesOrder(opts))
  const dn = await deliver(so.name, opts.date)
  const si = await invoice(dn.name, opts.date)
  const pe = await pay(si.name, opts.date)
  return { so, dn, si, pe }
}

// --- test site lifecycle -------------------------------------------------------------------------------

const SITE_CONFIG = `sites/${TEST_SITE}/site_config.json`
// reads db name/user/password of test.localhost from its site config (no credentials on a command line)
const DB_ENV = `eval "$(env/bin/python -c 'import json,shlex;c=json.load(open("${SITE_CONFIG}"));print("DB="+shlex.quote(c["db_name"]),"DBU="+shlex.quote(c["db_user"]),"export MYSQL_PWD="+shlex.quote(c["db_password"]),sep=";")')"`
const DB_HOST = `"$(env/bin/python -c 'import json;print(json.load(open("sites/common_site_config.json")).get("db_host","mariadb"))')"`

async function snapshotExists(): Promise<boolean> {
  const { stdout } = await benchShell(`test -f ${SNAPSHOT} && echo yes || echo no`)
  return stdout.trim() === 'yes'
}

async function restoreSnapshot(): Promise<void> {
  await benchShell(
    `${DB_ENV} && mariadb -h ${DB_HOST} -u "$DBU" "$DB" < ${SNAPSHOT} && rm -f ${SNAPSHOT}`,
  )
  await bench('clear-cache')
}

export interface PrepareOptions {
  /** run the erp_town scheduled jobs (publish sweeper) during the suite */
  scheduler?: boolean
}

/**
 * Snapshot test.localhost's database (restoring a snapshot an aborted run left behind first), seed it,
 * issue Administrator API keys into `TEST_API_KEY`/`TEST_API_SECRET` (process env only), point Town
 * Settings at `town:test:events` (emptied) and optionally enable the erp_town scheduled jobs.
 */
export async function prepareTestSite({ scheduler = false }: PrepareOptions = {}): Promise<void> {
  if (await snapshotExists()) {
    console.warn('[test-site] restoring the snapshot an aborted run left behind')
    await restoreSnapshot()
  }
  await benchShell(
    `mkdir -p "$(dirname ${SNAPSHOT})" && ${DB_ENV} && mariadb-dump -h ${DB_HOST} -u "$DBU" --single-transaction --quick --skip-lock-tables "$DB" > ${SNAPSHOT}.part && mv ${SNAPSHOT}.part ${SNAPSHOT}`,
  )
  try {
    await bench('execute', 'erp_town.setup.seed.run') // output holds test-site agent keys: not printed
    const keys = await benchExecute<string>('erp_town.setup.testing.issue_test_api_keys')
    const [key, secret] = (keys ?? '').split(':')
    if (!key || !secret) throw new Error('issue_test_api_keys returned no key pair')
    process.env.TEST_API_KEY = key
    process.env.TEST_API_SECRET = secret
    await updateDoc('Town Settings', 'Town Settings', { stream_name: TEST_STREAM })
    await townRedis().del(TEST_STREAM)
    if (scheduler) await benchExecute('erp_town.setup.testing.enable_town_scheduler')
  } catch (e) {
    await restoreTestSite().catch(() => {})
    throw e
  }
}

/** Disable the scheduler and put test.localhost's database back to the snapshot. */
export async function restoreTestSite(): Promise<void> {
  delete process.env.TEST_API_KEY
  delete process.env.TEST_API_SECRET
  try {
    await closeTownRedis()
    await bench('disable-scheduler')
  } finally {
    // always: a seeded test.localhost would break `pnpm erp:test` (the snapshot also resets the scheduler)
    if (await snapshotExists()) await restoreSnapshot()
  }
}
