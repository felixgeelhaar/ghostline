// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const sha = 'a'.repeat(40)
const commit = { sha, shortSha: 'aaaaaaa', message: 'Add retry guard', author: 'Felix', committedAt: '2026-11-04T09:00:00Z', url: `https://github.com/acme/app/commit/${sha}` }
const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Retry guard', state: 'open',
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [commit],
  checks: { availability: 'available', revisionSha: sha, revisionShortSha: 'aaaaaaa', revisionUrl: commit.url, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [{
    id: 21, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings',
    summary: 'Review notes.', findings: [{ id: '21:1', title: 'Guard concurrent retries', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-21' }],
    commitSha: sha, commitUrl: commit.url, createdAt: '2026-11-04T09:05:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-21',
  }] },
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('change activity index', () => {
  it('shows commit and Qodo evidence in source-time order with explicit time semantics', async () => {
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
    await vi.waitFor(() => expect(host?.querySelectorAll('.activity-list li')).toHaveLength(2))

    const entries = [...host.querySelectorAll('.activity-list li')]
    expect(entries[0]?.textContent).toContain('COMMIT METADATA TIME')
    expect(entries[0]?.textContent).toContain('Add retry guard')
    expect(entries[1]?.textContent).toContain('QODO COMMENT TIME')
    expect(entries[1]?.textContent).toContain('Guard concurrent retries')
    expect(entries[1]?.querySelector('a')?.getAttribute('href')).toBe(pullRequest.qodoReviews.items[0]?.url)
    expect(host.querySelector('.activity-index .footnote')?.textContent).toContain('sequence alone does not establish causation')
    expect(host.querySelector('[aria-label="Focus activity revision aaaaaaa"]')).not.toBeNull()
  })
})
