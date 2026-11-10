#!/usr/bin/env bash
# Verifies run-pickup.sh with stub codex, claude, and gh binaries; no model or GitHub call is made.
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_COMMON_DIR
unset GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
# Run the wrapper from a throwaway checkout so stub runners can change local history.
checkout="$tmp/repo"
git init --quiet --initial-branch=main "$checkout"
git -C "$checkout" config user.name "Ghostline Runner Test"
git -C "$checkout" config user.email "ghostline-runner-test@example.invalid"
git -C "$checkout" config commit.gpgsign false
mkdir -p "$checkout/.agents/skills/ghostline-ready-issue/scripts"
cp "$script_dir/run-pickup.sh" "$script_dir/prompt.md" "$checkout/.agents/skills/ghostline-ready-issue/scripts/"
git -C "$checkout" add -A && git -C "$checkout" commit --quiet -m "fixture"
wrapper="$checkout/.agents/skills/ghostline-ready-issue/scripts/run-pickup.sh"
export GHOSTLINE_RUN_LOG_DIR="$tmp/logs" GHOSTLINE_WORKTREE_DIR="$tmp/wt" PATH="$tmp/bin:$PATH"

fail() { echo "FAIL: $*" >&2; exit 1; }

# stub NAME EXIT OUTPUT [ACTION]: ACTION is shell run before printing, to simulate work.
stub() {
  printf '%b\n' "$3" > "$tmp/$1-output"
  cat >"$tmp/bin/$1" <<EOF
#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' "\$@" > "$tmp/$1-args"
echo "$1 ran" >> "$tmp/calls"
${4:-:}
cat "$tmp/$1-output"
exit $2
EOF
  chmod +x "$tmp/bin/$1"
}
# The gh stub reports the "remote state" kept in a file, or fails when asked to.
# Like gh, it returns only the first N records when given --limit N.
cat >"$tmp/bin/gh" <<EOF
#!/usr/bin/env bash
[[ -f "$tmp/gh-fail" ]] && { echo "gh: network error" >&2; exit 1; }
limit=""
while [[ \$# -gt 0 ]]; do [[ "\$1" == --limit ]] && limit="\$2"; shift; done
if [[ -n "\$limit" ]]; then head -n "\$limit" "$tmp/state"; else cat "$tmp/state"; fi
EOF
chmod +x "$tmp/bin/gh"

calls() { if [[ -f "$tmp/calls" ]]; then tr '\n' ' ' < "$tmp/calls"; fi; }
reset() { rm -rf "$tmp/calls" "$tmp/gh-fail" "$tmp/wt"; echo "issues=29" > "$tmp/state"; }
run() { bash "$wrapper" >"$tmp/out" 2>&1; }

limit_msg="ERROR: You've hit your usage limit. Try again at 7:53 AM."
claude_ok='{"type":"result","subtype":"success","is_error":false,"result":"done"}'

# A usage limit before any work falls back to the next runner.
stub codex 1 "$limit_msg"; stub claude 0 "$claude_ok"
reset; run || fail "fallback run failed: $(cat "$tmp/out")"
[[ "$(calls)" == "codex ran claude ran " ]] || fail "limit did not fall back: $(calls)"

# Any other failure stops the run.
stub codex 1 "compile error in the repo"
reset; if run; then fail "non-limit failure should fail the run"; fi
[[ "$(calls)" == "codex ran " ]] || fail "non-limit failure fell back: $(calls)"

# Limit text early in the log does not count when the run ended on another error.
stub codex 1 "429 seen in a test fixture$(printf '\\nline%.0s' {1..25})\\ncompile error"
reset; if run; then fail "old limit text should not cause a fallback"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back on stale limit text: $(calls)"

# A limit hit after the runner changed GitHub state must not start a second agent.
stub codex 1 "$limit_msg" "echo issues= > '$tmp/state'"
reset; if run; then fail "limit after remote work should fail the run"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back after remote work: $(calls)"

# Nor after it created a worktree.
stub codex 1 "$limit_msg" "mkdir -p '$tmp/wt/issue-29'"
reset; if run; then fail "limit after local work should fail the run"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back after local work: $(calls)"

# Nor after it made a clean local commit (git status stays empty).
stub codex 1 "$limit_msg" "git -C '$checkout' commit --quiet --allow-empty -m work"
reset; if run; then fail "limit after a local commit should fail the run"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back after a local commit: $(calls)"

# Nor after it switched the checkout to another clean branch.
stub codex 1 "$limit_msg" "git -C '$checkout' switch --quiet -c agent/issue-29"
reset; if run; then fail "limit after a branch switch should fail the run"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back after a branch switch: $(calls)"
git -C "$checkout" switch --quiet main

# Nor after it claimed an issue beyond the first 500 in the queue.
stub codex 1 "$limit_msg" "grep -vx 550 '$tmp/state' > '$tmp/state.next'; mv '$tmp/state.next' '$tmp/state'"
reset; seq 1 600 > "$tmp/state"; if run; then fail "limit after a deep-queue claim should fail the run"; fi
if grep -qx 550 "$tmp/state"; then fail "the stub did not claim issue 550"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back after a deep-queue claim: $(calls)"

# Without a readable state snapshot there is no evidence that nothing happened.
stub codex 1 "$limit_msg"
reset; touch "$tmp/gh-fail"; if run; then fail "unreadable state should fail the run"; fi
[[ "$(calls)" == "codex ran " ]] || fail "fell back without a state snapshot: $(calls)"

# Claude can report an error in its JSON result while exiting zero.
stub claude 0 '{"type":"result","is_error":true,"subtype":"success","result":"Credit balance is too low","errors":[]}'
stub codex 0 "done"
reset; GHOSTLINE_AGENT_RUNNERS="claude codex" run || fail "claude limit fallback failed: $(cat "$tmp/out")"
[[ "$(calls)" == "claude ran codex ran " ]] || fail "claude is_error limit did not fall back: $(calls)"
stub claude 0 '{"type":"result","is_error":true,"subtype":"error_during_execution","result":"","errors":["tool crashed"]}'
reset; if GHOSTLINE_AGENT_RUNNERS="claude codex" run; then fail "claude is_error should fail the run"; fi
[[ "$(calls)" == "claude ran " ]] || fail "claude is_error fell back: $(calls)"
grep -q "tool crashed" "$tmp/out" || fail "claude errors were not logged: $(cat "$tmp/out")"

# Empty or malformed Claude output with exit 0 is an unknown outcome, not a success.
for bad in "" "not json" '{"result":"done"}' '[1,2]'; do
  stub claude 0 "$bad"
  reset; if GHOSTLINE_AGENT_RUNNERS="claude codex" run; then fail "malformed output '$bad' counted as success"; fi
  [[ "$(calls)" == "claude ran " ]] || fail "malformed output '$bad' fell back: $(calls)"
done
# An unknown outcome never falls back, even when its text mentions a limit.
stub claude 0 "$limit_msg"
reset; if GHOSTLINE_AGENT_RUNNERS="claude codex" run; then fail "unknown outcome with limit text counted as success"; fi
[[ "$(calls)" == "claude ran " ]] || fail "unknown outcome with limit text fell back: $(calls)"
# A CLI startup failure (non-zero exit, plain text) is still classified normally.
stub claude 1 "Not logged in · Please run /login"
reset; GHOSTLINE_AGENT_RUNNERS="claude codex" run || fail "claude startup limit did not fall back: $(cat "$tmp/out")"
[[ "$(calls)" == "claude ran codex ran " ]] || fail "claude startup limit did not fall back: $(calls)"

# Runner order is configurable.
stub codex 0 "done"; stub claude 0 "$claude_ok"
reset; GHOSTLINE_AGENT_RUNNERS="claude codex" run
[[ "$(calls)" == "claude ran " ]] || fail "runner order ignored: $(calls)"

# Claude gets exactly this permission boundary. Change it here deliberately, with review.
expected_args="-p
--output-format
json
--permission-mode
dontAsk
--add-dir
$tmp/wt
--allowedTools
Read
Edit
Write
Glob
Grep
mcp__scout
Bash(git *)
Bash(gh *)
Bash(go *)
Bash(npm *)
Bash(npx *)
Bash(warden *)
Bash(nox *)
Bash(bash .agents/skills/*)
Bash(pwd)
Bash(ls *)
Bash(mkdir *)
Bash(date *)
Bash(cat *)
Bash(head *)
Bash(tail *)
Bash(wc *)
Bash(sort *)
Bash(grep *)
Bash(jq *)
Bash(echo *)
Bash(printf *)
Bash(test *)"
[[ "$(cat "$tmp/claude-args")" == "$expected_args" ]] || {
  diff <(printf '%s\n' "$expected_args") "$tmp/claude-args" >&2 || true
  fail "claude allowlist changed"
}

# All runners limited fails.
stub codex 1 "$limit_msg"; stub claude 1 "You've hit your limit"
reset; if run; then fail "all runners limited should fail"; fi

# Only one of two simultaneous invocations runs an agent.
stub codex 0 "done" "sleep 2"
reset
bash "$wrapper" >/dev/null 2>&1 & first=$!
bash "$wrapper" >/dev/null 2>&1 & second=$!
wait "$first" "$second"
[[ "$(calls)" == "codex ran " ]] || fail "overlapping invocations both ran: $(calls)"

# A lock held by another live process is respected.
reset
python3 -c 'import fcntl, sys, time
f = open(sys.argv[1], "a"); fcntl.flock(f, fcntl.LOCK_EX); open(sys.argv[2], "w").close(); time.sleep(3)' \
  "$tmp/logs/.run.flock" "$tmp/held" &
holder=$!
until [[ -f "$tmp/held" ]]; do sleep 0.1; done
run
wait "$holder"
[[ -z "$(calls)" ]] || fail "run started while the lock was held: $(calls)"

echo "runner fallback, state guard, error results, allowlist, ordering, and overlap lock passed"
