import { describe, expect, it } from 'vitest'
import { CLIENT_IP_HEADER, clientIpAwareProxy, setClientIpHeader } from './proxyClientIp'

function proxyRequest() {
  const headers = new Map<string, string>([[CLIENT_IP_HEADER.toLowerCase(), '6.6.6.6']])
  return {
    headers,
    setHeader: (name: string, value: string) => { headers.set(name.toLowerCase(), value) },
    removeHeader: (name: string) => { headers.delete(name.toLowerCase()) },
  }
}

describe('setClientIpHeader', () => {
  it('overwrites a caller-supplied value with the connection address', () => {
    const req = proxyRequest()
    setClientIpHeader(req, { socket: { remoteAddress: '192.168.1.10' } })
    expect(req.headers.get(CLIENT_IP_HEADER.toLowerCase())).toBe('192.168.1.10')
  })

  it('removes the header when the connection address is unknown', () => {
    const req = proxyRequest()
    setClientIpHeader(req, { socket: {} })
    expect(req.headers.has(CLIENT_IP_HEADER.toLowerCase())).toBe(false)
  })
})

describe('clientIpAwareProxy', () => {
  it('installs the caller-IP overwrite hook on OAuth proxy routes', () => {
    const route = clientIpAwareProxy('http://backend:8080')
    let listener: typeof setClientIpHeader | undefined
    const fakeProxy = { on: (_event: string, handler: typeof setClientIpHeader) => { listener = handler } }
    route.configure?.(fakeProxy as never, {} as never)

    expect(route.target).toBe('http://backend:8080')
    expect(listener).toBeDefined()
    const request = proxyRequest()
    listener?.(request, { socket: { remoteAddress: '192.168.1.10' } })
    expect(request.headers.get(CLIENT_IP_HEADER.toLowerCase())).toBe('192.168.1.10')
  })

  it('uses the real client IP only when the connection comes from a configured trusted proxy', () => {
    const route = clientIpAwareProxy('http://backend:8080', ['10.0.0.0/8'])
    let listener: typeof setClientIpHeader | undefined
    const fakeProxy = { on: (_event: string, handler: typeof setClientIpHeader) => { listener = handler } }
    route.configure?.(fakeProxy as never, {} as never)
    const request = proxyRequest()

    listener?.(request, { socket: { remoteAddress: '10.1.2.3' }, headers: { 'x-real-ip': '203.0.113.9' } })

    expect(request.headers.get(CLIENT_IP_HEADER.toLowerCase())).toBe('203.0.113.9')
  })

  it('ignores a caller-supplied real IP when the connection is not a trusted proxy', () => {
    const route = clientIpAwareProxy('http://backend:8080', ['10.0.0.0/8'])
    let listener: typeof setClientIpHeader | undefined
    const fakeProxy = { on: (_event: string, handler: typeof setClientIpHeader) => { listener = handler } }
    route.configure?.(fakeProxy as never, {} as never)
    const request = proxyRequest()

    listener?.(request, { socket: { remoteAddress: '192.168.1.10' }, headers: { 'x-real-ip': '203.0.113.9' } })

    expect(request.headers.get(CLIENT_IP_HEADER.toLowerCase())).toBe('192.168.1.10')
  })

  it('rejects invalid trusted proxy ranges at startup', () => {
    expect(() => clientIpAwareProxy('http://backend:8080', ['not-a-cidr'])).toThrow('Invalid trusted proxy CIDR')
  })
})
