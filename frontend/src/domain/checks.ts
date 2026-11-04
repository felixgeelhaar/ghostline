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

const severity: CheckTone[] = ['fail', 'running', 'neutral', 'pass']
const toneWord: Record<CheckTone, string> = { fail: 'failing', running: 'running', neutral: 'neutral', pass: 'passing' }

// Failing checks first so the reason a revision is not ready is never buried mid-list.
export function orderChecks(checks: Check[]): Check[] {
  return [...checks].sort((a, b) => severity.indexOf(checkOutcome(a).tone) - severity.indexOf(checkOutcome(b).tone))
}

export function summarizeChecks(checks: Check[]): { tone: CheckTone; label: string } {
  const counts = new Map<CheckTone, number>()
  for (const check of checks) counts.set(checkOutcome(check).tone, (counts.get(checkOutcome(check).tone) ?? 0) + 1)
  const passing = counts.get('pass') ?? 0
  if (passing === checks.length) return { tone: 'pass', label: `${passing} of ${checks.length} passing` }
  const present = severity.filter((tone) => counts.has(tone))
  const tone = counts.has('fail') ? 'fail' : counts.has('running') ? 'running' : passing > 0 ? 'pass' : 'neutral'
  return { tone, label: present.map((t) => `${counts.get(t)} ${toneWord[t]}`).join(' · ') }
}
