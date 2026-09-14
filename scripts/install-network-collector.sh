#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
COLLECTOR_ROOT="$ROOT/deploy/agents/network"
PHASE=all
TARGETS_FILE=""
TLS_CA_FILE=""
TLS_CERT_FILE=""
TLS_KEY_FILE=""
ENV_FILE="$COLLECTOR_ROOT/.env"
WITH_SYSLOG=false

die() { printf '%s\n' "[sentinelops][network-collector][erro] $*" >&2; exit 1; }
log() { printf '%s\n' "[sentinelops][network-collector] $*"; }
usage() {
  cat <<'EOF'
Uso: ./scripts/install-network-collector.sh [opções]

  --phase preflight|configure|deploy|verify|all
  --targets FILE              allowlist JSON de IPs SNMP exatos
  --tls-ca-file FILE          CA do gateway mTLS
  --tls-cert-file FILE        certificado mTLS exclusivo do collector
  --tls-key-file FILE         chave mTLS exclusiva do collector
  --env-file FILE             arquivo 0600 com endpoints e segredos SNMPv3
  --with-syslog               habilita listener UDP/TCP do site, após ACL aprovada

O arquivo de ambiente é preparado fora da linha de comando. Ele nunca é
impresso pelo script e deve conter as três variáveis SNMPv3 exigidas. Syslog
exige também endpoint Loki mTLS e IP de bind não-loopback aprovado pelo site.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --phase) PHASE=${2:?}; shift 2 ;;
    --targets) TARGETS_FILE=${2:?}; shift 2 ;;
    --tls-ca-file) TLS_CA_FILE=${2:?}; shift 2 ;;
    --tls-cert-file) TLS_CERT_FILE=${2:?}; shift 2 ;;
    --tls-key-file) TLS_KEY_FILE=${2:?}; shift 2 ;;
    --env-file) ENV_FILE=${2:?}; shift 2 ;;
    --with-syslog) WITH_SYSLOG=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "opção desconhecida: $1" ;;
  esac
done

case "$PHASE" in preflight|configure|deploy|verify|all) ;; *) die "fase inválida: $PHASE" ;; esac
phase_selected() { [ "$PHASE" = all ] || [ "$PHASE" = "$1" ]; }
compose() {
  if [ "$WITH_SYSLOG" = true ]; then
    SENTINEL_NETWORK_ENV_FILE="$ENV_FILE" docker compose --env-file "$ENV_FILE" -f "$COLLECTOR_ROOT/docker-compose.yml" --profile syslog "$@"
  else
    SENTINEL_NETWORK_ENV_FILE="$ENV_FILE" docker compose --env-file "$ENV_FILE" -f "$COLLECTOR_ROOT/docker-compose.yml" "$@"
  fi
}

validate_env() {
  [ -s "$ENV_FILE" ] || die "arquivo de ambiente ausente ou vazio: $ENV_FILE"
  [ "$(stat -f '%Lp' "$ENV_FILE" 2>/dev/null || stat -c '%a' "$ENV_FILE" 2>/dev/null || printf 999)" -le 600 ] || die "arquivo de ambiente precisa de permissão 0600 ou mais restrita"
  for key in SENTINEL_METRICS_ENDPOINT SENTINEL_TLS_SERVER_NAME SENTINEL_SNMP_V3_USERNAME SENTINEL_SNMP_V3_AUTH_PASSWORD SENTINEL_SNMP_V3_PRIV_PASSWORD; do
    value=$(awk -F= -v wanted="$key" '$1 == wanted {print substr($0, index($0, "=") + 1); exit}' "$ENV_FILE")
    [ -n "$value" ] || die "$key ausente no arquivo de ambiente"
    [ "$value" != replace-me ] || die "$key ainda contém placeholder"
  done
  metrics_endpoint=$(awk -F= '$1 == "SENTINEL_METRICS_ENDPOINT" {print substr($0, index($0, "=") + 1); exit}' "$ENV_FILE")
  printf '%s' "$metrics_endpoint" | grep -Eq '^https://[^[:space:]?#[\]]+$' || die "SENTINEL_METRICS_ENDPOINT deve usar HTTPS sem query ou fragmento"
}

env_value() { awk -F= -v wanted="$1" '$1 == wanted {print substr($0, index($0, "=") + 1); exit}' "$ENV_FILE"; }

