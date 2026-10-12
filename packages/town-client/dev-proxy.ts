import type { ProxyOptions } from 'vite'

type DevEnvironment = Record<string, string | undefined>

function loopbackTarget(value: string): string {
  const url = new URL(value)
  if (
    url.protocol !== 'http:' ||
    !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.pathname !== '/' ||
    url.search ||
    url.hash
  ) {
    throw new Error('Development proxies require an HTTP loopback origin without credentials')
  }
  return url.origin
}

/** Server-only configuration. No service credentials are exposed to the browser. */
export function createDevProxy(env: DevEnvironment): Record<string, ProxyOptions> {
  const gateway = loopbackTarget(env.TOWN_DEV_GATEWAY_URL ?? 'http://127.0.0.1:8090')
  const erp = loopbackTarget(env.TOWN_DEV_ERP_URL ?? 'http://127.0.0.1:8080')
  const site = env.TOWN_DEV_ERP_SITE ?? 'town.localhost'
  if (!/^[a-z0-9][a-z0-9.-]{0,139}$/.test(site)) throw new Error('Invalid development ERP site')
  const town: ProxyOptions = {
    target: gateway,
    ws: true,
    // Preserve Host and Origin together so the world socket's same-origin check
    // still applies. Never enable rewriteWsOrigin or a permissive CORS policy.
    changeOrigin: false,
    configure(proxy) {
      const stripSite = (request: { removeHeader(name: string): void }) =>
        request.removeHeader('X-Frappe-Site-Name')
      proxy.on('proxyReq', stripSite)
      proxy.on('proxyReqWs', stripSite)
    },
  }
  const frappe: ProxyOptions = {
    target: erp,
    changeOrigin: true,
    headers: { 'X-Frappe-Site-Name': site },
  }
  return {
    '^/town/(?:api(?:/|$)|ws(?:\\?|$))': town,
    '^/(?:api(?:/|$)|desk(?:/|$)|app(?:/|$)|login(?:\\?|$)|logout(?:\\?|$)|files/|private/files/|website_script\\.js(?:\\?|$)|assets/(?:frappe|erpnext|hrms|erp_town)/)':
      frappe,
  }
}
