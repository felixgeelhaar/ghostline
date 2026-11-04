import { z } from 'zod'

const fullSha = z.string().regex(/^[0-9a-f]{40}$/)
const QodoReviewSchema = z.object({
  id: z.number(),
  kind: z.enum(['summary', 'findings']),
  summary: z.string().optional(),
  findingTitles: z.array(z.string()),
  commitSha: fullSha.optional(),
  commitUrl: z.url({ protocol: /^https$/ }).optional(),
  createdAt: z.string(),
  url: z.url({ protocol: /^https$/ }),
})

export const QodoReviewSetSchema = z.discriminatedUnion('availability', [
  z.object({ availability: z.literal('available'), items: z.array(QodoReviewSchema) }),
  z.object({ availability: z.literal('unavailable'), reason: z.string().min(1), items: z.array(QodoReviewSchema).length(0) }),
])

export type QodoReviewSet = z.infer<typeof QodoReviewSetSchema>
export type QodoReview = QodoReviewSet['items'][number]

export type FindingOccurrence = {
  reviewId: number
  createdAt: string
  url: string
  commitSha?: string
  commitUrl?: string
}

export type RecurringFinding = {
  title: string
  occurrences: FindingOccurrence[]
}

export function findRecurringFindings(reviews: QodoReview[]): RecurringFinding[] {
  const grouped = new Map<string, RecurringFinding>()
  for (const review of reviews) {
    if (review.kind !== 'findings') continue
    const seenInReview = new Set<string>()
    for (const title of review.findingTitles) {
      const normalized = title.trim().replace(/\s+/g, ' ').toLowerCase()
      if (!normalized || seenInReview.has(normalized)) continue
      seenInReview.add(normalized)
      const finding = grouped.get(normalized) ?? { title: title.trim().replace(/\s+/g, ' '), occurrences: [] }
      finding.occurrences.push({
        reviewId: review.id,
        createdAt: review.createdAt,
        url: review.url,
        ...(review.commitSha ? { commitSha: review.commitSha } : {}),
        ...(review.commitUrl ? { commitUrl: review.commitUrl } : {}),
      })
      grouped.set(normalized, finding)
    }
  }
  return [...grouped.values()].filter((finding) => finding.occurrences.length > 1)
}

export function qodoReviewState(reviews: QodoReviewSet): 'recorded' | 'none' | 'unavailable' {
  if (reviews.availability === 'unavailable') return 'unavailable'
  return reviews.items.length > 0 ? 'recorded' : 'none'
}
