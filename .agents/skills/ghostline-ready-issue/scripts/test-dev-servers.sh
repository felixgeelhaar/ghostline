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


# Startup: build a tiny app whose servers listen on the ports they are given.
ports="$(python3 -c 'import socket
s = [socket.socket() for _ in range(2)]
for x in s: x.bind(("127.0.0.1", 0))
print(" ".join(str(x.getsockname()[1]) for x in s))')"
read -r backend_port frontend_port <<<"$ports"
export GHOSTLINE_BACKEND_PORT="$backend_port" GHOSTLINE_FRONTEND_PORT="$frontend_port"
app="$tmp/app"
git init --quiet "$app"
mkdir -p "$app/backend" "$app/frontend"
printf 'module example.invalid/app\n\ngo 1.21\n' >"$app/go.mod"
cat >"$app/backend/main.go" <<'GO'
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	if os.Getenv("FAKE_BACKEND_SILENT") != "" {
		time.Sleep(time.Minute)
		return
	}
	_ = http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), http.NotFoundHandler())
}
GO
cat >"$app/frontend/server.js" <<'JS'
const port = Number(process.argv[process.argv.indexOf("--port") + 1]);
require("http").createServer((_, res) => res.end("ok")).listen(port, "127.0.0.1");
JS
printf '{"name":"fake","private":true,"scripts":{"dev":"node server.js"}}\n' >"$app/frontend/package.json"
app_state="$(git -C "$app" rev-parse --path-format=absolute --git-path ghostline-dev-servers)"

# Both servers start, are owned by this script's process trees, and stop again.
bash "$servers" start "$app" >/dev/null || fail "start failed: $(tail -n 5 "$app_state"/*.log)"
curl -fsS "http://127.0.0.1:$frontend_port" >/dev/null || fail "frontend not reachable"
bash "$servers" stop "$app" >/dev/null
sleep 0.5
if lsof -nP -iTCP:"$backend_port" -sTCP:LISTEN -t >/dev/null; then fail "backend still listening after stop"; fi

# A foreign process that takes the backend port during startup is not mistaken for the
# backend: start fails, cleans up its own processes, and leaves the foreign one running.
python3 -c 'import socket, sys, time
time.sleep(1)
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", int(sys.argv[1]))); s.listen(); time.sleep(60)' "$backend_port" &
foreign=$!
others+=("$foreign")
if FAKE_BACKEND_SILENT=1 bash "$servers" start "$app" >/dev/null 2>&1; then
  fail "start succeeded although a foreign process owned the backend port"
fi
alive "$foreign" || fail "the foreign listener was stopped"
[[ ! -e "$app_state/pids" ]] || fail "failed start left a record behind"
if lsof -nP -iTCP:"$frontend_port" -sTCP:LISTEN -t >/dev/null; then fail "failed start left the frontend running"; fi

echo "dev server validation and owner-checked start and stop passed"
