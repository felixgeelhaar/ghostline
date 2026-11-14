import { describe, expect, it } from 'vitest'
import { parseRevisionComparison, RevisionComparisonSchema } from './comparison'

const baseSha = 'a'.repeat(40)
const headSha = 'b'.repeat(40)
const payload = {
  baseSha, headSha, compareUrl: `https://github.com/acme/app/compare/${baseSha}...${headSha}`,
  status: 'ahead', aheadBy: 2, behindBy: 0, totalCommits: 2,
  files: [{ path: 'backend/retry.go', status: 'modified', additions: 4, deletions: 1, changes: 5, url: `https://github.com/acme/app/blob/${headSha}/backend/retry.go` }],
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

  it('rejects a non-ancestor revision pair', () => {
    expect(RevisionComparisonSchema.safeParse({ ...payload, status: 'diverged', behindBy: 1 }).success).toBe(false)
    expect(RevisionComparisonSchema.safeParse({ ...payload, behindBy: 1 }).success).toBe(false)
  })

  it('requires changed-file links to identify the exact revision and file path', () => {
    expect(() => parseRevisionComparison({ ...payload, files: [{ ...payload.files[0], url: `https://github.com/acme/app/blob/${'c'.repeat(40)}/backend/retry.go` }] }, {
      pullRequestUrl: 'https://github.com/acme/app/pull/7', baseSha, headSha,
    })).toThrow('different revision or path')
    expect(() => parseRevisionComparison({ ...payload, files: [{ ...payload.files[0], url: `https://github.com/acme/app/blob/${headSha}/backend/other.go` }] }, {
      pullRequestUrl: 'https://github.com/acme/app/pull/7', baseSha, headSha,
    })).toThrow('different revision or path')
  })

  it('binds source evidence to the requested PR repository and revision pair', () => {
    expect(parseRevisionComparison(payload, {
      pullRequestUrl: 'https://github.com/acme/app/pull/7', baseSha, headSha,
    })).toMatchObject({ baseSha, headSha, filesTruncated: false })
    expect(() => parseRevisionComparison(payload, {
      pullRequestUrl: 'https://github.com/acme/app/pull/7', baseSha, headSha: 'c'.repeat(40),
    })).toThrow('different revisions')
    expect(() => parseRevisionComparison({ ...payload, compareUrl: `https://github.com/other/repo/compare/${baseSha}...${headSha}` }, {
      pullRequestUrl: 'https://github.com/acme/app/pull/7', baseSha, headSha,
    })).toThrow('different repository')
  })

  it('preserves an explicit changed-file truncation signal', () => {
    expect(RevisionComparisonSchema.parse({ ...payload, filesTruncated: true }).filesTruncated).toBe(true)
  })
})
