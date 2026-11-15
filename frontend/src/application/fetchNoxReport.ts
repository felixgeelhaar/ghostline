import { ghostlineApiError } from './apiError'
import { NoxReportSchema } from '../domain/noxReport'

export async function fetchNoxReport(url: string, sha: string) {
  const response = await fetch('/api/nox-report', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, sha }),
  })
  const payload: unknown = await response.json()
  if (!response.ok) throw ghostlineApiError(response, payload, 'Unable to load the Nox report for this revision.')
  const report = NoxReportSchema.parse(payload)
  if (report.revisionSha !== sha) throw new Error('GitHub returned a Nox report for a different revision.')
  return report
}
