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
  title: string
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
        title: finding.title.trim().replace(/\s+/g, ' '),
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
  return [...grouped.values()].filter((finding) => {
    const markedRevisions = new Set(finding.occurrences.flatMap((occurrence) => occurrence.commitSha ? [occurrence.commitSha.toLowerCase()] : []))
    return markedRevisions.size > 1
  })
}

export function qodoReviewState(reviews: QodoReviewSet): 'recorded' | 'partial' | 'none' | 'unrecognized' | 'unavailable' {
  if (reviews.availability === 'unavailable') return 'unavailable'
  if (reviews.items.length > 0 && reviews.unrecognizedCommentCount > 0) return 'partial'
  if (reviews.items.length > 0) return 'recorded'
  return reviews.unrecognizedCommentCount > 0 ? 'unrecognized' : 'none'
}
