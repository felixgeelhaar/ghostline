// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import { fetchReadiness } from '../application/fetchReadiness'
import type { PullRequest } from '../domain/pullRequest'
import type { Readiness } from '../domain/readiness'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn().mockResolvedValue({ authRequired: false, authenticated: true }) }))
vi.mock('../application/fetchNoxReport', () => ({ fetchNoxReport: vi.fn().mockReturnValue(new Promise(() => {})) }))
vi.mock('../application/fetchReadiness', () => ({ fetchReadiness: vi.fn() }))

const sha = 'a'.repeat(40)
const movedSha = 'd'.repeat(40)
const makePR = (number: number, head = sha): PullRequest => ({
  repository: 'acme/app', owner: 'acme', repo: 'app', number, title: `Change ${number}`, state: 'open', merged: false,
  url: `https://github.com/acme/app/pull/${number}`, createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [{ sha, shortSha: sha.slice(0, 7), message: 'Change', author: 'Felix', committedAt: '2026-11-10T10:00:00Z', url: `https://github.com/acme/app/commit/${sha}` }],
  checks: { availability: 'available', revisionSha: head, revisionShortSha: head.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${head}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] },
  referencedIssues: { availability: 'available', items: [] },
})
const verdict = (number: number, overrides: Partial<Readiness> = {}): Readiness => ({
  repository: 'acme/app', number, headSha: sha, headShortSha: sha.slice(0, 7), headUrl: `https://github.com/acme/app/commit/${sha}`,
  verdict: 'fail', findings: { high: 0, medium: 1, low: 0 },
  reasons: [
    { subject: 'branch', outcome: 'pass', message: 'The branch is up to date with main.' },
    { subject: 'qodo', outcome: 'fail', message: `Qodo reports 1 High or Medium finding on PR ${number}.`, sourceUrl: `https://github.com/acme/app/pull/${number}#issuecomment-1` },
  ],
  ...overrides,
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined
const settle = async () => { await Promise.resolve(); await nextTick(); await nextTick() }

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
}
async function importPR(number: number, title = `Change ${number}`) {
  const input = host!.querySelector<HTMLInputElement>('#pr-url')!
  input.value = `https://github.com/acme/app/pull/${number}`
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host!.querySelector<HTMLButtonElement>('.import-panel button')!.click()
  await vi.waitFor(() => expect(host!.querySelector('.change-card h2')?.textContent).toBe(title))
}
const checkButton = () => host!.querySelector<HTMLButtonElement>('.readiness button')!
const panelText = () => host!.querySelector('.readiness')?.textContent ?? ''

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
  vi.mocked(fetchReadiness).mockReset()
  vi.mocked(importPullRequest).mockReset()
})

