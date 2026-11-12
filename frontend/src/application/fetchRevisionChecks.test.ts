import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchRevisionChecks } from './fetchRevisionChecks'

const sha = 'b'.repeat(40)
const payload = {
  availability: 'available',
  revisionSha: sha,
  revisionShortSha: sha.slice(0, 7),
  revisionUrl: `https://github.com/acme/app/commit/${sha}`,
  items: [{ id: 314, name: 'historical-build', kind: 'check_run', status: 'completed', conclusion: 'success' }],
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchRevisionChecks', () => {
  it('requests and validates GitHub checks for the selected exact SHA', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    vi.stubGlobal('fetch', fetch)

    const checks = await fetchRevisionChecks('https://github.com/acme/app/pull/7', sha)

    expect(fetch).toHaveBeenCalledWith('/api/checks', expect.objectContaining({ method: 'POST' }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ url: 'https://github.com/acme/app/pull/7', sha })
    expect(checks.revisionSha).toBe(sha)
    expect(checks.items[0]?.name).toBe('historical-build')
  })

  it('rejects a response tied to a different revision', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...payload, revisionSha: 'a'.repeat(40) }), { status: 200 })))
    await expect(fetchRevisionChecks('https://github.com/acme/app/pull/7', sha)).rejects.toThrow('different revision')
  })

  it('surfaces the API error when checks cannot be loaded', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'invalid SHA' }), { status: 400 })))
    await expect(fetchRevisionChecks('https://github.com/acme/app/pull/7', sha)).rejects.toThrow('invalid SHA')
  })
})
