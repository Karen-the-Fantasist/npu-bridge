#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
dist_dir=${NPU_BRIDGE_DIST_DIR:-$repo_dir/dist}
bench_dir=${NPU_BRIDGE_BENCH_DIR:-${TMPDIR:-/tmp}/npu-bridge-cross-boundary}
linux_bridge="$dist_dir/npu-bridge-linux-amd64"
windows_bridge="$dist_dir/npu-bridge-windows-amd64.exe"
windows_backend="$dist_dir/npu-bridge-test-backend-windows-amd64.exe"

mkdir -p "$bench_dir"
backend_log="$bench_dir/backend.log"
bridge_log="$bench_dir/bridge.log"
doctor_log="$bench_dir/doctor.json"

for required in "$linux_bridge" "$windows_bridge" "$windows_backend"; do
  if [[ ! -f "$required" ]]; then
    echo "missing test binary: $required" >&2
    exit 1
  fi
done

bridge_pid=
backend_pid=
cleanup() {
  if [[ -n "$bridge_pid" ]] && kill -0 "$bridge_pid" 2>/dev/null; then
    kill "$bridge_pid" 2>/dev/null || true
    wait "$bridge_pid" 2>/dev/null || true
  fi
  if [[ -n "$backend_pid" ]]; then
    wait "$backend_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

: >"$backend_log"
"$windows_backend" --listen 127.0.0.1:0 --max-requests 2 --max-lifetime 15s >"$backend_log" 2>&1 &
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

"$linux_bridge" doctor --worker "$windows_bridge" --target "$windows_target" >"$doctor_log"

: >"$bridge_log"
"$linux_bridge" serve --listen tcp://127.0.0.1:0 --worker "$windows_bridge" --target "$windows_target" >"$bridge_log" 2>&1 &
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

request_file=$(mktemp)
response_file=$(mktemp)
stream_file=$(mktemp)
trap 'rm -f "$request_file" "$response_file" "$stream_file"; cleanup' EXIT
printf 'wsl\000windows\377bridge' >"$request_file"
curl --noproxy '*' --silent --show-error --fail --data-binary "@$request_file" "http://$wsl_address/echo" >"$response_file"
cmp "$request_file" "$response_file"

curl --noproxy '*' --silent --show-error --fail --no-buffer "http://$wsl_address/stream" >"$stream_file"
[[ $(grep -c '^data: {"index":' "$stream_file") -eq 3 ]]

wait "$backend_pid"
backend_pid=
kill "$bridge_pid"
wait "$bridge_pid" || true
bridge_pid=

echo "cross-boundary HTTP and SSE test passed"
echo "doctor report: $doctor_log"
echo "bridge log: $bridge_log"
