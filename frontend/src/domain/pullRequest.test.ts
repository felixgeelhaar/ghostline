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
  referencedIssues: { availability: 'available', items: [] },
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
  const referencedIssues = {
    availability: 'available',
    items: [{ number: 31, url: 'https://github.com/acme/ghostline/issues/31', state: 'open' }],
  }

  it('accepts issue evidence linked to the imported repository and number', () => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, merged: true, referencedIssues }).success).toBe(true)
  })

  it('rejects an issue response exceeding the backend lookup limit', () => {
    const items = Array.from({ length: 21 }, (_, index) => ({
      number: index + 1,
      url: `https://github.com/acme/ghostline/issues/${index + 1}`,
      state: 'closed',
    }))
    const candidate = { ...validPullRequest, referencedIssues: { availability: 'unavailable', reason: 'Only the first 20 issue references are checked.', items } }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })

  it.each([
    ['another repository', 'https://github.com/acme/other/issues/31'],
    ['another issue number', 'https://github.com/acme/ghostline/issues/32'],
    ['a pull request path', 'https://github.com/acme/ghostline/pull/31'],
    ['a non-GitHub URL', 'https://evil.example/acme/ghostline/issues/31'],
  ])('rejects a referenced issue URL with %s', (_reason, url) => {
    const candidate = { ...validPullRequest, referencedIssues: { ...referencedIssues, items: [{ ...referencedIssues.items[0], url }] } }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })

  it('rejects unknown issue state values', () => {
    const candidate = { ...validPullRequest, referencedIssues: { ...referencedIssues, items: [{ ...referencedIssues.items[0], state: 'merged' }] } }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })

  it('accepts unavailable issue evidence with an explanation', () => {
    const candidate = {
      ...validPullRequest,
      referencedIssues: { availability: 'unavailable', reason: 'GitHub could not confirm the issue state.', items: [{ ...referencedIssues.items[0], state: 'unavailable' }] },
    }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(true)
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
