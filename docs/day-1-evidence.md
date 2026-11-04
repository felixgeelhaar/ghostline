# Ghostline Day 1 delivery evidence

## What works

- Enter a public GitHub pull request URL and import its repository, number, title, state, source link, and commits through the Go/Gin API.
- The Vue 3 + TypeScript interface validates the URL with Zod and presents the returned commits in chronological order.
- Commit times are labeled as commit times. They describe when a commit was authored, not when it was pushed or the complete event history of a pull request.
- Docker Compose starts the frontend and backend; the frontend waits for backend health. The frontend binds to `0.0.0.0` on the configured port for access from the local network. This is a development/demo deployment, not a public hosted service.
- The screenshot below was captured from the running Compose app after importing Ghostline PR #1; the API returned the live PR and its 15 commits.

![Ghostline showing the imported PR #1 and its commit timeline](assets/day-1-pr-timeline.png)

## Revision-linked checks

The implementation PR is [#1](https://github.com/felixgeelhaar/ghostline/pull/1), at head `c7166df77e4031807a4bf9ce62c128b497e77be3` when these results were recorded. GitHub reported:

- [Go checks passed](https://github.com/felixgeelhaar/ghostline/actions/runs/41788813200/job/126437100704).
- [Frontend checks passed](https://github.com/felixgeelhaar/ghostline/actions/runs/41788813200/job/126437101026).
- [Nox security scan passed](https://github.com/felixgeelhaar/ghostline/actions/runs/41788813168/job/126437100341).
- `warden/gate` passed on the pull request.

Nox findings are reported as security feedback; an incomplete scan fails the action. The accepted baseline items and tool coverage limits are documented in the repository. These checks are CI evidence for the cited PR revision; the running demo itself is available on the developer's local network and is not a public hosted URL. The Day 1 interface does not yet display GitHub check runs.

## Qodo review observation

Qodo caught an incorrect argument order in the Nox shell wrapper. Warden now invokes Nox directly, and the wrapper was removed. This was a useful review because it exposed a real integration defect at the boundary between the developer gate and the scanner. A later review comment still mentioned the deleted wrapper, so review feedback must be checked against the current diff before acting on it.

## Scope and next increment

This delivery imports a public pull request and shows its commit history. It does not yet attach Qodo review findings or validation events to revisions, diagnose repair loops, or prove that a later commit fixed a finding. Those require source-backed events and are follow-up work.
