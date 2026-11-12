import { describe, expect, it } from 'vitest'
import { QodoReviewSetSchema, findRecurringFindings, latestQodoReviewCoverage, laterRevisionsToInspect, noOpenFindingsNote, qodoReviewState, reviewCountSummary, reviewsForRevision } from './reviews'

const sha = 'a'.repeat(40)

describe('Qodo review evidence', () => {
  it('compares the latest code-review comment with the current PR head', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      unrecognizedCommentCount: 0,
      items: [
        { id: 1, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [], commitSha: 'b'.repeat(40), commitUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' },
        { id: 2, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'summary', findings: [], createdAt: '2026-11-04T09:02:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-2' },
        { id: 3, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [], commitSha: sha, commitUrl: `https://github.com/acme/app/commit/${sha}`, createdAt: '2026-11-04T09:01:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-3' },
      ],
    })

    expect(latestQodoReviewCoverage(reviews, sha)).toMatchObject({ status: 'covers_head', review: { id: 3 } })
  })

  it('distinguishes a stale review, an unmarked review, no review, and unavailable comments', () => {
    const review = (id: number, createdAt: string, commitSha?: string) => ({
      id, provider: 'Qodo' as const, sourceType: 'github_issue_comment' as const,
      sourceAuthor: 'qodo-code-review[bot]', kind: 'findings' as const, findings: [],
      ...(commitSha ? { commitSha, commitUrl: `https://github.com/acme/app/commit/${commitSha}` } : {}),
      createdAt, url: `https://github.com/acme/app/pull/7#issuecomment-${id}`,
    })
    const available = (items: ReturnType<typeof review>[]) => QodoReviewSetSchema.parse({ availability: 'available', unrecognizedCommentCount: 0, items })

    const olderCommit = { sha: 'b'.repeat(40), shortSha: 'bbbbbbb', message: 'Earlier', author: 'A', committedAt: '2026-11-04T08:00:00Z', url: `https://github.com/acme/app/commit/${'b'.repeat(40)}` }
    expect(latestQodoReviewCoverage(available([review(1, '2026-11-04T09:00:00Z', olderCommit.sha)]), sha, [olderCommit])).toMatchObject({ status: 'different_revision', review: { id: 1 }, reviewedRevisionInPullRequest: true })
    expect(latestQodoReviewCoverage(available([review(1, '2026-11-04T09:00:00Z', olderCommit.sha)]), sha)).toMatchObject({ status: 'different_revision', reviewedRevisionInPullRequest: false })
    expect(latestQodoReviewCoverage(available([review(2, '2026-11-04T09:01:00Z')]), sha)).toMatchObject({ status: 'unmarked', review: { id: 2 } })
    expect(latestQodoReviewCoverage(available([review(3, '2026-11-04T09:02:00Z', olderCommit.sha)]))).toMatchObject({ status: 'head_unknown', review: { id: 3 } })
    expect(latestQodoReviewCoverage(available([]), sha)).toEqual({ status: 'no_review' })
    expect(latestQodoReviewCoverage(QodoReviewSetSchema.parse({ availability: 'unavailable', reason: 'GitHub unavailable', unrecognizedCommentCount: 0, items: [] }), sha)).toEqual({ status: 'unavailable', reason: 'GitHub unavailable' })
  })

  it('accepts source-linked findings tied to the reviewed commit', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      unrecognizedCommentCount: 0,
      items: [{
        id: 17,
        provider: 'Qodo',
        sourceType: 'github_issue_comment',
        sourceAuthor: 'qodo-code-review[bot]',
        kind: 'findings',
        findings: [{ id: '17:1', title: 'Reject stale checks', categories: ['🐞 Bug', '⛨ Security'], lifecycle: '⭐ New', severity: 'Medium', actionLevel: 'Remediation recommended', description: 'Checks from the previous revision can appear current.', sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-17' }],
        commitSha: sha,
        commitUrl: `https://github.com/acme/app/commit/${sha}`,
        createdAt: '2026-11-04T09:00:00Z',
        url: 'https://github.com/acme/app/pull/7#issuecomment-17',
      }],
    })
    expect(reviews.items[0]?.commitSha).toBe(sha)
    expect(reviews.items[0]?.findings[0]?.description).toContain('previous revision')
    expect(reviews.items[0]?.sourceType).toBe('github_issue_comment')
    expect(reviews.items[0]?.findings[0]?.categories).toEqual(['🐞 Bug', '⛨ Security'])
    expect(qodoReviewState(reviews)).toBe('recorded')
  })

  it('distinguishes no Qodo review from unavailable GitHub evidence', () => {
    expect(qodoReviewState(QodoReviewSetSchema.parse({ availability: 'available', unrecognizedCommentCount: 0, items: [] }))).toBe('none')
    expect(qodoReviewState(QodoReviewSetSchema.parse({ availability: 'unavailable', reason: 'not loaded', unrecognizedCommentCount: 0, items: [] }))).toBe('unavailable')
    expect(qodoReviewState(QodoReviewSetSchema.parse({ availability: 'available', unrecognizedCommentCount: 1, items: [] }))).toBe('unrecognized')
    expect(qodoReviewState(QodoReviewSetSchema.parse({ availability: 'available', unrecognizedCommentCount: 1, items: [{ id: 1, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'summary', findings: [], createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' }] }))).toBe('partial')
  })

  it('preserves an explicit no-open-findings review as distinct evidence', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      unrecognizedCommentCount: 0,
      items: [{ id: 26, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'no_open_findings', findings: [], commitSha: sha, commitUrl: `https://github.com/acme/app/commit/${sha}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-26' }],
    })

    expect(qodoReviewState(reviews)).toBe('recorded')
    expect(findRecurringFindings(reviews.items)).toEqual([])
    expect(noOpenFindingsNote(reviews.items[0]!)).toContain('linked revision')
  })

  it('keeps unmarked clean-review feedback scoped to context only', () => {
    expect(noOpenFindingsNote({ commitSha: undefined })).toContain('no revision marker')
    expect(noOpenFindingsNote({ commitSha: undefined })).toContain('does not establish the state of the current revision')
  })

  it('rejects malformed commit references', () => {
    expect(() => QodoReviewSetSchema.parse({ availability: 'available', unrecognizedCommentCount: 0, items: [{ id: 1, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [], commitSha: 'bad', commitUrl: 'https://github.com/acme/app/commit/bad', createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' }] })).toThrow()
  })

  it('only reports exact findings repeated across distinct Qodo comments with source revisions', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      unrecognizedCommentCount: 0,
      items: [
        { id: 1, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [{ id: '1:1', title: 'Reject stale checks', sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-1' }, { id: '1:2', title: 'Reject stale checks', sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-1' }], commitSha: sha, commitUrl: `https://github.com/acme/app/commit/${sha}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' },
        { id: 2, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'summary', findings: [], createdAt: '2026-11-04T09:01:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-2' },
        { id: 3, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [{ id: '3:1', title: ' reject stale checks ', sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-3' }], commitSha: 'b'.repeat(40), commitUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}`, createdAt: '2026-11-04T09:02:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-3' },
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

  it('focuses Qodo evidence on an exact revision and keeps unmarked comments separate', () => {
    const reviews = QodoReviewSetSchema.parse({
      availability: 'available',
      unrecognizedCommentCount: 0,
      items: [
        { id: 1, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [{ id: '1:1', title: 'Current finding', sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-1' }], commitSha: sha, commitUrl: `https://github.com/acme/app/commit/${sha}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-1' },
        { id: 2, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'summary', findings: [], commitSha: 'b'.repeat(40), commitUrl: `https://github.com/acme/app/commit/${'b'.repeat(40)}`, createdAt: '2026-11-04T09:01:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-2' },
        { id: 3, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'summary', findings: [], createdAt: '2026-11-04T09:02:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-3' },
      ],
    }).items

    const selected = reviewsForRevision(reviews, sha)
    expect(selected.matched.map((review) => review.id)).toEqual([1])
    expect(selected.unmarked.map((review) => review.id)).toEqual([3])
    const fullHistory = reviewsForRevision(reviews)
    expect(fullHistory.matched.map((review) => review.id)).toEqual([1, 2])
    expect(fullHistory.unmarked.map((review) => review.id)).toEqual([3])
    expect(reviewCountSummary(reviews)).toBe('3 COMMENTS')
    expect(reviewCountSummary(reviews, sha)).toBe('1 MATCHED · 1 CONTEXT')
  })

  it('offers later PR revisions as unconfirmed follow-up context only', () => {
    const review = {
      id: 17,
      provider: 'Qodo' as const,
      sourceType: 'github_issue_comment' as const,
      sourceAuthor: 'qodo-code-review[bot]',
      kind: 'findings' as const,
      findings: [],
      commitSha: sha,
      commitUrl: `https://github.com/acme/app/commit/${sha}`,
      createdAt: '2026-11-04T09:00:00Z',
      url: 'https://github.com/acme/app/pull/7#issuecomment-17',
    }
    const commits = [
      { sha, shortSha: 'aaaaaaa', message: 'Review target', author: 'A', committedAt: '2026-11-04T08:00:00Z', url: `https://github.com/acme/app/commit/${sha}` },
      { sha: 'b'.repeat(40), shortSha: 'bbbbbbb', message: 'Possible follow-up', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/app/commit/${'b'.repeat(40)}` },
    ]

    expect(laterRevisionsToInspect(review, commits)).toEqual([{ ...commits[1], relationBasis: 'github_pr_commit_order' }])
    expect(laterRevisionsToInspect({ ...review, commitSha: undefined }, commits)).toEqual([])
    expect(laterRevisionsToInspect({ ...review, commitSha: 'c'.repeat(40) }, commits)).toEqual([])
  })
})
