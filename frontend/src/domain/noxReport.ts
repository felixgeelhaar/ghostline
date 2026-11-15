import { z } from 'zod'

const webURL = z.url({ protocol: /^https?$/ })
const fullSha = z.string().regex(/^[0-9a-f]{40}$/)

export const NoxFindingSchema = z.object({
  ruleId: z.string().min(1),
  title: z.string().min(1),
  severity: z.enum(['critical', 'high', 'medium', 'low', 'info', 'unknown']),
  message: z.string(),
  path: z.string().optional(),
  line: z.number().int().positive().optional(),
  fingerprint: z.string().optional(),
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
    truncated: z.boolean(),
    findings: z.array(NoxFindingSchema),
  }),
])

export type NoxReport = z.infer<typeof NoxReportSchema>

export function noxSeverityLabel(value: NoxReport['findings'][number]['severity']): string {
  return value.toUpperCase()
}

export function noxReportNeedsAttention(report: NoxReport): boolean {
  return report.availability === 'unavailable'
    || report.findings.length > 0
    || report.runConclusion !== 'success'
}

export function noxReportHeadline(report: NoxReport): string {
  if (report.availability === 'unavailable') return 'Nox report unavailable'

  if (report.totalCount === 0) {
    return report.runConclusion === 'success'
      ? 'No security findings reported'
      : 'Scan did not complete successfully · results may be incomplete'
  }

  const count = `${report.totalCount} security finding${report.totalCount === 1 ? '' : 's'} reported`
  return report.runConclusion === 'success' ? count : `${count} · scan did not complete successfully`
}
