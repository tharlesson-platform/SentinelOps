#!/bin/sh
set -eu
if [ -z "${SENTINELOPS_TEST_DATABASE_MIGRATION_URL:-}" ] || [ -z "${SENTINELOPS_TEST_DATABASE_URL:-}" ]; then
  echo 'BLOCKED: configure URLs de banco isolado para migration e runtime' >&2
  exit 2
fi
network_args=""
if [ -n "${SENTINELOPS_TEST_DOCKER_NETWORK:-}" ]; then
  case "$SENTINELOPS_TEST_DOCKER_NETWORK" in
    *[!a-zA-Z0-9_.-]*|'') echo 'BLOCKED: SENTINELOPS_TEST_DOCKER_NETWORK inválida' >&2; exit 2 ;;
  esac
  network_args="--network $SENTINELOPS_TEST_DOCKER_NETWORK"
fi
# shellcheck disable=SC2086
exec docker run --rm --add-host=host.docker.internal:host-gateway $network_args -e SENTINELOPS_TEST_DATABASE_MIGRATION_URL -e SENTINELOPS_TEST_DATABASE_URL -v "$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)":/src -w /src golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c go test -race ./internal/database ./internal/events ./internal/httpapi -run TestPostgres
