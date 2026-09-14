#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

COLLECTOR_ROOT="$ROOT/deploy/agents/linux"
PHASE=all
INSTALL_RUNTIME=false
HOST_NAME=$(hostname -s 2>/dev/null || hostname 2>/dev/null || uname -n 2>/dev/null || printf localhost)
HOST_NAME_SET=false
ENVIRONMENT=development
TEAM=platform
LOCATION=on-premises
METRICS_ENDPOINT=""
LOGS_ENDPOINT=""
OTLP_ENDPOINT=""
WITH_CONTAINERS=false
WITH_CONTAINERS_SET=false
WITH_CADVISOR=false
WITH_CADVISOR_SET=false
WITH_BEYLA=false
WITH_BEYLA_SET=false
ENABLE_SERVICE=false
ADMIN_PORT=""
OTLP_GRPC_PORT=""
OTLP_HTTP_PORT=""
TLS_CA_FILE=""
TLS_CERT_FILE=""
TLS_KEY_FILE=""
TLS_SERVER_NAME=""
DOCKER_LOG_ROOT=""

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
  --with-containers            habilita logs JSON Docker por mount read-only
  --with-cadvisor              habilita métricas cAdvisor privilegiadas; requer aprovação por host
  --with-beyla                 habilita APM eBPF para processos em contêiner; requer kernel compatível e aprovação por host
  --docker-log-root DIR        diretório de logs JSON do Docker; padrão: DockerRootDir/containers
  --admin-port PORT            porta local da UI/readiness Alloy
  --otlp-grpc-port PORT        receiver local para aplicações
  --otlp-http-port PORT        receiver local para aplicações
  --tls-ca-file FILE           CA que valida o gateway de ingestão
  --tls-cert-file FILE         certificado mTLS exclusivo do collector
  --tls-key-file FILE          chave privada mTLS do collector
  --tls-server-name NAME       nome DNS presente no certificado do gateway
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
    --with-cadvisor) WITH_CADVISOR=true; WITH_CADVISOR_SET=true; shift ;;
    --with-beyla) WITH_BEYLA=true; WITH_BEYLA_SET=true; shift ;;
    --docker-log-root) DOCKER_LOG_ROOT=$2; shift 2 ;;
    --admin-port) ADMIN_PORT=$2; shift 2 ;;
    --otlp-grpc-port) OTLP_GRPC_PORT=$2; shift 2 ;;
    --otlp-http-port) OTLP_HTTP_PORT=$2; shift 2 ;;
    --tls-ca-file) TLS_CA_FILE=$2; shift 2 ;;
    --tls-cert-file) TLS_CERT_FILE=$2; shift 2 ;;
    --tls-key-file) TLS_KEY_FILE=$2; shift 2 ;;
    --tls-server-name) TLS_SERVER_NAME=$2; shift 2 ;;
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
  if [ "$WITH_CADVISOR_SET" = false ] && [ "$(collector_env_value SENTINEL_COLLECT_CADVISOR)" = true ]; then
    WITH_CADVISOR=true
  fi
  if [ "$WITH_BEYLA_SET" = false ] && [ "$(collector_env_value SENTINEL_COLLECT_BEYLA)" = true ]; then
    WITH_BEYLA=true
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
    printf '%s' "$endpoint" | grep -Eq '^https://[^[:space:]]+$' || die "Endpoint deve usar HTTPS: $endpoint"
  done
  [ -n "$TLS_SERVER_NAME" ] || die "--tls-server-name é obrigatório"
  validate_name "$TLS_SERVER_NAME" tls-server-name
  for endpoint in "$METRICS_ENDPOINT" "$LOGS_ENDPOINT" "$OTLP_ENDPOINT"; do
    endpoint_authority=${endpoint#https://}
    endpoint_host=${endpoint_authority%%/*}
    endpoint_host=${endpoint_host%%:*}
    [ "$endpoint_host" = "$TLS_SERVER_NAME" ] || \
      die "O host de $endpoint deve ser igual a --tls-server-name ($TLS_SERVER_NAME); use DNS interno para evitar rejeição HTTP por SNI"
  done
  for certificate_file in "$TLS_CA_FILE" "$TLS_CERT_FILE" "$TLS_KEY_FILE"; do
    [ -s "$certificate_file" ] || die "arquivo TLS ausente ou vazio: $certificate_file"
  done
fi
for port in "$ADMIN_PORT" "$OTLP_GRPC_PORT" "$OTLP_HTTP_PORT"; do
  printf '%s' "$port" | grep -Eq '^[0-9]{1,5}$' || die "Porta inválida: $port"
  if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
    die "Porta fora da faixa: $port"
  fi
done

phase_selected() { [ "$PHASE" = all ] || [ "$PHASE" = "$1" ]; }
collector_compose() {
  docker_prefix=$(docker_command)
  profile_option=""
  [ "$WITH_CONTAINERS" = true ] && profile_option="$profile_option --profile containers"
  [ "$WITH_CADVISOR" = true ] && profile_option="$profile_option --profile cadvisor"
  [ "$WITH_BEYLA" = true ] && profile_option="$profile_option --profile beyla"
  printf '%s\n' "$docker_prefix compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option"
}

resolve_docker_log_root() {
  if [ -n "$DOCKER_LOG_ROOT" ]; then
    candidate=$DOCKER_LOG_ROOT
  else
    docker_prefix=$(docker_command)
    docker_root=$($docker_prefix info --format '{{.DockerRootDir}}' 2>/dev/null || true)
    [ -n "$docker_root" ] || die "não foi possível obter DockerRootDir; use --docker-log-root somente após validar o host Docker"
    candidate=$docker_root/containers
  fi
  case "$candidate" in
    /*) ;;
    *) die "--docker-log-root deve ser caminho absoluto" ;;
  esac
  [ -d "$candidate" ] || die "diretório de logs Docker não existe: $candidate"
  printf '%s\n' "$candidate"
}

phase_preflight() {
  log "Fase 00/50: preflight do collector"
  validate_linux_host 1 5
  if [ "$WITH_BEYLA" = true ]; then
    [ -r /sys/kernel/btf/vmlinux ] || die "Beyla requer BTF em /sys/kernel/btf/vmlinux; não habilite eBPF neste host."
    [ -d /sys/fs/cgroup ] || die "Beyla requer cgroupfs em /sys/fs/cgroup."
    [ -d /sys/kernel/tracing ] || die "Beyla requer tracefs em /sys/kernel/tracing; monte tracefs antes de habilitar eBPF."
    lockdown=none
    if [ -r /sys/kernel/security/lockdown ]; then
      lockdown=$(cat /sys/kernel/security/lockdown 2>/dev/null || printf unknown)
    fi
    case "$lockdown" in
      *"[none]"*) log "Beyla: kernel sem lockdown; propagação distribuída pode ser habilitada." ;;
      *) warn "Beyla: kernel lockdown ausente/ativo; métricas e traces locais continuam disponíveis, mas propagação distribuída pode ficar indisponível." ;;
    esac
  fi
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
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_TLS_SERVER_NAME "$TLS_SERVER_NAME"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECTOR_ADMIN_PORT "$ADMIN_PORT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECTOR_OTLP_GRPC_PORT "$OTLP_GRPC_PORT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECTOR_OTLP_HTTP_PORT "$OTLP_HTTP_PORT"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECT_CONTAINERS "$WITH_CONTAINERS"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECT_CADVISOR "$WITH_CADVISOR"
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_COLLECT_BEYLA "$WITH_BEYLA"
  log_reader_gid=$(getent group adm 2>/dev/null | awk -F: 'NR == 1 && $3 ~ /^[0-9]+$/ { print $3 }')
  [ -n "$log_reader_gid" ] || log_reader_gid=4
  set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_LOG_READ_GID "$log_reader_gid"
  if [ "$WITH_CONTAINERS" = true ]; then
    docker_log_root=$(resolve_docker_log_root)
    set_env_value "$COLLECTOR_ROOT/.env" SENTINEL_DOCKER_LOG_ROOT "$docker_log_root"
  fi
  mkdir -p "$COLLECTOR_ROOT/certs"
  copy_tls_file "$TLS_CA_FILE" "$COLLECTOR_ROOT/certs/ca.crt" 0644
  copy_tls_file "$TLS_CERT_FILE" "$COLLECTOR_ROOT/certs/client.crt" 0644
  copy_tls_file "$TLS_KEY_FILE" "$COLLECTOR_ROOT/certs/client.key" 0600
  openssl x509 -checkend 604800 -noout -in "$COLLECTOR_ROOT/certs/client.crt" >/dev/null 2>&1 || \
    die "Certificado do collector expira em menos de 7 dias; gere um novo bundle."
  openssl verify -CAfile "$COLLECTOR_ROOT/certs/ca.crt" "$COLLECTOR_ROOT/certs/client.crt" >/dev/null || \
    die "Certificado do collector não valida contra a CA fornecida."
  chmod 600 "$COLLECTOR_ROOT/.env"
  log "Collector configurado sem imprimir credenciais."
}

copy_tls_file() {
  source_file=$1
  destination_file=$2
  file_mode=$3
  source_absolute=$(CDPATH='' cd -- "$(dirname -- "$source_file")" && pwd)/$(basename -- "$source_file")
  destination_absolute=$(CDPATH='' cd -- "$(dirname -- "$destination_file")" && pwd)/$(basename -- "$destination_file")
  if [ "$source_absolute" != "$destination_absolute" ]; then
    install -m "$file_mode" "$source_file" "$destination_file"
  else
    chmod "$file_mode" "$destination_file"
  fi
}

phase_deploy() {
  log "Fase 30/50: deploy do collector"
  docker_prefix=$(docker_command)
  if ! $docker_prefix image inspect sentinelops-alloy:1.18.1-patched.2 >/dev/null 2>&1; then
    # A imagem corrigida é compilada a partir do fonte e seu cache transitório
    # pode ser grande. Falhar antes do build protege hosts de aplicação de
    # esgotar o filesystem Docker e interromper a escrita de logs existentes.
    minimum_build_disk_gib=${SENTINEL_ALLOY_BUILD_MIN_FREE_GIB:-25}
    available_build_disk_gib=$(df -Pk "$ROOT" | awk 'NR==2 {print int($4 / 1024 / 1024)}')
    case "$minimum_build_disk_gib" in
      ''|*[!0-9]*) die "SENTINEL_ALLOY_BUILD_MIN_FREE_GIB deve ser inteiro positivo" ;;
    esac
    [ "$minimum_build_disk_gib" -ge 1 ] || die "SENTINEL_ALLOY_BUILD_MIN_FREE_GIB deve ser maior que zero"
    [ "$available_build_disk_gib" -ge "$minimum_build_disk_gib" ] || \
      die "São necessários pelo menos ${minimum_build_disk_gib} GiB livres para construir Alloy corrigido; detectado: ${available_build_disk_gib} GiB. Publique uma imagem pré-construída ou amplie o disco antes de continuar."
    [ -f "$ROOT/deploy/alloy/Dockerfile.patched" ] || die "Imagem Alloy corrigida ausente e Dockerfile não incluído no bundle."
    log "Construindo Alloy corrigido e fixado; esta etapa pode levar alguns minutos."
    # shellcheck disable=SC2086
    $docker_prefix build --file "$ROOT/deploy/alloy/Dockerfile.patched" --tag sentinelops-alloy:1.18.1-patched.2 "$ROOT"
  fi
  compose=$(collector_compose)
  # shellcheck disable=SC2086
  $compose config --quiet
  # shellcheck disable=SC2086
  # Não remova contêineres fora do projeto do collector. Hosts de aplicação
  # podem ter stacks independentes no mesmo Docker daemon.
  $compose up -d
}

phase_verify() {
  log "Fase 40/50: verificação do collector"
  attempts=0
  until curl -fsS "http://127.0.0.1:$ADMIN_PORT/-/ready" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 30 ] || die "Alloy não ficou ready no tempo esperado."
    sleep 2
  done
  unhealthy=$(curl -fsS "http://127.0.0.1:$ADMIN_PORT/api/v0/web/components" | jq '[.[] | select(.health.state != "healthy")] | length')
  [ "$unhealthy" -eq 0 ] || die "Alloy respondeu ready, mas há $unhealthy componente(s) não saudáveis. Consulte /api/v0/web/components."
  compose=$(collector_compose)
  # shellcheck disable=SC2086
  $compose ps
  log "Alloy pronto. Valide no Prometheus: up{job=\"linux-node\",instance=\"$HOST_NAME\"}."
}

phase_service() {
  log "Fase 50/50: integração com init"
  docker_binary=$(command -v docker)
  profile_option=""
  [ "$WITH_CONTAINERS" = true ] && profile_option="$profile_option --profile containers"
  [ "$WITH_CADVISOR" = true ] && profile_option="$profile_option --profile cadvisor"
  [ "$WITH_BEYLA" = true ] && profile_option="$profile_option --profile beyla"
  if command_exists rc-update; then
    openrc_file=$(mktemp)
    cat > "$openrc_file" <<EOF
#!/sbin/openrc-run
description="SentinelOps Linux collector"
depend() { need docker; after net; }
start() { $docker_binary compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option up -d; }
stop() { $docker_binary compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option down; }
EOF
    run_as_root install -m 0755 "$openrc_file" /etc/init.d/sentinelops-collector
    rm -f "$openrc_file"
    run_as_root rc-update add sentinelops-collector default
    log "Serviço OpenRC habilitado."
    return
  fi
  if ! command_exists systemctl; then
    warn "Init não reconhecido; restart policies do Compose permanecerão ativas."
    return
  fi
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
ExecStart=$docker_binary compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option up -d
ExecStop=$docker_binary compose --env-file $COLLECTOR_ROOT/.env -f $COLLECTOR_ROOT/docker-compose.yml $profile_option down

[Install]
WantedBy=multi-user.target
EOF
  run_as_root install -m 0644 "$unit_file" /etc/systemd/system/sentinelops-collector.service
  rm -f "$unit_file"
  run_as_root systemctl daemon-reload
  run_as_root systemctl enable --now sentinelops-collector.service
}

phase_selected preflight && phase_preflight
phase_selected runtime && phase_runtime
phase_selected configure && phase_configure
phase_selected deploy && phase_deploy
phase_selected verify && phase_verify
if [ "$PHASE" = service ] || { [ "$PHASE" = all ] && [ "$ENABLE_SERVICE" = true ]; }; then phase_service; fi

log "Bootstrap do collector concluído para as fases selecionadas."
