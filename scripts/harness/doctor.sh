#!/bin/sh
set -eu
printf 'HARNESS_RESULT=PASS\nchecked_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
for tool in docker python3 git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FAIL: ferramenta ausente: $tool" >&2; exit 1; }
  printf 'tool.%s=%s\n' "$tool" "$(command -v "$tool")"
done
docker version --format 'docker.server={{.Server.Version}}' 2>/dev/null || { echo 'BLOCKED: Docker daemon indisponível' >&2; exit 2; }
docker compose version
git rev-parse --short HEAD
git diff --quiet || printf 'dirty_diff=true\n'
