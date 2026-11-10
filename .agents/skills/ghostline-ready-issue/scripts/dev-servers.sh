#!/usr/bin/env bash
# Starts or stops the Ghostline backend (port 18080) and frontend (port 15173) for one
# issue worktree, for screenshot evidence in an unattended pickup.
#
#   dev-servers.sh start <worktree>   start both servers and wait until they listen
#   dev-servers.sh stop  <worktree>   stop only the processes this script started
#
# Each server is launched as the leader of its own process group; the leader's PID
# (which is also the group ID) and start time are recorded in the worktree's private
# Git directory. Stop signals only those groups, so children that outlive their
# launcher are still stopped. A live leader whose start time no longer matches is a
# reused PID and is left alone; while any member of a group exists, the kernel does
# not hand its ID to a new process. Other local services (including whatever owns
# 8080/5173) are never touched.
set -euo pipefail

# The ports are fixed for pickups; the overrides exist so the test can use free ports.
backend_port="${GHOSTLINE_BACKEND_PORT:-18080}"
frontend_port="${GHOSTLINE_FRONTEND_PORT:-15173}"

usage() {
  echo "Usage: dev-servers.sh start|stop <worktree>" >&2
  exit 2
}

[[ $# -eq 2 ]] || usage
action="$1"
[[ "$action" == start || "$action" == stop ]] || usage
[[ -d "$2" ]] || { echo "worktree $2 does not exist" >&2; exit 1; }
worktree="$(cd "$2" && pwd -P)"
toplevel="$(git -C "$worktree" rev-parse --show-toplevel 2>/dev/null || true)"
[[ -n "$toplevel" && "$(cd "$toplevel" && pwd -P)" == "$worktree" ]] || {
  echo "$worktree is not the root of a Git worktree" >&2
  exit 1
}
state_dir="$(git -C "$worktree" rev-parse --path-format=absolute --git-path ghostline-dev-servers)"
pid_file="$state_dir/pids"

listening() { lsof -nP -iTCP:"$1" -sTCP:LISTEN -t >/dev/null 2>&1; }
started_at() { ps -o lstart= -p "$1" 2>/dev/null || true; }

# Runs a command as the leader of a new process group (macOS has no setsid command).
new_group=(python3 -c 'import os, sys; os.setpgid(0, 0); os.execvp(sys.argv[1], sys.argv[1:])')

group_members() { ps -A -o pid= -o pgid= | awk -v g="$1" '$2 == g { print $1 }'; }

stop_servers() {
  [[ -f "$pid_file" ]] || { echo "no dev servers recorded for $worktree"; return 0; }
  local pid recorded leader_start group groups=()
  while IFS=$'\t' read -r pid recorded; do
    [[ "$pid" =~ ^[0-9]+$ ]] || continue
    leader_start="$(started_at "$pid")"
    if [[ -n "$leader_start" && "$leader_start" != "$recorded" ]]; then
      echo "pid $pid is no longer the recorded server; leaving it alone"
      continue
    fi
    [[ -n "$(group_members "$pid")" ]] && groups+=("$pid")
  done <"$pid_file"
  if [[ ${#groups[@]} -gt 0 ]]; then
    for group in "${groups[@]}"; do kill -TERM -- "-$group" 2>/dev/null || true; done
    local tries=0 remaining
    while [[ $tries -lt 50 ]]; do
      remaining=""
      for group in "${groups[@]}"; do remaining+="$(group_members "$group")"; done
      [[ -n "$remaining" ]] || break
      sleep 0.2
      tries=$((tries + 1))
    done
    for group in "${groups[@]}"; do kill -KILL -- "-$group" 2>/dev/null || true; done
  fi
  rm -f "$pid_file"
  echo "stopped dev servers for $worktree"
}

record() { printf '%s\t%s\n' "$1" "$(started_at "$1")" >>"$pid_file"; }

recorded_pids() { cut -f1 "$pid_file"; }

# Prints "owned" when every listener on the port belongs to a recorded process group,
# "foreign" when any listener does not, and "none" when nothing listens yet.
port_owner() {
  local listeners pid owned=" "
  listeners="$(lsof -nP -iTCP:"$1" -sTCP:LISTEN -t 2>/dev/null || true)"
  [[ -n "$listeners" ]] || { echo none; return; }
  owned+="$(recorded_pids | tr '\n' ' ')"
  for pid in $listeners; do
    [[ "$owned" == *" $(ps -o pgid= -p "$pid" | tr -d ' ') "* ]] || { echo foreign; return; }
  done
  echo owned
}

fail_start() {
  echo "$1; logs follow" >&2
  tail -n 20 "$state_dir/backend.log" "$state_dir/frontend.log" >&2 || true
  stop_servers >&2
  exit 1
}

start_servers() {
  if [[ -f "$pid_file" ]]; then
    echo "dev servers are already recorded for $worktree; run stop first" >&2
    exit 1
  fi
  local port
  for port in "$backend_port" "$frontend_port"; do
    if listening "$port"; then
      echo "port $port is already in use by another process; not starting and not stopping it" >&2
      exit 1
    fi
  done
  mkdir -p "$state_dir"
  : >"$pid_file"
  # Each server leads its own process group, and does not inherit the pickup
  # wrapper's lock descriptor.
  PORT="$backend_port" nohup "${new_group[@]}" go -C "$worktree" run ./backend \
    >"$state_dir/backend.log" 2>&1 </dev/null 9>&- &
  record "$!"
  GHOSTLINE_API_URL="http://localhost:$backend_port" nohup "${new_group[@]}" \
    npm --prefix "$worktree/frontend" run dev -- --port "$frontend_port" --strictPort \
    >"$state_dir/frontend.log" 2>&1 </dev/null 9>&- &
  record "$!"
  # Ready only when both ports are served by the processes started here.
  local waited=0 pid backend frontend
  while true; do
    for pid in $(recorded_pids); do
      kill -0 "$pid" 2>/dev/null || fail_start "a dev server exited during startup"
    done
    backend="$(port_owner "$backend_port")"
    frontend="$(port_owner "$frontend_port")"
    [[ "$backend" != foreign ]] || fail_start "another process took port $backend_port during startup"
    [[ "$frontend" != foreign ]] || fail_start "another process took port $frontend_port during startup"
    [[ "$backend" == owned && "$frontend" == owned ]] && break
    [[ $waited -lt 180 ]] || fail_start "dev servers did not start within 90 seconds"
    sleep 0.5
    waited=$((waited + 1))
  done
  echo "backend http://localhost:$backend_port and frontend http://localhost:$frontend_port are running for $worktree"
}

"${action}_servers"
