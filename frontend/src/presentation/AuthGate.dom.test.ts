// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiResponseError } from '../application/apiError'
import { fetchAuthStatus } from '../application/fetchAuthStatus'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { importPullRequest } from '../application/importPullRequest'
import { compareRevisions } from '../application/compareRevisions'
import type { PullRequest } from '../domain/pullRequest'
import App from './App.vue'

vi.mock('../application/fetchAuthStatus', () => ({ fetchAuthStatus: vi.fn() }))
vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))
vi.mock('../application/compareRevisions', () => ({ compareRevisions: vi.fn() }))

const headSha = 'a'.repeat(40)
const earlierSha = 'b'.repeat(40)
const middleSha = 'c'.repeat(40)
const makePR = (): PullRequest => ({
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 12, title: 'Session-bound change', state: 'open', merged: false,
  url: 'https://github.com/acme/app/pull/12', createdAt: '2026-11-11T09:00:00Z', updatedAt: '2026-11-11T10:00:00Z',
  commits: [
    { sha: earlierSha, shortSha: 'bbbbbbb', message: 'Add behavior', author: 'felix', committedAt: '2026-11-11T09:00:00Z', url: `https://github.com/acme/app/commit/${earlierSha}` },
    { sha: middleSha, shortSha: 'ccccccc', message: 'Candidate repair', author: 'felix', committedAt: '2026-11-11T09:30:00Z', url: `https://github.com/acme/app/commit/${middleSha}` },
    { sha: headSha, shortSha: 'aaaaaaa', message: 'Follow-up', author: 'felix', committedAt: '2026-11-11T10:00:00Z', url: `https://github.com/acme/app/commit/${headSha}` },
  ],
  checks: { availability: 'available', revisionSha: headSha, revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/app/commit/${headSha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [] },
})

function unauthorized() {
  return new ApiResponseError('Sign in with GitHub to continue.', 401)
}

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

  it('returns to the sign-in gate when an import reports an expired session', async () => {
    vi.mocked(fetchAuthStatus).mockResolvedValueOnce({ enabled: true, login: 'felixgeelhaar' })
    vi.mocked(importPullRequest).mockRejectedValueOnce(unauthorized())
    await mount()

    const input = host!.querySelector<HTMLInputElement>('#pr-url')!
    input.value = 'https://github.com/acme/app/pull/12'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()

    await vi.waitFor(() => expect(host?.querySelector('.github-sign-in')).not.toBeNull())
    expect(host?.querySelector('#pr-url')).toBeNull()
    expect(host?.textContent).toContain('Sign in to continue.')
  })

  it('returns to the sign-in gate when a focused-revision check reports an expired session', async () => {
    vi.mocked(fetchAuthStatus).mockResolvedValueOnce({ enabled: true, login: 'felixgeelhaar' })
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR())
    vi.mocked(fetchRevisionChecks).mockRejectedValueOnce(unauthorized())
    await mount()

    const input = host!.querySelector<HTMLInputElement>('#pr-url')!
    input.value = 'https://github.com/acme/app/pull/12'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await vi.waitFor(() => expect(host?.querySelector('h2')?.textContent).toBe('Session-bound change'))
    host!.querySelector<HTMLButtonElement>('[aria-label="Focus history on revision bbbbbbb"]')!.click()

    await vi.waitFor(() => expect(host?.querySelector('.github-sign-in')).not.toBeNull())
    expect(host?.querySelector('#pr-url')).toBeNull()
  })

  it('returns to the sign-in gate when a Deep Trace comparison reports an expired session', async () => {
    vi.mocked(fetchAuthStatus).mockResolvedValueOnce({ enabled: true, login: 'felixgeelhaar' })
    const pr = makePR()
    pr.qodoReviews.items = [earlierSha, headSha].map((sha, index) => ({
      id: 20 + index, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings',
      findings: [{ id: `finding-${index}`, title: 'Repeated issue', categories: [], sourceUrl: `https://github.com/acme/app/pull/12#issuecomment-${20 + index}` }],
      commitSha: sha, commitUrl: `https://github.com/acme/app/commit/${sha}`, createdAt: index === 0 ? '2026-11-11T09:00:00Z' : '2026-11-11T10:00:00Z',
      url: `https://github.com/acme/app/pull/12#issuecomment-${20 + index}`,
    }))
    vi.mocked(importPullRequest).mockResolvedValueOnce(pr)
    vi.mocked(compareRevisions).mockRejectedValueOnce(unauthorized())
    await mount()

    const input = host!.querySelector<HTMLInputElement>('#pr-url')!
    input.value = pr.url
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await vi.waitFor(() => expect(host?.querySelector('.trace-open')).not.toBeNull())
    host!.querySelector<HTMLButtonElement>('.trace-open')!.click()
    await vi.waitFor(() => expect(host?.querySelector('[aria-label^="Compare candidate"]')).not.toBeNull())
    host!.querySelector<HTMLButtonElement>('[aria-label^="Compare candidate"]')!.click()

    await vi.waitFor(() => expect(host?.querySelector('.github-sign-in')).not.toBeNull())
    expect(host?.querySelector('#pr-url')).toBeNull()
  })
})
