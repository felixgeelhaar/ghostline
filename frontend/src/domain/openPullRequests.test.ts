import { describe, expect, it } from 'vitest'
import { OpenPullRequestsSchema, rowNextAction, rowVerdict, type OpenPullRequest, type RowCheck } from './openPullRequests'
import type { Readiness } from './readiness'

const head = 'a'.repeat(40)
const moved = 'b'.repeat(40)
const row: OpenPullRequest = { number: 7, title: 'Change', draft: false, author: 'octo', headSha: head, headShortSha: 'aaaaaaa', url: 'https://github.com/acme/app/pull/7' }
const verdict = (headSha: string, overrides: Partial<Readiness> = {}): Readiness => ({
  repository: 'acme/app', number: 7, headSha, headShortSha: headSha.slice(0, 7), headUrl: `https://github.com/acme/app/commit/${headSha}`,
  verdict: 'incomplete', nextAction: 'update branch: 3 behind main', reasons: [], ...overrides,
})

describe('open pull request list', () => {
  it('accepts a list only when every item names its full head', () => {
    const list = { repository: 'acme/app', complete: true, limit: 30, items: [row] }
    expect(OpenPullRequestsSchema.parse(list).items[0].headSha).toBe(head)
    expect(OpenPullRequestsSchema.safeParse({ ...list, items: [{ ...row, headSha: 'abc' }] }).success).toBe(false)
  })

  it('shows a verdict and its next action only for the head the row was listed with', () => {
    const checked: RowCheck = { state: 'checked', readiness: verdict(head) }
    expect(rowVerdict(row, checked)).toBe('incomplete')
    expect(rowNextAction(row, checked)).toBe('update branch: 3 behind main')
  })

  it('marks a row stale when its verdict describes another head', () => {
    const checked: RowCheck = { state: 'checked', readiness: verdict(moved, { verdict: 'pass', nextAction: 'ready: the engineer decides whether to merge' }) }
    expect(rowVerdict(row, checked)).toBe('stale')
    expect(rowNextAction(row, checked)).toBe('head moved from aaaaaaa to bbbbbbb: list again')
  })

  it('marks a row stale when opening it found a newer head', () => {
    expect(rowVerdict(row, { state: 'checked', readiness: verdict(head) }, moved)).toBe('stale')
    expect(rowNextAction(row, { state: 'unchecked' }, moved)).toBe('head moved from aaaaaaa to bbbbbbb: list again')
    expect(rowVerdict(row, { state: 'checked', readiness: verdict(head) }, head)).toBe('incomplete')
  })

  it('says what is still unknown for a row without a verdict', () => {
    expect(rowVerdict(row, { state: 'unchecked' })).toBe('unchecked')
    expect(rowNextAction(row, { state: 'unchecked' })).toBe('check readiness')
    expect(rowNextAction(row, { state: 'checking' })).toBe('checking…')
    expect(rowNextAction(row, { state: 'error', message: 'Too many requests. Try again in 20 seconds.' })).toBe('Too many requests. Try again in 20 seconds.')
  })
})
