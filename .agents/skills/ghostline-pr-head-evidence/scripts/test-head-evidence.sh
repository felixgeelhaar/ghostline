#!/usr/bin/env bash
# Tests head-evidence.sh against fixture GitHub API responses served by a fake gh.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checker="$script_dir/head-evidence.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

head_sha="abcdef1111111111111111111111111111111111"
old_sha="2222222222222222222222222222222222222222"
repo="acme/app"
failures=0

# The fake gh answers `gh api [--paginate] PATH` from $FIXTURES/<PATH with / ? = & replaced by _>.json.
mkdir -p "$tmp_dir/bin"
cat > "$tmp_dir/bin/gh" <<'FAKE'
#!/usr/bin/env bash
path=""
for arg in "$@"; do
  case "$arg" in api|--paginate) ;; *) path="$arg" ;; esac
done
file="$FIXTURES/$(printf '%s' "$path" | tr '/?=&.' '_____').json"
if [[ -f "$file" ]]; then
  cat "$file"
  # Real gh (2.8x) prints this advisory on stderr even when the call succeeds.
  echo "hint: if you need to wait for this pull request to be reviewed, merged, or closed before continuing, use this instead of polling:" >&2
else
  echo "fake gh: no fixture for $path" >&2; exit 1
fi
FAKE
chmod +x "$tmp_dir/bin/gh"

fixture() { # fixture CASE PATH CONTENT
  local dir="$tmp_dir/$1"
  mkdir -p "$dir"
  printf '%s' "$3" > "$dir/$(printf '%s' "$2" | tr '/?=&.' '_____').json"
}

review_body() { # review_body SHA GROUPS_HTML
  printf '<h3>Code Review by Qodo</h3>%s<!-- https://github.com/%s/commit/%s -->' "$2" "$repo" "$1"
}

required_json='{"contexts":["Go checks","Frontend checks","warden/gate","Security (nox)"],"checks":[{"context":"Go checks","app_id":15368},{"context":"Frontend checks","app_id":15368},{"context":"warden/gate","app_id":null},{"context":"Security (nox)","app_id":15368}]}'

clean_groups='<p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p>'

comments_json() { # comments_json BODY [SUMMARY_SHA]
  local summary="${2:-$old_sha}"
  jq -n --arg body "$1" --arg summary "<h3>PR Summary by Qodo</h3><!-- https://github.com/$repo/commit/$summary -->" '[
    {"user":{"login":"qodo-code-review[bot]"},"updated_at":"2026-11-21T10:00:00Z","body":$summary},
    {"user":{"login":"qodo-code-review[bot]"},"updated_at":"2026-11-21T09:00:00Z","body":$body},
    {"user":{"login":"someone"},"updated_at":"2026-11-21T11:00:00Z","body":"<h3>Code Review by Qodo</h3> quoted by a human"}
  ]'
}

checks_json() { # checks_json GO_CONCLUSION
  jq -n --arg go "$1" '{"check_runs":[
    {"name":"Go checks","status":"completed","conclusion":$go,"started_at":"2026-11-21T09:00:00Z","app":{"id":15368}},
    {"name":"Frontend checks","status":"completed","conclusion":"success","started_at":"2026-11-21T09:00:00Z","app":{"id":15368}},
    {"name":"Security (nox)","status":"completed","conclusion":"success","started_at":"2026-11-21T09:00:00Z","app":{"id":15368}}]}'
}

statuses_json() { # statuses_json [WARDEN_STATE]
  if [[ -z "${1:-}" ]]; then echo '{"statuses":[]}'; return; fi
  jq -n --arg s "$1" '{"statuses":[{"context":"warden/gate","state":$s}]}'
}

