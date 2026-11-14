// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import { importPullRequest } from '../application/importPullRequest'
import { GhostlineApiError } from '../application/apiError'
import App from './App.vue'

vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn() }))
vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await Promise.resolve()
  await nextTick()
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe('hosted GitHub sign-in presentation', () => {
  it('keeps the workspace hidden until the access check completes', async () => {
    const pending = deferred<{ authRequired: boolean; authenticated: boolean }>()
    vi.mocked(fetchGhostlineSession).mockReturnValue(pending.promise)
    await mount()

    expect(host?.querySelector('[role="status"]')?.textContent).toContain('Checking sign-in')
    expect(host?.querySelector('#pr-url')).toBeNull()

    pending.resolve({ authRequired: false, authenticated: true })
    await Promise.resolve()
    await nextTick()
    expect(host?.querySelector('#pr-url')).not.toBeNull()
  })

  it('shows the GitHub sign-in path without exposing the pull request workspace', async () => {
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authRequired: true, authenticated: false })
    await mount()

    expect(host?.querySelector<HTMLButtonElement>('form[action="/auth/login"] button')?.textContent).toContain('SIGN IN WITH GITHUB')
    expect(host?.querySelector('#pr-url')).toBeNull()
    expect(host?.textContent).toContain('Use the GitHub account invited')
  })

  it('shows the signed-in account and ends its session', async () => {
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 204 })))
    await mount()

    expect(host?.querySelector('.account')?.textContent).toContain('Felix-Dev')
    host?.querySelector<HTMLButtonElement>('.account button')?.click()
    await Promise.resolve()
    await nextTick()

    expect(fetch).toHaveBeenCalledWith('/auth/logout', { method: 'POST', credentials: 'same-origin' })
    expect(host?.querySelector('form[action="/auth/login"] button')).not.toBeNull()
  })

  it('warns when sign-out fails and the session remains active', async () => {
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })))
    await mount()

    host?.querySelector<HTMLButtonElement>('.account button')?.click()
    await Promise.resolve()
    await nextTick()

    expect(host?.querySelector('[role="alert"]')?.textContent).toContain('could not end this session')
    expect(host?.querySelector('.account')?.textContent).toContain('Felix-Dev')
    expect(host?.querySelector('.workspace-content')).not.toBeNull()
  })

  it('fails closed when session status cannot be verified', async () => {
    vi.mocked(fetchGhostlineSession).mockRejectedValue(new Error('Could not reach Ghostline to verify sign-in status.'))
    await mount()

    expect(host?.querySelector('[role="alert"]')?.textContent).toContain('Could not reach Ghostline')
    expect(host?.querySelector('#pr-url')).toBeNull()
  })

  it('hides the workspace and returns to sign-in when a protected API reports an expired session', async () => {
    vi.mocked(fetchGhostlineSession)
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
      .mockResolvedValueOnce({ authRequired: true, authenticated: false })
    vi.mocked(importPullRequest).mockRejectedValue(new GhostlineApiError(401, 'Your Ghostline session expired.', 'AUTH_REQUIRED'))
    await mount()

    const input = host?.querySelector<HTMLInputElement>('#pr-url')
    if (!input) throw new Error('PR import field was not rendered')
    input.value = 'https://github.com/acme/app/pull/7'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    host?.querySelector<HTMLButtonElement>('.import-panel button')?.click()

    await vi.waitFor(() => expect(host?.querySelector('form[action="/auth/login"]')).not.toBeNull())
    expect(host?.querySelector('#pr-url')).toBeNull()
    expect(host?.textContent).toContain('Sign in to your control room')
  })
})
