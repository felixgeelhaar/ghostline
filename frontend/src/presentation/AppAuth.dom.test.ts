// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { GhostlineApiError } from '../application/apiError'
import App from './App.vue'

vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn() }))
vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined
const extraApps: ReturnType<typeof createApp>[] = []
const extraHosts: HTMLDivElement[] = []

class TestBroadcastChannel {
  static instances = new Set<TestBroadcastChannel>()
  onmessage: ((event: MessageEvent) => void) | null = null

  constructor(private readonly name: string) { TestBroadcastChannel.instances.add(this) }

  postMessage(data: unknown) {
    for (const channel of TestBroadcastChannel.instances) {
      if (channel !== this && channel.name === this.name) channel.onmessage?.({ data } as MessageEvent)
    }
  }

  close() { TestBroadcastChannel.instances.delete(this) }
}

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
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

afterEach(() => {
  app?.unmount()
  extraApps.forEach((mounted) => mounted.unmount())
  extraApps.length = 0
  extraHosts.forEach((element) => element.remove())
  extraHosts.length = 0
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  TestBroadcastChannel.instances.clear()
  window.history.replaceState(window.history.state, '', '/')
})

const importedPRFixture = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Auth expiry', state: 'open', merged: false,
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: ['a', 'b'].map((prefix) => {
    const sha = prefix.repeat(40)
    return { sha, shortSha: sha.slice(0, 7), message: 'Commit', author: 'Felix', committedAt: '2026-11-10T09:30:00Z', url: `https://github.com/acme/app/commit/${sha}` }
  }),
  checks: { revisionSha: 'c'.repeat(40), revisionShortSha: 'ccccccc', revisionUrl: `https://github.com/acme/app/commit/${'c'.repeat(40)}`, availability: 'unavailable', reason: 'No checks', items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [] },
}

async function mountExtra() {
  const element = document.createElement('div')
  document.body.append(element)
  const mounted = createApp(App)
  extraHosts.push(element)
  extraApps.push(mounted)
  mounted.mount(element)
  await Promise.resolve()
  await nextTick()
  return element
}

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

  it('discards an in-flight pull request import when sign-out completes', async () => {
    const pendingImport = deferred<never>()
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
    vi.mocked(importPullRequest).mockReturnValue(pendingImport.promise)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 204 })))
    await mount()

    const input = host?.querySelector<HTMLInputElement>('#pr-url')
    if (!input) throw new Error('PR import field was not rendered')
    input.value = 'https://github.com/acme/app/pull/7'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    host?.querySelector<HTMLButtonElement>('.import-panel button')?.click()
    await vi.waitFor(() => expect(importPullRequest).toHaveBeenCalledOnce())

    host?.querySelector<HTMLButtonElement>('.account button')?.click()
    await vi.waitFor(() => expect(host?.querySelector('form[action="/auth/login"]')).not.toBeNull())
    pendingImport.resolve({} as never)
    await Promise.resolve()
    await nextTick()

    expect(host?.querySelector('#pr-url')).toBeNull()
    expect(host?.querySelector('.timeline')).toBeNull()
  })

  it('keeps late unauthorized errors from undoing the signed-out screen', async () => {
    const pendingImport = deferred<never>()
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
    vi.mocked(importPullRequest).mockReturnValue(pendingImport.promise)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 204 })))
    await mount()

    const input = host?.querySelector<HTMLInputElement>('#pr-url')
    if (!input) throw new Error('PR import field was not rendered')
    input.value = importedPRFixture.url
    input.dispatchEvent(new Event('input', { bubbles: true }))
    host?.querySelector<HTMLButtonElement>('.import-panel button')?.click()
    await vi.waitFor(() => expect(importPullRequest).toHaveBeenCalledOnce())
    host?.querySelector<HTMLButtonElement>('.account button')?.click()
    await vi.waitFor(() => expect(host?.querySelector('form[action="/auth/login"]')).not.toBeNull())

    pendingImport.reject(new GhostlineApiError(401, 'Your Ghostline session expired.', 'AUTH_REQUIRED'))
    await Promise.resolve()
    await nextTick()

    expect(host?.querySelector('form[action="/auth/login"]')).not.toBeNull()
    expect(fetchGhostlineSession).toHaveBeenCalledOnce()
  })

  it('clears another tab workspace when a new sign-in completes', async () => {
    vi.stubGlobal('BroadcastChannel', TestBroadcastChannel)
    vi.mocked(fetchGhostlineSession)
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'old-account' })
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'new-account' })
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'new-account' })
    vi.mocked(importPullRequest).mockResolvedValue(importedPRFixture as never)
    const otherTab = await mountExtra()
    const input = otherTab.querySelector<HTMLInputElement>('#pr-url')!
    input.value = importedPRFixture.url
    input.dispatchEvent(new Event('input', { bubbles: true }))
    otherTab.querySelector<HTMLButtonElement>('.import-panel button')?.click()
    await vi.waitFor(() => expect(otherTab.querySelector('.timeline')).not.toBeNull())

    window.history.replaceState(window.history.state, '', '/?ghostline_auth=complete')
    await mount()

    await vi.waitFor(() => expect(otherTab.querySelector('.account')?.textContent).toContain('new-account'))
    expect(otherTab.querySelector('.timeline')).toBeNull()
    expect(window.location.search).toBe('')
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

  it('hides the workspace when a stale revision check reports an expired session', async () => {
    const delayedChecks = deferred<never>()
    vi.mocked(fetchGhostlineSession)
      .mockResolvedValueOnce({ authRequired: true, authenticated: true, login: 'Felix-Dev' })
      .mockResolvedValueOnce({ authRequired: true, authenticated: false })
    vi.mocked(importPullRequest).mockResolvedValue(importedPRFixture as never)
    vi.mocked(fetchRevisionChecks).mockReturnValue(delayedChecks.promise)
    await mount()

    const input = host?.querySelector<HTMLInputElement>('#pr-url')
    if (!input) throw new Error('PR import field was not rendered')
    input.value = importedPRFixture.url
    input.dispatchEvent(new Event('input', { bubbles: true }))
    host?.querySelector<HTMLButtonElement>('.import-panel button')?.click()
    await vi.waitFor(() => expect(host?.querySelector('.timeline')).not.toBeNull())

    host?.querySelector<HTMLButtonElement>('[aria-label="Focus history on revision aaaaaaa"]')?.click()
    await nextTick()
    expect(fetchRevisionChecks).toHaveBeenCalledTimes(1)
    host?.querySelector<HTMLButtonElement>('.clear-focus')?.click()
    delayedChecks.reject(new GhostlineApiError(401, 'Your Ghostline session expired.', 'AUTH_REQUIRED'))

    await vi.waitFor(() => expect(host?.querySelector('form[action="/auth/login"]')).not.toBeNull())
    expect(host?.querySelector('#pr-url')).toBeNull()
  })
})
