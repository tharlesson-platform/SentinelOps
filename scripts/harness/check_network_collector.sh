#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
renderer="$root/scripts/render-network-targets.py"
network_dir="$root/deploy/agents/network"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

for file in "$renderer" "$network_dir/docker-compose.yml" "$network_dir/config.alloy" "$network_dir/config-syslog.alloy" "$network_dir/snmp-auth.yml.tmpl"; do
  test -s "$file" || { printf 'FAIL: artefato de rede ausente: %s\n' "$file" >&2; exit 1; }
done

sh -n "$root/scripts/install-network-collector.sh"
SENTINEL_NETWORK_ENV_FILE=.env.example docker compose --env-file "$network_dir/.env.example" -f "$network_dir/docker-compose.yml" config --quiet
SENTINEL_NETWORK_ENV_FILE=.env.example docker compose --env-file "$network_dir/.env.example" -f "$network_dir/docker-compose.yml" --profile syslog config --quiet
docker run --rm -v "$network_dir/config-syslog.alloy":/etc/alloy/config-syslog.alloy:ro \
  -e SENTINEL_HOST_NAME=check -e SENTINEL_ENVIRONMENT=hml -e SENTINEL_TEAM=network \
  -e SENTINEL_LOGS_ENDPOINT=https://example.invalid/loki/api/v1/push -e SENTINEL_TLS_SERVER_NAME=example.invalid \
  sentinelops-alloy:1.18.1-patched.2 validate /etc/alloy/config-syslog.alloy

python3 "$renderer" --targets "$network_dir/targets.json.example" --output "$work/targets.generated.alloy"
grep -Fq 'discovery.static "network_devices"' "$work/targets.generated.alloy"
grep -Fq '__param_target = "udp://192.0.2.10:161"' "$work/targets.generated.alloy"
grep -Fq '__address__ = "snmp-exporter:9116"' "$work/targets.generated.alloy"

printf '%s\n' '{"targets":[{"asset_id":"bad","address":"router.example.invalid","site":"lab","environment":"hml","team":"network","module":"if_mib"}]}' > "$work/hostname.json"
if python3 "$renderer" --targets "$work/hostname.json" --output "$work/invalid.alloy" >/dev/null 2>&1; then
  printf 'FAIL: hostname foi aceito como target SNMP.\n' >&2
  exit 1
fi

printf '%s\n' '{"targets":[{"asset_id":"bad","address":"10.0.0.0/8","site":"lab","environment":"hml","team":"network","module":"if_mib"}]}' > "$work/cidr.json"
if python3 "$renderer" --targets "$work/cidr.json" --output "$work/invalid.alloy" >/dev/null 2>&1; then
  printf 'FAIL: CIDR foi aceito como target SNMP.\n' >&2
  exit 1
fi

grep -Fq 'security_level: authPriv' "$network_dir/snmp-auth.yml.tmpl"
grep -Fq -- '--config.expand-environment-variables' "$network_dir/docker-compose.yml"
grep -Fq 'read_only: true' "$network_dir/docker-compose.yml"
grep -Fq 'cap_drop:' "$network_dir/docker-compose.yml"
grep -Fq 'profiles: [syslog]' "$network_dir/docker-compose.yml"
grep -Fq 'loki.source.syslog "network_udp"' "$network_dir/config-syslog.alloy"
grep -Fq 'loki.source.syslog "network_tcp"' "$network_dir/config-syslog.alloy"
grep -Fq 'possible_secret' "$network_dir/config-syslog.alloy"
grep -Fq -- '--with-syslog' "$root/scripts/install-network-collector.sh"
grep -Eq '^SENTINEL_SNMP_EXPORTER_IMAGE=prom/snmp-exporter:v0\.30\.1@sha256:[0-9a-f]{64}$' "$network_dir/.env.example"
grep -Eq 'SENTINEL_SNMP_EXPORTER_IMAGE:-prom/snmp-exporter:v0\.30\.1@sha256:[0-9a-f]{64}' "$network_dir/docker-compose.yml"
printf 'PASS: SNMP allowlist, syslog mTLS e isolamento local validados.\n'
