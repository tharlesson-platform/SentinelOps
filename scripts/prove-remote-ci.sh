#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
for dependency in gh git jq; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done

remote_url=$(git -C "$ROOT" remote get-url origin 2>/dev/null || true)
test -n "$remote_url" || { echo "BLOCKED: remote origin nao configurado; informe conta/organizacao e visibilidade." >&2; exit 2; }

head_sha=$(git -C "$ROOT" rev-parse HEAD)
repository=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
runs=$(gh run list --repo "$repository" --commit "$head_sha" --limit 20 --json databaseId,workflowName,status,conclusion,headSha,url)
printf '%s' "$runs" | jq -e --arg sha "$head_sha" '
  any(.[]; .headSha == $sha and .workflowName == "ci" and .status == "completed" and .conclusion == "success")
' >/dev/null || { echo "BLOCKED: CI remoto terminal e verde nao encontrado para $head_sha." >&2; exit 1; }

default_branch=$(gh api "repos/$repository" --jq .default_branch)
protection=$(gh api "repos/$repository/branches/$default_branch/protection")
printf '%s' "$protection" | jq -e '.required_pull_request_reviews != null and .required_status_checks != null' >/dev/null || {
  echo "BLOCKED: branch protection sem PR reviews ou checks obrigatorios." >&2
  exit 1
}

printf 'remote_ci=PASS\nrepository=%s\ncommit=%s\ndefault_branch=%s\n' "$repository" "$head_sha" "$default_branch"
