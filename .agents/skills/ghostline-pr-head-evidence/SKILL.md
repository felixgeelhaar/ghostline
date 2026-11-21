---
name: ghostline-pr-head-evidence
description: Check whether a Ghostline pull request's current head has a completed Qodo code review and every required check, and whether the branch is up to date, before calling gate ③ passed or asking the engineer to merge. Use when asked if a PR is ready to merge, after pushing or updating a PR branch, or before reporting a PR as verified.
---

# Ghostline PR head evidence

Gate ③ in `docs/factory-verification.md` passes only when Warden, the hosted checks and a completed Qodo review all cover the **exact current head** of the PR, and the branch is up to date with `main`. A review or check of an earlier head does not count. Timestamps and comment order don't establish coverage either: Qodo edits one "Code Review by Qodo" comment in place, and only its commit marker names the reviewed head.

This skill runs a read-only check and reports PASS, FAIL or INCOMPLETE. It never pushes, comments, requests a review, merges, or edits labels.

## Inputs

- **Required:** the PR number. Use the PR the user named, or the one you just pushed. Never guess.
- **Optional:** `OWNER/REPO`, default `felixgeelhaar/ghostline`.
- **Context:** a checkout of this repository, and `gh` (already authenticated) plus `jq` on `PATH`.

Stop and ask the user if no PR number is given, or if more than one PR could be meant.

## Steps

1. From the repository root, run:

   ```sh
   bash .agents/skills/ghostline-pr-head-evidence/scripts/head-evidence.sh PR_NUMBER
   ```

   If the user named a repository, pass it as the second argument (`… head-evidence.sh PR_NUMBER OWNER/REPO`) and check that the `pr:` line names it. Without it the script checks `felixgeelhaar/ghostline`.

   Keep the complete output; it is the evidence. Don't summarise it from memory.
2. Read the exit status and the `verdict:` line:
   - `0` PASS: the Qodo review names the head and has no High or Medium findings, every required check passed (or was skipped) on the head, and the branch is up to date. A check that passed on one rerun after a run that ended with no result (`cancelled`, `startup_failure` or `stale`) still passes; the script says so on its `check` line.
   - `1` FAIL: the head has a High or Medium Qodo finding, or a required check failed. A run that reported a result, such as `failure`, still fails the check even when a rerun on the same head passed (`docs/factory-verification.md`).
   - `2` INCOMPLETE: something is missing. For example, no review names the head, a required check is missing or still running, the branch is behind `main`, the PR is a draft or closed, the finding counts can't be read, a check needed more than one rerun after runs with no result, or an earlier run of a check timed out (a hang in the code or the runner can cause that, so the engineer decides).
   - `3` error: bad input, a missing tool, a failed `gh api` call, or an unreadable response. The `error:` line names the cause.
   - Any other status, or output with no `verdict:` line, is also an error: the script didn't finish. Never read a verdict into partial output.

   On an error, stop and report the exact output. Don't retry with different credentials, and don't edit the script during the check.
3. Confirm independently that the `head:` line matches `gh pr view PR_NUMBER -R OWNER/REPO --json headRefOid -q .headRefOid`, using the same repository as step 1. If they differ, a push happened during the check: run step 1 again once. If they still differ, stop and report.
4. Report to the user:
   - the PR and head SHA;
   - the verdict, with every reason line;
   - Qodo's finding counts. List every Low for the engineer: Lows don't block, but AGENTS.md requires them to be reported.

## When to stop and hand over

- **FAIL:** don't call the PR verified. Report the finding or check. Fixing it is a separate task under the repository's gate rules.
- **INCOMPLETE because "only the PR summary names the head":** Qodo often skips code review of a head that only merges `main`. Report this. Don't request a review or treat the older review as covering the head. The engineer decides, as described in `docs/factory-verification.md`.
- **INCOMPLETE because the branch is behind:** report it. Updating the branch is the engineer's or the delivery task's decision, and it creates a new head that needs this check again.
- **Checks or review still pending:** report what's pending. Don't wait more than the user asked.

## Evidence to save

When the check is part of a PR delivery, paste the script output (in a fenced block) and the step 3 head comparison into the PR description or the run report. Don't write files into the repository.
