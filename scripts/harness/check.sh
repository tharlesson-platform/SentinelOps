#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mkdir -p "$root/artifacts/evidence"
run_id="local-check-$(date -u +%Y%m%dT%H%M%SZ)-$(git -C "$root" rev-parse --short HEAD)"
evidence="$root/artifacts/evidence/$run_id"
mkdir -p "$evidence"
printf 'run_id=%s\nsha=%s\nstarted_at=%s\ncommand=make harness-check\n' "$run_id" "$(git -C "$root" rev-parse HEAD)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$evidence/metadata.txt"
docker run --rm -v "$root":/src -w /src golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 sh -ec 'test -z "$(gofmt -l apps internal)" && go vet ./... && go test -race ./...'
docker run --rm -v "$root/apps/web":/app -w /app node:26.7.0-alpine3.23@sha256:ce3cc39fe3b8b2602d3b1c4d63d301e46b48c550ecb627869853ddcdda418b63 sh -ec 'npm ci --ignore-scripts --no-audit --no-fund && npm test && npm run build'
"$root/scripts/harness/check_windows_collector.sh"
"$root/scripts/harness/check_network_collector.sh"
"$root/scripts/harness/check_cloud_inventory.sh"
"$root/scripts/harness/check_kubernetes_collector.sh"
"$root/scripts/harness/check_postgres_exporter.sh"
"$root/scripts/harness/check_apm_onboarding.sh"
"$root/scripts/harness/check_lifecycle.sh"
printf 'result=PASS\nfinished_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$evidence/metadata.txt"
printf 'PASS: evidência sanitizada em %s\n' "$evidence"
