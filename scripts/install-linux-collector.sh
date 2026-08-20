#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

COLLECTOR_ROOT="$ROOT/deploy/agents/linux"
PHASE=all
INSTALL_RUNTIME=false
HOST_NAME=$(hostname -s 2>/dev/null || hostname)
HOST_NAME_SET=false
ENVIRONMENT=development
TEAM=platform
LOCATION=on-premises
METRICS_ENDPOINT=""
LOGS_ENDPOINT=""
OTLP_ENDPOINT=""
WITH_CONTAINERS=false
WITH_CONTAINERS_SET=false
ALLOW_INSECURE=false
ENABLE_SERVICE=false
ADMIN_PORT=""
OTLP_GRPC_PORT=""
OTLP_HTTP_PORT=""

usage() {
  cat <<'EOF'
Uso: ./scripts/install-linux-collector.sh [opções]

  --phase preflight|runtime|configure|deploy|verify|service|all
  --install-runtime
  --host-name NAME
  --environment DEV|STG|HML|PRD
  --team TEAM
  --location LOCATION
  --metrics-endpoint URL       Prometheus remote write
  --logs-endpoint URL          Loki push API
  --otlp-endpoint URL          OTLP HTTP base
  --with-containers            habilita cAdvisor com acesso privilegiado ao host
  --admin-port PORT            porta local da UI/readiness Alloy
  --otlp-grpc-port PORT        receiver local para aplicações
  --otlp-http-port PORT        receiver local para aplicações
  --allow-insecure             aceita HTTP fora de loopback conscientemente
  --enable-service             instala uma unit systemd
  --help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --phase) PHASE=$2; shift 2 ;;
    --install-runtime) INSTALL_RUNTIME=true; shift ;;
    --host-name) HOST_NAME=$2; HOST_NAME_SET=true; shift 2 ;;
    --environment) ENVIRONMENT=$2; shift 2 ;;
    --team) TEAM=$2; shift 2 ;;
    --location) LOCATION=$2; shift 2 ;;
    --metrics-endpoint) METRICS_ENDPOINT=$2; shift 2 ;;
    --logs-endpoint) LOGS_ENDPOINT=$2; shift 2 ;;
    --otlp-endpoint) OTLP_ENDPOINT=$2; shift 2 ;;
    --with-containers) WITH_CONTAINERS=true; WITH_CONTAINERS_SET=true; shift ;;
    --admin-port) ADMIN_PORT=$2; shift 2 ;;
    --otlp-grpc-port) OTLP_GRPC_PORT=$2; shift 2 ;;
    --otlp-http-port) OTLP_HTTP_PORT=$2; shift 2 ;;
    --allow-insecure) ALLOW_INSECURE=true; shift ;;
    --enable-service) ENABLE_SERVICE=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

case "$PHASE" in preflight|runtime|configure|deploy|verify|service|all) ;; *) die "Fase inválida: $PHASE" ;; esac
collector_env_value() {
  key=$1
  [ -f "$COLLECTOR_ROOT/.env" ] || return 0
  awk -F= -v wanted_key="$key" '$1 == wanted_key { print substr($0, index($0, "=") + 1); exit }' "$COLLECTOR_ROOT/.env"
}
if [ "$PHASE" = configure ] || [ "$PHASE" = all ]; then
  [ -n "$ADMIN_PORT" ] || ADMIN_PORT=12345
  [ -n "$OTLP_GRPC_PORT" ] || OTLP_GRPC_PORT=4317
  [ -n "$OTLP_HTTP_PORT" ] || OTLP_HTTP_PORT=4318
else
  if [ "$HOST_NAME_SET" = false ] && [ -n "$(collector_env_value SENTINEL_HOST_NAME)" ]; then
    HOST_NAME=$(collector_env_value SENTINEL_HOST_NAME)
  fi
  [ -n "$ADMIN_PORT" ] || ADMIN_PORT=$(collector_env_value SENTINEL_COLLECTOR_ADMIN_PORT)
  [ -n "$OTLP_GRPC_PORT" ] || OTLP_GRPC_PORT=$(collector_env_value SENTINEL_COLLECTOR_OTLP_GRPC_PORT)
  [ -n "$OTLP_HTTP_PORT" ] || OTLP_HTTP_PORT=$(collector_env_value SENTINEL_COLLECTOR_OTLP_HTTP_PORT)
  [ -n "$ADMIN_PORT" ] || ADMIN_PORT=12345
  [ -n "$OTLP_GRPC_PORT" ] || OTLP_GRPC_PORT=4317
  [ -n "$OTLP_HTTP_PORT" ] || OTLP_HTTP_PORT=4318
  if [ "$WITH_CONTAINERS_SET" = false ] && [ "$(collector_env_value SENTINEL_COLLECT_CONTAINERS)" = true ]; then
    WITH_CONTAINERS=true
  fi
