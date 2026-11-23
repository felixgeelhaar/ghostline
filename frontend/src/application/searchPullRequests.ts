import { ghostlineApiError, readResponsePayload } from './apiError'
import { PullRequestSearchResultSchema, type PullRequestSearchResult } from '../domain/pullRequestSearch'

// Searches the signed-in user's public pull requests. The server runs the search with
// the user's GitHub token; the browser never sees it.
export async function searchPullRequests(query: string): Promise<PullRequestSearchResult> {
  const response = await fetch('/api/pr-search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query }),
  })
  const payload = await readResponsePayload(response)
  if (!response.ok) throw ghostlineApiError(response, payload, 'Could not search your pull requests. Paste a pull request URL instead.')
  const parsed = PullRequestSearchResultSchema.safeParse(payload)
  if (!parsed.success) throw new Error('GitHub search results did not pass validation. Paste a pull request URL instead.')
  return parsed.data
}
