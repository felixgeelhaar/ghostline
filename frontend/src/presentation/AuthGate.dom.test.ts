// @vitest-environment happy-dom
import { createApp } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchAuthStatus } from '../application/fetchAuthStatus'
import App from './App.vue'

vi.mock('../application/fetchAuthStatus', () => ({ fetchAuthStatus: vi.fn() }))
vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('.auth-panel, #pr-url')).not.toBeNull())
}

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('hosted GitHub sign-in gate', () => {
  it('keeps a signed-out visitor out of the PR workspace', async () => {
    vi.mocked(fetchAuthStatus).mockResolvedValue({ enabled: true, login: null })
    await mount()

    expect(host?.querySelector('.auth-panel h1')?.textContent).toBe('Sign in to continue.')
    expect(host?.querySelector<HTMLAnchorElement>('.github-sign-in')?.getAttribute('href')).toBe('/auth/github/login')
    expect(host?.querySelector('#pr-url')).toBeNull()
    expect(host?.textContent).toContain('does not request repository access')
  })

  it('shows the change workspace and sign-out control for an approved account', async () => {
    vi.mocked(fetchAuthStatus).mockResolvedValue({ enabled: true, login: 'felixgeelhaar' })
    await mount()

    expect(host?.querySelector('#pr-url')).not.toBeNull()
    expect(host?.querySelector('.signed-in')?.textContent).toBe('felixgeelhaar')
    expect(host?.querySelector<HTMLFormElement>('.topbar-status form')?.getAttribute('action')).toBe('/auth/logout')
  })

  it('fails closed when the server cannot verify access status', async () => {
    vi.mocked(fetchAuthStatus).mockRejectedValue(new Error('Status API unavailable.'))
    await mount()

    expect(host?.querySelector('[role="alert"]')?.textContent).toContain('Status API unavailable.')
    expect(host?.querySelector('#pr-url')).toBeNull()
  })
})