make_case() { # make_case NAME BEHIND REVIEW_BODY GO_CONCLUSION WARDEN_STATE [SUMMARY_SHA]
  local name="$1"
  fixture "$name" "repos/$repo/pulls/7" "{\"state\":\"open\",\"draft\":false,\"head\":{\"sha\":\"$head_sha\"},\"base\":{\"ref\":\"main\"}}"
  fixture "$name" "repos/$repo/compare/main...$head_sha" "{\"behind_by\":$2}"
  fixture "$name" "repos/$repo/branches/main/protection/required_status_checks" "$required_json"
  fixture "$name" "repos/$repo/issues/7/comments?per_page=100" "$(comments_json "$3" "${6:-$old_sha}")"
  fixture "$name" "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json "$4")"
  fixture "$name" "repos/$repo/commits/$head_sha/status" "$(statuses_json "$5")"
}

expect() { # expect NAME EXIT VERDICT_SUBSTRING [REASON_SUBSTRING]
  local name="$1" want_exit="$2" want_verdict="$3" want_reason="${4:-}" out status=0
  out="$(PATH="$tmp_dir/bin:$PATH" FIXTURES="$tmp_dir/$name" bash "$checker" 7 "$repo" 2>&1)" || status=$?
  if [[ "$status" != "$want_exit" ]] || grep -qiE 'unbound variable|syntax error|command not found' <<<"$out" || ! grep -qF "verdict: $want_verdict" <<<"$out" || { [[ -n "$want_reason" ]] && ! grep -qF -- "$want_reason" <<<"$out"; }; then
    echo "FAIL $name: want exit $want_exit, verdict $want_verdict${want_reason:+, reason '$want_reason'}; got exit $status:"
    sed 's/^/  | /' <<<"$out"
    failures=$((failures + 1))
  else
    echo "ok   $name"
  fi
}

make_case pass 0 "$(review_body "$head_sha" "$clean_groups")" success success
expect pass 0 PASS

make_case short-clean 0 "$(review_body "$head_sha" '<code>Bugs (0)</code><h3>Great, no issues found!</h3>')" success success
expect short-clean 0 PASS

make_case stale-review 0 "$(review_body "$old_sha" "$clean_groups")" success success "$head_sha"
expect stale-review 2 INCOMPLETE "only the PR summary names the head"

make_case medium-finding 0 "$(review_body "$head_sha" '<details><summary>View medium (1)</summary></details><p>Nice, no open findings for this group</p>')" success success
expect medium-finding 1 FAIL "medium=1"

make_case low-only 0 "$(review_body "$head_sha" '<details><summary>View low (2)</summary></details>')" success success
expect low-only 0 PASS "low=2 (report to the engineer)"

make_case missing-warden 0 "$(review_body "$head_sha" "$clean_groups")" success ""
expect missing-warden 2 INCOMPLETE "warden/gate: missing on head"

make_case failed-check 0 "$(review_body "$head_sha" "$clean_groups")" failure success
expect failed-check 1 FAIL "Go checks: failure"

make_case behind-main 3 "$(review_body "$head_sha" "$clean_groups")" success success
expect behind-main 2 INCOMPLETE "behind main by 3"

make_case unreadable-findings 0 "$(review_body "$head_sha" '<p>something new</p>')" success success
expect unreadable-findings 2 INCOMPLETE "could not read finding counts"

# Review findings from the local Qodo review of this skill (2026-11-21).
make_case earlier-failure 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture earlier-failure "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq '.check_runs += [{"name":"Go checks","status":"completed","conclusion":"failure","started_at":"2026-11-21T08:00:00Z","app":{"id":15368}}]')"
expect earlier-failure 1 FAIL "Go checks: an earlier run on this head concluded failure"

# docs/factory-verification.md: only a run that ended with no result may be rerun, once.
earlier_run() { # earlier_run CONCLUSION HOUR
  printf '{"name":"Go checks","status":"completed","conclusion":"%s","started_at":"2026-11-21T0%s:00:00Z","app":{"id":15368}}' "$1" "$2"
}
make_case startup-failure-rerun 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture startup-failure-rerun "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson e "$(earlier_run startup_failure 8)" '.check_runs += [$e]')"
expect startup-failure-rerun 0 PASS "Go checks: success on a rerun; an earlier run ended startup_failure with no result"

