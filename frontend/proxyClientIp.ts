// The dev/Compose proxy tells the backend which browser sent each request, so
// the backend's per-client rate limit is not shared by everyone behind it.
export const CLIENT_IP_HEADER = 'X-Ghostline-Client-IP'

interface OutgoingProxyRequest {
  setHeader(name: string, value: string): unknown
  removeHeader(name: string): unknown
}
interface IncomingRequest { socket: { remoteAddress?: string } }

// Always overwrite, never forward: a caller must not be able to choose its own identity.
export function setClientIpHeader(proxyReq: OutgoingProxyRequest, req: IncomingRequest): void {
  const address = req.socket.remoteAddress
  if (address) proxyReq.setHeader(CLIENT_IP_HEADER, address)
  else proxyReq.removeHeader(CLIENT_IP_HEADER)
}
