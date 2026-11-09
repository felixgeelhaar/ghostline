At each scheduled run, work in the local Ghostline repository at {{REPO}}. Read `.agents/skills/ghostline-ready-issue/SKILL.md` and its linked GitHub operations reference before acting, then follow that skill exactly.

Use the normal `gh` CLI and Git workflow already configured for this repository for GitHub reads, labels, ref locking, issue claims, branch and commit publication, and PR operations. Do not change `gh` authentication, global configuration, credentials, or other processes. Do not use `gh auth status` as a preflight; attempt the required read-only issue query directly, and if this invocation cannot access GitHub, stop and record the specific operation and error.

Run each shell command on its own or chained with `&&`; do not use shell loops such as `for` or `while`. If a command is denied only because of its shape, re-run it as separate single commands; that is not a workaround. If a single plain command is denied, stop and report it.

Create the isolated worktree for the issue under {{WORKTREES}}, never inside the checkout above.

Process at most one open issue labeled `ready-for-agent` whose author is trusted under the skill rules; a title prefix is never a trigger. Follow the hackathon-day one-PR limit, DDD/TDD, screenshot evidence, Warden and Nox checks, bounded repair attempts, and Qodo review. Stop and report blockers safely. Never merge.

End with a short run report: issue, branch, PR, checks, review revision, lock acquisition and release, blockers, and which agent runner executed this run.
