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
    runConclusion: z.string(),
    runUrl: webURL,
    artifactId: z.number().int().positive(),
    artifactUrl: webURL,
    artifactDigest: z.string(),
    createdAt: z.string().datetime(),
    expiresAt: z.string().datetime(),
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
}

export function noxReportHeadline(report: NoxReport): string {
  if (report.availability === 'unavailable') return 'Nox report unavailable'

  if (report.activeCount === 0 && report.suppressedCount > 0) {
    const suppressed = `${report.suppressedCount} suppressed result${report.suppressedCount === 1 ? '' : 's'}`
    return report.runConclusion === 'success'
      ? `No active findings · ${suppressed}`
      : `No active findings · ${suppressed} · scan did not complete successfully`
  }

  if (report.totalCount === 0) {
    return report.runConclusion === 'success'
      ? 'No security findings reported'
      : 'Scan did not complete successfully · results may be incomplete'
  }

  const active = `${report.activeCount} active security finding${report.activeCount === 1 ? '' : 's'} reported`
  const suppressed = report.suppressedCount > 0
    ? ` · ${report.suppressedCount} suppressed result${report.suppressedCount === 1 ? '' : 's'}`
    : ''
  const count = `${active}${suppressed}`
  return report.runConclusion === 'success' ? count : `${count} · scan did not complete successfully`
}
