#!/usr/bin/env bash
# Verifies that dev-servers.sh stops only the processes it recorded; no server is started.
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
servers="$script_dir/dev-servers.sh"
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_COMMON_DIR
tmp="$(mktemp -d)"
others=()
cleanup() {
  for p in ${others[@]+"${others[@]}"}; do kill "$p" 2>/dev/null || true; done
  rm -rf "$tmp"
}
trap cleanup EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
alive() { kill -0 "$1" 2>/dev/null; }

worktree="$tmp/issue-1"
git init --quiet "$worktree"
state="$(git -C "$worktree" rev-parse --path-format=absolute --git-path ghostline-dev-servers)"
mkdir -p "$state"

# Arguments are validated.
if bash "$servers" start "$tmp/missing" >/dev/null 2>&1; then fail "missing worktree accepted"; fi
mkdir -p "$tmp/plain"
if bash "$servers" start "$tmp/plain" >/dev/null 2>&1; then fail "non-Git directory accepted"; fi
if bash "$servers" restart "$worktree" >/dev/null 2>&1; then fail "unknown action accepted"; fi

# Stopping with nothing recorded is a no-op.
bash "$servers" stop "$worktree" >/dev/null || fail "stop without a record failed"

# A recorded process tree (parent and child, like `go run` or `npm run dev`) is stopped.
bash -c 'sleep 60 & wait' & recorded=$!
others+=("$recorded")
until pgrep -P "$recorded" >/dev/null; do sleep 0.1; done
child="$(pgrep -P "$recorded")"
others+=("$child")
# An unrelated process, and a recorded PID whose start time no longer matches (reused PID).
sleep 60 & unrelated=$!
others+=("$unrelated")
sleep 60 & reused=$!
others+=("$reused")
printf '%s\t%s\n' "$recorded" "$(ps -o lstart= -p "$recorded")" >"$state/pids"
printf '%s\t%s\n' "$reused" "Thu Jan  1 00:00:00 1970" >>"$state/pids"

bash "$servers" stop "$worktree" >/dev/null || fail "stop failed"
sleep 0.5
if alive "$recorded"; then fail "recorded process still running"; fi
if alive "$child"; then fail "recorded process's child still running"; fi
alive "$unrelated" || fail "unrelated process was stopped"
alive "$reused" || fail "a reused PID was stopped"
[[ ! -e "$state/pids" ]] || fail "record not removed after stop"

echo "dev server validation and owner-checked stop passed"
