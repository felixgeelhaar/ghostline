// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn().mockResolvedValue({ authRequired: false, authenticated: true }) }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

// A pull request from a repository without Qodo or CI (#128).
const headSha = 'a'.repeat(40)
const prURL = 'https://github.com/acme/plain/pull/3'
const bare: PullRequest = {
  repository: 'acme/plain', owner: 'acme', repo: 'plain', number: 3, title: 'Plain change', state: 'open', merged: false,
  url: prURL, createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [{ sha: headSha, shortSha: 'aaaaaaa', message: 'Only commit', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/plain/commit/${headSha}` }],
  checks: { availability: 'available', revisionSha: headSha, revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/plain/commit/${headSha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [] },
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function show(result: PullRequest) {
  vi.mocked(importPullRequest).mockResolvedValue(result)
  host = document.createElement('div')
  document.body.append(host)
  mounted = createApp(App)
  mounted.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = prURL
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.qodo-reviews')).not.toBeNull())
}

const reviewsArea = () => host!.querySelector<HTMLElement>('.qodo-reviews')!
const checksArea = () => host!.querySelector<HTMLElement>('.checks')!

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('a pull request without Qodo review or checks', () => {
  it('says no Qodo review was found and links the pull request it checked', async () => {
    await show(bare)
    const empty = reviewsArea().querySelector<HTMLElement>('.no-evidence')!
    expect(empty.textContent).toContain('No Qodo review found on this PR')
    const source = empty.querySelector<HTMLAnchorElement>(`a[href="${prURL}"]`)!
    expect(source.textContent).toContain('acme/plain#3')
  })

  it('names the revision it checked when no checks were reported', async () => {
    await show(bare)
    const empty = checksArea().querySelector<HTMLElement>('.no-evidence')!
    expect(empty.textContent).toContain('No checks reported for aaaaaaa')
    expect(empty.querySelector(`a[href="https://github.com/acme/plain/commit/${headSha}"]`)).not.toBeNull()
  })

  it('says only what was checked: no verdict on whether the change is safe', async () => {
    await show(bare)
    const text = `${reviewsArea().textContent} ${checksArea().textContent}`.toLowerCase()
    for (const verdict of ['safe', 'passing', 'passed', 'clean', 'no issues', 'no problems']) expect(text).not.toContain(verdict)
  })

  it('shows a GitHub failure loading reviews as an error, not as no review', async () => {
    await show({ ...bare, qodoReviews: { availability: 'unavailable', reason: 'Qodo review comments could not be loaded from GitHub.', unrecognizedCommentCount: 0, items: [] } })
    expect(reviewsArea().textContent).toContain('Qodo review unavailable.')
    expect(reviewsArea().textContent).not.toContain('No Qodo review found')
    expect(reviewsArea().querySelector('.no-evidence')).toBeNull()
  })

  it('shows a GitHub rate limit on checks as an error, not as no checks', async () => {
    await show({ ...bare, checks: { ...bare.checks, availability: 'unavailable', reason: 'GitHub rate limit reached; try again later.' } })
    expect(checksArea().textContent).toContain('Checks unavailable.')
    expect(checksArea().textContent).toContain('GitHub rate limit reached')
    expect(checksArea().textContent).not.toContain('No checks reported')
    expect(checksArea().querySelector('.no-evidence')).toBeNull()
  })
})
