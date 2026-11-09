#!/usr/bin/env bash
# Verifies runner fallback with stub codex/claude binaries; no model is called.
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
export GHOSTLINE_RUN_LOG_DIR="$tmp/logs" GHOSTLINE_WORKTREE_DIR="$tmp/wt" PATH="$tmp/bin:$PATH"

stub() { # name exit-code message
  printf '#!/usr/bin/env bash\ncat >/dev/null\necho "%s"\necho "%s ran" >> "%s/calls"\nexit %s\n' "$3" "$1" "$tmp" "$2" > "$tmp/bin/$1"
  chmod +x "$tmp/bin/$1"
}
calls() { if [[ -f "$tmp/calls" ]]; then tr '\n' ' ' < "$tmp/calls"; fi; }
reset() { rm -f "$tmp/calls"; }

stub codex 1 "You've hit your usage limit."; stub claude 0 "done"
reset; bash "$script_dir/run-pickup.sh" >/dev/null 2>&1
[[ "$(calls)" == "codex ran claude ran " ]] || { echo "limit did not fall back: $(calls)" >&2; exit 1; }

stub codex 1 "compile error in the repo"; stub claude 0 "done"
reset; if bash "$script_dir/run-pickup.sh" >/dev/null 2>&1; then echo "non-limit failure should fail the run" >&2; exit 1; fi
[[ "$(calls)" == "codex ran " ]] || { echo "non-limit failure fell back: $(calls)" >&2; exit 1; }

stub codex 0 "done"; stub claude 0 "done"
reset; GHOSTLINE_AGENT_RUNNERS="claude codex" bash "$script_dir/run-pickup.sh" >/dev/null 2>&1
[[ "$(calls)" == "claude ran " ]] || { echo "runner order ignored: $(calls)" >&2; exit 1; }

stub codex 1 "usage limit"; stub claude 1 "rate limit"
reset; if bash "$script_dir/run-pickup.sh" >/dev/null 2>&1; then echo "all runners limited should fail" >&2; exit 1; fi

mkdir -p "$tmp/logs/.run.lock"; echo $$ > "$tmp/logs/.run.lock/pid"
reset; bash "$script_dir/run-pickup.sh" >/dev/null 2>&1
[[ -z "$(calls)" ]] || { echo "overlapping run was not skipped" >&2; exit 1; }
echo "runner fallback, ordering, and overlap lock passed"
