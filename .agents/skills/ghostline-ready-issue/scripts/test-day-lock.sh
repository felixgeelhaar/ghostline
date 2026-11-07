#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
lock_script="$repo_root/.agents/skills/ghostline-ready-issue/scripts/day-lock.sh"
# Git hooks export the caller's index/worktree paths. Keep temporary clones isolated.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_COMMON_DIR
unset GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

remote="$tmp_dir/remote.git"
run_one="$tmp_dir/run-one"
run_two="$tmp_dir/run-two"
git init --bare --initial-branch=main "$remote" >/dev/null
git init --initial-branch=main "$run_one" >/dev/null
mkdir -p "$tmp_dir/empty-hooks"
git -C "$run_one" config user.name "Ghostline Lock Test"
git -C "$run_one" config user.email "ghostline-lock-test@example.invalid"
git -C "$run_one" config commit.gpgsign false
git -C "$run_one" config core.hooksPath "$tmp_dir/empty-hooks"
printf 'base\n' > "$run_one/base.txt"
git -C "$run_one" add base.txt
git -C "$run_one" commit -m "Create lock test base" >/dev/null
git -C "$run_one" remote add origin "$remote"
git -C "$run_one" push --set-upstream origin main >/dev/null
git clone --branch main "$remote" "$run_two" >/dev/null 2>&1
published_main="$(git --git-dir="$remote" rev-parse refs/heads/main)"
remote_ref="refs/heads/ghostline-automation/locks/day-4"
# Match the API lock protocol's persistent idle ref before a scheduled run.
git --git-dir="$remote" update-ref "$remote_ref" "$published_main"
printf 'unpublished local work\n' > "$run_one/unpublished.txt"
git -C "$run_one" add unpublished.txt
git -C "$run_one" commit -m "Unpublished local work must not be pushed by lock" >/dev/null

mkdir -p "$tmp_dir/outcomes"
(
  if (cd "$run_one" && bash "$lock_script" acquire 4 run-one-01 > "$tmp_dir/outcomes/one.sha" 2> "$tmp_dir/outcomes/one.err"); then
    echo acquired > "$tmp_dir/outcomes/one.status"
  else
    echo rejected > "$tmp_dir/outcomes/one.status"
  fi
) &
first_pid=$!
(
  if (cd "$run_two" && bash "$lock_script" acquire 4 run-two-01 > "$tmp_dir/outcomes/two.sha" 2> "$tmp_dir/outcomes/two.err"); then
    echo acquired > "$tmp_dir/outcomes/two.status"
  else
    echo rejected > "$tmp_dir/outcomes/two.status"
  fi
) &
second_pid=$!
wait "$first_pid"
wait "$second_pid"

one_status="$(cat "$tmp_dir/outcomes/one.status")"
two_status="$(cat "$tmp_dir/outcomes/two.status")"
if [[ "$one_status" == "$two_status" ]]; then
  echo "concurrent runs did not produce exactly one lock winner" >&2
  cat "$tmp_dir/outcomes/one.err" "$tmp_dir/outcomes/two.err" >&2
  exit 1
fi
if [[ "$one_status" == acquired ]]; then
  winner_dir="$run_one"
  winner_run_id="run-one-01"
  winner_sha="$(cat "$tmp_dir/outcomes/one.sha")"
  loser_dir="$run_two"
else
  winner_dir="$run_two"
  winner_run_id="run-two-01"
  winner_sha="$(cat "$tmp_dir/outcomes/two.sha")"
  loser_dir="$run_one"
fi
[[ "$(git --git-dir="$remote" rev-parse "$remote_ref")" == "$winner_sha" ]]
[[ "$(git -C "$winner_dir" rev-parse "${winner_sha}^1")" == "$published_main" ]]
if git --git-dir="$remote" cat-file -e "$(git -C "$run_one" rev-parse HEAD)^{commit}" 2>/dev/null; then
  echo "day lock unexpectedly published an unpushed local commit" >&2
  exit 1
fi

if (cd "$loser_dir" && bash "$lock_script" release 4 wrong-run-02 "$winner_sha" >/dev/null 2>&1); then
  echo "a non-owner unexpectedly released the day lock" >&2
  exit 1
fi
(cd "$winner_dir" && bash "$lock_script" release 4 "$winner_run_id" "$winner_sha")
if [[ "$(git --git-dir="$remote" rev-parse "$remote_ref")" != "$published_main" ]]; then
  echo "day lock did not return to the saved idle ref after release" >&2
  exit 1
fi

second_lock="$(cd "$run_two" && bash "$lock_script" acquire 4 run-two-02)"
(cd "$run_two" && bash "$lock_script" release 4 run-two-02 "$second_lock")
if [[ "$(git --git-dir="$remote" rev-parse "$remote_ref")" != "$published_main" ]]; then
  echo "day lock did not return to the saved idle ref after the second release" >&2
  exit 1
fi

echo "day lock acquisition, contention, and release passed"
