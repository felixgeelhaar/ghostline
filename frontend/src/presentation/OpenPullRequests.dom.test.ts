// @vitest-environment happy-dom
import { createApp, h, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { listOpenPullRequests } from '../application/listOpenPullRequests'
import { fetchReadiness } from '../application/fetchReadiness'
import { GhostlineApiError } from '../application/apiError'
import type { OpenPullRequest, OpenPullRequests } from '../domain/openPullRequests'
import type { Readiness } from '../domain/readiness'
import OpenPullRequestsPanel from './OpenPullRequests.vue'

vi.mock('../application/listOpenPullRequests', () => ({ listOpenPullRequests: vi.fn() }))
vi.mock('../application/fetchReadiness', () => ({ fetchReadiness: vi.fn() }))

const sha = (c: string) => c.repeat(40)
const row = (number: number, head = sha(String(number)), repo = 'acme/app'): OpenPullRequest => ({
  number, title: `Change ${number}`, draft: false, author: 'octo', headSha: head, headShortSha: head.slice(0, 7), url: `https://github.com/${repo}/pull/${number}`,
})
const list = (items: OpenPullRequest[], complete = true, repository = 'acme/app'): OpenPullRequests => ({ repository, complete, limit: 30, items })
const verdict = (number: number, headSha: string, overrides: Partial<Readiness> = {}): Readiness => ({
  repository: 'acme/app', number, headSha, headShortSha: headSha.slice(0, 7), headUrl: `https://github.com/acme/app/commit/${headSha}`,
  verdict: 'incomplete', nextAction: `waiting on Qodo review of ${headSha.slice(0, 7)}`, reasons: [], ...overrides,
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined
const opened = vi.fn()
const openedPull = ref<{ repository: string; number: number; headSha: string }>()
const settle = async () => { for (let i = 0; i < 4; i++) { await Promise.resolve(); await nextTick() } }

async function mount() {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp({ render: () => h(OpenPullRequestsPanel, { openedPull: openedPull.value, onOpen: opened }) })
  app.mount(host)
  await nextTick()
}
async function listRepository(repository: string) {
  const input = host!.querySelector<HTMLInputElement>('#open-pulls-repository')!
  input.value = repository
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host!.querySelector<HTMLFormElement>('.open-pulls form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await settle()
}
const rows = () => [...host!.querySelectorAll('.open-pulls tbody tr')].map((tr) => tr.textContent ?? '')
const checkAll = () => host!.querySelector<HTMLButtonElement>('.open-pulls-check')!

afterEach(() => {
  app?.unmount(); app = undefined
  host?.remove(); host = undefined
  openedPull.value = undefined
  vi.mocked(listOpenPullRequests).mockReset()
  vi.mocked(fetchReadiness).mockReset()
  opened.mockReset()
})

describe('open pull request list', () => {
  it('lists each open pull request with its head and says when the list is partial', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue(list([row(2), row(1)], false))
    await mount()
    await listRepository('acme/app')
    expect(listOpenPullRequests).toHaveBeenCalledWith('acme/app')
    expect(rows()).toHaveLength(2)
    expect(rows()[0]).toContain('#2')
    expect(rows()[0]).toContain('2222222')
    expect(rows()[0]).toContain('check readiness')
    expect(host!.querySelector('.open-pulls')?.textContent).toContain('PARTIAL')
  })

  it('checks each row in turn and shows its verdict and next action', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue(list([row(2), row(1)]))
    vi.mocked(fetchReadiness)
      .mockResolvedValueOnce(verdict(2, sha('2')))
      .mockResolvedValueOnce(verdict(1, sha('1'), { verdict: 'pass', nextAction: 'ready: the engineer decides whether to merge' }))
    await mount()
    await listRepository('acme/app')
    checkAll().click()
    await vi.waitFor(() => expect(rows()[1]).toContain('ready: the engineer decides whether to merge'))
    expect(fetchReadiness).toHaveBeenNthCalledWith(1, 'https://github.com/acme/app/pull/2', 'acme/app', 2)
    expect(fetchReadiness).toHaveBeenNthCalledWith(2, 'https://github.com/acme/app/pull/1', 'acme/app', 1)
    expect(rows()[0]).toContain('INCOMPLETE')
    expect(rows()[0]).toContain('waiting on Qodo review of 2222222')
    expect(rows()[1]).toContain('PASS')
  })

  it('marks a row stale when its verdict describes a newer head', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue(list([row(1)]))
    vi.mocked(fetchReadiness).mockResolvedValue(verdict(1, sha('9'), { verdict: 'pass', nextAction: 'ready: the engineer decides whether to merge' }))
    await mount()
    await listRepository('acme/app')
    checkAll().click()
    await vi.waitFor(() => expect(rows()[0]).toContain('STALE'))
    expect(rows()[0]).toContain('head moved from 1111111 to 9999999: list again')
    expect(rows()[0]).not.toContain('PASS')
  })

  it('stops checking when the rate limit is reached and says why', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue(list([row(3), row(2), row(1)]))
    vi.mocked(fetchReadiness)
      .mockResolvedValueOnce(verdict(3, sha('3')))
      .mockRejectedValueOnce(new GhostlineApiError(429, 'Too many requests. Try again in 20 seconds.'))
    await mount()
    await listRepository('acme/app')
    checkAll().click()
    await vi.waitFor(() => expect(rows()[1]).toContain('Too many requests. Try again in 20 seconds.'))
    await settle()
    expect(fetchReadiness).toHaveBeenCalledTimes(2)
    expect(rows()[2]).toContain('check readiness')
  })

  it('drops verdicts for an earlier list that settle after a new list', async () => {
    const pending = deferred<Readiness>()
    vi.mocked(listOpenPullRequests).mockResolvedValueOnce(list([row(1)])).mockResolvedValueOnce(list([row(1, sha('5'), 'acme/web')], true, 'acme/web'))
    vi.mocked(fetchReadiness).mockReturnValueOnce(pending.promise)
    await mount()
    await listRepository('acme/app')
    checkAll().click()
    await settle()
    await listRepository('acme/web')
    pending.resolve(verdict(1, sha('1'), { verdict: 'pass', nextAction: 'ready: the engineer decides whether to merge' }))
    await settle()
    expect(rows()).toHaveLength(1)
    expect(rows()[0]).toContain('5555555')
    expect(rows()[0]).toContain('check readiness')
    expect(rows()[0]).not.toContain('PASS')
  })

  it('opens the pull request of the clicked row, with the head it was listed with', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue(list([row(2), row(1)]))
    await mount()
    await listRepository('acme/app')
    const input = host!.querySelector<HTMLInputElement>('#open-pulls-repository')!
    input.value = 'someone/else'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    const open = host!.querySelectorAll<HTMLButtonElement>('.open-pulls tbody button')[1]
    expect(open.getAttribute('aria-label')).toBe('Open acme/app#1: Change 1')
    open.click()
    expect(opened).toHaveBeenCalledWith({ repository: 'acme/app', row: row(1) })
  })

  it('marks a row stale when opening it found a newer head', async () => {
    vi.mocked(listOpenPullRequests).mockResolvedValue(list([row(1)]))
    await mount()
    await listRepository('acme/app')
    openedPull.value = { repository: 'acme/app', number: 1, headSha: sha('9') }
    await settle()
    expect(rows()[0]).toContain('STALE')
    expect(rows()[0]).toContain('head moved from 1111111 to 9999999: list again')
  })

  it('shows the server error instead of a list', async () => {
    vi.mocked(listOpenPullRequests).mockRejectedValue(new Error('Enter a public GitHub repository as owner/repo or https://github.com/owner/repo.'))
    await mount()
    await listRepository('nope')
    expect(host!.querySelector('.open-pulls [role="alert"]')?.textContent).toContain('Enter a public GitHub repository')
    expect(host!.querySelector('.open-pulls table')).toBeNull()
  })
})
