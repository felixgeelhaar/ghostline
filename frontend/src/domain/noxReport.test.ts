import { describe, expect, it } from 'vitest'
import { NoxReportSchema } from './noxReport'

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
})
