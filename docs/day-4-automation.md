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

The local Codex schedule invokes `.agents/skills/ghostline-ready-issue/SKILL.md` every two hours. The skill preflights authenticated GitHub write access, searches open issues for the `[ready-for-agent]` title prefix, selects at most one, reads the issue and `AGENTS.md`, and works in an isolated branch/worktree. It removes the prefix when claiming the ticket to prevent duplicate runs. It follows DDD/TDD, runs the relevant checks, and allows one focused repair attempt. It opens a PR for Qodo review only after checks pass; it never merges. If there is no eligible issue, it exits without changes. If GitHub access, Qodo review, requirements, or checks are blocked, it reports the blocker and stops. Re-adding the prefix is a human-controlled retry after reviewing the blocker.

The schedule uses the local Codex session and its connected GitHub tools plus local GitHub credentials for writes; no hosted model API key or GitHub Actions agent runner is involved. GitHub Actions remains responsible for hosted CI and Nox checks. The title prefix is the queue marker and is removed at pickup so a later schedule does not repeat the same ticket.

## Evidence

Record the schedule run, issue number, PR number, checks, Qodo review revision and findings, repair attempt, and human decision in the Day 4 Discussion. A PR timeline or commit timestamp does not establish causality. The schedule is not considered demonstrated until a run picks up a labeled issue and its outcome is inspected.

## Preflight note — 2026-11-06

Issue [#27](https://github.com/felixgeelhaar/ghostline/issues/27) is queued, and the local two-hour Codex schedule is active. The focused implementation is in [PR #28](https://github.com/felixgeelhaar/ghostline/pull/28). Go tests, `go vet`, frontend tests/build, `git diff --check`, and both configured Warden gates passed, including Nox. GitHub reports the `warden/gate` status as successful, and Nox's PR action posted its scan summary. Qodo posted a PR summary; `/agentic_review` was then requested, but a code-review result is not available yet. The scheduled workflow has not fired, so its automated pickup is not yet demonstrated. The local GitHub CLI token is invalid and Qodo CLI is not signed in; the skill now verifies write access before claiming work and supports the repository's `/agentic_review` trigger when authenticated GitHub access is available.
