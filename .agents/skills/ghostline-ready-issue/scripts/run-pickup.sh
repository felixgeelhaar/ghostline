#!/usr/bin/env bash
# Runs the ghostline-ready-issue skill once with Codex or Claude Code.
#
# GHOSTLINE_AGENT_RUNNERS  space-separated runner order (default: "codex claude").
# A later runner is tried only when both of these hold:
#   - the earlier runner's final output is a usage-limit or authentication error, and
#   - the GitHub queue, agent and lock refs, worktrees, and checkout are unchanged
#     since before it started, so it demonstrably did no work.
# Any other failure stops the run so a half-finished attempt is never repeated.
# GHOSTLINE_RUN_LOG_DIR    where run logs and the overlap lock live.
# GHOSTLINE_WORKTREE_DIR   where the issue worktree is created.
# GHOSTLINE_PROMPT_FILE    alternate prompt, e.g. a read-only smoke test.
# GHOSTLINE_REPO_SLUG      GitHub owner/name (default: felixgeelhaar/ghostline).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$script_dir" rev-parse --show-toplevel)"
runners="${GHOSTLINE_AGENT_RUNNERS:-codex claude}"
log_dir="${GHOSTLINE_RUN_LOG_DIR:-$HOME/.ghostline/runs}"
worktrees="${GHOSTLINE_WORKTREE_DIR:-${repo}-worktrees}"
slug="${GHOSTLINE_REPO_SLUG:-felixgeelhaar/ghostline}"
# Matched only against the end of a failed run's output, where the runner prints its fatal error.
limit_pattern='hit your usage limit|usage_limit_exceeded|usage limit reached|hit your limit|credit balance is too low|insufficient credits|quota exceeded|not logged in|please run /login|invalid api key|oauth token (has )?expired|authentication_error'

mkdir -p "$log_dir" "$worktrees"

# One run at a time. The kernel holds this lock until the wrapper exits, so there is
# no stale-lock recovery to race on.
exec 9>>"$log_dir/.run.flock"
if ! python3 -c 'import fcntl; fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)' 2>/dev/null; then
  echo "another pickup run is active; skipping" >&2
  exit 0
fi

prompt_file="${GHOSTLINE_PROMPT_FILE:-$script_dir/prompt.md}"
prompt="$(sed -e "s|{{REPO}}|${repo}|g" -e "s|{{WORKTREES}}|${worktrees}|g" "$prompt_file")"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
state_dir="$(mktemp -d)"
trap 'rm -rf "$state_dir"' EXIT

# Everything a pickup changes before it can fail part-way: the label queue, pushed
# agent branches, day-lock refs, the worktrees directory, and the checkout.
snapshot() {
  gh issue list --repo "$slug" --state open --label ready-for-agent --limit 500 \
    --json number --jq '[.[].number] | sort | map(tostring) | join(",")' &&
    gh api "repos/$slug/git/matching-refs/heads/agent/" --jq '[.[].ref] | sort | join(",")' &&
    gh api "repos/$slug/git/matching-refs/heads/ghostline-automation/" \
      --jq '[.[] | .ref + "@" + .object.sha] | sort | join(",")' &&
    ls -A "$worktrees" &&
    git -C "$repo" status --porcelain
}

claude_tools=(
  'Read' 'Edit' 'Write' 'Glob' 'Grep' 'mcp__scout'
  'Bash(git *)' 'Bash(gh *)' 'Bash(go *)' 'Bash(npm *)' 'Bash(npx *)'
  'Bash(warden *)' 'Bash(nox *)' 'Bash(bash .agents/skills/*)'
  'Bash(pwd)' 'Bash(ls *)' 'Bash(mkdir *)' 'Bash(date *)'
  'Bash(cat *)' 'Bash(head *)' 'Bash(tail *)' 'Bash(wc *)' 'Bash(sort *)'
  'Bash(grep *)' 'Bash(jq *)' 'Bash(echo *)' 'Bash(printf *)' 'Bash(test *)'
)

run_codex() {
  printf '%s\n' "$prompt" | codex exec -C "$repo" -s workspace-write \
    -c sandbox_workspace_write.network_access=true \
    --add-dir "$worktrees" --ephemeral -
}

run_claude() {
  local out status=0 report=0
  out="$state_dir/claude.json"
  (cd "$repo" && printf '%s\n' "$prompt" | claude -p --output-format json \
    --permission-mode dontAsk --add-dir "$worktrees" \
    --allowedTools "${claude_tools[@]}") >"$out" 2>&1 || status=$?
  # Print the report, any error the result carries, and every denied tool call.
  # A result with is_error is a failed run even when claude exits zero.
  python3 -c '
import json, sys
raw = open(sys.argv[1]).read()
try:
    d = json.loads(raw)
except ValueError:
    print(raw); sys.exit(0)
print(d.get("result", ""))
for x in d.get("permission_denials", []):
    print("PERMISSION DENIED:", x.get("tool_name"), json.dumps(x.get("tool_input"))[:400])
if d.get("is_error"):
    print("RUNNER ERROR:", d.get("subtype", "unknown"))
    for e in d.get("errors") or []:
        print("RUNNER ERROR:", e if isinstance(e, str) else json.dumps(e)[:400])
    sys.exit(3)
' "$out" || report=$?
  if [[ $status -ne 0 ]]; then return "$status"; fi
  return "$report"
}

can_fall_back=1
if ! snapshot >"$state_dir/before" 2>&1; then
  echo "could not record the pre-run state; a failed runner will not be retried with another" >&2
  can_fall_back=0
fi

for runner in $runners; do
  case "$runner" in
    codex | claude) ;;
    *) echo "unknown runner: $runner" >&2; exit 2 ;;
  esac
  command -v "$runner" >/dev/null 2>&1 || {
    echo "runner $runner is not installed; trying the next one" >&2
    continue
  }
  log="$log_dir/${stamp}-${runner}.log"
  echo "running pickup with $runner (log: $log)"
  status=0
  # The runner does not inherit the overlap lock's descriptor.
  "run_${runner}" >"$log" 2>&1 9>&- || status=$?
  cat "$log"
  [[ $status -eq 0 ]] && exit 0
  if ! tail -n 20 "$log" | grep -Eqi "$limit_pattern"; then
    echo "$runner failed (exit $status) for a reason that is not a limit; not retrying with another runner" >&2
    exit "$status"
  fi
  if [[ $can_fall_back -eq 0 ]]; then
    echo "$runner stopped on a limit, but without a pre-run state there is no proof it did no work; stopping" >&2
    exit "$status"
  fi
  if ! snapshot >"$state_dir/after" 2>&1 || ! cmp -s "$state_dir/before" "$state_dir/after"; then
    echo "$runner stopped on a limit after the queue, refs, worktrees, or checkout changed (or could not be read); stopping for human review" >&2
    exit "$status"
  fi
  echo "$runner stopped on a limit or authentication error before doing any work; trying the next runner" >&2
done
echo "no runner completed the pickup" >&2
exit 1
