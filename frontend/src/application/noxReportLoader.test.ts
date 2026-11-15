import { describe, expect, it, vi } from 'vitest'
import { createNoxReportLoader } from './noxReportLoader'
import { NoxReportSchema } from '../domain/noxReport'

const shaA = 'a'.repeat(40)
const shaB = 'b'.repeat(40)
const url = 'https://github.com/acme/app/pull/7'
const report = (revisionSha: string) => NoxReportSchema.parse({
  availability: 'available', revisionSha, runId: 12, runName: 'Nox Security', runConclusion: 'success',
  runUrl: 'https://github.com/acme/app/actions/runs/12', artifactId: 4,
  artifactUrl: 'https://api.github.com/repos/acme/app/actions/artifacts/4', artifactDigest: 'sha256:abc',
  createdAt: '2026-11-15T13:58:17Z', expiresAt: '2026-11-22T13:58:16Z', totalCount: 0, activeCount: 0, suppressedCount: 0, truncated: false, findings: [],
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('revision-bound Nox report loader', () => {
  it('publishes only the latest request when an earlier revision resolves last', async () => {
    const first = deferred<ReturnType<typeof report>>()
    const second = deferred<ReturnType<typeof report>>()
    let currentRevision = shaA
    const publish = vi.fn()
    const fetchReport = vi.fn((_url: string, sha: string) => sha === shaA ? first.promise : second.promise)
    const loader = createNoxReportLoader({ fetchReport, publish, isCurrent: (_url, sha) => sha === currentRevision })

    const earlierRequest = loader.load(url, shaA)
    currentRevision = shaB
    const latestRequest = loader.load(url, shaB)
    second.resolve(report(shaB))
    await latestRequest
    first.resolve(report(shaA))
    await earlierRequest

    expect(publish).toHaveBeenCalledWith({ loading: false, report: report(shaB), error: '' })
    expect(publish).not.toHaveBeenCalledWith({ loading: false, report: report(shaA), error: '' })
  })

  it('ignores a response after the pull-request view has changed', async () => {
    const request = deferred<ReturnType<typeof report>>()
    let isCurrent = true
    const publish = vi.fn()
    const loader = createNoxReportLoader({ fetchReport: () => request.promise, publish, isCurrent: () => isCurrent })
    const pending = loader.load(url, shaA)
    isCurrent = false
    request.resolve(report(shaA))
    await pending

    expect(publish).not.toHaveBeenCalledWith({ loading: false, report: report(shaA), error: '' })
  })

  it('invalidates pending responses when the view is cleared', async () => {
    const request = deferred<ReturnType<typeof report>>()
    const publish = vi.fn()
    const loader = createNoxReportLoader({ fetchReport: () => request.promise, publish, isCurrent: () => true })
    const pending = loader.load(url, shaA)
    loader.invalidate()
    request.resolve(report(shaA))
    await pending

    expect(publish).not.toHaveBeenCalledWith({ loading: false, report: report(shaA), error: '' })
  })

  it('publishes a current request failure and reports it to the session handler', async () => {
    const failure = new Error('Actions read permission is missing')
    const publish = vi.fn()
    const onError = vi.fn()
    const loader = createNoxReportLoader({ fetchReport: async () => { throw failure }, publish, onError, isCurrent: () => true })
    await loader.load(url, shaA)

    expect(onError).toHaveBeenCalledWith(failure)
    expect(publish).toHaveBeenCalledWith({ loading: false, error: failure.message })
  })
})
