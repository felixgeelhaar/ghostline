import { describe, expect, it } from 'vitest'
import { checkOutcome, isHeadCommit, RevisionChecksSchema, type RevisionChecks } from './checks'

const head = 'a'.repeat(40)
const checks: RevisionChecks = { headSha: head, headShortSha: 'aaaaaaa', headUrl: `https://github.com/acme/app/commit/${head}`, availability: 'available', items: [] }

describe('checkOutcome', () => {
  it('uses the conclusion of a completed check', () => expect(checkOutcome({ name: 'go', kind: 'check_run', status: 'completed', conclusion: 'success' })).toEqual({ label: 'success', tone: 'pass' }))
  it('marks failing conclusions', () => {
    for (const conclusion of ['failure', 'error', 'timed_out', 'action_required', 'cancelled']) expect(checkOutcome({ name: 'x', kind: 'check_run', status: 'completed', conclusion }).tone).toBe('fail')
  })
  it('treats skipped and neutral results as neutral', () => expect(checkOutcome({ name: 'x', kind: 'check_run', status: 'completed', conclusion: 'skipped' }).tone).toBe('neutral'))
  it('shows unfinished checks as running', () => expect(checkOutcome({ name: 'x', kind: 'check_run', status: 'in_progress' })).toEqual({ label: 'in progress', tone: 'running' }))
})

describe('isHeadCommit', () => {
  it('matches only the commit the checks describe', () => {
    expect(isHeadCommit(checks, head)).toBe(true)
    expect(isHeadCommit(checks, 'b'.repeat(40))).toBe(false)
    expect(isHeadCommit({ ...checks, headSha: '' }, '')).toBe(false)
  })
})

describe('RevisionChecksSchema', () => {
  it('rejects checks that are not tied to a full commit SHA', () => expect(RevisionChecksSchema.safeParse({ ...checks, headSha: 'abc' }).success).toBe(false))
  it('rejects unavailable checks without an explanation', () => expect(RevisionChecksSchema.safeParse({ ...checks, availability: 'unavailable' }).success).toBe(false))
  it('rejects non-web check links', () => expect(RevisionChecksSchema.safeParse({ ...checks, items: [{ name: 'x', kind: 'commit_status', status: 'pending', url: 'javascript:alert(1)' }] }).success).toBe(false))
  it('accepts unavailable checks without a head revision when explained', () => expect(RevisionChecksSchema.safeParse({ headSha: '', headShortSha: '', headUrl: '', availability: 'unavailable', reason: 'No head commit.', items: [] }).success).toBe(true))
})
