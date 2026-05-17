#!/usr/bin/env bash
# ── beishan-core Eval Shared Library ────────────────────────────
# Ported from FangLab runtime_stack_lib.sh
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PID_DIR="$PROJECT_ROOT/eval/run/pids"
LOG_DIR="$PROJECT_ROOT/eval/run/logs"
OUT_DIR_BASE="$PROJECT_ROOT/eval/run"

mkdir -p "$PID_DIR" "$LOG_DIR" "$OUT_DIR_BASE"

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

info() {
  printf '[%s] %s\n' "$(timestamp)" "$*"
}

die() {
  printf '[%s] ERROR: %s\n' "$(timestamp)" "$*" >&2
  exit 1
}

pid_file() {
  printf '%s/%s.pid' "$PID_DIR" "$1"
}

log_file() {
  printf '%s/%s.log' "$LOG_DIR" "$1"
}

is_pid_running() {
  local pid="$1"
  [[ -n "$pid" ]] && ps -p "$pid" >/dev/null 2>&1
}

http_up() {
  local url="$1"
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$url" || true)"
  [[ "$code" != "000" && -n "$code" ]]
}

port_listening() {
  local port="$1"
  lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
}

url_port() {
  local url="$1"
  # Extract port from http://host:port or http://host
  if [[ "$url" =~ :([0-9]+) ]]; then
    echo "${BASH_REMATCH[1]}"
  else
    echo "80"
  fi
}

service_available() {
  local url="$1"
  http_up "$url" || port_listening "$(url_port "$url")"
}

wait_for_service() {
  local url="$1"
  local timeout="${2:-15}"
  local elapsed=0
  while ! service_available "$url"; do
    if [ "$elapsed" -ge "$timeout" ]; then
      return 1
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 0
}
