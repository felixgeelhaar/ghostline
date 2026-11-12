import { apiErrorMessage } from './apiError'
import { CheckAnnotationPageSchema } from '../domain/checkAnnotations'

export async function fetchCheckAnnotations(url: string, sha: string, checkRunId: number, page: number) {
  const response = await fetch('/api/check-annotations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, sha, checkRunId, page }),
  })
  const payload: unknown = await response.json()
  if (!response.ok) throw new Error(apiErrorMessage(response, payload, 'Unable to load annotations for this check.'))
  const result = CheckAnnotationPageSchema.parse(payload)
  if (result.revisionSha !== sha || result.checkRunId !== checkRunId || result.page !== page) {
    throw new Error('GitHub returned annotations for a different check run or revision.')
  }
  return result
}
