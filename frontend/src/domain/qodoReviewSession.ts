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
  codeSnippet: z.string().optional(),
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
  warnings: z.array(z.string().min(1)).optional(),
})

export type QodoStructuredReview = z.infer<typeof QodoStructuredReviewSchema>
export type QodoManagedFinding = z.infer<typeof QodoManagedFindingSchema>

function githubPullRequestRepository(pullRequestUrl: string): { owner: string; repo: string } | undefined {
  try {
    const pullRequest = new URL(pullRequestUrl)
    if (pullRequest.protocol !== 'https:' || pullRequest.hostname !== 'github.com' || pullRequest.port || pullRequest.username || pullRequest.password || pullRequest.search || pullRequest.hash) return undefined
    const encodedSegments = pullRequest.pathname.replace(/\/+$/, '').slice(1).split('/')
    if (encodedSegments.length !== 4 || /%(?:2f|5c)/i.test(pullRequest.pathname)) return undefined
    const [encodedOwner, encodedRepo, pullSegment, number] = encodedSegments
    const owner = decodeURIComponent(encodedOwner ?? '')
    const repo = decodeURIComponent(encodedRepo ?? '')
    if (!/^[\w.-]+$/.test(owner) || !/^[\w.-]+$/.test(repo) || pullSegment?.toLowerCase() !== 'pull' || !/^[1-9]\d*$/.test(number ?? '')) return undefined
    return { owner, repo }
  } catch {
    return undefined
  }
}

export function qodoRevisionURL(pullRequestUrl: string, sha?: string): string | undefined {
  if (!sha || !fullSha.safeParse(sha).success) return undefined
  const repository = githubPullRequestRepository(pullRequestUrl)
  if (!repository) return undefined
  return `https://github.com/${encodeURIComponent(repository.owner)}/${encodeURIComponent(repository.repo)}/commit/${sha}`
}

export function qodoCitationURL(pullRequestUrl: string, source?: string): string | undefined {
  if (!source) return undefined
  const repository = githubPullRequestRepository(pullRequestUrl)
  if (!repository) return undefined
  try {
    const candidate = new URL(source)
    if (candidate.protocol !== 'https:' || candidate.hostname !== 'github.com' || candidate.port || candidate.username || candidate.password || candidate.search) return undefined
    const segments = candidate.pathname.replace(/\/+$/, '').slice(1).split('/').map((segment) => decodeURIComponent(segment))
    if (segments.length < 3 || /%(?:2f|5c)/i.test(candidate.pathname)) return undefined
    if (segments[0]?.toLowerCase() !== repository.owner.toLowerCase() || segments[1]?.toLowerCase() !== repository.repo.toLowerCase()) return undefined
    return candidate.href
  } catch {
    return undefined
  }
}

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
  currentFindingCount: number
  staleFindingCount: number
  unknownFindingRevisionCount: number
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

export function findingLifecycleStatus(finding: QodoManagedFinding): string {
  const attributionStatus = (finding.attributionStatus ?? '').trim().toLowerCase()
  const legacyStatus = (finding.status ?? '').trim().toLowerCase()
  const lifecycleStatuses = new Set(['open', 'implemented', 'dismissed', ...openAttributionStatuses, ...closedAttributionStatuses])
  if (lifecycleStatuses.has(attributionStatus)) return attributionStatus
  if (lifecycleStatuses.has(legacyStatus)) return legacyStatus
  return attributionStatus || legacyStatus
}

function staleFindingDetail(staleFindingCount: number, review: QodoStructuredReview, headSha: string): string {
  if (staleFindingCount === 0) return ''
  const staleFinding = review.findings.find((finding) => findingRevisionFreshness(finding, headSha) === 'stale')!
  return ` For example, ${staleFinding.id} references ${staleFinding.gitSha!.slice(0, 7)}, while the imported PR head is ${headSha.slice(0, 7)}.`
}

