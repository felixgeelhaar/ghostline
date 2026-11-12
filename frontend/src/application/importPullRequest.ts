import { ApiResponseError, apiErrorMessage } from './apiError'
import { PullRequestSchema, type PullRequest } from '../domain/pullRequest'

export async function importPullRequest(url: string): Promise<PullRequest> {
  const response = await fetch('/api/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }) })
  const payload: unknown = await response.json()
  if (!response.ok) throw new ApiResponseError(apiErrorMessage(response, payload, 'Unable to import this pull request.'), response.status)
  const result = PullRequestSchema.safeParse(payload)
  if (!result.success) {
    throw new Error('GitHub returned pull request data that did not pass validation. Please retry the import.')
  }
  return result.data
}
