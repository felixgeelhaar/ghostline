// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const shaA = 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678'
const shaB = 'b'.repeat(40)
const commitA = { sha: shaA, shortSha: 'a1b2c3d', message: 'First', author: 'A', committedAt: '2026-11-04T09:00:00Z', url: `https://github.com/acme/app/commit/${shaA}` }
const commitB = { sha: shaB, shortSha: 'bbbbbbb', message: 'Second', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/app/commit/${shaB}` }
const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Copy SHA', state: 'open', merged: false,
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [commitA, commitB],
  checks: { availability: 'available', revisionSha: shaB, revisionShortSha: 'bbbbbbb', revisionUrl: commitB.url, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: [],
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function importFixture() {
  vi.mocked(importPullRequest).mockResolvedValue(pullRequest)
  host = document.createElement('div')
  document.body.append(host)
  mounted = createApp(App)
  mounted.mount(host)
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = 'https://github.com/acme/app/pull/7'
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelectorAll('.timeline li')).toHaveLength(2))
}

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('copy full commit SHA from the revision timeline', () => {
  it('copies the full SHA and announces success without changing link or focus behavior', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    await importFixture()

    const button = host!.querySelector<HTMLButtonElement>(`[aria-label="Copy full SHA for revision a1b2c3d"]`)!
    expect(button).not.toBeNull()
    button.click()
    await vi.waitFor(() => expect(host!.querySelector('.copy-status')?.textContent).toContain('Copied full SHA for revision a1b2c3d.'))
    expect(writeText).toHaveBeenCalledWith(shaA)
    expect(host!.querySelector('.copy-status')?.getAttribute('role')).toBe('status')

    const link = host!.querySelector<HTMLAnchorElement>('.timeline li:first-child .commit-meta a')!
    expect(link.getAttribute('href')).toBe(commitA.url)
    expect(host!.querySelectorAll('.focus-revision')).toHaveLength(2)
  })

  it('shows a useful message when clipboard access is unavailable', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    await importFixture()

    host!.querySelector<HTMLButtonElement>(`[aria-label="Copy full SHA for revision bbbbbbb"]`)!.click()
    await vi.waitFor(() => expect(host!.querySelector('.copy-status')?.textContent).toContain('Clipboard access is unavailable'))
    expect(host!.querySelector('.copy-status')?.textContent).toContain(shaB)
    expect(host!.querySelector('.copy-status')?.classList.contains('warn')).toBe(true)
  })

  it('shows a useful message when the clipboard write is denied', async () => {
    const writeText = vi.fn().mockRejectedValue(new DOMException('denied', 'NotAllowedError'))
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    await importFixture()

    host!.querySelector<HTMLButtonElement>(`[aria-label="Copy full SHA for revision a1b2c3d"]`)!.click()
    await vi.waitFor(() => expect(host!.querySelector('.copy-status')?.textContent).toContain('Clipboard access was denied'))
    expect(host!.querySelector('.copy-status')?.textContent).toContain(shaA)
  })

  it('announces only the most recent copy when an earlier write finishes last', async () => {
    let finishFirst: () => void = () => {}
    const writeText = vi.fn()
      .mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirst = resolve }))
      .mockResolvedValueOnce(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    await importFixture()

    host!.querySelector<HTMLButtonElement>(`[aria-label="Copy full SHA for revision a1b2c3d"]`)!.click()
    host!.querySelector<HTMLButtonElement>(`[aria-label="Copy full SHA for revision bbbbbbb"]`)!.click()
    await vi.waitFor(() => expect(host!.querySelector('.copy-status')?.textContent).toContain('Copied full SHA for revision bbbbbbb.'))
    finishFirst()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
    expect(host!.querySelector('.copy-status')?.textContent).toContain('Copied full SHA for revision bbbbbbb.')
    expect(host!.querySelector('.copy-status')?.textContent).not.toContain('a1b2c3d')
  })
})
