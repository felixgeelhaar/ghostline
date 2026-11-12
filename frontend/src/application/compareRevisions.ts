import { ApiResponseError, apiErrorMessage } from './apiError'
import { RevisionComparisonSchema } from '../domain/comparison'
import { validatePullRequestURL } from '../domain/pullRequest'

export async function compareRevisions(url: string, baseSha: string, headSha: string) {
  if (validatePullRequestURL(url)) throw new Error('Choose a valid GitHub pull request before comparing revisions.')
  const response = await fetch('/api/compare', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, baseSha, headSha }),
  })
  const payload: unknown = await response.json()
  if (!response.ok) throw new ApiResponseError(apiErrorMessage(response, payload, 'Unable to compare these revisions.'), response.status)
  if (typeof payload === 'object' && payload !== null && 'baseSha' in payload && 'headSha' in payload &&
    (payload.baseSha !== baseSha || payload.headSha !== headSha)) {
    throw new Error('GitHub returned a comparison for different revisions.')
  }
  const comparison = RevisionComparisonSchema.parse(payload)
  const pr = new URL(url)
  const repositoryPath = `/${pr.pathname.split('/').filter(Boolean).slice(0, 2).join('/')}/`
  const comparisonPath = new URL(comparison.compareUrl).pathname.toLowerCase()
  if (!comparisonPath.startsWith(repositoryPath.toLowerCase())) throw new Error('GitHub returned a comparison from a different repository.')
  for (const file of comparison.files) {
    if (!new URL(file.url).pathname.toLowerCase().startsWith(repositoryPath.toLowerCase())) {
      throw new Error('GitHub returned a changed-file link from a different repository.')
    }
  }
  return comparison
}
