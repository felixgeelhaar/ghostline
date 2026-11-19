// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import type { NoxReport } from '../domain/noxReport'
import { importPullRequest } from '../application/importPullRequest'
import { fetchRevisionChecks } from '../application/fetchRevisionChecks'
import { fetchNoxReport } from '../application/fetchNoxReport'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchNoxReport', () => ({ fetchNoxReport: vi.fn() }))
vi.mock('../application/fetchRevisionChecks', () => ({ fetchRevisionChecks: vi.fn() }))
vi.mock('../application/fetchGhostlineSession', () => ({ fetchGhostlineSession: vi.fn().mockResolvedValue({ authRequired: false, authenticated: true }) }))

const headSha = 'a'.repeat(40)
const earlierSha = 'b'.repeat(40)
const pr: PullRequest = {
  repository: 'acme/app', owner: 'acme', repo: 'app', number: 7, title: 'Security report', state: 'open', merged: false,
  url: 'https://github.com/acme/app/pull/7', createdAt: '2026-11-10T09:00:00Z', updatedAt: '2026-11-10T10:00:00Z',
  commits: [
    { sha: earlierSha, shortSha: earlierSha.slice(0, 7), message: 'Earlier revision', author: 'A', committedAt: '2026-11-10T09:00:00Z', url: `https://github.com/acme/app/commit/${earlierSha}` },
    { sha: headSha, shortSha: headSha.slice(0, 7), message: 'Current revision', author: 'A', committedAt: '2026-11-10T10:00:00Z', url: `https://github.com/acme/app/commit/${headSha}` },
  ],
  checks: { availability: 'available', revisionSha: headSha, revisionShortSha: headSha.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${headSha}`, items: [] },
  qodoReviews: { availability: 'available', unrecognizedCommentCount: 0, items: [] }, referencedIssues: { availability: 'available', items: [] },
}

function report(revisionSha: string, count = 0, runConclusion = 'success'): Extract<NoxReport, { availability: 'available' }> {
  return {
    availability: 'available', revisionSha, runId: 12, runName: 'Nox Security', runConclusion, scanSuccessful: true,
    runUrl: 'https://github.com/acme/app/actions/runs/12', artifactId: 3, artifactUrl: 'https://github.com/acme/app/actions/runs/12',
    artifactDigest: 'sha256:abc', createdAt: '2026-11-15T13:58:17Z', expiresAt: '2026-11-22T13:58:16Z',
    totalCount: count, activeCount: count, suppressedCount: 0, truncated: false,
    findings: count ? [{ ruleId: 'SEC-1', title: 'Validate input', severity: 'high', message: 'Untrusted input reaches a command.', path: 'backend/server.go', line: 42, suppressed: false, suppressions: [], suppressionsTruncated: false }] : [],
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

async function settle() { await Promise.resolve(); await nextTick(); await nextTick() }

async function importPR(importedPR: PullRequest = pr) {
  vi.mocked(importPullRequest).mockResolvedValue(importedPR)
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(App)
  app.mount(host)
  await vi.waitFor(() => expect(host?.querySelector('#pr-url')).not.toBeNull())
  const input = host!.querySelector<HTMLInputElement>('#pr-url')!
  input.value = pr.url
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
  await vi.waitFor(() => expect(host?.querySelector('.change-card')).not.toBeNull())
}

afterEach(() => { app?.unmount(); app = undefined; host?.remove(); host = undefined; vi.clearAllMocks() })

describe('revision-linked Nox report', () => {
  it('shows exact revision, findings and source run', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue(report(headSha, 1))
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-findings')).not.toBeNull())
    expect(fetchNoxReport).toHaveBeenCalledWith(pr.url, headSha)
    expect(host!.querySelector('.nox-report')?.textContent).toContain('Untrusted input reaches a command.')
    expect(host!.querySelector('.nox-report')?.textContent).toContain('backend/server.go:42')
    expect(host!.querySelector('.nox-source a')?.getAttribute('href')).toBe('https://github.com/acme/app/actions/runs/12')
  })

  it('explains when optional source links and artifact dates were not reported', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue({
      ...report(headSha), runUrl: undefined, artifactUrl: undefined, createdAt: undefined, expiresAt: undefined,
    })
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-summary')).not.toBeNull())
    expect(host!.querySelector('.nox-summary')?.textContent).toContain('report from not reported · retained until not reported')
    expect(host!.querySelector('.nox-source')?.textContent).toContain('Source workflow link not reported')
    expect(host!.querySelector('.nox-source a')).toBeNull()
  })

  it('does not show a clean headline when the scan did not succeed', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue({ ...report(headSha, 0, 'failure'), scanSuccessful: false })
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-summary strong')?.textContent).toContain('results may be incomplete'))
    expect(host!.querySelector('.nox-summary strong')?.textContent).not.toContain('No security findings reported')
    expect(host!.querySelector('.nox-report')?.textContent).not.toContain('not an independent guarantee')
  })

  it('does not show the zero-result note when GitHub omits the workflow outcome', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue({ ...report(headSha, 0), runStatus: 'completed', runConclusion: undefined, scanSuccessful: undefined })
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-summary strong')?.textContent).toContain('Workflow outcome unavailable'))
    expect(host!.querySelector('.nox-report')?.textContent).not.toContain('not an independent guarantee')
  })

  it('shows suppressed reports and explains suppression is not a repair', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue({
      ...report(headSha), totalCount: 1, activeCount: 0, suppressedCount: 1,
      findings: [{ ruleId: 'SEC-BASELINE', title: 'Known issue', severity: 'medium', message: 'Still reported by Nox.', suppressed: true, suppressionKind: 'external', suppressionJustification: 'Accepted baseline', suppressions: [{ status: 'accepted', kind: 'external', justification: 'Accepted baseline' }, { status: 'accepted', kind: 'inSource', justification: 'Second accepted reason' }] }],
    })
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-suppression')).not.toBeNull())
    const panel = host!.querySelector('.nox-report')!
    expect(panel.textContent).toContain('No active findings · 1 suppressed result')
    expect(panel.textContent).toContain('Accepted baseline')
    expect(panel.textContent).toContain('Second accepted reason')
    expect(panel.textContent).toContain('does not mean it was fixed or verified')
    expect(panel.textContent).not.toContain('No security findings reported')
  })

  it('shows accepted suppression detail preserved beyond the displayed source-record limit', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue({
      ...report(headSha), totalCount: 1, activeCount: 0, suppressedCount: 1,
      findings: [{
        ruleId: 'SEC-BASELINE', title: 'Known issue', severity: 'medium', message: 'Still reported by Nox.', suppressed: true,
        suppressionKind: 'external', suppressionJustification: 'Late accepted detail',
        suppressions: Array.from({ length: 20 }, () => ({ status: 'underReview', kind: 'external' })), suppressionsTruncated: true,
      }],
    })
    await importPR()
    await vi.waitFor(() => expect([...host!.querySelectorAll('.nox-suppression')].some((node) => node.textContent?.includes('Late accepted detail'))).toBe(true))
    expect(host!.querySelector('.nox-report')?.textContent).toContain('Some suppression records were omitted')
  })

  it('explains when the displayed finding list is truncated', async () => {
    vi.mocked(fetchNoxReport).mockResolvedValue({
      ...report(headSha, 101), truncated: true,
      findings: Array.from({ length: 100 }, (_, index) => ({
        ruleId: `SEC-${index}`, title: 'Validate input', severity: 'high', message: 'Untrusted input reaches a command.',
        suppressed: false, suppressions: [], suppressionsTruncated: false,
      })),
    })
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-report')?.textContent).toContain('Showing 100 of 101 report results'))
    expect(host!.querySelector('.nox-report')?.textContent).toContain('Open the source workflow for the full report.')
  })

  it('restores the missing revision explanation after clearing focused history', async () => {
    const missingHead: PullRequest = {
      ...pr,
      checks: { availability: 'unavailable', reason: 'GitHub did not provide a pull request revision.', revisionSha: '', revisionShortSha: '', revisionUrl: '', items: [] },
    }
    vi.mocked(fetchNoxReport).mockResolvedValue(report(earlierSha))
    vi.mocked(fetchRevisionChecks).mockResolvedValue({
      availability: 'unavailable', reason: 'Checks unavailable for this commit.', revisionSha: earlierSha,
      revisionShortSha: earlierSha.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${earlierSha}`, items: [],
    })
    await importPR(missingHead)
    await vi.waitFor(() => expect(host!.querySelector('.nox-report')?.textContent).toContain('GitHub did not provide a pull request revision'))

    host!.querySelector<HTMLButtonElement>('.commit-actions .focus-revision')!.click()
    await vi.waitFor(() => expect(host!.querySelector('.nox-report')?.textContent).toContain('No security findings reported'))
    host!.querySelector<HTMLButtonElement>('.clear-focus')!.click()

    await vi.waitFor(() => expect(host!.querySelector('.nox-report')?.textContent).toContain('GitHub did not provide a pull request revision'))
  })

  it('ignores an earlier focused revision request that resolves last', async () => {
    const earlier = deferred<NoxReport>()
    vi.mocked(fetchNoxReport).mockImplementation((_url, sha) => sha === headSha ? earlier.promise : Promise.resolve(report(earlierSha)))
    await importPR()
    await vi.waitFor(() => expect(host!.textContent).toContain('Current revision'))
    host!.querySelector<HTMLButtonElement>('.commit-actions .focus-revision')!.click()
    await vi.waitFor(() => expect(host!.querySelector('.nox-report')?.textContent).toContain(earlierSha.slice(0, 7)))
    earlier.resolve(report(headSha, 1))
    await settle()
    expect(host!.querySelector('.nox-report')?.textContent).not.toContain('Untrusted input reaches a command.')
    expect(host!.querySelector('.nox-report')?.textContent).toContain('No security findings reported')
  })

  it('hides the previous report while the same pull request refreshes its head', async () => {
    const refresh = deferred<PullRequest>()
    const refreshedSHA = 'd'.repeat(40)
    const refreshedPR: PullRequest = {
      ...pr,
      title: 'Security report after refresh',
      commits: [...pr.commits, { sha: refreshedSHA, shortSha: refreshedSHA.slice(0, 7), message: 'Latest revision', author: 'A', committedAt: '2026-11-10T11:00:00Z', url: `https://github.com/acme/app/commit/${refreshedSHA}` }],
      checks: { ...pr.checks, revisionSha: refreshedSHA, revisionShortSha: refreshedSHA.slice(0, 7), revisionUrl: `https://github.com/acme/app/commit/${refreshedSHA}` },
    }
    vi.mocked(importPullRequest).mockResolvedValueOnce(pr).mockReturnValueOnce(refresh.promise)
    vi.mocked(fetchNoxReport).mockResolvedValueOnce(report(headSha, 1)).mockResolvedValueOnce(report(refreshedSHA))
    await importPR()
    await vi.waitFor(() => expect(host!.querySelector('.nox-findings')).not.toBeNull())

    host!.querySelector<HTMLButtonElement>('.input-row button')!.click()
    await settle()

    expect(host!.querySelector('.nox-report')?.textContent).toContain('Loading the published security report')
    expect(host!.querySelector('.nox-report')?.textContent).not.toContain('Untrusted input reaches a command.')

    refresh.resolve(refreshedPR)
    await vi.waitFor(() => expect(host!.querySelector('.nox-report')?.textContent).toContain('No security findings reported'))
    expect(fetchNoxReport).toHaveBeenLastCalledWith(pr.url, refreshedSHA)
  })
})
