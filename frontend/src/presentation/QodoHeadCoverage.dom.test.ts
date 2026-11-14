// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const headSha = 'a'.repeat(40)
const oldSha = 'b'.repeat(40)
const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Qodo review coverage', state: 'open',
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [
    { sha: oldSha, shortSha: oldSha.slice(0, 7), message: 'Earlier revision', author: 'A', committedAt: '2026-11-04T09:00:00Z', url: `https://github.com/acme/app/commit/${oldSha}` },
    { sha: headSha, shortSha: headSha.slice(0, 7), message: 'Current head', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/app/commit/${headSha}` },
  ],
  checks: { availability: 'available', revisionSha: headSha, revisionShortSha: headSha.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${headSha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [{ id: 17, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [], commitSha: oldSha, commitUrl: `https://github.com/acme/app/commit/${oldSha}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-17' }] },
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function showPullRequest(result: PullRequest = pullRequest) {
  vi.mocked(importPullRequest).mockResolvedValue(result)
  host = document.createElement('div')
  document.body.append(host)
  mounted = createApp(App)
  mounted.mount(host)
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = pullRequest.url
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.review-coverage')).not.toBeNull())
}

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('Qodo PR-head coverage status', () => {
  it('shows stale revision evidence, source links, and the next action', async () => {
    await showPullRequest()
    const status = host!.querySelector<HTMLElement>('.review-coverage')!
    expect(status.classList.contains('stale')).toBe(true)
    expect(status.textContent).toContain('PUBLISHED COMMENT · PR HEAD STALE')
    expect(status.textContent).toContain('MARKED REVISION bbbbbbb')
    expect(status.querySelector('a[href="https://github.com/acme/app/pull/7#issuecomment-17"]')?.textContent).toContain('SOURCE COMMENT')
    expect(status.textContent).toContain('Inspect the current PR head in Qodo')
    expect(status.textContent).toContain('does not establish the latest complete Qodo review')
  })

  it('labels a marked revision outside the imported PR history without calling it earlier', async () => {
    const outsideSha = 'c'.repeat(40)
    const outsidePullRequest = {
      ...pullRequest,
      qodoReviews: { availability: 'available' as const, unrecognizedCommentCount: 0, items: [{ id: 19, provider: 'Qodo' as const, sourceType: 'github_issue_comment' as const, sourceAuthor: 'qodo-code-review[bot]', kind: 'findings' as const, findings: [], commitSha: outsideSha, commitUrl: `https://github.com/acme/app/commit/${outsideSha}`, createdAt: '2026-11-04T10:30:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-19' }] },
    }
    vi.mocked(importPullRequest).mockResolvedValue(outsidePullRequest)
    host = document.createElement('div')
    document.body.append(host)
    mounted = createApp(App)
    mounted.mount(host)
    const input = host.querySelector<HTMLInputElement>('#pr-url')!
    input.value = pullRequest.url
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    host.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await vi.waitFor(() => expect(host?.querySelector('.review-coverage')).not.toBeNull())

    const status = host!.querySelector<HTMLElement>('.review-coverage')!
    expect(status.classList.contains('outside_history')).toBe(true)
    expect(status.textContent).toContain('MARKER OUTSIDE IMPORTED HISTORY')
    expect(status.textContent).not.toContain('earlier revision')
    expect(status.querySelector(`a[href="https://github.com/acme/app/commit/${outsideSha}"]`)?.textContent).toContain('MARKED REVISION')
  })

  it('does not call missing Qodo feedback a published comment', async () => {
    await showPullRequest({
      ...pullRequest,
      qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
    })

    const status = host!.querySelector<HTMLElement>('.review-coverage')!
    expect(status.textContent).toContain('QODO REVIEW COVERAGE · MISSING')
    expect(status.textContent).not.toContain('PUBLISHED COMMENT')
  })

  it('does not call unavailable Qodo feedback a published comment', async () => {
    await showPullRequest({
      ...pullRequest,
      qodoReviews: { availability: 'unavailable', reason: 'GitHub denied the request.', unrecognizedCommentCount: 0, items: [] },
    })

    const status = host!.querySelector<HTMLElement>('.review-coverage')!
    expect(status.textContent).toContain('QODO REVIEW COVERAGE · UNAVAILABLE')
    expect(status.textContent).not.toContain('PUBLISHED COMMENT')
  })
})
