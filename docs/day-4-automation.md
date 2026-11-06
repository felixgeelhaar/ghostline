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

The local Codex schedule invokes `.agents/skills/ghostline-ready-issue/SKILL.md` every two hours. Scheduled runs use the connected GitHub tools for repository reads and writes, so they do not depend on the `gh` CLI or SSH agent available in a user's interactive terminal. Before worktree creation, the local default-branch SHA must match the published GitHub SHA. The skill searches open issues labeled `ready-for-agent` oldest first, treating the maintainer-applied label as explicit approval. Issues from untrusted authors are skipped. An open PR suppresses an issue only when its author is trusted, it explicitly links the exact issue, and its changes or description address the ticket's acceptance criteria; a bare reference or unrelated PR does not suppress work. It selects at most one eligible issue and requires an explicit `Hackathon Day: N` field. A persistent per-day Git ref is claimed with an expected-SHA update before removing the issue label; overlapping runs compete to update the same ref. The lock is held through PR creation and released by moving the ref back to its saved base SHA. Unexpected termination can leave a lock commit that requires human verification and recovery. New PRs carry the day marker and issue link. The skill reads the acceptance criteria and `AGENTS.md`, then works in an isolated branch/worktree. It follows DDD/TDD and allows at most one focused repair for a confirmed check failure and one for confirmed Qodo findings. It runs the configured checks locally, then publishes the verified patch as GitHub Git objects and creates a branch and PR through the connected GitHub tools. It opens a PR for Qodo review only after checks pass; it never merges. If there is no eligible issue, it exits without changes. If GitHub access, Qodo review, requirements, or checks are blocked, it reports the blocker and stops. Reapplying the label is a human-controlled retry after reviewing the blocker.

The scheduled skill's API lock lives at `refs/heads/ghostline-automation/locks/day-N`: idle refs point at a saved base commit; active refs point at a unique lock commit. The connected GitHub ref update uses the expected prior SHA and never deletes the lock ref. The shell helper `scripts/day-lock.sh` remains covered by a local bare-remote concurrency test and Warden's pre-commit gate, but the scheduled API lock path is a separate implementation and has not yet been exercised by a scheduled run. The skill reports and preserves a lock after an unexpected termination rather than guessing whether it is stale.

The schedule is local and does not use a hosted model API key or GitHub Actions agent runner. GitHub Actions remains responsible for hosted CI and Nox checks. The `ready-for-agent` label is removed at pickup so later runs do not repeat the same ticket.

## Evidence

Record the schedule run, issue number, PR number, checks, Qodo review revision and findings, repair attempt, and human decision in the Day 4 Discussion. A PR timeline or commit timestamp does not establish causality. The schedule is not considered demonstrated until a run picks up a labeled issue and its outcome is inspected.

The shared-lock behavior can be reproduced locally with `bash .agents/skills/ghostline-ready-issue/scripts/test-day-lock.sh`; it uses a temporary bare Git remote and does not change the repository's GitHub refs. A release counts as successful when this run's SHA is gone; a different SHA means another run has already acquired the newly freed day lock.

## Run record — 2026-11-06

The local two-hour Codex schedule is active. Its latest recorded run launched the repo-local skill, read `AGENTS.md`, then stopped before queue selection because `gh auth status` in that scheduled shell exited with an invalid-token error. It made no issue, branch, worktree, PR, or source changes; no checks or review ran in that invocation. The result is recorded in the schedule's local run memory.

The user confirmed `gh` works in their other terminals; no account reauthentication is needed. The scheduled shell's earlier `gh auth status` was an execution-context-specific failure. In the host-approved context, `gh auth status`, `gh issue view`, `gh api repos/felixgeelhaar/ghostline --jq '.permissions.push'`, and a Git SSH dry-run succeeded. In the current task, the connected GitHub tools also read issue #29 and repository metadata with `permissions.push: true`; GitHub's current `main` SHA matches local `origin/main` at `445b8bb`. The active schedule prompt and skill now use the connected GitHub tools instead of the shell credential preflight. This updated scheduled path has not run yet.

Issue [#27](https://github.com/felixgeelhaar/ghostline/issues/27) is already represented by [PR #28](https://github.com/felixgeelhaar/ghostline/pull/28), so the skill will skip it. Issue [#29](https://github.com/felixgeelhaar/ghostline/issues/29), “Copy full commit SHA from revision timeline,” is now open with the maintainer-applied `ready-for-agent` label and explicit `Hackathon Day: 6` metadata. It is an eligible candidate for the next run, subject to the skill's author, duplicate-work, and daily-PR-cap checks. The schedule has not demonstrated a successful automatic ticket pickup. Day 4 remains partial until an eligible ticket is picked up and the scheduled run's outcome is inspected.

PR #28 is open at `afc7b19fd81fd658994ae8275d2246cf668ec6cd`. Qodo completed a structured review of code head `5c43489970224ec96d54e999535904ae2e84018a`; all findings returned for that run were attributed as implemented, including the earlier preflight concern. The earlier medium finding about overlapping runs was addressed in the shell day-lock helper and its concurrent two-clone test. The later documentation-only update at `b7a5be2` received a Qodo result marked `review_effort: skip` and `review_complete: false`; it is not a completed review of the current head or the new API lock procedure. These PR-level results are not results of a scheduled invocation.
