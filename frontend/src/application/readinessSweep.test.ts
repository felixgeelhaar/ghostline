import { describe, expect, it, vi } from 'vitest'
import { sweepReadiness } from './readinessSweep'
import { GhostlineApiError } from './apiError'
import type { OpenPullRequest, RowCheck } from '../domain/openPullRequests'
import type { Readiness } from '../domain/readiness'

const sha = (c: string) => c.repeat(40)
const row = (number: number): OpenPullRequest => ({ number, title: `Change ${number}`, draft: false, author: 'octo', headSha: sha(String(number)), headShortSha: sha(String(number)).slice(0, 7), url: `https://github.com/acme/app/pull/${number}` })
type FetchReadiness = (url: string, repository: string, number: number) => Promise<Readiness>
const verdict = (number: number): Readiness => ({ repository: 'acme/app', number, headSha: sha(String(number)), headShortSha: sha(String(number)).slice(0, 7), headUrl: `https://github.com/acme/app/commit/${sha(String(number))}`, verdict: 'incomplete', nextAction: 'update branch: 1 behind main', reasons: [] })

function run(rows: OpenPullRequest[], fetchReadiness: FetchReadiness, options: { isCurrent?: () => boolean; skip?: (row: OpenPullRequest) => boolean } = {}) {
  const updates: Array<[number, RowCheck]> = []
  const failures: unknown[] = []
  const done = sweepReadiness({
    repository: 'acme/app', rows, fetchReadiness,
    isCurrent: options.isCurrent ?? (() => true),
    skip: options.skip ?? (() => false),
    onUpdate: (number, check) => updates.push([number, check]),
    onFailure: (error) => failures.push(error),
  })
  return { done, updates, failures }
}

describe('readiness sweep', () => {
  it('checks each row in order, one at a time, for the row it was listed with', async () => {
    const fetchReadiness = vi.fn<FetchReadiness>().mockResolvedValueOnce(verdict(2)).mockResolvedValueOnce(verdict(1))
    const { done, updates } = run([row(2), row(1)], fetchReadiness)
    expect(fetchReadiness).toHaveBeenCalledTimes(1)
    await done
    expect(fetchReadiness.mock.calls).toEqual([['https://github.com/acme/app/pull/2', 'acme/app', 2], ['https://github.com/acme/app/pull/1', 'acme/app', 1]])
    expect(updates.map(([number, check]) => `${number}:${check.state}`)).toEqual(['2:checking', '2:checked', '1:checking', '1:checked'])
  })

  it('skips rows the caller already holds a verdict for', async () => {
    const fetchReadiness = vi.fn<FetchReadiness>().mockResolvedValue(verdict(1))
    await run([row(2), row(1)], fetchReadiness, { skip: (item) => item.number === 2 }).done
    expect(fetchReadiness).toHaveBeenCalledTimes(1)
  })

  it('records a failure and goes on, but stops when the rest would fail too', async () => {
    const fetchReadiness = vi.fn<FetchReadiness>()
      .mockRejectedValueOnce(new GhostlineApiError(409, 'This pull request changed while its readiness was being checked. Check it again.'))
      .mockRejectedValueOnce(new GhostlineApiError(429, 'Too many requests. Try again in 20 seconds.'))
    const { done, updates, failures } = run([row(3), row(2), row(1)], fetchReadiness)
    await done
    expect(fetchReadiness).toHaveBeenCalledTimes(2)
    expect(failures).toHaveLength(2)
    expect(updates.filter(([, check]) => check.state === 'error').map(([number]) => number)).toEqual([3, 2])
    expect(updates.some(([number]) => number === 1)).toBe(false)
  })

  it('applies nothing once the list it was started for is no longer current', async () => {
    let current = true
    const fetchReadiness = vi.fn<FetchReadiness>().mockImplementation(async () => { current = false; return verdict(2) })
    const { done, updates } = run([row(2), row(1)], fetchReadiness, { isCurrent: () => current })
    await done
    expect(fetchReadiness).toHaveBeenCalledTimes(1)
    expect(updates.map(([, check]) => check.state)).toEqual(['checking'])
  })
})
