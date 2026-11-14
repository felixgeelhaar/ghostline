import { z } from 'zod'

const fullSha = z.string().regex(/^[0-9a-f]{40}$/)
const githubUrl = z.url({ protocol: /^https$/ }).refine((value) => {
  const url = new URL(value)
  return url.hostname === 'github.com' && !url.port && !url.username && !url.password
}, 'Expected a GitHub source URL')

export const RevisionComparisonSchema = z.object({
  baseSha: fullSha,
  headSha: fullSha,
  compareUrl: githubUrl,
  status: z.enum(['ahead', 'behind', 'identical', 'diverged']),
  aheadBy: z.number().int().nonnegative(),
  behindBy: z.number().int().nonnegative(),
  totalCommits: z.number().int().nonnegative(),
  filesTruncated: z.boolean().default(false),
  files: z.array(z.object({
    path: z.string().min(1),
    status: z.string().min(1),
    additions: z.number().int().nonnegative(),
    deletions: z.number().int().nonnegative(),
    changes: z.number().int().nonnegative(),
    url: githubUrl,
  })),
}).superRefine((comparison, context) => {
  const url = new URL(comparison.compareUrl)
  const segments = url.pathname.split('/').filter(Boolean)
  const expected = `${comparison.baseSha}...${comparison.headSha}`
  if (segments.length < 4 || segments.at(-2) !== 'compare' || segments.at(-1)?.toLowerCase() !== expected) {
    context.addIssue({ code: 'custom', path: ['compareUrl'], message: 'GitHub comparison URL does not match the requested revisions.' })
  }
})

export type RevisionComparison = z.infer<typeof RevisionComparisonSchema>

export function parseRevisionComparison(
  payload: unknown,
  requested: { pullRequestUrl: string; baseSha: string; headSha: string },
): RevisionComparison {
  const comparison = RevisionComparisonSchema.parse(payload)
  if (comparison.baseSha.toLowerCase() !== requested.baseSha.toLowerCase() || comparison.headSha.toLowerCase() !== requested.headSha.toLowerCase()) {
    throw new Error('GitHub returned a comparison for different revisions.')
  }

  const pullRequest = new URL(requested.pullRequestUrl)
  const repositoryPath = `/${pullRequest.pathname.split('/').filter(Boolean).slice(0, 2).join('/')}/`
  const comparisonPath = new URL(comparison.compareUrl).pathname.toLowerCase()
  if (!comparisonPath.startsWith(repositoryPath.toLowerCase())) throw new Error('GitHub returned a comparison from a different repository.')
  for (const file of comparison.files) {
    if (!new URL(file.url).pathname.toLowerCase().startsWith(repositoryPath.toLowerCase())) {
      throw new Error('GitHub returned a changed-file link from a different repository.')
    }
  }
  return comparison
}
