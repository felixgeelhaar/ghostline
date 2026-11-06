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

The local Codex schedule invokes `.agents/skills/ghostline-ready-issue/SKILL.md` every two hours. The skill verifies Git write access with a dry-run push, searches open issues labeled `ready-for-agent` oldest first, and treats the maintainer-applied label as explicit approval. Issues from untrusted authors and tickets already represented by open PRs are skipped. It selects at most one eligible issue and requires an explicit `Hackathon Day: N` field. Before checking the daily PR cap or claiming the ticket, it atomically creates a unique remote Git ref for that day. Overlapping runs compete for that ref; only one can acquire it. The lock is held through PR creation and then released. Unexpected termination can leave a stale lock, which requires human verification and cleanup. New PRs carry the day marker and issue link. The skill reads the acceptance criteria and `AGENTS.md`, then works in an isolated branch/worktree. It removes the issue label when claiming the ticket to prevent duplicate runs. It follows DDD/TDD and allows at most one focused repair for a confirmed check failure and one for confirmed Qodo findings. It opens a PR for Qodo review only after checks pass; it never merges. If there is no eligible issue, it exits without changes. If GitHub access, Qodo review, requirements, or checks are blocked, it reports the blocker and stops. Reapplying the label is a human-controlled retry after reviewing the blocker.

The shared day lock lives at `refs/heads/ghostline-automation/locks/day-N`. `scripts/day-lock.sh` bases its unique lock commit on the published `origin/main`, uses Git's empty-ref lease when creating the ref, and verifies ownership before releasing it. It never publishes commits from the scheduled checkout. Lock-ref pushes skip local hooks because they only update coordination metadata; code commits and pushes still run Warden. If post-acquisition verification fails, it prints the lock SHA and attempts owner-checked cleanup. The test script launches two clones against a local bare remote concurrently and checks that exactly one wins, an unpublished local commit is not pushed, a non-owner cannot release the claim, and a later run can acquire the released lock. Warden pre-commit and GitHub Actions CI run this regression test.

The schedule is local and does not use a hosted model API key or GitHub Actions agent runner. GitHub Actions remains responsible for hosted CI and Nox checks. The `ready-for-agent` label is removed at pickup so later runs do not repeat the same ticket.

## Evidence

Record the schedule run, issue number, PR number, checks, Qodo review revision and findings, repair attempt, and human decision in the Day 4 Discussion. A PR timeline or commit timestamp does not establish causality. The schedule is not considered demonstrated until a run picks up a labeled issue and its outcome is inspected.

The shared-lock behavior can be reproduced locally with `bash .agents/skills/ghostline-ready-issue/scripts/test-day-lock.sh`; it uses a temporary bare Git remote and does not change the repository's GitHub refs. A release counts as successful when this run's SHA is gone; a different SHA means another run has already acquired the newly freed day lock.

## Run record — 2026-11-06

The local two-hour Codex schedule is active. Its latest recorded run launched the repo-local skill, read `AGENTS.md`, then stopped before queue selection because `gh auth status` in that scheduled shell exited with an invalid-token error. It made no issue, branch, worktree, PR, or source changes; no checks or review ran in that invocation. The result is recorded in the schedule's local run memory.

The user confirmed `gh` works in their other terminals. In this execution, the sandboxed `gh auth status` reported an invalid token, while an approved host-context check succeeded via the keychain and `gh api repos/felixgeelhaar/ghostline --jq '.permissions.push // false'` returned `true`. The sandboxed Git SSH dry-run also succeeded without changing a remote ref. The skill now uses that Git transport check instead of depending on the GitHub CLI token. The scheduled job has not yet run the updated preflight, so access from that scheduled execution remains unverified; these differing contexts do not establish that the user's GitHub setup is broken.

The connected GitHub search on 2026-11-06 returned no open issues labeled `ready-for-agent`. Issue [#27](https://github.com/felixgeelhaar/ghostline/issues/27) has the label text only in its title and already has the open implementation [PR #28](https://github.com/felixgeelhaar/ghostline/pull/28), so it is not eligible. No other labeled, unrepresented ready issue is currently available. The schedule has not demonstrated a successful automatic ticket pickup. Day 4 remains partial until an eligible ticket is picked up and the scheduled run's outcome is inspected.

PR #28 was open at `90181217cd23254a1a4e9b24ccb83447e5ae1e30` when this automation update began. The connected GitHub view reported it open and mergeable. Qodo posted a medium finding on that head: overlapping runs could open two PRs for one day. The remote day-lock helper and a concurrent two-clone test were added in response. A structured Qodo review record was read through the approved host context; it identified the race finding and showed that older findings applied to older revisions. These PR-level results are not results of a scheduled invocation. The local sandbox's GitHub CLI token status can differ from the user's other terminal sessions; the skill therefore checks Git's own write path and prefers the connected GitHub integration for issue operations.