fi
validate_name "$HOST_NAME" host-name
validate_name "$ENVIRONMENT" environment
validate_name "$TEAM" team
validate_name "$LOCATION" location
if [ "$PHASE" = configure ] || [ "$PHASE" = all ]; then
  [ -n "$METRICS_ENDPOINT" ] || die "--metrics-endpoint é obrigatório na configuração"
  [ -n "$LOGS_ENDPOINT" ] || die "--logs-endpoint é obrigatório na configuração"
  [ -n "$OTLP_ENDPOINT" ] || die "--otlp-endpoint é obrigatório na configuração"
  for endpoint in "$METRICS_ENDPOINT" "$LOGS_ENDPOINT" "$OTLP_ENDPOINT"; do
    printf '%s' "$endpoint" | grep -Eq '^https?://[^[:space:]]+$' || die "Endpoint inválido: $endpoint"
    case "$endpoint" in
      http://127.0.0.1:*|http://localhost:*|https://*) ;;
      http://*) [ "$ALLOW_INSECURE" = true ] || die "HTTP remoto recusado: $endpoint. Use HTTPS ou --allow-insecure em rede privada." ;;
    esac
  done
fi
for port in "$ADMIN_PORT" "$OTLP_GRPC_PORT" "$OTLP_HTTP_PORT"; do
  printf '%s' "$port" | grep -Eq '^[0-9]{1,5}$' || die "Porta inválida: $port"
  [ "$port" -ge 1 ] && [ "$port" -le 65535 ] || die "Porta fora da faixa: $port"
done

phase_selected() { [ "$PHASE" = all ] || [ "$PHASE" = "$1" ]; }
collector_compose() {
  docker_prefix=$(docker_command)
  profile_option=""
  [ "$WITH_CONTAINERS" = true ] && profile_option="--profile containers"
  printf '%s\n' "$docker_prefix compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option"
}

phase_preflight() {
  log "Fase 00/50: preflight do collector"
  validate_linux_host 1 5
}

phase_runtime() {
  log "Fase 10/50: runtime do collector"
  [ "$INSTALL_RUNTIME" = false ] || install_runtime_packages
  validate_runtime
}

phase_configure() {
  log "Fase 20/50: identidade e endpoints"
  umask 077
  [ -f "$COLLECTOR_ROOT/.env" ] || cp "$COLLECTOR_ROOT/.env.example" "$COLLECTOR_ROOT/.env"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_HOST_NAME "$HOST_NAME"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_ENVIRONMENT "$ENVIRONMENT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_TEAM "$TEAM"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_LOCATION "$LOCATION"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_METRICS_ENDPOINT "$METRICS_ENDPOINT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_LOGS_ENDPOINT "$LOGS_ENDPOINT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_OTLP_ENDPOINT "$OTLP_ENDPOINT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECTOR_ADMIN_PORT "$ADMIN_PORT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECTOR_OTLP_GRPC_PORT "$OTLP_GRPC_PORT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECTOR_OTLP_HTTP_PORT "$OTLP_HTTP_PORT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECT_CONTAINERS "$WITH_CONTAINERS"
  chmod 600 "$COLLECTOR_ROOT/.env"
  log "Collector configurado sem imprimir credenciais."
}

phase_deploy() {
  log "Fase 30/50: deploy do collector"
  compose=$(collector_compose)
  # shellcheck disable=SC2086
  $compose config --quiet
  # shellcheck disable=SC2086
  $compose up -d --remove-orphans
}

phase_verify() {
  log "Fase 40/50: verificação do collector"
  attempts=0
  until curl -fsS "http://127.0.0.1:$ADMIN_PORT/-/ready" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 30 ] || die "Alloy não ficou ready no tempo esperado."
    sleep 2
  done
  compose=$(collector_compose)
  # shellcheck disable=SC2086
  $compose ps
  log "Alloy pronto. Valide no Prometheus: up{job=\"linux-node\",instance=\"$HOST_NAME\"}."
}

phase_service() {
  log "Fase 50/50: integração com init"
  command_exists systemctl || die "Unit automática requer systemd; use restart policies em outros init systems."
  docker_binary=$(command -v docker)
  profile_option=""
  [ "$WITH_CONTAINERS" = true ] && profile_option="--profile containers"
  unit_file=$(mktemp)
  cat > "$unit_file" <<EOF
[Unit]
Description=SentinelOps Linux collector
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=$COLLECTOR_ROOT
ExecStart=$docker_binary compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option up -d --remove-orphans
ExecStop=$docker_binary compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option down --remove-orphans

[Install]
WantedBy=multi-user.target
EOF
  run_as_root install -m 0644 "$unit_file" /etc/systemd/system/sentinelops-collector.service
  rm -f "$unit_file"
  run_as_root systemctl daemon-reload
  run_as_root systemctl enable sentinelops-collector.service
}

phase_selected preflight && phase_preflight
phase_selected runtime && phase_runtime
phase_selected configure && phase_configure
phase_selected deploy && phase_deploy
phase_selected verify && phase_verify
if [ "$PHASE" = service ] || { [ "$PHASE" = all ] && [ "$ENABLE_SERVICE" = true ]; }; then phase_service; fi

log "Bootstrap do collector concluído para as fases selecionadas."
