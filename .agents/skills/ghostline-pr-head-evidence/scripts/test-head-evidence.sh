#!/usr/bin/env bash
# Tests head-evidence.sh against fixture GitHub API responses served by a fake gh.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checker="$script_dir/head-evidence.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

head_sha="1111111111111111111111111111111111111111"
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
if [[ -f "$file" ]]; then cat "$file"; else echo "fake gh: no fixture for $path" >&2; exit 1; fi
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
    {"name":"Go checks","status":"completed","conclusion":$go,"started_at":"2026-11-21T09:00:00Z"},
    {"name":"Frontend checks","status":"completed","conclusion":"success","started_at":"2026-11-21T09:00:00Z"},
    {"name":"Security (nox)","status":"completed","conclusion":"success","started_at":"2026-11-21T09:00:00Z"}]}'
}

statuses_json() { # statuses_json [WARDEN_STATE]
  if [[ -z "${1:-}" ]]; then echo '{"statuses":[]}'; return; fi
  jq -n --arg s "$1" '{"statuses":[{"context":"warden/gate","state":$s}]}'
}

make_case() { # make_case NAME BEHIND REVIEW_BODY GO_CONCLUSION WARDEN_STATE [SUMMARY_SHA]
  local name="$1"
  fixture "$name" "repos/$repo/pulls/7" "{\"state\":\"open\",\"draft\":false,\"head\":{\"sha\":\"$head_sha\"},\"base\":{\"ref\":\"main\"}}"
  fixture "$name" "repos/$repo/compare/main...$head_sha" "{\"behind_by\":$2}"
  fixture "$name" "repos/$repo/branches/main/protection/required_status_checks" '{"contexts":["Go checks","Frontend checks","warden/gate","Security (nox)"]}'
  fixture "$name" "repos/$repo/issues/7/comments?per_page=100" "$(comments_json "$3" "${6:-$old_sha}")"
  fixture "$name" "repos/$repo/commits/$head_sha/check-runs?per_page=100" "$(checks_json "$4")"
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

if (( failures > 0 )); then echo "$failures case(s) failed"; exit 1; fi
echo "all head-evidence cases passed"
