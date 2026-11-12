import { z } from 'zod'

const fullSha = z.string().regex(/^[0-9a-f]{40}$/i)
const revisionStatus = z.enum(['current', 'stale', 'missing', 'unknown'])

const StructuredLocationSchema = z.object({
  filePath: z.string(),
  startLine: z.number().int().positive().optional(),
  endLine: z.number().int().positive().optional(),
  side: z.string().optional(),
  codeSnippet: z.string().optional(),
})

const StructuredCitationSchema = z.object({
  sourceType: z.string().optional(),
  source: z.string().optional(),
  citation: z.string().optional(),
})

export const StructuredQodoFindingSchema = z.object({
  id: z.string().min(1),
  title: z.string().min(1),
  description: z.string().optional(),
  category: z.string().optional(),
  actionLevel: z.string().optional(),
  attributionStatus: z.string().optional(),
  status: z.string().optional(),
  gitSha: z.string().optional(),
  revisionStatus,
  reviewRunIds: z.array(z.string()),
  locations: z.array(StructuredLocationSchema),
  citations: z.array(StructuredCitationSchema),
})

export const StructuredQodoReviewRunSchema = z.object({
  id: z.string().min(1),
  status: z.string().optional(),
  commitSha: z.string().optional(),
  revisionStatus,
  startedAt: z.string().optional(),
  reviewEffort: z.string().optional(),
  reviewComplete: z.boolean().optional(),
})

export const StructuredQodoReviewSetSchema = z.object({
  provider: z.literal('Qodo'),
  sourceType: z.literal('qodo_agentic_toolbox'),
  currentRevisionSha: fullSha,
  reviewSession: StructuredQodoReviewRunSchema.optional(),
  findings: z.array(StructuredQodoFindingSchema),
  reviewRuns: z.array(StructuredQodoReviewRunSchema),
})

export type StructuredQodoFinding = z.infer<typeof StructuredQodoFindingSchema>
export type StructuredQodoReviewRun = z.infer<typeof StructuredQodoReviewRunSchema>
export type StructuredQodoReviewSet = z.infer<typeof StructuredQodoReviewSetSchema>

export function structuredQodoNextAction(review: StructuredQodoReviewSet): string {
  const activeRun = review.reviewSession ?? [...review.reviewRuns].sort((left, right) => (right.startedAt ?? '').localeCompare(left.startedAt ?? ''))[0]
  if (activeRun && (activeRun.reviewComplete === false || ['running', 'in_progress', 'pending', 'queued'].includes((activeRun.status ?? '').toLowerCase()))) {
    return `Review run ${activeRun.id} is incomplete or still running. Check its Qodo status before acting on its findings.`
  }
  if (activeRun && activeRun.reviewComplete === undefined && !activeRun.status) {
    return `Qodo did not report whether review run ${activeRun.id} completed. Check its source status before treating this review as finished.`
  }

  if (review.findings.length === 0) {
    if (!activeRun) return 'Qodo returned no findings, but no review run is linked to establish which revision Qodo reviewed. Inspect the source review before interpreting this as current feedback.'
    if (activeRun.revisionStatus === 'stale') return 'Qodo returned no findings for an earlier pull-request revision. Re-import the pull request and confirm that Qodo reviewed the current revision before deciding it has no current findings.'
    if (activeRun.revisionStatus !== 'current') return 'Qodo returned no findings, but the reviewed revision cannot be established. Inspect the source review before treating this as feedback about the current pull-request revision.'

    const runStatus = (activeRun.status ?? '').toLowerCase()
    const completed = activeRun.reviewComplete === true || ['completed', 'complete', 'succeeded', 'success', 'finished'].includes(runStatus)
    if (!completed) return `Qodo returned no findings for the current revision, but did not confirm that the review run completed. Check run ${activeRun.id} before interpreting the result.`

    return `Qodo returned no findings for the current PR revision ${review.currentRevisionSha.slice(0, 7)}. Treat that as reviewer feedback; inspect the revision and its required checks independently.`
  }

  const stale = review.findings.some((finding) => finding.revisionStatus === 'stale')
  if (stale) return 'Some findings describe an earlier pull-request revision. Re-import the pull request and review the current revision before deciding whether they still apply.'

  const unanchored = review.findings.some((finding) => finding.revisionStatus === 'missing' || finding.revisionStatus === 'unknown')
  if (unanchored) return 'Some findings cannot be matched to a known pull-request revision. Inspect their Qodo source and avoid treating them as current until the revision is clarified.'

  const unclassified = review.findings.some((finding) => /unclassified|unknown/i.test(`${finding.status ?? ''} ${finding.attributionStatus ?? ''}`))
  if (unclassified) return 'Review the unclassified finding evidence and decide whether it needs action; Qodo attribution is not Ghostline verification.'

  const open = review.findings.filter((finding) => ['open', 'unresolved'].includes((finding.status ?? '').toLowerCase()))
  if (open.length > 0) return `Qodo marks ${open.length} finding${open.length === 1 ? '' : 's'} open. Inspect each finding at its cited location and revision before deciding what to change.`

  return 'Inspect each finding at its cited location and revision, then decide what to change. Later commits and a finding disappearing from a later review do not prove a repair.'
}

export function structuredRevisionLabel(status: StructuredQodoFinding['revisionStatus']): string {
  switch (status) {
    case 'current': return 'CURRENT PR REVISION'
    case 'stale': return 'EARLIER PR REVISION'
    case 'missing': return 'REVISION NOT REPORTED'
    case 'unknown': return 'REVISION UNKNOWN'
  }
}
