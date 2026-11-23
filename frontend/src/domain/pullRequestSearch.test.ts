import { describe, expect, it } from 'vitest'
import { classifyImportInput, PullRequestSearchResultSchema, suggestionStateLabel } from './pullRequestSearch'

describe('classifyImportInput', () => {
  it.each([
    ['https://github.com/acme/widgets/pull/7', { kind: 'pull-request', url: 'https://github.com/acme/widgets/pull/7' }],
    ['  https://github.com/acme/widgets/pull/7 ', { kind: 'pull-request', url: 'https://github.com/acme/widgets/pull/7' }],
    ['github.com/acme/widgets/pull/7', { kind: 'pull-request', url: 'github.com/acme/widgets/pull/7' }],
    ['http://example.com/x', { kind: 'pull-request', url: 'http://example.com/x' }],
    ['acme/widgets#7', { kind: 'pull-request', url: 'https://github.com/acme/widgets/pull/7' }],
    [' acme/my.repo_name-2#12 ', { kind: 'pull-request', url: 'https://github.com/acme/my.repo_name-2/pull/12' }],
    ['widgets', { kind: 'search', query: 'widgets' }],
    ['  fix   flaky test ', { kind: 'search', query: 'fix flaky test' }],
    ['acme/widgets#0', { kind: 'search', query: 'acme/widgets#0' }],
    ['ab', { kind: 'too-short' }],
    ['   ', { kind: 'empty' }],
  ])('%j', (input, expected) => {
    expect(classifyImportInput(input)).toEqual(expected)
  })

  it('caps the search text at the server limit', () => {
    expect(classifyImportInput('x'.repeat(300))).toEqual({ kind: 'search', query: 'x'.repeat(256) })
  })
})

describe('PullRequestSearchResultSchema', () => {
  const item = { repository: 'acme/widgets', owner: 'acme', repo: 'widgets', number: 7, title: 'Add widgets', state: 'draft', updatedAt: '2026-11-20T10:00:00Z', url: 'https://github.com/acme/widgets/pull/7' }

  it('accepts a suggestion whose URL names its repository and number', () => {
    expect(PullRequestSearchResultSchema.safeParse({ items: [item], partial: false }).success).toBe(true)
  })

  it.each([
    ['a URL for another pull request', { ...item, url: 'https://github.com/acme/widgets/pull/8' }],
    ['a URL for another repository', { ...item, url: 'https://github.com/evil/widgets/pull/7' }],
    ['a repository that disagrees with owner and repo', { ...item, repository: 'acme/other' }],
    ['an unknown state', { ...item, state: 'reviewed' }],
    ['a missing update time', { ...item, updatedAt: '' }],
  ])('rejects %s', (_, bad) => {
    expect(PullRequestSearchResultSchema.safeParse({ items: [bad], partial: false }).success).toBe(false)
  })
})

describe('suggestionStateLabel', () => {
  it.each([['open', 'OPEN'], ['draft', 'DRAFT'], ['merged', 'MERGED'], ['closed', 'CLOSED']] as const)('%s', (state, label) => {
    expect(suggestionStateLabel(state)).toBe(label)
  })
})
