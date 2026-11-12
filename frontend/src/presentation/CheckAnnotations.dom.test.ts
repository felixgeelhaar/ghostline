// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { fetchCheckAnnotations } from '../application/fetchCheckAnnotations'
import type { PullRequest } from '../domain/pullRequest'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))
vi.mock('../application/fetchCheckAnnotations', () => ({ fetchCheckAnnotations: vi.fn() }))

const head = 'b'.repeat(40)
const older = 'a'.repeat(40)
const run = (id: number) => ({ id, name: 'Nox security scan', kind: 'check_run' as const, status: 'completed', conclusion: 'failure', url: `https://github.com/acme/app/actions/runs/${id}` })
const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Payments', state: 'open',
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [
    { sha: older, shortSha: 'aaaaaaa', message: 'Older revision', author: 'Felix', committedAt: '2026-11-04T09:00:00Z', url: `https://github.com/acme/app/commit/${older}` },
    { sha: head, shortSha: 'bbbbbbb', message: 'Current revision', author: 'Felix', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/app/commit/${head}` },
  ],
  checks: { availability: 'available', revisionSha: head, revisionShortSha: 'bbbbbbb', revisionUrl: `https://github.com/acme/app/commit/${head}`, items: [run(314)] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
}
const page = (sha: string, checkRunId: number, message: string, details?: string, pageNumber = 1, hasMore = false) => ({
  revisionSha: sha, checkRunId, checkRunUrl: `https://github.com/acme/app/actions/runs/${checkRunId}`, page: pageNumber, pageSize: 100, hasMore,
  items: [{ path: 'backend/payments.go', startLine: 12, endLine: 14, level: 'warning', title: 'Idempotency', message, ...(details ? { details } : {}), sourceUrl: `https://github.com/acme/app/blob/${sha}/backend/payments.go#L12-L14` }],
})

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined
async function settle() { await Promise.resolve(); await nextTick(); await nextTick() }
async function mountImportedPR() {
  vi.mocked(importPullRequest).mockResolvedValue(pullRequest)
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  const input = host.querySelector<HTMLInputElement>('#pr-url')!
  input.value = pullRequest.url
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.check-list')).not.toBeNull())
}

afterEach(() => {
  app?.unmount(); app = undefined
  host?.remove(); host = undefined
  vi.resetAllMocks()
})

describe('GitHub check annotations', () => {
  it('loads annotations from the imported PR and links each finding to its exact source revision', async () => {
    vi.mocked(fetchCheckAnnotations).mockResolvedValue(page(head, 314, 'Review the transaction guard.'))
    await mountImportedPR()

    host!.querySelector<HTMLButtonElement>('.annotation-toggle')!.click()
    await vi.waitFor(() => expect(host?.querySelector('.annotation-list')).not.toBeNull())

    expect(fetchCheckAnnotations).toHaveBeenCalledWith(pullRequest.url, head, 314, 1)
    expect(host!.querySelector('.annotation-boundary')?.textContent).toContain('revision bbbbbbb')
    expect(host!.querySelector('.annotation-list')?.textContent).toContain('backend/payments.go:12–14')
    expect(host!.querySelector<HTMLAnchorElement>('.annotation-list a')?.href).toBe(`https://github.com/acme/app/blob/${head}/backend/payments.go#L12-L14`)
    expect(host!.textContent).toContain('GitHub check annotations')
    expect(host!.textContent).not.toContain('Ghostline security verdict')
  })

  it('shows raw annotation details as text when GitHub supplies them', async () => {
    vi.mocked(fetchCheckAnnotations).mockResolvedValue(page(head, 314, 'Review the transaction guard.', 'raw_details: lock is acquired before commit'))
    await mountImportedPR()

    host!.querySelector<HTMLButtonElement>('.annotation-toggle')!.click()
    await vi.waitFor(() => expect(host?.querySelector('.annotation-list')).not.toBeNull())

    expect(host!.querySelector('.annotation-details')?.textContent).toContain('raw_details: lock is acquired before commit')
  })

  it('renders annotations when GitHub omits the check-run link', async () => {
    const { checkRunUrl: _omitted, ...withoutRunUrl } = page(head, 314, 'Finding without a run link.')
    vi.mocked(fetchCheckAnnotations).mockResolvedValue(withoutRunUrl)
    await mountImportedPR()

    host!.querySelector<HTMLButtonElement>('.annotation-toggle')!.click()
    await vi.waitFor(() => expect(host?.querySelector('.annotation-list')).not.toBeNull())

    expect(host!.textContent).toContain('Finding without a run link.')
    expect(host!.textContent).not.toContain('Unable to load annotations')
  })

  it('uses cached annotation pages when navigating back after they were loaded', async () => {
    vi.mocked(fetchCheckAnnotations)
      .mockResolvedValueOnce(page(head, 314, 'First-page annotation.', undefined, 1, true))
      .mockResolvedValueOnce(page(head, 314, 'Second-page annotation.', undefined, 2, false))
      .mockRejectedValue(new Error('Temporary GitHub error'))
    await mountImportedPR()

    host!.querySelector<HTMLButtonElement>('.annotation-toggle')!.click()
    await vi.waitFor(() => expect(host?.textContent).toContain('First-page annotation.'))
    host!.querySelector<HTMLButtonElement>('.annotation-pagination button')!.click()
    await vi.waitFor(() => expect(host?.textContent).toContain('Second-page annotation.'))
    host!.querySelector<HTMLButtonElement>('.annotation-pagination button')!.click()
    await settle()

    expect(fetchCheckAnnotations).toHaveBeenCalledTimes(2)
    expect(host!.textContent).toContain('First-page annotation.')
    expect(host!.textContent).not.toContain('Temporary GitHub error')
  })

  it('discards an older annotation response after the selected revision changes', async () => {
    let resolveHead!: (value: ReturnType<typeof page>) => void
    vi.mocked(fetchCheckAnnotations)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveHead = resolve }))
      .mockResolvedValueOnce(page(older, 271, 'Finding on the focused revision.'))
    vi.mocked(fetchRevisionChecks).mockResolvedValue({
      availability: 'available', revisionSha: older, revisionShortSha: 'aaaaaaa', revisionUrl: `https://github.com/acme/app/commit/${older}`, items: [run(271)],
    })
    await mountImportedPR()

    host!.querySelector<HTMLButtonElement>('.annotation-toggle')!.click()
    await vi.waitFor(() => expect(fetchCheckAnnotations).toHaveBeenCalledTimes(1))
    host!.querySelector<HTMLButtonElement>('[aria-label="Focus history on revision aaaaaaa"]')!.click()
    await vi.waitFor(() => expect(fetchRevisionChecks).toHaveBeenCalledWith(pullRequest.url, older))
    await vi.waitFor(() => expect(host?.querySelector<HTMLButtonElement>('.annotation-toggle')).not.toBeNull())
    host!.querySelector<HTMLButtonElement>('.annotation-toggle')!.click()
    await vi.waitFor(() => expect(host?.textContent).toContain('Finding on the focused revision.'))

    resolveHead(page(head, 314, 'Stale finding from the previous revision.'))
    await settle()
    expect(host!.textContent).toContain('Finding on the focused revision.')
    expect(host!.textContent).not.toContain('Stale finding from the previous revision.')
    expect(fetchCheckAnnotations).toHaveBeenNthCalledWith(1, pullRequest.url, head, 314, 1)
    expect(fetchCheckAnnotations).toHaveBeenNthCalledWith(2, pullRequest.url, older, 271, 1)
  })
})
