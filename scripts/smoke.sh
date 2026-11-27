#!/usr/bin/env bash
set -euo pipefail

base_url="${BASE_URL:-http://127.0.0.1:8080}"
curl --fail --silent --show-error --max-time 10 "${base_url}/" >/dev/null
curl --fail --silent --show-error --max-time 10 "${base_url}/api/healthz"
curl --fail --silent --show-error --max-time 10 "${base_url}/api/readyz"
status="$(curl --silent --show-error --max-time 10 -o /dev/null -w '%{http_code}' "${base_url}/api/missing")"
if [[ "$status" != 404 ]]; then
  printf 'Expected unknown API route to return 404, got %s\n' "$status" >&2
  exit 1
fi
printf 'Smoke checks passed for %s\n' "$base_url"
