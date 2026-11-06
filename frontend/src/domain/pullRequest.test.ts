import { describe, expect, it } from 'vitest'
import { PullRequestSchema, validatePullRequestURL } from './pullRequest'

const validPullRequest = {
  repository: 'acme/ghostline',
  owner: 'acme',
  repo: 'ghostline',
  number: 12,
  title: 'Trust source links',
  state: 'open',
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
}

describe('validatePullRequestURL', () => {
  it('accepts a GitHub PR URL', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/pull/12')).toBeUndefined())
  it('rejects other hosts', () => expect(validatePullRequestURL('https://evil.example/acme/ghostline/pull/12')).toBeTruthy())
  it('rejects a non-pull URL', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/issues/12')).toBeTruthy())
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
  ])('rejects a PR URL with %s', (_reason, override) => {
    expect(PullRequestSchema.safeParse({ ...validPullRequest, ...override }).success).toBe(false)
  })

  it.each([
    ['a non-GitHub host', 'https://evil.example/acme/ghostline/commit/' + 'a'.repeat(40)],
    ['a different repository', 'https://github.com/acme/other/commit/' + 'a'.repeat(40)],
    ['a different commit SHA', 'https://github.com/acme/ghostline/commit/' + 'b'.repeat(40)],
  ])('rejects a commit URL with %s', (_reason, url) => {
    const candidate = { ...validPullRequest, commits: [{ ...validPullRequest.commits[0], url }] }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })

  it('rejects an encoded slash inside a commit path segment', () => {
    const candidate = { ...validPullRequest, commits: [{ ...validPullRequest.commits[0], url: `https://github.com/acme%2Fghostline/commit/${'a'.repeat(40)}` }] }
    expect(PullRequestSchema.safeParse(candidate).success).toBe(false)
  })
})
