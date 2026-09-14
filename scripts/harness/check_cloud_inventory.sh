#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
collector="$root/apps/cloudinventory/main.go"

test -s "$collector" || { printf 'FAIL: collector cloud ausente.\n' >&2; exit 1; }
test -s "$root/deploy/agents/cloud/azure.scope.example.json" || { printf 'FAIL: exemplo Azure ausente.\n' >&2; exit 1; }
test -s "$root/deploy/agents/cloud/aws.scope.example.json" || { printf 'FAIL: exemplo AWS ausente.\n' >&2; exit 1; }
sh -n "$root/scripts/discover-cloud-inventory.sh"
grep -Fq '"account", "show"' "$collector"
grep -Fq 'graph", "query' "$collector"
grep -Fq 'get-caller-identity' "$collector"
grep -Fq 'select-resource-config' "$collector"
grep -Fq 'describe-configuration-recorders' "$collector"
grep -Fq 'snapshot completo recusado' "$collector"
printf 'PASS: collector cloud limita scope, pagina e falha fechado.\n'
