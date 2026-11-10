import { describe, expect, it } from 'vitest'
import { CLIENT_IP_HEADER, setClientIpHeader } from './proxyClientIp'

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