validate_syslog_env() {
  for key in SENTINEL_LOGS_ENDPOINT SENTINEL_SYSLOG_BIND_ADDRESS SENTINEL_SYSLOG_PORT; do
    value=$(env_value "$key")
    [ -n "$value" ] || die "$key ausente no arquivo de ambiente"
    [ "$value" != replace-me ] || die "$key ainda contém placeholder"
  done
  logs_endpoint=$(env_value SENTINEL_LOGS_ENDPOINT)
  printf '%s' "$logs_endpoint" | grep -Eq '^https://[^[:space:]?#[\]]+$' || die "SENTINEL_LOGS_ENDPOINT deve usar HTTPS sem query ou fragmento"
  syslog_bind=$(env_value SENTINEL_SYSLOG_BIND_ADDRESS)
  python3 - "$syslog_bind" <<'PY' || die "SENTINEL_SYSLOG_BIND_ADDRESS deve ser IP literal unicast não-loopback"
import ipaddress
import sys
try:
    address = ipaddress.ip_address(sys.argv[1])
except ValueError:
    raise SystemExit(1)
raise SystemExit(0 if not (address.is_loopback or address.is_unspecified or address.is_multicast) else 1)
PY
  syslog_port=$(env_value SENTINEL_SYSLOG_PORT)
  case "$syslog_port" in ''|*[!0-9]*) die "SENTINEL_SYSLOG_PORT deve ser inteiro entre 1 e 65535";; esac
  [ "$syslog_port" -ge 1 ] && [ "$syslog_port" -le 65535 ] || die "SENTINEL_SYSLOG_PORT deve ser inteiro entre 1 e 65535"
}

copy_tls_file() {
  source_file=$1
  destination_file=$2
  [ -s "$source_file" ] || die "arquivo TLS ausente ou vazio: $source_file"
  install -m 0600 "$source_file" "$destination_file"
}

preflight() {
  command -v docker >/dev/null 2>&1 || die "docker não encontrado"
  command -v python3 >/dev/null 2>&1 || die "python3 não encontrado"
  docker info >/dev/null 2>&1 || die "Docker indisponível"
  log 'preflight concluído sem sondar alvo SNMP e sem alterar equipamento.'
}

configure() {
  validate_env
  [ "$WITH_SYSLOG" = false ] || validate_syslog_env
  [ -s "$TARGETS_FILE" ] || die "--targets é obrigatório na configuração"
  umask 077
  mkdir -p "$COLLECTOR_ROOT/certs"
  copy_tls_file "$TLS_CA_FILE" "$COLLECTOR_ROOT/certs/ca.crt"
  copy_tls_file "$TLS_CERT_FILE" "$COLLECTOR_ROOT/certs/client.crt"
  copy_tls_file "$TLS_KEY_FILE" "$COLLECTOR_ROOT/certs/client.key"
  python3 "$ROOT/scripts/render-network-targets.py" --targets "$TARGETS_FILE" --output "$COLLECTOR_ROOT/targets.generated.alloy"
  install -m 0600 "$COLLECTOR_ROOT/snmp-auth.yml.tmpl" "$COLLECTOR_ROOT/snmp-auth.yml"
  compose config --quiet
  log 'allowlist SNMP renderizada; nenhuma conexão SNMP foi aberta nesta fase.'
}

deploy() {
  compose up -d --remove-orphans
  if [ "$WITH_SYSLOG" = true ]; then
    log 'collector e listener syslog iniciados; confirme firewall somente dos emissores aprovados.'
  else
    log 'collector iniciado; a única porta publicada é a administração Alloy em loopback.'
  fi
}

wait_ready() {
  port=$1
  name=$2
  attempts=0
  until curl -fsS "http://127.0.0.1:$port/-/ready" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 30 ] || die "$name não ficou ready; consulte compose logs sem expor segredos"
    sleep 2
  done
}

verify() {
  validate_env
  [ "$WITH_SYSLOG" = false ] || validate_syslog_env
  admin_port=$(env_value SENTINEL_NETWORK_COLLECTOR_ADMIN_PORT)
  [ -n "$admin_port" ] || admin_port=12347
  wait_ready "$admin_port" Alloy
  if [ "$WITH_SYSLOG" = true ]; then
    syslog_admin_port=$(env_value SENTINEL_SYSLOG_ADMIN_PORT)
    [ -n "$syslog_admin_port" ] || syslog_admin_port=12348
    wait_ready "$syslog_admin_port" syslog-Alloy
  fi
  compose ps
  log 'Alloy pronto. Valide cada equipamento autorizado por asset_id, source e timestamps no gateway; scrape HTTP 200 isolado não aprova SNMP.'
}

phase_selected preflight && preflight
phase_selected configure && configure
phase_selected deploy && deploy
phase_selected verify && verify
