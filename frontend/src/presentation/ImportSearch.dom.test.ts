// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { importPullRequest } from '../application/importPullRequest'
import { searchPullRequests } from '../application/searchPullRequests'
import { fetchGhostlineSession } from '../application/fetchGhostlineSession'
import { GhostlineApiError } from '../application/apiError'
import type { PullRequestSearchResult, PullRequestSuggestion } from '../domain/pullRequestSearch'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn().mockReturnValue(new Promise(() => {})) }))
vi.mock('../application/searchPullRequests', () => ({ searchPullRequests: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const suggestion = (repo: string, number: number, title: string, state: PullRequestSuggestion['state'] = 'open'): PullRequestSuggestion => ({
  repository: `acme/${repo}`, owner: 'acme', repo, number, title, state, updatedAt: '2026-11-20T10:00:00Z', url: `https://github.com/acme/${repo}/pull/${number}`,
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

const input = () => host!.querySelector<HTMLInputElement>('#pr-url')!
const options = () => [...host!.querySelectorAll<HTMLLIElement>('[role="option"]')]
const status = () => host!.querySelector('[role="status"][aria-live="polite"]')?.textContent ?? ''

async function settle() {
  await Promise.resolve()
  await nextTick()
  await Promise.resolve()
  await nextTick()
}

async function mount(signedIn: boolean) {
  vi.mocked(fetchGhostlineSession).mockResolvedValue(signedIn ? { authRequired: true, authenticated: true, login: 'octo' } : { authRequired: false, authenticated: true })
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
}

async function type(value: string) {
  input().value = value
  input().dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
}

async function key(name: string) {
  input().dispatchEvent(new KeyboardEvent('keydown', { key: name, bubbles: true, cancelable: true }))
  await settle()
}

async function typeAndWait(value: string) {
  await type(value)
  vi.advanceTimersByTime(300)
  await settle()
}

afterEach(() => {
  vi.useRealTimers()
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('searching your pull requests from the import field', () => {
  it('lists nothing until the user types, then searches once typing pauses', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets', 'draft'), suggestion('gears', 3, 'Fix gears', 'merged')], partial: false })
    await mount(true)
    expect(options()).toHaveLength(0)
    expect(input().getAttribute('aria-expanded')).toBe('false')

    await type('widgets')
    vi.advanceTimersByTime(299)
    await settle()
    expect(searchPullRequests).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    await settle()

    expect(searchPullRequests).toHaveBeenCalledTimes(1)
    expect(searchPullRequests).toHaveBeenCalledWith('widgets')
    expect(input().getAttribute('aria-expanded')).toBe('true')
    expect(options().map((option) => option.textContent)).toEqual([
      expect.stringContaining('acme/widgets #7'),
      expect.stringContaining('acme/gears #3'),
    ])
    expect(options()[0]!.textContent).toContain('DRAFT')
    expect(options()[0]!.textContent).toContain('UPDATED')
    expect(options()[1]!.textContent).toContain('MERGED')
    expect(status()).toBe('2 pull requests found')
  })

  it('does not search for fewer than three characters or for a URL', async () => {
    await mount(true)
    await typeAndWait('ab')
    await typeAndWait('https://github.com/acme/app/pull/7')
    await typeAndWait('acme/app#7')
    expect(searchPullRequests).not.toHaveBeenCalled()
  })

  it('imports the suggestion chosen with the arrow keys, not the typed text', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets'), suggestion('gears', 3, 'Fix gears')], partial: false })
    await mount(true)
    await typeAndWait('acme')

    await key('ArrowDown')
    await key('ArrowDown')
    expect(input().getAttribute('aria-activedescendant')).toBe(options()[1]!.id)
    expect(options()[1]!.getAttribute('aria-selected')).toBe('true')
    await key('Enter')

    expect(importPullRequest).toHaveBeenCalledTimes(1)
    expect(importPullRequest).toHaveBeenCalledWith('https://github.com/acme/gears/pull/3')
    expect(input().value).toBe('https://github.com/acme/gears/pull/3')
    expect(input().getAttribute('aria-expanded')).toBe('false')
  })

  it('imports a clicked suggestion by its own URL', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets')], partial: false })
    await mount(true)
    await typeAndWait('widgets')
    options()[0]!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await settle()
    expect(importPullRequest).toHaveBeenCalledWith('https://github.com/acme/widgets/pull/7')
  })

  it('discards an earlier search that settles after the latest one', async () => {
    const earlier = deferred<PullRequestSearchResult>()
    vi.mocked(searchPullRequests)
      .mockReturnValueOnce(earlier.promise)
      .mockResolvedValueOnce({ items: [suggestion('widgets', 7, 'Latest match')], partial: false })
    await mount(true)

    await typeAndWait('widg')
    await typeAndWait('widgets')
    expect(options().map((option) => option.textContent)).toEqual([expect.stringContaining('Latest match')])

    earlier.resolve({ items: [suggestion('stale', 1, 'Stale match')], partial: false })
    await settle()

    expect(host!.textContent).not.toContain('Stale match')
    expect(options().map((option) => option.textContent)).toEqual([expect.stringContaining('Latest match')])
  })

  it('says when nothing matches', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [], partial: false })
    await mount(true)
    await typeAndWait('nothing here')
    expect(host!.textContent).toContain('No matching pull requests — paste a PR URL')
    expect(status()).toBe('No matching pull requests — paste a PR URL')
  })

  it('labels a partial result', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets')], partial: true })
    await mount(true)
    await typeAndWait('widgets')
    expect(host!.textContent).toContain('GitHub returned a partial result')
  })

  it('shows a rate limit as an error, not as an empty result', async () => {
    vi.mocked(searchPullRequests).mockRejectedValue(new GhostlineApiError(429, 'Too many requests. Try again in 12 seconds.'))
    await mount(true)
    await typeAndWait('widgets')
    expect(host!.querySelector('.pr-search-popup [role="alert"]')?.textContent).toContain('Try again in 12 seconds')
    expect(host!.textContent).not.toContain('No matching pull requests')
  })

  it('closes the list on Escape and ignores a search still running', async () => {
    const pending = deferred<PullRequestSearchResult>()
    vi.mocked(searchPullRequests).mockReturnValueOnce(pending.promise)
    await mount(true)
    await typeAndWait('widgets')
    await key('Escape')
    pending.resolve({ items: [suggestion('widgets', 7, 'Late match')], partial: false })
    await settle()
    expect(input().getAttribute('aria-expanded')).toBe('false')
    expect(host!.textContent).not.toContain('Late match')
  })

  it('keeps the results open when Enter is pressed on search text with nothing highlighted', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets')], partial: false })
    await mount(true)
    await typeAndWait('widgets')
    await key('Enter')
    expect(importPullRequest).not.toHaveBeenCalled()
    expect(input().getAttribute('aria-expanded')).toBe('true')
    expect(options()).toHaveLength(1)
  })

  it('searches straight away when Enter is pressed before typing pauses', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets')], partial: false })
    await mount(true)
    await type('widgets')
    await key('Enter')
    expect(searchPullRequests).toHaveBeenCalledTimes(1)
    expect(options()).toHaveLength(1)
    vi.advanceTimersByTime(300)
    await settle()
    expect(searchPullRequests).toHaveBeenCalledTimes(1)
  })

  it('does not search after focus leaves the field', async () => {
    await mount(true)
    await type('widgets')
    input().dispatchEvent(new FocusEvent('blur'))
    vi.advanceTimersByTime(300)
    await settle()
    expect(searchPullRequests).not.toHaveBeenCalled()
    expect(input().getAttribute('aria-expanded')).toBe('false')
  })

  it('returns to sign-in when a search reports an expired session', async () => {
    vi.mocked(searchPullRequests).mockRejectedValue(new GhostlineApiError(401, 'Your Ghostline session expired. Sign in with GitHub again.', 'AUTH_REQUIRED'))
    await mount(true)
    vi.mocked(fetchGhostlineSession).mockResolvedValue({ authRequired: true, authenticated: false })
    await typeAndWait('widgets')
    vi.useRealTimers()
    await vi.waitFor(() => expect(host?.querySelector('#pr-url')).toBeNull())
    expect(host!.textContent).toContain('Sign in to your control room.')
  })

  it('hides earlier results as soon as the search text changes', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets')], partial: false })
    await mount(true)
    await typeAndWait('widgets')
    expect(options()).toHaveLength(1)

    await type('gears')
    expect(options()).toHaveLength(0)
    expect(input().getAttribute('aria-expanded')).toBe('false')
  })

  it('stops a search that is still waiting for typing to pause when Escape is pressed', async () => {
    await mount(true)
    await type('widgets')
    await key('Escape')
    vi.advanceTimersByTime(300)
    await settle()
    expect(searchPullRequests).not.toHaveBeenCalled()
    expect(input().getAttribute('aria-expanded')).toBe('false')
  })

  it('imports the highlighted suggestion when the import button is clicked', async () => {
    vi.mocked(searchPullRequests).mockResolvedValue({ items: [suggestion('widgets', 7, 'Add widgets')], partial: false })
    await mount(true)
    await typeAndWait('widgets')
    await key('ArrowDown')
    const button = host!.querySelector<HTMLButtonElement>('.input-row button')!
    const pressed = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    button.dispatchEvent(pressed)
    // Pressing the button must not move focus off the field, which would close the list.
    expect(pressed.defaultPrevented).toBe(true)
    button.click()
    await settle()
    expect(importPullRequest).toHaveBeenCalledWith('https://github.com/acme/widgets/pull/7')
  })

  it('imports owner/repo#number directly', async () => {
    await mount(true)
    await type('acme/app#7')
    await key('Enter')
    expect(importPullRequest).toHaveBeenCalledWith('https://github.com/acme/app/pull/7')
  })
})

describe('signed out', () => {
  it('accepts URLs and owner/repo#number but never searches', async () => {
    await mount(false)
    await typeAndWait('widgets')
    expect(searchPullRequests).not.toHaveBeenCalled()
    expect(input().getAttribute('aria-expanded')).toBe('false')
    await key('Enter')
    expect(host!.querySelector('.import-panel > [role="alert"]')?.textContent).toContain('Enter a valid GitHub pull request URL')

    await type('acme/app#7')
    await key('Enter')
    expect(importPullRequest).toHaveBeenCalledWith('https://github.com/acme/app/pull/7')
  })
})
