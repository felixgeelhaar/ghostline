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

export function reviewSessionFreshness(review: QodoStructuredReview, headSha: string): 'current' | 'stale' | 'unknown' {
  if (!fullSha.safeParse(headSha).success) return 'unknown'
  const reviewedSha = review.reviewSession?.commitSha
  if (!reviewedSha) return 'unknown'
  return reviewedSha.toLowerCase() === headSha.toLowerCase() ? 'current' : 'stale'
}

export function isReviewSessionCurrent(review: QodoStructuredReview, headSha: string): boolean {
  return reviewSessionFreshness(review, headSha) === 'current'
}

export function reviewRunForFinding(finding: QodoManagedFinding, review: QodoStructuredReview): QodoStructuredReview['reviewRuns'][number] | undefined {
  if (!finding.reviewRunId) return undefined
  return review.reviewRuns.find((run) => run.id === finding.reviewRunId)
}
