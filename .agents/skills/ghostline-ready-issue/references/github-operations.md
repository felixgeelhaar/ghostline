# GitHub operations for scheduled runs

Use the repository's normal `gh` CLI and Git remotes. The scheduled job must not modify global GitHub configuration, switch credentials, print tokens, or use an unrelated account. Do not run `gh auth status` as a preflight: attempt the actual read-only operation and report its exact failure if this scheduled invocation cannot access GitHub.

## Repository, issue, and PR reads

- Read repository/default branch metadata with `gh api repos/felixgeelhaar/ghostline`.
- Read the published default-branch ref with `gh api repos/felixgeelhaar/ghostline/git/ref/heads/main`; retain `object.sha` as the published SHA.
- Refresh local `origin/main` with `git fetch --no-tags https://github.com/felixgeelhaar/ghostline.git +refs/heads/main:refs/remotes/origin/main`, then compare `git rev-parse origin/main` to the published SHA. Stop if they differ. Create the issue worktree from that verified `origin/main` SHA, never from a ref you did not just verify.
- Search oldest eligible issues with `gh api --paginate "repos/felixgeelhaar/ghostline/issues?state=open&labels=ready-for-agent&per_page=100&sort=created&direction=asc" --jq '.[] | select(.pull_request | not) | {number, title, user: .user.login, author_association, created_at, html_url}'`. The REST `author_association` field is the trust check; `gh issue list --json` does not expose it. Read one issue's complete content with `gh api repos/felixgeelhaar/ghostline/issues/N --jq '{number, title, body, state, author_association, labels: [.labels[].name], html_url}'`.
- Inspect open PRs with `gh api --paginate "repos/felixgeelhaar/ghostline/pulls?state=open&per_page=100" --jq '.[] | {number, title, body, user: .user.login, author_association, head: .head.ref, head_sha: .head.sha, html_url}'`. For the selected PR, use `gh pr view N --repo felixgeelhaar/ghostline --json headRefOid,baseRefOid,state,url` to bind review/check evidence to its current revision.
- `--paginate` reads every page. If a paginated read fails part-way, stop; never select an issue or declare a day unoccupied from a partial list.
- Run one GitHub or Git command per shell call, or chain allowed commands with `&&`/`;`. Do not wrap them in shell loops (`for`, `while`) or subshell scripts: the unattended Claude runner's allowlist matches command prefixes and denies loops.
- Preserve GitHub native issue/PR numbers, URLs, author association, and commit SHAs in the run report.

## Per-day compare-and-set lock

The persistent ref `ghostline-automation/locks/day-N` is either free (points at its saved base commit) or held (points at a small lock commit). Do not delete it.

1. The lock helper fetches `origin/main` and reads the lock ref. If absent, it claims the empty ref with Git's server-checked `--force-with-lease`. If present, fetch and inspect the lock commit. A message starting `Ghostline automation lock day N run` means held; stop. Otherwise this is the saved idle ref.
2. The helper creates a lock commit with the idle commit's tree and idle commit as its parent, then pushes it using `--force-with-lease=<ref>:<observed-idle-sha>`. For a ref that was absent, it uses an empty expected SHA. GitHub rejects the push if the ref changed after observation, so concurrent contenders cannot both acquire it.
3. Record the run ID, prior free SHA, and lock SHA printed by the helper. Keep it until PR creation or a stop condition, then release with the exact run ID and lock SHA.
4. Release verifies the ref still points to the owned lock SHA and that the commit message has the run ID; then it atomically restores the prior free SHA with another expected-SHA lease. Verify the ref equals that saved SHA afterward. Never delete the persistent ref or release another run's lock.

A stranded lock is not proof that its owner stopped. Leave it for human recovery after confirming no run is active. A lock whose message reads `Ghostline automation lock for day N, run ID` came from the earlier script and is also treated as held; a human resets the ref to its parent commit after confirming no run is active. After a release the idle ref may point at an older `main` commit; acquire keys on the lock message, not the SHA, so this is expected.

## Publish the verified change

Run configured checks locally before committing. Keep the first PR commit's parent equal to the refreshed published default-branch SHA. Push the local branch with `git push -u origin BRANCH`; do not force-push. Create one PR with `gh pr create`, including `Hackathon Day: N` and the exact issue URL in the body. Read the PR head SHA back with `gh pr view N --json headRefOid,url` before requesting Qodo review. For a repair, commit on the same branch, push normally, then verify the new head SHA and request a fresh Qodo review. Never merge. If a required operation fails or its result is ambiguous, stop, release an owned day lock if safe, and report the exact operation and revision.
