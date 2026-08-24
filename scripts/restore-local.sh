#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

ARCHIVE=""
PASSPHRASE_FILE=""
TARGET_PROJECT=""
CONFIRM=""

usage() {
  cat <<'EOF'
Uso: restore-local.sh --archive FILE --passphrase-file FILE \
  --target-project NAME --confirm RESTORE

Restaura somente em projeto Compose novo cujo nome termine em -restore. O alvo
sentinelops ativo é recusado. As portas publicadas são removidas no override,
permitindo o ensaio lado a lado. O projeto restaurado permanece ligado para QA.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --archive) ARCHIVE=$2; shift 2 ;;
    --passphrase-file) PASSPHRASE_FILE=$2; shift 2 ;;
    --target-project) TARGET_PROJECT=$2; shift 2 ;;
    --confirm) CONFIRM=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

[ "$CONFIRM" = RESTORE ] || die "Restauração recusada: use --confirm RESTORE"
[ -s "$ARCHIVE" ] || die "--archive inválido"
[ -s "$PASSPHRASE_FILE" ] || die "--passphrase-file inválido"
ARCHIVE=$(CDPATH='' cd -- "$(dirname -- "$ARCHIVE")" && printf '%s/%s\n' "$PWD" "$(basename -- "$ARCHIVE")")
PASSPHRASE_FILE=$(CDPATH='' cd -- "$(dirname -- "$PASSPHRASE_FILE")" && printf '%s/%s\n' "$PWD" "$(basename -- "$PASSPHRASE_FILE")")
permissions=$(stat -c '%a' "$PASSPHRASE_FILE" 2>/dev/null || stat -f '%Lp' "$PASSPHRASE_FILE")
[ "$permissions" = 600 ] || die "passphrase-file deve ter modo 0600"
validate_name "$TARGET_PROJECT" target-project
case "$TARGET_PROJECT" in *-restore) ;; *) die "target-project deve terminar em -restore" ;; esac
[ "$TARGET_PROJECT" != sentinelops ] || die "projeto ativo sentinelops é proibido como alvo"

workdir=$(mktemp -d /tmp/sentinelops-restore.XXXXXX)
trap 'rm -rf "$workdir"' EXIT HUP INT TERM

docker_prefix=$(docker_command)
crypto_image=sentinelops-backupcrypt:1.3.1
log "Construindo helper e autenticando backup antes da extração"
# shellcheck disable=SC2086
$docker_prefix build -q -f "$ROOT/Dockerfile.app" --build-arg APP=backupcrypt -t "$crypto_image" "$ROOT" >/dev/null
# shellcheck disable=SC2086
$docker_prefix run --rm --network none --user "$(id -u):$(id -g)" \
  -v "$(dirname "$ARCHIVE"):/input:ro" -v "$workdir:/output" -v "$PASSPHRASE_FILE:/run/secrets/passphrase:ro" \
  "$crypto_image" decrypt --input "/input/$(basename "$ARCHIVE")" --output /output/package.tar.gz --passphrase-file /run/secrets/passphrase
if tar -tzf "$workdir/package.tar.gz" | awk 'BEGIN{bad=0} /^\// || /(^|\/)\.\.($|\/)/ {bad=1} END{exit bad}'; then
  tar -C "$workdir" -xzf "$workdir/package.tar.gz"
else
  die "backup contém caminho inseguro e foi recusado"
fi

if command_exists sha256sum; then
  (cd "$workdir" && sha256sum -c SHA256SUMS)
else
  (cd "$workdir" && shasum -a 256 -c SHA256SUMS)
fi

override="$workdir/restore-override.yml"
cat > "$override" <<'EOF'
services:
  minio:
    ports: !reset []
EOF

base="$ROOT/deploy/compose/docker-compose.yml"
env_file="$ROOT/.env"
log "Subindo PostgreSQL e MinIO isolados no projeto $TARGET_PROJECT"
# shellcheck disable=SC2086
$docker_prefix compose -p "$TARGET_PROJECT" --env-file "$env_file" -f "$base" -f "$override" up -d postgres minio
# shellcheck disable=SC2086,SC2016
$docker_prefix compose -p "$TARGET_PROJECT" --env-file "$env_file" -f "$base" -f "$override" exec -T postgres sh -ec '
  until pg_isready -U sentinel -d sentinel >/dev/null; do sleep 1; done
  for database in temporal temporal_visibility; do createdb -U sentinel "$database" 2>/dev/null || true; done
'

for database in sentinel temporal temporal_visibility; do
  log "Restaurando $database"
  # shellcheck disable=SC2086
  $docker_prefix compose -p "$TARGET_PROJECT" --env-file "$env_file" -f "$base" -f "$override" exec -T postgres \
    pg_restore -U sentinel --clean --if-exists --no-owner --no-acl --exit-on-error -d "$database" < "$workdir/databases/$database.dump"
done

log "Restaurando bucket sentinel-artifacts"
# shellcheck disable=SC2086,SC2016
$docker_prefix compose -p "$TARGET_PROJECT" --env-file "$env_file" -f "$base" -f "$override" run --rm -T \
  -v "$workdir/minio:/restore:ro" --entrypoint /bin/sh minio-init -ec \
  'mc alias set target http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null && mc mb --ignore-existing target/sentinel-artifacts >/dev/null && { [ ! -d /restore/sentinel-artifacts ] || mc mirror --overwrite /restore/sentinel-artifacts target/sentinel-artifacts; }'

# shellcheck disable=SC2086
counts=$($docker_prefix compose -p "$TARGET_PROJECT" --env-file "$env_file" -f "$base" -f "$override" exec -T postgres \
  psql -U sentinel -d sentinel -Atc "select 'organizations='||count(*) from organizations union all select 'services='||count(*) from services union all select 'releases='||count(*) from releases")
printf '%s\n' "$counts"
log "Restore concluído. Projeto $TARGET_PROJECT permanece ativo para testes de isolamento, API e RTO/RPO."
