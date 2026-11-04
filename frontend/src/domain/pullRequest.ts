import { z } from 'zod'
import { RevisionChecksSchema } from './checks'
import { QodoReviewSetSchema } from './reviews'

export const PullRequestSchema = z.object({
  repository: z.string(), owner: z.string(), repo: z.string(), number: z.number(),
  title: z.string(), state: z.string(), url: z.string().url(),
  createdAt: z.string(), updatedAt: z.string(),
  commits: z.array(z.object({ sha: z.string(), shortSha: z.string(), message: z.string(), author: z.string(), committedAt: z.string(), url: z.string().url() })),
  checks: RevisionChecksSchema,
  qodoReviews: QodoReviewSetSchema,
})
export type PullRequest = z.infer<typeof PullRequestSchema>

export function validatePullRequestURL(value: string): string | undefined {
  try {
    const url = new URL(value.trim())
    if (url.protocol !== 'https:' || url.hostname !== 'github.com' || url.search || url.hash) return 'Enter a public GitHub pull request URL.'
    if (!/^\/[\w.-]+\/[\w.-]+\/pull\/[1-9]\d*\/?$/.test(url.pathname)) return 'Use a URL ending in /owner/repo/pull/number.'
  } catch { return 'Enter a valid GitHub pull request URL.' }
}
