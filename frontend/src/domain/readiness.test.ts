import { describe, expect, it } from 'vitest'
import { orderReadinessReasons, readinessHeadline, ReadinessSchema, type Readiness } from './readiness'

const sha = 'c'.repeat(40)
const base: Readiness = { repository: 'acme/app', number: 7, headSha: sha, headShortSha: 'ccccccc', headUrl: `https://github.com/acme/app/commit/${sha}`, verdict: 'pass', reasons: [] }

describe('readiness presentation rules', () => {
  it('names the head commit in every headline', () => {
    for (const verdict of ['pass', 'fail', 'incomplete', 'not-applicable'] as const) {
      expect(readinessHeadline({ ...base, verdict })).toContain('ccccccc')
    }
    expect(readinessHeadline({ ...base, verdict: 'incomplete' })).toMatch(/^INCOMPLETE/)
  })

  it('says readiness does not apply, rather than that evidence is missing, for a pull request that is not open', () => {
    const headline = readinessHeadline({ ...base, verdict: 'not-applicable' })
    expect(headline).toBe('NOT APPLICABLE · ccccccc is the head of a pull request that is not open')
    expect(headline).not.toMatch(/missing|not yet current/)
  })

  it('accepts a not-applicable verdict and reason from the API', () => {
    const parsed = ReadinessSchema.parse({ ...base, verdict: 'not-applicable', reasons: [{ subject: 'pull-request', outcome: 'not-applicable', message: 'The pull request is already merged. Merge readiness applies only to an open pull request.' }] })
    expect(parsed.verdict).toBe('not-applicable')
  })

  it('lists failures, then gaps, then passes, keeping source order within each', () => {
    const ordered = orderReadinessReasons([
      { subject: 'branch', outcome: 'pass', message: 'a' },
      { subject: 'qodo', outcome: 'incomplete', message: 'b' },
      { subject: 'check:Go', outcome: 'fail', message: 'c' },
      { subject: 'check:Web', outcome: 'incomplete', message: 'd' },
    ])
    expect(ordered.map((reason) => reason.message)).toEqual(['c', 'b', 'd', 'a'])
  })
})
