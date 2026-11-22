import type { ProxyOptions } from 'vite'
import { BlockList, isIP } from 'node:net'

// The dev/Compose proxy tells the backend which browser sent each request, so
// the backend's per-client rate limit is not shared by everyone behind it.
export const CLIENT_IP_HEADER = 'X-Ghostline-Client-IP'

interface OutgoingProxyRequest {
  setHeader(name: string, value: string): unknown
  removeHeader(name: string): unknown
}
interface IncomingRequest {
  socket: { remoteAddress?: string }
  headers?: Record<string, string | string[] | undefined>
}

function normalizedIP(address: string): string {
  return address.toLowerCase().startsWith('::ffff:') && isIP(address.slice(7)) === 4
    ? address.slice(7)
    : address
}

function trustedProxyList(cidrs: readonly string[]): BlockList | undefined {
  if (cidrs.length === 0) return undefined
  const list = new BlockList()
  for (const cidr of cidrs) {
    const [address, prefix, extra] = cidr.trim().split('/')
    const normalized = normalizedIP(address ?? '')
    const family = isIP(normalized)
    const prefixLength = Number(prefix)
    const maxPrefix = family === 4 ? 32 : 128
    if (!family || extra !== undefined || !/^\d+$/.test(prefix ?? '') || prefixLength > maxPrefix) {
      throw new Error(`Invalid trusted proxy CIDR: ${cidr}`)
    }
    list.addSubnet(normalized, prefixLength, family === 4 ? 'ipv4' : 'ipv6')
  }
  return list
}

function clientAddress(req: IncomingRequest, trustedProxies?: BlockList): string | undefined {
  const remoteAddress = req.socket.remoteAddress
  if (!remoteAddress) return undefined
  const remote = normalizedIP(remoteAddress)
  const family = isIP(remote)
  if (family && trustedProxies?.check(remote, family === 4 ? 'ipv4' : 'ipv6')) {
    const realAddress = req.headers?.['x-real-ip']
    if (typeof realAddress === 'string') {
      const candidate = normalizedIP(realAddress.trim())
      if (isIP(candidate)) return candidate
    }
  }
  return remote
}

// The backend trusts this proxy's X-Request-ID, so a caller's own value is dropped and
// the backend generates the ID.
export const REQUEST_ID_HEADER = 'X-Request-ID'

// Always overwrite, never forward: a caller must not be able to choose its own identity
// or the request ID its requests are logged under.
export function setClientIpHeader(proxyReq: OutgoingProxyRequest, req: IncomingRequest, trustedProxies?: BlockList): void {
  const address = clientAddress(req, trustedProxies)
  if (address) proxyReq.setHeader(CLIENT_IP_HEADER, address)
  else proxyReq.removeHeader(CLIENT_IP_HEADER)
  proxyReq.removeHeader(REQUEST_ID_HEADER)
}

export function clientIpAwareProxy(target: string, trustedProxyCIDRs: readonly string[] = []): ProxyOptions {
  const trustedProxies = trustedProxyList(trustedProxyCIDRs.filter(Boolean))
  return {
    target,
    configure: (proxy) => { proxy.on('proxyReq', (proxyReq, req) => setClientIpHeader(proxyReq, req, trustedProxies)) },
  }
}
