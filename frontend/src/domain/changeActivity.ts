import type { QodoReview, RevisionRef } from './reviews'

export type ChangeActivityEvent = {
  id: string
  kind: 'commit' | 'qodo_review'
  timestamp: string
  timestampLabel: 'COMMIT METADATA TIME' | 'QODO COMMENT TIME'
  title: string
  detail: string
  sourceUrl: string
  sourceLabel: string
  revisionLabel: string
  revision?: RevisionRef
}

export function buildChangeActivity(commits: RevisionRef[], reviews: QodoReview[]): ChangeActivityEvent[] {
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
    if (left.kind !== right.kind) return left.kind === 'commit' ? -1 : 1
    return left.id.localeCompare(right.id)
  })
}
