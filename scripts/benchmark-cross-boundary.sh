#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
dist_dir=${NPU_BRIDGE_DIST_DIR:-$repo_dir/dist}
bench_dir=${NPU_BRIDGE_BENCH_DIR:-${TMPDIR:-/tmp}/npu-bridge-relay-benchmark}
small_requests=${NPU_BRIDGE_SMALL_REQUESTS:-30}
large_requests=${NPU_BRIDGE_LARGE_REQUESTS:-10}

linux_bridge="$dist_dir/npu-bridge-linux-amd64"
windows_bridge="$dist_dir/npu-bridge-windows-amd64.exe"
windows_backend="$dist_dir/npu-bridge-test-backend-windows-amd64.exe"
windows_curl=/mnt/c/Windows/System32/curl.exe

for required in "$linux_bridge" "$windows_bridge" "$windows_backend" "$windows_curl"; do
  if [[ ! -f "$required" ]]; then
    echo "missing benchmark dependency: $required" >&2
    exit 1
  fi
done
if (( small_requests < 2 || large_requests < 2 )); then
  echo "request counts must both be at least 2" >&2
  exit 1
fi

mkdir -p "$bench_dir"
payload_small="$bench_dir/payload-1b.bin"
payload_large="$bench_dir/payload-1m.bin"
printf x >"$payload_small"
dd if=/dev/zero of="$payload_large" bs=1M count=1 status=none

backend_log="$bench_dir/backend.log"
bridge_log="$bench_dir/bridge.log"
total_requests=$((2 * small_requests + 2 * large_requests))

bridge_pid=
backend_pid=
cleanup() {
  if [[ -n "$bridge_pid" ]] && kill -0 "$bridge_pid" 2>/dev/null; then
    kill "$bridge_pid" 2>/dev/null || true
    wait "$bridge_pid" 2>/dev/null || true
  fi
  if [[ -n "$backend_pid" ]] && kill -0 "$backend_pid" 2>/dev/null; then
    kill "$backend_pid" 2>/dev/null || true
    wait "$backend_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

: >"$backend_log"
"$windows_backend" \
  --listen 127.0.0.1:0 \
  --max-requests "$total_requests" \
  --max-lifetime 2m >"$backend_log" 2>&1 &
backend_pid=$!

windows_target=
for _ in $(seq 1 100); do
  windows_target=$(sed -n 's/^LISTEN //p' "$backend_log" | head -n 1)
  [[ -n "$windows_target" ]] && break
  sleep 0.05
done
if [[ -z "$windows_target" ]]; then
  echo "Windows test backend did not report its endpoint" >&2
  exit 1
fi
windows_http_address=${windows_target#tcp://}

: >"$bridge_log"
"$linux_bridge" serve \
  --listen tcp://127.0.0.1:0 \
  --worker "$windows_bridge" \
  --target "$windows_target" >"$bridge_log" 2>&1 &
bridge_pid=$!

wsl_address=
for _ in $(seq 1 100); do
  wsl_address=$(sed -n 's/^npu-bridge listening on \([^ ]*\) .*/\1/p' "$bridge_log" | head -n 1)
  [[ -n "$wsl_address" ]] && break
  sleep 0.05
done
if [[ -z "$wsl_address" ]]; then
  echo "WSL bridge did not report its endpoint" >&2
  exit 1
fi

run_wsl_series() {
  local count=$1
  local payload=$2
  local output=$3
  local url=$4
  local -a args=(
    --noproxy '*'
    --silent
    --show-error
    --fail
    --data-binary "@$payload"
    --write-out '%{time_total}\n'
  )
  for _ in $(seq 1 "$count"); do
    args+=(--output /dev/null "$url")
  done
  curl "${args[@]}" >"$output"
}

run_windows_series() {
  local count=$1
  local payload=$2
  local output=$3
  local url=$4
  local windows_payload
  windows_payload=$(wslpath -w "$payload")
  local -a args=(
    --noproxy '*'
    --silent
    --show-error
    --fail
    --data-binary "@$windows_payload"
    --write-out '%{time_total}\n'
  )
  for _ in $(seq 1 "$count"); do
    args+=(--output NUL "$url")
  done
  "$windows_curl" "${args[@]}" | tr -d '\r' >"$output"
}

run_windows_series "$small_requests" "$payload_small" "$bench_dir/direct-small.txt" "http://$windows_http_address/echo"
run_wsl_series "$small_requests" "$payload_small" "$bench_dir/bridge-small.txt" "http://$wsl_address/echo"
run_windows_series "$large_requests" "$payload_large" "$bench_dir/direct-large.txt" "http://$windows_http_address/echo"
run_wsl_series "$large_requests" "$payload_large" "$bench_dir/bridge-large.txt" "http://$wsl_address/echo"

wait "$backend_pid"
backend_pid=
kill "$bridge_pid"
wait "$bridge_pid" || true
bridge_pid=

echo "raw relay benchmark complete: $bench_dir"
echo "each curl process reuses one HTTP connection; line 1 is cold and remaining lines are warm"
