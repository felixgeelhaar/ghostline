import { describe, expect, it } from 'vitest'
import { validatePullRequestURL } from './pullRequest'

describe('validatePullRequestURL', () => {
  it('accepts a GitHub PR URL', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/pull/12')).toBeUndefined())
  it('rejects other hosts', () => expect(validatePullRequestURL('https://evil.example/acme/ghostline/pull/12')).toBeTruthy())
  it('rejects a non-pull URL', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/issues/12')).toBeTruthy())
  it('rejects query strings and fragments', () => expect(validatePullRequestURL('https://github.com/acme/ghostline/pull/12?x=1')).toBeTruthy())
})
