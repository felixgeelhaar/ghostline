// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import type { PullRequest } from '../domain/pullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchAuthStatus', () => ({ fetchAuthStatus: vi.fn().mockResolvedValue({ enabled: false, login: null }) }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const makePR = (number: number, title: string): PullRequest => ({
  repository: 'acme/app', owner: 'acme', repo: 'app', number, title, state: 'open',
  url: `https://github.com/acme/app/pull/${number}`, createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [],
  checks: { availability: 'available', revisionSha: 'a'.repeat(40), revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/app/commit/${'a'.repeat(40)}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function settle() {
  await Promise.resolve()
  await nextTick()
  await nextTick()
}

async function submitURL(value: string) {
  const input = host!.querySelector<HTMLInputElement>('#pr-url')!
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }))
  await settle()
}

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
}

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('latest pull request import wins', () => {
  it('ignores an earlier success that settles after the latest import', async () => {
    const earlier = deferred<PullRequest>()
    vi.mocked(importPullRequest)
      .mockReturnValueOnce(earlier.promise)
      .mockResolvedValueOnce(makePR(8, 'Latest change'))
    await mount()

    await submitURL('https://github.com/acme/app/pull/7')
    await submitURL('https://github.com/acme/app/pull/8')
    expect(host?.querySelector('h2')?.textContent).toBe('Latest change')

    earlier.resolve(makePR(7, 'Earlier change'))
    await settle()

    expect(host?.querySelector('h2')?.textContent).toBe('Latest change')
    expect(host?.textContent).not.toContain('Earlier change')
  })

  it('ignores an earlier failure that settles after the latest import', async () => {
    const earlier = deferred<PullRequest>()
    vi.mocked(importPullRequest)
      .mockReturnValueOnce(earlier.promise)
      .mockResolvedValueOnce(makePR(8, 'Latest change'))
    await mount()

    await submitURL('https://github.com/acme/app/pull/7')
    await submitURL('https://github.com/acme/app/pull/8')
    earlier.reject(new Error('Earlier import failed'))
    await settle()

    expect(host?.querySelector('h2')?.textContent).toBe('Latest change')
    expect(host?.querySelector('[role="alert"]')).toBeNull()
    expect(host?.textContent).not.toContain('FETCHING…')
  })

  it('clears loading and keeps the latest validation error when an older request settles', async () => {
    const earlier = deferred<PullRequest>()
    vi.mocked(importPullRequest).mockReturnValueOnce(earlier.promise)
    await mount()

    await submitURL('https://github.com/acme/app/pull/7')
    await submitURL('not a GitHub pull request')
    expect(host?.querySelector('[role="alert"]')?.textContent).toContain('Enter a valid GitHub pull request URL')
    expect(host?.textContent).not.toContain('FETCHING…')

    earlier.resolve(makePR(7, 'Stale result'))
    await settle()

    expect(host?.querySelector('h2')).toBeNull()
    expect(host?.querySelector('[role="alert"]')?.textContent).toContain('Enter a valid GitHub pull request URL')
    expect(host?.textContent).not.toContain('FETCHING…')
  })
})
