import { ghostlineApiError, readResponsePayload } from './apiError'
import { OpenPullRequestsSchema } from '../domain/openPullRequests'

// Lists one page of a public repository's open pull requests with each one's head.
export async function listOpenPullRequests(repository: string) {
  const response = await fetch('/api/open-pulls', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repository }),
  })
  const payload = await readResponsePayload(response)
  if (!response.ok) throw ghostlineApiError(response, payload, 'Unable to list open pull requests for this repository.')
  const parsed = OpenPullRequestsSchema.safeParse(payload)
  if (!parsed.success) throw new Error('Ghostline returned open pull requests without a verifiable head commit.')
  return parsed.data
}
