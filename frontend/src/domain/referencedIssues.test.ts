import { describe, expect, it } from 'vitest'
import { isMergedOpenIssue } from './referencedIssues'

describe('isMergedOpenIssue', () => {
  it('flags a GitHub-confirmed open issue only for a merged PR', () => {
    expect(isMergedOpenIssue(true, { availability: 'available', state: 'open' })).toBe(true)
    expect(isMergedOpenIssue(false, { availability: 'available', state: 'open' })).toBe(false)
    expect(isMergedOpenIssue(true, { availability: 'available', state: 'closed' })).toBe(false)
    expect(isMergedOpenIssue(true, { availability: 'unavailable' })).toBe(false)
  })
})
