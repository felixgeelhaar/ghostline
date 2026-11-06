# GitHub operations for scheduled runs

Use the connected GitHub tools. They use the linked account and are independent of the `gh` executable and SSH agent in a terminal. Do not copy tokens or switch accounts. If a required tool is unavailable or a write is denied, stop and report the exact operation; do not fall back to credentials from another context.

## Repository and issue reads

- Read repository metadata with `github_get_repo`; confirm `felixgeelhaar/ghostline`, default branch, and `permissions.push`.
- Read the published default-branch ref and any lock refs with `github_fetch` against `https://api.github.com/repos/felixgeelhaar/ghostline/git/ref/heads/<branch>`.
- Read a commit's tree with `github_fetch` against `https://api.github.com/repos/felixgeelhaar/ghostline/git/commits/<sha>`.
- Use `github_search_issues` for the oldest open `ready-for-agent` issues, then `github_fetch` on each issue's API URL to inspect its `author_association`; the normalized `github_fetch_issue` result may omit that field. Use PR search/fetch tools for duplicate checks. Preserve issue/PR links and native IDs.
- Confirm the local `origin/main` SHA is the same as the published default-branch SHA before worktree creation. If it differs, stop and report a stale checkout; do not build from an outdated base.

## Per-day compare-and-set lock

The persistent ref `ghostline-automation/locks/day-N` is either free (points at its saved base commit) or held (points at a small lock commit). Do not delete the ref.

1. Read the lock ref. If it does not exist, create it once with `github_create_branch` from the published default branch, then read it again. If another run created it concurrently, re-read rather than treating the create conflict as a failure.
2. Fetch the ref's current commit. If its commit message starts with `Ghostline automation lock day N`, treat it as held and stop. Do not infer that an old timestamp means the owner is inactive.
3. Fetch the current commit's tree SHA. Create a lock commit with that same tree, the current lock-ref SHA as its parent, and a message containing `Ghostline automation lock day N` plus this run's unique ID.
4. Acquire atomically with `github_update_ref`, setting the lock ref to the new commit with `expected_sha` equal to the SHA read in step 1 and `force=false`. A conflict means another run won; stop without claiming an issue. Record both the prior free SHA and the new lock SHA.
5. On release, first verify the lock ref still points to this run's lock SHA and its commit message contains this run ID. Call `github_update_ref` with `expected_sha` set to the lock SHA, `sha` set to the saved prior free SHA, and `force=true`. Re-read the ref to verify release. Never release another run's lock.

If a run ends unexpectedly while holding the lock, leave it in place and report the run ID and SHA for human recovery. A human may reset it to the recorded prior free SHA only after confirming no owner is active.

## Publish a locally verified commit

Run all configured checks locally first. Commit locally and keep the commit's first parent equal to the published default-branch SHA for the first PR commit. The GitHub tools publish the same patch as a Git commit; the API-created SHA may differ from the local SHA. Report and review the GitHub SHA as the PR revision.

1. Get the parent commit's tree SHA. Enumerate every path changed from the parent to local `HEAD`, including additions, modifications, and deletions. For each added or modified file, create a GitHub blob using the exact file bytes (`base64` for binary files). Preserve executable and symlink modes from the local Git tree. Represent deletions with a tree entry whose `sha` is `null`.
2. Call `github_create_tree` with the parent tree SHA and the complete changed-path entries. Call `github_create_commit` with the returned tree SHA, the exact parent SHA, and the local commit message.
3. Create the remote branch directly at the new GitHub commit with `github_create_branch` using its `sha` argument. Do not create it at the parent and then race to update it. If the branch already exists, stop and inspect it; never overwrite it.
4. Re-read the branch ref and confirm it points to the created commit. Then create the PR, including `Hackathon Day: N` and the issue URL. Verify the PR's head SHA and record that SHA before requesting Qodo review.

For a later repair commit, use the current PR head as the parent, publish its changed files the same way, then advance the existing PR branch with `github_update_ref` using `expected_sha` equal to the previously verified head and `force=false`. Re-read the ref and verify the new SHA. If the expected-SHA update fails, inspect the branch and stop rather than overwriting concurrent work.

Do not use this process to publish unverified changes. If a required binary artifact exceeds tool limits, the repo is stale, or an API operation's result is ambiguous, stop and report the blocker without claiming success.
