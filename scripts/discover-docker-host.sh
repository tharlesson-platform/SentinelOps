#!/bin/sh
set -eu

# Generates a sanitized, read-only Docker host inventory. It never reads
# container environments, labels, log contents, mounted file contents or secrets.
OUTPUT_DIR=""

usage() {
  cat <<'EOF'
Uso: ./scripts/discover-docker-host.sh [--output DIRETORIO]

Gera um inventário sanitizado do host, Docker, containers, redes e listeners.
Execute localmente no host-alvo. Nenhum container é reiniciado ou alterado.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) OUTPUT_DIR=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf '%s\n' "Opção desconhecida: $1" >&2; exit 2 ;;
  esac
done

for command in docker jq uname hostname date df ss; do
  command -v "$command" >/dev/null 2>&1 || { printf '%s\n' "Dependência ausente: $command" >&2; exit 1; }
done

host=$(hostname -s | tr -cd 'A-Za-z0-9._-')
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
OUTPUT_DIR=${OUTPUT_DIR:-"./artifacts/discovery/${host}-${timestamp}"}
umask 077
mkdir -p "$OUTPUT_DIR"

docker_info=$(docker info --format '{{json .}}')
docker_version=$(docker version --format '{{json .}}')
containers=$(docker ps -a --no-trunc --format '{{json .}}' | jq -s '[.[] | {id:.ID,name:.Names,image:.Image,state:.State,status:.Status,ports:.Ports}]')
networks=$(docker network ls --format '{{json .}}' | jq -s '[.[] | {id:.ID,name:.Name,driver:.Driver,scope:.Scope}]')
listeners=$(ss -ltnH | awk '{print $4}' | sort -u | jq -Rsc 'split("\n") | map(select(length > 0))')
filesystems=$(df -P -x tmpfs -x devtmpfs | awk 'NR > 1 {print $1 "|" $2 "|" $3 "|" $4 "|" $5 "|" $6}' | jq -Rsc 'split("\n") | map(select(length > 0) | split("|") | {device:.[0],blocks:.[1],used:.[2],available:.[3],capacity:.[4],mountpoint:.[5]})')

jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg hostname "$host" \
  --arg os "$(uname -sr)" \
  --argjson docker_info "$docker_info" \
  --argjson docker_version "$docker_version" \
  --argjson containers "$containers" \
  --argjson networks "$networks" \
  --argjson listeners "$listeners" \
  --argjson filesystems "$filesystems" \
  '{generatedAt:$generated_at,hostname:$hostname,operatingSystem:$os,docker:{serverVersion:$docker_version.Server.Version,rootDir:$docker_info.DockerRootDir,loggingDriver:$docker_info.LoggingDriver,cgroupDriver:$docker_info.CgroupDriver,containers:$containers,networks:$networks},listeners:$listeners,filesystems:$filesystems}' \
  > "$OUTPUT_DIR/inventory.json"

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$OUTPUT_DIR/inventory.json" > "$OUTPUT_DIR/inventory.json.sha256"
else
  shasum -a 256 "$OUTPUT_DIR/inventory.json" > "$OUTPUT_DIR/inventory.json.sha256"
fi
chmod 600 "$OUTPUT_DIR/inventory.json" "$OUTPUT_DIR/inventory.json.sha256"
printf 'Inventário sanitizado: %s\n' "$OUTPUT_DIR/inventory.json"
