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
  status: z.literal('ahead'),
  aheadBy: z.number().int().nonnegative(),
  behindBy: z.number().int().nonnegative(),
  totalCommits: z.number().int().nonnegative(),
  filesTruncated: z.boolean().default(false),
  files: z.array(z.object({
    path: z.string().min(1),
    status: z.enum(['added', 'modified', 'removed', 'renamed']),
    additions: z.number().int().nonnegative(),
    deletions: z.number().int().nonnegative(),
    changes: z.number().int().nonnegative(),
    url: githubUrl,
  })),
}).superRefine((comparison, context) => {
  if (comparison.behindBy !== 0) {
    context.addIssue({ code: 'custom', path: ['behindBy'], message: 'Candidate revision must descend from the reviewed revision.' })
  }
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
  const repositorySegments = pullRequest.pathname.split('/').filter(Boolean)
  const owner = repositorySegments[0]
  const repo = repositorySegments[1]
  if (!owner || !repo || repositorySegments[2] !== 'pull') throw new Error('Expected a GitHub pull request URL.')
  const repositoryPath = `/${owner}/${repo}`
  const comparisonPath = new URL(comparison.compareUrl).pathname.toLowerCase()
  if (!comparisonPath.startsWith(`${repositoryPath.toLowerCase()}/compare/`)) throw new Error('GitHub returned a comparison from a different repository.')
  for (const file of comparison.files) {
    const revision = file.status === 'removed' ? comparison.baseSha : comparison.headSha
    let actualPath: string
    try { actualPath = decodeURIComponent(new URL(file.url).pathname) } catch { throw new Error('GitHub returned an invalid changed-file source link.') }
    const actualSegments = actualPath.split('/').filter(Boolean)
    const expectedFileSegments = file.path.split('/')
    if (actualSegments.length < 5 || actualSegments[0]?.toLowerCase() !== owner?.toLowerCase() || actualSegments[1]?.toLowerCase() !== repo?.toLowerCase()) {
      throw new Error('GitHub returned a changed-file link from a different repository.')
    }
    if (actualSegments[2]?.toLowerCase() !== 'blob' || actualSegments[3]?.toLowerCase() !== revision ||
      actualSegments.slice(4).length !== expectedFileSegments.length || actualSegments.slice(4).some((segment, index) => segment !== expectedFileSegments[index])) {
      throw new Error('GitHub returned a changed-file link for a different revision or path.')
    }
  }
  return comparison
}
