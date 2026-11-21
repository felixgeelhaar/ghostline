import { describe, expect, it } from 'vitest'
import { orderReadinessReasons, readinessHeadline, type Readiness } from './readiness'

const sha = 'c'.repeat(40)
const base: Readiness = { repository: 'acme/app', number: 7, headSha: sha, headShortSha: 'ccccccc', headUrl: `https://github.com/acme/app/commit/${sha}`, verdict: 'pass', reasons: [] }

describe('readiness presentation rules', () => {
  it('names the head commit in every headline', () => {
    for (const verdict of ['pass', 'fail', 'incomplete'] as const) {
      expect(readinessHeadline({ ...base, verdict })).toContain('ccccccc')
    }
    expect(readinessHeadline({ ...base, verdict: 'incomplete' })).toMatch(/^INCOMPLETE/)
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
