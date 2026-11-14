import { ApiResponseError, apiErrorMessage } from './apiError'
import { RevisionChecksSchema } from '../domain/checks'

export async function fetchRevisionChecks(url: string, sha: string) {
  const response = await fetch('/api/checks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, sha }),
  })
  const payload: unknown = await response.json()
  if (!response.ok) throw new ApiResponseError(apiErrorMessage(response, payload, 'Unable to load checks for this revision.'), response.status)
  const checks = RevisionChecksSchema.parse(payload)
  if (checks.revisionSha !== sha) throw new Error('GitHub returned checks for a different revision.')
  return checks
}
