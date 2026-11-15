import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchNoxReport } from './fetchNoxReport'

const sha = 'd'.repeat(40)
const payload = {
  availability: 'available', revisionSha: sha, runId: 12, runName: 'Nox', runConclusion: 'success',
  runUrl: 'https://github.com/acme/app/actions/runs/12', artifactId: 4,
  artifactUrl: 'https://github.com/acme/app/actions/runs/12', artifactDigest: 'sha256:abc',
  createdAt: '2026-11-15T13:58:17Z', expiresAt: '2026-11-22T13:58:16Z', totalCount: 0,
  truncated: false, findings: [],
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchNoxReport', () => {
  it('requests a report for the supplied PR and revision and validates it', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    const result = await fetchNoxReport('https://github.com/acme/app/pull/7', sha)
    expect(fetch).toHaveBeenCalledWith('/api/nox-report', expect.objectContaining({ method: 'POST' }))
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({ url: 'https://github.com/acme/app/pull/7', sha })
    expect(result.revisionSha).toBe(sha)
  })

  it('rejects reports associated with another revision', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ ...payload, revisionSha: 'e'.repeat(40) }), { status: 200 })))
    await expect(fetchNoxReport('https://github.com/acme/app/pull/7', sha)).rejects.toThrow('different revision')
  })

  it('shows API errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'bad revision' }), { status: 400 })))
    await expect(fetchNoxReport('https://github.com/acme/app/pull/7', sha)).rejects.toThrow('bad revision')
  })
})
