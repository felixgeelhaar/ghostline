import { describe, expect, it } from 'vitest'
import { isReviewSessionCurrent, QodoStructuredReviewSchema, reviewRunForFinding, reviewSessionFreshness } from './qodoReviewSession'

const shaA = 'a'.repeat(40)
const shaB = 'b'.repeat(40)

describe('structured Qodo review evidence', () => {
  it('validates source data and keeps the session revision distinct from the current head', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available',
      source: 'Qodo Agentic Toolbox · get-pr-review-findings',
      reviewSession: { status: 'completed', commitSha: shaA, startedAt: '2026-11-12T09:00:00Z' },
      findings: [{ id: 'finding-1', title: 'Guard duplicate requests', status: 'implemented', gitSha: shaA, reviewRunId: 'run-2' }],
      reviewRuns: [{ id: 'run-2', status: 'completed', commitSha: shaB }],
    })

    expect(isReviewSessionCurrent(review, shaA)).toBe(true)
    expect(isReviewSessionCurrent(review, shaB)).toBe(false)
    expect(reviewRunForFinding(review.findings[0]!, review)?.commitSha).toBe(shaB)
  })

  it('rejects malformed source revisions instead of displaying them as evidence', () => {
    const parsed = QodoStructuredReviewSchema.safeParse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: 'not-a-sha' },
      findings: [], reviewRuns: [],
    })

    expect(parsed.success).toBe(false)
  })

  it('does not invent a review-run relationship when Qodo returned no run identifier', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo', reviewSession: null,
      findings: [{ id: 'finding-1', title: 'Guard duplicate requests' }],
      reviewRuns: [{ id: 'run-2', status: 'completed', commitSha: shaB }],
    })

    expect(reviewRunForFinding(review.findings[0]!, review)).toBeUndefined()
  })

  it('preserves citations when Qodo metadata is null or not an object', () => {
    for (const metadata of [null, ['line 19'], 'line 19']) {
      const parsed = QodoStructuredReviewSchema.safeParse({
        availability: 'available', source: 'Qodo', reviewSession: null,
        findings: [{
          id: 'finding-1', title: 'Guard duplicate requests',
          evidence: { citations: [{ sourceType: 'code', source: 'backend/replay.go', metadata }] },
        }],
        reviewRuns: [],
      })

      expect(parsed.success).toBe(true)
    }
  })

  it('reports unknown freshness when Qodo omitted the reviewed revision', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed' }, findings: [], reviewRuns: [],
    })

    expect(reviewSessionFreshness(review, shaA)).toBe('unknown')
    expect(isReviewSessionCurrent(review, shaA)).toBe(false)
  })

  it('reports unknown freshness when the imported head revision is missing', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA }, findings: [], reviewRuns: [],
    })

    expect(reviewSessionFreshness(review, '')).toBe('unknown')
    expect(isReviewSessionCurrent(review, '')).toBe(false)
  })
})
