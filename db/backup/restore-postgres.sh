#!/usr/bin/env bash
# Restore a local snapshot into an empty, isolated PostgreSQL container.
set -euo pipefail

if (($# != 2)); then
  echo "Usage: $0 <snapshot-directory> <target-postgres-container>" >&2
  exit 2
fi
snapshot_dir="$(cd "$1" && pwd)"
postgres="$2"

(cd "$snapshot_dir" && shasum -a 256 -c SHA256SUMS >/dev/null)
source_project="$(cat "$snapshot_dir/compose-project.txt")"
target_project="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$postgres")"
if [[ "$target_project" == "$source_project" ]]; then
  echo "Refusing to restore into the source Compose project" >&2
  exit 1
fi
if [[ "$target_project" != "<no value>" && -n "$target_project" ]]; then
  for service in metapus-app metapus-worker; do
    if docker ps --filter "label=com.docker.compose.project=$target_project" \
      --filter "label=com.docker.compose.service=$service" --format '{{.ID}}' | grep -q .; then
      echo "Stop $service in target project $target_project before restore" >&2
      exit 1
    fi
  done
fi

docker exec "$postgres" pg_isready -U metapus -d tenants >/dev/null
table_count="$(docker exec "$postgres" psql -U metapus -d tenants -At -c \
  "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p');")"
if [[ "$table_count" != 0 ]]; then
  echo "Target tenants database is not empty" >&2
  exit 1
fi

shopt -s nullglob
tenant_dumps=("$snapshot_dir"/mt_*.dump)
if ((${#tenant_dumps[@]} == 0)); then
  echo "Snapshot contains no tenant dumps" >&2
  exit 1
fi
for dump in "${tenant_dumps[@]}"; do
  database="$(basename "$dump" .dump)"
  if [[ ! "$database" =~ ^mt_[a-zA-Z0-9_]+$ ]]; then
    echo "Invalid tenant database name in snapshot" >&2
    exit 1
  fi
  exists="$(docker exec "$postgres" psql -U metapus -d postgres -At \
    -c "SELECT 1 FROM pg_database WHERE datname='$database';")"
  if [[ -n "$exists" ]]; then
    echo "Target database $database already exists" >&2
    exit 1
  fi
done

docker exec -i "$postgres" pg_restore -U metapus --no-owner --no-acl -d tenants \
  < "$snapshot_dir/tenants.dump"
for dump in "${tenant_dumps[@]}"; do
  database="$(basename "$dump" .dump)"
  docker exec "$postgres" createdb -U metapus "$database"
  docker exec -i "$postgres" pg_restore -U metapus --no-owner --no-acl -d "$database" < "$dump"
done
echo "Restored PostgreSQL snapshot into $postgres; verify matching Vault state before starting workers."
