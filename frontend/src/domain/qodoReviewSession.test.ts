import { describe, expect, it } from 'vitest'
import { findingRevisionFreshness, isReviewSessionCurrent, QodoStructuredReviewSchema, reviewRunForFinding, reviewSessionFreshness, reviewAttentionSummary } from './qodoReviewSession'

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

  it('compares each finding revision to the imported head without requiring session metadata', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo', reviewSession: null,
      findings: [{ id: 'finding-1', title: 'Guard retries', gitSha: shaA }], reviewRuns: [],
    })

    expect(findingRevisionFreshness(review.findings[0]!, shaB)).toBe('stale')
    expect(findingRevisionFreshness(review.findings[0]!, shaA)).toBe('current')
    expect(findingRevisionFreshness(review.findings[0]!, '')).toBe('unknown')
    expect(findingRevisionFreshness({ id: 'finding-2', title: 'Unknown revision' }, shaB)).toBe('unknown')
  })

  it('reports unknown freshness when the imported head revision is missing', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA }, findings: [], reviewRuns: [],
    })

    expect(reviewSessionFreshness(review, '')).toBe('unknown')
    expect(isReviewSessionCurrent(review, '')).toBe(false)
  })

  it('asks for a current review when Qodo inspected a different revision', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA }, findings: [], reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaB)).toMatchObject({
      kind: 'stale',
      title: 'Review needs a refresh',
      openFindingCount: 0,
    })
    expect(reviewAttentionSummary(review, shaB).detail).toContain(shaA.slice(0, 7))
    expect(reviewAttentionSummary(review, shaB).detail).toContain(shaB.slice(0, 7))
  })

  it('keeps Qodo-reported open findings visible when the review revision is stale', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [
        { id: 'finding-1', title: 'Validate idempotency', status: 'open' },
        { id: 'finding-2', title: 'Bound replay window', status: 'open' },
      ], reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary.kind).toBe('stale')
    expect(summary.detail).toContain('Qodo reports 2 findings as open')
    expect(summary.nextAction).toContain('Inspect the 2 Qodo-reported open findings')
    expect(summary.nextAction).toContain('current PR head')
  })

  it('asks to confirm freshness when either revision is unavailable', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA }, findings: [], reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, '').kind).toBe('unknown')
    expect(reviewAttentionSummary(review, '').nextAction).toContain('confirm')
  })

  it('keeps open findings actionable when the Qodo reviewed revision is unknown', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed' },
      findings: [{ id: 'finding-1', title: 'Validate idempotency', status: 'open' }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.kind).toBe('unknown')
    expect(summary.detail).toContain('Qodo reports 1 finding as open')
    expect(summary.nextAction).toContain('Inspect the 1 Qodo-reported open finding')
    expect(summary.nextAction).toContain('confirm')
  })

  it('reports the active Qodo session before recommending action on findings', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'started', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Check idempotency', status: 'open' }], reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'in-progress', title: 'Qodo review is still running', openFindingCount: 1,
    })
    expect(reviewAttentionSummary(review, shaA).nextAction.toLowerCase()).toContain('wait')
  })

  it('keeps open and unclassified findings visible during running and incomplete sessions', () => {
    for (const status of ['running', 'failed']) {
      const review = QodoStructuredReviewSchema.parse({
        availability: 'available', source: 'Qodo',
        reviewSession: { status, commitSha: shaA },
        findings: [
          { id: 'finding-open', title: 'Check idempotency', status: 'open' },
          { id: 'finding-unclassified', title: 'Bound retries' },
        ],
        reviewRuns: [],
      })

      const summary = reviewAttentionSummary(review, shaA)
      expect(summary.openFindingCount).toBe(1)
      expect(summary.detail).toContain('Qodo reports 1 finding as open')
      expect(summary.detail).toContain('1 finding has no recognized status')
      expect(summary.nextAction).toContain('Inspect the 1 Qodo-reported open finding')
      expect(summary.nextAction).toContain('1 finding with no recognized status')
    }
  })

  it('surfaces Qodo-reported open and unspecified findings without claiming repairs', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [
        { id: 'finding-open', title: 'Check idempotency', status: 'open' },
        { id: 'finding-implemented', title: 'Validate retry key', status: 'implemented' },
        { id: 'finding-unknown', title: 'Bound retries' },
      ], reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary).toMatchObject({ kind: 'attention', openFindingCount: 1, unspecifiedFindingCount: 1 })
    expect(summary.title).toContain('1 finding')
    expect(summary.detail).toContain('1 finding has no recognized status')
    expect(summary.detail).toContain('not independently verify')
  })

  it('counts Qodo pending attribution as an open finding when the legacy status is absent', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-pending', title: 'Validate retry key', attributionStatus: 'pending' }],
      reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'attention', openFindingCount: 1, unspecifiedFindingCount: 0, unrecognizedFindingCount: 0,
      title: '1 finding reported open',
    })
    expect(reviewAttentionSummary(review, shaA).detail).toContain('Qodo reports 1 finding as open')
  })

  it('maps Qodo open and closed attribution states without treating them as unknown', () => {
    const openStatuses = ['pending', 'partial_implementation', 'not_implemented', 'focus_areas_edited']
    const closedStatuses = ['implemented', 'full_implementation', 'dismissed', 'detected_after_merge', 'outdated']

    for (const attributionStatus of openStatuses) {
      const review = QodoStructuredReviewSchema.parse({
        availability: 'available', source: 'Qodo',
        reviewSession: { status: 'completed', commitSha: shaA },
        findings: [{ id: attributionStatus, title: 'Review finding', attributionStatus }], reviewRuns: [],
      })
      expect(reviewAttentionSummary(review, shaA)).toMatchObject({
        kind: 'attention', openFindingCount: 1, unspecifiedFindingCount: 0, unrecognizedFindingCount: 0,
      })
    }

    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: closedStatuses.map((attributionStatus, index) => ({
        id: `finding-${index}`, title: 'Review finding', attributionStatus,
      })), reviewRuns: [],
    })
    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'clear', openFindingCount: 0, unspecifiedFindingCount: 0, unrecognizedFindingCount: 0,
      implementedFindingCount: 2, dismissedFindingCount: 1,
    })
  })

  it('does not describe a Qodo implemented status as independently verified correctness', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard duplicates', status: 'implemented' }], reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.kind).toBe('clear')
    expect(summary.detail).toContain('Qodo marks 1 finding implemented')
    expect(summary.detail).toContain('not independently verify')
  })

  it('keeps findings visible as next steps when Qodo returns no session', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo', reviewSession: null,
      findings: [{ id: 'finding-1', title: 'Inspect retry handling', status: 'open' }], reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'unknown', openFindingCount: 1, title: 'Review revision unconfirmed',
    })
    expect(reviewAttentionSummary(review, shaA).detail.toLowerCase()).toContain('findings remain available below')
  })
})
