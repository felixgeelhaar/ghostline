import { describe, expect, it } from 'vitest'
import { AuthStatusSchema } from './auth'

describe('AuthStatusSchema', () => {
  it('accepts an enabled signed-out session state', () => {
    expect(AuthStatusSchema.parse({ enabled: true, login: null })).toEqual({ enabled: true, login: null })
  })

  it('accepts a signed-in GitHub identity', () => {
    expect(AuthStatusSchema.parse({ enabled: true, login: 'felixgeelhaar' }).login).toBe('felixgeelhaar')
  })

  it('rejects malformed authentication status payloads', () => {
    expect(AuthStatusSchema.safeParse({ enabled: 'yes', login: null }).success).toBe(false)
    expect(AuthStatusSchema.safeParse({ enabled: true, login: '' }).success).toBe(false)
  })
})