describe('merge readiness panel', () => {
  it('shows the verdict, its head and each reason with its source', async () => {
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('FAIL'))
    expect(panelText()).toContain('aaaaaaa')
    expect(panelText()).toContain('1 MEDIUM')
    const reasons = [...host!.querySelectorAll('.readiness-reasons li')].map((item) => item.textContent ?? '')
    expect(reasons[0]).toContain('Qodo reports')
    expect(host!.querySelector<HTMLAnchorElement>('.readiness-reasons a')?.href).toBe('https://github.com/acme/app/pull/7#issuecomment-1')
  })

  it('asks for the imported pull request, not the edited import field', async () => {
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7))
    await mount()
    await importPR(7)
    const input = host!.querySelector<HTMLInputElement>('#pr-url')!
    input.value = 'https://github.com/acme/app/pull/99'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    checkButton().click()
    await settle()
    expect(fetchReadiness).toHaveBeenCalledWith('https://github.com/acme/app/pull/7', 'acme/app', 7)
  })

  it('drops a verdict for an earlier pull request that settles after a new import', async () => {
    const first = deferred<Readiness>()
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR(7)).mockResolvedValueOnce(makePR(8))
    vi.mocked(fetchReadiness).mockReturnValueOnce(first.promise)
    await mount()
    await importPR(7)
    checkButton().click()
    await settle()
    await importPR(8)
    first.resolve(verdict(7))
    await settle()
    expect(panelText()).not.toContain('PR 7')
    expect(panelText()).not.toContain('FAIL')
  })

  it('does not start a second check while one is in flight', async () => {
    const first = deferred<Readiness>()
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    vi.mocked(fetchReadiness).mockReturnValueOnce(first.promise)
    await mount()
    await importPR(7)
    checkButton().click()
    await settle()
    expect(checkButton().disabled).toBe(true)
    checkButton().click()
    await settle()
    expect(fetchReadiness).toHaveBeenCalledTimes(1)
    first.resolve(verdict(7))
    await vi.waitFor(() => expect(checkButton().disabled).toBe(false))
  })

  it('warns when the verdict describes a newer head than the imported snapshot', async () => {
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7, { headSha: movedSha, headShortSha: movedSha.slice(0, 7), headUrl: `https://github.com/acme/app/commit/${movedSha}` }))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('ddddddd'))
    expect(host!.querySelector('.readiness-head-moved')?.textContent).toContain('aaaaaaa')
  })

  it('clears a verdict for the old head when a re-import of the same PR finds a new head', async () => {
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR(7)).mockResolvedValueOnce({ ...makePR(7, movedSha), title: 'Moved' })
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('FAIL'))
    await importPR(7, 'Moved')
    await settle()
    expect(host!.querySelector('.readiness-verdict')).toBeNull()
  })

  it('drops an in-flight verdict when a re-import of the same PR finds a new head', async () => {
    const pending = deferred<Readiness>()
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR(7)).mockResolvedValueOnce({ ...makePR(7, movedSha), title: 'Moved' })
    vi.mocked(fetchReadiness).mockReturnValueOnce(pending.promise)
    await mount()
    await importPR(7)
    checkButton().click()
    await settle()
    await importPR(7, 'Moved')
    pending.resolve(verdict(7))
    await settle()
    expect(host!.querySelector('.readiness-verdict')).toBeNull()
  })

  it('keeps a verdict when a re-import of the same PR finds the same head', async () => {
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR(7)).mockResolvedValueOnce({ ...makePR(7), title: 'Same head' })
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('FAIL'))
    await importPR(7, 'Same head')
    await settle()
    expect(panelText()).toContain('FAIL')
  })

  it('clears a verdict when a same-head re-import finds the pull request closed', async () => {
    vi.mocked(importPullRequest).mockResolvedValueOnce(makePR(7)).mockResolvedValueOnce({ ...makePR(7), title: 'Closed', state: 'closed' })
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7, { verdict: 'pass', reasons: [] }))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('PASS'))
    await importPR(7, 'Closed')
    await settle()
    expect(host!.querySelector('.readiness-verdict')).toBeNull()
  })

  it('says merge readiness does not apply to a merged pull request', async () => {
    vi.mocked(importPullRequest).mockResolvedValue({ ...makePR(7), state: 'closed', merged: true })
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(7, {
      verdict: 'not-applicable', findings: undefined,
      reasons: [{ subject: 'pull-request', outcome: 'not-applicable', message: 'The pull request is already merged. Merge readiness applies only to an open pull request.' }],
    }))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('NOT APPLICABLE'))
    expect(host!.querySelector('.readiness-verdict')?.className).toContain('not-applicable')
    expect(panelText()).not.toContain('INCOMPLETE')
    expect(panelText()).not.toContain('QODO FINDINGS')
    const reasons = [...host!.querySelectorAll('.readiness-reasons li')].map((item) => item.textContent ?? '')
    expect(reasons).toHaveLength(1)
    expect(reasons[0]).toContain('already merged')
    expect(host!.querySelector('.readiness-reasons .check-state')?.className).toContain('not-applicable')
  })

  it('shows the server error instead of a verdict', async () => {
    vi.mocked(importPullRequest).mockResolvedValue(makePR(7))
    vi.mocked(fetchReadiness).mockRejectedValue(new Error('This pull request changed while its readiness was being checked. Check it again.'))
    await mount()
    await importPR(7)
    checkButton().click()
    await vi.waitFor(() => expect(panelText()).toContain('changed while'))
    expect(host!.querySelector('.readiness-verdict')).toBeNull()
  })
})
