import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiErrorMessage } from './apiError'
import { importPullRequest } from './importPullRequest'
import { fetchRevisionChecks } from './fetchRevisionChecks'

const rateLimited = () =>
  new Response(JSON.stringify({ error: 'Too many requests from this client. Try again in 42 seconds.' }), { status: 429, headers: { 'Retry-After': '42' } })

afterEach(() => vi.unstubAllGlobals())

describe('apiErrorMessage', () => {
  it('tells the user when they can retry after a 429 with Retry-After', () => {
    expect(apiErrorMessage(rateLimited(), { error: 'ignored' }, 'fallback')).toBe('Too many requests. Try again in 42 seconds.')
  })

  it('uses singular wording for one second', () => {
    const response = new Response('{}', { status: 429, headers: { 'Retry-After': '1' } })
    expect(apiErrorMessage(response, {}, 'fallback')).toBe('Too many requests. Try again in 1 second.')
  })

  it('keeps the API message for a 429 without a usable Retry-After (for example GitHub quota)', () => {
    const response = new Response('{}', { status: 429 })
    expect(apiErrorMessage(response, { error: 'GitHub’s public API rate limit has been reached.' }, 'fallback')).toBe('GitHub’s public API rate limit has been reached.')
    const bad = new Response('{}', { status: 429, headers: { 'Retry-After': 'soon' } })
    expect(apiErrorMessage(bad, { error: 'API says no' }, 'fallback')).toBe('API says no')
  })

  it('uses the API error for other statuses and the fallback when absent', () => {
    expect(apiErrorMessage(new Response('{}', { status: 400 }), { error: 'bad url' }, 'fallback')).toBe('bad url')
    expect(apiErrorMessage(new Response('{}', { status: 500 }), null, 'fallback')).toBe('fallback')
  })
})

describe('rate-limited requests', () => {
  it('importPullRequest surfaces the retry time', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(rateLimited()))
    await expect(importPullRequest('https://github.com/acme/app/pull/7')).rejects.toThrow('Try again in 42 seconds')
  })

  it('fetchRevisionChecks surfaces the retry time', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(rateLimited()))
    await expect(fetchRevisionChecks('https://github.com/acme/app/pull/7', 'b'.repeat(40))).rejects.toThrow('Try again in 42 seconds')
  })
})
