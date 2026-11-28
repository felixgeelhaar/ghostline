// @vitest-environment happy-dom
// Issue #41 requires the published-comment experience to work when no Qodo key
// is supplied. A gate on the structured (key-backed) review once hid these cards
// while every other test still passed (PR #75), so this test pins it.
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import { fetchQodoReviewSession } from '../application/fetchQodoReviewSession'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn().mockResolvedValue({ authRequired: false, authenticated: true }) }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))
vi.mock('../application/fetchQodoReviewSession', () => ({ fetchQodoReviewSession: vi.fn() }))
// Focusing a revision also loads its Nox report and checks; keep those pending
// so the test stays offline and asserts only the review cards.
vi.mock('../application/fetchNoxReport', () => ({ fetchNoxReport: vi.fn().mockReturnValue(new Promise(() => {})) }))
vi.mock('../application/fetchReadiness', () => ({ fetchReadiness: vi.fn().mockReturnValue(new Promise(() => {})) }))

const headSha = 'a'.repeat(40)
const oldSha = 'b'.repeat(40)
const commitUrl = (sha: string) => `https://github.com/acme/app/commit/${sha}`
const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Published Qodo comments', state: 'open', merged: false,
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [
    { sha: oldSha, shortSha: oldSha.slice(0, 7), message: 'Earlier revision', author: 'A', committedAt: '2026-11-04T09:00:00Z', url: commitUrl(oldSha) },
    { sha: headSha, shortSha: headSha.slice(0, 7), message: 'Current head', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: commitUrl(headSha) },
  ],
  checks: { availability: 'available', revisionSha: headSha, revisionShortSha: headSha.slice(0, 7), revisionUrl: commitUrl(headSha), items: [] },
  qodoReviews: {
    availability: 'available',
    unrecognizedCommentCount: 0,
    items: [
      { id: 21, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [{ id: '21:1', title: 'Guard concurrent retries', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-21' }], commitSha: oldSha, commitUrl: commitUrl(oldSha), createdAt: '2026-11-04T09:30:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-21' },
      { id: 22, provider: 'Qodo', sourceType: 'github_issue_comment', sourceAuthor: 'qodo-code-review[bot]', kind: 'findings', findings: [{ id: '22:1', title: 'Keep revision links scoped', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-22' }], commitSha: headSha, commitUrl: commitUrl(headSha), createdAt: '2026-11-04T10:30:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-22' },
    ],
  },
  referencedIssues: { availability: 'available', items: [] },
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function importWithoutQodoKey() {
  vi.mocked(importPullRequest).mockResolvedValue(pullRequest)
  vi.mocked(fetchRevisionChecks).mockReturnValue(new Promise(() => {}))
  host = document.createElement('div')
  document.body.append(host)
  mounted = createApp(App)
  mounted.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = pullRequest.url
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.review-coverage')).not.toBeNull())
}

function reviewCardTitles() {
  return [...host!.querySelectorAll('.review-list .finding-heading strong')].map((title) => title.textContent)
}

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('published Qodo comments without a Qodo key', () => {
  it('renders every published review card before any revision is focused', async () => {
    await importWithoutQodoKey()

    const keyInput = host!.querySelector<HTMLInputElement>('#qodo-api-key')
    expect(keyInput).not.toBeNull()
    expect(keyInput!.value).toBe('')
    expect(host!.querySelector('.qodo-structured')).toBeNull()
    expect(reviewCardTitles()).toEqual(['Guard concurrent retries', 'Keep revision links scoped'])
    const sources = [...host!.querySelectorAll<HTMLAnchorElement>('.review-list .finding-list .review-source')]
    expect(sources.map((link) => link.getAttribute('href'))).toEqual(['https://github.com/acme/app/pull/7#issuecomment-21', 'https://github.com/acme/app/pull/7#issuecomment-22'])
    expect(sources.every((link) => link.textContent?.includes('SOURCE FINDING'))).toBe(true)
    expect(fetchQodoReviewSession).not.toHaveBeenCalled()
  })

  it('renders the card for the focused revision only', async () => {
    await importWithoutQodoKey()

    host!.querySelector<HTMLButtonElement>(`button[aria-label="Focus history on revision ${oldSha.slice(0, 7)}"]`)!.click()
    await vi.waitFor(() => expect(reviewCardTitles()).toEqual(['Guard concurrent retries']))
    expect(host!.querySelector('.review-list .review-revision a')?.getAttribute('href')).toBe(commitUrl(oldSha))
    expect(fetchQodoReviewSession).not.toHaveBeenCalled()
  })
})
