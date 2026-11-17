# Ghostline factory verification

This is the intended path from an accepted ticket to `main`. The agent owns
implementation and evidence collection; engineers own ambiguous findings,
stalled work, and the merge decision. A check or review only describes the
revision it inspected.

```mermaid
flowchart TD
    A[Ticket with acceptance criteria] --> B[Read AGENTS.md, relevant docs, and current code]
    B --> WZ[Investigate the ticket with Codebase Wisdom: existing capability, affected components, duplicate work; check cited sources]
    WZ --> WZD{Does the ticket still hold?}
    WZD -- No --> WZR[Revise or close the ticket with evidence; engineer confirms]
    WZR --> A
    WZD -- Yes --> C[Check ticket readiness, clean base, baseline, and criterion checks]
    C --> D[Write a focused failing test]
    D --> E[Implement within domain, application, and edge boundaries]
    E --> F[Run focused tests, Go checks, frontend checks, and Nox]
    F --> G[Review uncommitted changes with Qodo and ticket context]
    G --> H{Confirmed High or Medium finding?}
    H -- Yes --> VL{Has the same confirmed finding recurred after one focused repair?}
    VL -- Yes --> W[Stop and ask engineer for a decision]
    VL -- No --> I[Fix with a focused test or direct check]
    I --> F
    H -- No --> J[Report every Low; support disagreements with evidence]
    J --> K[Open one focused PR, mark it ready for review, and confirm Qodo starts a review]
    K --> L[Run Warden gates and hosted checks on the PR head]
    K --> M[Inspect Qodo PR feedback and the revision it reviewed]
    L --> N{All required checks pass on current head?}
    N -- No --> FP{Did the check produce a result?}
    FP -- No, runner or network failure --> RU{Already rerun once?}
    RU -- No --> RR[Rerun the failed job once; record the run link]
    RR --> L
    RU -- Yes --> W
    FP -- Yes, it reported a finding or failure --> Q[Fix failure, or ask the engineer to baseline a false positive with a written reason, or report blocker]
    Q --> L
    N -- Yes --> R{Review and checks cover latest head?}
    M --> O{Confirmed High or Medium finding?}
    O -- Yes --> VP{Has the same confirmed finding recurred after one focused repair?}
    VP -- Yes --> W
    VP -- No --> I2[Fix with a focused regression test]
    I2 --> P2[Run local checks and Warden gates]
    P2 --> K2[Push update to same PR; request fresh Qodo review]
    K2 --> K
    O -- No --> P[Account for every Low and evidence-backed disagreement]
    P --> D1[Engineer decides disputed findings and Low reports]
    D1 -- Resolved --> R
    D1 -- Unresolved --> U[Hold PR and record next action]
    R -- No --> K
    R -- Yes --> BU{Branch up to date with main?}
    BU -- No --> UB[Update the branch, run the Warden pre-push gate on the new head, and wait for fresh checks and review]
    UB --> K
    BU -- Yes --> S[Human decides whether to merge]
    S -- Merge --> T[main]
    S -- Hold or reject --> U
```

## Gates and decisions

- Before implementation, investigate the ticket with Qodo Codebase Wisdom: ask
  which existing capability it touches, which components depend on that code,
  and whether the work already exists. Check every answer that influences the
  decision against the cited code. If the investigation shows the ticket is
  already delivered, wrong, or incomplete, revise or close it with that
  evidence and let the engineer confirm before building.
- Then confirm the ticket is actionable, check for duplicate
  work, start from the intended clean base, record relevant baseline results,
  and map each criterion to a check. If a criterion or source is unclear, ask
  rather than inventing behavior.
- Use TDD for behavior changes: first add a focused failing test, then the
  smallest implementation. Keep domain rules out of HTTP and rendering edges.
- Before opening a PR, run the relevant tests and checks and review the complete
  local diff with Qodo using the ticket and project rules as context.
- Open the PR as ready for review, or mark a draft ready before relying on
  Qodo: Qodo does not review draft PRs. Confirm a review has started for the
  current head instead of assuming it will.
- On the PR, inspect Qodo's reviewed SHA and every finding. Fix confirmed High
  and Medium findings (including Medium Bugs) with a direct test or check; flag
  each Low for the engineer. For disagreements, record source, reproduction,
  or test evidence and get a human decision.
- Include the repository's tracked-file Nox scan in local verification, and require
  the hosted Nox check on the PR head alongside Go, frontend, and Warden gates.
  Name the revision each result covers; a pass on an earlier head does not
  verify a later head.
- Before treating the PR gate as complete, compare GitHub's current `head.sha`
  with the commit SHA recorded by the latest completed Qodo review and with the
  revision each required check reports. Each value must match exactly. A missing
  or unreadable review/check revision is incomplete evidence; request a fresh
  review or check, and stop for an engineer if that evidence cannot be obtained.
  Repeat the comparison after every push; timestamps or comment order do not
  establish revision coverage.
- A failed required check that reported a finding is a real result, even if the
  same check passed on another run. Do not rerun it to get a green check. Fix
  the cause, or, for a false positive, ask the engineer to record a baseline
  entry with a written reason (AGENTS.md rule 5). Rerun a failed job once only
  when it produced no result because of the runner or network, and record the
  run link in the PR. If the rerun also produces no result, stop and ask the
  engineer.
