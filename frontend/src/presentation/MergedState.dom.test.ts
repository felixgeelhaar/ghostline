// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn().mockResolvedValue({ authRequired: false, authenticated: true }) }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

// GitHub reports a merged pull request as state "closed" with merged: true.
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
  await vi.waitFor(() => expect(host?.querySelector('.change-heading .state')).not.toBeNull())
}

const badge = () => host!.querySelector<HTMLElement>('.change-heading .state')!

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('the imported pull request state badge', () => {
  it('says MERGED for a merged pull request, not CLOSED', async () => {
    await show({ ...bare, state: 'closed', merged: true })
    expect(badge().textContent?.trim()).toBe('merged')
    expect(badge().classList.contains('merged')).toBe(true)
    expect(badge().classList.contains('closed')).toBe(false)
  })

  it('says CLOSED for a pull request closed without merging', async () => {
    await show({ ...bare, state: 'closed', merged: false })
    expect(badge().textContent?.trim()).toBe('closed')
    expect(badge().classList.contains('closed')).toBe(true)
  })

  it('says OPEN for an open pull request', async () => {
    await show(bare)
    expect(badge().textContent?.trim()).toBe('open')
  })
})
