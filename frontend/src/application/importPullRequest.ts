import { PullRequestSchema, type PullRequest } from '../domain/pullRequest'

export async function importPullRequest(url: string): Promise<PullRequest> {
  const response = await fetch('/api/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }) })
  const payload: unknown = await response.json()
  if (!response.ok) throw new Error(typeof payload === 'object' && payload !== null && 'error' in payload ? String(payload.error) : 'Unable to import this pull request.')
  const result = PullRequestSchema.safeParse(payload)
  if (!result.success) {
    throw new Error('GitHub returned pull request data that did not match its repository identity. Please retry the import.')
  }
  return result.data
}
