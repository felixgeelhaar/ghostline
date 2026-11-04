#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

staged=false
if [[ "${1:-}" == "--staged" ]]; then
  staged=true
elif [[ $# -ne 0 ]]; then
  echo "usage: scripts/nox-scan.sh [--staged]" >&2
  exit 2
fi

if ! command -v nox >/dev/null 2>&1; then
  echo "Nox 1.57.0 is required. Install it with: brew install nox-hq/tap/nox" >&2
  exit 2
fi

nox_version="$(nox --version | sed -E 's/^nox ([^ ]+).*/\1/')"
if [[ "$nox_version" != "1.57.0" ]]; then
  echo "Nox 1.57.0 is required (found ${nox_version:-unknown}); see README.md for setup." >&2
  exit 2
fi

report_dir="$(mktemp -d "${TMPDIR:-/tmp}/ghostline-nox.XXXXXX")"
args=(--format all --output "$report_dir" scan .)
if [[ "$staged" == true ]]; then
  args+=(--staged)
else
  args+=(--tracked-only)
fi

set +e
nox "${args[@]}"
scan_status=$?
set -e

report="$report_dir/findings.json"
if [[ ! -s "$report" ]]; then
  echo "Nox did not produce findings.json; reports (if any): $report_dir" >&2
  exit 2
fi

# Docker base images are inventoried, but Nox's OSV lookup does not yet support
# the Docker ecosystem. On developer machines, Nox may also report installed
# optional plugins that this repository has not enabled. Any other coverage
# degradation fails the gate.
if ! jq -e '
  all(.meta.degradations[]?;
    (.kind == "lockfile_parse" and
     (.detail | test("^[0-9]+ docker packages are in the inventory but the advisory pipeline has no OSV ecosystem for docker$")) and
     (.impact | startswith("these packages were NOT checked for known vulnerabilities"))) or
    (.kind == "plugin" and
     (.detail | test("^[0-9]+ installed plugin\\(s\\) are not listed in plugins.required and did not run:")) and
     (.impact | startswith("their findings are absent from this scan; add the ones you want to plugins.required in .nox.yaml")))
  )
' "$report" >/dev/null; then
  echo "Nox reported an unexpected scan coverage gap; reports: $report_dir" >&2
  jq -r '.meta.degradations[]? | "\(.kind): \(.detail) — \(.impact)"' "$report" >&2
  exit 2
fi

if [[ "$scan_status" -ne 0 ]]; then
  echo "Nox gate failed (exit $scan_status); reports: $report_dir" >&2
  jq --slurpfile baseline "$repo_root/.nox/baseline.json" -r '
    .findings[]? |
    select(.Fingerprint as $fingerprint | ($baseline[0].entries | map(.fingerprint) | index($fingerprint)) == null) |
    "\(.Severity) \(.RuleID) \(.Location.FilePath):\(.Location.StartLine) — \(.Message)"
  ' "$report" >&2
  exit "$scan_status"
fi

rm -rf "$report_dir"
