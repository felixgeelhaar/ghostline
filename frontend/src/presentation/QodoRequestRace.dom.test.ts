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
  repository: 'acme/app', owner: 'acme', repo: 'app', number, title: `Change ${number}`, state: 'open', merged: false,
  url: `https://github.com/acme/app/pull/${number}`, createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [],
  checks: { availability: 'available', revisionSha: sha, revisionShortSha: sha.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${sha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [] },
})

const review = (title: string): QodoStructuredReview => ({
  availability: 'available', source: 'Qodo Agentic Toolbox · get-pr-review-findings',
  reviewSession: { status: 'completed', commitSha: sha },
  findings: [{ id: title, title, status: 'open', gitSha: sha }], reviewRuns: [],
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
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
  it('labels the first load, active request, and subsequent refresh accurately', async () => {
    vi.stubGlobal('isSecureContext', true)
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    const pending = deferred<QodoStructuredReview>()
    vi.mocked(fetchQodoReviewSession).mockReturnValueOnce(pending.promise)
    await mount()

    await setValue('#pr-url', 'https://github.com/acme/app/pull/7')
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await settle()
    const button = host!.querySelector<HTMLButtonElement>('#load-qodo-review')!
    expect(button.textContent).toContain('LOAD STRUCTURED REVIEW')

    await setValue('#qodo-api-key', 'first-key')
    button.click()
    await settle()
    expect(button.textContent).toContain('LOADING STRUCTURED REVIEW')

    pending.resolve(review('Initial finding'))
    await settle()
    expect(button.textContent).toContain('REFRESH STRUCTURED REVIEW')
  })

  it('refuses to send a personal Qodo key from an insecure context', async () => {
    vi.stubGlobal('isSecureContext', false)
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    await mount()

    await setValue('#pr-url', 'https://github.com/acme/app/pull/7')
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await settle()

    const keyInput = host!.querySelector<HTMLInputElement>('#qodo-api-key')!
    expect(keyInput.disabled).toBe(true)
    expect(host?.textContent).toContain('Secure connection required.')

    // Simulate a forged form submission to exercise the handler's second guard.
    keyInput.disabled = false
    await setValue('#qodo-api-key', 'sensitive-test-key')
    host!.querySelector<HTMLFormElement>('.qodo-connect')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await settle()

    expect(fetchQodoReviewSession).not.toHaveBeenCalled()
  })

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

  it('keeps the existing findings visible while refreshing and when refresh fails', async () => {
    vi.stubGlobal('isSecureContext', true)
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    const refresh = deferred<QodoStructuredReview>()
    vi.mocked(fetchQodoReviewSession).mockResolvedValueOnce(review('Existing finding')).mockReturnValueOnce(refresh.promise)
    await mount()

    await setValue('#pr-url', 'https://github.com/acme/app/pull/7')
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await settle()
    await setValue('#qodo-api-key', 'first-key')
    host!.querySelector<HTMLButtonElement>('#load-qodo-review')!.click()
    await settle()
    expect(host?.textContent).toContain('Existing finding')

    await setValue('#qodo-api-key', 'refresh-key')
    host!.querySelector<HTMLButtonElement>('#load-qodo-review')!.click()
    await settle()
    expect(host?.textContent).toContain('Existing finding')
    refresh.reject(new Error('Qodo is temporarily unavailable.'))
    await settle()

    expect(host?.textContent).toContain('Existing finding')
    expect(host?.querySelector('[role="alert"]')?.textContent).toContain('temporarily unavailable')
  })

  it('shows actionable guidance when Qodo rejects an expired API key', async () => {
    vi.stubGlobal('isSecureContext', true)
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    vi.mocked(fetchQodoReviewSession).mockRejectedValue(new Error('Qodo rejected this API key; it may be expired or revoked. Create a fresh personal Qodo key, then retry.'))
    await mount()

    await setValue('#pr-url', 'https://github.com/acme/app/pull/7')
    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await settle()
    await setValue('#qodo-api-key', 'expired-key')
    host!.querySelector<HTMLButtonElement>('#load-qodo-review')!.click()
    await settle()

    const alert = host!.querySelector<HTMLElement>('.qodo-connect [role="alert"]')
    expect(alert?.textContent).toContain('may be expired or revoked')
    expect(alert?.textContent).toContain('Create a fresh personal Qodo key')
    expect(host?.querySelector<HTMLInputElement>('#qodo-api-key')?.value).toBe('')
  })
})
