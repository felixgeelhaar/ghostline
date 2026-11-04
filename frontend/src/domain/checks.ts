import { z } from 'zod'

const webURL = z.url({ protocol: /^https?$/ })
const fullSha = z.string().regex(/^[0-9a-f]{40}$/)

export const CheckSchema = z.object({
  name: z.string(), kind: z.enum(['check_run', 'commit_status']), source: z.string().optional(),
  status: z.string(), conclusion: z.string().optional(), url: webURL.optional(),
})
export type Check = z.infer<typeof CheckSchema>

// Checks always describe one commit: the pull request head GitHub reported at import time.
export const RevisionChecksSchema = z.discriminatedUnion('availability', [
  z.object({ availability: z.literal('available'), headSha: fullSha, headShortSha: z.string(), headUrl: webURL, items: z.array(CheckSchema) }),
  z.object({ availability: z.literal('unavailable'), reason: z.string().min(1), headSha: z.union([fullSha, z.literal('')]), headShortSha: z.string(), headUrl: z.union([webURL, z.literal('')]), items: z.array(CheckSchema).length(0) }),
])
export type RevisionChecks = z.infer<typeof RevisionChecksSchema>

export type CheckTone = 'pass' | 'fail' | 'neutral' | 'running'
const failing = new Set(['failure', 'error', 'timed_out', 'action_required', 'cancelled', 'startup_failure'])
const passing = new Set(['success'])

export function checkOutcome(check: Check): { label: string; tone: CheckTone } {
  if (check.status !== 'completed') return { label: check.status.replaceAll('_', ' '), tone: 'running' }
  const conclusion = check.conclusion ?? 'unknown'
  const tone: CheckTone = passing.has(conclusion) ? 'pass' : failing.has(conclusion) ? 'fail' : 'neutral'
  return { label: conclusion.replaceAll('_', ' '), tone }
}

export function isHeadCommit(checks: RevisionChecks, sha: string): boolean {
  return checks.headSha !== '' && checks.headSha === sha
}
