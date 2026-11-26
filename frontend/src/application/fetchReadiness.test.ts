import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchReadiness } from './fetchReadiness'

const sha = 'c'.repeat(40)
const payload = {
  repository: 'acme/app', number: 7, headSha: sha, headShortSha: sha.slice(0, 7), headUrl: `https://github.com/acme/app/commit/${sha}`,
  verdict: 'pass', nextAction: 'ready: the engineer decides whether to merge',
  reasons: [{ subject: 'qodo', outcome: 'pass', message: "Qodo's review covers the head with no open findings.", sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-1' }],
  findings: { high: 0, medium: 0, low: 0 },
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchReadiness', () => {
  it('requests and validates the verdict for the imported pull request', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    const readiness = await fetchReadiness('https://github.com/acme/app/pull/7', 'acme/app', 7)
    expect(fetch).toHaveBeenCalledWith('/api/readiness', expect.objectContaining({ method: 'POST' }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ url: 'https://github.com/acme/app/pull/7' })
    expect(readiness.verdict).toBe('pass')
    expect(readiness.reasons[0]?.sourceUrl).toContain('issuecomment-1')
  })

  it('rejects a verdict for a different pull request', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...payload, number: 8 }), { status: 200 })))
    await expect(fetchReadiness('https://github.com/acme/app/pull/7', 'acme/app', 7)).rejects.toThrow('different pull request')
  })

  it('rejects a verdict that names no head commit', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...payload, headSha: '', headShortSha: '', headUrl: '' }), { status: 200 })))
    await expect(fetchReadiness('https://github.com/acme/app/pull/7', 'acme/app', 7)).rejects.toThrow()
  })

  it('reports the server error when the head moved', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'This pull request changed while its readiness was being checked. Check it again.' }), { status: 409 })))
    await expect(fetchReadiness('https://github.com/acme/app/pull/7', 'acme/app', 7)).rejects.toThrow('changed while')
  })
})

describe('when checking several pull requests stops', () => {
  it('stops at a request limit, a lost sign-in or an unusable server token, and goes on after any other failure', async () => {
    const { endsReadinessSweep } = await import('./fetchReadiness')
    const { GhostlineApiError } = await import('./apiError')
    expect(endsReadinessSweep(new GhostlineApiError(429, 'Too many requests.'))).toBe(true)
    expect(endsReadinessSweep(new GhostlineApiError(401, 'Sign in again.'))).toBe(true)
    // The server's GitHub token is rejected or rate-limited for every row alike.
    expect(endsReadinessSweep(new GhostlineApiError(503, 'The server’s GitHub token has reached its rate limit.'))).toBe(true)
    expect(endsReadinessSweep(new GhostlineApiError(409, 'This pull request changed.'))).toBe(false)
    expect(endsReadinessSweep(new Error('network'))).toBe(false)
  })
})
