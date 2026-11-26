import { z } from 'zod'

const webURL = z.url({ protocol: /^https?$/ })
const fullSha = z.string().regex(/^[0-9a-f]{40}$/)
// not-applicable: the pull request is merged or closed, so there is nothing left to merge.
const outcome = z.enum(['pass', 'fail', 'incomplete', 'not-applicable'])
export type ReadinessOutcome = z.infer<typeof outcome>

// A readiness verdict always names the exact head commit its evidence describes.
export const ReadinessSchema = z.object({
  repository: z.string().min(1),
  number: z.number().int().positive(),
  headSha: fullSha,
  headShortSha: z.string().min(7),
  headUrl: webURL,
  verdict: outcome,
  reasons: z.array(z.object({ subject: z.string().min(1), outcome, message: z.string().min(1), sourceUrl: webURL.optional() })),
  findings: z.object({ high: z.number().int().nonnegative(), medium: z.number().int().nonnegative(), low: z.number().int().nonnegative() }).optional(),
})
export type Readiness = z.infer<typeof ReadinessSchema>

const verdictLabels: Record<ReadinessOutcome, string> = { pass: 'PASS', fail: 'FAIL', incomplete: 'INCOMPLETE', 'not-applicable': 'NOT APPLICABLE' }

export function readinessHeadline(readiness: Readiness): string {
  const label = verdictLabels[readiness.verdict]
  switch (readiness.verdict) {
    case 'pass': return `${label} · every required source covers ${readiness.headShortSha}`
    case 'fail': return `${label} · a required source reports a problem on ${readiness.headShortSha}`
    case 'not-applicable': return `${label} · ${readiness.headShortSha} is the head of a pull request that is not open`
    default: return `${label} · evidence for ${readiness.headShortSha} is missing or not yet current`
  }
}

// Reasons are ordered so a failure or a gap is read before what already passed.
const outcomeOrder: Record<ReadinessOutcome, number> = { 'not-applicable': 0, fail: 1, incomplete: 2, pass: 3 }
export function orderReadinessReasons(reasons: Readiness['reasons']): Readiness['reasons'] {
  return reasons.map((reason, index) => ({ reason, index }))
    .sort((a, b) => outcomeOrder[a.reason.outcome] - outcomeOrder[b.reason.outcome] || a.index - b.index)
    .map(({ reason }) => reason)
}
