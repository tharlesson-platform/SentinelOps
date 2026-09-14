#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
source="$root/apps/postgresexporter/main.go"

test -s "$source" || { printf 'FAIL: exporter PostgreSQL ausente.\n' >&2; exit 1; }
test -s "$root/deploy/agents/database/postgres.env.example" || { printf 'FAIL: ambiente PostgreSQL ausente.\n' >&2; exit 1; }
docker compose --env-file "$root/.env" -f "$root/deploy/compose/docker-compose.yml" --profile database-monitor config --quiet
docker run --rm --entrypoint promtool -v "$root/deploy/prometheus":/etc/prometheus:ro \
  mirror.gcr.io/prom/prometheus:v3.13.2@sha256:508729e0e2d18e11fd742a5a5ca70e557b940a93948c3c95fd0123a6fd538b69 \
  check config /etc/prometheus/prometheus.yml
grep -Fq 'POSTGRES_MONITOR_DSN_FILE não pode conceder permissões' "$source"
grep -Fq 'TLS com validação de certificado' "$source"
grep -Fq 'sentinelops_postgres_blocked_locks' "$source"
grep -Fq 'postgres-exporter' "$root/scripts/lock-local-images.sh"
grep -Fq 'postgres-exporter' "$root/scripts/sign-local-image-lock.sh"
grep -Fq 'PostgreSQLBlockedLocks' "$root/deploy/prometheus/rules.yml"
if grep -Eqi '(^|[^A-Za-z])(insert|update|delete|alter|create|drop)[[:space:]]' "$source"; then
  printf 'FAIL: exporter PostgreSQL contém SQL ou operação de escrita.\n' >&2
  exit 1
fi
printf 'PASS: exporter PostgreSQL usa somente estatísticas agregadas read-only.\n'
