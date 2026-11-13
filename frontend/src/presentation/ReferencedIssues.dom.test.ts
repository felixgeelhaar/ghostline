// @vitest-environment happy-dom
import { createApp } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))

const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7,
  title: 'Add payment idempotency', state: 'closed', merged: true,
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-01T08:00:00Z', updatedAt: '2026-11-02T08:00:00Z',
  commits: [],
  checks: { availability: 'available', revisionSha: '', revisionShortSha: '', revisionUrl: '', items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [{ number: 31, url: 'https://github.com/acme/app/issues/31', state: 'open' }] },
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  window.history.replaceState({}, '', '/')
  vi.clearAllMocks()
})

async function importPR(data: PullRequest) {
  vi.mocked(importPullRequest).mockResolvedValue(data)
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  host.querySelector<HTMLInputElement>('#pr-url')!.value = data.url
  host.querySelector<HTMLInputElement>('#pr-url')!.dispatchEvent(new Event('input', { bubbles: true }))
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.change-card')).not.toBeNull())
}

describe('referenced issues', () => {
  it('warns that an issue remains open after a PR is merged and links to both sources', async () => {
    await importPR(pullRequest)

    const warning = host!.querySelector('.referenced-issue-warning')!
    expect(warning.textContent).toContain('Referenced issue #31 is still open')
    expect(warning.querySelector('a[href="https://github.com/acme/app/pull/7"]')).not.toBeNull()
    expect(warning.querySelector('a[href="https://github.com/acme/app/issues/31"]')).not.toBeNull()
    expect(warning.textContent).toContain('Ghostline does not decide whether the work is delivered.')
  })

  it('does not call an open issue delivered when the pull request has not merged', async () => {
    await importPR({ ...pullRequest, state: 'open', merged: false })
    expect(host!.querySelector('.referenced-issue-warning')).toBeNull()
    expect(host!.querySelector('.referenced-issue-row a')?.textContent).toContain('ISSUE #31')
    expect(host!.querySelector('.referenced-issue-row span')?.textContent).toBe('OPEN')
  })

  it('shows lookup gaps instead of hiding referenced issues', async () => {
    await importPR({
      ...pullRequest,
      referencedIssues: {
        availability: 'unavailable',
        reason: 'GitHub could not confirm the state of every referenced issue.',
        items: [{ number: 31, url: 'https://github.com/acme/app/issues/31', state: 'unavailable' }],
      },
    })
    expect(host!.querySelector('.referenced-issues')?.textContent).toContain('could not confirm')
    expect(host!.querySelector('.referenced-issues')?.textContent).toContain('ISSUE STATE UNAVAILABLE')
  })
})
