import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchAuthStatus } from './fetchAuthStatus'

afterEach(() => vi.unstubAllGlobals())

describe('fetchAuthStatus', () => {
  it('returns validated hosted sign-in status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ enabled: true, login: null }), { status: 200 })))
    await expect(fetchAuthStatus()).resolves.toEqual({ enabled: true, login: null })
    expect(fetch).toHaveBeenCalledWith('/api/auth/status', { headers: { Accept: 'application/json' } })
  })

  it('rejects a failed status request', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 503 })))
    await expect(fetchAuthStatus()).rejects.toThrow('Could not check Ghostline sign-in status')
  })

  it('rejects an invalid status payload', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ enabled: 'yes', login: null }), { status: 200 })))
    await expect(fetchAuthStatus()).rejects.toThrow('invalid sign-in status')
  })
})
