#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

MODE=${1:-}
[ "$#" -eq 0 ] || shift
PASSPHRASE_FILE=""
MANIFEST=""
CONFIRM=""
SIMULATE_FAILURE=false
PROJECT_NAME=sentinelops
SERVICES="api worker agent web demo-api"

usage() {
  cat <<'EOF'
Uso:
  upgrade-local.sh upgrade --passphrase-file FILE --confirm UPGRADE [--simulate-failure]
  upgrade-local.sh rollback --manifest FILE --confirm ROLLBACK

O upgrade preserva volumes, cria backup autenticado, registra os IDs imutáveis
das imagens atuais, reconstrói os serviços próprios e executa doctor. Qualquer
falha após o snapshot aciona rollback automático. --simulate-failure exercita
o caminho de rollback sem introduzir falha real na aplicação.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --passphrase-file) PASSPHRASE_FILE=$2; shift 2 ;;
    --manifest) MANIFEST=$2; shift 2 ;;
    --confirm) CONFIRM=$2; shift 2 ;;
    --project-name) PROJECT_NAME=$2; shift 2 ;;
    --simulate-failure) SIMULATE_FAILURE=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

case "$MODE" in upgrade|rollback) ;; *) usage >&2; exit 2 ;; esac
validate_name "$PROJECT_NAME" project-name
compose=$(compose_command "$ROOT")
mkdir -p "$ROOT/artifacts/upgrades"

rollback_images() {
  rollback_manifest=$1
  [ -s "$rollback_manifest" ] || die "manifest de rollback inválido"
  while IFS='|' read -r service image_ref image_id retained_ref; do
    [ -n "$service" ] && [ -n "$image_ref" ] && [ -n "$image_id" ] && [ -n "$retained_ref" ] || die "linha inválida no manifest"
    case " $SERVICES " in *" $service "*) ;; *) die "serviço inesperado no manifest: $service" ;; esac
    docker image inspect "$retained_ref" >/dev/null 2>&1 || die "imagem de rollback indisponível: $retained_ref"
    retained_id=$(docker image inspect "$retained_ref" --format '{{.Id}}')
    [ "$retained_id" = "$image_id" ] || die "imagem retida diverge do ID registrado para $service"
    docker image tag "$retained_ref" "$image_ref"
  done < "$rollback_manifest"
  # shellcheck disable=SC2086
  $compose -p "$PROJECT_NAME" up -d --no-build --force-recreate $SERVICES
  "$ROOT/scripts/doctor.sh"
  log "Rollback comprovado com imagens registradas em $rollback_manifest"
}

if [ "$MODE" = rollback ]; then
  [ "$CONFIRM" = ROLLBACK ] || die "rollback recusado: use --confirm ROLLBACK"
  rollback_images "$MANIFEST"
  exit 0
fi

[ "$CONFIRM" = UPGRADE ] || die "upgrade recusado: use --confirm UPGRADE"
[ -s "$PASSPHRASE_FILE" ] || die "--passphrase-file válido é obrigatório"
manifest="$ROOT/artifacts/upgrades/rollback-$(date -u +%Y%m%dT%H%M%SZ).manifest"
: > "$manifest"
chmod 600 "$manifest"
snapshot_id=$(date -u +%Y%m%dT%H%M%SZ)
for service in $SERVICES; do
  # shellcheck disable=SC2086
  container_id=$($compose -p "$PROJECT_NAME" ps -q "$service")
  [ -n "$container_id" ] || die "serviço $service não está em execução"
  image_ref=$(docker inspect "$container_id" --format '{{.Config.Image}}')
  image_id=$(docker inspect "$container_id" --format '{{.Image}}')
  retained_ref="sentinelops-rollback-$service:$snapshot_id"
  docker image tag "$image_id" "$retained_ref"
  printf '%s|%s|%s|%s\n' "$service" "$image_ref" "$image_id" "$retained_ref" >> "$manifest"
done

"$ROOT/scripts/backup-local.sh" --passphrase-file "$PASSPHRASE_FILE" --project-name "$PROJECT_NAME"
log "Reconstruindo e promovendo serviços próprios"
set +e
# shellcheck disable=SC2086
$compose -p "$PROJECT_NAME" build $SERVICES && $compose -p "$PROJECT_NAME" up -d --no-build --force-recreate $SERVICES
deploy_status=$?
set -e
if [ "$deploy_status" -ne 0 ]; then
  log "Deploy falhou; iniciando rollback automático"
  rollback_images "$manifest"
  exit 1
fi

if [ "$SIMULATE_FAILURE" = true ]; then
  log "Falha pós-deploy simulada; exercitando rollback automático"
  rollback_images "$manifest"
  log "Ensaio de upgrade/rollback concluído"
  exit 0
fi

if ! "$ROOT/scripts/doctor.sh"; then
  log "Health gate falhou; iniciando rollback automático"
  rollback_images "$manifest"
  exit 1
fi
log "Upgrade aprovado. Manifest de rollback preservado: $manifest"
