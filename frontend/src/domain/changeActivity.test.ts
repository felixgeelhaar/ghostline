import { describe, expect, it } from 'vitest'
import { buildChangeActivity, countReviewsWithUnavailableDetails, describeCheckActivity } from './changeActivity'
import type { RevisionChecks } from './checks'
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

  it('does not report zero findings when a recognized finding comment has no parsed details', () => {
    const event = buildChangeActivity([], [{ ...review, findings: [] }])[0]

    expect(event?.detail).toBe('FINDING DETAILS UNAVAILABLE')
    expect(event?.detail).not.toContain('0 FINDINGS')
  })

  it('counts recognized reviews without parsed findings in the domain layer', () => {
    const complete = { ...review, id: 22, findings: [{ id: '22:1', title: 'Guard retries', categories: [], sourceUrl: review.url }] }

    expect(countReviewsWithUnavailableDetails([review, complete])).toBe(1)
  })

  it('counts reviews with some unparsed findings as incomplete', () => {
    const partial = {
      ...review,
      findings: [{ id: '21:1', title: 'Guard retries', categories: [], sourceUrl: review.url }],
      unparsedFindingCount: 1,
    }

    expect(countReviewsWithUnavailableDetails([partial])).toBe(1)
    expect(buildChangeActivity([], [partial])[0]?.detail).toContain('DETAILS UNAVAILABLE')
  })

  it('uses deterministic ordering for equal timestamps without claiming a causal order', () => {
    const sameTime = { ...review, createdAt: commit.committedAt }
    const activity = buildChangeActivity([commit], [sameTime])

    expect(activity.map((event) => event.kind)).toEqual(['commit', 'qodo_review'])
    expect(activity.every((event) => event.timestamp === commit.committedAt)).toBe(true)
  })

  it('preserves a review marker and source link when its revision is outside the imported commit list', () => {
    const outside = { ...review, commitSha: 'b'.repeat(40), commitUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}` }
    const event = buildChangeActivity([], [outside])[0]

    expect(event).toMatchObject({
      revisionLabel: `MARKED REVISION OUTSIDE IMPORT · ${'b'.repeat(7)}`,
      markedRevision: { sha: 'b'.repeat(40), shortSha: 'bbbbbbb', url: outside.commitUrl },
    })
    expect(event).not.toHaveProperty('revision')
  })

  it('keeps long Qodo PR summaries as a compact event label and bounded detail', () => {
    const longSummary = 'A useful Qodo summary. '.repeat(20)
    const event = buildChangeActivity([], [{ ...review, kind: 'summary', findings: [], summary: longSummary }])[0]

    expect(event?.title).toBe('Qodo pull request summary')
    expect(event?.detail.length).toBeLessThanOrEqual(140)
    expect(event?.detail).toContain('A useful Qodo summary.')
  })

  it('adds source-linked check records for the exact checked revision with source timestamps', () => {
    const checks: RevisionChecks = {
      availability: 'available', revisionSha: commit.sha, revisionShortSha: commit.shortSha,
      revisionUrl: commit.url, items: [
        {
          id: 314, name: 'Nox security scan', kind: 'check_run', status: 'completed', conclusion: 'failure',
          url: 'https://github.com/acme/app/actions/runs/314', completedAt: '2026-11-04T09:10:00Z',
        },
        {
          id: 271, name: 'legacy-ci', kind: 'commit_status', status: 'completed', conclusion: 'success',
          updatedAt: '2026-11-04T09:11:00Z',
        },
      ],
    }

    const activity = buildChangeActivity([commit], [review], checks)
    const checkEvents = activity.filter((event) => event.kind === 'github_check')

    expect(checkEvents).toEqual([
      expect.objectContaining({
        id: `check:${commit.sha}:check_run:314`, kind: 'github_check', timestamp: '2026-11-04T09:10:00Z',
        timestampLabel: 'CHECK RUN COMPLETION TIME', title: 'Nox security scan',
        detail: 'FAILURE', sourceUrl: 'https://github.com/acme/app/actions/runs/314',
        sourceLabel: 'VIEW CHECK RUN #314', revisionLabel: 'CHECKED REVISION aaaaaaa',
        revision: expect.objectContaining({ sha: commit.sha }),
      }),
      expect.objectContaining({
        id: `check:${commit.sha}:commit_status:271`, kind: 'github_check', timestamp: '2026-11-04T09:11:00Z',
        timestampLabel: 'COMMIT STATUS UPDATE TIME', title: 'legacy-ci', detail: 'SUCCESS',
        sourceUrl: commit.url, sourceLabel: 'VIEW REVISION CONTEXT · STATUS #271',
        revisionLabel: 'CHECKED REVISION aaaaaaa',
      }),
    ])
  })

  it('uses the start time for a running check and omits check records without a source timestamp', () => {
    const checks: RevisionChecks = {
      availability: 'available', revisionSha: commit.sha, revisionShortSha: commit.shortSha,
      revisionUrl: commit.url, items: [
        { name: 'Queued scan', kind: 'check_run', status: 'in_progress', startedAt: '2026-11-04T09:12:00Z' },
        { name: 'Undated scan', kind: 'check_run', status: 'queued' },
      ],
    }

    const events = buildChangeActivity([commit], [], checks).filter((event) => event.kind === 'github_check')

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({
      timestamp: '2026-11-04T09:12:00Z', timestampLabel: 'CHECK RUN START TIME',
      title: 'Queued scan', detail: 'IN PROGRESS', revisionLabel: 'CHECKED REVISION aaaaaaa',
    })
  })

  it('does not include unavailable or differently scoped checks in the activity index', () => {
    const unavailable: RevisionChecks = {
      availability: 'unavailable', reason: 'offline', revisionSha: commit.sha,
      revisionShortSha: commit.shortSha, revisionUrl: commit.url, items: [],
    }
    const mismatched: RevisionChecks = {
      availability: 'available', revisionSha: 'b'.repeat(40), revisionShortSha: 'bbbbbbb',
      revisionUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}`,
      items: [{ name: 'stale', kind: 'check_run', status: 'completed', conclusion: 'success', completedAt: '2026-11-04T09:10:00Z' }],
    }

    expect(buildChangeActivity([], [], unavailable)).toEqual([])
    expect(buildChangeActivity([commit], [], mismatched).some((event) => event.kind === 'github_check')).toBe(false)
  })

  it('describes partially omitted check records through the domain policy', () => {
    const checks: RevisionChecks = {
      availability: 'available', revisionSha: commit.sha, revisionShortSha: commit.shortSha,
      revisionUrl: commit.url, items: [
        { name: 'Dated scan', kind: 'check_run', status: 'completed', conclusion: 'success', completedAt: '2026-11-04T09:10:00Z' },
        { name: 'Undated scan', kind: 'check_run', status: 'queued' },
      ],
    }
    const events = buildChangeActivity([commit], [], checks)

    expect(describeCheckActivity({ checks, events, revisionSha: commit.sha })).toMatchObject({
      kind: 'warn',
      text: expect.stringContaining('1 of 2 GitHub check records'),
    })
  })

  it('reports an unplaced check revision and leaves complete or empty results unmarked', () => {
    const checks: RevisionChecks = {
      availability: 'available', revisionSha: 'b'.repeat(40), revisionShortSha: 'bbbbbbb',
      revisionUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}`,
      items: [{ name: 'Undated scan', kind: 'check_run', status: 'queued' }],
    }
    const event = buildChangeActivity([], [], checks)

    expect(describeCheckActivity({ checks, events: event, revisionSha: checks.revisionSha })?.text).toContain('could not represent any of the 1 GitHub check records')
    expect(describeCheckActivity({ checks: { ...checks, items: [] }, events: [], revisionSha: checks.revisionSha })?.kind).toBe('info')
    const complete = { ...checks, revisionSha: commit.sha, revisionShortSha: commit.shortSha, revisionUrl: commit.url, items: [{ name: 'Dated scan', kind: 'check_run' as const, status: 'queued', startedAt: '2026-11-04T09:12:00Z' }] }
    expect(describeCheckActivity({ checks: complete, events: buildChangeActivity([commit], [], complete), revisionSha: commit.sha })).toBeUndefined()
  })
})
