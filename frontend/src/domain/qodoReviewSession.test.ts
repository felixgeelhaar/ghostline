import { describe, expect, it } from 'vitest'
import { findingRevisionFreshness, isReviewSessionCurrent, qodoCitationURL, qodoRevisionURL, QodoStructuredReviewSchema, reviewRunForFinding, reviewSessionFreshness, reviewAttentionSummary } from './qodoReviewSession'

const shaA = 'a'.repeat(40)
const shaB = 'b'.repeat(40)

describe('structured Qodo review evidence', () => {
  it('only links secure Qodo citations and revisions from validated public GitHub PR URLs', () => {
    const pullRequest = 'https://github.com/acme/payments/pull/42'
    expect(qodoCitationURL(pullRequest, 'https://github.com/acme/payments/blob/main/review.md#finding')).toBe('https://github.com/acme/payments/blob/main/review.md#finding')
    expect(qodoCitationURL(pullRequest, 'https://github.com/other/repo/blob/main/review.md')).toBeUndefined()
    expect(qodoCitationURL(pullRequest, 'https://docs.example.test/review#finding')).toBeUndefined()
    expect(qodoCitationURL(pullRequest, 'http://github.com/acme/payments/review')).toBeUndefined()
    expect(qodoCitationURL(pullRequest, 'https://user:secret@github.com/acme/payments/review')).toBeUndefined()
    expect(qodoRevisionURL('https://github.com/acme/payments/pull/42', shaA)).toBe(`https://github.com/acme/payments/commit/${shaA}`)
    expect(qodoRevisionURL('https://github.com.evil.test/acme/payments/pull/42', shaA)).toBeUndefined()
    expect(qodoRevisionURL('https://github.com/acme/payments/pull/42?redirect=https://evil.test', shaA)).toBeUndefined()
    expect(qodoRevisionURL('https://github.com/acme/payments/pull/42', 'not-a-sha')).toBeUndefined()
  })

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

  it('accepts review-run evidence when Qodo omitted the run status', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo', reviewSession: null,
      findings: [{ id: 'finding-1', title: 'Keep findings visible' }],
      reviewRuns: [{ id: 'run-1', commitSha: shaA }],
    })
    expect(review.reviewRuns[0]).toMatchObject({ id: 'run-1', commitSha: shaA })
    expect(reviewAttentionSummary(review, shaA).nextAction).toContain('Inspect')
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
    expect(findingRevisionFreshness({ id: 'finding-2', title: 'Unknown revision' }, shaB)).toBe('missing')
  })

  it('keeps a missing finding revision distinct from an unknown pull request head', () => {
    const withRevision = { id: 'finding-1', title: 'Guard retries', gitSha: shaA }
    const withoutRevision = { id: 'finding-2', title: 'No revision', reviewRunId: 'run-1' }

    expect(findingRevisionFreshness(withRevision, '')).toBe('unknown')
    expect(findingRevisionFreshness(withRevision, 'not-a-sha')).toBe('unknown')
    expect(findingRevisionFreshness(withoutRevision, shaA)).toBe('missing')
    expect(findingRevisionFreshness(withoutRevision, '')).toBe('missing')
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

  it('waits for an active review before acting when its revision is unknown', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'running' },
      findings: [{ id: 'finding-1', title: 'Check idempotency', status: 'open' }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary).toMatchObject({ kind: 'in-progress', title: 'Qodo review is still running', openFindingCount: 1 })
    expect(summary.detail).toContain('did not provide a revision')
    expect(summary.nextAction.toLowerCase()).toContain('wait')
    expect(summary.nextAction).not.toContain('inspect the original Qodo source')
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

  it('surfaces missing finding revision evidence alongside open status', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-open-unanchored', title: 'Validate retry key', status: 'open', reviewRunId: 'run-1' }],
      reviewRuns: [{ id: 'run-1', status: 'completed', commitSha: shaA }],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary).toMatchObject({ kind: 'attention', openFindingCount: 1, missingFindingRevisionCount: 1 })
    expect(summary.detail).toContain('no finding-specific revision')
    expect(summary.detail).toContain('cannot be tied to the imported PR head')
    expect(summary.nextAction).toContain('confirm missing finding-specific revisions against their Qodo sources and linked runs')
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
        id: `finding-${index}`, title: 'Review finding', attributionStatus, gitSha: shaA,
      })), reviewRuns: [],
    })
    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'clear', openFindingCount: 0, unspecifiedFindingCount: 0, unrecognizedFindingCount: 0,
      implementedFindingCount: 2, dismissedFindingCount: 1,
    })
  })

  it('falls back to a recognized lifecycle status when attribution is not a lifecycle state', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard retries', attributionStatus: 'attributed', status: 'implemented', gitSha: shaA }],
      reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'clear', openFindingCount: 0, unrecognizedFindingCount: 0, implementedFindingCount: 1,
    })
  })

  it('uses a recognized attribution lifecycle state over a conflicting legacy status', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard retries', attributionStatus: 'pending', status: 'implemented' }],
      reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaA)).toMatchObject({ kind: 'attention', openFindingCount: 1, implementedFindingCount: 0 })
  })

  it('does not report closed findings as clear when finding revisions are missing', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard retries', status: 'implemented', reviewRunId: 'run-1' }],
      reviewRuns: [{ id: 'run-1', status: 'completed', commitSha: shaA }],
    })

    expect(reviewAttentionSummary(review, shaA)).toMatchObject({
      kind: 'attention', title: 'Finding revision missing', missingFindingRevisionCount: 1,
    })
    expect(reviewAttentionSummary(review, shaA).detail).toContain('finding-specific revision')
    expect(reviewAttentionSummary(review, shaA).detail).toContain('linked run revision')
  })

  it('does not report a clear review when a closed finding is tied to an older revision', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaB },
      findings: [{ id: 'finding-1', title: 'Guard retries', status: 'implemented', gitSha: shaA }],
      reviewRuns: [],
    })

    expect(reviewAttentionSummary(review, shaB)).toMatchObject({
      kind: 'attention', title: 'Finding revision needs review',
    })
    expect(reviewAttentionSummary(review, shaB).detail).toContain(shaA.slice(0, 7))
    expect(reviewAttentionSummary(review, shaB).nextAction.toLowerCase()).toContain('confirm')
  })

  it('keeps finding revision attention visible while Qodo is running', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'running', commitSha: shaB },
      findings: [{ id: 'finding-1', title: 'Guard retries', status: 'implemented', gitSha: shaA }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary).toMatchObject({ kind: 'attention', staleFindingCount: 1 })
    expect(summary.nextAction.toLowerCase()).toContain('wait for the qodo session to complete')
  })

  it('waits for a running stale-revision Qodo session before suggesting a refresh', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'running', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard retries', status: 'open', gitSha: shaA }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary.kind).toBe('stale')
    expect(summary.nextAction.toLowerCase()).toContain('wait for the current qodo review to finish')
  })

  it('waits for a current-head review run when the returned session is stale', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard retries', status: 'open', gitSha: shaA }],
      reviewRuns: [{ id: 'run-current', status: 'running', commitSha: shaB }],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary).toMatchObject({ kind: 'in-progress', title: 'Qodo review is still running' })
    expect(summary.detail).toContain('active review run run-current')
    expect(summary.detail).toContain(shaB.slice(0, 7))
    expect(summary.nextAction.toLowerCase()).toContain('wait for the current qodo review run to finish')
    expect(summary.nextAction).not.toContain('load a Qodo review for the current PR head')
  })

  it('does not call a finding stale when its own revision matches a stale session head', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-current', title: 'Guard retries', status: 'open', gitSha: shaB }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary.kind).toBe('stale')
    expect(summary.staleFindingCount).toBe(0)
    expect(summary.currentFindingCount).toBe(1)
    expect(summary.detail).toContain('Session-level freshness is stale')
    expect(summary.detail).toContain('1 finding has a revision marker matching the imported PR head')
    expect(summary.nextAction).toContain('revision matches the imported head')
    expect(summary.detail).not.toContain('findings do not describe the current head')
  })

  it('shows findings when session status is missing and labels completion unknown', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { commitSha: shaB },
      findings: [{ id: 'finding-1', title: 'Guard retries', status: 'open', gitSha: shaB }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary).toMatchObject({ kind: 'incomplete', title: 'Qodo review is not complete', openFindingCount: 1 })
    expect(summary.nextAction.toLowerCase()).toContain('open finding')
  })

  it('includes open and unclassified findings in stale-session next steps', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [
        { id: 'finding-open', title: 'Open', status: 'open', gitSha: shaB },
        { id: 'finding-unknown', title: 'Unknown', gitSha: shaA },
      ],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaB)
    expect(summary.kind).toBe('stale')
    expect(summary.nextAction).toContain('1 Qodo-reported open finding')
    expect(summary.nextAction).toContain('1 finding with no recognized status')
  })

  it('keeps stale finding revisions visible while an unknown-revision session is running', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'running' },
      findings: [{ id: 'finding-stale', title: 'Guard retries', status: 'open', gitSha: shaB }],
      reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.kind).toBe('in-progress')
    expect(summary.staleFindingCount).toBe(1)
    expect(summary.detail).toContain('finding-stale references')
    expect(summary.detail).toContain(shaB.slice(0, 7))
  })

  it('preserves Qodo code snippets as finding source context', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo', reviewSession: null,
      findings: [{ id: 'finding-code', title: 'Guard replay', codeSnippet: 'if replayed {\n  return nil\n}' }], reviewRuns: [],
    })

    expect(review.findings[0]?.codeSnippet).toBe('if replayed {\n  return nil\n}')
  })

  it('does not describe a Qodo implemented status as independently verified correctness', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard duplicates', status: 'implemented', gitSha: shaA }], reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.kind).toBe('clear')
    expect(summary.detail).toContain('Qodo marks 1 finding implemented')
    expect(summary.detail).toContain('not independently verify')
  })

  it('keeps source warnings visible without replacing useful review guidance', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-1', title: 'Guard duplicate requests', status: 'open', gitSha: shaA }],
      reviewRuns: [], warnings: ['An additional response block could not be interpreted.'],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary).toMatchObject({ kind: 'incomplete', title: 'Qodo response may be incomplete', openFindingCount: 1 })
    expect(summary.nextAction).toContain('Inspect the open findings below')
    expect(summary.detail).toContain('Qodo reports 1 finding as open')
    expect(summary.detail).toContain('An additional response block could not be interpreted.')
    expect(summary.nextAction.toLowerCase()).toContain('retry the structured qodo review')
  })

  it('keeps closed-finding inspection guidance when a response is incomplete', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [{ id: 'finding-implemented', title: 'Guard duplicate requests', status: 'implemented', gitSha: shaA }],
      reviewRuns: [], warnings: ['Qodo truncated the extended review response.'],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary).toMatchObject({ kind: 'incomplete', title: 'Qodo response may be incomplete', implementedFindingCount: 1 })
    expect(summary.nextAction).toContain('Inspect the source findings and revision links before deciding whether to ship.')
    expect(summary.nextAction).toContain('Retry the structured Qodo review')
  })

  it('does not present an empty partial response as a clear review', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed', commitSha: shaA },
      findings: [], reviewRuns: [],
      warnings: ['An additional response block could not be interpreted.'],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary).toMatchObject({ kind: 'incomplete', title: 'Qodo response may be incomplete', openFindingCount: 0 })
    expect(summary.detail).toContain('No findings were present in the interpretable response')
    expect(summary.nextAction).toContain('Retry the structured Qodo review')
    expect(summary.nextAction).not.toContain('when deciding whether to ship')
  })

  it('keeps the wait step when a running review response is partial', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'running', commitSha: shaA },
      findings: [], reviewRuns: [],
      warnings: ['An additional response block could not be interpreted.'],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.kind).toBe('incomplete')
    expect(summary.nextAction.toLowerCase()).toContain('wait')
    expect(summary.nextAction).toContain('After it completes')
    expect(summary.nextAction.toLowerCase()).toContain('retry the structured qodo review')
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

  it('surfaces a finding revision mismatch when Qodo omits session metadata', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo', reviewSession: null,
      findings: [{ id: 'finding-old', title: 'Inspect retry handling', status: 'open', gitSha: shaB }], reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.staleFindingCount).toBe(1)
    expect(summary.detail).toContain(`finding-old references ${shaB.slice(0, 7)}`)
    expect(summary.detail).toContain(shaA.slice(0, 7))
  })

  it('surfaces a finding revision mismatch when Qodo omits the session revision', () => {
    const review = QodoStructuredReviewSchema.parse({
      availability: 'available', source: 'Qodo',
      reviewSession: { status: 'completed' },
      findings: [{ id: 'finding-old', title: 'Inspect retry handling', status: 'open', gitSha: shaB }], reviewRuns: [],
    })

    const summary = reviewAttentionSummary(review, shaA)
    expect(summary.staleFindingCount).toBe(1)
    expect(summary.detail).toContain(`finding-old references ${shaB.slice(0, 7)}`)
    expect(summary.detail).toContain(shaA.slice(0, 7))
  })
})
