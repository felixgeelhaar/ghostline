// @vitest-environment happy-dom
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import type { FindingTrace, TracedOccurrence } from '../domain/trace'
import { importPullRequest } from '../application/importPullRequest'
import App from './App.vue'
import DeepTrace from './DeepTrace.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))

const shaA = 'a'.repeat(40)
const shaB = 'b'.repeat(40)
const commitA = { sha: shaA, shortSha: 'aaaaaaa', message: 'First', author: 'A', committedAt: '2026-11-04T09:00:00Z', url: `https://github.com/acme/app/commit/${shaA}` }
const commitB = { sha: shaB, shortSha: 'bbbbbbb', message: 'Second', author: 'A', committedAt: '2026-11-04T10:00:00Z', url: `https://github.com/acme/app/commit/${shaB}` }
const reviews = [
  { id: 201, provider: 'Qodo' as const, sourceType: 'github_issue_comment' as const, sourceAuthor: 'qodo-code-review[bot]', kind: 'findings' as const, findings: [
    { id: '201:1', title: 'Finding one', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-201' },
    { id: '201:2', title: 'Finding two', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-202' },
  ], commitSha: shaA, commitUrl: commitA.url, createdAt: '2026-11-04T10:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-201' },
  { id: 202, provider: 'Qodo' as const, sourceType: 'github_issue_comment' as const, sourceAuthor: 'qodo-code-review[bot]', kind: 'findings' as const, findings: [
    { id: '202:1', title: 'Finding one', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-203' },
    { id: '202:2', title: 'Finding two', categories: [], sourceUrl: 'https://github.com/acme/app/pull/7#issuecomment-204' },
  ], commitSha: shaB, commitUrl: commitB.url, createdAt: '2026-11-04T11:00:00Z', url: 'https://github.com/acme/app/pull/7#issuecomment-205' },
]

const pullRequest: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Keyboard trace test', state: 'open',
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-04T08:00:00Z', updatedAt: '2026-11-04T11:00:00Z',
  commits: [commitA, commitB],
  checks: { availability: 'available', revisionSha: shaB, revisionShortSha: 'bbbbbbb', revisionUrl: commitB.url, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: reviews },
}

function occurrence(id: number, sha: string): TracedOccurrence {
  const revision = sha === shaA ? commitA : commitB
  return {
    reviewId: id, findingId: `${id}:1`, createdAt: '2026-11-04T10:00:00Z',
    url: `https://github.com/acme/app/pull/7#issuecomment-${id}`,
    sourceUrl: `https://github.com/acme/app/pull/7#issuecomment-${id}`, categories: [],
    commitSha: sha, commitUrl: revision.url, revisionInPullRequest: true, revision,
  }
}

function findingTrace(id: string, title: string): FindingTrace {
  return {
    id, title, occurrences: [occurrence(301, shaA), occurrence(302, shaB)],
    summary: { commentCount: 2, markedRevisionCount: 2, distinctRevisionCount: 2, unmarkedCommentCount: 0, focusableRevisionCount: 2,
      evidence: `The normalized title ${title} appears in 2 comments.`, revisionCoverage: '2 comments across 2 focusable PR revisions.', nextStep: `Inspect ${title} evidence.` },
  }
}

let mounted: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function settleVue() {
  await Promise.resolve()
  await nextTick()
  await nextTick()
}

afterEach(() => {
  mounted?.unmount()
  mounted = undefined
  host?.remove()
  host = undefined
  vi.clearAllMocks()
})

describe('Deep Trace browser interactions', () => {
  it('moves keyboard focus into the trace and restores it to its opening control', async () => {
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
    await vi.waitFor(() => expect(host?.querySelectorAll('.trace-open')).toHaveLength(2))

    const opener = host.querySelector<HTMLButtonElement>('.recurrence-list li:first-child .trace-open')!
    opener.focus()
    opener.click()
    await settleVue()
    expect(document.activeElement?.id).toBe('deep-trace-title')

    host.querySelector<HTMLButtonElement>('.trace-close')!.click()
    await settleVue()
    expect(document.activeElement).toBe(opener)

    opener.click()
    await settleVue()
    expect(document.activeElement?.id).toBe('deep-trace-title')
    opener.click()
    await settleVue()
    expect(document.activeElement).toBe(opener)
  })

  it('shows the live empty state without exposing a trace opener when no title repeats', async () => {
    vi.mocked(importPullRequest).mockResolvedValue({
      ...pullRequest,
      qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [
        { ...reviews[0]!, findings: [reviews[0]!.findings[0]!] },
      ] },
    })
    host = document.createElement('div')
    document.body.append(host)
    mounted = createApp(App)
    mounted.mount(host)
    const input = host.querySelector<HTMLInputElement>('#pr-url')!
    input.value = 'https://github.com/acme/app/pull/7'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    host.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await vi.waitFor(() => expect(host?.querySelector('.recurrence')).not.toBeNull())
    expect(host.querySelector('.recurrence')?.textContent).toContain('No repeated finding title appears')
    expect(host.querySelector('.trace-open')).toBeNull()
  })

  it('clears copied guidance status when the reused panel switches findings', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const current = ref(findingTrace('one', 'Finding one'))
    const Parent = defineComponent({ setup: () => () => h(DeepTrace, { trace: current.value }) })
    host = document.createElement('div')
    document.body.append(host)
    mounted = createApp(Parent)
    mounted.mount(host)

    host.querySelector<HTMLButtonElement>('.trace-copy')!.click()
    await settleVue()
    expect(host.querySelector('[role="status"]')?.textContent).toContain('Guidance copied')
    current.value = findingTrace('two', 'Finding two')
    await settleVue()
    expect(host.querySelector('[role="status"]')?.textContent).toBe('')
  })
})
