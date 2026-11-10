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
}).superRefine((pullRequest, context) => {
  if (!matchesGitHubPath(
    pullRequest.url,
    `${pullRequest.owner}/${pullRequest.repo}/pull/${pullRequest.number}`,
  )) {
    context.addIssue({ code: 'custom', path: ['url'], message: 'Pull request URL does not match its GitHub identity.' })
  }

  pullRequest.commits.forEach((commit, index) => {
    if (!matchesGitHubPath(
      commit.url,
      `${pullRequest.owner}/${pullRequest.repo}/commit/${commit.sha}`,
    )) {
      context.addIssue({
        code: 'custom',
        path: ['commits', index, 'url'],
        message: 'Commit URL does not match its GitHub identity.',
      })
    }
  })
})
export type PullRequest = z.infer<typeof PullRequestSchema>

function matchesGitHubPath(value: string, expectedPath: string): boolean {
  try {
    const url = new URL(value)
    if (url.protocol !== 'https:' || url.hostname !== 'github.com' || url.port || url.username || url.password || url.search || url.hash) return false
    const encodedPath = url.pathname.replace(/\/+$/, '')
    if (/%(?:2f|5c)/i.test(encodedPath)) return false
    const path = encodedPath.slice(1)
      .split('/')
      .map((segment) => decodeURIComponent(segment))
    if (path.some((segment) => segment.includes('/') || segment.includes('\\'))) return false
    const normalizedPath = path
      .map((segment) => segment.toLowerCase())
      .join('/')
    return normalizedPath === expectedPath.toLowerCase()
  } catch {
    return false
  }
}

export function validatePullRequestURL(value: string): string | undefined {
  try {
    const url = new URL(value.trim())
    if (url.protocol !== 'https:' || url.hostname !== 'github.com' || url.port || url.search || url.hash) return 'Enter a public GitHub pull request URL.'
    if (!/^\/[\w.-]+\/[\w.-]+\/pull\/[1-9]\d*\/?$/.test(url.pathname)) return 'Use a URL ending in /owner/repo/pull/number.'
  } catch { return 'Enter a valid GitHub pull request URL.' }
}
