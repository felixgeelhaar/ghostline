import { PullRequestSchema, ScanResponseSchema, type PullRequest } from '../domain/pullRequest'

export async function importPullRequest(url: string): Promise<PullRequest> {
  const response = await fetch('/api/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url }) })
  const payload: unknown = await response.json()
  if (!response.ok) throw new Error(typeof payload === 'object' && payload !== null && 'error' in payload ? String(payload.error) : 'Unable to import this pull request.')
  return PullRequestSchema.parse(payload)
}

export async function scanRepository(path: string) {
  const response = await fetch('/api/scan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) })
  const payload: unknown = await response.json()
  if (!response.ok) throw new Error(typeof payload === 'object' && payload !== null && 'error' in payload ? String(payload.error) : 'Unable to scan this repository.')
  return ScanResponseSchema.parse(payload)
}
