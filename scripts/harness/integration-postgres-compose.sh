#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
project="sentinelops-it-$(date -u +%Y%m%d%H%M%S)-$$"
compose="docker compose -p $project --env-file $root/.env -f $root/deploy/compose/docker-compose.yml"
network="${project}_control"
packages="${SENTINELOPS_INTEGRATION_PACKAGES:-./internal/database ./internal/events ./internal/httpapi}"

case "$packages" in
  ''|*[!A-Za-z0-9_./\ -]*)
    echo 'BLOCKED: SENTINELOPS_INTEGRATION_PACKAGES aceita somente paths Go separados por espaço' >&2
    exit 2
    ;;
esac

cleanup() {
  # The unique UTC/PID project can only own resources created by this run.
  # shellcheck disable=SC2086
  $compose down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

test -f "$root/.env" || { echo 'BLOCKED: .env local é obrigatório' >&2; exit 2; }
# shellcheck disable=SC2086
$compose up -d postgres >/dev/null
# shellcheck disable=SC2086
$compose run --rm database-role-bootstrap >/dev/null

docker run --rm --network "$network" --env-file "$root/.env" -e "SENTINELOPS_INTEGRATION_PACKAGES=$packages" -v "$root":/src -w /src \
  golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 \
  sh -ec 'export SENTINELOPS_TEST_DATABASE_MIGRATION_URL="postgres://sentinel:${POSTGRES_PASSWORD}@postgres:5432/sentinel?sslmode=disable"; export SENTINELOPS_TEST_DATABASE_URL="postgres://sentinel_app:${POSTGRES_PASSWORD}@postgres:5432/sentinel?sslmode=disable"; go test -race $SENTINELOPS_INTEGRATION_PACKAGES -run TestPostgres'
