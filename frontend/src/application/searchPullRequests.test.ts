import { afterEach, describe, expect, it, vi } from 'vitest'
import { searchPullRequests } from './searchPullRequests'

const item = { repository: 'acme/widgets', owner: 'acme', repo: 'widgets', number: 7, title: 'Add widgets', state: 'open', updatedAt: '2026-11-20T10:00:00Z', url: 'https://github.com/acme/widgets/pull/7' }

afterEach(() => vi.unstubAllGlobals())

describe('searchPullRequests', () => {
  it('posts the query and validates the suggestions', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [item], partial: true }), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    const result = await searchPullRequests('widgets')
    expect(fetch).toHaveBeenCalledWith('/api/pr-search', expect.objectContaining({ method: 'POST' }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ query: 'widgets' })
    expect(result).toEqual({ items: [item], partial: true })
  })

  it('reports a rate limit as an error, not as an empty result', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'Too many searches' }), { status: 429, headers: { 'Retry-After': '12' } })))
    await expect(searchPullRequests('widgets')).rejects.toThrow('Try again in 12 seconds')
  })

  it('rejects suggestions that do not pass validation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [{ ...item, url: 'https://github.com/evil/x/pull/1' }], partial: false }), { status: 200 })))
    await expect(searchPullRequests('widgets')).rejects.toThrow('did not pass validation')
  })
})
