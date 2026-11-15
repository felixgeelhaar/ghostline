import { describe, expect, it } from 'vitest'
import { noxReportHeadline, noxReportNeedsAttention, NoxReportSchema } from './noxReport'

const sha = 'c'.repeat(40)
const valid = {
  availability: 'available', revisionSha: sha, runId: 12, runName: 'Nox', runConclusion: 'success',
  runUrl: 'https://github.com/acme/app/actions/runs/12', artifactId: 4,
  artifactUrl: 'https://github.com/acme/app/actions/runs/12', artifactDigest: 'sha256:abc',
  createdAt: '2026-11-15T13:58:17Z', expiresAt: '2026-11-22T13:58:16Z', totalCount: 1,
  truncated: false, findings: [{ ruleId: 'SEC-1', title: 'Input validation', severity: 'critical', message: 'Validate input.', path: 'backend/server.go', line: 42 }],
}

describe('Nox report evidence schema', () => {
  it('accepts a report with source, expiry and exact revision', () => {
    expect(NoxReportSchema.parse(valid).revisionSha).toBe(sha)
  })

  it('rejects unknown severities and invalid source URLs', () => {
    expect(NoxReportSchema.safeParse({ ...valid, findings: [{ ...valid.findings[0], severity: 'urgent' }] }).success).toBe(false)
    expect(NoxReportSchema.safeParse({ ...valid, runUrl: 'javascript:alert(1)' }).success).toBe(false)
  })

  it('marks findings and unsuccessful workflows for attention', () => {
    expect(noxReportNeedsAttention(NoxReportSchema.parse(valid))).toBe(true)
    expect(noxReportNeedsAttention(NoxReportSchema.parse({ ...valid, totalCount: 0, findings: [] }))).toBe(false)
    expect(noxReportNeedsAttention(NoxReportSchema.parse({ ...valid, totalCount: 0, findings: [], runConclusion: 'failure' }))).toBe(true)
    expect(noxReportNeedsAttention(NoxReportSchema.parse({ availability: 'unavailable', revisionSha: sha, reason: 'Not retained', totalCount: 0, truncated: false, findings: [] }))).toBe(true)
  })

  it('does not describe an unsuccessful scan with no findings as clean', () => {
    const failed = NoxReportSchema.parse({ ...valid, totalCount: 0, findings: [], runConclusion: 'failure' })
    expect(noxReportHeadline(failed)).toBe('Scan did not complete successfully · results may be incomplete')
    expect(noxReportHeadline(NoxReportSchema.parse({ ...valid, totalCount: 2, findings: [], runConclusion: 'failure' })))
      .toBe('2 security findings reported · scan did not complete successfully')
    expect(noxReportHeadline(NoxReportSchema.parse({ ...valid, totalCount: 0, findings: [] })))
      .toBe('No security findings reported')
    expect(noxReportHeadline(NoxReportSchema.parse({ availability: 'unavailable', revisionSha: sha, reason: 'Artifact expired', totalCount: 0, truncated: false, findings: [] })))
      .toBe('Nox report unavailable')
  })
})
