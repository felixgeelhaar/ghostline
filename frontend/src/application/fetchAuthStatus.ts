import { AuthStatusSchema, type AuthStatus } from '../domain/auth'

export async function fetchAuthStatus(): Promise<AuthStatus> {
  const response = await fetch('/api/auth/status', { headers: { Accept: 'application/json' } })
  if (!response.ok) throw new Error('Could not check Ghostline sign-in status. Reload to try again.')
  const result = AuthStatusSchema.safeParse(await response.json())
  if (!result.success) throw new Error('Ghostline returned an invalid sign-in status. Reload to try again.')
  return result.data
}
