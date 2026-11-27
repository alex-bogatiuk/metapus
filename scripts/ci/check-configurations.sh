#!/usr/bin/env bash
# Builds and checks every configuration of the repository the way its images
# are built: the Go commands of each configuration and the frontend assembled
# for it (frontend/scripts/configure.cjs). "none" is the platform alone.
#
#   scripts/ci/check-configurations.sh            # none, crypto, trade, autoparts
#   scripts/ci/check-configurations.sh autoparts  # one configuration (an add-on with its base)
#   SKIP_FRONTEND_BUILD=1 ...                     # typecheck only, no next build
#
# Go unit tests (configurationtest.Check per configuration) and the
# integration gate (go run ./cmd/integration-test) cover the rest.
set -euo pipefail

cd "$(dirname "$0")/../.."
configurations=("$@")
if [ ${#configurations[@]} -eq 0 ]; then
  configurations=(none crypto trade autoparts)
fi
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

for name in "${configurations[@]}"; do
  echo "=== ${name}: Go commands ==="
  if [ "$name" = none ]; then
    commands=(server worker tenant seed)
  else
    commands=("${name}-server" "${name}-worker" "${name}-tenant" "${name}-seed")
  fi
  for command in "${commands[@]}"; do
    go build -o "$out/$command" "./cmd/$command"
  done

  echo "=== ${name}: frontend ==="
  (
    cd frontend
    npm run --silent configure -- "$name"
    npx tsc --noEmit
    if [ -z "${SKIP_FRONTEND_BUILD:-}" ]; then
      METAPUS_CONFIGURATION="$name" npm run --silent build
    fi
  )
done

echo "=== HTTP contracts of every configuration ==="
go run ./cmd/http-contracts -check
