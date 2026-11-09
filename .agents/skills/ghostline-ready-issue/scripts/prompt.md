At each scheduled run, work in the local Ghostline repository at {{REPO}}. Read `.agents/skills/ghostline-ready-issue/SKILL.md` and its linked GitHub operations reference before acting, then follow that skill exactly.

Use the normal `gh` CLI and Git workflow already configured for this repository for GitHub reads, labels, ref locking, issue claims, branch and commit publication, and PR operations. Do not change `gh` authentication, global configuration, credentials, or other processes. Do not use `gh auth status` as a preflight; attempt the required read-only issue query directly, and if this invocation cannot access GitHub, stop and record the specific operation and error.

Command rules for unattended runs (the tool allowlist matches command prefixes):
- Never start a command with `cd`. Address other directories with `git -C <dir>`, `go -C <dir>`, `npm --prefix <dir>`, and absolute paths. Run the day-lock helper from this checkout as `bash .agents/skills/ghostline-ready-issue/scripts/day-lock.sh ...`.
- Change files with the Edit and Write tools, not shell redirects (`>`, `>>`), `sed -i`, or heredocs. Do not use command substitution (`$(...)`) or shell loops; write literal values, for example a run ID such as `run-YYYYMMDD-HHMM` typed out.
- Ports 8080 and 5173 belong to other local services; never stop them. Start the backend as `PORT=18080 go -C <worktree> run ./backend` and the frontend as `GHOSTLINE_API_URL=http://localhost:18080 npm --prefix <worktree>/frontend run dev -- --port 15173 --strictPort`, both in the background, and take screenshots of `http://localhost:15173` with the Scout browser tools. Save screenshots inside the worktree and stop both servers before finishing.
- If a command is denied only because of its shape, re-run it in an allowed shape; that is not a workaround. If an allowed-shape command is still denied, stop and report it.

Create the isolated worktree for the issue under {{WORKTREES}}, never inside the checkout above.

Process at most one open issue labeled `ready-for-agent` whose author is trusted under the skill rules; a title prefix is never a trigger. Follow the hackathon-day one-PR limit, DDD/TDD, screenshot evidence, Warden and Nox checks, bounded repair attempts, and Qodo review. Stop and report blockers safely. Never merge.

End with a short run report: issue, branch, PR, checks, review revision, lock acquisition and release, blockers, and which agent runner executed this run.