- `main` requires branches to be up to date. Updating a branch, including
  GitHub's "Update branch" button, creates a new head that has no `warden/gate`
  status, because Warden publishes that status from a local run. Run the
  Warden pre-push gate on the new head, then wait for fresh hosted checks and a
  Qodo review of that head before asking for the merge.
- Rerun checks after changes and request a new review. A review of an older head,
  an incomplete review, or a failed required check does not pass the gate.
- Stop for an engineer if the same confirmed High or Medium finding remains
  after one focused repair attempt, if a required check cannot be made to pass,
  or if evidence is insufficient to resolve a disagreement. Report the finding,
  attempted repair, revisions reviewed, and decision needed. Agents do not merge;
  the engineer decides after the latest head is reviewed and verified.

The repository's current commands and Warden gate are listed in
[`AGENTS.md`](../AGENTS.md). For every release, capture the actual release
candidate in working software and include screenshot evidence in the release
notes or delivery. A feature delivery also includes a screenshot of its changed
UI; a documentation-only change has no changed UI to capture.

## Walk-through against a real ticket

Issue [#39](https://github.com/felixgeelhaar/ghostline/issues/39) asked that
only the latest pull-request import could update the page, including its error
and loading states. It required clearing focused state and tests where an
earlier success or failure settled last. The ticket linked the project rules
and asked for TDD, Qodo review, and no merge by the agent.

Its implementation PR, [#44](https://github.com/felixgeelhaar/ghostline/pull/44),
closed the issue. The PR record was checked on 2026-11-16 and identifies
`ec64bb18acf0129c96cfda19762f568109cb2276` as its head revision. The PR body
records:

- DOM tests for an earlier success and failure settling last, plus an invalid
  latest import.
- Warden pre-commit and pre-push checks, including Go tests, `go vet`, frontend
  tests/build, and Nox, reported as passing on
  `ec64bb18acf0129c96cfda19762f568109cb2276`; Docker Compose startup and an
  attended Chrome check of the changed behavior (reported in the PR body, with
  no separate run log attached).
- A local full Qodo review of the current diff with no findings, reported in
  the PR body for that change. The record does not include a separate Qodo
  review artifact or reviewed SHA.
- A screenshot of the running app showing the latest import still displayed
  after the slower response settled. The image is linked from the PR body at
  `docs/evidence/latest-import-race.jpg` in that exact commit.
- A successful `warden/gate` status on
  `ec64bb18acf0129c96cfda19762f568109cb2276`; the PR is merged.

The issue and PR were also checked in GitHub on 2026-11-16. That record
exercises ticket criteria, race-test coverage, local review, quality gates,
real UI verification, evidence capture, and a human merge. It
does **not** show a submitted GitHub PR review from Qodo: GitHub has no formal
review submission recorded for PR #44. The intended Qodo PR-feedback and
finding-repair path above is therefore documented but was not exercised by
this example. That distinction is part of the verification result, not a
claim that the PR received feedback it did not receive.

The available PR record confirms the race tests, but does not show that they
were observed failing before implementation; it also does not record a
pre-change baseline or duplicate-work check. It records Warden's Nox gate, but
does not establish a separate hosted Nox result. Those remain workflow steps
and evidence to capture explicitly on a future run.

## Second walk-through: PR #69

[PR #69](https://github.com/felixgeelhaar/ghostline/pull/69) (Nox findings
reported as GitHub issues) was taken through the Day 9 quality gate on
2026-11-17. That run exercised the steps PR #44 did not:

- Two local Qodo reviews of the change, with the requirements and the gate as context, and three
  hosted Qodo reviews, each tied to its reviewed SHA.
- Six confirmed High or Medium findings fixed: five code bugs, each with a
  regression test observed failing first, and one documentation gap. One
  disagreement was backed by evidence and decided by the engineer.
- The "required check cannot pass" stop: Warden's Nox step blocked a commit on
  three new dependency advisories with no available fix. The engineer recorded
  a decision on [issue #45](https://github.com/felixgeelhaar/ghostline/issues/45#issuecomment-6960376515)
  and the baseline entries were merged in
  [PR #71](https://github.com/felixgeelhaar/ghostline/pull/71).
- The final head `853da64` had a completed Qodo review with no open findings
  and passing required checks on that SHA. The PR then waited for the engineer's
  merge decision.

The run also exposed four gaps, which this page now covers:

1. The pre-work investigation was implicit. Codebase Wisdom is now a named step
   with its own decision; a Day 6 Wisdom investigation found that issue #27 was
   already delivered.
2. PR #69 stayed a draft and received no Qodo review until it was marked ready.
3. GitHub's "Update branch" on PR #68 produced a head without a `warden/gate`
   status; the Warden pre-push gate had to be run locally on that head.
4. On PR #71 head `7a72b8b`, hosted Nox
   [run 44170424603](https://github.com/felixgeelhaar/ghostline/actions/runs/44170424603)
   failed on an AI-038 finding at `README.md:22`, which issue #66 tracks as a
   suspected false positive. The failed job was rerun on the same SHA and
   passed, and the PR was merged. That rerun turned a reported finding green
   without a baseline entry. The rule above no longer allows it: a reported
   finding must be fixed or baselined with the engineer's written reason.
   Issue #66 still needs that decision.
