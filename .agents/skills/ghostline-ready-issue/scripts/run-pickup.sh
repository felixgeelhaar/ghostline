#!/usr/bin/env bash
# Runs the ghostline-ready-issue skill once with Codex or Claude Code.
#
# GHOSTLINE_AGENT_RUNNERS  space-separated runner order (default: "codex claude").
# A later runner is tried only when the earlier one failed before doing work
# because of a usage limit, quota, or authentication error. Any other failure
# stops the run so a half-finished attempt is never repeated by a second agent.
# GHOSTLINE_RUN_LOG_DIR    where run logs and the overlap lock live.
# GHOSTLINE_WORKTREE_DIR   where the issue worktree is created.
# GHOSTLINE_PROMPT_FILE    alternate prompt, e.g. a read-only smoke test.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$script_dir" rev-parse --show-toplevel)"
runners="${GHOSTLINE_AGENT_RUNNERS:-codex claude}"
log_dir="${GHOSTLINE_RUN_LOG_DIR:-$HOME/.ghostline/runs}"
worktrees="${GHOSTLINE_WORKTREE_DIR:-${repo}-worktrees}"
limit_pattern='usage limit|rate limit|quota|credit balance|insufficient credits|overloaded|not logged in|please run /login|invalid api key|authentication (failed|error)|401|429'

mkdir -p "$log_dir" "$worktrees"

# One run at a time, whichever runner started it.
lock="$log_dir/.run.lock"
if ! mkdir "$lock" 2>/dev/null; then
  holder="$(cat "$lock/pid" 2>/dev/null || true)"
  if [[ -n "$holder" ]] && kill -0 "$holder" 2>/dev/null; then
    echo "another pickup run is active (pid $holder); skipping" >&2
    exit 0
  fi
  rm -rf "$lock"
  mkdir "$lock"
fi
echo $$ > "$lock/pid"
trap 'rm -rf "$lock"' EXIT

prompt_file="${GHOSTLINE_PROMPT_FILE:-$script_dir/prompt.md}"
prompt="$(sed -e "s|{{REPO}}|${repo}|g" -e "s|{{WORKTREES}}|${worktrees}|g" "$prompt_file")"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

run_codex() {
  printf '%s\n' "$prompt" | codex exec -C "$repo" -s workspace-write \
    -c sandbox_workspace_write.network_access=true \
    --add-dir "$worktrees" --ephemeral -
}

run_claude() {
  (cd "$repo" && printf '%s\n' "$prompt" | claude -p \
    --permission-mode dontAsk --add-dir "$worktrees" \
    --allowedTools 'Read' 'Edit' 'Write' 'Glob' 'Grep' \
      'Bash(git *)' 'Bash(gh *)' 'Bash(go *)' 'Bash(npm *)' 'Bash(npx *)' \
      'Bash(warden *)' 'Bash(nox *)' 'Bash(bash .agents/skills/*)' \
      'Bash(cd *)' 'Bash(pwd)' 'Bash(ls *)' 'Bash(mkdir *)' 'Bash(date *)' \
      'Bash(cat *)' 'Bash(head *)' 'Bash(tail *)' 'Bash(wc *)' 'Bash(sort *)' \
      'Bash(grep *)' 'Bash(jq *)' 'Bash(echo *)' 'Bash(printf *)' 'Bash(test *)')
}

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
  set +e
  "run_${runner}" >"$log" 2>&1
  status=$?
  set -e
  cat "$log"
  [[ $status -eq 0 ]] && exit 0
  if grep -Eqi "$limit_pattern" "$log"; then
    echo "$runner stopped on a limit or authentication error; trying the next runner" >&2
    continue
  fi
  echo "$runner failed (exit $status) for a reason that is not a limit; not retrying with another runner" >&2
  exit "$status"
done
echo "no runner completed the pickup" >&2
exit 1
