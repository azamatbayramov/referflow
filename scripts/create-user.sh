#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

read -r -p 'Email: ' account_email
read -r -p 'Имя: ' account_name
read -r -s -p 'Пароль (15–128 символов): ' account_password
printf '\n'
trap 'unset account_password' EXIT

if [[ "${1:-}" == host ]]; then
  set -a
  source .env
  set +a
  printf '%s\n' "$account_password" | (cd backend && "${GO:-go}" run ./cmd/manage create-user \
    --email "$account_email" --name "$account_name" --password-stdin)
else
  printf '%s\n' "$account_password" | docker compose exec -T api manage create-user \
    --email "$account_email" --name "$account_name" --password-stdin
fi
