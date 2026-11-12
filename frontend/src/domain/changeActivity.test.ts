import { describe, expect, it } from 'vitest'
import { buildChangeActivity } from './changeActivity'
import type { RevisionRef } from './reviews'
import type { QodoReview } from './reviews'

const commit: RevisionRef = {
  sha: 'a'.repeat(40), shortSha: 'aaaaaaa', message: 'Implement retry guard', author: 'Felix',
  committedAt: '2026-11-04T09:00:00Z', url: `https://github.com/acme/app/commit/${'a'.repeat(40)}`,
}
const review: QodoReview = {
  id: 21, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]',
  kind: 'findings', summary: 'One concern to inspect.', findings: [], commitSha: 'a'.repeat(40),
  commitUrl: `https://github.com/acme/app/commit/${'a'.repeat(40)}`, createdAt: '2026-11-04T09:05:00Z',
  url: 'https://github.com/acme/app/pull/7#issuecomment-21',
}

describe('change activity', () => {
  it('combines commits and Qodo comments by their own timestamps with source-specific labels', () => {
    const activity = buildChangeActivity([commit], [review])

    expect(activity.map((event) => [event.kind, event.timestamp, event.timestampLabel])).toEqual([
      ['commit', commit.committedAt, 'COMMIT METADATA TIME'],
      ['qodo_review', review.createdAt, 'QODO COMMENT TIME'],
    ])
  })

  it('keeps an unmarked Qodo comment visible without assigning it to a PR revision', () => {
    const unmarked = { ...review, commitSha: undefined, commitUrl: undefined }
    const event = buildChangeActivity([], [unmarked])[0]

    expect(event).toMatchObject({ kind: 'qodo_review', revisionLabel: 'REVISION NOT MARKED' })
    expect(event).not.toHaveProperty('revision')
  })

  it('uses deterministic ordering for equal timestamps without claiming a causal order', () => {
    const sameTime = { ...review, createdAt: commit.committedAt }
    const activity = buildChangeActivity([commit], [sameTime])

    expect(activity.map((event) => event.kind)).toEqual(['commit', 'qodo_review'])
    expect(activity.every((event) => event.timestamp === commit.committedAt)).toBe(true)
  })

  it('does not invent an in-PR revision link for a review marker outside the imported commit list', () => {
    const outside = { ...review, commitSha: 'b'.repeat(40), commitUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}` }
    const event = buildChangeActivity([], [outside])[0]

    expect(event).toMatchObject({ revisionLabel: 'MARKED REVISION OUTSIDE IMPORT' })
    expect(event).not.toHaveProperty('revision')
  })

  it('keeps long Qodo PR summaries as a compact event label and bounded detail', () => {
    const longSummary = 'A useful Qodo summary. '.repeat(20)
    const event = buildChangeActivity([], [{ ...review, kind: 'summary', findings: [], summary: longSummary }])[0]

    expect(event?.title).toBe('Qodo pull request summary')
    expect(event?.detail.length).toBeLessThanOrEqual(140)
    expect(event?.detail).toContain('A useful Qodo summary.')
  })
})
