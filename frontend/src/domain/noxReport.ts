import { z } from 'zod'

const webURL = z.url({ protocol: /^https?$/ })
const fullSha = z.string().regex(/^[0-9a-f]{40}$/)

export const NoxSuppressionSchema = z.object({
  status: z.string().max(100),
  kind: z.string().max(100),
  justification: z.string().max(1000).optional(),
})

export const NoxFindingSchema = z.object({
  ruleId: z.string().min(1),
  title: z.string().min(1),
  severity: z.enum(['critical', 'high', 'medium', 'low', 'info', 'unknown']),
  message: z.string(),
  path: z.string().optional(),
  line: z.number().int().positive().optional(),
  fingerprint: z.string().optional(),
  suppressed: z.boolean(),
  suppressionKind: z.string().max(100).optional(),
  suppressionJustification: z.string().max(1000).optional(),
  suppressions: z.array(NoxSuppressionSchema).default([]),
  suppressionsTruncated: z.boolean().optional(),
})

export const NoxReportSchema = z.discriminatedUnion('availability', [
  z.object({
    availability: z.literal('available'),
    revisionSha: fullSha,
    runId: z.number().int().positive(),
    runName: z.string(),
    runStatus: z.string().optional(),
    runConclusion: z.string().optional(),
    scanSuccessful: z.boolean().optional(),
    runUrl: webURL.optional(),
    artifactId: z.number().int().positive(),
    artifactUrl: webURL.optional(),
    artifactDigest: z.string(),
    createdAt: z.string().datetime().optional(),
    expiresAt: z.string().datetime().optional(),
    totalCount: z.number().int().nonnegative(),
    activeCount: z.number().int().nonnegative(),
    suppressedCount: z.number().int().nonnegative(),
    truncated: z.boolean(),
    findings: z.array(NoxFindingSchema),
  }),
  z.object({
    availability: z.literal('unavailable'),
    reason: z.string().min(1),
    revisionSha: z.union([fullSha, z.literal('')]),
    runId: z.number().int().positive().optional(),
    runName: z.string().optional(),
    runStatus: z.string().optional(),
    runConclusion: z.string().optional(),
    runUrl: webURL.optional(),
    artifactId: z.number().int().positive().optional(),
    artifactUrl: webURL.optional(),
    artifactDigest: z.string().optional(),
    createdAt: z.string().datetime().optional(),
    expiresAt: z.string().datetime().optional(),
    totalCount: z.number().int().nonnegative(),
    activeCount: z.number().int().nonnegative(),
    suppressedCount: z.number().int().nonnegative(),
    truncated: z.boolean(),
    findings: z.array(NoxFindingSchema),
  }),
]).refine((report) => report.totalCount === report.activeCount + report.suppressedCount
  && (report.truncated ? report.findings.length < report.totalCount : report.findings.length === report.totalCount), {
  message: 'Nox result counts do not match the included findings',
})

export type NoxReport = z.infer<typeof NoxReportSchema>

export function noxSeverityLabel(value: NoxReport['findings'][number]['severity']): string {
  return value.toUpperCase()
}

export function noxReportNeedsAttention(report: NoxReport): boolean {
  return report.availability === 'unavailable'
    || report.findings.length > 0
    || report.suppressedCount > 0
    || report.runConclusion !== 'success'
    || report.scanSuccessful !== true
    || report.runStatus === 'queued'
    || report.runStatus === 'in_progress'
}

export function noxReportCanShowZeroFindingNote(report: NoxReport): boolean {
  return report.availability === 'available'
    && report.totalCount === 0
    && report.runConclusion === 'success'
    && report.runStatus !== 'queued'
    && report.runStatus !== 'in_progress'
    // A zero-result report is clean only when SARIF says the scan completed.
    && report.scanSuccessful === true
}

function workflowOutcome(conclusion: string): string {
  switch (conclusion) {
    case 'failure': return 'failed'
    case 'cancelled': return 'cancelled'
    case 'skipped': return 'skipped'
    case 'timed_out': return 'timed out'
    default: return `outcome ${conclusion.replaceAll('_', ' ')}`
  }
}

function scanStatusSuffix(report: Extract<NoxReport, { availability: 'available' }>): string {
  if (report.runStatus === 'queued' || report.runStatus === 'in_progress') return 'workflow still running; scan may be incomplete'
  if (report.scanSuccessful === false) return 'scan did not complete successfully'
  if (!report.runConclusion) {
    return report.scanSuccessful === true
      ? 'scan completed; workflow outcome unavailable'
      : 'workflow outcome unavailable; scan completeness unknown'
  }
  if (report.runConclusion !== 'success' && report.scanSuccessful === true) return `scan completed; workflow ${workflowOutcome(report.runConclusion)}`
  if (report.runConclusion !== 'success' && report.scanSuccessful === undefined) return `workflow ${workflowOutcome(report.runConclusion)}; scan completeness unknown`
  if (report.scanSuccessful === undefined) return 'workflow succeeded; scan completeness unknown'
  return ''
}

function statusLabel(status: string): string {
  if (status === 'workflow still running; scan may be incomplete') return 'Workflow still running · scan may be incomplete'
  if (status === 'scan did not complete successfully') return 'Scan did not complete successfully · results may be incomplete'
  if (status.endsWith('; scan completeness unknown')) {
    const outcome = status.replace(/^workflow /, '').replace(/; scan completeness unknown$/, '')
    return `Workflow ${outcome} · scan completeness unknown`
  }
  if (status === 'scan completed; workflow outcome unavailable') return 'Scan completed · workflow outcome unavailable'
  return status
}

export function noxReportHeadline(report: NoxReport): string {
  if (report.availability === 'unavailable') return 'Nox report unavailable'

  const status = scanStatusSuffix(report)
  if (report.activeCount === 0 && report.suppressedCount > 0) {
    const suppressed = `${report.suppressedCount} suppressed result${report.suppressedCount === 1 ? '' : 's'}`
    return ['No active findings', suppressed, status].filter(Boolean).join(' · ')
  }

  if (report.totalCount === 0) {
    return status
      ? statusLabel(status)
      : 'No security findings reported'
  }

  const active = `${report.activeCount} active security finding${report.activeCount === 1 ? '' : 's'} reported`
  const suppressed = report.suppressedCount > 0
    ? `${report.suppressedCount} suppressed result${report.suppressedCount === 1 ? '' : 's'}`
    : ''
  return [active, suppressed, status].filter(Boolean).join(' · ')
}
