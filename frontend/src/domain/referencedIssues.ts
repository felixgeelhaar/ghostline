export type ReferencedIssueStatus = {
  availability: 'available' | 'unavailable'
  state?: 'open' | 'closed'
}

export function isMergedOpenIssue(merged: boolean, issue: ReferencedIssueStatus): boolean {
  return merged && issue.availability === 'available' && issue.state === 'open'
}
