// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import { fetchQodoReviewSession } from '../application/fetchQodoReviewSession'
import type { PullRequest } from '../domain/pullRequest'
import type { QodoStructuredReview } from '../domain/qodoReviewSession'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))
vi.mock('../application/fetchQodoReviewSession', () => ({ fetchQodoReviewSession: vi.fn() }))

const sha = 'a'.repeat(40)
const makePR = (number: number): PullRequest => ({
  repository: 'acme/app', owner: 'acme', repo: 'app', number, title: `Change ${number}`, state: 'open',
  url: `https://github.com/acme/app/pull/${number}`, createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [],
  checks: { availability: 'available', revisionSha: sha, revisionShortSha: sha.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${sha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
})

const review = (title: string): QodoStructuredReview => ({
  availability: 'available', source: 'Qodo Agentic Toolbox · get-pr-review-findings',
  reviewSession: { status: 'completed', commitSha: sha },
  findings: [{ id: title, title, status: 'open', gitSha: sha }], reviewRuns: [],
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function settle() {
  await Promise.resolve()
  await nextTick()
  await nextTick()
}

async function setValue(selector: string, value: string) {
  const input = host!.querySelector<HTMLInputElement>(selector)!
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
}

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
}

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe('structured Qodo review request binding', () => {
  it('keeps the latest clicked finding result and binds requests to the imported PR, not the edited URL field', async () => {
    vi.stubGlobal('isSecureContext', true)
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    const first = deferred<QodoStructuredReview>()
    const second = deferred<QodoStructuredReview>()
    vi.mocked(fetchQodoReviewSession).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    await mount()

    await setValue('#pr-url', 'https://github.com/acme/app/pull/7')
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await settle()
    await setValue('#qodo-api-key', 'first-key')
    host!.querySelector<HTMLButtonElement>('#load-qodo-review')!.click()
    await settle()

    await setValue('#pr-url', 'https://github.com/acme/app/pull/999')
    await setValue('#qodo-api-key', 'second-key')
    host!.querySelector<HTMLButtonElement>('#load-qodo-review')!.click()
    await settle()

    expect(fetchQodoReviewSession).toHaveBeenNthCalledWith(1, 'https://github.com/acme/app/pull/7', 'first-key')
    expect(fetchQodoReviewSession).toHaveBeenNthCalledWith(2, 'https://github.com/acme/app/pull/7', 'second-key')

    second.resolve(review('Latest structured finding'))
    await settle()
    first.resolve(review('Stale structured finding'))
    await settle()

    expect(host?.textContent).toContain('Latest structured finding')
    expect(host?.textContent).not.toContain('Stale structured finding')
    expect(host?.querySelector<HTMLInputElement>('#qodo-api-key')?.value).toBe('')
  })
})
