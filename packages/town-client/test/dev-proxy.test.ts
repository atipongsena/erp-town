import { createHash } from 'node:crypto'
import { createServer as createHTTPServer } from 'node:http'
import type { Socket } from 'node:net'
import { createConnection } from 'node:net'
import path from 'node:path'
import { createServer as createViteServer } from 'vite'
import { describe, expect, it } from 'vitest'
import { createDevProxy } from '../dev-proxy.ts'

describe('local ERP routing', () => {
  it('forwards sessions, site selection and binary upgrades instead of returning the SPA', async () => {
    const seen: { path: string; cookie?: string; site?: string; origin?: string; host?: string }[] =
      []
    const sockets = new Set<Socket>()
    const upstream = createHTTPServer((request, response) => {
      seen.push({
        path: request.url ?? '',
        cookie: request.headers.cookie,
        site: request.headers['x-frappe-site-name'] as string,
      })
      response.setHeader('Content-Type', 'application/json')
      if (request.url === '/api/method/login')
        response.setHeader('Set-Cookie', 'sid=fixture; Path=/; HttpOnly; SameSite=Lax')
      response.statusCode = request.url === '/town/api/me' && !request.headers.cookie ? 401 : 200
      response.end(JSON.stringify({ route: request.url }))
    })
    upstream.on('upgrade', (request, socket, head) => {
      seen.push({
        path: request.url ?? '',
        origin: request.headers.origin,
        host: request.headers.host,
      })
      const accept = createHash('sha1')
        .update(`${request.headers['sec-websocket-key']}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
        .digest('base64')
      socket.write(
        `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`,
      )
      // An unmasked binary frame remains unchanged across the proxy.
      socket.write(Buffer.from([0x82, 3, 0, 1, 255]))
      if (head.length) socket.write(head)
    })
    upstream.on('connection', (socket) => {
      sockets.add(socket)
      socket.on('close', () => sockets.delete(socket))
    })
    await new Promise<void>((resolve) => upstream.listen(0, '127.0.0.1', resolve))
    const address = upstream.address()
    if (!address || typeof address === 'string') throw new Error('Missing upstream port')
    const target = `http://127.0.0.1:${address.port}`
    const vite = await createViteServer({
      configFile: false,
      root: path.join(import.meta.dirname, '../test-results/proxy-host'),
      cacheDir: path.join(import.meta.dirname, '../node_modules/.vite-proxy-test'),
      logLevel: 'silent',
      server: {
        host: '127.0.0.1',
        port: 0,
        proxy: createDevProxy({
          TOWN_DEV_GATEWAY_URL: target,
          TOWN_DEV_ERP_URL: target,
          TOWN_DEV_ERP_SITE: 'test.localhost',
        }),
      },
    })
    try {
      await vite.listen()
      const front = vite.httpServer?.address()
      if (!front || typeof front === 'string') throw new Error('Missing frontend port')
      const origin = `http://127.0.0.1:${front.port}`
      const guest = await fetch(`${origin}/town/api/me`)
      expect(guest.status).toBe(401)
      expect(guest.headers.get('content-type')).toContain('application/json')
      const login = await fetch(`${origin}/api/method/login`, {
        method: 'POST',
        headers: { 'X-Frappe-Site-Name': 'other.localhost' },
      })
      expect(login.headers.get('set-cookie')).toContain('sid=fixture; Path=/; HttpOnly')
      const me = await fetch(`${origin}/town/api/me`, {
        headers: { Cookie: 'sid=fixture', 'X-Frappe-Site-Name': 'other.localhost' },
      })
      expect(await me.json()).toEqual({ route: '/town/api/me' })
      for (const path of ['/desk', '/login', '/assets/frappe/js/desk.bundle.js']) {
        const response = await fetch(`${origin}${path}`)
        expect(await response.json()).toEqual({ route: path })
      }
      expect(seen.find((r) => r.path === '/api/method/login')?.site).toBe('test.localhost')
      expect(seen.find((r) => r.path === '/town/api/me' && r.cookie)?.cookie).toBe('sid=fixture')
      expect(seen.find((r) => r.path === '/town/api/me')?.site).toBeUndefined()
      const socket = createConnection(front.port, '127.0.0.1')
      try {
        const raw = await new Promise<Buffer>((resolve, reject) => {
          const chunks: Buffer[] = []
          socket.setTimeout(5000, () => reject(new Error('Upgrade timed out')))
          socket.on('error', reject)
          socket.on('data', (chunk) => {
            chunks.push(typeof chunk === 'string' ? Buffer.from(chunk) : chunk)
            const received = Buffer.concat(chunks)
            if (received.includes(Buffer.from([0x82, 3, 0, 1, 255]))) resolve(received)
          })
          socket.on('connect', () =>
            socket.write(
              `GET /town/ws HTTP/1.1\r\nHost: 127.0.0.1:${front.port}\r\nOrigin: ${origin}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n`,
            ),
          )
        })
        expect(raw.toString()).toContain('101 Switching Protocols')
        expect(seen.find((r) => r.path === '/town/ws')).toMatchObject({
          origin,
          host: `127.0.0.1:${front.port}`,
        })
      } finally {
        socket.destroy()
      }
    } finally {
      await vite.close()
      for (const socket of sockets) socket.destroy()
      upstream.closeAllConnections()
      await new Promise<void>((resolve) => upstream.close(() => resolve()))
    }
  }, 15000)

  it('refuses remote targets and strips any supplied site header at the gateway', () => {
    expect(() => createDevProxy({ TOWN_DEV_GATEWAY_URL: 'https://example.com' })).toThrow(
      'loopback',
    )
    expect(() => createDevProxy({ TOWN_DEV_ERP_URL: 'http://user:secret@127.0.0.1:8080' })).toThrow(
      'loopback',
    )
  })
})
