import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchCheckAnnotations } from './fetchCheckAnnotations'

const sha = 'a'.repeat(40)
const payload = {
  revisionSha: sha, checkRunId: 314, checkRunUrl: 'https://github.com/acme/app/actions/runs/314',
  page: 1, pageSize: 100, hasMore: false, items: [],
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchCheckAnnotations', () => {
  it('requests annotations using the imported PR, exact revision and check run', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    vi.stubGlobal('fetch', fetch)

    const result = await fetchCheckAnnotations('https://github.com/acme/app/pull/7', sha, 314, 1)

    expect(fetch).toHaveBeenCalledWith('/api/check-annotations', expect.objectContaining({ method: 'POST' }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ url: 'https://github.com/acme/app/pull/7', sha, checkRunId: 314, page: 1 })
    expect(result.revisionSha).toBe(sha)
    expect(result.checkRunId).toBe(314)
  })

  it('rejects annotations associated with a different run or revision', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...payload, revisionSha: 'b'.repeat(40) }), { status: 200 })))
    await expect(fetchCheckAnnotations('https://github.com/acme/app/pull/7', sha, 314, 1)).rejects.toThrow('different check run or revision')
  })

  it('surfaces API errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'revision mismatch' }), { status: 409 })))
    await expect(fetchCheckAnnotations('https://github.com/acme/app/pull/7', sha, 314, 1)).rejects.toThrow('revision mismatch')
  })
})
