import { ghostlineApiError, readResponsePayload } from './apiError'
import { ReadinessSchema } from '../domain/readiness'

// The URL, repository and number come from the imported pull request, never from the
// editable import field, so the verdict can be checked against the PR it was asked for.
export async function fetchReadiness(url: string, repository: string, number: number) {
  const response = await fetch('/api/readiness', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  })
  const payload = await readResponsePayload(response)
  if (!response.ok) throw ghostlineApiError(response, payload, 'Unable to check merge readiness for this pull request.')
  const parsed = ReadinessSchema.safeParse(payload)
  if (!parsed.success) throw new Error('Ghostline returned a readiness verdict without a verifiable head commit.')
  if (parsed.data.repository.toLowerCase() !== repository.toLowerCase() || parsed.data.number !== number) {
    throw new Error('Ghostline returned a readiness verdict for a different pull request.')
  }
  return parsed.data
}
