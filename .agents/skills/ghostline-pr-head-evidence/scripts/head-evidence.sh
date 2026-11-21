#!/usr/bin/env bash
# Checks whether a pull request's current head has a Qodo code review and every
# required check, and whether the branch is up to date with its base.
#
# Usage: head-evidence.sh PR_NUMBER [OWNER/REPO]
# Exit:  0 PASS, 1 FAIL, 2 INCOMPLETE, 3 usage or tool error.
# Read-only: it only calls `gh api` GET endpoints.
set -euo pipefail

pr="${1:-}"
repo="${2:-felixgeelhaar/ghostline}"
qodo_bot="qodo-code-review[bot]"

usage_error() { echo "error: $1" >&2; echo "usage: head-evidence.sh PR_NUMBER [OWNER/REPO]" >&2; exit 3; }
[[ "$pr" =~ ^[0-9]+$ ]] || usage_error "PR_NUMBER must be a number"
[[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || usage_error "OWNER/REPO is malformed"
command -v gh >/dev/null || usage_error "gh is not installed"
command -v jq >/dev/null || usage_error "jq is not installed"

api() {
  local out
  if ! out="$(gh api "$@" 2>&1)"; then
    echo "error: gh api ${*: -1} failed: $out" >&2
    exit 3
  fi
  printf '%s' "$out"
}

incomplete=()
failed=()

pr_json="$(api "repos/$repo/pulls/$pr")"
state="$(jq -r '.state' <<<"$pr_json")"
draft="$(jq -r '.draft' <<<"$pr_json")"
head="$(jq -r '.head.sha' <<<"$pr_json")"
base="$(jq -r '.base.ref' <<<"$pr_json")"
echo "pr: $repo#$pr ($state)"
echo "head: $head"
[[ "$state" == "open" ]] || incomplete+=("PR is $state, not open")
[[ "$draft" != "true" ]] || incomplete+=("PR is a draft; Qodo does not review drafts")

behind="$(api "repos/$repo/compare/$base...$head" | jq -r '.behind_by')"
if [[ "$behind" == "0" ]]; then
  echo "branch: up to date with $base"
else
  echo "branch: behind $base by $behind"
  incomplete+=("branch is behind $base by $behind; updating it creates a new head that needs fresh evidence")
fi

# Qodo edits one "Code Review by Qodo" comment in place; its commit marker names the reviewed head.
comments="$(api --paginate "repos/$repo/issues/$pr/comments?per_page=100" | jq -s 'add // []')"
review="$(jq --arg bot "$qodo_bot" '[.[] | select(.user.login == $bot and (.body | contains("Code Review by Qodo")))] | sort_by(.updated_at) | last // empty' <<<"$comments")"
summary_names_head="$(jq --arg bot "$qodo_bot" --arg head "$head" '[.[] | select(.user.login == $bot and (.body | contains("PR Summary by Qodo")) and (.body | contains("/commit/" + $head)))] | length > 0' <<<"$comments")"

if [[ -z "$review" ]]; then
  echo "qodo review: none"
  incomplete+=("no Qodo code review comment")
else
  reviewed="$(jq -r '[.body | scan("/commit/([0-9a-f]{40})") | .[0]] | unique | join(",")' <<<"$review")"
  if [[ "$reviewed" == "$head" ]]; then
    echo "qodo review: covers head (comment updated $(jq -r '.updated_at' <<<"$review"))"
    body="$(jq -r '.body' <<<"$review" | sed 's/<[^>]*>/ /g')"
    count() { { grep -oiE "View $1 \(([0-9]+)\)" <<<"$body" || true; } | { grep -oE '[0-9]+' || true; } | awk '{s+=$1} END {print s+0}'; }
    high="$(count high)"; medium="$(count medium)"; low="$(count low)"
    if (( high + medium + low == 0 )) && ! grep -qiE 'no open findings for this group|no issues found' <<<"$body"; then
      echo "findings: unknown"
      incomplete+=("could not read finding counts from the Qodo review; read it manually")
    else
      low_note=""
      if (( low > 0 )); then low_note=" (report to the engineer)"; fi
      echo "findings: high=$high medium=$medium low=$low$low_note"
      if (( high + medium > 0 )); then failed+=("Qodo reports high=$high medium=$medium on the head"); fi
    fi
  else
    echo "qodo review: covers ${reviewed:-no commit marker}, not the head"
    if [[ "$summary_names_head" == "true" ]]; then
      incomplete+=("Qodo code review covers ${reviewed:-an unknown commit}; only the PR summary names the head (a merge-only update can cause this)")
    else
      incomplete+=("Qodo code review covers ${reviewed:-an unknown commit}, not the head")
    fi
  fi
fi

required="$(api "repos/$repo/branches/$base/protection/required_status_checks" | jq -r '.contexts[]')"
runs="$(api --paginate "repos/$repo/commits/$head/check-runs?per_page=100" | jq -s '[.[].check_runs[]]')"
statuses="$(api "repos/$repo/commits/$head/status" | jq '.statuses')"
while IFS= read -r ctx; do
  [[ -n "$ctx" ]] || continue
  result="$(jq -r --arg c "$ctx" '[.[] | select(.name == $c)] | sort_by(.started_at) | last | if . == null then "" elif .status != "completed" then .status else .conclusion end' <<<"$runs")"
  [[ -n "$result" ]] || result="$(jq -r --arg c "$ctx" '[.[] | select(.context == $c)] | last | .state // ""' <<<"$statuses")"
  case "$result" in
    success|skipped|neutral) echo "check $ctx: $result" ;;
    "") echo "check $ctx: missing on head"; incomplete+=("$ctx: missing on head") ;;
    queued|in_progress|pending|waiting|requested) echo "check $ctx: $result"; incomplete+=("$ctx: $result") ;;
    *) echo "check $ctx: $result"; failed+=("$ctx: $result") ;;
  esac
done <<<"$required"

# "${arr[@]+"${arr[@]}"}" keeps empty arrays safe under `set -u` in macOS bash 3.2.
if (( ${#failed[@]} > 0 )); then
  echo "verdict: FAIL"; printf -- '- %s\n' "${failed[@]}" ${incomplete[@]+"${incomplete[@]}"}; exit 1
elif (( ${#incomplete[@]} > 0 )); then
  echo "verdict: INCOMPLETE"; printf -- '- %s\n' "${incomplete[@]}"; exit 2
fi
echo "verdict: PASS"
