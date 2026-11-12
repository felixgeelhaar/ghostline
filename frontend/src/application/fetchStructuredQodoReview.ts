import { ApiResponseError, apiErrorMessage } from './apiError'
import { StructuredQodoReviewSetSchema } from '../domain/structuredQodoReviews'

export async function fetchStructuredQodoReview(url: string, apiKey: string) {
  const response = await fetch('/api/qodo/reviews', {
    method: 'POST',
    cache: 'no-store',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Qodo-API-Key': apiKey },
    body: JSON.stringify({ url }),
  })
  const payload: unknown = await response.json()
  if (!response.ok) throw new ApiResponseError(apiErrorMessage(response, payload, 'Unable to load structured Qodo review data.'), response.status)
  const result = StructuredQodoReviewSetSchema.safeParse(payload)
  if (!result.success) throw new Error('Qodo returned review data that did not pass validation. Please retry the lookup.')
  return result.data
}
