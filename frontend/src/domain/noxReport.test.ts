import { describe, expect, it } from 'vitest'
import { noxReportCanShowZeroFindingNote, noxReportHeadline, noxReportNeedsAttention, NoxReportSchema } from './noxReport'

const sha = 'c'.repeat(40)
const valid = {
  availability: 'available', revisionSha: sha, runId: 12, runName: 'Nox', runConclusion: 'success',
  runUrl: 'https://github.com/acme/app/actions/runs/12', artifactId: 4,
  artifactUrl: 'https://github.com/acme/app/actions/runs/12', artifactDigest: 'sha256:abc',
  createdAt: '2026-11-15T13:58:17Z', expiresAt: '2026-11-22T13:58:16Z', totalCount: 1, activeCount: 1, suppressedCount: 0,
  truncated: false, findings: [{ ruleId: 'SEC-1', title: 'Input validation', severity: 'critical', message: 'Validate input.', path: 'backend/server.go', line: 42, suppressed: false }],
}

describe('Nox report evidence schema', () => {
  it('accepts a report with source, expiry and exact revision', () => {
    expect(NoxReportSchema.parse(valid).revisionSha).toBe(sha)
  })

  it('accepts a report from a still-running workflow without inventing a conclusion', () => {
    const pending = NoxReportSchema.parse({
      ...valid,
      runStatus: 'in_progress',
      runConclusion: undefined,
      totalCount: 0,
      activeCount: 0,
      suppressedCount: 0,
      findings: [],
    })
    expect(noxReportNeedsAttention(pending)).toBe(true)
    expect(noxReportHeadline(pending)).toBe('Workflow still running · scan may be incomplete')
  })

  it('keeps finding counts visible while a workflow is still running', () => {
    const pending = NoxReportSchema.parse({
      ...valid,
      runStatus: 'in_progress',
      runConclusion: undefined,
    })
    expect(noxReportHeadline(pending)).toBe('1 active security finding reported · workflow still running; scan may be incomplete')
  })

  it('accepts an available report when GitHub omits optional source metadata', () => {
    const result = NoxReportSchema.parse({
      ...valid,
      runUrl: undefined,
      artifactUrl: undefined,
      createdAt: undefined,
      expiresAt: undefined,
    })
    expect(result.availability).toBe('available')
    if (result.availability === 'available') {
      expect(result.runUrl).toBeUndefined()
      expect(result.artifactUrl).toBeUndefined()
      expect(result.createdAt).toBeUndefined()
      expect(result.expiresAt).toBeUndefined()
    }
  })

  it('does not infer workflow failure when GitHub omits the conclusion', () => {
    const unknown = NoxReportSchema.parse({
      ...valid,
      runStatus: 'completed',
      runConclusion: undefined,
      totalCount: 0,
      activeCount: 0,
      suppressedCount: 0,
      findings: [],
    })
    expect(noxReportHeadline(unknown)).toBe('Workflow outcome unavailable · scan completeness unknown')
    expect(noxReportNeedsAttention(unknown)).toBe(true)
  })

  it('keeps cancelled workflow outcomes distinct from failures', () => {
    const cancelled = NoxReportSchema.parse({
      ...valid,
      runStatus: 'completed',
      runConclusion: 'cancelled',
      totalCount: 0,
      activeCount: 0,
      suppressedCount: 0,
      findings: [],
    })
    expect(noxReportHeadline(cancelled)).toBe('Workflow cancelled · scan completeness unknown')
    expect(noxReportCanShowZeroFindingNote(cancelled)).toBe(false)
    expect(noxReportCanShowZeroFindingNote(NoxReportSchema.parse({
      ...valid, totalCount: 0, activeCount: 0, suppressedCount: 0, findings: [],
    }))).toBe(true)
  })

  it('rejects unknown severities and invalid source URLs', () => {
    expect(NoxReportSchema.safeParse({ ...valid, findings: [{ ...valid.findings[0], severity: 'urgent' }] }).success).toBe(false)
    expect(NoxReportSchema.safeParse({ ...valid, runUrl: 'javascript:alert(1)' }).success).toBe(false)
  })

  it('rejects inconsistent active, suppressed and total result counts', () => {
    expect(NoxReportSchema.safeParse({ ...valid, suppressedCount: 1 }).success).toBe(false)
  })

  it('marks findings and unsuccessful workflows for attention', () => {
    expect(noxReportNeedsAttention(NoxReportSchema.parse(valid))).toBe(true)
    expect(noxReportNeedsAttention(NoxReportSchema.parse({ ...valid, totalCount: 0, activeCount: 0, suppressedCount: 0, findings: [] }))).toBe(false)
    expect(noxReportNeedsAttention(NoxReportSchema.parse({ ...valid, totalCount: 0, activeCount: 0, suppressedCount: 0, findings: [], runConclusion: 'failure', scanSuccessful: false }))).toBe(true)
    expect(noxReportNeedsAttention(NoxReportSchema.parse({ availability: 'unavailable', revisionSha: sha, reason: 'Not retained', totalCount: 0, activeCount: 0, suppressedCount: 0, truncated: false, findings: [] }))).toBe(true)
  })

  it('does not describe an unsuccessful scan with no findings as clean', () => {
    const failed = NoxReportSchema.parse({ ...valid, totalCount: 0, activeCount: 0, suppressedCount: 0, findings: [], runConclusion: 'failure', scanSuccessful: false })
    expect(noxReportHeadline(failed)).toBe('Scan did not complete successfully · results may be incomplete')
    expect(noxReportHeadline(NoxReportSchema.parse({ ...valid, totalCount: 2, activeCount: 2, findings: [valid.findings[0], { ...valid.findings[0], ruleId: 'SEC-2' }], runConclusion: 'failure', scanSuccessful: false })))
      .toBe('2 active security findings reported · scan did not complete successfully')
    expect(noxReportHeadline(NoxReportSchema.parse({ ...valid, totalCount: 0, activeCount: 0, suppressedCount: 0, findings: [] })))
      .toBe('No security findings reported')
    expect(noxReportHeadline(NoxReportSchema.parse({ availability: 'unavailable', revisionSha: sha, reason: 'Artifact expired', totalCount: 0, activeCount: 0, suppressedCount: 0, truncated: false, findings: [] })))
      .toBe('Nox report unavailable')
  })

  it('keeps suppressed results visible and does not describe them as clear or fixed', () => {
    const suppressed = NoxReportSchema.parse({
      ...valid,
      totalCount: 1,
      activeCount: 0,
      suppressedCount: 1,
      findings: [{ ruleId: 'SEC-1', title: 'Known issue', severity: 'medium', message: 'Still reported.', suppressed: true, suppressionKind: 'external', suppressionJustification: 'Accepted baseline' }],
    })
    expect(noxReportNeedsAttention(suppressed)).toBe(true)
    expect(noxReportHeadline(suppressed)).toBe('No active findings · 1 suppressed result')
    expect(suppressed.findings[0].suppressionJustification).toBe('Accepted baseline')
  })
})
