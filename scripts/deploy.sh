#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ ! -f .env.production ]]; then
  printf 'Create .env.production from deploy/.env.production.example first. See docs/deployment.md.\n' >&2
  exit 1
fi

compose=(docker compose --env-file .env.production -f deploy/compose.yaml)
# -q validates required variables without printing credentials.
"${compose[@]}" config --quiet
# Pull every image before touching running containers.
"${compose[@]}" pull
"${compose[@]}" up -d --wait --wait-timeout 120 db
# Migrate once, before starting any replicas of the new API.
"${compose[@]}" run --rm --no-deps --entrypoint manage api migrate
"${compose[@]}" up -d --wait --wait-timeout 120 --remove-orphans
"${compose[@]}" ps
