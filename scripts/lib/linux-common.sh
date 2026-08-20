#!/bin/sh

set -eu

log() { printf '%s\n' "[sentinelops] $*"; }
warn() { printf '%s\n' "[sentinelops][aviso] $*" >&2; }
die() { printf '%s\n' "[sentinelops][erro] $*" >&2; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

run_as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command_exists sudo; then
    sudo "$@"
  else
    die "A operação requer root ou sudo: $*"
  fi
}

detect_package_manager() {
  for manager in apt-get dnf yum zypper apk pacman; do
    if command_exists "$manager"; then
      printf '%s\n' "$manager"
      return 0
    fi
  done
  return 1
}

install_runtime_packages() {
  manager=$(detect_package_manager || true)
  [ -n "$manager" ] || die "Gerenciador não reconhecido. Instale Docker Engine, Compose v2, make, curl, jq, openssl e htpasswd manualmente."

  log "Instalando dependências com $manager"
  case "$manager" in
    apt-get)
      run_as_root apt-get update
      run_as_root apt-get install -y ca-certificates curl git make jq openssl apache2-utils docker.io
      run_as_root apt-get install -y docker-compose-v2 || run_as_root apt-get install -y docker-compose-plugin || run_as_root apt-get install -y docker-compose
      ;;
    dnf)
      run_as_root dnf install -y ca-certificates curl git make jq openssl httpd-tools docker docker-compose-plugin || \
        run_as_root dnf install -y ca-certificates curl git make jq openssl httpd-tools moby-engine docker-compose-plugin
      ;;
    yum)
      run_as_root yum install -y ca-certificates curl git make jq openssl httpd-tools docker docker-compose-plugin || \
        run_as_root yum install -y ca-certificates curl git make jq openssl httpd-tools docker docker-compose
      ;;
    zypper)
      run_as_root zypper --non-interactive install ca-certificates curl git make jq openssl apache2-utils docker docker-compose
      ;;
    apk)
      run_as_root apk add ca-certificates curl git make jq openssl apache2-utils docker docker-cli-compose
      ;;
    pacman)
      run_as_root pacman -Sy --noconfirm ca-certificates curl git make jq openssl apache docker docker-compose
      ;;
  esac

  if command_exists systemctl; then
    run_as_root systemctl enable --now docker
  elif command_exists rc-update && command_exists rc-service; then
    run_as_root rc-update add docker default || true
    run_as_root rc-service docker start
  else
    warn "Init system não reconhecido; inicie o daemon Docker manualmente."
  fi
}

docker_command() {
  if docker info >/dev/null 2>&1; then
    printf '%s\n' "docker"
  elif command_exists sudo && sudo docker info >/dev/null 2>&1; then
    printf '%s\n' "sudo docker"
  else
    die "Docker daemon indisponível ou sem permissão."
  fi
}

validate_runtime() {
  for command_name in make curl jq openssl htpasswd; do
    command_exists "$command_name" || die "Comando obrigatório ausente: $command_name"
  done
  command_exists docker || die "Docker Engine não encontrado."
  docker_prefix=$(docker_command)
  # shellcheck disable=SC2086
  $docker_prefix compose version >/dev/null 2>&1 || die "Docker Compose v2 não encontrado."
  log "$($docker_prefix version --format '{{.Server.Version}}' 2>/dev/null || $docker_prefix version | head -1)"
  log "$($docker_prefix compose version)"
}

validate_linux_host() {
  minimum_memory_gib=${1:-8}
  minimum_disk_gib=${2:-40}
  [ "$(uname -s)" = "Linux" ] || die "Este bootstrap é exclusivo para Linux."
  architecture=$(uname -m)
  case "$architecture" in
    x86_64|amd64|aarch64|arm64) ;;
    *) die "Arquitetura não suportada: $architecture" ;;
  esac

  memory_kib=$(awk '/MemTotal:/ {print $2}' /proc/meminfo)
  memory_gib=$((memory_kib / 1024 / 1024))
  [ "$memory_gib" -ge "$minimum_memory_gib" ] || die "São necessários pelo menos ${minimum_memory_gib} GiB de RAM; detectado: ${memory_gib} GiB."

  available_kib=$(df -Pk . | awk 'NR==2 {print $4}')
  available_gib=$((available_kib / 1024 / 1024))
  [ "$available_gib" -ge "$minimum_disk_gib" ] || die "São necessários pelo menos ${minimum_disk_gib} GiB livres; detectado: ${available_gib} GiB."

  log "Host Linux $architecture: ${memory_gib} GiB RAM e ${available_gib} GiB livres."
}

validate_name() {
  value=$1
  label=$2
  printf '%s' "$value" | grep -Eq '^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$' || die "$label inválido: $value"
}

validate_bind_address() {
  value=$1
  printf '%s' "$value" | grep -Eq '^[0-9a-fA-F:.]+$' || die "Endereço de bind inválido: $value"
}

set_env_value() {
  env_file=$1
  key=$2
  value=$3
  temporary_file=$(mktemp "${env_file}.XXXXXX")
  awk -F= -v wanted_key="$key" -v wanted_value="$value" '
    BEGIN { found=0 }
    $1 == wanted_key { print wanted_key "=" wanted_value; found=1; next }
    { print }
    END { if (!found) print wanted_key "=" wanted_value }
  ' "$env_file" > "$temporary_file"
  chmod 600 "$temporary_file"
  mv "$temporary_file" "$env_file"
}

compose_command() {
  root_directory=$1
  docker_prefix=$(docker_command)
  printf '%s\n' "$docker_prefix compose --env-file $root_directory/.env -f $root_directory/deploy/compose/docker-compose.yml"
}
