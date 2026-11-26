import { endsReadinessSweep, fetchReadiness as fetchReadinessFromApi } from './fetchReadiness'
import { GhostlineApiError } from './apiError'
import type { OpenPullRequest, RowCheck } from '../domain/openPullRequests'
import type { Readiness } from '../domain/readiness'

export interface ReadinessSweep {
  repository: string
  rows: OpenPullRequest[]
  // isCurrent is false once the list the sweep started for has been replaced.
  isCurrent: () => boolean
  // skip leaves out rows the caller already holds a verdict for.
  skip: (row: OpenPullRequest) => boolean
  onUpdate: (number: number, check: RowCheck) => void
  onFailure: (error: unknown) => void
  fetchReadiness?: (url: string, repository: string, number: number) => Promise<Readiness>
}

// Checks each listed pull request in turn, so a long list meets the request limit
// gradually. Each check asks for the row's own pull request. A failure belongs to its
// row and the sweep goes on, unless endsReadinessSweep says the rest would fail too.
export async function sweepReadiness(sweep: ReadinessSweep): Promise<void> {
  const fetchReadiness = sweep.fetchReadiness ?? fetchReadinessFromApi
  for (const row of sweep.rows) {
    if (sweep.skip(row)) continue
    sweep.onUpdate(row.number, { state: 'checking' })
    try {
      const readiness = await fetchReadiness(row.url, sweep.repository, row.number)
      if (!sweep.isCurrent()) return
      sweep.onUpdate(row.number, { state: 'checked', readiness })
    } catch (error) {
      if (!sweep.isCurrent()) return
      // Ghostline answers 409 when the pull request changed between its reads.
      if (error instanceof GhostlineApiError && error.status === 409) {
        sweep.onUpdate(row.number, { state: 'changed' })
        continue
      }
      sweep.onUpdate(row.number, { state: 'error', message: error instanceof Error ? error.message : 'Could not check merge readiness.' })
      sweep.onFailure(error)
      if (endsReadinessSweep(error)) return
    }
  }
}
