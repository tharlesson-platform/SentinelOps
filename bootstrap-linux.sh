#!/bin/sh
set -eu

SCRIPT_ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
SOURCE_URL=""
SOURCE_SHA256=""
INSTALL_DIR=/opt/sentinelops
WEB_BIND=127.0.0.1
INGEST_BIND=127.0.0.1
INGEST_SERVER_NAME=ingest.local
ALLOW_PUBLIC_INGEST=false
ENABLE_SERVICE=true
SEED=true
FUNCTIONAL_PROOF=true

usage() {
  cat <<'EOF'
Bootstrap completo SentinelOps para servidor Linux

Uso local: sudo ./bootstrap-linux.sh
Uso remoto: sudo ./bootstrap-linux.sh --source-url https://host/sentinelops.tar.gz --sha256 SHA256

  --source-url URL              release .tar.gz via HTTPS
  --sha256 HASH                SHA-256 obrigatório para release remoto
  --install-dir DIR            destino (padrão /opt/sentinelops)
  --web-bind IP                bind Web (padrão 127.0.0.1)
  --ingest-bind IP             IP privado do gateway mTLS
  --ingest-server-name DNS     nome presente no certificado TLS
  --allow-public-ingest        confirma bind público; configure firewall
  --without-seed               omite dados demonstrativos
  --without-service            não instala unit systemd
  --without-functional-proof   omite a prova fim a fim
EOF
}

die() { printf '%s\n' "[sentinelops][erro] $*" >&2; exit 1; }
log() { printf '%s\n' "[sentinelops] $*"; }
command_exists() { command -v "$1" >/dev/null 2>&1; }
as_root() {
  if [ "$(id -u)" -eq 0 ]; then "$@"; elif command_exists sudo; then sudo "$@"; else die "root ou sudo é obrigatório"; fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source-url) [ "$#" -ge 2 ] || die "--source-url requer valor"; SOURCE_URL=$2; shift 2 ;;
    --sha256) [ "$#" -ge 2 ] || die "--sha256 requer valor"; SOURCE_SHA256=$2; shift 2 ;;
    --install-dir) [ "$#" -ge 2 ] || die "--install-dir requer valor"; INSTALL_DIR=$2; shift 2 ;;
    --web-bind) [ "$#" -ge 2 ] || die "--web-bind requer valor"; WEB_BIND=$2; shift 2 ;;
    --ingest-bind) [ "$#" -ge 2 ] || die "--ingest-bind requer valor"; INGEST_BIND=$2; shift 2 ;;
    --ingest-server-name) [ "$#" -ge 2 ] || die "--ingest-server-name requer valor"; INGEST_SERVER_NAME=$2; shift 2 ;;
    --allow-public-ingest) ALLOW_PUBLIC_INGEST=true; shift ;;
    --without-seed) SEED=false; shift ;;
    --without-service) ENABLE_SERVICE=false; shift ;;
    --without-functional-proof) FUNCTIONAL_PROOF=false; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

