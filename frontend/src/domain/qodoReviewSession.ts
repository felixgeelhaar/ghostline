import { z } from 'zod'

const fullSha = z.string().regex(/^[0-9a-f]{40}$/i)
const QodoCitationSchema = z.object({
  sourceType: z.string().optional(),
  source: z.string().optional(),
  text: z.string().optional(),
  context: z.string().optional(),
  // Qodo's citation metadata is source-specific and may be null, scalar, or
  // structured JSON. Preserve it without allowing it to invalidate the review.
  metadata: z.unknown().optional(),
})

const QodoFindingEvidenceSchema = z.object({
  explanation: z.string().optional(),
  citations: z.array(QodoCitationSchema).optional(),
})

export const QodoReviewRunSchema = z.object({
  id: z.string().min(1),
  status: z.string().min(1),
  commitSha: fullSha.optional(),
  startedAt: z.string().optional(),
})

export const QodoManagedFindingSchema = z.object({
  id: z.string().min(1),
  title: z.string().min(1),
  description: z.string().optional(),
  category: z.string().optional(),
  actionLevel: z.string().optional(),
  attributionStatus: z.string().optional(),
  gitSha: fullSha.optional(),
  status: z.string().optional(),
  resolutionReason: z.string().optional(),
  reviewRunId: z.string().optional(),
  evidence: QodoFindingEvidenceSchema.optional(),
  filePath: z.string().optional(),
  startLine: z.number().int().positive().optional(),
  endLine: z.number().int().positive().optional(),
  side: z.string().optional(),
})

export const QodoReviewSessionSchema = z.object({
  status: z.string().min(1),
  commitSha: fullSha.optional(),
  startedAt: z.string().optional(),
})

export const QodoStructuredReviewSchema = z.object({
  availability: z.literal('available'),
  source: z.string().min(1),
  reviewSession: QodoReviewSessionSchema.nullable(),
  findings: z.array(QodoManagedFindingSchema),
  reviewRuns: z.array(QodoReviewRunSchema),
})

export type QodoStructuredReview = z.infer<typeof QodoStructuredReviewSchema>
export type QodoManagedFinding = z.infer<typeof QodoManagedFindingSchema>

export function findingRevisionFreshness(finding: QodoManagedFinding, headSha: string): 'current' | 'stale' | 'unknown' {
  if (!fullSha.safeParse(headSha).success || !finding.gitSha) return 'unknown'
  return finding.gitSha.toLowerCase() === headSha.toLowerCase() ? 'current' : 'stale'
}

export type ReviewAttentionSummary = {
  kind: 'unknown' | 'stale' | 'in-progress' | 'incomplete' | 'attention' | 'clear'
  title: string
  detail: string
  nextAction: string
  findingCount: number
  openFindingCount: number
  unspecifiedFindingCount: number
  unrecognizedFindingCount: number
  implementedFindingCount: number
  dismissedFindingCount: number
}

export function reviewSessionFreshness(review: QodoStructuredReview, headSha: string): 'current' | 'stale' | 'unknown' {
  if (!fullSha.safeParse(headSha).success) return 'unknown'
  const reviewedSha = review.reviewSession?.commitSha
  if (!reviewedSha) return 'unknown'
  return reviewedSha.toLowerCase() === headSha.toLowerCase() ? 'current' : 'stale'
}

export function isReviewSessionCurrent(review: QodoStructuredReview, headSha: string): boolean {
  return reviewSessionFreshness(review, headSha) === 'current'
}

const openAttributionStatuses = new Set(['pending', 'partial_implementation', 'not_implemented', 'focus_areas_edited'])
const closedAttributionStatuses = new Set(['implemented', 'full_implementation', 'dismissed', 'detected_after_merge', 'outdated'])

function findingStatus(finding: QodoManagedFinding): string {
  return (finding.attributionStatus ?? finding.status ?? '').trim().toLowerCase()
}

function freshnessFindingDetail(openFindingCount: number, unclassifiedFindingCount: number): string {
  const details: string[] = []
  if (openFindingCount > 0) {
    details.push(`Qodo reports ${openFindingCount} finding${openFindingCount === 1 ? '' : 's'} as open`)
  }
  if (unclassifiedFindingCount > 0) {
    details.push(`${unclassifiedFindingCount} finding${unclassifiedFindingCount === 1 ? ' has' : 's have'} no recognized status`)
  }
  return details.length > 0 ? ` ${details.join('; ')}.` : ''
}

function freshnessNextAction(
  openFindingCount: number,
  unclassifiedFindingCount: number,
  nextStep: string,
): string {
  const findings: string[] = []
  if (openFindingCount > 0) {
    findings.push(`${openFindingCount} Qodo-reported open finding${openFindingCount === 1 ? '' : 's'}`)
  }
  if (unclassifiedFindingCount > 0) {
    findings.push(`${unclassifiedFindingCount} finding${unclassifiedFindingCount === 1 ? '' : 's'} with no recognized status`)
  }
  if (findings.length === 0) return nextStep
  return `Inspect the ${findings.join(' and ')} below, then ${nextStep[0]!.toLowerCase()}${nextStep.slice(1)}`
}

