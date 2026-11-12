// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import type { PullRequest } from '../domain/pullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const basePR: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 12, title: 'Merged change', state: 'closed', merged: true,
  url: 'https://github.com/acme/app/pull/12', createdAt: '2026-11-12T08:00:00Z', updatedAt: '2026-11-12T09:00:00Z',
  commits: [],
  checks: { availability: 'available', revisionSha: 'a'.repeat(40), revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/app/commit/${'a'.repeat(40)}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: [],
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function importPR(pr: PullRequest) {
  vi.mocked(importPullRequest).mockResolvedValue(pr)
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = pr.url
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }))
  await vi.waitFor(() => expect(host?.querySelector('#referenced-issues-title')).not.toBeNull())
}

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('referenced issue status', () => {
  const availableOpen = { number: 27, title: 'Idempotency tracking', availability: 'available' as const, state: 'open' as const, url: 'https://github.com/acme/app/issues/27' }

  it('flags an open issue on a merged PR and links both GitHub records', async () => {
    await importPR({ ...basePR, referencedIssues: [availableOpen] })

    const row = host?.querySelector('.referenced-issues li')
    expect(row?.textContent).toContain('Referenced issue #27 is still open.')
    expect(row?.querySelector('a[href="https://github.com/acme/app/pull/12"]')?.textContent).toContain('PR #12')
    expect(row?.querySelector('a[href="https://github.com/acme/app/issues/27"]')?.textContent).toContain('Idempotency tracking')
    expect(host?.textContent).toContain('does not decide whether the work was delivered')
    expect(host?.textContent).toContain('Issue state reflects GitHub at import time.')
  })

  it('shows a closed reference without the open warning', async () => {
    await importPR({ ...basePR, referencedIssues: [{ ...availableOpen, state: 'closed' }] })

    expect(host?.querySelector('.referenced-issues li')?.textContent).toContain('Referenced issue #27 is closed.')
    expect(host?.textContent).not.toContain('is still open')
  })

  it('does not call an open reference still open when the PR is not merged', async () => {
    await importPR({ ...basePR, merged: false, state: 'open', referencedIssues: [availableOpen] })

    expect(host?.querySelector('.referenced-issues li')?.textContent).toContain('this pull request is not merged')
    expect(host?.textContent).not.toContain('is still open')
  })

  it('shows failed lookups as unavailable and keeps the issue source link', async () => {
    await importPR({ ...basePR, referencedIssues: [{ number: 27, title: 'Issue #27', availability: 'unavailable', reason: 'GitHub denied the lookup.', url: availableOpen.url }] })

    const row = host?.querySelector('.referenced-issues li')
    expect(row?.textContent).toContain('Referenced issue #27 is unavailable.')
    expect(row?.textContent).toContain('GitHub denied the lookup.')
    expect(row?.querySelector('a[href="https://github.com/acme/app/issues/27"]')).not.toBeNull()
    expect(row?.textContent).not.toContain('is still open')
  })
})
