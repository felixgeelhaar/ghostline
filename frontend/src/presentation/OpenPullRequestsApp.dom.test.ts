// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import { listOpenPullRequests } from '../application/listOpenPullRequests'
import type { PullRequest } from '../domain/pullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/listOpenPullRequests', () => ({ listOpenPullRequests: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn().mockResolvedValue({ authRequired: false, authenticated: true }) }))
vi.mock('../application/fetchNoxReport', () => ({ fetchNoxReport: vi.fn().mockReturnValue(new Promise(() => {})) }))

const listed = 'a'.repeat(40)
const moved = 'd'.repeat(40)
const pull = (head: string): PullRequest => ({
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Change 7', state: 'open', merged: false,
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [{ sha: head, shortSha: head.slice(0, 7), message: 'Change', author: 'Felix', committedAt: '2026-11-10T10:00:00Z', url: `https://github.com/acme/app/commit/${head}` }],
  checks: { availability: 'available', revisionSha: head, revisionShortSha: head.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${head}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [] },
})

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined
afterEach(() => { app?.unmount(); host?.remove(); vi.clearAllMocks() })

describe('opening a listed pull request', () => {
  it('imports the clicked row and marks it stale when the import finds a newer head', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue({ repository: 'acme/app', complete: true, limit: 30, items: [{ number: 7, title: 'Change 7', draft: false, author: 'octo', headSha: listed, headShortSha: 'aaaaaaa', url: 'https://github.com/acme/app/pull/7' }] })
    vi.mocked(importPullRequest).mockResolvedValue(pull(moved))
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(App)
    app.mount(host)
    await vi.waitFor(() => expect(host!.querySelector('#open-pulls-repository')).not.toBeNull())
    const input = host.querySelector<HTMLInputElement>('#open-pulls-repository')!
    input.value = 'acme/app'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    host.querySelector<HTMLFormElement>('.open-pulls form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(host!.querySelector('.open-pulls tbody button')).not.toBeNull())
    host.querySelector<HTMLButtonElement>('.open-pulls tbody button')!.click()
    await vi.waitFor(() => expect(host!.querySelector('.change-card h2')?.textContent).toBe('Change 7'))
    expect(importPullRequest).toHaveBeenCalledWith('https://github.com/acme/app/pull/7')
    expect(host.querySelector<HTMLInputElement>('#pr-url')?.value).toBe('acme/app#7')
    await vi.waitFor(() => expect(host!.querySelector('.open-pulls tbody tr')?.textContent).toContain('STALE'))
    expect(host.querySelector('.open-pulls tbody tr')?.textContent).toContain('head moved from aaaaaaa to ddddddd: list again')
  })
})
