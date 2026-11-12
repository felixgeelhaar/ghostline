// @vitest-environment happy-dom
import { createApp, nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import StructuredQodoReview from './StructuredQodoReview.vue'
import type { QodoStructuredReview } from '../domain/qodoReviewSession'

const reviewedSha = 'a'.repeat(40)
const currentSha = 'b'.repeat(40)
const review: QodoStructuredReview = {
  availability: 'available',
  source: 'Qodo Agentic Toolbox · get-pr-review-findings',
  reviewSession: { status: 'completed', commitSha: reviewedSha },
  findings: [{ id: 'finding-17', title: 'Handle replay safely', status: 'implemented', gitSha: reviewedSha, reviewRunId: 'run-2', filePath: 'backend/replay.go', startLine: 19, side: 'RIGHT', evidence: { explanation: 'The reviewer cited this branch.', citations: [{ sourceType: 'code', source: 'https://github.com/acme/payments/blob/main/backend/replay.go#L19', text: 'apply(request)', context: 'inside the replay handler' }] } }],
  reviewRuns: [{ id: 'run-2', status: 'completed', commitSha: currentSha }],
}

let app: ReturnType<typeof createApp> | undefined
let host: HTMLDivElement | undefined

afterEach(() => {
  app?.unmount()
  app = undefined
  host?.remove()
  host = undefined
})

describe('structured Qodo review presentation', () => {
  it('keeps Qodo statuses source-attributed and warns when reviewed revision is stale', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review,
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    expect(host.textContent).toContain('OBSERVED · Qodo Agentic Toolbox · get-pr-review-findings')
    expect(host.textContent).toContain('DOES NOT MATCH PR HEAD')
    expect(host.textContent).toContain('Review evidence is stale.')
    expect(host.textContent).toContain('QODO FINDING finding-17 · REPORTED implemented')
    expect(host.textContent).toContain('QODO-LINKED REVIEW RUN run-2 · completed')
    expect(host.textContent).toContain('QODO EVIDENCE · The reviewer cited this branch.')
    expect(host.textContent).toContain('inside the replay handler')
    expect(host.textContent).toContain('does not independently verify that a finding was fixed')
    expect(host.querySelector<HTMLAnchorElement>('a[href*="/commit/"]')?.href).toBe(`https://github.com/acme/payments/commit/${reviewedSha}`)
    expect(host.querySelector<HTMLAnchorElement>('a[href*="/blob/main/"]')?.href).toBe('https://github.com/acme/payments/blob/main/backend/replay.go#L19')
    expect(host.querySelector('.qodo-attention')?.textContent).toContain('Review needs a refresh')
    expect(host.querySelector('.qodo-attention')?.textContent).toContain(reviewedSha.slice(0, 7))
    expect(host.querySelector('.qodo-attention')?.textContent).toContain(currentSha.slice(0, 7))
  })

  it('summarizes Qodo reported open findings as the next inspection step', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review: {
        ...review,
        reviewSession: { status: 'completed', commitSha: currentSha },
        findings: [{ id: 'finding-open', title: 'Validate idempotency', status: 'open', gitSha: currentSha }],
      },
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    const summary = host.querySelector('.qodo-attention')
    expect(summary?.textContent).toContain('1 finding reported open')
    expect(summary?.textContent).toContain('Inspect the open findings below')
    expect(summary?.getAttribute('role')).toBe('status')
    expect(host.textContent).toContain('QODO FINDING finding-open · REPORTED open')
  })

  it('keeps open finding urgency visible when the review is stale', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review: {
        ...review,
        reviewSession: { status: 'completed', commitSha: reviewedSha },
        findings: [
          { id: 'finding-open-1', title: 'Validate replay key', status: 'open', gitSha: reviewedSha },
          { id: 'finding-open-2', title: 'Bound retry count', status: 'open', gitSha: reviewedSha },
        ],
      },
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    const summary = host.querySelector('.qodo-attention')
    expect(summary?.textContent).toContain('Qodo reports 2 findings as open')
    expect(summary?.textContent).toContain('Inspect the 2 Qodo-reported open findings below')
    expect(summary?.textContent).toContain('current PR head')
    expect(host.textContent).toContain('Review evidence is stale.')
  })

  it('shows open finding urgency while Qodo is still reviewing the current head', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review: {
        ...review,
        reviewSession: { status: 'running', commitSha: currentSha },
        findings: [{ id: 'finding-running', title: 'Validate retry key', status: 'open', gitSha: currentSha }],
      },
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    const summary = host.querySelector('.qodo-attention')
    expect(summary?.textContent).toContain('Qodo review is still running')
    expect(summary?.textContent).toContain('Qodo reports 1 finding as open')
    expect(summary?.textContent).toContain('Inspect the 1 Qodo-reported open finding below')
    expect(summary?.textContent.toLowerCase()).toContain('wait for the qodo session to complete')
  })

  it('does not call a session stale when Qodo omitted its reviewed revision', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review: { ...review, reviewSession: { status: 'completed' } },
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    expect(host.textContent).toContain('REVISION UNKNOWN')
    expect(host.textContent).toContain('Review freshness is unknown.')
    expect(host.textContent).not.toContain('Review evidence is stale.')
  })

  it('explains unknown freshness when the imported pull request head is unavailable', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review,
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: '',
    })
    app.mount(host)
    await nextTick()

    expect(host.textContent).toContain('REVISION UNKNOWN')
    expect(host.textContent).toContain('Ghostline cannot compare this session with a known pull request head revision.')
    expect(host.textContent).not.toContain('Qodo did not return a reviewed commit SHA')
    expect(host.textContent).not.toContain('Review evidence is stale.')
  })

  it('keeps source findings visible when Qodo returned no session metadata', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review: { ...review, reviewSession: null },
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    expect(host.textContent).toContain('No review session returned.')
    expect(host.textContent).toContain('QODO FINDING finding-17 · REPORTED implemented')
    expect(host.textContent).toContain('QODO-LINKED REVIEW RUN run-2 · completed')
  })

  it('warns when a finding revision is older even without session metadata', async () => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(StructuredQodoReview, {
      review: {
        ...review,
        reviewSession: null,
        findings: [{ id: 'finding-old', title: 'Guard replay', status: 'open', gitSha: reviewedSha }],
      },
      pullRequestUrl: 'https://github.com/acme/payments/pull/42',
      headSha: currentSha,
    })
    app.mount(host)
    await nextTick()

    expect(host.textContent).toContain('Finding revision does not match the imported PR head.')
    expect(host.textContent).toContain(reviewedSha.slice(0, 7))
    expect(host.textContent).toContain(currentSha.slice(0, 7))
  })
})
