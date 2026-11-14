import { describe, expect, it, vi } from 'vitest'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import { useGhostlineSession } from './useGhostlineSession'

vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn() }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('latest Ghostline access check wins', () => {
  it('ignores an older response or failure that settles after a newer check', async () => {
    const earlier = deferred<{ authRequired: boolean; authenticated: boolean }>()
    vi.mocked(fetchGhostlineSession)
      .mockReturnValueOnce(earlier.promise)
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'latest-account' })
    const session = useGhostlineSession()

    const earlierRequest = session.refreshGhostlineSession()
    const latestRequest = session.refreshGhostlineSession()
    await latestRequest
    earlier.reject(new Error('stale access check failed'))
    await earlierRequest

    expect(session.ghostlineSession.value).toEqual({ authRequired: true, authenticated: true, login: 'latest-account' })
    expect(session.sessionError.value).toBe('')
    expect(session.sessionLoading.value).toBe(false)
  })

  it('hides an expired workspace while rechecking session state', async () => {
    vi.mocked(fetchGhostlineSession)
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
      .mockResolvedValueOnce({ authRequired: true, authenticated: false })
    const session = useGhostlineSession()
    await session.refreshGhostlineSession()
    expect(session.ghostlineSession.value?.authenticated).toBe(true)

    const recheck = session.handleSessionRejected()
    expect(session.ghostlineSession.value).toBeUndefined()
    expect(session.sessionLoading.value).toBe(true)
    await recheck
    expect(session.ghostlineSession.value).toEqual({ authRequired: true, authenticated: false })
  })
})
