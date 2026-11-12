import { describe, expect, it } from 'vitest'
import { StructuredQodoReviewSetSchema, structuredQodoNextAction, structuredRevisionLabel } from './structuredQodoReviews'

const head = 'a'.repeat(40)

function fixture(overrides: Record<string, unknown> = {}) {
  return StructuredQodoReviewSetSchema.parse({
    provider: 'Qodo',
    sourceType: 'qodo_agentic_toolbox',
    currentRevisionSha: head,
    reviewSession: { id: 'run-current', status: 'completed', commitSha: head, revisionStatus: 'current', reviewComplete: true },
    findings: [{
      id: 'finding-native-id', title: 'Reject stale ownership', description: 'The cached owner can be old.', category: 'bug',
      actionLevel: 'action_required', attributionStatus: 'unclassified', status: 'open', gitSha: head, revisionStatus: 'current', reviewRunIds: ['run-current'],
      locations: [{ filePath: 'backend/cache.go', startLine: 31, endLine: 32, side: 'RIGHT', codeSnippet: 'if owner == current {' }],
      citations: [{ sourceType: 'pull_request', source: 'https://github.com/acme/app/pull/7', citation: 'The owner can be stale.' }],
    }],
    reviewRuns: [{ id: 'run-current', status: 'completed', commitSha: head, revisionStatus: 'current', startedAt: '2026-11-12T10:00:00Z', reviewComplete: true }],
    ...overrides,
  })
}

describe('structured Qodo review evidence', () => {
  it('validates and preserves native finding, run, citation, location, and revision data', () => {
    const result = fixture()
    expect(result.findings[0]).toMatchObject({ id: 'finding-native-id', gitSha: head, reviewRunIds: ['run-current'], locations: [{ filePath: 'backend/cache.go', startLine: 31 }], citations: [{ source: 'https://github.com/acme/app/pull/7' }] })
  })

  it('rejects unknown revision status and malformed current revision payloads', () => {
    expect(() => StructuredQodoReviewSetSchema.parse({ ...fixture(), currentRevisionSha: 'short' })).toThrow()
    expect(() => StructuredQodoReviewSetSchema.parse({ ...fixture(), findings: [{ ...fixture().findings[0], revisionStatus: 'probably-current' }] })).toThrow()
  })

  it('gives explicit next steps for stale, unanchored, incomplete, open, and unclassified findings', () => {
    expect(structuredQodoNextAction(fixture({ findings: [{ ...fixture().findings[0], revisionStatus: 'stale' }] }))).toContain('earlier pull-request revision')
    expect(structuredQodoNextAction(fixture({ findings: [{ ...fixture().findings[0], revisionStatus: 'missing', gitSha: undefined }] }))).toContain('cannot be matched')
    expect(structuredQodoNextAction(fixture({ reviewSession: { ...fixture().reviewSession!, status: 'running', reviewComplete: false } }))).toContain('incomplete or still running')
    expect(structuredQodoNextAction(fixture({ findings: [{ ...fixture().findings[0], status: 'open', attributionStatus: 'classified' }] }))).toContain('marks 1 finding open')
    expect(structuredQodoNextAction(fixture())).toContain('unclassified finding evidence')
    expect(structuredQodoNextAction(fixture({ findings: [{ ...fixture().findings[0], attributionStatus: 'classified' }] }))).toContain('Inspect each finding')
  })

  it('keeps the no-findings result from becoming a correctness claim', () => {
    const action = structuredQodoNextAction(fixture({ findings: [] }))
    expect(action).toContain('returned no findings for the current PR revision')
    expect(action).toContain('Treat that as reviewer feedback')
    expect(action).toContain('required checks independently')
  })

  it('does not describe an empty response as current when its run revision is stale or missing', () => {
    const stale = structuredQodoNextAction(fixture({
      reviewSession: { ...fixture().reviewSession!, commitSha: 'b'.repeat(40), revisionStatus: 'stale' },
      findings: [],
    }))
    expect(stale).toContain('earlier pull-request revision')
    expect(stale).not.toContain('no findings for the current PR revision')

    const missing = structuredQodoNextAction(fixture({
      reviewSession: { ...fixture().reviewSession!, commitSha: undefined, revisionStatus: 'missing' },
      findings: [],
    }))
    expect(missing).toContain('reviewed revision cannot be established')
    expect(missing).not.toContain('no findings for the current PR revision')
  })

  it('does not call an empty result current when Qodo returned no linked review run', () => {
    const action = structuredQodoNextAction(fixture({ reviewSession: undefined, reviewRuns: [], findings: [] }))
    expect(action).toContain('no review run is linked to establish which revision Qodo reviewed')
    expect(action).not.toContain('no findings for the current PR revision')
  })

  it('requires a completed run before interpreting an empty current-revision result', () => {
    const action = structuredQodoNextAction(fixture({
      reviewSession: { ...fixture().reviewSession!, reviewComplete: undefined, status: 'unknown' },
      findings: [],
    }))
    expect(action).toContain('did not confirm that the review run completed')
    expect(action).not.toContain('no findings for the current PR revision')
  })

  it('labels revision evidence without relying on color', () => {
    expect(structuredRevisionLabel('missing')).toBe('REVISION NOT REPORTED')
    expect(structuredRevisionLabel('unknown')).toBe('REVISION UNKNOWN')
  })
})
