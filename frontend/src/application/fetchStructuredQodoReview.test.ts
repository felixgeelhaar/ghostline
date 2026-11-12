import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchStructuredQodoReview } from './fetchStructuredQodoReview'

const sha = 'a'.repeat(40)
const payload = {
  provider: 'Qodo', sourceType: 'qodo_agentic_toolbox', currentRevisionSha: sha,
  reviewSession: { id: 'run-1', status: 'completed', commitSha: sha, revisionStatus: 'current', reviewComplete: true },
  findings: [{ id: 'finding-1', title: 'Check stale state', gitSha: sha, revisionStatus: 'current', reviewRunIds: ['run-1'], locations: [], citations: [] }],
  reviewRuns: [{ id: 'run-1', status: 'completed', commitSha: sha, revisionStatus: 'current', reviewComplete: true }],
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchStructuredQodoReview', () => {
  it('sends the request-scoped key only in a header and validates the structured response', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    vi.stubGlobal('fetch', fetch)

    const result = await fetchStructuredQodoReview('https://github.com/acme/app/pull/7', 'personal-key')

    expect(fetch).toHaveBeenCalledWith('/api/qodo/reviews', expect.objectContaining({
      method: 'POST', cache: 'no-store', credentials: 'same-origin',
      headers: expect.objectContaining({ 'X-Qodo-API-Key': 'personal-key' }),
    }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ url: 'https://github.com/acme/app/pull/7' })
    expect(result.findings[0]?.id).toBe('finding-1')
  })

  it('rejects malformed data and surfaces upstream errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...payload, currentRevisionSha: 'short' }), { status: 200 })))
    await expect(fetchStructuredQodoReview('https://github.com/acme/app/pull/7', 'personal-key')).rejects.toThrow('did not pass validation')

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'Qodo lookup is disabled.' }), { status: 404 })))
    await expect(fetchStructuredQodoReview('https://github.com/acme/app/pull/7', 'personal-key')).rejects.toThrow('disabled')
  })
})
