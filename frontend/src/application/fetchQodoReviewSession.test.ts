import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchQodoReviewSession } from './fetchQodoReviewSession'

const url = 'https://github.com/acme/payments/pull/42'
const key = 'qodo-private-key'
const emptyReview = {
  availability: 'available', source: 'Qodo Agentic Toolbox · get-pr-review-findings',
  reviewSession: { status: 'completed', commitSha: 'a'.repeat(40) }, findings: [], reviewRuns: [],
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchQodoReviewSession', () => {
  it('sends the imported pull request and one-request key, then validates Qodo source data', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(emptyReview), { status: 200 }))
    vi.stubGlobal('fetch', fetch)

    const result = await fetchQodoReviewSession(url, key)

    expect(fetch).toHaveBeenCalledWith('/api/qodo-review', expect.objectContaining({
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Qodo-API-Key': key },
      body: JSON.stringify({ url }),
    }))
    expect(result.reviewSession?.commitSha).toBe('a'.repeat(40))
  })

  it('rejects malformed Qodo responses and reports service errors without leaking the key', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'Qodo workspace access is unavailable.' }), { status: 502 }))
    vi.stubGlobal('fetch', fetch)

    const error = await fetchQodoReviewSession(url, key).catch((reason: unknown) => reason)
    expect(error).toBeInstanceOf(Error)
    expect((error as Error).message).toBe('Qodo workspace access is unavailable.')
    expect((error as Error).message).not.toContain(key)
  })
})
