#!/usr/bin/env bash
# Consistent local PostgreSQL snapshot of a running public test Compose project.
set -euo pipefail

project="${1:-metapus-public-test}"
if [[ ! "$project" =~ ^[a-zA-Z0-9_-]+$ ]]; then
  echo "Invalid Compose project name" >&2
  exit 2
fi

backup_root="$(cd "$(dirname "$0")" && pwd)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="$backup_root/${project}-${stamp}"
mkdir -m 700 "$backup_dir"

container_for() {
  local service="$1" matches
  matches="$(docker ps -a --filter "label=com.docker.compose.project=$project" \
    --filter "label=com.docker.compose.service=$service" --format '{{.ID}}')"
  if [[ -z "$matches" || "$matches" == *$'\n'* ]]; then
    echo "Expected exactly one $service container in $project" >&2
    return 1
  fi
  printf '%s' "$matches"
}

postgres="$(container_for postgres)"
app="$(container_for metapus-app)"
worker="$(container_for metapus-worker)"
stopped=()
restart_stopped() {
  if ((${#stopped[@]})); then
    docker start "${stopped[@]}" >/dev/null
  fi
}
trap restart_stopped EXIT

for container in "$app" "$worker"; do
  if [[ "$(docker inspect --format '{{.State.Running}}' "$container")" == true ]]; then
    docker stop "$container" >/dev/null
    stopped+=("$container")
  fi
done

tenant_list="$(docker exec "$postgres" psql -U metapus -d tenants \
  -At -c 'SELECT db_name FROM tenants ORDER BY db_name')"
tenant_databases=()
while IFS= read -r database; do
  if [[ -n "$database" ]]; then
    tenant_databases+=("$database")
  fi
done <<< "$tenant_list"
if ((${#tenant_databases[@]} == 0)); then
  echo "No tenant databases found" >&2
  exit 1
fi

umask 077
docker exec "$postgres" pg_dump -U metapus -Fc -d tenants > "$backup_dir/tenants.dump"
for database in "${tenant_databases[@]}"; do
  if [[ ! "$database" =~ ^[a-zA-Z0-9_]+$ ]]; then
    echo "Invalid tenant database name" >&2
    exit 1
  fi
  docker exec "$postgres" pg_dump -U metapus -Fc -d "$database" > "$backup_dir/$database.dump"
done
docker exec "$postgres" pg_dumpall -U metapus --globals-only > "$backup_dir/globals.sql"

for dump in "$backup_dir"/*.dump; do
  docker exec -i "$postgres" pg_restore --list < "$dump" >/dev/null
done
(cd "$backup_dir" && shasum -a 256 *.dump globals.sql > SHA256SUMS)
printf '%s\n' "$project" > "$backup_dir/compose-project.txt"
printf '%s\n' "$stamp" > "$backup_dir/captured-at-utc.txt"
chmod 600 "$backup_dir"/*

restart_stopped
stopped=()
trap - EXIT
echo "Snapshot ready: $backup_dir"
