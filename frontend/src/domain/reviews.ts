import { z } from 'zod'

const fullSha = z.string().regex(/^[0-9a-f]{40}$/)
const QodoFindingSchema = z.object({
  id: z.string().min(1),
  title: z.string().min(1),
  categories: z.array(z.string()).default([]),
  lifecycle: z.string().optional(),
  severity: z.string().optional(),
  actionLevel: z.string().optional(),
  description: z.string().optional(),
  sourceUrl: z.url({ protocol: /^https$/ }),
})
const QodoReviewSchema = z.object({
  id: z.number().int().positive(),
  provider: z.literal('Qodo'),
  sourceType: z.literal('github_issue_comment'),
  sourceAuthor: z.string().min(1),
  kind: z.enum(['summary', 'findings', 'no_open_findings']),
  summary: z.string().optional(),
  findings: z.array(QodoFindingSchema),
  commitSha: fullSha.optional(),
  commitUrl: z.url({ protocol: /^https$/ }).optional(),
  createdAt: z.string(),
  url: z.url({ protocol: /^https$/ }),
})

export const QodoReviewSetSchema = z.discriminatedUnion('availability', [
  z.object({ availability: z.literal('available'), unrecognizedCommentCount: z.number().int().nonnegative(), items: z.array(QodoReviewSchema) }),
  z.object({ availability: z.literal('unavailable'), reason: z.string().min(1), unrecognizedCommentCount: z.number().int().nonnegative(), items: z.array(QodoReviewSchema).length(0) }),
])

export type QodoReviewSet = z.infer<typeof QodoReviewSetSchema>
export type QodoReview = QodoReviewSet['items'][number]
export type QodoFinding = QodoReview['findings'][number]
export type RevisionRef = { sha: string; shortSha: string; message: string; author: string; committedAt: string; url: string }

export type QodoHeadCoverage =
  | { state: 'current' | 'stale' | 'outside_history'; detail: string; revisionSha: string; revisionUrl?: string; sourceUrl: string }
  | { state: 'unknown' | 'missing' | 'unavailable'; detail: string }

// This describes whether published comments contain an explicit marker for
// the supplied PR head. It does not establish the latest complete review run.
export function qodoHeadCoverage(reviews: QodoReviewSet, headSha?: string, prCommits: Pick<RevisionRef, 'sha'>[] = []): QodoHeadCoverage {
  if (reviews.availability === 'unavailable') return { state: 'unavailable', detail: reviews.reason }
  if (!headSha) return { state: 'unknown', detail: 'GitHub did not identify the current PR head revision, so Qodo comment coverage cannot be compared.' }
  if (reviews.items.length === 0) {
    return reviews.unrecognizedCommentCount > 0
      ? { state: 'unknown', detail: 'GitHub returned Qodo comments, but Ghostline could not recognize their review format or revision.' }
      : { state: 'missing', detail: 'No recognized Qodo review comments were found for this pull request.' }
  }

  const newestFirst = [...reviews.items].sort((left, right) => Date.parse(right.createdAt) - Date.parse(left.createdAt) || right.id - left.id)
  const current = newestFirst.find((review) => review.commitSha?.toLowerCase() === headSha.toLowerCase())
  if (current) {
    return {
      state: 'current',
      detail: 'A published Qodo comment explicitly marks the current pull request head revision.',
      revisionSha: current.commitSha!,
      ...(current.commitUrl ? { revisionUrl: current.commitUrl } : {}),
      sourceUrl: current.url,
    }
  }

  if (reviews.unrecognizedCommentCount > 0 || newestFirst.some((review) => !review.commitSha)) {
    return { state: 'unknown', detail: 'Qodo comments are present, but revision markers or recognized comment coverage are incomplete.' }
  }

  const newestMarked = newestFirst.find((review) => review.commitSha)
  if (newestMarked?.commitSha) {
    const markerIsInPullRequest = prCommits.some((commit) => commit.sha.toLowerCase() === newestMarked.commitSha?.toLowerCase())
    return {
      state: markerIsInPullRequest ? 'stale' : 'outside_history',
      detail: markerIsInPullRequest
        ? 'Recognized Qodo comments mark earlier revisions in the imported pull request history only; none marks the current pull request head.'
        : 'A published Qodo comment marks a revision outside the imported pull request history; Ghostline cannot determine how it relates to this change.',
      revisionSha: newestMarked.commitSha,
      ...(newestMarked.commitUrl ? { revisionUrl: newestMarked.commitUrl } : {}),
      sourceUrl: newestMarked.url,
    }
  }

  return { state: 'unknown', detail: 'Qodo comments are present, but none identifies the revision it reviewed.' }
}

