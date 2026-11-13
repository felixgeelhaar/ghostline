import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from './importPullRequest'

afterEach(() => vi.unstubAllGlobals())

describe('importPullRequest', () => {
  it('returns a concise error when GitHub source data fails validation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      repository: 'acme/ghostline', owner: 'acme', repo: 'ghostline', number: 12,
      title: 'Trust source links', state: 'open', merged: false, url: 'https://github.com/acme/other/pull/12',
      createdAt: '2026-11-06T08:00:00Z', updatedAt: '2026-11-06T08:00:00Z', commits: [],
      checks: { availability: 'unavailable', reason: 'Not configured', revisionSha: '', revisionShortSha: '', revisionUrl: '', items: [] },
      qodoReviews: { availability: 'unavailable', reason: 'Not configured', unrecognizedCommentCount: 0, items: [] },
      referencedIssues: { availability: 'available', items: [] },
    }), { status: 200 })))

    await expect(importPullRequest('https://github.com/acme/ghostline/pull/12'))
      .rejects.toThrow('GitHub returned pull request data that did not pass validation. Please retry the import.')
  })

  it('does not blame repository links when another field fails validation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      repository: 'acme/ghostline', owner: 'acme', repo: 'ghostline', number: 12,
      title: 'Trust source links', state: 'open', merged: false, url: 'https://github.com/acme/ghostline/pull/12',
      createdAt: '2026-11-06T08:00:00Z', updatedAt: '2026-11-06T08:00:00Z', commits: [],
      checks: { availability: 'unavailable', reason: 'Not configured', revisionSha: '', revisionShortSha: '', revisionUrl: '', items: [] },
      qodoReviews: null,
      referencedIssues: { availability: 'available', items: [] },
    }), { status: 200 })))

    await expect(importPullRequest('https://github.com/acme/ghostline/pull/12'))
      .rejects.toThrow('GitHub returned pull request data that did not pass validation. Please retry the import.')
  })
})
