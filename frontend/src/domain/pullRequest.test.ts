import { describe, expect, it } from 'vitest'
import { PullRequestSchema, validatePullRequestURL } from './pullRequest'

const validPullRequest = {
  repository: 'acme/ghostline',
  owner: 'acme',
  repo: 'ghostline',
  number: 12,
  title: 'Trust source links',
  state: 'open',
  merged: false,
  url: 'https://github.com/acme/ghostline/pull/12',
  createdAt: '2026-11-06T08:00:00Z',
  updatedAt: '2026-11-06T08:00:00Z',
  commits: [{
    sha: 'a'.repeat(40),
    shortSha: 'aaaaaaaa',
    message: 'Validate evidence links',
    author: 'felixgeelhaar',
    committedAt: '2026-11-06T08:00:00Z',
    url: `https://github.com/acme/ghostline/commit/${'a'.repeat(40)}`,
  }],
  checks: { availability: 'unavailable', reason: 'Not configured', revisionSha: '', revisionShortSha: '', revisionUrl: '', items: [] },
  qodoReviews: { availability: 'unavailable', reason: 'Not configured', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: [],
}

describe('validatePullRequestURL', () => {
  it('accepts a GitHub PR URL', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/pull/12')).toBeUndefined())
  it('rejects other hosts', () => expect(validatePullRequestURL('https://evil.example/acme/ghostline/pull/12')).toBeTruthy())
  it('rejects a non-pull URL', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/issues/12')).toBeTruthy())
  it('rejects a non-default port', () => expect(validatePullRequestURL('https://github.com:8443/acme/ghostline/pull/12')).toBeTruthy())
  it('rejects query strings and fragments', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/pull/12?x=1')).toBeTruthy())
})

describe('PullRequestSchema source links', () => {
  it('accepts PR and commit URLs that match their GitHub identity', () => {
    expect(PullRequestSchema.safeParse(validPullRequest).success).toBe(true)
  })

  it.each([
    ['a non-GitHub host', { url: 'https://evil.example/acme/ghostline/pull/12' }],
    ['a different repository', { url: 'https://github.com/acme/other/pull/12' }],
    ['a different PR number', { url: 'https://github.com/acme/ghostline/pull/13' }],
    ['a PR URL with a query string', { url: 'https://github.com/acme/ghostline/pull/12?redirect=1' }],
    ['an encoded slash in the owner', { url: 'https://github.com/acme%2Fghostline/pull/12' }],
    ['an encoded slash in the repository', { url: 'https://github.com/acme/ghostline%2Fother/pull/12' }],
    ['a non-default port', { url: 'https://github.com:8443/acme/ghostline/pull/12' }],
  ])('rejects a PR URL with %s', (_reason, override) => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, ...override }).success).toBe(false)
  })

  it.each([
    ['a non-GitHub host', 'https://evil.example/acme/ghostline/commit/' + 'a'.repeat(40)],
    ['a different repository', 'https://github.com/acme/other/commit/' + 'a'.repeat(40)],
    ['a different commit SHA', 'https://github.com/acme/ghostline/commit/' + 'b'.repeat(40)],
    ['a non-default port', 'https://github.com:8443/acme/ghostline/commit/' + 'a'.repeat(40)],
  ])('rejects a commit URL with %s', (_reason, url) => {
    const candidate = { ...validPullRequest, commits: [{ ...validPullRequest.commits[0], url }] }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })

  it('rejects an encoded slash inside a commit path segment', () => {
    const candidate = { ...validPullRequest, commits: [{ ...validPullRequest.commits[0], url: `https://github.com/acme%2Fghostline/commit/${'a'.repeat(40)}` }] }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })
})

describe('PullRequestSchema referenced issues', () => {
  const issue = { number: 27, title: 'Idempotency tracking', availability: 'available', state: 'open', url: 'https://github.com/acme/ghostline/issues/27' }

  it('accepts same-repository issue state and source URL', () => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, referencedIssues: [issue] }).success).toBe(true)
  })

  it.each([
    ['another repository', 'https://github.com/acme/other/issues/27'],
    ['another issue number', 'https://github.com/acme/ghostline/issues/28'],
    ['another host', 'https://evil.example/acme/ghostline/issues/27'],
  ])('rejects an issue URL for %s', (_reason, url) => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, referencedIssues: [{ ...issue, url }] }).success).toBe(false)
  })

  it('requires a state when issue details are available', () => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, referencedIssues: [{ ...issue, state: undefined }] }).success).toBe(false)
  })

  it('allows explicit unavailable issue data without a fabricated state', () => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, referencedIssues: [{ number: 27, title: 'Issue #27', availability: 'unavailable', reason: 'GitHub unavailable', url: issue.url }] }).success).toBe(true)
  })
})

describe('PullRequestSchema Qodo review commit links', () => {
  const reviewedSha = 'a'.repeat(40)
  const withReview = (review: Record<string, unknown>) => ({
    ...validPullRequest,
    qodoReviews: {
      availability: 'available',
      unrecognizedCommentCount: 0,
      items: [{
        id: 30,
        provider: 'Qodo',
        sourceType: 'github_issue_comment',
        sourceAuthor: 'qodo-code-review[bot]',
        kind: 'no_open_findings',
        findings: [],
        createdAt: '2026-11-06T08:00:00Z',
        url: 'https://github.com/acme/ghostline/pull/12#issuecomment-30',
        ...review,
      }],
    },
  })

  it('accepts a review without a commit marker', () => {
    expect(PullRequestSchema.safeParse(withReview({})).success).toBe(true)
  })

  it('accepts a commit link in the imported repository, in any letter case', () => {
    expect(PullRequestSchema.safeParse(withReview({ commitSha: reviewedSha, commitUrl: `https://github.com/acme/ghostline/commit/${reviewedSha}` })).success).toBe(true)
    expect(PullRequestSchema.safeParse(withReview({ commitSha: reviewedSha, commitUrl: `https://github.com/ACME/Ghostline/commit/${reviewedSha}` })).success).toBe(true)
  })

  it.each([
    ['another repository', `https://github.com/acme/other/commit/${reviewedSha}`],
    ['another host', `https://evil.example/acme/ghostline/commit/${reviewedSha}`],
    ['a different SHA than commitSha', `https://github.com/acme/ghostline/commit/${'b'.repeat(40)}`],
  ])('rejects a commit link to %s', (_reason, commitUrl) => {
    expect(PullRequestSchema.safeParse(withReview({ commitSha: reviewedSha, commitUrl })).success).toBe(false)
  })

  it('rejects a commit link without the SHA it claims to name', () => {
    expect(PullRequestSchema.safeParse(withReview({ commitUrl: `https://github.com/acme/ghostline/commit/${reviewedSha}` })).success).toBe(false)
  })
})