make_case failure-then-cancelled 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture failure-then-cancelled "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson a "$(earlier_run failure 7)" --argjson b "$(earlier_run cancelled 8)" '.check_runs += [$a, $b]')"
expect failure-then-cancelled 1 FAIL "Go checks: an earlier run on this head concluded failure"

make_case two-no-result 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture two-no-result "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson a "$(earlier_run startup_failure 7)" --argjson b "$(earlier_run startup_failure 8)" '.check_runs += [$a, $b]')"
expect two-no-result 2 INCOMPLETE "Go checks: 2 earlier runs on this head ended with no result; only one rerun is allowed, so the engineer decides"

make_case timed-out-rerun 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture timed-out-rerun "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson e "$(earlier_run timed_out 8)" '.check_runs += [$e]')"
expect timed-out-rerun 2 INCOMPLETE "Go checks: an earlier run on this head ended timed_out; GitHub does not show whether the runner or the code caused it, so the engineer decides"

make_case cancelled-rerun 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture cancelled-rerun "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson e "$(earlier_run cancelled 8)" '.check_runs += [$e]')"
expect cancelled-rerun 2 INCOMPLETE "Go checks: an earlier run on this head ended cancelled; GitHub does not show whether the runner or the code caused it, so the engineer decides"

# The same order as classifyCheckRuns in backend/internal/domain/readiness.go (#157).
make_case earlier-no-conclusion 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture earlier-no-conclusion "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq '.check_runs += [{"name":"Go checks","status":"completed","conclusion":null,"started_at":"2026-11-21T08:00:00Z","app":{"id":15368}}]')"
expect earlier-no-conclusion 2 INCOMPLETE "Go checks: an earlier run on this head ended without a conclusion; GitHub does not show whether the runner or the code caused it, so the engineer decides"

make_case running-after-failure 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture running-after-failure "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson e "$(earlier_run failure 8)" '(.check_runs[] | select(.name == "Go checks")) |= (.status = "in_progress" | .conclusion = null) | .check_runs += [$e]')"
expect running-after-failure 1 FAIL "Go checks: an earlier run on this head concluded failure; a later run does not clear it"

make_case latest-never-started 0 "$(review_body "$head_sha" "$clean_groups")" startup_failure success
expect latest-never-started 2 INCOMPLETE "Go checks: startup_failure; the job never started, so one rerun is allowed"

make_case latest-cancelled 0 "$(review_body "$head_sha" "$clean_groups")" cancelled success
expect latest-cancelled 2 INCOMPLETE "Go checks: the latest run on this head ended cancelled; GitHub does not show whether the runner or the code caused it, so the engineer decides"

make_case wrong-app 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture wrong-app "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq '(.check_runs[] | select(.name == "Go checks") | .app.id) = 999')"
expect wrong-app 2 INCOMPLETE "Go checks: missing on head"

make_case alt-login 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture alt-login "repos/$repo/issues/7/comments?per_page=100" "$(comments_json "$(review_body "$head_sha" "$clean_groups")" | jq 'map(if .user.login == "qodo-code-review[bot]" then .user.login = "qodo-free-for-open-source-projects[bot]" else . end)')"
expect alt-login 0 PASS

make_case foreign-marker 0 "$(printf '<h3>Code Review by Qodo</h3>%s<!-- https://github.com/other/fork/commit/%s -->' "$clean_groups" "$head_sha")" success success
expect foreign-marker 2 INCOMPLETE "names no commit of $repo"

make_case unknown-group 0 "$(review_body "$head_sha" '<details><summary>View critical (1)</summary></details><p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p>')" success success
expect unknown-group 2 INCOMPLETE "could not read finding counts"

make_case short-nonzero 0 "$(review_body "$head_sha" '<code>Bugs (0)</code><code>Rule violations (1)</code><h3>Great, no issues found!</h3>')" success success
expect short-nonzero 2 INCOMPLETE "could not read finding counts"

