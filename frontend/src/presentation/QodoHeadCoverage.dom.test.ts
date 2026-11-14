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
  commits: [{ sha: headSha, shortSha: headSha.slice(0, 7), message: 'Current head', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/app/commit/${headSha}` }],
  checks: { availability: 'available', revisionSha: headSha, revisionShortSha: headSha.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${headSha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [{ id: 17, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [], commitSha: oldSha, commitUrl: `https://github.com/acme/app/commit/${oldSha}`, createdAt: '2026-11-04T09:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-17' }] },
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function showPullRequest() {
  vi.mocked(importPullRequest).mockResolvedValue(pullRequest)
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
  })
})
