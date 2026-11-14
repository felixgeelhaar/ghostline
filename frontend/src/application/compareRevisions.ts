import { ApiResponseError, apiErrorMessage } from './apiError'
import { parseRevisionComparison } from '../domain/comparison'
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
  return parseRevisionComparison(payload, { pullRequestUrl: url, baseSha, headSha })
}