# Second local Qodo review round.
make_case other-base 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture other-base "repos/$repo/pulls/7" "{\"state\":\"open\",\"draft\":false,\"head\":{\"sha\":\"$head_sha\"},\"base\":{\"ref\":\"release\"}}"
fixture other-base "repos/$repo/compare/main...$head_sha" '{"behind_by":0}'
expect other-base 2 INCOMPLETE "targets release, not main"

make_case protection-omits-nox 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture protection-omits-nox "repos/$repo/branches/main/protection/required_status_checks" '{"contexts":["Go checks"],"checks":[{"context":"Go checks","app_id":15368}]}'
fixture protection-omits-nox "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq '.check_runs |= map(select(.name != "Security (nox)"))')"
expect protection-omits-nox 2 INCOMPLETE "Security (nox): missing on head"

make_case upper-marker 0 "$(printf '<h3>Code Review by Qodo</h3>%s<!-- https://github.com/%s/commit/%s -->' "$clean_groups" "$repo" "$(tr a-f A-F <<<"$head_sha")")" success success
expect upper-marker 0 PASS

make_case newer-status 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture newer-status "repos/$repo/commits/$head_sha/status" '{"statuses":[{"context":"warden/gate","state":"success","updated_at":"2026-11-21T10:00:00Z"},{"context":"warden/gate","state":"pending","updated_at":"2026-11-21T09:00:00Z"}]}'
expect newer-status 0 PASS

# Third local Qodo review round.
make_case status-impersonates-app 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture status-impersonates-app "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq '.check_runs |= map(select(.name != "Go checks"))')"
fixture status-impersonates-app "repos/$repo/commits/$head_sha/status" '{"statuses":[{"context":"warden/gate","state":"success"},{"context":"Go checks","state":"success"}]}'
expect status-impersonates-app 2 INCOMPLETE "Go checks: missing on head"

make_case spaced-count 0 "$(review_body "$head_sha" '<details><summary>View medium ( 1 )</summary></details><p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p>')" success success
expect spaced-count 1 FAIL "medium=1"

# Fourth local Qodo review round.
make_case summary-mentions-review 0 "$(review_body "$old_sha" "$clean_groups")" success success
fixture summary-mentions-review "repos/$repo/issues/7/comments?per_page=100" "$(comments_json "$(review_body "$old_sha" "$clean_groups")" | jq --arg s "<h3>PR Summary by Qodo</h3><p>See the Code Review by Qodo below. Nice, no open findings for this group</p><!-- https://github.com/$repo/commit/$head_sha -->" '.[0].body = $s')"
expect summary-mentions-review 2 INCOMPLETE "not the head"

make_case queued-rerun 0 "$(review_body "$head_sha" "$clean_groups")" success success
fixture queued-rerun "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq '.check_runs |= (to_entries | map(.value + {id: (100 + .key)})) | .check_runs += [{"id":900,"name":"Go checks","status":"queued","conclusion":null,"started_at":null,"app":{"id":15368}}]')"
expect queued-rerun 2 INCOMPLETE "Go checks: queued"

# testdata/check-run-cases.json: the check-run table the Go domain tests run too
# (TestClassifyCheckRunsMatchesTheSharedCases), so the script and the app reach the same outcome.
shared_cases="$script_dir/../../../../testdata/check-run-cases.json"
shared_check="$(jq -r '.check' "$shared_cases")"
[[ "$shared_check" == "Go checks" ]] || { echo "FAIL shared cases: they must name Go checks, the check these fixtures require"; failures=$((failures + 1)); }
shared_count="$(jq '.cases | length' "$shared_cases")"
for ((i = 0; i < shared_count; i++)); do
  shared="$(jq -c --argjson i "$i" '.cases[$i]' "$shared_cases")"
  name="shared-$i"
  make_case "$name" 0 "$(review_body "$head_sha" "$clean_groups")" success success
  fixture "$name" "repos/$repo/commits/$head_sha/check-runs?per_page=100&filter=all" "$(checks_json success | jq --argjson case "$shared" '
    .check_runs |= (map(select(.name != "Go checks")) | to_entries | map(.value + {id: (10 + .key)}))
    | .check_runs += [$case.runs | to_entries[] | {id: (100 + .key), name: "Go checks", status: .value.status, conclusion: .value.conclusion, started_at: null, app: {id: 15368}}]')"
  case "$(jq -r '.outcome' <<<"$shared")" in
    pass) want_exit=0; want_verdict=PASS ;;
    fail) want_exit=1; want_verdict=FAIL ;;
    incomplete) want_exit=2; want_verdict=INCOMPLETE ;;
    *) echo "FAIL $name: unknown outcome"; failures=$((failures + 1)); continue ;;
  esac
  echo "     $name: $(jq -r '.name' <<<"$shared")"
  expect "$name" "$want_exit" "$want_verdict" "$(jq -r '.message' <<<"$shared")"