export type LaterRevisionToInspect = RevisionRef & { relationBasis: 'github_pr_commit_order' }

export function laterRevisionsToInspect(review: Pick<QodoReview, 'commitSha'>, commits: RevisionRef[]): LaterRevisionToInspect[] {
  if (!review.commitSha) return []
  const index = commits.findIndex((commit) => commit.sha.toLowerCase() === review.commitSha?.toLowerCase())
  if (index < 0) return []
  return commits.slice(index + 1).map((commit) => ({ ...commit, relationBasis: 'github_pr_commit_order' }))
}

export type FindingOccurrence = {
  reviewId: number
  findingId: string
  createdAt: string
  url: string
  sourceUrl: string
  categories: string[]
  description?: string
  severity?: string
  lifecycle?: string
  actionLevel?: string
  commitSha?: string
  commitUrl?: string
}

export type RecurringFinding = {
  id: string
  title: string
  occurrences: FindingOccurrence[]
}

export function noOpenFindingsNote(review: Pick<QodoReview, 'commitSha'>): string {
  if (!review.commitSha) return 'This comment has no revision marker. Treat it as context only; it does not establish the state of the current revision.'
  return 'This records the review outcome for the linked revision; it is not an independent correctness guarantee.'
}

export type RevisionReviewSelection = {
  matched: QodoReview[]
  unmarked: QodoReview[]
}

export function reviewsForRevision(reviews: QodoReview[], sha?: string): RevisionReviewSelection {
  if (!sha) return {
    matched: reviews.filter((review) => review.commitSha),
    unmarked: reviews.filter((review) => !review.commitSha),
  }
  return {
    matched: reviews.filter((review) => review.commitSha?.toLowerCase() === sha.toLowerCase()),
    unmarked: reviews.filter((review) => !review.commitSha),
  }
}

export function reviewCountSummary(reviews: QodoReview[], sha?: string): string {
  if (!sha) return `${reviews.length} COMMENTS`
  const selection = reviewsForRevision(reviews, sha)
  return `${selection.matched.length} MATCHED · ${selection.unmarked.length} CONTEXT`
}

export function findRecurringFindings(reviews: QodoReview[]): RecurringFinding[] {
  const grouped = new Map<string, RecurringFinding>()
  for (const review of reviews) {
    if (review.kind !== 'findings') continue
    const seenInReview = new Set<string>()
    for (const finding of review.findings) {
      const title = finding.title
      const normalized = title.trim().replace(/\s+/g, ' ').toLowerCase()
      if (!normalized || seenInReview.has(normalized)) continue
      seenInReview.add(normalized)
      const groupedFinding = grouped.get(normalized) ?? { id: normalized, title: title.trim().replace(/\s+/g, ' '), occurrences: [] }
      groupedFinding.occurrences.push({
        reviewId: review.id,
        findingId: finding.id,
        createdAt: review.createdAt,
        url: review.url,
        sourceUrl: finding.sourceUrl,
        categories: finding.categories,
        ...(finding.description ? { description: finding.description } : {}),
        ...(finding.severity ? { severity: finding.severity } : {}),
        ...(finding.lifecycle ? { lifecycle: finding.lifecycle } : {}),
        ...(finding.actionLevel ? { actionLevel: finding.actionLevel } : {}),
        ...(review.commitSha ? { commitSha: review.commitSha } : {}),
        ...(review.commitUrl ? { commitUrl: review.commitUrl } : {}),
      })
      grouped.set(normalized, groupedFinding)
    }
  }
  return [...grouped.values()].filter((finding) => finding.occurrences.length > 1)
}

export function qodoReviewState(reviews: QodoReviewSet): 'recorded' | 'partial' | 'none' | 'unrecognized' | 'unavailable' {
  if (reviews.availability === 'unavailable') return 'unavailable'
  if (reviews.items.length > 0 && reviews.unrecognizedCommentCount > 0) return 'partial'
  if (reviews.items.length > 0) return 'recorded'
  return reviews.unrecognizedCommentCount > 0 ? 'unrecognized' : 'none'
}
