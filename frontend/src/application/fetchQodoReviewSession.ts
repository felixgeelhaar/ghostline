import { apiErrorMessage } from './apiError'
import { QodoStructuredReviewSchema, type QodoStructuredReview } from '../domain/qodoReviewSession'

export async function fetchQodoReviewSession(url: string, apiKey: string): Promise<QodoStructuredReview> {
  const response = await fetch('/api/qodo-review', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Qodo-API-Key': apiKey },
    body: JSON.stringify({ url }),
  })
  let payload: unknown = null
  try {
    payload = await response.json()
  } catch {
    // Proxies can return empty or non-JSON error pages; status-based guidance
    // still applies, including Retry-After for rate limits.
  }
  if (!response.ok) throw new Error(apiErrorMessage(response, payload, 'Unable to load structured Qodo review data.'))
  const result = QodoStructuredReviewSchema.safeParse(payload)
  if (!result.success) throw new Error('Qodo returned review data that did not pass validation. Please retry.')
  return result.data
}
