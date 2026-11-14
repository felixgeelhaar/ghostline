import { z } from 'zod'

const webURL = z.url({ protocol: /^https?$/ })
const fullSha = z.string().regex(/^[0-9a-f]{40}$/)

export const CheckSchema = z.object({
  name: z.string(), kind: z.enum(['check_run', 'commit_status']), source: z.string().optional(),
  status: z.string(), conclusion: z.string().optional(), url: webURL.optional(),
  outputTitle: z.string().optional(), outputSummary: z.string().optional(), outputSummaryTruncated: z.boolean().optional(),
})
export type Check = z.infer<typeof CheckSchema>

// Checks always describe one exact commit reported by GitHub.
export const RevisionChecksSchema = z.discriminatedUnion('availability', [
  z.object({ availability: z.literal('available'), revisionSha: fullSha, revisionShortSha: z.string(), revisionUrl: webURL, items: z.array(CheckSchema) }),
  z.object({ availability: z.literal('unavailable'), reason: z.string().min(1), revisionSha: z.union([fullSha, z.literal('')]), revisionShortSha: z.string(), revisionUrl: z.union([webURL, z.literal('')]), items: z.array(CheckSchema).length(0) }),
])
export type RevisionChecks = z.infer<typeof RevisionChecksSchema>

export type CheckTone = 'pass' | 'fail' | 'neutral' | 'running'
const failing = new Set(['failure', 'error', 'timed_out', 'action_required', 'cancelled', 'startup_failure', 'stale'])
const passing = new Set(['success'])

export function checkOutcome(check: Check): { label: string; tone: CheckTone } {
  if (check.status !== 'completed') return { label: check.status.replaceAll('_', ' '), tone: 'running' }
  const conclusion = check.conclusion ?? 'unknown'
  const tone: CheckTone = passing.has(conclusion) ? 'pass' : failing.has(conclusion) ? 'fail' : 'neutral'
  return { label: conclusion.replaceAll('_', ' '), tone }
}

const checkSummaryPreviewLimit = 280

// GitHub check summaries are Markdown. Show a bounded text preview while the
// native check-run link remains the source for the complete report.
export function checkSummaryPreview(summary: string): { text: string; truncated: boolean } {
  const text = summary
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s*>\s?/gm, '')
    .replace(/^\s*[-+*]\s+/gm, '')
    .replace(/^\s*\d+[.)]\s+/gm, '')
    .replace(/^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$/gm, '')
    .replace(/^\s*\|/gm, '')
    .replace(/\|\s*$/gm, '')
    .replace(/\s*\|\s*/g, ' · ')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/[*_~]{1,3}/g, '')
    .replace(/[ \t]+/g, ' ')
    .replace(/\s*\n+\s*/g, ' · ')
    .replace(/(?:\s*·\s*){2,}/g, ' · ')
    .trim()

  const characters = Array.from(text)
  if (characters.length <= checkSummaryPreviewLimit) return { text, truncated: false }
  return { text: `${characters.slice(0, checkSummaryPreviewLimit - 1).join('').trimEnd()}…`, truncated: true }
}

export function isHeadCommit(checks: RevisionChecks, sha: string): boolean {
  return checks.revisionSha !== '' && checks.revisionSha === sha
}

export function headCommitLabel(checks: RevisionChecks, sha: string): 'HEAD' | 'HEAD · CHECKED' | undefined {
  if (!isHeadCommit(checks, sha)) return undefined
  return checks.availability === 'available' && checks.items.length > 0 ? 'HEAD · CHECKED' : 'HEAD'
}

const severity: CheckTone[] = ['fail', 'running', 'neutral', 'pass']
const toneWord: Record<CheckTone, string> = { fail: 'failing', running: 'running', neutral: 'neutral', pass: 'passing' }

// Failing checks first so the reason a revision is not ready is never buried mid-list.
export function orderChecks(checks: Check[]): Check[] {
  return [...checks].sort((a, b) => severity.indexOf(checkOutcome(a).tone) - severity.indexOf(checkOutcome(b).tone))
}

export function summarizeChecks(checks: Check[]): { tone: CheckTone; label: string } {
  if (checks.length === 0) return { tone: 'neutral', label: 'No checks reported' }
  const counts = new Map<CheckTone, number>()
  for (const check of checks) counts.set(checkOutcome(check).tone, (counts.get(checkOutcome(check).tone) ?? 0) + 1)
  const passing = counts.get('pass') ?? 0
  if (passing === checks.length) return { tone: 'pass', label: `${passing} of ${checks.length} passing` }
  const present = severity.filter((tone) => counts.has(tone))
  const tone = counts.has('fail') ? 'fail' : counts.has('running') ? 'running' : counts.has('neutral') ? 'neutral' : 'pass'
  return { tone, label: present.map((t) => `${counts.get(t)} ${toneWord[t]}`).join(' · ') }
}
