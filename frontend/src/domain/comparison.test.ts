import { describe, expect, it } from 'vitest'
import { RevisionComparisonSchema } from './comparison'

const baseSha = 'a'.repeat(40)
const headSha = 'b'.repeat(40)
const payload = {
  baseSha, headSha, compareUrl: `https://github.com/acme/app/compare/${baseSha}...${headSha}`,
  status: 'ahead', aheadBy: 2, behindBy: 0, totalCommits: 2,
  files: [{ path: 'backend/retry.go', status: 'modified', additions: 4, deletions: 1, changes: 5, url: `https://github.com/acme/app/commit/${headSha}` }],
}

describe('revision comparison contract', () => {
  it('accepts a GitHub comparison tied to both requested revisions', () => {
    expect(RevisionComparisonSchema.parse(payload)).toMatchObject({ baseSha, headSha, files: [{ path: 'backend/retry.go' }] })
  })

  it('rejects a response for a different requested revision', () => {
    expect(RevisionComparisonSchema.safeParse({ ...payload, headSha: 'c'.repeat(40) }).success).toBe(false)
  })

  it('rejects external comparison and file links', () => {
    expect(RevisionComparisonSchema.safeParse({ ...payload, compareUrl: 'https://example.com/compare' }).success).toBe(false)
    expect(RevisionComparisonSchema.safeParse({ ...payload, files: [{ ...payload.files[0], url: 'https://example.com/file' }] }).success).toBe(false)
  })
})
