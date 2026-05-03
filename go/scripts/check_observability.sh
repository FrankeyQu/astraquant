#!/usr/bin/env sh
set -eu

base_url="${1:-${NOF0_BASE_URL:-http://127.0.0.1:8888}}"
timeout="${NOF0_PROBE_TIMEOUT:-3}"

fetch() {
  path="$1"
  curl -fsS --max-time "$timeout" "${base_url}${path}"
}

fetch "/healthz" >/dev/null
fetch "/readyz" >/dev/null

metrics="$(fetch "/debug/vars")"
for key in db_writes_total persistence_latency_seconds cache_ops_total inconsistency_counters_total; do
  printf '%s' "$metrics" | grep -q "\"$key\"" || {
    printf 'missing expvar map: %s\n' "$key" >&2
    exit 1
  }
done

printf 'observability endpoints ok: %s\n' "$base_url"
