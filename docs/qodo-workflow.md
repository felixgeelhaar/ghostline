# Qodo workflow in Ghostline

Ghostline uses Qodo Review as source-backed review feedback and the Qodo
Agentic Toolbox as coding-agent assistance. Neither replaces tests, GitHub's
revision history, Nox security findings, or Warden's local developer gates.

## Pull request review

The root `.pr_agent.toml` asks Qodo to publish a PR summary and code review on
published or updated pull requests. The summary includes a review-effort label.
Review comments include the available context and prior-round outcomes so a
repair loop is easier to inspect. The configuration leaves the selected
Precision preset's finding and display choices in control; it does not enable
automatic code changes or merges.

Link the issue or specification and its acceptance criteria in each PR. Qodo's
requirement and design checks need relevant source context to compare against;
Ghostline should not claim those checks ran unless Qodo's review shows them.

In GitHub, use `@qodo` in a PR comment to ask for an explanation or follow-up.
Treat the response as review discussion. Verify any proposed change against the
current diff and relevant checks before accepting it.

## Coding-agent workflow

After signing in and installing the Qodo Agentic Toolbox for Codex, use its
managed workflows at the relevant stages:

1. **Codebase Wisdom** before a change to understand dependencies, impact, and
   recent history.
2. **Get Rules** before implementation to retrieve the applicable Qodo Review
   Standards.
3. **Review** on local changes before opening the PR.
4. **Review Resolver** after Qodo posts PR findings. Scope the findings to the
   requested change, then report each as fixed, skipped with a reason, or
   reported without changes.
5. **Manage Standards** when project rules need to be created, corrected,
   retired, or re-scoped. Keep `AGENTS.md` as the repository-owned source and
   verify the imported rules are active in Qodo Review Standards.

Run Ghostline's tests, build, Warden gates, and Nox scan independently. Qodo
review feedback is not a correctness guarantee, and security findings remain
owned by Nox.

## Review evidence in Ghostline

For public pull requests, Ghostline reads Qodo's published GitHub issue
comments. Each imported review preserves the GitHub comment ID, author, source
URL, comment timestamp, and Qodo's explicit commit marker when present. Finding
titles, categories, lifecycle labels, severity/action labels, and descriptions
are extracted from the published review. A comment without a commit marker is
shown as unmarked context, not assigned to the current PR head.

Structured Qodo findings also retain their own revision marker. Ghostline
compares that SHA with the imported PR head and labels a mismatch even when
Qodo omits review-session metadata. Missing session or finding revision data
remains explicitly unknown.

When a comment marks a revision present in the imported PR, its card can also
list later commits from GitHub's PR commit list for inspection. They are later
revisions only; their position does not show that they are repair attempts or
that they address the review.

When the same finding title appears in separate Qodo comments and both adjacent
comments explicitly mark revisions that appear in the imported pull request's
commit list, Ghostline lists the commits between those revision anchors as
candidates to inspect. It links each source comment and revision, and can focus
the exact candidate SHA in the timeline. If an anchor is missing, outside the
imported commit list, unchanged, adjacent, or reversed in commit-list order,
Ghostline reports that state instead of inventing an interval. These candidates
are based only on GitHub PR commit-list order; they are not verified repair
attempts and do not establish causality. A repeated title is a signal to inspect,
not proof the same defect persisted. Deep Trace labels whether the matched comments
point to one of four explicit states: `SAME PR REVISION`, `DISTINCT PR
REVISIONS`, `MARKED REVISIONS OUTSIDE IMPORT`, or `NO REVIEW REVISION
MARKERS`. When comments are unmarked or include revisions outside the imported
commit list, the first three states append ` · INCOMPLETE COVERAGE`; the
no-marker state already makes the missing revision data explicit. These labels
describe available markers only; they do not say whether an agent attempted a
repair. A finding disappearing from a later Qodo
review is not proof it was fixed, and a Qodo outcome is not independent
verification. Inspect the linked comments and candidate patches, then use
relevant checks for the exact revision you intend to ship.

Published GitHub comments remain available without a Qodo credential. For a
Qodo workspace's structured findings and review runs, an engineer can supply a
personal Qodo API key for one request from an HTTPS Ghostline origin (or
localhost during development). Ghostline forwards it only to Qodo's fixed
Agentic Toolbox MCP endpoint, clears the input before waiting for the result,
and does not store it. Use this only on a Ghostline instance you trust. The
structured result preserves Qodo's finding/run identifiers, statuses, source,
revision references, locations, and citation evidence; it is shown separately
from published comments.
Workspace access and current availability depend on the supplied key. A result
whose reviewed SHA differs from the imported PR head is explicitly labeled
stale. If Qodo omits evidence, Ghostline shows that gap rather than filling it
with an inferred relationship.

The structured review also includes an engineer next-action summary derived
only from Qodo's reported session and finding statuses. It distinguishes stale
or unknown revisions, unfinished sessions, open or unclassified findings, and
Qodo-reported outcomes. The summary directs engineers to the underlying source
evidence; `implemented` or `dismissed` statuses are not presented as
independent verification.

The running UI's structured-review entry point is shown in the
[feature screenshot](feature-evidence/qodo-structured-review.jpg). It shows
the optional per-request credential control alongside the imported PR's
source-backed review context; it does not depict a fetched structured session.
The next-action presentation is shown in
[its focused screenshot](feature-evidence/qodo-attention-summary.jpg), captured
from a prepared replay labeled as sample data rather than a live Qodo response.

## Features to evaluate in the Qodo workspace

The repository config cannot activate organization or workspace settings.
Review these in the Qodo portal after connecting the repository:

- Confirm `AGENTS.md` rules are imported and active in Review Standards.
- Keep reviews on published PRs and enable review on every push.
- Keep chat with Qodo enabled.
- Evaluate the Recall finding types (skill insights, specification gaps, and
  design gaps) while keeping automatic remediation disabled. Ghostline's
  current Precision preset intentionally prioritizes fewer, actionable
  comments, so change this only as an explicit workspace choice.
- Connect related repositories before relying on cross-repository findings.
- Try latest-dependency review on a dependency-change PR and inspect the
  revision and evidence Qodo reports.
- Treat PR Triage, analytics, and cross-repository Software Map as team-scale
  capabilities to evaluate when the repository set and review volume justify
  them; confirm plan and access requirements in the workspace.

In Ghostline, imported Qodo comments retain their source URL and inspected
revision when Qodo marks one. Their sequence does not prove a repair caused a
finding to disappear.
