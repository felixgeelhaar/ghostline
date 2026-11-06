# Day 4: Automate the first factory job

## Ticket

**Title:** Validate imported pull request links against their GitHub identity

As an engineer importing a pull request, I want Ghostline to accept only source links that match the imported GitHub repository and revision, so every displayed link remains trustworthy evidence.

Acceptance criteria:

- The imported PR URL uses HTTPS on `github.com` and matches the response owner, repository, and PR number.
- Each commit URL uses HTTPS on `github.com` and matches the response owner, repository, and commit SHA.
- Mismatched and off-host links are rejected at the Zod API boundary.
- Tests cover valid links and host, repository, PR-number, and commit-SHA mismatches.
- Valid GitHub responses keep the existing import and timeline behavior.

## Trigger and workflow

The local Codex schedule invokes `.agents/skills/ghostline-ready-issue/SKILL.md` every two hours. The skill preflights GitHub write access in the scheduled run's configured execution context. It searches open issues labeled `ready-for-agent` oldest first; the label is the explicit approval trigger. Issues from untrusted authors and tickets already represented by open PRs are skipped. It selects at most one eligible issue, requires an explicit `Hackathon Day: N` field, then checks whether a PR already exists for that challenge day. This check happens after ticket selection, so a prior day's PR cannot block later-day work. New PRs carry the same day marker and issue link. The skill reads the acceptance criteria and `AGENTS.md`, then works in an isolated branch/worktree. It removes the label when claiming the ticket to prevent duplicate runs. It follows DDD/TDD and allows at most one focused repair for a confirmed check failure and one for confirmed Qodo findings. It opens a PR for Qodo review only after checks pass; it never merges. If there is no eligible issue, it exits without changes. If GitHub access, Qodo review, requirements, or checks are blocked, it reports the blocker and stops. Reapplying the label is a human-controlled retry after reviewing the blocker.

The schedule uses the local Codex session and its configured GitHub access; no hosted model API key or GitHub Actions agent runner is involved. GitHub Actions remains responsible for hosted CI and Nox checks. The `ready-for-agent` label is removed at pickup so a later schedule does not repeat the same ticket.

## Evidence

Record the schedule run, issue number, PR number, checks, Qodo review revision and findings, repair attempt, and human decision in the Day 4 Discussion. A PR timeline or commit timestamp does not establish causality. The schedule is not considered demonstrated until a run picks up a labeled issue and its outcome is inspected.

## Run record — 2026-11-06

The local two-hour Codex schedule is active. Its latest recorded run launched the repo-local skill, read `AGENTS.md`, then stopped before queue selection because `gh auth status` in the isolated scheduled shell exited with an invalid-token error. It made no issue, branch, worktree, PR, or source changes; no checks or review ran in that invocation. The result is recorded in the schedule's local run memory.

At 19:54 UTC, a separate host-authenticated terminal check succeeded for `gh auth status`, and `gh api repos/felixgeelhaar/ghostline --jq '.permissions.push // false'` returned `true`. The user also confirmed that `gh` works in their other terminals. This shell and the recorded scheduled shell report an invalid token, so the discrepancy is specific to the execution context and remains unresolved; it does not establish that the user's GitHub setup is broken. The pickup skill stops safely when its configured context lacks GitHub access and reports that context-specific blocker.

Issue [#27](https://github.com/felixgeelhaar/ghostline/issues/27) has the `[ready-for-agent]` title prefix but does not use the label trigger, and it already has the open implementation [PR #28](https://github.com/felixgeelhaar/ghostline/pull/28). Its title is not approval, and its open PR makes it ineligible even if labeled. It also lacks the explicit hackathon-day field required by the skill. No other labeled, unrepresented ready issue is currently available. The schedule has not demonstrated a successful automatic ticket pickup. Day 4 remains partial until an eligible ticket is picked up and the scheduled run's outcome is inspected.

PR #28 is open at `f43426fdef57fe45cfacdfe3402f83004ff443a7`. GitHub reports Go checks, frontend checks, the Nox security scan, and `warden/gate` passing. Qodo's structured review completed on this exact head with six findings marked implemented and no open findings. These are PR-level results; they are not results of the failed scheduled invocation.
