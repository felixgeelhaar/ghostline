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

The local Codex schedule invokes `.agents/skills/ghostline-ready-issue/SKILL.md` every two hours. Before issue selection, the skill generates a run ID, checks that `origin` is Ghostline, and performs a non-mutating Git push dry run to check transport and SSH authentication. That dry run does not prove ref-write permission; successful acquisition of the per-day lock is the actual write check. An unattended run reports sandbox network restrictions and stops rather than requesting approval. The skill searches open issues labeled `ready-for-agent` oldest first, treating the maintainer-applied label as explicit approval. Issues from untrusted authors and tickets already represented by open PRs are skipped. It selects at most one eligible issue and requires an explicit `Hackathon Day: N` field. Before checking the daily PR cap or claiming the ticket, it atomically creates a unique remote Git ref for that day. Overlapping runs compete for that ref; only one can acquire it. The lock is held through PR creation and then released. Unexpected termination can leave a stale lock, which requires human verification and cleanup. New PRs carry the day marker and issue link. The skill reads the acceptance criteria and `AGENTS.md`, then works in an isolated branch/worktree. It removes the issue label when claiming the ticket to prevent duplicate runs. It follows DDD/TDD and allows at most one focused repair for a confirmed check failure and one for confirmed Qodo findings. It opens a PR for Qodo review only after checks pass; it never merges. If there is no eligible issue, it exits without changes. If GitHub access, Qodo review, requirements, or checks are blocked, it reports the blocker and stops. Reapplying the label is a human-controlled retry after reviewing the blocker.

The shared day lock lives at `refs/heads/ghostline-automation/locks/day-N`. `scripts/day-lock.sh` bases its unique lock commit on the published `origin/main`, uses Git's empty-ref lease when creating the ref, and verifies ownership before releasing it. It never publishes commits from the scheduled checkout. Lock-ref pushes skip local hooks because they only update coordination metadata; code commits and pushes still run Warden. If post-acquisition verification fails, it prints the lock SHA and attempts owner-checked cleanup. The test script launches two clones against a local bare remote concurrently and checks that exactly one wins, an unpublished local commit is not pushed, a non-owner cannot release the claim, and a later run can acquire the released lock. Warden pre-commit and GitHub Actions CI run this regression test.

The schedule is local and does not use a hosted model API key or GitHub Actions agent runner. GitHub Actions remains responsible for hosted CI and Nox checks. The `ready-for-agent` label is removed at pickup so later runs do not repeat the same ticket.

## Evidence

Record the schedule run, issue number, PR number, checks, Qodo review revision and findings, repair attempt, and human decision in the Day 4 Discussion. A PR timeline or commit timestamp does not establish causality. The schedule is not considered demonstrated until a run picks up a labeled issue and its outcome is inspected.

The shared-lock behavior can be reproduced locally with `bash .agents/skills/ghostline-ready-issue/scripts/test-day-lock.sh`; it uses a temporary bare Git remote and does not change the repository's GitHub refs. A release counts as successful when this run's SHA is gone; a different SHA means another run has already acquired the newly freed day lock.

## Run record — 2026-11-06

The local two-hour Codex schedule is active. Its latest recorded run launched the repo-local skill, read `AGENTS.md`, then stopped before queue selection because `gh auth status` in that scheduled shell exited with an invalid-token error. It made no issue, branch, worktree, PR, or source changes; no checks or review ran in that invocation. The result is recorded in the schedule's local run memory.

The user confirmed `gh` works in their other terminals. The restricted shell's `gh auth status` reported an invalid token, and its SSH preflight could not access the SSH agent socket or resolve GitHub. In the host-approved context, `gh auth status`, `gh issue view`, `gh api repos/felixgeelhaar/ghostline --jq '.permissions.push'`, and a Git SSH dry-run all succeeded; the API reported push permission. The skill now distinguishes a restricted execution context from invalid credentials, and the dry run is documented as a transport check rather than proof of write permission. The scheduled job has not yet run the updated preflight, so access from that scheduled execution remains unverified; these differing contexts do not establish that the user's GitHub setup is broken.

Issue [#27](https://github.com/felixgeelhaar/ghostline/issues/27) is already represented by [PR #28](https://github.com/felixgeelhaar/ghostline/pull/28), so the skill will skip it. Issue [#29](https://github.com/felixgeelhaar/ghostline/issues/29), “Copy full commit SHA from revision timeline,” is now open with the maintainer-applied `ready-for-agent` label and explicit `Hackathon Day: 6` metadata. It is an eligible candidate for the next run, subject to the skill's author, duplicate-work, and daily-PR-cap checks. The schedule has not demonstrated a successful automatic ticket pickup. Day 4 remains partial until an eligible ticket is picked up and the scheduled run's outcome is inspected.

PR #28 is open at `5c43489970224ec96d54e999535904ae2e84018a`. Qodo completed a structured review of this exact head. Its preflight concern about run-ID ordering, remote identity, and dry-run semantics was addressed; the review attributes the finding as implemented. The earlier medium finding about overlapping runs was addressed with the remote day-lock helper and a concurrent two-clone test. These PR-level results are not results of a scheduled invocation.
