import type { QodoReview, RevisionRef } from './reviews'
import { checkOutcome, type Check, type RevisionChecks } from './checks'

export type ChangeActivityEvent = {
  id: string
  kind: 'commit' | 'qodo_review' | 'github_check'
  timestamp: string
  timestampLabel: 'COMMIT METADATA TIME' | 'QODO COMMENT TIME' | 'CHECK RUN COMPLETION TIME' | 'CHECK RUN START TIME' | 'CHECK RUN UPDATE TIME' | 'COMMIT STATUS UPDATE TIME' | 'COMMIT STATUS CREATION TIME'
  title: string
  detail: string
  sourceUrl: string
  sourceLabel: string
  revisionLabel: string
  revision?: RevisionRef
}

function checkTimestamp(check: Check): { timestamp: string; label: ChangeActivityEvent['timestampLabel'] } | undefined {
  if (check.kind === 'check_run') {
    if (check.completedAt) return { timestamp: check.completedAt, label: 'CHECK RUN COMPLETION TIME' }
    if (check.startedAt) return { timestamp: check.startedAt, label: 'CHECK RUN START TIME' }
    if (check.updatedAt) return { timestamp: check.updatedAt, label: 'CHECK RUN UPDATE TIME' }
    return undefined
  }
  if (check.updatedAt) return { timestamp: check.updatedAt, label: 'COMMIT STATUS UPDATE TIME' }
  if (check.createdAt) return { timestamp: check.createdAt, label: 'COMMIT STATUS CREATION TIME' }
  return undefined
}

function checkActivity(checks: RevisionChecks | undefined, revisionBySha: Map<string, RevisionRef>): ChangeActivityEvent[] {
  if (!checks || checks.availability !== 'available') return []
  const revision = revisionBySha.get(checks.revisionSha.toLowerCase())
  if (!revision) return []
  return checks.items.flatMap((check) => {
    const when = checkTimestamp(check)
    if (!when || !Number.isFinite(Date.parse(when.timestamp))) return []
    const sourceUrl = check.url || checks.revisionUrl
    const sourceName = check.kind === 'check_run' ? 'VIEW CHECK RUN' : 'VIEW STATUS TARGET'
    const recordType = check.kind === 'check_run' ? 'RUN' : 'STATUS'
    const sourceLabel = check.url
      ? `${sourceName}${check.id === undefined ? '' : ` #${check.id}`}`
      : `VIEW REVISION CONTEXT${check.id === undefined ? '' : ` · ${recordType} #${check.id}`}`
    const identity = check.id === undefined ? `${check.name}:${when.timestamp}` : String(check.id)
    return [{
      id: `check:${checks.revisionSha}:${check.kind}:${identity}`,
      kind: 'github_check',
      timestamp: when.timestamp,
      timestampLabel: when.label,
      title: check.name,
      detail: checkOutcome(check).label.toUpperCase(),
      sourceUrl,
      sourceLabel,
      revisionLabel: `CHECKED REVISION ${revision.shortSha}`,
      revision,
    }]
  })
}

export function buildChangeActivity(commits: RevisionRef[], reviews: QodoReview[], checks?: RevisionChecks): ChangeActivityEvent[] {
  const revisionBySha = new Map(commits.map((commit) => [commit.sha.toLowerCase(), commit]))
  const events: ChangeActivityEvent[] = [
    ...commits.map((commit): ChangeActivityEvent => ({
      id: `commit:${commit.sha}`,
      kind: 'commit',
      timestamp: commit.committedAt,
      timestampLabel: 'COMMIT METADATA TIME',
      title: commit.message,
      detail: `COMMITTED BY ${commit.author}`,
      sourceUrl: commit.url,
      sourceLabel: 'VIEW COMMIT',
      revisionLabel: `REVISION ${commit.shortSha}`,
      revision: commit,
    })),
    ...checkActivity(checks, revisionBySha),
    ...reviews.map((review): ChangeActivityEvent => {
      const revision = review.commitSha ? revisionBySha.get(review.commitSha.toLowerCase()) : undefined
      const revisionLabel = revision
        ? `REVIEWED REVISION ${revision.shortSha}`
        : review.commitSha
          ? 'MARKED REVISION OUTSIDE IMPORT'
          : 'REVISION NOT MARKED'
      const detail = review.kind === 'findings'
        ? review.findings.length === 0
          ? 'FINDING DETAILS UNAVAILABLE'
          : `${review.findings.length} FINDING${review.findings.length === 1 ? '' : 'S'}`
        : review.kind === 'no_open_findings'
          ? 'QODO REPORTED NO OPEN FINDINGS'
          : review.summary
            ? `${review.summary.replace(/\s+/g, ' ').trim().slice(0, 137).trimEnd()}${review.summary.replace(/\s+/g, ' ').trim().length > 137 ? '…' : ''}`
            : 'QODO PULL REQUEST SUMMARY'
      const title = review.kind === 'findings'
        ? review.findings[0]?.title ?? 'Qodo code review findings'
        : review.kind === 'no_open_findings'
          ? 'Qodo reported no open findings'
          : 'Qodo pull request summary'
      return {
        id: `qodo:${review.id}`,
        kind: 'qodo_review',
        timestamp: review.createdAt,
        timestampLabel: 'QODO COMMENT TIME',
        title,
        detail,
        sourceUrl: review.url,
        sourceLabel: `QODO COMMENT #${review.id}`,
        revisionLabel,
        ...(revision ? { revision } : {}),
      }
    }),
  ]

  return events.sort((left, right) => {
    const leftTime = Date.parse(left.timestamp)
    const rightTime = Date.parse(right.timestamp)
    const validLeft = Number.isFinite(leftTime) ? leftTime : Number.POSITIVE_INFINITY
    const validRight = Number.isFinite(rightTime) ? rightTime : Number.POSITIVE_INFINITY
    if (validLeft !== validRight) return validLeft - validRight
    if (left.kind !== right.kind) {
      const rank: Record<ChangeActivityEvent['kind'], number> = { commit: 0, github_check: 1, qodo_review: 2 }
      return rank[left.kind] - rank[right.kind]
    }
    return left.id.localeCompare(right.id)
  })
}