[ "$(uname -s)" = Linux ] || die "Este bootstrap é exclusivo para Linux."
case "$INSTALL_DIR" in /*) ;; *) die "--install-dir deve ser absoluto" ;; esac
printf '%s' "$SOURCE_SHA256" | grep -Eq '^$|^[a-fA-F0-9]{64}$' || die "SHA-256 inválido"

install_fetch_tools() {
  command_exists curl && command_exists tar && { command_exists sha256sum || command_exists openssl; } && return 0
  for manager in apt-get dnf yum zypper apk pacman microdnf; do
    command_exists "$manager" || continue
    log "Fase 00/40: instalando ferramentas de aquisição com $manager"
    case "$manager" in
      apt-get) as_root apt-get update; as_root apt-get install -y ca-certificates curl tar coreutils openssl ;;
      dnf) as_root dnf install -y ca-certificates curl tar coreutils openssl ;;
      yum) as_root yum install -y ca-certificates curl tar coreutils openssl ;;
      zypper) as_root zypper --non-interactive install ca-certificates curl tar coreutils openssl ;;
      apk) as_root apk add ca-certificates curl tar coreutils openssl ;;
      pacman) as_root pacman -Sy --noconfirm ca-certificates curl tar coreutils openssl ;;
      microdnf) as_root microdnf install -y ca-certificates curl tar gzip openssl ;;
    esac
    return 0
  done
  die "Não foi possível instalar curl, tar e SHA-256."
}

sha256_file() {
  if command_exists sha256sum; then sha256sum "$1" | awk '{print $1}'; else openssl dgst -sha256 "$1" | awk '{print $NF}'; fi
}

acquire_release() {
  [ -n "$SOURCE_SHA256" ] || die "--sha256 é obrigatório com --source-url"
  printf '%s' "$SOURCE_URL" | grep -Eq '^https://[^[:space:]]+$' || die "--source-url deve usar HTTPS"
  install_fetch_tools
  if [ -d "$INSTALL_DIR" ] && [ -n "$(find "$INSTALL_DIR" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]; then
    die "$INSTALL_DIR não está vazio; recusa sobrescrever uma instalação."
  fi
  work_dir=$(mktemp -d)
  trap 'rm -rf "$work_dir"' EXIT HUP INT TERM
  archive="$work_dir/sentinelops.tar.gz"
  log "Fase 10/40: baixando e validando release"
  curl -fL --proto '=https' --tlsv1.2 --retry 3 --max-time 900 -o "$archive" "$SOURCE_URL"
  [ "$(sha256_file "$archive")" = "$SOURCE_SHA256" ] || die "Checksum do release não confere."
  tar -tzf "$archive" | awk '/^\// || /(^|\/)\.\.(\/|$)/ { bad=1 } END { exit bad }' || die "Archive contém caminho inseguro."
  extract_dir="$work_dir/extracted"
  mkdir -p "$extract_dir"
  tar -xzf "$archive" -C "$extract_dir"
  source_marker=$(find "$extract_dir" -type f -path '*/scripts/install-linux-server.sh' -print -quit)
  [ -n "$source_marker" ] || die "Release não contém scripts/install-linux-server.sh"
  source_root=$(dirname "$(dirname "$source_marker")")
  as_root mkdir -p "$INSTALL_DIR"
  as_root cp -R "$source_root"/. "$INSTALL_DIR"/
  SCRIPT_ROOT=$INSTALL_DIR
}

if [ -n "$SOURCE_URL" ]; then
  acquire_release
else
  [ -x "$SCRIPT_ROOT/scripts/install-linux-server.sh" ] || die "Execute dentro do release ou informe --source-url."
fi

log "Fase 20/40: instalando runtime e subindo a plataforma"
set -- --phase all --install-runtime --web-bind "$WEB_BIND" --ingest-bind "$INGEST_BIND" --ingest-server-name "$INGEST_SERVER_NAME"
[ "$ALLOW_PUBLIC_INGEST" = false ] || set -- "$@" --allow-public-ingest
[ "$SEED" = true ] || set -- "$@" --without-seed
[ "$ENABLE_SERVICE" = false ] || set -- "$@" --enable-service
"$SCRIPT_ROOT/scripts/install-linux-server.sh" "$@"

if [ "$FUNCTIONAL_PROOF" = true ]; then
  log "Fase 30/40: provando coleta, ingestão e consulta"
  make -C "$SCRIPT_ROOT" prove-local
  "$SCRIPT_ROOT/scripts/prove-dashboards.sh"
fi

log "Fase 40/40: registrando evidência"
mkdir -p "$SCRIPT_ROOT/artifacts/installation"
evidence="$SCRIPT_ROOT/artifacts/installation/linux-$(date -u +%Y%m%dT%H%M%SZ).json"
docker_version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || true)
compose_version=$(docker compose version --short 2>/dev/null || true)
jq -n --arg installedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg host "$(hostname)" \
  --arg architecture "$(uname -m)" --arg kernel "$(uname -r)" --arg docker "$docker_version" \
  --arg compose "$compose_version" --arg web "http://$WEB_BIND:3000" \
  --arg ingest "https://$INGEST_SERVER_NAME:8443" --argjson proof "$FUNCTIONAL_PROOF" \
  '{installedAt:$installedAt,host:$host,architecture:$architecture,kernel:$kernel,docker:$docker,compose:$compose,web:$web,ingest:$ingest,functionalProof:$proof}' > "$evidence"
chmod 600 "$evidence"
log "Plataforma funcional. Web: http://$WEB_BIND:3000 | Grafana: http://127.0.0.1:3001"
log "Evidência: $evidence"
