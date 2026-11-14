import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGhostlineSession } from './fetchGhostlineSession'

afterEach(() => vi.unstubAllGlobals())

describe('fetchGhostlineSession', () => {
  it('validates the session status at the HTTP boundary', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ authRequired: true, authenticated: true, login: 'felix' }), { status: 200 })))
    await expect(fetchGhostlineSession()).resolves.toEqual({ authRequired: true, authenticated: true, login: 'felix' })
  })

  it('rejects malformed session data and unavailable API responses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response('{"authRequired":true,"authenticated":true}', { status: 200 })).mockResolvedValueOnce(new Response('', { status: 503 })))
    await expect(fetchGhostlineSession()).rejects.toThrow('invalid sign-in status')
    await expect(fetchGhostlineSession()).rejects.toThrow('could not verify sign-in status')
  })
})
