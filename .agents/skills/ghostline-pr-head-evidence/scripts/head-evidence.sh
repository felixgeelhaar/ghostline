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
# The same Qodo identities backend/reviews.go accepts.
qodo_bots='["qodo-code-review","qodo-code-review[bot]","qodo-free-for-open-source-projects[bot]"]'

usage_error() { echo "error: $1" >&2; echo "usage: head-evidence.sh PR_NUMBER [OWNER/REPO]" >&2; exit 3; }
[[ "$pr" =~ ^[0-9]+$ ]] || usage_error "PR_NUMBER must be a number"
[[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || usage_error "OWNER/REPO is malformed"
command -v gh >/dev/null || usage_error "gh is not installed"
command -v jq >/dev/null || usage_error "jq is not installed"

# Any unexpected failure (for example jq rejecting a response) is a tool error, not a verdict.
# An api() failure already exits 3 with its own message, so only other failures are announced here.
trap 'status=$?; if [[ "$status" != 3 ]]; then echo "error: unexpected failure (exit $status) at line $LINENO; no verdict" >&2; fi; exit 3' ERR

# gh writes advisories such as "hint: ... gh event listen" to stderr even on success,
# so stdout (the JSON) and stderr are captured separately.
api() {
  local out err_file
  err_file="$(mktemp)"
  if ! out="$(gh api "$@" 2>"$err_file")"; then
    echo "error: gh api ${*: -1} failed: $(cat "$err_file")" >&2
    rm -f "$err_file"
    exit 3
  fi
  rm -f "$err_file"
  printf '%s' "$out"
}

incomplete=()
failed=()

pr_json="$(api "repos/$repo/pulls/$pr")"
if ! jq -e '(.state | type == "string") and (.draft | type == "boolean") and (.head.sha | type == "string" and test("^[0-9a-f]{40}$")) and (.base.ref | type == "string" and length > 0)' <<<"$pr_json" >/dev/null; then
  echo "error: unexpected pull request response (missing state, draft, head.sha or base.ref); no verdict" >&2
  exit 3
fi
state="$(jq -r '.state' <<<"$pr_json")"
draft="$(jq -r '.draft' <<<"$pr_json")"
head="$(jq -r '.head.sha' <<<"$pr_json")"
base="$(jq -r '.base.ref' <<<"$pr_json")"
echo "pr: $repo#$pr ($state)"
echo "head: $head"
[[ "$state" == "open" ]] || incomplete+=("PR is $state, not open")
[[ "$draft" != "true" ]] || incomplete+=("PR is a draft; Qodo does not review drafts")

# Gate ③ is about reaching main: the PR must target main and be up to date with it.
gate_branch="main"
if [[ "$base" != "$gate_branch" ]]; then
  incomplete+=("PR targets $base, not $gate_branch; gate ③ covers merges into $gate_branch")
fi
behind="$(api "repos/$repo/compare/$gate_branch...$head" | jq -r '.behind_by')"
if [[ "$behind" == "0" ]]; then
  echo "branch: up to date with $gate_branch"
else
  echo "branch: behind $gate_branch by $behind"
  incomplete+=("branch is behind $gate_branch by $behind; updating it creates a new head that needs fresh evidence")
fi

# Qodo edits one "Code Review by Qodo" comment in place; its commit marker names the reviewed head.
comments="$(api --paginate "repos/$repo/issues/$pr/comments?per_page=100" | jq -s 'add // []')"
review="$(jq --argjson bots "$qodo_bots" '[.[] | select((.user.login as $l | $bots | index($l)) and (.body | test("^\\s*<h3>\\s*Code Review by Qodo\\s*</h3>"; "i")))] | sort_by(.updated_at) | last // empty' <<<"$comments")"
# Only a marker comment for this repository counts, as in backend/reviews.go.
repo_re="$(printf '%s' "$repo" | sed 's/\./\\./g')"
marker_re='<!--[[:space:]]*https://github\.com/'"$repo_re"'/commit/([0-9a-fA-F]{40})[[:space:]]*-->'
summary_names_head="$(jq --argjson bots "$qodo_bots" --arg re "$marker_re" --arg head "$head" '[.[] | select((.user.login as $l | $bots | index($l)) and (.body | contains("PR Summary by Qodo"))) | .body | scan($re; "i") | .[0] | ascii_downcase] | index($head) != null' <<<"$comments")"

if [[ -z "$review" ]]; then
  echo "qodo review: none"
  incomplete+=("no Qodo code review comment")
else
  reviewed="$(jq -r --arg re "$marker_re" '[.body | scan($re; "i") | .[0] | ascii_downcase] | unique | join(",")' <<<"$review")"
  if [[ "$reviewed" == "$head" ]]; then
    echo "qodo review: covers head (comment updated $(jq -r '.updated_at' <<<"$review"))"
    body="$(jq -r '.body' <<<"$review" | sed 's/<[^>]*>/ /g')"
    count() { { grep -oiE "View $1 \([[:space:]]*[0-9]+[[:space:]]*\)" <<<"$body" || true; } | { grep -oE '[0-9]+' || true; } | awk '{s+=$1} END {print s+0}'; }
    high="$(count high)"; medium="$(count medium)"; low="$(count low)"
    # Fail closed: any finding group or tally the counter can't attribute to high/medium/low.
    all_groups="$(count '[a-z]+')"
    unknown_groups=$((all_groups - high - medium - low))
    nonzero_tallies="$({ grep -oiE '(Bugs|Rule violations|Requirement gaps)[[:space:]]*\([[:space:]]*[0-9]+[[:space:]]*\)' <<<"$body" || true; } | { grep -oE '[0-9]+' || true; } | awk '{s+=$1} END {print s+0}')"
    if (( unknown_groups > 0 || nonzero_tallies > 0 )) || { (( high + medium + low == 0 )) && ! grep -qiE 'no open findings for this group|no issues found' <<<"$body"; }; then
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
    if [[ -z "$reviewed" ]]; then
      incomplete+=("the Qodo code review names no commit of $repo")
    elif [[ "$summary_names_head" == "true" ]]; then
      incomplete+=("Qodo code review covers ${reviewed:-an unknown commit}; only the PR summary names the head (a merge-only update can cause this)")
    else
      incomplete+=("Qodo code review covers ${reviewed:-an unknown commit}, not the head")
    fi
  fi
fi

# Each requirement is a context plus, when branch protection names one, the app that must produce it.
# Gate ③ names these checks; branch protection may add more but can't remove them.
gate_checks='["Go checks","Frontend checks","Security (nox)","warden/gate"]'
required="$(api "repos/$repo/branches/$gate_branch/protection/required_status_checks" | jq -c --argjson gate "$gate_checks" '
  (if (.checks | length) > 0 then .checks else [.contexts[] | {context: ., app_id: null}] end) as $protected
  | ($protected + [$gate[] | select(. as $c | $protected | map(.context) | index($c) | not) | {context: ., app_id: null}])[]')"
# filter=all: GitHub otherwise returns only the latest run of each check, hiding earlier runs.
runs="$(api --paginate "repos/$repo/commits/$head/check-runs?per_page=100&filter=all" | jq -s '[.[].check_runs[]]')"
statuses="$(api "repos/$repo/commits/$head/status" | jq '.statuses')"
while IFS= read -r requirement; do
  [[ -n "$requirement" ]] || continue
  ctx="$(jq -r '.context' <<<"$requirement")"
  app_id="$(jq -r '.app_id // ""' <<<"$requirement")"
  matching="$(jq --arg c "$ctx" --arg app "$app_id" '[.[] | select(.name == $c and ($app == "" or ((.app.id // "") | tostring) == $app))] | sort_by(.id // 0, .started_at // "")' <<<"$runs")"
  # docs/factory-verification.md, in the order classifyCheckRuns uses in
  # backend/internal/domain/readiness.go: a run that reported a result stays real even
  # if a later run passed or is still running. A run whose cause GitHub does not record
  # (cancelled, timed out, stale, or no conclusion) may have been a hang in the code, so
  # the engineer decides. Only a job that never started (startup_failure) may be rerun, once.
  verdict="$(jq -c --arg c "$ctx" '
    def passing: IN("success","skipped","neutral");
    def unknown_cause: IN("cancelled","timed_out","stale","");
    def no_result: . == "startup_failure";
    def reported: (passing or unknown_cause or no_result) | not;
    def ended: if . == "" then "without a conclusion" else . end;
    if length == 0 then {kind: "none"} else
    last as $latest | (.[:-1] | map(select(.status == "completed") | (.conclusion // ""))) as $earlier
    | ($latest.status == "completed") as $done | ($latest.conclusion // "") as $lc
    | ([$earlier[] | select(no_result)] | length) + (if $done and ($lc | no_result) then 1 else 0 end) as $no_result
    | (if $done then (if $lc == "" then "no conclusion" else $lc end) else $latest.status end) as $shown
    | if $done and ($lc | reported) then {kind: "fail", shown: $shown, reason: "\($c): \($lc)"}
      elif ([$earlier[] | select(reported)] | first) != null then
        {kind: "fail", shown: $shown, reason: "\($c): an earlier run on this head concluded \([$earlier[] | select(reported)] | first); a later run does not clear it"}
      elif $done and ($lc | unknown_cause) then
        {kind: "incomplete", shown: $shown, reason: "\($c): the latest run on this head ended \($lc | ended); GitHub does not show whether the runner or the code caused it, so the engineer decides"}
      elif ([$earlier[] | select(unknown_cause)] | first) != null then
        {kind: "incomplete", shown: $shown, reason: "\($c): an earlier run on this head ended \([$earlier[] | select(unknown_cause)] | first | ended); GitHub does not show whether the runner or the code caused it, so the engineer decides"}
      elif $no_result > 1 then
        {kind: "incomplete", shown: $shown, reason: "\($c): \($no_result) \(if $done and ($lc | no_result) then "runs" else "earlier runs" end) on this head ended with no result; only one rerun is allowed, so the engineer decides"}
      elif ($done | not) then {kind: "incomplete", shown: $shown, reason: "\($c): \($latest.status)"}
      elif ($lc | no_result) then {kind: "incomplete", shown: $shown, reason: "\($c): \($lc); the job never started, so one rerun is allowed"}
      elif $no_result == 1 then {kind: "pass", shown: "\($lc) on a rerun; an earlier run ended startup_failure with no result"}
      else {kind: "pass", shown: $lc} end
    end' <<<"$matching")"
  kind="$(jq -r '.kind' <<<"$verdict")"
  if [[ "$kind" == "none" ]]; then
    # A commit status carries no app, so it can only satisfy a requirement that names none.
    state=""
    [[ -n "$app_id" ]] || state="$(jq -r --arg c "$ctx" '[.[] | select(.context == $c)] | sort_by(.updated_at // "") | last | .state // ""' <<<"$statuses")"
    case "$state" in
      success) echo "check $ctx: $state" ;;
      "") echo "check $ctx: missing on head"; incomplete+=("$ctx: missing on head") ;;
      pending) echo "check $ctx: $state"; incomplete+=("$ctx: $state") ;;
      *) echo "check $ctx: $state"; failed+=("$ctx: $state") ;;
    esac
    continue
  fi
  echo "check $ctx: $(jq -r '.shown' <<<"$verdict")"
  case "$kind" in
    fail) failed+=("$(jq -r '.reason' <<<"$verdict")") ;;
    incomplete) incomplete+=("$(jq -r '.reason' <<<"$verdict")") ;;
  esac
done <<<"$required"

# "${arr[@]+"${arr[@]}"}" keeps empty arrays safe under `set -u` in macOS bash 3.2.
if (( ${#failed[@]} > 0 )); then
  echo "verdict: FAIL"; printf -- '- %s\n' "${failed[@]}" ${incomplete[@]+"${incomplete[@]}"}; exit 1
elif (( ${#incomplete[@]} > 0 )); then
  echo "verdict: INCOMPLETE"; printf -- '- %s\n' "${incomplete[@]}"; exit 2
fi
echo "verdict: PASS"
