// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import { fetchStructuredQodoReview } from '../application/fetchStructuredQodoReview'
import type { PullRequest } from '../domain/pullRequest'
import type { StructuredQodoReviewSet } from '../domain/structuredQodoReviews'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchAuthStatus', () => ({ fetchAuthStatus: vi.fn().mockResolvedValue({ enabled: false, login: null }) }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))
vi.mock('../application/fetchStructuredQodoReview', () => ({ fetchStructuredQodoReview: vi.fn() }))

const makePR = (number: number, title: string): PullRequest => ({
  repository: 'acme/app', owner: 'acme', repo: 'app', number, title, state: 'open',
  url: `https://github.com/acme/app/pull/${number}`, createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [],
  checks: { availability: 'available', revisionSha: 'a'.repeat(40), revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/app/commit/${'a'.repeat(40)}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
})

const review = (title: string): StructuredQodoReviewSet => ({
  provider: 'Qodo', sourceType: 'qodo_agentic_toolbox', currentRevisionSha: 'a'.repeat(40),
  reviewSession: { id: 'run-current', status: 'completed', commitSha: 'a'.repeat(40), revisionStatus: 'current', reviewComplete: true },
  findings: [{ id: 'finding-1', title, status: 'open', gitSha: 'a'.repeat(40), revisionStatus: 'current', reviewRunIds: ['run-current'], locations: [], citations: [] }],
  reviewRuns: [{ id: 'run-current', status: 'completed', commitSha: 'a'.repeat(40), revisionStatus: 'current', reviewComplete: true }],
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function settle() { await Promise.resolve(); await nextTick(); await nextTick() }

async function importPR(number: number, title: string) {
  const input = host!.querySelector<HTMLInputElement>('#pr-url')!
  input.value = `https://github.com/acme/app/pull/${number}`
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }))
  await settle()
  expect(host?.querySelector('h2')?.textContent).toBe(title)
}

async function submitStructuredKey(key: string) {
  const input = host!.querySelector<HTMLInputElement>('#qodo-api-key')!
  input.value = key
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host!.querySelector<HTMLFormElement>('#structured-qodo-form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await settle()
}

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
}

afterEach(() => { app?.unmount(); app = undefined; host?.remove(); host = undefined; vi.clearAllMocks() })

describe('structured Qodo review lookup', () => {
  it('uses the imported pull request and clears the request-scoped key after submission', async () => {
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7, 'Imported change'))
    vi.mocked(fetchStructuredQodoReview).mockResolvedValue(review('Inspect this finding'))
    await mount()
    await importPR(7, 'Imported change')

    const urlInput = host!.querySelector<HTMLInputElement>('#pr-url')!
    urlInput.value = 'https://github.com/acme/app/pull/8'
    urlInput.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    await submitStructuredKey('request-only-personal-key')

    expect(fetchStructuredQodoReview).toHaveBeenCalledWith('https://github.com/acme/app/pull/7', 'request-only-personal-key')
    expect(host?.querySelector<HTMLInputElement>('#qodo-api-key')?.value).toBe('')
    expect(host?.textContent).toContain('Inspect this finding')
  })

  it('ignores structured results for an earlier imported pull request when they settle last', async () => {
    const earlier = deferred<StructuredQodoReviewSet>()
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR(7, 'Earlier change')).mockResolvedValueOnce(makePR(8, 'Latest change'))
    vi.mocked(fetchStructuredQodoReview).mockReturnValueOnce(earlier.promise)
    await mount()
    await importPR(7, 'Earlier change')
    await submitStructuredKey('first-request-key')

    await importPR(8, 'Latest change')
    earlier.resolve(review('Stale structured result'))
    await settle()

    expect(host?.querySelector('h2')?.textContent).toBe('Latest change')
    expect(host?.textContent).not.toContain('Stale structured result')
    expect(host?.textContent).not.toContain('FETCHING…')
  })
})
