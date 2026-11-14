import { describe, expect, it } from 'vitest'
import { GhostlineSessionSchema } from './auth'

describe('GhostlineSessionSchema', () => {
  it('accepts local-demo and authenticated hosted sessions', () => {
    expect(GhostlineSessionSchema.safeParse({ authRequired: false, authenticated: true }).success).toBe(true)
    expect(GhostlineSessionSchema.safeParse({ authRequired: true, authenticated: true, login: 'felix' }).success).toBe(true)
  })

  it('rejects authenticated hosted state without a source identity', () => {
    expect(GhostlineSessionSchema.safeParse({ authRequired: true, authenticated: true }).success).toBe(false)
    expect(GhostlineSessionSchema.safeParse({ authRequired: true, authenticated: false, login: 'felix' }).success).toBe(false)
  })
})
