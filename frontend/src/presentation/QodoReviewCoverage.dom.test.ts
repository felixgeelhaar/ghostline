// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import type { PullRequest } from '../domain/pullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const headSha = 'a'.repeat(40)
const olderSha = 'b'.repeat(40)
const headUrl = `https://github.com/acme/app/commit/${headSha}`
const reviewUrl = 'https://github.com/acme/app/pull/7#issuecomment-12'
const headCommit = { sha: headSha, shortSha: 'aaaaaaa', message: 'Head revision', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: headUrl }
const latestReview = {
  id: 12, provider: 'Qodo' as const, sourceType: 'github_issue_comment' as const,
  sourceAuthor: 'qodo-code-review[bot]', kind: 'findings' as const, findings: [],
  commitSha: olderSha, commitUrl: `https://github.com/acme/app/commit/${olderSha}`,
  createdAt: '2026-11-04T11:00:00Z', url: reviewUrl,
}

function pullRequest(commitSha = olderSha): PullRequest {
  const review = { ...latestReview, commitSha, commitUrl: `https://github.com/acme/app/commit/${commitSha}` }
  return {
    repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Coverage test', state: 'open',
    url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
    commits: [{ ...headCommit }],
    checks: { availability: 'available', revisionSha: headSha, revisionShortSha: 'aaaaaaa', revisionUrl: headUrl, items: [] },
    qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [review] },
  }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

async function load(pr: PullRequest) {
  vi.mocked(importPullRequest).mockResolvedValue(pr)
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = 'https://github.com/acme/app/pull/7'
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.review-coverage')).not.toBeNull())
}

describe('latest Qodo review head coverage', () => {
  it('shows a source-linked warning when the latest Qodo review names an earlier revision', async () => {
    await load(pullRequest())
    const status = host!.querySelector('.review-coverage [role="status"]')!
    expect(status.textContent).toContain('different revision')
    expect(status.textContent).toContain('present in the imported PR history')
    expect(host!.querySelector('.review-coverage a[href="' + reviewUrl + '"]')).not.toBeNull()
  })

  it('states that a matching revision marker is coverage evidence, not correctness proof', async () => {
    await load(pullRequest(headSha))
    const status = host!.querySelector('.review-coverage [role="status"]')!
    expect(status.textContent).toContain('names the current PR head')
    expect(status.textContent).toContain('not correctness or repair')
    expect(host!.querySelector(`.review-coverage a[href="${headUrl}"]`)).not.toBeNull()
  })

  it('warns when unparsed Qodo comments could include a newer review', async () => {
    await load({ ...pullRequest(headSha), qodoReviews: { availability: 'available', unrecognizedCommentCount: 1, items: [pullRequest(headSha).qodoReviews.items[0]!] } })
    const warning = host!.querySelectorAll('.review-coverage [role="status"]')[0]!
    expect(warning.textContent).toContain('coverage may be incomplete')
    expect(warning.textContent).toContain('one may contain a newer review')
  })
})