function currentFindingDetail(currentFindingCount: number): string {
  if (currentFindingCount === 0) return ''
  return ` ${currentFindingCount} finding${currentFindingCount === 1 ? ' has' : 's have'} a revision marker matching the imported PR head.`
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
  const statuses = review.findings.map(findingLifecycleStatus)
  const openStatuses = new Set(['open', ...openAttributionStatuses])
  const closedStatuses = new Set(['implemented', 'dismissed', ...closedAttributionStatuses])
  const knownStatuses = new Set([...openStatuses, ...closedStatuses])
  const openFindingCount = statuses.filter((status) => openStatuses.has(status)).length
  const unspecifiedFindingCount = statuses.filter((status) => status === '').length
  const implementedFindingCount = statuses.filter((status) => ['implemented', 'full_implementation'].includes(status)).length
  const dismissedFindingCount = statuses.filter((status) => status === 'dismissed').length
  const unrecognizedFindingCount = statuses.filter((status) => status !== '' && !knownStatuses.has(status)).length
  const unclassifiedFindingCount = unspecifiedFindingCount + unrecognizedFindingCount
  const staleFindingCount = review.findings.filter((finding) => findingRevisionFreshness(finding, headSha) === 'stale').length
  const currentFindingCount = review.findings.filter((finding) => findingRevisionFreshness(finding, headSha) === 'current').length
  const unknownFindingRevisionCount = review.findings.filter((finding) => !finding.gitSha).length
  const counts = {
    findingCount: review.findings.length,
    openFindingCount,
    unspecifiedFindingCount,
    unrecognizedFindingCount,
    currentFindingCount,
    implementedFindingCount,
    dismissedFindingCount,
    staleFindingCount,
    unknownFindingRevisionCount,
  }

  if (review.warnings?.length) {
    return {
      kind: 'incomplete',
      title: 'Qodo response may be incomplete',
      detail: review.warnings.join(' '),
      nextAction: 'Refresh the Qodo review before relying on this result.',
      ...counts,
    }
  }

  if (!review.reviewSession) {
    return {
      ...counts,
      kind: 'unknown',
      title: 'Review revision unconfirmed',
      detail: `Qodo returned no review session metadata. ${review.findings.length ? 'Findings remain available below, but their session freshness cannot be confirmed.' : 'No session freshness or finding status can be confirmed.'}${staleFindingDetail(staleFindingCount, review, headSha)}${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'inspect the original Qodo source and confirm which revision it reviewed.'),
    }
  }

  const freshness = reviewSessionFreshness(review, headSha)
  const sessionStatus = review.reviewSession.status.trim().toLowerCase()
  const sessionStillRunning = sessionStatus === 'started' || sessionStatus === 'running'
  if (freshness === 'stale') {
    const reviewedShortSha = review.reviewSession.commitSha?.slice(0, 7) ?? 'unknown'
    const headShortSha = fullSha.safeParse(headSha).success ? headSha.slice(0, 7) : 'unknown'
    return {
      ...counts,
      kind: 'stale',
      title: 'Review needs a refresh',
      detail: `Qodo’s review session inspected ${reviewedShortSha}, while the imported PR head is ${headShortSha}. Session-level freshness is stale; inspect each finding’s own revision marker to determine whether it matches the current head.${currentFindingDetail(currentFindingCount)}${staleFindingDetail(staleFindingCount, review, headSha)}${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}${sessionStillRunning ? ' The active Qodo review is still running; wait for it to finish before relying on its findings or starting another review.' : ''}`,
      nextAction: sessionStillRunning
        ? 'Wait for the current Qodo review to finish, then load a fresh review for the imported PR head before relying on its findings.'
        : currentFindingCount > 0
          ? `Inspect the ${currentFindingCount} finding${currentFindingCount === 1 ? '' : 's'} whose revision matches the imported head and follow the Qodo source links. Load a fresh Qodo review before relying on session completion.`
          : freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'load a Qodo review for the current PR head before relying on session completion.'),
    }
  }
  if (freshness === 'unknown') {
    if (sessionStatus === 'started' || sessionStatus === 'running') {
      return {
        ...counts,
        kind: 'in-progress',
        title: 'Qodo review is still running',
        detail: `Qodo reports session status “${review.reviewSession.status}” but did not provide a revision to compare with the imported PR head. Current findings are preliminary.${staleFindingDetail(staleFindingCount, review, headSha)}${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
        nextAction: 'Wait for Qodo to finish, then confirm its revision and inspect the findings before acting.',
      }
    }
    return {
      ...counts,
      kind: 'unknown',
      title: 'Review revision unconfirmed',
      detail: `Ghostline cannot compare Qodo’s reviewed revision with a known pull-request head. Findings remain source data, but their freshness is unknown.${staleFindingDetail(staleFindingCount, review, headSha)}${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)}`,
      nextAction: freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'inspect the original Qodo source and confirm which revision it reviewed.'),
    }
  }

  if (staleFindingCount > 0) {
    return {
      ...counts,
      kind: 'attention',
      title: 'Finding revision needs review',
      detail: `${staleFindingCount} Qodo finding${staleFindingCount === 1 ? ' references' : 's reference'} a revision different from the imported PR head.${staleFindingDetail(staleFindingCount, review, headSha)}${freshnessFindingDetail(openFindingCount, unclassifiedFindingCount)} Its Qodo status may reflect later attribution; inspect the linked review run before relying on that status for the current head.${sessionStillRunning ? ' The Qodo review session is still running, so its findings may change.' : ''}`,
      nextAction: sessionStillRunning
        ? 'Wait for the Qodo session to complete, then confirm the finding status against the current PR head using its source and linked run evidence.'
        : freshnessNextAction(openFindingCount, unclassifiedFindingCount, 'confirm its Qodo status against the current PR head using the finding source and linked run evidence.'),
    }
  }

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
    const revisionUncertainty = unknownFindingRevisionCount > 0
      ? ` ${unknownFindingRevisionCount} finding${unknownFindingRevisionCount === 1 ? ' has' : 's have'} no finding-specific revision, so ${unknownFindingRevisionCount === 1 ? 'its Qodo status cannot' : 'their Qodo statuses cannot'} be tied to the imported PR head.`
      : ''
    return {
      ...counts,
      kind: 'attention',
      title: `${openFindingCount} finding${openFindingCount === 1 ? '' : 's'} reported open`,
      detail: `Qodo reports ${openFindingCount} finding${openFindingCount === 1 ? '' : 's'} as open.${uncertainty}${revisionUncertainty} Qodo’s implemented or dismissed statuses do not independently verify correctness.`,
      nextAction: unknownFindingRevisionCount > 0
        ? 'Inspect the open findings below, then confirm missing finding-specific revisions against their Qodo sources and linked runs before relying on reported status.'
        : 'Inspect the open findings below and follow their source and revision links.',
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

  if (unknownFindingRevisionCount > 0) {
    const revisionDetail = `${unknownFindingRevisionCount} finding${unknownFindingRevisionCount === 1 ? ' has' : 's have'} no finding-specific revision. A linked run revision identifies that run, not the finding’s source revision.`
    return {
      ...counts,
      kind: 'attention',
      title: 'Finding revision unconfirmed',
      detail: revisionDetail,
      nextAction: 'Inspect the source finding and linked review run before relying on its status for the current PR head.',
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
