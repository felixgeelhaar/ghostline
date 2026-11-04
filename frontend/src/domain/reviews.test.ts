import { describe, expect, it } from 'vitest'
import { QodoReviewSetSchema, findRecurringFindings, qodoReviewState } from './reviews'

const sha = 'a'.repeat(40)

describe('Qodo review evidence', () => {
  it('accepts source-linked findings tied to the reviewed commit', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      items: [{
        id: 17,
        kind: 'findings',
        findingTitles: ['Reject stale checks'],
        commitSha: sha,
        commitUrl: `https://github.com/acme/app/commit/${sha}`,
        createdAt: '2026-11-04T09:00:00Z',
        url: 'https://github.com/acme/app/pull/7#issuecomment-17',
      }],
    })
    expect(reviews.items[0]?.commitSha).toBe(sha)
    expect(qodoReviewState(reviews)).toBe('recorded')
  })

  it('distinguishes no Qodo review from unavailable GitHub evidence', () => {
    expect(qodoReviewState(QodoReviewSetSchema.parse({ availability: 'available', items: [] }))).toBe('none')
    expect(qodoReviewState(QodoReviewSetSchema.parse({ availability: 'unavailable', reason: 'not loaded', items: [] }))).toBe('unavailable')
  })

  it('rejects malformed commit references', () => {
    expect(() => QodoReviewSetSchema.parse({ availability: 'available', items: [{ id: 1, kind: 'findings', findingTitles: [], commitSha: 'bad', commitUrl: 'https://github.com/acme/app/commit/bad', createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' }] })).toThrow()
  })

  it('only reports exact findings repeated across distinct Qodo comments with source revisions', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      items: [
        { id: 1, kind: 'findings', findingTitles: ['Reject stale checks', 'Reject stale checks'], commitSha: sha, commitUrl: `https://github.com/acme/app/commit/${sha}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' },
        { id: 2, kind: 'summary', findingTitles: ['Reject stale checks'], createdAt: '2026-11-04T09:01:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-2' },
        { id: 3, kind: 'findings', findingTitles: [' reject stale checks '], commitSha: 'b'.repeat(40), commitUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}`, createdAt: '2026-11-04T09:02:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-3' },
      ],
    })

    const repeated = findRecurringFindings(reviews.items)
    expect(repeated).toHaveLength(1)
    expect(repeated[0]?.title).toBe('Reject stale checks')
    expect(repeated[0]?.occurrences).toHaveLength(2)
    expect(repeated[0]?.occurrences.map((item) => item.reviewId)).toEqual([1, 3])
    expect(repeated[0]?.occurrences[0]?.commitSha).toBe(sha)
    expect(repeated[0]?.occurrences[1]?.commitSha).toBe('b'.repeat(40))
  })
})