done

dotted="acme/widget.v2"
mkdir -p "$tmp_dir/dotted"
for f in "$tmp_dir/pass"/*.json; do
  name="$(basename "$f")"; cp "$f" "$tmp_dir/dotted/${name//acme_app/acme_widget_v2}"
done
jq --arg r "$dotted" --arg h "$head_sha" 'map(.body |= gsub("acme/app"; $r))' "$tmp_dir/pass/repos_acme_app_issues_7_comments_per_page_100.json" > "$tmp_dir/dotted/repos_acme_widget_v2_issues_7_comments_per_page_100.json"
out="$(PATH="$tmp_dir/bin:$PATH" FIXTURES="$tmp_dir/dotted" bash "$checker" 7 "$dotted" 2>&1)" && status=0 || status=$?
if [[ "$status" == 0 ]] && grep -qF "qodo review: covers head" <<<"$out"; then
  echo "ok   dotted-repo"
else
  echo "FAIL dotted-repo: want PASS with the review covering the head; got exit $status:"; sed 's/^/  | /' <<<"$out"; failures=$((failures + 1))
fi

fixture malformed-pr "repos/$repo/pulls/7" '{"state":"open","draft":false,"head":{},"base":{"ref":"main"}}'
out="$(PATH="$tmp_dir/bin:$PATH" FIXTURES="$tmp_dir/malformed-pr" bash "$checker" 7 "$repo" 2>&1)" && status=0 || status=$?
if [[ "$status" == 3 ]] && grep -qF "error: unexpected pull request response" <<<"$out"; then
  echo "ok   malformed-pr"
else
  echo "FAIL malformed-pr: want exit 3 rejecting the response; got exit $status:"; sed 's/^/  | /' <<<"$out"; failures=$((failures + 1))
fi

# A failed gh call, or any unexpected tool failure, must map to the documented exit 3.
mkdir -p "$tmp_dir/api-error"
out="$(PATH="$tmp_dir/bin:$PATH" FIXTURES="$tmp_dir/api-error" bash "$checker" 7 "$repo" 2>&1)" && status=0 || status=$?
if [[ "$status" == 3 ]] && grep -qF "error: gh api repos/$repo/pulls/7 failed" <<<"$out"; then
  echo "ok   api-error"
else
  echo "FAIL api-error: want exit 3 and the failed call named; got exit $status:"; sed 's/^/  | /' <<<"$out"; failures=$((failures + 1))
fi

fixture bad-json "repos/$repo/pulls/7" 'not json'
out="$(PATH="$tmp_dir/bin:$PATH" FIXTURES="$tmp_dir/bad-json" bash "$checker" 7 "$repo" 2>&1)" && status=0 || status=$?
if [[ "$status" == 3 ]] && grep -qF "error:" <<<"$out"; then
  echo "ok   bad-json"
else
  echo "FAIL bad-json: want exit 3 with an error line; got exit $status:"; sed 's/^/  | /' <<<"$out"; failures=$((failures + 1))
fi

if (( failures > 0 )); then echo "$failures case(s) failed"; exit 1; fi
echo "all head-evidence cases passed"
