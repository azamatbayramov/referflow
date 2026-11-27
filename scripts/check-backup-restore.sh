#!/usr/bin/env bash
set -euo pipefail

# Dev/CI Compose only. Both databases are created here; no application database
# is dumped, restored, stopped or deleted. Production configuration is not read.
cd "$(dirname "${BASH_SOURCE[0]}")/.."
umask 077
work_dir="$(mktemp -d)"
source_db="referflow_check_source_${RANDOM}_${RANDOM}"
target_db="referflow_check_target_${RANDOM}_${RANDOM}"
source_created=false
target_created=false
cleanup() {
  status=$?
  trap - EXIT
  if $target_created; then
    docker compose -f compose.yaml exec -T db sh -eu -c 'dropdb -U "$POSTGRES_USER" "$1"' sh "$target_db" || status=1
  fi
  if $source_created; then
    docker compose -f compose.yaml exec -T db sh -eu -c 'dropdb -U "$POSTGRES_USER" "$1"' sh "$source_db" || status=1
  fi
  rm -rf -- "$work_dir"
  exit "$status"
}
trap cleanup EXIT

docker compose -f compose.yaml exec -T db sh -eu -c 'createdb -U "$POSTGRES_USER" "$1"' sh "$source_db"
source_created=true
docker compose -f compose.yaml exec -T db sh -eu -c 'createdb -U "$POSTGRES_USER" "$1"' sh "$target_db"
target_created=true

docker compose -f compose.yaml exec -T -e PGDATABASE="$source_db" api manage migrate
docker compose -f compose.yaml exec -T db sh -eu -c 'psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"' sh "$source_db" < scripts/fixtures/backup.sql

# Ordered per-table digests cover every field, including contact data, permanent
# operation keys, snapshots, FKs, cancellation audit and migration checksums.
cat > "$work_dir/signatures.sql" <<'SQL'
SELECT format('SELECT %L, count(*), md5(COALESCE(string_agg(row_to_json(t)::text, %L ORDER BY row_to_json(t)::text), %L)) FROM %I.%I t;',
    tablename, E'\n', '', schemaname, tablename)
FROM pg_tables WHERE schemaname='public' ORDER BY tablename
\gexec
SQL
signature() {
  docker compose -f compose.yaml exec -T db sh -eu -c 'psql -X -qAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"' sh "$1" < "$work_dir/signatures.sql"
}
signature "$source_db" > "$work_dir/source.signature"
# Guard against an accidentally empty fixture/check rather than accepting 0=0.
test "$(wc -l < "$work_dir/source.signature")" -eq 10
if grep -q '|0|' "$work_dir/source.signature"; then
  printf 'Backup fixture unexpectedly contains an empty table\n' >&2
  exit 1
fi

docker compose -f compose.yaml exec -T db sh -eu -c 'pg_dump -U "$POSTGRES_USER" -d "$1" -Fc' sh "$source_db" > "$work_dir/backup.dump"
docker compose -f compose.yaml exec -T db sh -eu -c 'pg_restore -U "$POSTGRES_USER" -d "$1" --exit-on-error --single-transaction' sh "$target_db" < "$work_dir/backup.dump"
docker compose -f compose.yaml exec -T -e PGDATABASE="$target_db" api manage migrate
signature "$target_db" > "$work_dir/target.signature"
if ! cmp -s "$work_dir/source.signature" "$work_dir/target.signature"; then
  printf 'Backup restore data or migration signatures differ\n' >&2
  exit 1
fi
printf 'Backup/restore verified on isolated artificial databases: all 10 tables and migration checksums preserved.\n'
