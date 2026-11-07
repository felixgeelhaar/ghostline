#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: day-lock.sh acquire <day-number> <unique-run-id> | release <day-number> <unique-run-id> <lock-commit-sha>" >&2
  exit 2
}

[[ $# -eq 3 || $# -eq 4 ]] || usage
action="$1"
day="$2"
claim="$3"
[[ "$day" =~ ^[1-9][0-9]*$ ]] || usage
lock_ref="refs/heads/ghostline-automation/locks/day-${day}"
lock_prefix="Ghostline automation lock day ${day} run "
# Locks stranded by the earlier script used this message; keep treating them as held.
legacy_prefix="Ghostline automation lock for day ${day}, run "

push_lock_ref() {
  # Lock refs are coordination metadata; the scheduled code branch still uses Warden gates.
  git -c core.hooksPath=/dev/null push "$@"
}

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "day lock must run inside a Git worktree" >&2
  exit 1
}
git remote get-url origin >/dev/null 2>&1 || {
  echo "origin remote is required for the shared day lock" >&2
  exit 1
}

case "$action" in
  acquire)
    [[ $# -eq 3 ]] || usage
    [[ "$claim" =~ ^[A-Za-z0-9._-]{8,80}$ ]] || {
      echo "run id must contain 8 to 80 letters, numbers, dots, underscores, or hyphens" >&2
      exit 2
    }
    if ! git fetch --quiet origin refs/heads/main; then
      echo "could not fetch the published main branch to create the day lock" >&2
      exit 1
    fi
    published_base="$(git rev-parse FETCH_HEAD)"
    idle_sha=""
    if ! remote_ref="$(git ls-remote --refs origin "$lock_ref")"; then
      echo "could not inspect day ${day} lock ref" >&2
      exit 1
    fi
    if [[ -n "$remote_ref" ]]; then
      idle_sha="${remote_ref%%$'\t'*}"
      if ! git fetch --quiet origin "$lock_ref"; then
        echo "could not fetch day ${day} lock ref" >&2
        exit 1
      fi
      lock_message="$(git show -s --format=%B FETCH_HEAD)"
      if [[ "$lock_message" == "$lock_prefix"* || "$lock_message" == "$legacy_prefix"* ]]; then
        echo "day ${day} already has an active remote automation claim" >&2
        exit 1
      fi
      if [[ "$(git rev-parse FETCH_HEAD)" != "$idle_sha" ]]; then
        echo "day ${day} lock ref changed while it was inspected; refusing to claim" >&2
        exit 1
      fi
    fi
    lock_parent="${idle_sha:-$published_base}"
    lock_tree="$(git rev-parse "${lock_parent}^{tree}")"
    lock_commit="$(printf '%s%s\n' "$lock_prefix" "$claim" | GIT_AUTHOR_NAME='Ghostline Automation' GIT_AUTHOR_EMAIL='ghostline-automation@example.invalid' GIT_COMMITTER_NAME='Ghostline Automation' GIT_COMMITTER_EMAIL='ghostline-automation@example.invalid' git -c commit.gpgsign=false commit-tree "$lock_tree" -p "$lock_parent")"
    if [[ -n "$idle_sha" ]]; then
      lease="--force-with-lease=${lock_ref}:${idle_sha}"
    else
      lease="--force-with-lease=${lock_ref}:"
    fi
    if ! push_lock_ref --porcelain "$lease" origin "${lock_commit}:${lock_ref}" >/dev/null; then
      echo "could not atomically claim day ${day}; another run may hold the lock or the remote denied the ref" >&2
      exit 1
    fi
    printf '%s\n' "$lock_commit" >&2
    if ! actual="$(git ls-remote --refs origin "$lock_ref" | awk 'NR == 1 { print $1 }')"; then
      echo "could not verify day ${day} lock; attempting owner-checked cleanup for ${lock_commit}" >&2
      if ! push_lock_ref --porcelain --force-with-lease="${lock_ref}:${lock_commit}" origin "${lock_parent}:${lock_ref}" >/dev/null; then
        echo "cleanup failed; if no run is active, restore ${lock_ref} only after confirming it still points to ${lock_commit}" >&2
      fi
      exit 1
    fi
    if [[ "$actual" != "$lock_commit" ]]; then
      echo "day ${day} lock ref did not retain this run's claim; attempted owner-checked cleanup for ${lock_commit}" >&2
      push_lock_ref --porcelain --force-with-lease="${lock_ref}:${lock_commit}" origin "${lock_parent}:${lock_ref}" >/dev/null || true
      exit 1
    fi
    printf '%s\n' "$lock_commit"
    ;;
  release)
    [[ $# -eq 4 ]] || usage
    [[ "$claim" =~ ^[A-Za-z0-9._-]{8,80}$ && "$4" =~ ^[0-9a-fA-F]{40,64}$ ]] || usage
    run_id="$claim"
    lock_sha="$4"
    if ! actual="$(git ls-remote --refs origin "$lock_ref" | awk 'NR == 1 { print $1 }')"; then
      echo "could not inspect day ${day} lock ${claim} before release; the remote lock state is unknown" >&2
      exit 1
    fi
    if [[ -z "$actual" ]]; then
      echo "day ${day} has no active remote automation claim" >&2
      exit 1
    fi
    if [[ "$actual" != "$lock_sha" ]]; then
      echo "day ${day} lock is owned by a different run; refusing to release it" >&2
      exit 1
    fi
    if ! git fetch --quiet origin "$lock_ref"; then
      echo "could not fetch day ${day} lock before release" >&2
      exit 1
    fi
    lock_message="$(git show -s --format=%B FETCH_HEAD)"
    if [[ "$lock_message" != "${lock_prefix}${run_id}" ]]; then
      echo "day ${day} lock does not identify run ${run_id}; refusing to release it" >&2
      exit 1
    fi
    free_sha="$(git rev-parse 'FETCH_HEAD^1')"
    push_lock_ref --porcelain --force-with-lease="${lock_ref}:${lock_sha}" origin "${free_sha}:${lock_ref}" >/dev/null || {
      echo "failed to release day ${day} lock ${lock_sha}; the remote may have changed or denied the update" >&2
      exit 1
    }
    if ! actual="$(git ls-remote --refs origin "$lock_ref" | awk 'NR == 1 { print $1 }')"; then
      echo "released day ${day} lock ${lock_sha}, but could not verify the final ref state" >&2
      exit 1
    fi
    if [[ "$actual" != "$free_sha" ]]; then
      echo "day ${day} lock did not return to its saved idle SHA after release" >&2
      exit 1
    fi
    printf 'released day %s lock %s\n' "$day" "$lock_sha"
    ;;
  *)
    usage
    ;;
esac
