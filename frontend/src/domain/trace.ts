import type { FindingOccurrence, RecurringFinding, RevisionRef } from './reviews'

export type TracedOccurrence = FindingOccurrence & {
  revisionInPullRequest: boolean
  revision?: RevisionRef
}

export type FindingTrace = {
  id: string
  title: string
  occurrences: TracedOccurrence[]
  summary: {
    commentCount: number
    markedRevisionCount: number
    distinctRevisionCount: number
    unmarkedCommentCount: number
    focusableRevisionCount: number
    evidence: string
    revisionCoverage: string
    nextStep: string
  }
}

export function buildFindingTrace(finding: RecurringFinding, commits: RevisionRef[]): FindingTrace {
  const occurrences = finding.occurrences
    .map((occurrence): TracedOccurrence => {
      const revision = occurrence.commitSha
        ? commits.find((commit) => commit.sha.toLowerCase() === occurrence.commitSha?.toLowerCase())
        : undefined
      return {
        ...occurrence,
        revisionInPullRequest: Boolean(revision),
        ...(revision ? { revision } : {}),
      }
    })
    .sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt))

  const marked = occurrences.filter((occurrence) => occurrence.commitSha)
  const unmarkedCommentCount = occurrences.length - marked.length
  const distinctRevisionCount = new Set(marked.map((occurrence) => occurrence.commitSha?.toLowerCase())).size
  const focusableRevisionCount = new Set(occurrences.flatMap((occurrence) => occurrence.revision ? [occurrence.revision.sha.toLowerCase()] : [])).size

  let nextStep: string
  if (focusableRevisionCount > 1) {
    nextStep = 'Compare the source comments and marked PR revisions. If a repair was attempted, inspect its patch directly, then run relevant checks on the revision you intend to ship and guide your agent from your IDE.'
  } else if (focusableRevisionCount === 1) {
    nextStep = 'Inspect the source comments and focus the marked PR revision. If a repair was attempted, compare its patch directly, then run relevant checks on the revision you intend to ship and guide your agent from your IDE.'
  } else {
    nextStep = 'Inspect each source comment first. Ghostline cannot focus these comments to a revision in this PR, so confirm the relevant code revision manually. Run relevant checks on the revision you intend to ship, then guide your agent from your IDE.'
  }

  return {
    id: finding.id,
    title: finding.title,
    occurrences,
    summary: {
      commentCount: occurrences.length,
      markedRevisionCount: marked.length,
      distinctRevisionCount,
      unmarkedCommentCount,
      focusableRevisionCount,
    evidence: `This finding title, matched after normalizing case and whitespace, appears in ${occurrences.length} separate Qodo comments.`,
      revisionCoverage: `${marked.length} comment${marked.length === 1 ? '' : 's'} include an explicit revision marker across ${distinctRevisionCount} revision${distinctRevisionCount === 1 ? '' : 's'}; ${unmarkedCommentCount} comment${unmarkedCommentCount === 1 ? '' : 's'} are unmarked.`,
      nextStep,
    },
  }
}

export function interventionPrompt(trace: FindingTrace): string {
  const sources = trace.occurrences.map((occurrence) => {
    const revision = occurrence.commitSha
      ? `revision ${occurrence.commitSha.slice(0, 7)}${occurrence.revisionInPullRequest ? ' in this PR' : ' (not in the imported PR commit list)'}`
      : 'revision not marked'
    return `- Qodo comment time ${occurrence.createdAt}; ${revision}; source: ${occurrence.sourceUrl}`
  }).join('\n')

  return [
    `Investigate this repeated Qodo finding title (case- and whitespace-normalized match): ${JSON.stringify(trace.title)}`,
    '',
    'The normalized title was matched across separate comments. This text match is a signal to inspect, not proof that the same defect persisted.',
    sources,
    '',
    'Inspect the source comments and affected code. If a repair was attempted, compare that patch directly and run relevant checks on the revision intended for shipping. This trace does not prove that a repair happened or explain why the comments differ. Keep unmarked comments unattributed to a revision.',
  ].join('\n')
}
