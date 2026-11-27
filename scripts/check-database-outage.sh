#!/usr/bin/env bash
set -euo pipefail

# Local/CI stack only. Never pass production configuration to this script.
cd "$(dirname "${BASH_SOURCE[0]}")/.."
base_url="${BASE_URL:-http://127.0.0.1:8080}"
restore_database() {
  docker compose up -d --wait --wait-timeout 120 db api web
}
trap restore_database EXIT
docker compose stop db
status="$(curl --silent --show-error --max-time 10 -o /dev/null -w '%{http_code}' "${base_url}/api/readyz")"
if [[ "$status" != 503 ]]; then
  printf 'Readiness must return 503 without PostgreSQL, got %s\n' "$status" >&2
  exit 1
fi
curl --fail --silent --show-error --max-time 10 "${base_url}/api/healthz"
printf 'Liveness remains healthy; readiness detects the database outage.\n'
