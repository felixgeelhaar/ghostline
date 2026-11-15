import { GhostlineSessionSchema } from '../domain/auth'
import type { GhostlineSession } from '../domain/auth'

export async function fetchGhostlineSession(): Promise<GhostlineSession> {
  let response: Response
  try {
    response = await fetch('/api/session', { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
  } catch {
    throw new Error('Could not reach Ghostline to verify sign-in status.')
  }
  if (!response.ok) throw new Error('Ghostline could not verify sign-in status. Try again.')
  const parsed = GhostlineSessionSchema.safeParse(await response.json())
  if (!parsed.success) throw new Error('Ghostline returned invalid sign-in status.')
  return parsed.data
}
