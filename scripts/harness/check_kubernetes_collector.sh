#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
kube_dir="$root/deploy/agents/kubernetes"
renderer="$root/scripts/render-kubernetes-rbac.py"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

for file in "$root/apps/kubeinventory/main.go" "$kube_dir/rbac-cluster.yaml" "$kube_dir/scope.example.json" "$renderer"; do
  test -s "$file" || { printf 'FAIL: artefato Kubernetes ausente: %s\n' "$file" >&2; exit 1; }
done
sh -n "$root/scripts/discover-kubernetes-inventory.sh"
python3 "$renderer" --scope "$kube_dir/scope.example.json" --output "$work/rbac.yaml"
grep -Fq 'namespace: applications-hml' "$work/rbac.yaml"
grep -Fq 'namespace: observability' "$work/rbac.yaml"
if grep -Eqi 'resources:.*secrets|verbs:.*(create|patch|update|delete|watch)' "$kube_dir/rbac-cluster.yaml"; then
  printf 'FAIL: RBAC Kubernetes contém leitura de segredo ou verbo excessivo.\n' >&2
  exit 1
fi
printf 'PASS: RBAC Kubernetes é read-only e namespace-scoped.\n'
