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

export function qodoReviewState(reviews: QodoReviewSet): 'recorded' | 'none' | 'unavailable' {
  if (reviews.availability === 'unavailable') return 'unavailable'
  return reviews.items.length > 0 ? 'recorded' : 'none'
}
