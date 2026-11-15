// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PullRequest } from '../domain/pullRequest'
import type { NoxReport } from '../domain/noxReport'
import { importPullRequest } from '../application/importPullRequest'
import { fetchNoxReport } from '../application/fetchNoxReport'
import App from './App.vue'

vi.mock('../application/importPullRequest', () => ({ importPullRequest: vi.fn() }))
vi.mock('../application/fetchNoxReport', () => ({ fetchNoxReport: vi.fn() }))
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

function report(revisionSha: string, count = 0): NoxReport {
  return {
    availability: 'available', revisionSha, runId: 12, runName: 'Nox Security', runConclusion: 'success',
    runUrl: 'https://github.com/acme/app/actions/runs/12', artifactId: 3, artifactUrl: 'https://github.com/acme/app/actions/runs/12',
    artifactDigest: 'sha256:abc', createdAt: '2026-11-15T13:58:17Z', expiresAt: '2026-11-22T13:58:16Z',
    totalCount: count, truncated: false,
    findings: count ? [{ ruleId: 'SEC-1', title: 'Validate input', severity: 'high', message: 'Untrusted input reaches a command.', path: 'backend/server.go', line: 42 }] : [],
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

async function importPR() {
  vi.mocked(importPullRequest).mockResolvedValue(pr)
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
})
