import type { NoxReport } from '../domain/noxReport'

type ReportFetcher = (url: string, sha: string) => Promise<NoxReport>

type NoxReportStateUpdate = {
  loading: boolean
  report?: NoxReport
  error?: string
}

export interface NoxReportLoaderOptions {
  fetchReport: ReportFetcher
  isCurrent: (pullRequestURL: string, revisionSHA: string) => boolean
  publish: (update: NoxReportStateUpdate) => void
  onError?: (error: unknown) => void
  /** How long focus must rest on a revision before its report is requested. */
  settleDelayMs?: number
}

const defaultSettleDelayMs = 300

/** Keeps report loading and stale-response rules outside the view component. */
export function createNoxReportLoader(options: NoxReportLoaderOptions) {
  let request = 0

  function invalidate() {
    request++
  }

  async function load(pullRequestURL: string, revisionSHA: string) {
    const currentRequest = ++request
    if (!revisionSHA) return

    options.publish({ loading: true, error: '' })
    await fetchAndPublish(currentRequest, pullRequestURL, revisionSHA)
  }

  /**
   * Requests a report once the revision has stayed selected for the settle
   * delay. Report requests share the per-client API budget with imports, so
   * quickly browsing revisions must not spend one request per click.
   */
  async function loadWhenSettled(pullRequestURL: string, revisionSHA: string) {
    const currentRequest = ++request
    if (!revisionSHA) return

    options.publish({ loading: true, error: '' })
    await new Promise((resolve) => setTimeout(resolve, options.settleDelayMs ?? defaultSettleDelayMs))
    if (currentRequest !== request || !options.isCurrent(pullRequestURL, revisionSHA)) return
    await fetchAndPublish(currentRequest, pullRequestURL, revisionSHA)
  }

  async function fetchAndPublish(currentRequest: number, pullRequestURL: string, revisionSHA: string) {
    try {
      const report = await options.fetchReport(pullRequestURL, revisionSHA)
      if (currentRequest !== request || !options.isCurrent(pullRequestURL, revisionSHA)) return
      options.publish({ loading: false, report, error: '' })
    } catch (error) {
      if (currentRequest !== request || !options.isCurrent(pullRequestURL, revisionSHA)) return
      options.onError?.(error)
      options.publish({
        loading: false,
        error: error instanceof Error ? error.message : 'Could not load the Nox report for this revision.',
      })
    } finally {
      if (currentRequest === request) options.publish({ loading: false })
    }
  }

  return { invalidate, load, loadWhenSettled }
}