export function reviewAttentionSummary(review: QodoStructuredReview, headSha: string): ReviewAttentionSummary {
  const statuses = review.findings.map(findingStatus)
  const openStatuses = new Set(['open', ...openAttributionStatuses])
  const closedStatuses = new Set(['implemented', 'dismissed', ...closedAttributionStatuses])
  const knownStatuses = new Set([...openStatuses, ...closedStatuses])
  const openFindingCount = statuses.filter((status) => openStatuses.has(status)).length
  const unspecifiedFindingCount = statuses.filter((status) => status === '').length
  const implementedFindingCount = statuses.filter((status) => ['implemented', 'full_implementation'].includes(status)).length
  const dismissedFindingCount = statuses.filter((status) => status === 'dismissed').length
  const unrecognizedFindingCount = statuses.filter((status) => status !== '' && !knownStatuses.has(status)).length
  const unclassifiedFindingCount = unspecifiedFindingCount + unrecognizedFindingCount
  const counts = {
    findingCount: review.findings.length,
    openFindingCount,
    unspecifiedFindingCount,
    unrecognizedFindingCount,
    implementedFindingCount,
    dismissedFindingCount,
  }

  if (!review.reviewSession) {
    return {
      ...counts,
      kind: 'unknown',
      title: 'Review revision unconfirmed',
      detail: `Qodo returned no review session metadata. ${review.findings.length ? 'Findings remain available below, but their session freshness cannot be confirmed.' : 'No session freshness or finding status can be confirmed.'}${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'inspect the original Qodo source and confirm which revision it reviewed.'),
    }
  }

  const freshness = reviewSessionFreshness(review, headSha)
  if (freshness === 'stale') {
    const reviewedShortSha = review.reviewSession.commitSha?.slice(0, 7) ?? 'unknown'
    const headShortSha = fullSha.safeParse(headSha).success ? headSha.slice(0, 7) : 'unknown'
    return {
      ...counts,
      kind: 'stale',
      title: 'Review needs a refresh',
      detail: `Qodo reviewed ${reviewedShortSha}, while the imported PR head is ${headShortSha}. The findings do not describe the current head.${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'load a Qodo review for the current PR head before relying on these findings.'),
    }
  }
  if (freshness === 'unknown') {
    return {
      ...counts,
      kind: 'unknown',
      title: 'Review revision unconfirmed',
      detail: `Ghostline cannot compare Qodo’s reviewed revision with a known pull-request head. Findings remain source data, but their freshness is unknown.${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'inspect the original Qodo source and confirm which revision it reviewed.'),
    }
  }

  const sessionStatus = review.reviewSession.status.trim().toLowerCase()
  if (sessionStatus === 'started' || sessionStatus === 'running') {
    return {
      ...counts,
      kind: 'in-progress',
      title: 'Qodo review is still running',
      detail: `Qodo reports session status “${review.reviewSession.status}” for the current PR head.${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'wait for the Qodo session to complete before refreshing its findings.'),
    }
  }
  if (sessionStatus !== 'completed') {
    return {
      ...counts,
      kind: 'incomplete',
      title: 'Qodo review is not complete',
      detail: `Qodo reports session status “${review.reviewSession.status}” for the current PR head.${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'inspect the Qodo session and decide whether to retry before acting on its findings.'),
    }
  }

  if (openFindingCount > 0) {
    const uncertainty = unclassifiedFindingCount > 0 ? ` ${unclassifiedFindingCount} finding${unclassifiedFindingCount === 1 ? ' has' : 's have'} no recognized status.` : ''
    return {
      ...counts,
      kind: 'attention',
      title: `${openFindingCount} finding${openFindingCount === 1 ? '' : 's'} reported open`,
      detail: `Qodo reports ${openFindingCount} finding${openFindingCount === 1 ? '' : 's'} as open.${uncertainty} Qodo’s implemented or dismissed statuses do not independently verify correctness.`,
      nextAction: 'Inspect the open findings below and follow their source and revision links.',
    }
  }

  if (unclassifiedFindingCount > 0) {
    return {
      ...counts,
      kind: 'attention',
      title: `${unclassifiedFindingCount} finding${unclassifiedFindingCount === 1 ? '' : 's'} need status review`,
      detail: `${unclassifiedFindingCount} finding${unclassifiedFindingCount === 1 ? ' has' : 's have'} no recognized Qodo status. Ghostline does not infer whether they are resolved.`,
      nextAction: 'Inspect those findings and their revision-linked source evidence.',
    }
  }

  if (review.findings.length === 0) {
    return {
      ...counts,
      kind: 'clear',
      title: 'Qodo reported no findings',
      detail: 'The completed Qodo session matches the imported PR head. This review result is not an independent correctness guarantee.',
      nextAction: 'Use the linked revision checks and your own review when deciding whether to ship.',
    }
  }

  const reportedStatuses = [
    implementedFindingCount ? `Qodo marks ${implementedFindingCount} finding${implementedFindingCount === 1 ? '' : 's'} implemented` : '',
    dismissedFindingCount ? `Qodo marks ${dismissedFindingCount} dismissed` : '',
    statuses.some((status) => status === 'detected_after_merge') ? 'Qodo marks findings detected after merge' : '',
    statuses.some((status) => status === 'outdated') ? 'Qodo marks findings outdated' : '',
  ].filter(Boolean).join(' and ')
  return {
    ...counts,
    kind: 'clear',
    title: 'Qodo reports no findings marked open',
    detail: `${reportedStatuses}. These Qodo statuses do not independently verify correctness.`,
    nextAction: 'Inspect the source findings and revision links before deciding whether to ship.',
  }
}

export function reviewRunForFinding(finding: QodoManagedFinding, review: QodoStructuredReview): QodoStructuredReview['reviewRuns'][number] | undefined {
  if (!finding.reviewRunId) return undefined
  return review.reviewRuns.find((run) => run.id === finding.reviewRunId)
}
