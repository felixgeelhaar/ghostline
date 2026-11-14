import { describe, expect, it } from 'vitest'
import { checkOutcome, checkSummaryPreview, CheckSchema, headCommitLabel, isHeadCommit, orderChecks, RevisionChecksSchema, summarizeChecks, type Check, type RevisionChecks } from './checks'

const head = 'a'.repeat(40)
const checks: RevisionChecks = { revisionSha: head, revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/app/commit/${head}`, availability: 'available', items: [] }

describe('checkOutcome', () => {
  it('uses the conclusion of a completed check', () => expect(checkOutcome({ name: 'go', kind: 'check_run', status: 'completed', conclusion: 'success' })).toEqual({ label: 'success', tone: 'pass' }))
  it('marks failing conclusions', () => {
    for (const conclusion of ['failure', 'error', 'timed_out', 'action_required', 'cancelled', 'startup_failure', 'stale']) expect(checkOutcome({ name: 'x', kind: 'check_run', status: 'completed', conclusion }).tone).toBe('fail')
  })
  it('treats skipped and neutral results as neutral', () => expect(checkOutcome({ name: 'x', kind: 'check_run', status: 'completed', conclusion: 'skipped' }).tone).toBe('neutral'))
  it('shows unfinished checks as running', () => expect(checkOutcome({ name: 'x', kind: 'check_run', status: 'in_progress' })).toEqual({ label: 'in progress', tone: 'running' }))
})

describe('isHeadCommit', () => {
  it('matches only the commit the checks describe', () => {
    expect(isHeadCommit(checks, head)).toBe(true)
    expect(isHeadCommit(checks, 'b'.repeat(40))).toBe(false)
    expect(isHeadCommit({ ...checks, revisionSha: '' }, '')).toBe(false)
  })
})

describe('RevisionChecksSchema', () => {
  it('rejects checks that are not tied to a full commit SHA', () => expect(RevisionChecksSchema.safeParse({ ...checks, revisionSha: 'abc' }).success).toBe(false))
  it('rejects unavailable checks without an explanation', () => expect(RevisionChecksSchema.safeParse({ ...checks, availability: 'unavailable' }).success).toBe(false))
  it('rejects non-web check links', () => expect(RevisionChecksSchema.safeParse({ ...checks, items: [{ name: 'x', kind: 'commit_status', status: 'pending', url: 'javascript:alert(1)' }] }).success).toBe(false))
  it('accepts unavailable checks without a revision when explained', () => expect(RevisionChecksSchema.safeParse({ revisionSha: '', revisionShortSha: '', revisionUrl: '', availability: 'unavailable', reason: 'No revision.', items: [] }).success).toBe(true))
})

describe('CheckSchema output evidence', () => {
  it('preserves GitHub check-run titles and summaries', () => {
    const check = {
      name: 'nox', kind: 'check_run', status: 'completed', conclusion: 'success',
      outputTitle: 'No new alerts in code changed by this pull request',
      outputSummary: '[View all branch alerts](/acme/app/security/code-scanning?query=pr%3A7+tool%3Anox+is%3Aopen).',
    }
    expect(CheckSchema.parse(check)).toEqual(check)
  })

  it('shows Markdown link labels without rendering or exposing destinations as text', () => {
    expect(checkSummaryPreview('[View all branch alerts](/acme/app/security/code-scanning?query=nox).')).toBe('View all branch alerts.')
  })
})

const run = (name: string, status: string, conclusion?: string): Check => ({ name, kind: 'check_run', status, conclusion })

describe('summarizeChecks', () => {
  it('reports all passing', () => expect(summarizeChecks([run('a', 'completed', 'success'), run('b', 'completed', 'success')])).toEqual({ tone: 'pass', label: '2 of 2 passing' }))
  it('leads with failures, then running checks', () => expect(summarizeChecks([run('a', 'completed', 'failure'), run('b', 'queued'), run('c', 'in_progress'), run('d', 'completed', 'success')])).toEqual({ tone: 'fail', label: '1 failing · 2 running · 1 passing' }))
  it('is running while nothing has failed', () => expect(summarizeChecks([run('a', 'queued'), run('b', 'completed', 'skipped')])).toEqual({ tone: 'running', label: '1 running · 1 neutral' }))
  it('does not give a passing verdict when a passing check is mixed with a neutral check', () => expect(summarizeChecks([run('a', 'completed', 'success'), run('b', 'completed', 'skipped')])).toEqual({ tone: 'neutral', label: '1 neutral · 1 passing' }))
  it('does not call an empty check list passing', () => expect(summarizeChecks([])).toEqual({ tone: 'neutral', label: 'No checks reported' }))
})

describe('headCommitLabel', () => {
  it('distinguishes an identified head from a head with reported checks', () => {
    expect(headCommitLabel(checks, head)).toBe('HEAD')
    expect(headCommitLabel({ ...checks, items: [run('go', 'completed', 'success')] }, head)).toBe('HEAD · CHECKED')
    expect(headCommitLabel({ ...checks, availability: 'unavailable', reason: 'offline' }, head)).toBe('HEAD')
    expect(headCommitLabel(checks, 'b'.repeat(40))).toBeUndefined()
  })
})

describe('orderChecks', () => {
  it('puts failing checks first and passing checks last, keeping name order within a group', () => {
    const ordered = orderChecks([run('a', 'completed', 'success'), run('b', 'completed', 'skipped'), run('c', 'queued'), run('d', 'completed', 'failure'), run('e', 'completed', 'error')])
    expect(ordered.map((c) => c.name)).toEqual(['d', 'e', 'c', 'b', 'a'])
  })
})
