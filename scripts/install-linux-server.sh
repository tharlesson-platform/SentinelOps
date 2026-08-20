#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

PHASE=all
INSTALL_RUNTIME=false
WEB_BIND_ADDRESS=127.0.0.1
INGEST_BIND_ADDRESS=127.0.0.1
ALLOW_PUBLIC_INGEST=false
SEED_DATA=true
ENABLE_SERVICE=false

usage() {
  cat <<'EOF'
Uso: ./scripts/install-linux-server.sh [opções]

  --phase preflight|runtime|configure|deploy|seed|verify|service|all
  --install-runtime              instala pacotes usando apt/dnf/yum/zypper/apk/pacman
  --web-bind ADDRESS             bind da interface Web (padrão 127.0.0.1)
  --ingest-bind ADDRESS          bind OTLP/Prometheus/Loki (padrão 127.0.0.1)
  --allow-public-ingest          aceita ingest-bind 0.0.0.0 ou :: conscientemente
  --without-seed                 não cria os dados demonstrativos
  --enable-service               instala uma unit systemd ao final
  --help

O perfil é single-node para laboratório/piloto. Produção HA usa Helm e serviços
externos conforme docs/operations/production.md.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --phase) [ "$#" -ge 2 ] || die "--phase requer valor"; PHASE=$2; shift 2 ;;
    --install-runtime) INSTALL_RUNTIME=true; shift ;;
    --web-bind) [ "$#" -ge 2 ] || die "--web-bind requer valor"; WEB_BIND_ADDRESS=$2; shift 2 ;;
    --ingest-bind) [ "$#" -ge 2 ] || die "--ingest-bind requer valor"; INGEST_BIND_ADDRESS=$2; shift 2 ;;
    --allow-public-ingest) ALLOW_PUBLIC_INGEST=true; shift ;;
    --without-seed) SEED_DATA=false; shift ;;
    --enable-service) ENABLE_SERVICE=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

case "$PHASE" in
  preflight|runtime|configure|deploy|seed|verify|service|all) ;;
  *) die "Fase inválida: $PHASE" ;;
esac
validate_bind_address "$WEB_BIND_ADDRESS"
validate_bind_address "$INGEST_BIND_ADDRESS"
if { [ "$INGEST_BIND_ADDRESS" = "0.0.0.0" ] || [ "$INGEST_BIND_ADDRESS" = "::" ]; } && [ "$ALLOW_PUBLIC_INGEST" != true ]; then
  die "Bind público de ingestão recusado. Use IP privado ou --allow-public-ingest após configurar firewall/TLS."
fi

phase_selected() { [ "$PHASE" = all ] || [ "$PHASE" = "$1" ]; }

phase_preflight() {
  log "Fase 00/60: preflight"
  validate_linux_host
  if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    log "Distribuição detectada: ${PRETTY_NAME:-desconhecida}"
  fi
}

phase_runtime() {
  log "Fase 10/60: runtime"
  if [ "$INSTALL_RUNTIME" = true ]; then
    install_runtime_packages
  fi
  validate_runtime
}

phase_configure() {
  log "Fase 20/60: configuração"
  chmod +x "$ROOT"/scripts/*.sh "$ROOT"/scripts/lib/*.sh
  "$ROOT/scripts/bootstrap.sh"
  "$ROOT/scripts/generate-dashboards.sh"
  set_env_value "$ROOT/.env" SENTINEL_WEB_BIND_ADDRESS "$WEB_BIND_ADDRESS"
  set_env_value "$ROOT/.env" SENTINEL_OTLP_BIND_ADDRESS "$INGEST_BIND_ADDRESS"
  set_env_value "$ROOT/.env" SENTINEL_PROMETHEUS_BIND_ADDRESS "$INGEST_BIND_ADDRESS"
  set_env_value "$ROOT/.env" SENTINEL_LOKI_BIND_ADDRESS "$INGEST_BIND_ADDRESS"
  chmod 600 "$ROOT/.env"
  log "Configuração gravada em .env sem imprimir secrets."
}

phase_deploy() {
  log "Fase 30/60: deploy"
  compose=$(compose_command "$ROOT")
  # shellcheck disable=SC2086
  $compose config --quiet
  # shellcheck disable=SC2086
  $compose up -d --build --remove-orphans
}

phase_seed() {
  log "Fase 40/60: configuração inicial"
  if [ "$SEED_DATA" = true ]; then
    "$ROOT/scripts/seed.sh"
  else
    log "Seed omitido por opção."
  fi
}

phase_verify() {
  log "Fase 50/60: verificação"
  "$ROOT/scripts/doctor.sh"
  compose=$(compose_command "$ROOT")
  # shellcheck disable=SC2086
  $compose ps
  log "Interface: http://$WEB_BIND_ADDRESS:${SENTINEL_WEB_PORT:-3000}"
  log "Use 'make credentials' localmente e execute './scripts/prove-gates.sh' para a prova funcional."
}

phase_service() {
  log "Fase 60/60: integração com init"
  command_exists systemctl || die "A instalação automática da unit requer systemd. Em OpenRC, use docker compose e restart policies."
  docker_binary=$(command -v docker)
  unit_file=$(mktemp)
  cat > "$unit_file" <<EOF
[Unit]
Description=SentinelOps single-node stack
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=$ROOT
ExecStart=$docker_binary compose --env-file $ROOT/.env -f $ROOT/deploy/compose/docker-compose.yml up -d --remove-orphans
ExecStop=$docker_binary compose --env-file $ROOT/.env -f $ROOT/deploy/compose/docker-compose.yml down --remove-orphans
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
EOF
  run_as_root install -m 0644 "$unit_file" /etc/systemd/system/sentinelops.service
  rm -f "$unit_file"
  run_as_root systemctl daemon-reload
  run_as_root systemctl enable sentinelops.service
  log "Unit sentinelops.service instalada e habilitada."
}

phase_selected preflight && phase_preflight
phase_selected runtime && phase_runtime
phase_selected configure && phase_configure
phase_selected deploy && phase_deploy
phase_selected seed && phase_seed
phase_selected verify && phase_verify
if [ "$PHASE" = service ] || { [ "$PHASE" = all ] && [ "$ENABLE_SERVICE" = true ]; }; then
  phase_service
fi

log "Bootstrap do servidor concluído para as fases selecionadas."
