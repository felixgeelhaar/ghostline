import { afterEach, describe, expect, it, vi } from 'vitest'
import { compareRevisions } from './compareRevisions'

const baseSha = 'a'.repeat(40)
const headSha = 'b'.repeat(40)
const payload = {
  baseSha, headSha, compareUrl: `https://github.com/acme/app/compare/${baseSha}...${headSha}`,
  status: 'ahead', aheadBy: 1, behindBy: 0, totalCommits: 1, files: [],
}

afterEach(() => vi.unstubAllGlobals())

describe('compareRevisions', () => {
  it('requests a comparison using the imported PR and selected revision pair', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    await compareRevisions('https://github.com/acme/app/pull/7', baseSha, headSha)
    expect(fetch).toHaveBeenCalledWith('/api/compare', expect.objectContaining({ method: 'POST' }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ url: 'https://github.com/acme/app/pull/7', baseSha, headSha })
  })

  it('rejects a response tied to another candidate revision', async () => {
    const otherHead = 'c'.repeat(40)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      ...payload, headSha: otherHead, compareUrl: `https://github.com/acme/app/compare/${baseSha}...${otherHead}`,
    }), { status: 200 })))
    await expect(compareRevisions('https://github.com/acme/app/pull/7', baseSha, headSha)).rejects.toThrow('different revisions')
  })

  it('rejects source links from a different repository', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      ...payload,
      compareUrl: `https://github.com/other/repo/compare/${baseSha}...${headSha}`,
    }), { status: 200 })))
    await expect(compareRevisions('https://github.com/acme/app/pull/7', baseSha, headSha)).rejects.toThrow('different repository')
  })

  it('surfaces an API comparison error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'compare unavailable' }), { status: 502 })))
    await expect(compareRevisions('https://github.com/acme/app/pull/7', baseSha, headSha)).rejects.toThrow('compare unavailable')
  })
})
