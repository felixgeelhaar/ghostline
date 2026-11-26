import { z } from 'zod'
import type { Readiness, ReadinessOutcome } from './readiness'

const fullSha = z.string().regex(/^[0-9a-f]{40}$/)

// One open pull request and the head GitHub reported for it when the list was read.
export const OpenPullRequestSchema = z.object({
  number: z.number().int().positive(),
  title: z.string(),
  draft: z.boolean(),
  author: z.string(),
  headSha: fullSha,
  headShortSha: z.string().min(7),
  url: z.url({ protocol: /^https$/, hostname: /^github\.com$/ }),
})
export type OpenPullRequest = z.infer<typeof OpenPullRequestSchema>

// complete is false when GitHub holds more open pull requests than one page.
export const OpenPullRequestsSchema = z.object({
  repository: z.string().min(3),
  complete: z.boolean(),
  limit: z.number().int().positive(),
  items: z.array(OpenPullRequestSchema),
})
export type OpenPullRequests = z.infer<typeof OpenPullRequestsSchema>

export type RowCheck =
  | { state: 'unchecked' }
  | { state: 'checking' }
  | { state: 'checked'; readiness: Readiness }
  // The pull request changed while its readiness was being read, so no verdict applies.
  | { state: 'changed' }
  | { state: 'error'; message: string }

export type RowVerdict = ReadinessOutcome | 'stale' | 'unchecked' | 'checking' | 'error'

// The head this row's evidence actually describes, when it differs from the listed one.
function movedHead(row: OpenPullRequest, check: RowCheck, openedHeadSha?: string): string | undefined {
  if (openedHeadSha && openedHeadSha !== row.headSha) return openedHeadSha
  if (check.state === 'checked' && check.readiness.headSha !== row.headSha) return check.readiness.headSha
  return undefined
}

// A verdict counts only for the head the row was listed with; otherwise the row is stale.
export function rowVerdict(row: OpenPullRequest, check: RowCheck, openedHeadSha?: string): RowVerdict {
  if (movedHead(row, check, openedHeadSha) || check.state === 'changed') return 'stale'
  return check.state === 'checked' ? check.readiness.verdict : check.state
}

export function rowNextAction(row: OpenPullRequest, check: RowCheck, openedHeadSha?: string): string {
  const moved = movedHead(row, check, openedHeadSha)
  if (moved) return `head moved from ${row.headShortSha} to ${moved.slice(0, 7)}: list again`
  switch (check.state) {
    case 'unchecked': return 'check readiness'
    case 'checking': return 'checking…'
    case 'error': return check.message
    case 'checked': return check.readiness.nextAction
    case 'changed': return 'changed while being checked: list again'
  }
}

// The head an import found for this row's pull request, or nothing when the import is of
// another pull request or named no head. Only such a head can show that the listed one moved.
export function importedHeadFor(row: OpenPullRequest, imported?: { url: string; checks: { revisionSha?: string } }): string | undefined {
  if (!imported || imported.url !== row.url || !imported.checks.revisionSha) return undefined
  return imported.checks.revisionSha
}
